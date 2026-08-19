package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/Nischoy-ai/topo/pkg/discovery"
	"github.com/Nischoy-ai/topo/pkg/model"
)

// discoverTimeout bounds one local discovery pass.
const discoverTimeout = 30 * time.Second

// Config configures one Run invocation.
type Config struct {
	SiteID      string
	CollectorID string
	Interval    time.Duration
	Plugin      discovery.Plugin
	Sender      *Sender
	Spool       *Spool
	Logger      *slog.Logger

	// HeartbeatInterval, when positive, sends a liveness heartbeat on this
	// separate cadence in addition to the discovery/delivery Interval —
	// distinct from observation delivery, so the controller can tell the
	// collector is alive even between long discovery scans, rather than
	// only inferring liveness from Interval-paced observation delivery.
	// Zero or negative disables heartbeats entirely.
	HeartbeatInterval time.Duration
}

// Run discovers and delivers on Interval until ctx is cancelled, returning
// nil on a clean shutdown. Each tick first retries anything already
// spooled, oldest first, then performs a fresh discovery pass. If
// HeartbeatInterval is positive, a liveness heartbeat is also sent on that
// independent cadence.
func Run(ctx context.Context, cfg Config) error {
	if cfg.Interval <= 0 {
		return errors.New("agent interval must be positive")
	}
	if cfg.Plugin == nil || cfg.Sender == nil || cfg.Spool == nil {
		return errors.New("agent config requires a discovery plugin, sender, and spool")
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	tick := func() {
		drainSpool(ctx, cfg, logger)
		discoverAndSend(ctx, cfg, logger)
	}

	tick()
	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()

	// A nil heartbeatC blocks forever in the select below, which is
	// exactly how HeartbeatInterval <= 0 disables heartbeats — and job
	// polling, which rides the same cadence — without a separate branch.
	var heartbeatC <-chan time.Time
	if cfg.HeartbeatInterval > 0 {
		checkIn(ctx, cfg, logger)
		heartbeatTicker := time.NewTicker(cfg.HeartbeatInterval)
		defer heartbeatTicker.Stop()
		heartbeatC = heartbeatTicker.C
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			tick()
		case <-heartbeatC:
			checkIn(ctx, cfg, logger)
		}
	}
}

// checkIn sends a liveness heartbeat and polls for, then runs, any jobs
// the controller has queued — the two ride the same HeartbeatInterval
// cadence rather than each needing its own ticker and flag, since both
// are cheap, frequent, best-effort check-ins distinct from the heavier
// discovery/delivery Interval.
func checkIn(ctx context.Context, cfg Config, logger *slog.Logger) {
	sendHeartbeat(ctx, cfg, logger)
	pollAndRunJobs(ctx, cfg, logger)
}

// sendHeartbeat is best-effort: a failed heartbeat is logged, not
// retried or spooled, since a stale heartbeat has no lasting value once
// the next one supersedes it.
func sendHeartbeat(ctx context.Context, cfg Config, logger *slog.Logger) {
	sendCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	err := cfg.Sender.SendHeartbeat(sendCtx, cfg.CollectorID, cfg.SiteID)
	cancel()
	if err != nil {
		logger.Warn("heartbeat delivery failed", "error", err)
	}
}

// pollAndRunJobs fetches any jobs dispatched to this collector and runs
// them one at a time. A poll failure is logged and simply tried again on
// the next check-in — an undelivered job stays queued at the controller,
// unlike a heartbeat, so there is nothing to lose by waiting.
func pollAndRunJobs(ctx context.Context, cfg Config, logger *slog.Logger) {
	pollCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	jobs, err := cfg.Sender.PollJobs(pollCtx, cfg.CollectorID)
	cancel()
	if err != nil {
		logger.Warn("poll for jobs failed", "error", err)
		return
	}
	for _, job := range jobs {
		runJob(ctx, cfg, logger, job)
	}
}

// runJob executes one dispatched job and reports the outcome back to the
// controller. "discover" is the only job type today, since it is the
// only capability topo agent run actually has; any other type is
// reported as a failure rather than silently ignored, so an operator can
// tell the collector rejected it instead of assuming it is still queued.
func runJob(ctx context.Context, cfg Config, logger *slog.Logger, job model.Job) {
	logger.Info("running dispatched job", "job_id", job.JobID, "type", job.Type)
	result := model.JobResult{CompletedAt: time.Now().UTC()}
	switch job.Type {
	case model.JobTypeDiscover:
		if err := discoverAndSend(ctx, cfg, logger); err != nil {
			result.Error = err.Error()
		} else {
			result.Success = true
		}
	default:
		result.Error = fmt.Sprintf("collector does not support job type %q", job.Type)
	}

	reportCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	err := cfg.Sender.ReportJobResult(reportCtx, cfg.CollectorID, job.JobID, result)
	cancel()
	if err != nil {
		logger.Warn("report job result failed", "job_id", job.JobID, "error", err)
	}
}

// drainSpool retries queued observations oldest first, stopping at the
// first retryable failure so entries stay in order for the next tick.
// Entries that are unreadable or that the controller permanently rejects
// are dropped rather than retried forever.
func drainSpool(ctx context.Context, cfg Config, logger *slog.Logger) {
	names, err := cfg.Spool.Pending()
	if err != nil {
		logger.Error("list spooled observations", "error", err)
		return
	}
	for _, name := range names {
		if ctx.Err() != nil {
			return
		}
		envelope, err := cfg.Spool.Read(name)
		if err != nil {
			logger.Error("spooled observation unreadable, dropping", "entry", name, "error", err)
			if removeErr := cfg.Spool.Remove(name); removeErr != nil {
				logger.Error("remove unreadable spool entry", "entry", name, "error", removeErr)
			}
			continue
		}

		sendCtx, cancel := context.WithTimeout(ctx, requestTimeout)
		sendErr := cfg.Sender.Send(sendCtx, envelope)
		cancel()
		if sendErr == nil {
			if removeErr := cfg.Spool.Remove(name); removeErr != nil {
				logger.Error("remove delivered spool entry", "entry", name, "error", removeErr)
			}
			continue
		}

		var delivery *DeliveryError
		if errors.As(sendErr, &delivery) && !delivery.Retryable {
			logger.Error("controller rejected spooled observation, dropping", "entry", name, "error", sendErr)
			if removeErr := cfg.Spool.Remove(name); removeErr != nil {
				logger.Error("remove rejected spool entry", "entry", name, "error", removeErr)
			}
			continue
		}
		logger.Warn("controller unreachable, will retry spooled observations later", "error", sendErr)
		return
	}
}

// discoverAndSend runs one local discovery pass and either delivers it
// immediately or buffers it for later delivery. The returned error
// reflects only whether discovery itself succeeded — a job's "discover"
// result is meant to answer "did the collector manage to collect
// anything", not "did that data reach the controller synchronously",
// since delivery already has its own robust, independent retry-via-spool
// path regardless of how discovery was triggered.
func discoverAndSend(ctx context.Context, cfg Config, logger *slog.Logger) error {
	discoverCtx, cancel := context.WithTimeout(ctx, discoverTimeout)
	envelope, err := cfg.Plugin.Discover(discoverCtx, discovery.Request{SiteID: cfg.SiteID, CollectorID: cfg.CollectorID, Targets: []string{"local"}})
	cancel()
	if err != nil {
		logger.Error("local discovery failed", "error", err)
		return err
	}
	deliver(ctx, cfg, logger, envelope)
	return nil
}

func deliver(ctx context.Context, cfg Config, logger *slog.Logger, envelope model.ObservationEnvelope) {
	sendCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	err := cfg.Sender.Send(sendCtx, envelope)
	cancel()
	if err == nil {
		return
	}
	if spoolErr := cfg.Spool.Enqueue(envelope); spoolErr != nil {
		logger.Error("observation dropped: delivery failed and it did not fit in the spool", "send_error", err, "spool_error", spoolErr)
		return
	}
	logger.Warn("controller unreachable, observation buffered", "error", err)
}
