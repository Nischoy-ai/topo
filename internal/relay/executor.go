package relay

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Nischoy-ai/topo/pkg/credentialref"
	"github.com/Nischoy-ai/topo/pkg/discovery"
	localdiscovery "github.com/Nischoy-ai/topo/pkg/discovery/local"
	"github.com/Nischoy-ai/topo/pkg/discovery/sshlinux"
	"github.com/Nischoy-ai/topo/pkg/model"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

const maxDiscoveryDuration = 30 * time.Minute

// Executor turns a locally configured profile into one invocation of a
// compiled-in discovery plugin.
type Executor struct {
	Config FileConfig
}

func (e Executor) Execute(ctx context.Context, job Job) (model.ObservationEnvelope, error) {
	if err := validateJob(job); err != nil {
		return model.ObservationEnvelope{}, err
	}
	profile, ok := e.Config.Profile(job.ProfileID)
	if !ok {
		return model.ObservationEnvelope{}, fmt.Errorf("unknown local discovery profile %q", job.ProfileID)
	}
	plugin, targets, timeout, err := buildPlugin(profile)
	if err != nil {
		return model.ObservationEnvelope{}, err
	}
	discoverCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	request := discovery.Request{
		JobID:       job.JobID,
		SiteID:      e.Config.SiteID,
		CollectorID: e.Config.RelayID,
		Targets:     targets,
		Deadline:    time.Now().Add(timeout),
	}
	if err := plugin.ValidateConfiguration(discoverCtx, request); err != nil {
		return model.ObservationEnvelope{}, fmt.Errorf("validate profile %q: %w", profile.ID, err)
	}
	observation, err := plugin.Discover(discoverCtx, request)
	if err != nil {
		return model.ObservationEnvelope{}, fmt.Errorf("discover profile %q: %w", profile.ID, err)
	}
	return observation, nil
}

func buildPlugin(profile ProfileConfig) (discovery.Plugin, []string, time.Duration, error) {
	switch profile.Plugin {
	case "local":
		return localdiscovery.Plugin{}, []string{"local"}, 30 * time.Second, nil
	case "ssh-linux":
		return buildSSHPlugin(profile)
	default:
		return nil, nil, 0, fmt.Errorf("unsupported local discovery plugin %q", profile.Plugin)
	}
}

func buildSSHPlugin(profile ProfileConfig) (discovery.Plugin, []string, time.Duration, error) {
	if profile.SSH == nil {
		return nil, nil, 0, errors.New("ssh configuration is required")
	}
	targets, err := readTargets(profile.TargetsFile)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("profile %q: %w", profile.ID, err)
	}
	callback, err := knownhosts.New(profile.SSH.KnownHostsFile)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("profile %q load known_hosts: %w", profile.ID, err)
	}
	var signer ssh.Signer
	if profile.SSH.PrivateKeyRef != "" {
		key, err := credentialref.Resolve(profile.SSH.PrivateKeyRef)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("profile %q resolve SSH private key: %w", profile.ID, err)
		}
		signer, err = ssh.ParsePrivateKey(key)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("profile %q parse SSH private key: %w", profile.ID, err)
		}
	}
	var password string
	if profile.SSH.PasswordRef != "" {
		value, err := credentialref.Resolve(profile.SSH.PasswordRef)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("profile %q resolve SSH password: %w", profile.ID, err)
		}
		password = string(value)
	}
	connectTimeout, _ := parseDuration(profile.SSH.ConnectTimeout, 10*time.Second)
	commandTimeout, _ := parseDuration(profile.SSH.CommandTimeout, 10*time.Second)
	concurrency := profile.SSH.Concurrency
	if concurrency == 0 {
		concurrency = defaultConcurrency
	}
	maxOutput := profile.SSH.MaxOutputBytes
	if maxOutput == 0 {
		maxOutput = 4 << 20
	}
	plugin := sshlinux.Plugin{Config: sshlinux.Config{
		Password:        password,
		Signer:          signer,
		HostKeyCallback: callback,
		Concurrency:     concurrency,
		ConnectTimeout:  connectTimeout,
		CommandTimeout:  commandTimeout,
		MaxOutputBytes:  maxOutput,
	}}
	jobTimeout := time.Duration(len(targets)/concurrency+1) * (connectTimeout + 8*commandTimeout)
	if jobTimeout > maxDiscoveryDuration {
		jobTimeout = maxDiscoveryDuration
	}
	return plugin, targets, jobTimeout, nil
}
