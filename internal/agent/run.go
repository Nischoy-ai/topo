package agent

import (
	"context"
	"errors"
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
}

// Run discovers and delivers on Interval until ctx is cancelled, returning
// nil on a clean shutdown. Each tick first retries anything already
// spooled, oldest first, then performs a fresh discovery pass.
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
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			tick()
		}
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
// immediately or buffers it for later delivery.
func discoverAndSend(ctx context.Context, cfg Config, logger *slog.Logger) {
	discoverCtx, cancel := context.WithTimeout(ctx, discoverTimeout)
	envelope, err := cfg.Plugin.Discover(discoverCtx, discovery.Request{SiteID: cfg.SiteID, CollectorID: cfg.CollectorID, Targets: []string{"local"}})
	cancel()
	if err != nil {
		logger.Error("local discovery failed", "error", err)
		return
	}
	deliver(ctx, cfg, logger, envelope)
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
