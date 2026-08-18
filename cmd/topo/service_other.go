//go:build !windows

package main

import (
	"errors"

	"github.com/Nischoy-ai/topo/internal/agent"
)

var errWindowsServiceOnly = errors.New("Windows service management is only supported on windows; run `topo agent run` directly, for example under systemd on Linux")

func isWindowsService() (bool, error) { return false, nil }

func runAgentService(agent.Config) error { return errWindowsServiceOnly }

func installAgentService(string, []string) error { return errWindowsServiceOnly }

func uninstallAgentService(string) error { return errWindowsServiceOnly }
