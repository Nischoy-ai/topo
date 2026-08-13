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
	"github.com/Nischoy-ai/topo/pkg/lab"
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
	case "lab":
		return runLab(args[1:])
	case "version":
		fmt.Println(version)
		return nil
	default:
		return usage()
	}
}
func usage() error {
	fmt.Fprintln(os.Stderr, "usage: topo <serve|discover|lab|version>")
	return errors.New("command required")
}

func runLab(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: topo lab <generate|expected|serve|run>")
	}
	switch args[0] {
	case "generate":
		return labGenerate(args[1:])
	case "expected":
		return labExpected(args[1:])
	case "serve":
		return labServe(args[1:])
	case "run":
		return labRun(args[1:])
	default:
		return fmt.Errorf("unknown lab command %q", args[0])
	}
}

func labGenerate(args []string) error {
	fs := flag.NewFlagSet("lab generate", flag.ContinueOnError)
	hosts := fs.Int("hosts", 500, "number of simulated hosts")
	windows := fs.Int("windows-percent", 30, "percentage of Windows hosts")
	seed := fs.Int64("seed", 42, "deterministic seed")
	out := fs.String("out", "-", "output scenario path or - for stdout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	s := lab.DefaultScenario(*hosts, *windows, *seed)
	var w *os.File
	if *out == "-" {
		w = os.Stdout
	} else {
		f, err := os.Create(*out)
		if err != nil {
			return err
		}
		defer f.Close()
		w = f
	}
	return lab.EncodeScenario(w, s)
}

func loadScenario(path string) (lab.Scenario, error) {
	f, err := os.Open(path)
	if err != nil {
		return lab.Scenario{}, err
	}
	defer f.Close()
	return lab.DecodeScenario(f)
}

func labExpected(args []string) error {
	fs := flag.NewFlagSet("lab expected", flag.ContinueOnError)
	scenarioPath := fs.String("scenario", "examples/lab/estate.json", "scenario JSON path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	s, err := loadScenario(*scenarioPath)
	if err != nil {
		return err
	}
	estate, err := lab.Generate(s)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(estate.Expected)
}

func labServe(args []string) error {
	fs := flag.NewFlagSet("lab serve", flag.ContinueOnError)
	scenarioPath := fs.String("scenario", "examples/lab/estate.json", "scenario JSON path")
	addr := fs.String("addr", "127.0.0.1:9090", "listen address")
	if err := fs.Parse(args); err != nil {
		return err
	}
	s, err := loadScenario(*scenarioPath)
	if err != nil {
		return err
	}
	estate, err := lab.Generate(s)
	if err != nil {
		return err
	}
	srv := &http.Server{Addr: *addr, Handler: lab.NewServer(estate).Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 30 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	slog.Info("Topo Lab listening", "address", *addr, "hosts", len(estate.Hosts), "seed", s.Seed)
	err = srv.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func labRun(args []string) error {
	fs := flag.NewFlagSet("lab run", flag.ContinueOnError)
	scenarioPath := fs.String("scenario", "examples/lab/estate.json", "scenario JSON path")
	baseURL := fs.String("url", "http://127.0.0.1:9090", "Topo Lab server URL")
	concurrency := fs.Int("concurrency", 64, "maximum concurrent requests")
	repeat := fs.Int("repeat", 2, "number of scans")
	timeout := fs.Duration("request-timeout", 2*time.Second, "per-host timeout")
	minCoverage := fs.Float64("min-coverage", 0, "fail when asset coverage falls below this percentage")
	if err := fs.Parse(args); err != nil {
		return err
	}
	s, err := loadScenario(*scenarioPath)
	if err != nil {
		return err
	}
	estate, err := lab.Generate(s)
	if err != nil {
		return err
	}
	if *repeat < 1 {
		return errors.New("repeat must be positive")
	}
	if *minCoverage < 0 || *minCoverage > 100 {
		return errors.New("min-coverage must be between 0 and 100")
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	for i := 1; i <= *repeat; i++ {
		result, err := lab.Discover(context.Background(), lab.RunOptions{BaseURL: *baseURL, Concurrency: *concurrency, RequestTimeout: *timeout})
		if err != nil {
			return err
		}
		evaluation := lab.Evaluate(estate.Expected, result.Observation)
		if err := enc.Encode(map[string]any{"scan": i, "attempted": result.Attempted, "discovered": result.Discovered, "partial": result.Partial, "failed": result.Failed, "duration_ms": result.Duration.Milliseconds(), "evaluation": evaluation}); err != nil {
			return err
		}
		if evaluation.CoveragePercent < *minCoverage {
			return fmt.Errorf("scan %d coverage %.2f%% is below required %.2f%%", i, evaluation.CoveragePercent, *minCoverage)
		}
	}
	return nil
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
