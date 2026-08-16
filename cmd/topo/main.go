package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Nischoy-ai/topo/internal/controller"
	"github.com/Nischoy-ai/topo/internal/store"
	"github.com/Nischoy-ai/topo/pkg/discovery"
	localdiscovery "github.com/Nischoy-ai/topo/pkg/discovery/local"
	"github.com/Nischoy-ai/topo/pkg/discovery/sshlinux"
	"github.com/Nischoy-ai/topo/pkg/discovery/winrm"
	"github.com/Nischoy-ai/topo/pkg/lab"
	"github.com/Nischoy-ai/topo/pkg/model"
	"github.com/Nischoy-ai/topo/pkg/publisher/jsonlines"
	"github.com/Nischoy-ai/topo/pkg/publisher/servicenow"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
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
		return errors.New("usage: topo lab <generate|expected|serve|run|ssh-serve|ssh-targets|winrm-serve|winrm-targets>")
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
	case "ssh-serve":
		return labSSHServe(args[1:])
	case "ssh-targets":
		return labSSHTargets(args[1:])
	case "winrm-serve":
		return labWinRMServe(args[1:])
	case "winrm-targets":
		return labWinRMTargets(args[1:])
	default:
		return fmt.Errorf("unknown lab command %q", args[0])
	}
}

func labWinRMServe(args []string) error {
	fs := flag.NewFlagSet("lab winrm-serve", flag.ContinueOnError)
	scenarioPath := fs.String("scenario", "examples/lab/clean-500.json", "scenario JSON path")
	addr := fs.String("addr", "127.0.0.1:5985", "loopback listen address")
	if err := fs.Parse(args); err != nil {
		return err
	}
	host, _, err := net.SplitHostPort(*addr)
	if err != nil {
		return fmt.Errorf("invalid WinRM Lab listen address: %w", err)
	}
	if ip := net.ParseIP(host); !strings.EqualFold(host, "localhost") && (ip == nil || !ip.IsLoopback()) {
		return errors.New("Topo Lab WinRM must listen on loopback")
	}
	scenario, err := loadScenario(*scenarioPath)
	if err != nil {
		return err
	}
	estate, err := lab.Generate(scenario)
	if err != nil {
		return err
	}
	server := &http.Server{
		Addr:              *addr,
		Handler:           lab.NewWinRMServer(estate).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	slog.Info("Topo Lab WinRM listening", "address", *addr, "windows_hosts", countWindowsHosts(estate), "username", lab.LabWinRMUsername)
	err = server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func labWinRMTargets(args []string) error {
	fs := flag.NewFlagSet("lab winrm-targets", flag.ContinueOnError)
	scenarioPath := fs.String("scenario", "examples/lab/clean-500.json", "scenario JSON path")
	addr := fs.String("addr", "127.0.0.1:5985", "WinRM Lab server address")
	if err := fs.Parse(args); err != nil {
		return err
	}
	host, _, err := net.SplitHostPort(*addr)
	if err != nil {
		return fmt.Errorf("invalid WinRM Lab server address: %w", err)
	}
	if ip := net.ParseIP(host); !strings.EqualFold(host, "localhost") && (ip == nil || !ip.IsLoopback()) {
		return errors.New("Topo Lab WinRM server address must be loopback")
	}
	scenario, err := loadScenario(*scenarioPath)
	if err != nil {
		return err
	}
	estate, err := lab.Generate(scenario)
	if err != nil {
		return err
	}
	for _, host := range estate.Hosts {
		if host.OS == "windows" {
			fmt.Println(lab.WinRMTargetURL(*addr, host.ID))
		}
	}
	return nil
}

func labSSHServe(args []string) error {
	fs := flag.NewFlagSet("lab ssh-serve", flag.ContinueOnError)
	scenarioPath := fs.String("scenario", "examples/lab/clean-500.json", "scenario JSON path")
	addr := fs.String("addr", "127.0.0.1:2222", "listen address")
	if err := fs.Parse(args); err != nil {
		return err
	}
	scenario, err := loadScenario(*scenarioPath)
	if err != nil {
		return err
	}
	estate, err := lab.Generate(scenario)
	if err != nil {
		return err
	}
	server, err := lab.NewSSHServer(estate)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		return err
	}
	defer listener.Close()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()
	slog.Info("Topo Lab SSH listening", "address", listener.Addr(), "linux_hosts", countLinuxHosts(estate), "username", "host ID")
	return server.Serve(listener)
}

func labSSHTargets(args []string) error {
	fs := flag.NewFlagSet("lab ssh-targets", flag.ContinueOnError)
	scenarioPath := fs.String("scenario", "examples/lab/clean-500.json", "scenario JSON path")
	addr := fs.String("addr", "127.0.0.1:2222", "SSH server address")
	if err := fs.Parse(args); err != nil {
		return err
	}
	scenario, err := loadScenario(*scenarioPath)
	if err != nil {
		return err
	}
	estate, err := lab.Generate(scenario)
	if err != nil {
		return err
	}
	for _, host := range estate.Hosts {
		if host.OS == "linux" {
			fmt.Printf("%s@%s\n", host.ID, *addr)
		}
	}
	return nil
}

func countLinuxHosts(estate lab.Estate) int {
	count := 0
	for _, host := range estate.Hosts {
		if host.OS == "linux" {
			count++
		}
	}
	return count
}

func countWindowsHosts(estate lab.Estate) int {
	count := 0
	for _, host := range estate.Hosts {
		if host.OS == "windows" {
			count++
		}
	}
	return count
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
	if len(args) > 0 && args[0] == "ssh" {
		return discoverSSH(args[1:])
	}
	if len(args) > 0 && args[0] == "winrm" {
		return discoverWinRM(args[1:])
	}
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

func discoverWinRM(args []string) error {
	fs := flag.NewFlagSet("discover winrm", flag.ContinueOnError)
	targetsPath := fs.String("targets", "", "file containing one WinRM endpoint URL per line")
	username := fs.String("username", env("TOPO_WINRM_USERNAME", ""), "WinRM username (DOMAIN\\user, SERVER\\user, or user@domain)")
	passwordEnv := fs.String("password-env", "TOPO_WINRM_PASSWORD", "environment variable containing the WinRM password")
	authMode := fs.String("auth", "", "production authentication mode (ntlm)")
	labBasic := fs.Bool("lab-basic", false, "enable Basic authentication to loopback Topo Lab endpoints")
	concurrency := fs.Int("concurrency", 32, "maximum concurrent WinRM targets")
	connectTimeout := fs.Duration("connect-timeout", 10*time.Second, "WinRM connection timeout")
	operationTimeout := fs.Duration("operation-timeout", 10*time.Second, "per-operation timeout")
	maxResponseBytes := fs.Int64("max-response-bytes", 4<<20, "maximum response retained per WS-Management request")
	site := fs.String("site", "default", "site ID")
	collector := fs.String("collector", "winrm-relay", "collector ID")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *targetsPath == "" {
		return errors.New("-targets is required")
	}
	if *labBasic && *authMode != "" {
		return errors.New("-lab-basic and -auth cannot be combined")
	}
	if !*labBasic && *authMode != winrm.AuthModeNTLM {
		return errors.New("select -auth ntlm for production HTTPS targets or -lab-basic for loopback Topo Lab")
	}
	targets, err := readTargets(*targetsPath)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		return errors.New("targets file contains no targets")
	}
	selectedUsername := *username
	if *labBasic && selectedUsername == "" {
		selectedUsername = lab.LabWinRMUsername
	}
	if selectedUsername == "" {
		return errors.New("WinRM username is required; set -username or TOPO_WINRM_USERNAME")
	}
	password := os.Getenv(*passwordEnv)
	if password == "" {
		return fmt.Errorf("no WinRM credential: set %s", *passwordEnv)
	}
	plugin := winrm.Plugin{Config: winrm.Config{
		Username:         selectedUsername,
		Password:         password,
		AuthMode:         *authMode,
		LabMode:          *labBasic,
		Concurrency:      *concurrency,
		ConnectTimeout:   *connectTimeout,
		OperationTimeout: *operationTimeout,
		MaxResponseBytes: *maxResponseBytes,
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	observation, err := plugin.Discover(ctx, discovery.Request{SiteID: *site, CollectorID: *collector, Targets: targets})
	if err != nil {
		return err
	}
	_, err = jsonlines.Publisher{Writer: os.Stdout}.PublishBatch(ctx, []model.ObservationEnvelope{observation})
	return err
}

func discoverSSH(args []string) error {
	fs := flag.NewFlagSet("discover ssh", flag.ContinueOnError)
	targetsPath := fs.String("targets", "", "file containing one username@host:port target per line")
	passwordEnv := fs.String("password-env", "TOPO_SSH_PASSWORD", "environment variable containing the SSH password")
	privateKeyPath := fs.String("private-key", "", "PEM private key path")
	knownHostsPath := fs.String("known-hosts", "", "known_hosts path")
	insecureHostKey := fs.Bool("insecure-host-key", false, "skip host key verification (Topo Lab only)")
	concurrency := fs.Int("concurrency", 32, "maximum concurrent SSH connections")
	connectTimeout := fs.Duration("connect-timeout", 10*time.Second, "SSH connection timeout")
	commandTimeout := fs.Duration("command-timeout", 10*time.Second, "per-command timeout")
	maxOutputBytes := fs.Int64("max-output-bytes", 4<<20, "maximum output retained per SSH command")
	site := fs.String("site", "default", "site ID")
	collector := fs.String("collector", "ssh-relay", "collector ID")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *targetsPath == "" {
		return errors.New("-targets is required")
	}
	targets, err := readTargets(*targetsPath)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		return errors.New("targets file contains no targets")
	}

	var callback ssh.HostKeyCallback
	switch {
	case *insecureHostKey:
		slog.Warn("SSH host key verification disabled; use only with Topo Lab")
		callback = ssh.InsecureIgnoreHostKey() // #nosec G106 -- explicit lab-only CLI option.
	case *knownHostsPath != "":
		callback, err = knownhosts.New(*knownHostsPath)
		if err != nil {
			return fmt.Errorf("load known_hosts: %w", err)
		}
	default:
		return errors.New("-known-hosts is required unless -insecure-host-key is explicitly set for Topo Lab")
	}

	var signer ssh.Signer
	if *privateKeyPath != "" {
		key, err := os.ReadFile(*privateKeyPath)
		if err != nil {
			return fmt.Errorf("read private key: %w", err)
		}
		signer, err = ssh.ParsePrivateKey(key)
		if err != nil {
			return fmt.Errorf("parse private key: %w", err)
		}
	}
	password := os.Getenv(*passwordEnv)
	if password == "" && signer == nil {
		return fmt.Errorf("no SSH credential: set %s or provide -private-key", *passwordEnv)
	}

	plugin := sshlinux.Plugin{Config: sshlinux.Config{Password: password, Signer: signer, HostKeyCallback: callback, Concurrency: *concurrency, ConnectTimeout: *connectTimeout, CommandTimeout: *commandTimeout, MaxOutputBytes: *maxOutputBytes}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	observation, err := plugin.Discover(ctx, discovery.Request{SiteID: *site, CollectorID: *collector, Targets: targets})
	if err != nil {
		return err
	}
	_, err = jsonlines.Publisher{Writer: os.Stdout}.PublishBatch(ctx, []model.ObservationEnvelope{observation})
	return err
}

func readTargets(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var targets []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			targets = append(targets, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return targets, nil
}
func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
