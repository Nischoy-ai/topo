package sshlinux

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Nischoy-ai/topo/pkg/discovery"
	"github.com/Nischoy-ai/topo/pkg/model"
	"golang.org/x/crypto/ssh"
)

type DialContextFunc func(context.Context, string, string) (net.Conn, error)
type Config struct {
	Password                       string
	Signer                         ssh.Signer
	HostKeyCallback                ssh.HostKeyCallback
	Concurrency                    int
	ConnectTimeout, CommandTimeout time.Duration
	MaxOutputBytes                 int64
	DialContext                    DialContextFunc
}
type Plugin struct{ Config Config }
type target struct{ Address, Username string }

func (p Plugin) DescribeCapabilities(context.Context) discovery.Capability {
	return discovery.Capability{Name: "ssh-linux", Version: "0.1.0", AssetTypes: []model.AssetType{model.AssetHost, model.AssetNetworkInterface}, RequiredPermissions: []string{"read /etc/machine-id and /etc/os-release", "read DMI product serial", "list interfaces, packages, and services"}}
}
func (p Plugin) ValidateConfiguration(_ context.Context, r discovery.Request) error {
	if len(r.Targets) == 0 {
		return errors.New("at least one SSH target is required")
	}
	if p.Config.HostKeyCallback == nil {
		return errors.New("SSH host key callback is required")
	}
	if p.Config.Password == "" && p.Config.Signer == nil {
		return errors.New("SSH password or private key is required")
	}
	if p.Config.Concurrency < 0 || p.Config.Concurrency > 1024 {
		return errors.New("SSH concurrency must be between 0 (default) and 1024")
	}
	if p.Config.MaxOutputBytes < 0 {
		return errors.New("SSH maximum command output cannot be negative")
	}
	for _, raw := range r.Targets {
		if _, err := parseTarget(raw); err != nil {
			return err
		}
	}
	return nil
}
func (p Plugin) CheckConnectivity(ctx context.Context, r discovery.Request) error {
	if err := p.ValidateConfiguration(ctx, r); err != nil {
		return err
	}
	t, _ := parseTarget(r.Targets[0])
	client, err := p.dial(ctx, t)
	if err != nil {
		return err
	}
	return client.Close()
}
func (p Plugin) Discover(ctx context.Context, r discovery.Request) (model.ObservationEnvelope, error) {
	if err := p.ValidateConfiguration(ctx, r); err != nil {
		return model.ObservationEnvelope{}, err
	}
	now := time.Now().UTC()
	obs := model.ObservationEnvelope{SchemaVersion: model.SchemaVersion, ObservationID: sshObservationID(), SiteID: defaultValue(r.SiteID, "default"), CollectorID: defaultValue(r.CollectorID, "ssh-relay"), Plugin: "ssh-linux", JobID: r.JobID, ObservedAt: now}
	concurrency := p.Config.Concurrency
	if concurrency < 1 {
		concurrency = 32
	}
	jobs := make(chan target)
	var wg sync.WaitGroup
	var mu sync.Mutex
	for range concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for t := range jobs {
				inv, errs := p.discoverTarget(ctx, t)
				mu.Lock()
				if inv != nil {
					assets, rels := inv.Assets(now)
					obs.Assets = append(obs.Assets, assets...)
					obs.Relationships = append(obs.Relationships, rels...)
				}
				obs.Errors = append(obs.Errors, errs...)
				mu.Unlock()
			}
		}()
	}
	for _, raw := range r.Targets {
		t, _ := parseTarget(raw)
		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return model.ObservationEnvelope{}, ctx.Err()
		case jobs <- t:
		}
	}
	close(jobs)
	wg.Wait()
	return obs, nil
}

func (p Plugin) discoverTarget(ctx context.Context, t target) (*Inventory, []model.CollectionError) {
	client, err := p.dial(ctx, t)
	if err != nil {
		return nil, []model.CollectionError{{Code: "ssh_connect", Message: t.Address + ": " + err.Error(), Retryable: true}}
	}
	defer client.Close()
	outputs := map[string]string{}
	errs := []model.CollectionError{}
	for _, cmd := range AllowedCommands {
		out, err := p.runCommand(ctx, client, cmd)
		if err != nil {
			required := cmd != CommandPackages && cmd != CommandServices
			if required {
				return nil, append(errs, model.CollectionError{Code: "ssh_command", Message: t.Address + ": " + commandLabel(cmd) + ": " + err.Error(), Retryable: true})
			}
			errs = append(errs, model.CollectionError{Code: "ssh_partial", Message: t.Address + ": " + commandLabel(cmd) + ": " + err.Error(), Retryable: false})
			continue
		}
		outputs[cmd] = out
	}
	osName, osVersion := ParseOSRelease(outputs[CommandOSRelease])
	cpu, err := ParsePositiveInt("CPU count", outputs[CommandCPUCount])
	if err != nil {
		return nil, append(errs, model.CollectionError{Code: "ssh_parse", Message: t.Address + ": " + err.Error()})
	}
	memory, err := ParsePositiveInt("memory", outputs[CommandMemoryMB])
	if err != nil {
		return nil, append(errs, model.CollectionError{Code: "ssh_parse", Message: t.Address + ": " + err.Error()})
	}
	ifaces, err := ParseInterfaces(outputs[CommandInterfaces])
	if err != nil {
		return nil, append(errs, model.CollectionError{Code: "ssh_parse", Message: t.Address + ": " + err.Error()})
	}
	inv := &Inventory{Hostname: strings.TrimSpace(outputs[CommandHostname]), MachineID: strings.TrimSpace(outputs[CommandMachineID]), OSName: osName, OSVersion: osVersion, Architecture: strings.TrimSpace(outputs[CommandArch]), Serial: strings.TrimSpace(outputs[CommandSerial]), CPUCount: cpu, MemoryMB: memory, Interfaces: ifaces, Packages: ParseLines(outputs[CommandPackages]), Services: ParseServices(outputs[CommandServices])}
	if inv.Hostname == "" || inv.MachineID == "" {
		return nil, append(errs, model.CollectionError{Code: "ssh_parse", Message: t.Address + ": empty hostname or machine ID"})
	}
	return inv, errs
}
func (p Plugin) dial(ctx context.Context, t target) (*ssh.Client, error) {
	timeout := p.Config.ConnectTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	auth := []ssh.AuthMethod{}
	if p.Config.Signer != nil {
		auth = append(auth, ssh.PublicKeys(p.Config.Signer))
	}
	if p.Config.Password != "" {
		auth = append(auth, ssh.Password(p.Config.Password))
	}
	cfg := &ssh.ClientConfig{User: t.Username, Auth: auth, HostKeyCallback: p.Config.HostKeyCallback, Timeout: timeout}
	dial := p.Config.DialContext
	if dial == nil {
		d := &net.Dialer{Timeout: timeout}
		dial = d.DialContext
	}
	conn, err := dial(ctx, "tcp", t.Address)
	if err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(timeout))
	cc, chans, reqs, err := ssh.NewClientConn(conn, t.Address, cfg)
	if err != nil {
		conn.Close()
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})
	return ssh.NewClient(cc, chans, reqs), nil
}
func (p Plugin) runCommand(ctx context.Context, client *ssh.Client, cmd string) (string, error) {
	if !IsAllowedCommand(cmd) {
		return "", errors.New("command is not allowlisted")
	}
	timeout := p.Config.CommandTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	session, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()
	maxOutput := p.Config.MaxOutputBytes
	if maxOutput == 0 {
		maxOutput = 4 << 20
	}
	output := &boundedOutput{remaining: maxOutput}
	session.Stdout = output
	session.Stderr = output
	type result struct{ err error }
	done := make(chan result, 1)
	go func() { done <- result{err: session.Run(cmd)} }()
	select {
	case <-cmdCtx.Done():
		_ = client.Close()
		return "", cmdCtx.Err()
	case r := <-done:
		if output.exceeded {
			return "", fmt.Errorf("command output exceeded %d bytes", maxOutput)
		}
		return output.String(), r.err
	}
}

type boundedOutput struct {
	mu        sync.Mutex
	data      strings.Builder
	remaining int64
	exceeded  bool
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	originalLength := len(p)
	if int64(len(p)) > b.remaining {
		p = p[:b.remaining]
		b.exceeded = true
	}
	_, _ = io.WriteString(&b.data, string(p))
	b.remaining -= int64(len(p))
	// Report the entire write as consumed so SSH keeps draining the channel.
	return originalLength, nil
}

func (b *boundedOutput) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.data.String()
}
func parseTarget(raw string) (target, error) {
	at := strings.LastIndex(raw, "@")
	if at <= 0 || at == len(raw)-1 {
		return target{}, fmt.Errorf("SSH target %q must be username@host:port", raw)
	}
	address := raw[at+1:]
	if _, _, err := net.SplitHostPort(address); err != nil {
		return target{}, fmt.Errorf("SSH target %q: %w", raw, err)
	}
	return target{Username: raw[:at], Address: address}, nil
}
func commandLabel(cmd string) string {
	for i, c := range AllowedCommands {
		if c == cmd {
			return "command_" + strconv.Itoa(i+1)
		}
	}
	return "command"
}
func defaultValue(v, d string) string {
	if v == "" {
		return d
	}
	return v
}
func sshObservationID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("obs-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
