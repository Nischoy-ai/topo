package worker

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"time"

	"github.com/Nischoy-ai/topo/pkg/discovery"
	localdiscovery "github.com/Nischoy-ai/topo/pkg/discovery/local"
	"github.com/Nischoy-ai/topo/pkg/discovery/sshlinux"
	"github.com/Nischoy-ai/topo/pkg/model"
	"golang.org/x/crypto/ssh"
)

var ErrCredentialResolution = errors.New("credential resolution failed")

type CredentialSource interface {
	SSH(context.Context) (SSHCredential, error)
}

type Executor struct {
	Policy             Policy
	Now                func() time.Time
	SSHHostKeyCallback ssh.HostKeyCallback
	SSHDialContext     sshlinux.DialContextFunc
}

func (e Executor) Execute(ctx context.Context, task Task) (model.ObservationEnvelope, error) {
	return e.execute(ctx, task, nil)
}

func (e Executor) ExecuteWithCredentials(ctx context.Context, task Task, credentials CredentialSource) (model.ObservationEnvelope, error) {
	return e.execute(ctx, task, credentials)
}

func (e Executor) execute(ctx context.Context, task Task, credentials CredentialSource) (model.ObservationEnvelope, error) {
	if err := e.Policy.Validate(); err != nil {
		return model.ObservationEnvelope{}, err
	}
	if err := validateTask(task); err != nil {
		return model.ObservationEnvelope{}, err
	}
	now := time.Now
	if e.Now != nil {
		now = e.Now
	}
	current := now().UTC()
	if !task.LeaseExpiresAt.After(current) {
		return model.ObservationEnvelope{}, errors.New("task lease has expired")
	}
	if !task.Deadline.After(current) {
		return model.ObservationEnvelope{}, errors.New("task deadline has expired")
	}
	deadline := current.Add(e.Policy.taskDuration())
	if task.Deadline.Before(deadline) {
		deadline = task.Deadline
	}
	discoverCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	switch task.Operation {
	case OperationLocalV1:
		return e.executeLocal(discoverCtx, task, deadline)
	case OperationSSHLinuxV1:
		return e.executeSSH(discoverCtx, task, deadline, credentials)
	default:
		return model.ObservationEnvelope{}, fmt.Errorf("operation %q is not allowed by local worker policy", task.Operation)
	}
}

func (e Executor) executeLocal(ctx context.Context, task Task, deadline time.Time) (model.ObservationEnvelope, error) {
	if !e.Policy.AllowLocal {
		return model.ObservationEnvelope{}, fmt.Errorf("operation %q is not allowed by local worker policy", task.Operation)
	}
	if task.TargetPartition != nil || task.CredentialBindingID != "" {
		return model.ObservationEnvelope{}, errors.New("local.v1 does not accept remote target or credential authority")
	}
	request := discovery.Request{
		JobID:       task.TaskID,
		SiteID:      e.Policy.SiteID,
		CollectorID: "worker-pool-" + e.Policy.WorkerPool,
		Targets:     []string{"local"},
		Deadline:    deadline,
	}
	plugin := localdiscovery.Plugin{}
	if err := plugin.ValidateConfiguration(ctx, request); err != nil {
		return model.ObservationEnvelope{}, fmt.Errorf("validate local.v1: %w", err)
	}
	observation, err := plugin.Discover(ctx, request)
	if err != nil {
		return model.ObservationEnvelope{}, fmt.Errorf("execute local.v1: %w", err)
	}
	return observation, nil
}

func (e Executor) executeSSH(ctx context.Context, task Task, deadline time.Time, credentials CredentialSource) (model.ObservationEnvelope, error) {
	if !e.Policy.AllowSSHLinux || e.SSHHostKeyCallback == nil {
		return model.ObservationEnvelope{}, fmt.Errorf("operation %q is not allowed by local worker policy", task.Operation)
	}
	if task.TargetPartition == nil || len(task.TargetPartition.CIDRs) != 1 {
		return model.ObservationEnvelope{}, errors.New("ssh_linux.v1 requires one IPv4 /32 target partition")
	}
	prefix, err := netip.ParsePrefix(task.TargetPartition.CIDRs[0])
	if err != nil || !prefix.Addr().Is4() || prefix.Bits() != 32 {
		return model.ObservationEnvelope{}, errors.New("ssh_linux.v1 target must be a canonical IPv4 /32")
	}
	if !targetAllowed(prefix.Addr(), e.Policy.SSHAllowlist) {
		return model.ObservationEnvelope{}, errors.New("ssh_linux.v1 target is outside the local allowlist")
	}
	if credentials == nil {
		return model.ObservationEnvelope{}, errors.New("ssh_linux.v1 requires an attempt-bound credential source")
	}
	credential, err := credentials.SSH(ctx)
	if err != nil {
		return model.ObservationEnvelope{}, fmt.Errorf("%w", ErrCredentialResolution)
	}
	if !safeSSHUsername(credential.Username) || credential.Password == "" || len(credential.Password) > 4096 {
		return model.ObservationEnvelope{}, ErrCredentialResolution
	}
	target := credential.Username + "@" + net.JoinHostPort(prefix.Addr().String(), "22")
	request := discovery.Request{
		JobID:       task.TaskID,
		SiteID:      e.Policy.SiteID,
		CollectorID: "worker-pool-" + e.Policy.WorkerPool,
		Targets:     []string{target},
		Deadline:    deadline,
	}
	plugin := sshlinux.Plugin{Config: sshlinux.Config{
		Password:        credential.Password,
		HostKeyCallback: e.SSHHostKeyCallback,
		Concurrency:     1,
		ConnectTimeout:  10 * time.Second,
		CommandTimeout:  10 * time.Second,
		MaxOutputBytes:  64 << 10,
		DialContext:     e.SSHDialContext,
	}}
	if err := plugin.ValidateConfiguration(ctx, request); err != nil {
		return model.ObservationEnvelope{}, fmt.Errorf("validate ssh_linux.v1: %w", err)
	}
	observation, err := plugin.Discover(ctx, request)
	if err != nil {
		return model.ObservationEnvelope{}, fmt.Errorf("execute ssh_linux.v1: %w", err)
	}
	return observation, nil
}
