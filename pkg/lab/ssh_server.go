package lab

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Nischoy-ai/topo/pkg/discovery/sshlinux"
	"golang.org/x/crypto/ssh"
)

const LabSSHPassword = "topo-lab"

// SSHServer exposes a Topo Lab estate over real SSH handshakes and sessions.
// The SSH username selects the simulated host; arbitrary commands are rejected.
type SSHServer struct {
	Estate Estate
	signer ssh.Signer
	mu     sync.Mutex
	calls  map[string]int
}

func NewSSHServer(estate Estate) (*SSHServer, error) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate SSH host key: %w", err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		return nil, fmt.Errorf("create SSH signer: %w", err)
	}
	return &SSHServer{Estate: estate, signer: signer, calls: map[string]int{}}, nil
}

func (s *SSHServer) PublicKey() ssh.PublicKey { return s.signer.PublicKey() }

func (s *SSHServer) Serve(listener net.Listener) error {
	for {
		conn, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		go func() { _ = s.ServeConn(conn) }()
	}
}

// ServeConn serves one SSH connection. It is exported so tests can use net.Pipe
// and create hundreds of real SSH sessions without opening local ports.
func (s *SSHServer) ServeConn(conn net.Conn) error {
	defer conn.Close()
	config := &ssh.ServerConfig{
		PasswordCallback: func(metadata ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			host, ok := s.host(metadata.User())
			if !ok || host.OS != "linux" {
				return nil, errors.New("unknown Linux host")
			}
			if host.Fault == "auth_failure" || string(password) != LabSSHPassword {
				return nil, errors.New("authentication rejected")
			}
			s.mu.Lock()
			s.calls[host.ID]++
			calls := s.calls[host.ID]
			s.mu.Unlock()
			if host.Fault == "disappear" && calls > 1 {
				return nil, errors.New("simulated host disappeared")
			}
			return &ssh.Permissions{Extensions: map[string]string{"topo-lab-host": host.ID}}, nil
		},
	}
	config.AddHostKey(s.signer)
	serverConn, channels, requests, err := ssh.NewServerConn(conn, config)
	if err != nil {
		return err
	}
	defer serverConn.Close()
	go ssh.DiscardRequests(requests)

	host, ok := s.host(serverConn.Permissions.Extensions["topo-lab-host"])
	if !ok {
		return errors.New("authenticated host no longer exists")
	}
	done := make(chan struct{})
	defer close(done)
	for newChannel := range channels {
		if newChannel.ChannelType() != "session" {
			_ = newChannel.Reject(ssh.UnknownChannelType, "only session channels are supported")
			continue
		}
		channel, reqs, err := newChannel.Accept()
		if err != nil {
			continue
		}
		go s.serveSession(channel, reqs, host, done)
	}
	return nil
}

func (s *SSHServer) serveSession(channel ssh.Channel, requests <-chan *ssh.Request, host Host, connectionDone <-chan struct{}) {
	defer channel.Close()
	for request := range requests {
		if request.Type != "exec" {
			_ = request.Reply(false, nil)
			continue
		}
		var payload struct{ Command string }
		if err := ssh.Unmarshal(request.Payload, &payload); err != nil {
			_ = request.Reply(false, nil)
			return
		}
		_ = request.Reply(true, nil)
		output, status := s.command(host, payload.Command, connectionDone)
		if output != "" {
			_, _ = io.WriteString(channel, output)
		}
		_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{status}))
		return
	}
}

func (s *SSHServer) command(host Host, command string, connectionDone <-chan struct{}) (string, uint32) {
	if !sshlinux.IsAllowedCommand(command) {
		return "command rejected by Topo Lab allowlist\n", 126
	}
	if host.Fault == "timeout" {
		<-connectionDone
		return "", 255
	}
	if host.LatencyMS > 0 {
		select {
		case <-connectionDone:
			return "", 255
		case <-time.After(time.Duration(host.LatencyMS) * time.Millisecond):
		}
	}
	if host.Fault == "permission_denied" && (command == sshlinux.CommandPackages || command == sshlinux.CommandServices) {
		return "permission denied\n", 1
	}

	switch command {
	case sshlinux.CommandHostname:
		return host.Name + "\n", 0
	case sshlinux.CommandMachineID:
		return host.MachineID + "\n", 0
	case sshlinux.CommandOSRelease:
		return fmt.Sprintf("NAME=%q\nVERSION_ID=%q\nID=%q\n", osName(host.Persona), osVersion(host.Persona), osID(host.Persona)), 0
	case sshlinux.CommandArch:
		return host.Architecture + "\n", 0
	case sshlinux.CommandCPUCount:
		return strconv.Itoa(host.CPUCount) + "\n", 0
	case sshlinux.CommandMemoryMB:
		return strconv.Itoa(host.MemoryMB) + "\n", 0
	case sshlinux.CommandSerial:
		return host.Serial + "\n", 0
	case sshlinux.CommandInterfaces:
		if host.Fault == "malformed" {
			return `[{"ifindex":`, 0
		}
		value := []map[string]any{{"ifindex": 1, "ifname": "primary", "address": host.MACAddress, "addr_info": []map[string]any{{"local": host.IPAddress, "prefixlen": 24}}}}
		encoded, _ := json.Marshal(value)
		return string(encoded) + "\n", 0
	case sshlinux.CommandPackages:
		return strings.Join(host.Packages, "\n") + "\n", 0
	case sshlinux.CommandServices:
		lines := make([]string, 0, len(host.Services))
		for _, service := range host.Services {
			lines = append(lines, service+".service loaded active running Topo Lab service")
		}
		return strings.Join(lines, "\n") + "\n", 0
	default:
		return "command rejected by Topo Lab allowlist\n", 126
	}
}

func (s *SSHServer) host(id string) (Host, bool) {
	for _, host := range s.Estate.Hosts {
		if host.ID == id {
			return host, true
		}
	}
	return Host{}, false
}

func osName(persona string) string {
	switch {
	case strings.HasPrefix(persona, "ubuntu-"):
		return "Ubuntu"
	case strings.HasPrefix(persona, "rocky-"):
		return "Rocky Linux"
	default:
		return "Linux"
	}
}

func osID(persona string) string {
	if strings.HasPrefix(persona, "ubuntu-") {
		return "ubuntu"
	}
	if strings.HasPrefix(persona, "rocky-") {
		return "rocky"
	}
	return "linux"
}

func osVersion(persona string) string {
	parts := strings.SplitN(persona, "-", 2)
	if len(parts) == 2 {
		return parts[1]
	}
	return persona
}
