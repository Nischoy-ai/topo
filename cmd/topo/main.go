package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Nischoy-ai/topo/internal/controller"
	"github.com/Nischoy-ai/topo/internal/store"
	"github.com/Nischoy-ai/topo/pkg/discovery"
	localdiscovery "github.com/Nischoy-ai/topo/pkg/discovery/local"
	"github.com/Nischoy-ai/topo/pkg/model"
	"github.com/Nischoy-ai/topo/pkg/publisher/jsonlines"
	"github.com/Nischoy-ai/topo/pkg/publisher/servicenow"
)

var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		slog.Error("topo failed", "error", err)
		os.Exit(1)
	}
}
func run(args []string) error {
	if len(args) == 0 {
		return usage()
	}
	switch args[0] {
	case "serve":
		return serve(args[1:])
	case "discover":
		return discover(args[1:])
	case "version":
		fmt.Println(version)
		return nil
	default:
		return usage()
	}
}
func usage() error {
	fmt.Fprintln(os.Stderr, "usage: topo <serve|discover|version>")
	return errors.New("command required")
}
func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	addr := fs.String("addr", env("TOPO_ADDR", ":8080"), "listen address")
	if err := fs.Parse(args); err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	srv := &http.Server{Addr: *addr, Handler: controller.New(store.NewMemory(), logger, os.Getenv("TOPO_API_KEY")).Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	logger.Info("controller listening", "address", *addr, "auth_enabled", os.Getenv("TOPO_API_KEY") != "")
	err := srv.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
func discover(args []string) error {
	fs := flag.NewFlagSet("discover", flag.ContinueOnError)
	site := fs.String("site", "default", "site ID")
	collector := fs.String("collector", "local", "collector ID")
	format := fs.String("format", "json", "json or servicenow-preview")
	instance := fs.String("servicenow-instance", os.Getenv("SERVICENOW_INSTANCE_URL"), "ServiceNow HTTPS URL")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 || fs.Arg(0) != "local" {
		return errors.New("currently supported target: local")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	e, err := (localdiscovery.Plugin{}).Discover(ctx, discovery.Request{SiteID: *site, CollectorID: *collector, Targets: []string{"local"}})
	if err != nil {
		return err
	}
	switch *format {
	case "json":
		_, err = jsonlines.Publisher{Writer: os.Stdout}.PublishBatch(ctx, []model.ObservationEnvelope{e})
		return err
	case "servicenow-preview":
		if *instance == "" {
			*instance = "https://example.service-now.com"
		}
		p := servicenow.Publisher{Config: servicenow.Config{InstanceURL: *instance, DiscoverySource: "Nischoy Topo", DryRun: true}}
		v, err := p.Preview(ctx, []model.ObservationEnvelope{e})
		if err != nil {
			return err
		}
		b, _ := json.MarshalIndent(v, "", "  ")
		fmt.Println(string(b))
		return nil
	default:
		return fmt.Errorf("unsupported format %q", *format)
	}
}
func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
