package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Nischoy-ai/topo/internal/workersetup"
)

func workerSetup(args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	return workersetup.Run(ctx, os.Stdin, os.Stdout, args)
}
