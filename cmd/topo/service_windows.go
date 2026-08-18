//go:build windows

package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/eventlog"
	"golang.org/x/sys/windows/svc/mgr"

	"log/slog"
	"os"

	"github.com/Nischoy-ai/topo/internal/agent"
)

// isWindowsService reports whether the current process is running under
// the Windows Service Control Manager.
func isWindowsService() (bool, error) {
	return svc.IsWindowsService()
}

// runAgentService runs cfg under the Windows Service Control Manager,
// translating SCM stop/shutdown requests into context cancellation. It
// blocks until the SCM stops the service.
func runAgentService(cfg agent.Config) error {
	logger := cfg.Logger
	if elog, err := eventlog.Open(windowsServiceName); err == nil {
		logger = slog.New(&eventLogHandler{elog: elog})
		defer elog.Close()
	} else if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stderr, nil))
	}
	cfg.Logger = logger
	return svc.Run(windowsServiceName, &agentServiceHandler{cfg: cfg, logger: logger})
}

// installAgentService registers the current executable as an
// automatically-started, restart-on-failure Windows service, passing args
// as the command line the service runs with (for example
// []string{"agent", "run", "-controller-url", "..."}). It also registers an
// Event Log source for the service, best-effort.
func installAgentService(name string, args []string) error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}

	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to service control manager: %w", err)
	}
	defer m.Disconnect()

	if existing, openErr := m.OpenService(name); openErr == nil {
		existing.Close()
		return fmt.Errorf("service %q is already installed; uninstall it first", name)
	}

	service, err := m.CreateService(name, exePath, mgr.Config{
		DisplayName: "Topo Agent",
		Description: "Outbound-only Topo endpoint discovery agent.",
		StartType:   mgr.StartAutomatic,
	}, args...)
	if err != nil {
		return fmt.Errorf("create service: %w", err)
	}
	defer service.Close()

	recovery := []mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 30 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 30 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 30 * time.Second},
	}
	if err := service.SetRecoveryActions(recovery, uint32((24 * time.Hour).Seconds())); err != nil {
		return fmt.Errorf("configure restart-on-failure: %w", err)
	}

	if err := eventlog.InstallAsEventCreate(name, eventlog.Error|eventlog.Warning|eventlog.Info); err != nil && !strings.Contains(err.Error(), "exists") {
		fmt.Fprintf(os.Stderr, "warning: register event log source: %v\n", err)
	}
	return nil
}

// uninstallAgentService stops the service if running, removes it from the
// service control manager, and removes its Event Log source.
func uninstallAgentService(name string) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to service control manager: %w", err)
	}
	defer m.Disconnect()

	service, err := m.OpenService(name)
	if err != nil {
		return fmt.Errorf("open service %q: %w", name, err)
	}
	defer service.Close()

	if status, queryErr := service.Query(); queryErr == nil && status.State != svc.Stopped {
		if _, err := service.Control(svc.Stop); err != nil {
			fmt.Fprintf(os.Stderr, "warning: stop service before removal: %v\n", err)
		}
	}
	if err := service.Delete(); err != nil {
		return fmt.Errorf("delete service: %w", err)
	}
	if err := eventlog.Remove(name); err != nil {
		fmt.Fprintf(os.Stderr, "warning: remove event log source: %v\n", err)
	}
	return nil
}

// agentServiceHandler implements svc.Handler by running the agent core
// loop and translating SCM change requests into context cancellation.
type agentServiceHandler struct {
	cfg    agent.Config
	logger *slog.Logger
}

func (h *agentServiceHandler) Execute(_ []string, r <-chan svc.ChangeRequest, s chan<- svc.Status) (bool, uint32) {
	const accepted = svc.AcceptStop | svc.AcceptShutdown
	s <- svc.Status{State: svc.StartPending}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- agent.Run(ctx, h.cfg) }()

	s <- svc.Status{State: svc.Running, Accepts: accepted}
	for {
		select {
		case err := <-done:
			if err != nil {
				h.logger.Error("agent loop exited unexpectedly", "error", err)
				s <- svc.Status{State: svc.Stopped}
				return false, 1
			}
			s <- svc.Status{State: svc.Stopped}
			return false, 0
		case req := <-r:
			switch req.Cmd {
			case svc.Interrogate:
				s <- req.CurrentStatus
			case svc.Stop, svc.Shutdown:
				s <- svc.Status{State: svc.StopPending}
				cancel()
				<-done
				s <- svc.Status{State: svc.Stopped}
				return false, 0
			}
		}
	}
}

// eventLogHandler is a minimal slog.Handler that writes log records to the
// Windows Event Log, since a service has no attached console.
type eventLogHandler struct {
	elog  *eventlog.Log
	attrs []slog.Attr
}

func (h *eventLogHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *eventLogHandler) Handle(_ context.Context, record slog.Record) error {
	var b strings.Builder
	b.WriteString(record.Message)
	for _, a := range h.attrs {
		fmt.Fprintf(&b, " %s=%v", a.Key, a.Value)
	}
	record.Attrs(func(a slog.Attr) bool {
		fmt.Fprintf(&b, " %s=%v", a.Key, a.Value)
		return true
	})
	const eventID = 1
	switch {
	case record.Level >= slog.LevelError:
		return h.elog.Error(eventID, b.String())
	case record.Level >= slog.LevelWarn:
		return h.elog.Warning(eventID, b.String())
	default:
		return h.elog.Info(eventID, b.String())
	}
}

func (h *eventLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &eventLogHandler{elog: h.elog, attrs: append(append([]slog.Attr{}, h.attrs...), attrs...)}
}

func (h *eventLogHandler) WithGroup(string) slog.Handler { return h }
