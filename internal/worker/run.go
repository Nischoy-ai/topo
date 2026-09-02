package worker

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/Nischoy-ai/topo/pkg/model"
)

const (
	DefaultPollInterval = 15 * time.Second
	MaxPollInterval     = time.Hour
	maxFailureBytes     = 4096
)

type ControlPlane interface {
	Register(context.Context, RegisterRequest) (RegisterResponse, error)
	Heartbeat(context.Context, HeartbeatRequest) (HeartbeatResponse, error)
	Claim(context.Context, ClaimRequest) (ClaimResponse, error)
	Renew(context.Context, string, RenewRequest) (RenewResponse, error)
	Credential(context.Context, string, CredentialRequest) (SSHCredential, error)
	SubmitResult(context.Context, string, ResultChunkRequest) (ResultChunkResponse, error)
	Complete(context.Context, string, CompleteRequest) (CompleteResponse, error)
}

type TaskExecutor interface {
	Execute(context.Context, Task) (model.ObservationEnvelope, error)
}

type credentialedTaskExecutor interface {
	ExecuteWithCredentials(context.Context, Task, CredentialSource) (model.ObservationEnvelope, error)
}

type RunConfig struct {
	Policy       Policy
	Version      string
	PollInterval time.Duration
	Control      ControlPlane
	Executor     TaskExecutor
	Logger       *slog.Logger
	Now          func() time.Time
	BootID       string
}

type registration struct {
	workerID string
	bootID   string
}

type attemptCredentialSource struct {
	control ControlPlane
	reg     registration
	task    Task
}

func (s attemptCredentialSource) SSH(ctx context.Context) (SSHCredential, error) {
	return s.control.Credential(ctx, s.task.TaskID, CredentialRequest{
		SchemaVersion: ContractVersion,
		WorkerID:      s.reg.workerID,
		BootID:        s.reg.bootID,
		AttemptID:     s.task.AttemptID,
		LeaseToken:    s.task.LeaseToken,
	})
}

type activeTasks struct {
	mu     sync.Mutex
	cancel map[string]context.CancelFunc
	wait   sync.WaitGroup
}

var (
	errLeaseLost     = errors.New("task lease could not be renewed before expiry")
	errTaskCancelled = errors.New("task was cancelled by ServiceNow")
)

func Run(ctx context.Context, config RunConfig) error {
	if err := config.Policy.Validate(); err != nil {
		return err
	}
	if config.Control == nil {
		return errors.New("worker control plane is required")
	}
	if config.Executor == nil {
		return errors.New("worker executor is required")
	}
	if config.PollInterval == 0 {
		config.PollInterval = DefaultPollInterval
	}
	if config.PollInterval < time.Second || config.PollInterval > MaxPollInterval {
		return errors.New("worker poll interval must be between 1s and 1h")
	}
	logger := config.Logger
	if logger == nil {
		logger = slog.Default()
	}
	reg, err := register(ctx, config)
	if err != nil {
		return err
	}
	state := &activeTasks{cancel: make(map[string]context.CancelFunc)}
	runCycle(ctx, config, reg, logger, state)
	ticker := time.NewTicker(config.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			state.cancelAll()
			state.wait.Wait()
			return nil
		case <-ticker.C:
			runCycle(ctx, config, reg, logger, state)
		}
	}
}

func register(ctx context.Context, config RunConfig) (registration, error) {
	bootID := config.BootID
	if bootID == "" {
		var err error
		bootID, err = randomID()
		if err != nil {
			return registration{}, err
		}
	}
	if !safeID.MatchString(bootID) {
		return registration{}, errors.New("worker boot ID is invalid")
	}
	digest, err := config.Policy.Digest()
	if err != nil {
		return registration{}, err
	}
	now := time.Now
	if config.Now != nil {
		now = config.Now
	}
	request := RegisterRequest{
		SchemaVersion:  ContractVersion,
		BootID:         bootID,
		WorkerPool:     config.Policy.WorkerPool,
		SiteID:         config.Policy.SiteID,
		Version:        truncate(config.Version, 64),
		Capabilities:   config.Policy.Capabilities(),
		PolicyDigest:   digest,
		MaxConcurrency: config.Policy.concurrency(),
		StartedAt:      now().UTC(),
	}
	callCtx, cancel := context.WithTimeout(ctx, controlRequestTimeout)
	response, err := config.Control.Register(callCtx, request)
	cancel()
	if err != nil {
		return registration{}, fmt.Errorf("register ServiceNow worker: %w", err)
	}
	if !safeID.MatchString(response.WorkerID) {
		return registration{}, errors.New("ServiceNow returned an invalid worker ID")
	}
	return registration{workerID: response.WorkerID, bootID: bootID}, nil
}

func runCycle(ctx context.Context, config RunConfig, reg registration, logger *slog.Logger, state *activeTasks) {
	now := time.Now
	if config.Now != nil {
		now = config.Now
	}
	heartbeatCtx, cancel := context.WithTimeout(ctx, controlRequestTimeout)
	heartbeat, heartbeatErr := config.Control.Heartbeat(heartbeatCtx, HeartbeatRequest{
		SchemaVersion: ContractVersion,
		WorkerID:      reg.workerID,
		BootID:        reg.bootID,
		CurrentLeases: state.count(),
		SentAt:        now().UTC(),
	})
	cancel()
	if heartbeatErr != nil {
		logger.Warn("ServiceNow worker heartbeat failed", "error", heartbeatErr)
		return
	}
	state.cancelAttempts(heartbeat.CancelAttemptIDs)
	for state.count() < config.Policy.concurrency() {
		claimCtx, cancel := context.WithTimeout(ctx, controlRequestTimeout)
		claim, err := config.Control.Claim(claimCtx, ClaimRequest{
			SchemaVersion: ContractVersion,
			WorkerID:      reg.workerID,
			BootID:        reg.bootID,
			Capabilities:  config.Policy.Capabilities(),
			CurrentLeases: state.count(),
		})
		cancel()
		if err != nil {
			logger.Warn("ServiceNow worker claim failed", "error", err)
			return
		}
		if claim.Task == nil {
			return
		}
		taskCtx, taskCancel := context.WithCancel(ctx)
		if !state.add(claim.Task.AttemptID, taskCancel, config.Policy.concurrency()) {
			taskCancel()
			logger.Warn("ServiceNow returned work after local concurrency was exhausted", "task_id", claim.Task.TaskID)
			return
		}
		state.wait.Add(1)
		go func(task Task) {
			defer state.wait.Done()
			defer state.remove(task.AttemptID)
			defer taskCancel()
			executeTask(ctx, taskCtx, taskCancel, config, reg, logger, task)
		}(*claim.Task)
	}
}

func executeTask(rootCtx, taskCtx context.Context, taskCancel context.CancelFunc, config RunConfig, reg registration, logger *slog.Logger, task Task) {
	logger.Info("executing ServiceNow task", "task_id", task.TaskID, "run_id", task.RunID, "attempt_id", task.AttemptID, "operation", task.Operation)
	renewCtx, stopRenew := context.WithCancel(rootCtx)
	renewDone := make(chan error, 1)
	go func() {
		renewDone <- maintainLease(renewCtx, taskCancel, config.Control, reg, task)
	}()
	var stopOnce sync.Once
	var renewErr error
	stopLease := func() error {
		stopOnce.Do(func() {
			stopRenew()
			renewErr = <-renewDone
		})
		return renewErr
	}
	defer stopLease()

	var observation model.ObservationEnvelope
	var err error
	if executor, ok := config.Executor.(credentialedTaskExecutor); ok {
		observation, err = executor.ExecuteWithCredentials(taskCtx, task, attemptCredentialSource{control: config.Control, reg: reg, task: task})
	} else {
		observation, err = config.Executor.Execute(taskCtx, task)
	}
	if err != nil {
		if rootCtx.Err() != nil {
			return
		}
		if errors.Is(err, context.Canceled) {
			renewErr := stopLease()
			if errors.Is(renewErr, errLeaseLost) {
				return
			}
			reportFailure(rootCtx, config.Control, reg, task, "cancelled", errTaskCancelled, logger)
			return
		}
		code := "operation_failed"
		if errors.Is(err, ErrCredentialResolution) {
			code = "credential_resolution_failed"
		}
		reportFailure(rootCtx, config.Control, reg, task, code, err, logger)
		_ = stopLease()
		return
	}
	payload, err := json.Marshal(observation)
	if err != nil {
		reportFailure(rootCtx, config.Control, reg, task, "observation_encode_failed", err, logger)
		_ = stopLease()
		return
	}
	if len(payload) > maxControlRequestBytes/2 {
		reportFailure(rootCtx, config.Control, reg, task, "observation_too_large", fmt.Errorf("observation exceeds %d bytes", maxControlRequestBytes/2), logger)
		_ = stopLease()
		return
	}
	sum := sha256.Sum256(payload)
	chunk := ResultChunkRequest{
		SchemaVersion:   ContractVersion,
		WorkerID:        reg.workerID,
		BootID:          reg.bootID,
		AttemptID:       task.AttemptID,
		LeaseToken:      task.LeaseToken,
		ChunkNumber:     0,
		ChunkCount:      1,
		Checksum:        hex.EncodeToString(sum[:]),
		ObservationJSON: string(payload),
	}
	resultCtx, cancel := context.WithTimeout(taskCtx, controlRequestTimeout)
	ack, err := config.Control.SubmitResult(resultCtx, task.TaskID, chunk)
	cancel()
	if err != nil {
		renewErr := stopLease()
		if errors.Is(renewErr, errTaskCancelled) && rootCtx.Err() == nil {
			reportFailure(rootCtx, config.Control, reg, task, "cancelled", errTaskCancelled, logger)
			return
		}
		logger.Warn("ServiceNow result chunk was not acknowledged; task will recover by lease expiry", "task_id", task.TaskID, "attempt_id", task.AttemptID, "error", err)
		return
	}
	if !ack.Accepted {
		_ = stopLease()
		logger.Warn("ServiceNow rejected result chunk; task will recover by lease expiry", "task_id", task.TaskID, "attempt_id", task.AttemptID)
		return
	}
	completeCtx, cancel := context.WithTimeout(taskCtx, controlRequestTimeout)
	_, err = config.Control.Complete(completeCtx, task.TaskID, CompleteRequest{
		SchemaVersion: ContractVersion,
		WorkerID:      reg.workerID,
		BootID:        reg.bootID,
		AttemptID:     task.AttemptID,
		LeaseToken:    task.LeaseToken,
		Success:       true,
		ChunkCount:    1,
	})
	cancel()
	renewErr = stopLease()
	if errors.Is(renewErr, errTaskCancelled) && rootCtx.Err() == nil {
		reportFailure(rootCtx, config.Control, reg, task, "cancelled", errTaskCancelled, logger)
		return
	}
	if err != nil {
		logger.Warn("ServiceNow task completion was not acknowledged; no local state was retained", "task_id", task.TaskID, "attempt_id", task.AttemptID, "error", err)
	}
}

func maintainLease(ctx context.Context, cancelTask context.CancelFunc, control ControlPlane, reg registration, task Task) error {
	expires := task.LeaseExpiresAt
	var lastError error
	for {
		now := time.Now().UTC()
		remaining := expires.Sub(now)
		if remaining <= 0 {
			cancelTask()
			if lastError != nil {
				return fmt.Errorf("%w: %v", errLeaseLost, lastError)
			}
			return errLeaseLost
		}
		delay := remaining / 2
		if delay > 30*time.Second {
			delay = 30 * time.Second
		}
		if delay < 10*time.Millisecond {
			delay = 10 * time.Millisecond
		}
		if delay >= remaining {
			delay = remaining / 2
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil
		case <-timer.C:
		}

		callCtx, cancel := context.WithDeadline(ctx, expires)
		response, err := control.Renew(callCtx, task.TaskID, RenewRequest{
			SchemaVersion: ContractVersion,
			WorkerID:      reg.workerID,
			BootID:        reg.bootID,
			AttemptID:     task.AttemptID,
			LeaseToken:    task.LeaseToken,
		})
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			lastError = err
			continue
		}
		lastError = nil
		if response.Cancelled {
			cancelTask()
			return errTaskCancelled
		}
		if !response.LeaseExpiresAt.After(time.Now().UTC()) || response.LeaseExpiresAt.After(task.Deadline) {
			cancelTask()
			return fmt.Errorf("%w: ServiceNow returned an invalid lease expiry", errLeaseLost)
		}
		expires = response.LeaseExpiresAt
	}
}

func reportFailure(ctx context.Context, control ControlPlane, reg registration, task Task, code string, cause error, logger *slog.Logger) {
	message := truncate(cause.Error(), maxFailureBytes)
	completeCtx, cancel := context.WithTimeout(ctx, controlRequestTimeout)
	_, err := control.Complete(completeCtx, task.TaskID, CompleteRequest{
		SchemaVersion: ContractVersion,
		WorkerID:      reg.workerID,
		BootID:        reg.bootID,
		AttemptID:     task.AttemptID,
		LeaseToken:    task.LeaseToken,
		Success:       false,
		Failure:       &Failure{Code: code, Message: message},
	})
	cancel()
	if err != nil {
		logger.Warn("ServiceNow task failure was not acknowledged; no local state was retained", "task_id", task.TaskID, "attempt_id", task.AttemptID, "error", err)
	}
}

func (s *activeTasks) add(attemptID string, cancel context.CancelFunc, maximum int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.cancel) >= maximum {
		return false
	}
	if _, exists := s.cancel[attemptID]; exists {
		return false
	}
	s.cancel[attemptID] = cancel
	return true
}

func (s *activeTasks) remove(attemptID string) {
	s.mu.Lock()
	delete(s.cancel, attemptID)
	s.mu.Unlock()
}

func (s *activeTasks) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.cancel)
}

func (s *activeTasks) cancelAttempts(attemptIDs []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, attemptID := range attemptIDs {
		if cancel, ok := s.cancel[attemptID]; ok {
			cancel()
		}
	}
}

func (s *activeTasks) cancelAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, cancel := range s.cancel {
		cancel()
	}
}

func randomID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate worker process identity: %w", err)
	}
	return hex.EncodeToString(value), nil
}
