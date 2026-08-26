package mid

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

const (
	DefaultPollInterval = 40 * time.Second
	maxPollInterval     = time.Hour
	cycleTimeout        = 2 * time.Minute
)

type Transport interface {
	Poll(context.Context, string, int) ([]Record, error)
	Get(context.Context, string, string) (Record, error)
	Claim(context.Context, Record) (Record, error)
	FindResponses(context.Context, string, string) ([]Record, error)
	InsertResult(context.Context, Record) (string, error)
	MarkProcessed(context.Context, Record) error
}

type RunConfig struct {
	MIDName      string
	Version      string
	PollInterval time.Duration
	BatchSize    int
	Transport    Transport
	State        *State
	Logger       *slog.Logger
}

func Run(ctx context.Context, config RunConfig) error {
	agent, err := AgentName(config.MIDName)
	if err != nil {
		return err
	}
	if config.Transport == nil || config.State == nil {
		return errors.New("MID run requires an ECC transport and locked local state")
	}
	if config.PollInterval == 0 {
		config.PollInterval = DefaultPollInterval
	}
	if config.PollInterval < time.Second || config.PollInterval > maxPollInterval {
		return errors.New("MID poll interval must be between 1s and 1h")
	}
	if config.BatchSize == 0 {
		config.BatchSize = 1
	}
	if config.BatchSize < 1 || config.BatchSize > maxPollRecords {
		return fmt.Errorf("MID batch size must be between 1 and %d", maxPollRecords)
	}
	logger := config.Logger
	if logger == nil {
		logger = slog.Default()
	}

	runCycle(ctx, config, agent, logger)
	ticker := time.NewTicker(config.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			runCycle(ctx, config, agent, logger)
		}
	}
}

func runCycle(ctx context.Context, config RunConfig, agent string, logger *slog.Logger) {
	cycleCtx, cancel := context.WithTimeout(ctx, cycleTimeout)
	defer cancel()
	entry, err := config.State.Load()
	if err != nil {
		logger.Error("load native ECC claim journal", "error", err)
		return
	}
	if entry != nil {
		if err := resumeClaim(cycleCtx, config, agent, logger, *entry); err != nil {
			logger.Warn("resume native ECC claim", "record_id", entry.RecordID, "error", err)
		}
		return
	}
	records, err := config.Transport.Poll(cycleCtx, agent, config.BatchSize)
	if err != nil {
		logger.Warn("poll ServiceNow native ECC queue", "error", err)
		return
	}
	for _, record := range records {
		digest, err := recordDigest(record)
		if err != nil {
			logger.Warn("digest ServiceNow ECC record", "record_id", record.SysID, "error", err)
			return
		}
		entry := journalEntry{Version: journalVersion, RecordID: record.SysID, RecordDigest: digest}
		if err := config.State.Save(entry); err != nil {
			logger.Error("retain native ECC claim before state transition", "record_id", record.SysID, "error", err)
			return
		}
		if err := resumeClaim(cycleCtx, config, agent, logger, entry); err != nil {
			logger.Warn("process native ECC record", "record_id", record.SysID, "error", err)
			return
		}
	}
}

func resumeClaim(ctx context.Context, config RunConfig, agent string, logger *slog.Logger, entry journalEntry) error {
	record, err := config.Transport.Get(ctx, entry.RecordID, agent)
	if err != nil {
		return err
	}
	if record.State == StateProcessed {
		// A crash can occur after the final state update but before the local
		// journal is removed. There is no work left to repeat in that case.
		return config.State.Clear()
	}
	if record.State == StateReady {
		record, err = config.Transport.Claim(ctx, record)
		if err != nil {
			return err
		}
	}
	if record.State != StateProcessing {
		return errors.New("journaled ECC record is in an unexpected state")
	}

	currentDigest, err := recordDigest(record)
	if err != nil {
		return err
	}
	if entry.Result == nil {
		var result Record
		if currentDigest != entry.RecordDigest {
			result, err = dispatchError(record, ClaimConflictCode, "Topo denied an ECC record that changed during claim")
		} else {
			result, err = Dispatch(record, config.Version)
		}
		if err != nil {
			return err
		}
		entry.Result = &result
		if err := config.State.Save(entry); err != nil {
			return fmt.Errorf("retain native ECC result before insertion: %w", err)
		}
	}

	responses, err := config.Transport.FindResponses(ctx, record.SysID, agent)
	if err != nil {
		return err
	}
	if len(responses) > 1 {
		return fmt.Errorf("found %d ECC responses for one output record; refusing to hide duplicate processing", len(responses))
	}
	if len(responses) == 1 {
		entry.ResponseSysID = responses[0].SysID
	} else {
		responseSysID, err := config.Transport.InsertResult(ctx, *entry.Result)
		if err != nil {
			return err
		}
		entry.ResponseSysID = responseSysID
	}
	if err := config.State.Save(entry); err != nil {
		return fmt.Errorf("retain native ECC response identity: %w", err)
	}
	if err := config.Transport.MarkProcessed(ctx, record); err != nil {
		return err
	}
	if err := config.State.Clear(); err != nil {
		return err
	}
	logger.Info("completed ServiceNow native ECC record",
		"record_id", record.SysID,
		"response_id", entry.ResponseSysID,
		"supported", record.Topic == TopicHeartbeat,
	)
	return nil
}
