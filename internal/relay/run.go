package relay

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/Nischoy-ai/topo/pkg/model"
	"github.com/Nischoy-ai/topo/pkg/publisher"
)

const (
	defaultPollInterval = time.Minute
	maxPollInterval     = time.Hour
	maxPublishAttempts  = 3
	maxResultErrorBytes = 4096
)

type ControlPlane interface {
	Poll(context.Context, PollRequest) ([]Job, error)
	Report(context.Context, JobResult) error
}

type ObservationPublisher interface {
	PublishBatch(context.Context, []model.ObservationEnvelope) (publisher.Result, error)
}

type RunConfig struct {
	FileConfig   FileConfig
	Version      string
	PollInterval time.Duration
	Control      ControlPlane
	Executor     Executor
	Publisher    ObservationPublisher
	Spool        *Spool
	Logger       *slog.Logger
}

func Run(ctx context.Context, config RunConfig) error {
	if err := config.FileConfig.Validate(); err != nil {
		return err
	}
	if config.Control == nil || config.Publisher == nil || config.Spool == nil {
		return errors.New("relay requires a control plane, publisher, and spool")
	}
	if config.PollInterval == 0 {
		config.PollInterval = defaultPollInterval
	}
	if config.PollInterval < time.Second || config.PollInterval > maxPollInterval {
		return errors.New("relay poll interval must be between 1s and 1h")
	}
	logger := config.Logger
	if logger == nil {
		logger = slog.Default()
	}

	cycle(ctx, config, logger)
	ticker := time.NewTicker(config.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			cycle(ctx, config, logger)
		}
	}
}

func cycle(ctx context.Context, config RunConfig, logger *slog.Logger) {
	if drained := drain(ctx, config, logger); !drained {
		return
	}
	request := PollRequest{
		SchemaVersion: ContractVersion,
		RelayID:       config.FileConfig.RelayID,
		SiteID:        config.FileConfig.SiteID,
		Version:       config.Version,
		Profiles:      config.FileConfig.Capabilities(),
		SentAt:        time.Now().UTC(),
	}
	pollCtx, cancel := context.WithTimeout(ctx, controlRequestTimeout)
	jobs, err := config.Control.Poll(pollCtx, request)
	cancel()
	if err != nil {
		logger.Warn("ServiceNow relay poll failed", "error", err)
		return
	}
	for _, job := range jobs {
		runJob(ctx, config, logger, job)
	}
}

func runJob(ctx context.Context, config RunConfig, logger *slog.Logger, job Job) {
	started := time.Now().UTC()
	logger.Info("running ServiceNow discovery job", "job_id", job.JobID, "profile_id", job.ProfileID)
	var observation model.ObservationEnvelope
	discoverErr := validateJob(job)
	if discoverErr == nil {
		observation, discoverErr = config.Executor.Execute(ctx, job)
	}
	delivery := PendingDelivery{Job: job, RelayID: config.FileConfig.RelayID, StartedAt: started, CompletedAt: time.Now().UTC()}
	if discoverErr != nil {
		delivery.DiscoveryError = truncate(discoverErr.Error(), maxResultErrorBytes)
	} else {
		delivery.Observation = &observation
	}
	if err := config.Spool.Enqueue(delivery); err != nil {
		logger.Error("ServiceNow job result could not be retained", "job_id", job.JobID, "error", err)
		result := resultFor(delivery, fmt.Errorf("retain job result in encrypted spool: %w", err))
		reportCtx, cancel := context.WithTimeout(ctx, controlRequestTimeout)
		if reportErr := config.Control.Report(reportCtx, result); reportErr != nil {
			logger.Error("ServiceNow job failure could not be reported", "job_id", job.JobID, "error", reportErr)
		}
		cancel()
		return
	}
	drain(ctx, config, logger)
}

// drain returns true only when no pending delivery remains. The Relay does
// not claim another job while an earlier result is still awaiting IRE or
// control-plane acknowledgement.
func drain(ctx context.Context, config RunConfig, logger *slog.Logger) bool {
	names, err := config.Spool.Pending()
	if err != nil {
		logger.Error("list ServiceNow relay spool", "error", err)
		return false
	}
	for _, name := range names {
		delivery, err := config.Spool.Read(name)
		if err != nil {
			logger.Error("ServiceNow relay spool entry is unreadable", "entry", name, "error", err)
			return false
		}
		if delivery.Observation != nil && !delivery.Published && delivery.PublicationError == "" {
			publishCtx, cancel := context.WithTimeout(ctx, controlRequestTimeout)
			_, publishErr := config.Publisher.PublishBatch(publishCtx, []model.ObservationEnvelope{*delivery.Observation})
			cancel()
			delivery.PublishAttempts++
			if publishErr == nil {
				delivery.Published = true
			} else if !retryable(publishErr) || delivery.PublishAttempts >= maxPublishAttempts {
				delivery.PublicationError = truncate(publishErr.Error(), maxResultErrorBytes)
			}
			if err := config.Spool.Replace(name, delivery); err != nil {
				logger.Error("persist ServiceNow publication state", "entry", name, "error", err)
				return false
			}
			if publishErr != nil && delivery.PublicationError == "" {
				logger.Warn("ServiceNow IRE publication failed; retained for retry", "job_id", delivery.Job.JobID, "attempt", delivery.PublishAttempts, "error", publishErr)
				return false
			}
		}
		result := resultFor(delivery, nil)
		reportCtx, cancel := context.WithTimeout(ctx, controlRequestTimeout)
		err = config.Control.Report(reportCtx, result)
		cancel()
		if err != nil {
			logger.Warn("ServiceNow job result report failed; retained for retry", "job_id", delivery.Job.JobID, "error", err)
			return false
		}
		if err := config.Spool.Remove(name); err != nil {
			logger.Error("remove acknowledged ServiceNow relay spool entry", "entry", name, "error", err)
			return false
		}
	}
	return true
}

func retryable(err error) bool {
	var classified interface{ Retryable() bool }
	return errors.As(err, &classified) && classified.Retryable()
}

func resultFor(delivery PendingDelivery, override error) JobResult {
	result := JobResult{
		SchemaVersion: ContractVersion,
		JobID:         delivery.Job.JobID,
		RelayID:       delivery.RelayID,
		ProfileID:     delivery.Job.ProfileID,
		StartedAt:     delivery.StartedAt,
		CompletedAt:   delivery.CompletedAt,
	}
	if delivery.Observation != nil {
		result.ObservationID = delivery.Observation.ObservationID
		result.Assets = len(delivery.Observation.Assets)
		result.Relationships = len(delivery.Observation.Relationships)
		result.CollectionErrors = len(delivery.Observation.Errors)
	}
	switch {
	case override != nil:
		result.Error = truncate(override.Error(), maxResultErrorBytes)
	case delivery.DiscoveryError != "":
		result.Error = delivery.DiscoveryError
	case delivery.PublicationError != "":
		result.Error = delivery.PublicationError
	case delivery.Published:
		result.Success = true
	default:
		result.Error = "observation was not published"
	}
	return result
}
