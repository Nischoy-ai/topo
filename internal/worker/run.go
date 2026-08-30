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
	"time"
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
	SubmitResult(context.Context, string, ResultChunkRequest) (ResultChunkResponse, error)
	Complete(context.Context, string, CompleteRequest) (CompleteResponse, error)
}

type RunConfig struct {
	Policy       Policy
	Version      string
	PollInterval time.Duration
	Control      ControlPlane
	Executor     Executor
	Logger       *slog.Logger
	Now          func() time.Time
	BootID       string
}

type registration struct {
	workerID string
	bootID   string
}

func Run(ctx context.Context, config RunConfig) error {
	if err := config.Policy.Validate(); err != nil {
		return err
	}
	if config.Control == nil {
		return errors.New("worker control plane is required")
	}
	if config.PollInterval == 0 {
		config.PollInterval = DefaultPollInterval
	}
	if config.PollInterval < time.Second || config.PollInterval > MaxPollInterval {
		return errors.New("worker poll interval must be between 1s and 1h")
	}
	if config.Executor.Policy.WorkerPool == "" {
		config.Executor.Policy = config.Policy
	}
	logger := config.Logger
	if logger == nil {
		logger = slog.Default()
	}
	reg, err := register(ctx, config)
	if err != nil {
		return err
	}
	runCycle(ctx, config, reg, logger)
	ticker := time.NewTicker(config.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			runCycle(ctx, config, reg, logger)
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
		SchemaVersion: ContractVersion,
		BootID:        bootID,
		WorkerPool:    config.Policy.WorkerPool,
		SiteID:        config.Policy.SiteID,
		Version:       truncate(config.Version, 64),
		Capabilities:  config.Policy.Capabilities(),
		PolicyDigest:  digest,
		StartedAt:     now().UTC(),
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

func runCycle(ctx context.Context, config RunConfig, reg registration, logger *slog.Logger) {
	now := time.Now
	if config.Now != nil {
		now = config.Now
	}
	heartbeatCtx, cancel := context.WithTimeout(ctx, controlRequestTimeout)
	_, heartbeatErr := config.Control.Heartbeat(heartbeatCtx, HeartbeatRequest{
		SchemaVersion: ContractVersion,
		WorkerID:      reg.workerID,
		BootID:        reg.bootID,
		SentAt:        now().UTC(),
	})
	cancel()
	if heartbeatErr != nil {
		logger.Warn("ServiceNow worker heartbeat failed", "error", heartbeatErr)
		return
	}
	claimCtx, cancel := context.WithTimeout(ctx, controlRequestTimeout)
	claim, err := config.Control.Claim(claimCtx, ClaimRequest{
		SchemaVersion: ContractVersion,
		WorkerID:      reg.workerID,
		BootID:        reg.bootID,
		Capabilities:  config.Policy.Capabilities(),
	})
	cancel()
	if err != nil {
		logger.Warn("ServiceNow worker claim failed", "error", err)
		return
	}
	if claim.Task == nil {
		return
	}
	executeTask(ctx, config, reg, logger, *claim.Task)
}

func executeTask(ctx context.Context, config RunConfig, reg registration, logger *slog.Logger, task Task) {
	logger.Info("executing ServiceNow task", "task_id", task.TaskID, "run_id", task.RunID, "attempt_id", task.AttemptID, "operation", task.Operation)
	observation, err := config.Executor.Execute(ctx, task)
	if err != nil {
		reportFailure(ctx, config.Control, reg, task, "operation_failed", err, logger)
		return
	}
	payload, err := json.Marshal(observation)
	if err != nil {
		reportFailure(ctx, config.Control, reg, task, "observation_encode_failed", err, logger)
		return
	}
	if len(payload) > maxControlRequestBytes/2 {
		reportFailure(ctx, config.Control, reg, task, "observation_too_large", fmt.Errorf("observation exceeds %d bytes", maxControlRequestBytes/2), logger)
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
	resultCtx, cancel := context.WithTimeout(ctx, controlRequestTimeout)
	ack, err := config.Control.SubmitResult(resultCtx, task.TaskID, chunk)
	cancel()
	if err != nil {
		logger.Warn("ServiceNow result chunk was not acknowledged; task will recover by lease expiry", "task_id", task.TaskID, "attempt_id", task.AttemptID, "error", err)
		return
	}
	if !ack.Accepted {
		logger.Warn("ServiceNow rejected result chunk; task will recover by lease expiry", "task_id", task.TaskID, "attempt_id", task.AttemptID)
		return
	}
	completeCtx, cancel := context.WithTimeout(ctx, controlRequestTimeout)
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
	if err != nil {
		logger.Warn("ServiceNow task completion was not acknowledged; no local state was retained", "task_id", task.TaskID, "attempt_id", task.AttemptID, "error", err)
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

func randomID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate worker process identity: %w", err)
	}
	return hex.EncodeToString(value), nil
}
