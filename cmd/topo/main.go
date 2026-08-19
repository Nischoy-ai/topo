package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Nischoy-ai/topo/internal/agent"
	"github.com/Nischoy-ai/topo/internal/controller"
	"github.com/Nischoy-ai/topo/internal/enrollment"
	"github.com/Nischoy-ai/topo/internal/store"
	"github.com/Nischoy-ai/topo/pkg/credentialref"
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

// windowsServiceName identifies the Topo Agent Windows service. Topo
// supports one agent instance per host, matching the fixed systemd unit
// name used on Linux.
const windowsServiceName = "TopoAgent"

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
	case "agent":
		return runAgent(args[1:])
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
	fmt.Fprintln(os.Stderr, "usage: topo <serve|discover|agent|lab|version>")
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
	apiKeyRef := fs.String("api-key-ref", "", "credential reference for the controller API key (env: or file:)")
	caDir := fs.String("ca-dir", "", "absolute path to persist the collector enrollment CA; enables POST /v1/enrollment-tokens and POST /v1/enroll when set")
	mtls := fs.Bool("mtls", false, "serve natively over mutual TLS, requiring a client certificate verified against -ca-dir; requires -ca-dir")
	mtlsSAN := fs.String("mtls-san", "localhost,127.0.0.1,::1", "comma-separated DNS names/IPs for the controller's own TLS server certificate")
	if err := fs.Parse(args); err != nil {
		return err
	}
	apiKey, err := resolveCredential(*apiKeyRef, "", "TOPO_API_KEY", true)
	if err != nil {
		return fmt.Errorf("resolve controller API key: %w", err)
	}
	if len(apiKey) > 4096 {
		return errors.New("controller API key exceeds 4096 bytes")
	}
	if *mtls && *caDir == "" {
		return errors.New("-mtls requires -ca-dir")
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	controllerServer := controller.New(store.NewMemory(), logger, string(apiKey))
	var ca *enrollment.CA
	if *caDir != "" {
		var caErr error
		ca, caErr = enrollment.LoadOrCreateCA(*caDir)
		if caErr != nil {
			return fmt.Errorf("load or create enrollment CA: %w", caErr)
		}
		controllerServer.CA = ca
		controllerServer.Tokens = enrollment.NewTokenStore()
	}
	srv := &http.Server{Addr: *addr, Handler: controllerServer.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	if *mtls {
		sans := strings.Split(*mtlsSAN, ",")
		for i := range sans {
			sans[i] = strings.TrimSpace(sans[i])
		}
		certPEM, keyPEM, certErr := ca.IssueServerCertificate(sans, enrollment.DefaultServerCertificateTTL)
		if certErr != nil {
			return fmt.Errorf("issue controller server certificate: %w", certErr)
		}
		serverCert, tlsErr := tls.X509KeyPair(certPEM, keyPEM)
		if tlsErr != nil {
			return fmt.Errorf("load controller server certificate: %w", tlsErr)
		}
		clientCAs := x509.NewCertPool()
		if !clientCAs.AppendCertsFromPEM(ca.CACertPEM()) {
			return errors.New("failed to load enrollment CA certificate for client verification")
		}
		// VerifyClientCertIfGiven, not RequireAndVerifyClientCert: the TLS
		// layer must accept connections with no client certificate at all,
		// because a collector's first-ever request (POST /v1/enroll) has
		// none to present yet — it authenticates with a single-use
		// enrollment token instead. The auth() middleware enforces the
		// per-endpoint requirement (valid peer certificate or bearer API
		// key) once the request reaches the application layer; a
		// certificate that IS presented is still verified against
		// ClientCAs during the handshake.
		srv.TLSConfig = &tls.Config{
			Certificates: []tls.Certificate{serverCert},
			ClientCAs:    clientCAs,
			ClientAuth:   tls.VerifyClientCertIfGiven,
			MinVersion:   tls.VersionTLS12,
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	logger.Info("controller listening", "address", *addr, "auth_enabled", len(apiKey) != 0, "enrollment_enabled", controllerServer.CA != nil, "mtls_enabled", *mtls)
	if *mtls {
		err = srv.ListenAndServeTLS("", "")
	} else {
		err = srv.ListenAndServe()
	}
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func runAgent(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: topo agent <run|install|uninstall|enroll>")
	}
	switch args[0] {
	case "run":
		return agentRun(args[1:])
	case "install":
		return agentInstall(args[1:])
	case "uninstall":
		return agentUninstall(args[1:])
	case "enroll":
		return agentEnroll(args[1:])
	default:
		return errors.New("usage: topo agent <run|install|uninstall|enroll>")
	}
}

func agentRun(args []string) error {
	fs := flag.NewFlagSet("agent run", flag.ContinueOnError)
	controllerURL := fs.String("controller-url", env("TOPO_AGENT_CONTROLLER_URL", ""), "Topo Hub controller base URL (http:// or https://)")
	apiKeyRef := fs.String("api-key-ref", "", "credential reference for the controller API key (env:, file:, vault:, or k8s:)")
	spoolDir := fs.String("spool-dir", "", "absolute path to the encrypted offline-buffer directory")
	spoolKeyRef := fs.String("spool-key-ref", "", "credential reference for the 64-hex-character (32-byte) spool encryption key")
	spoolMaxBytes := fs.Int64("spool-max-bytes", 64<<20, "maximum total bytes retained in the offline buffer")
	interval := fs.Duration("interval", 15*time.Minute, "discovery and delivery interval")
	site := fs.String("site", "default", "site ID")
	collector := fs.String("collector", "", "collector ID; defaults to the local hostname")
	mtlsCertDir := fs.String("mtls-cert-dir", "", "absolute path to the certificate, key, and CA certificate topo agent enroll wrote; authenticates outbound requests with mutual TLS instead of, or alongside, -api-key-ref")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *controllerURL == "" {
		return errors.New("-controller-url is required")
	}
	if *spoolDir == "" {
		return errors.New("-spool-dir is required")
	}

	apiKey, err := resolveCredential(*apiKeyRef, "", "TOPO_AGENT_API_KEY", true)
	if err != nil {
		return fmt.Errorf("resolve controller API key: %w", err)
	}
	spoolKeyHex, err := resolveCredential(*spoolKeyRef, "", "TOPO_AGENT_SPOOL_KEY", false)
	if err != nil {
		return fmt.Errorf("resolve spool encryption key: %w", err)
	}
	spoolKey, err := decodeSpoolKey(spoolKeyHex)
	if err != nil {
		return err
	}

	collectorID := *collector
	if collectorID == "" {
		if hostname, hostErr := os.Hostname(); hostErr == nil {
			collectorID = hostname
		} else {
			collectorID = "topo-agent"
		}
	}

	spool, err := agent.NewSpool(*spoolDir, spoolKey, *spoolMaxBytes)
	if err != nil {
		return fmt.Errorf("initialize spool: %w", err)
	}

	var tlsConfig *tls.Config
	if *mtlsCertDir != "" {
		tlsConfig, err = enrollment.LoadClientTLSConfig(*mtlsCertDir)
		if err != nil {
			return fmt.Errorf("load mTLS certificate: %w", err)
		}
	}
	sender, err := agent.NewSender(*controllerURL, string(apiKey), tlsConfig)
	if err != nil {
		return err
	}

	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	cfg := agent.Config{
		SiteID:      *site,
		CollectorID: collectorID,
		Interval:    *interval,
		Plugin:      localdiscovery.Plugin{},
		Sender:      sender,
		Spool:       spool,
		Logger:      logger,
	}

	asService, err := isWindowsService()
	if err != nil {
		return fmt.Errorf("determine Windows service state: %w", err)
	}
	if asService {
		return runAgentService(cfg)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	logger.Info("topo agent starting", "controller_url", *controllerURL, "interval", interval.String(), "site", *site, "collector", collectorID, "spool_dir", *spoolDir)
	return agent.Run(ctx, cfg)
}

func agentInstall(args []string) error {
	fs := flag.NewFlagSet("agent install", flag.ContinueOnError)
	controllerURL := fs.String("controller-url", env("TOPO_AGENT_CONTROLLER_URL", ""), "Topo Hub controller base URL (http:// or https://)")
	apiKeyRef := fs.String("api-key-ref", "", "credential reference for the controller API key (env:, file:, vault:, or k8s:)")
	spoolDir := fs.String("spool-dir", "", "absolute path to the encrypted offline-buffer directory")
	spoolKeyRef := fs.String("spool-key-ref", "", "credential reference for the 64-hex-character (32-byte) spool encryption key")
	spoolMaxBytes := fs.Int64("spool-max-bytes", 64<<20, "maximum total bytes retained in the offline buffer")
	interval := fs.Duration("interval", 15*time.Minute, "discovery and delivery interval")
	site := fs.String("site", "default", "site ID")
	collector := fs.String("collector", "", "collector ID; defaults to the local hostname at each service start")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *controllerURL == "" {
		return errors.New("-controller-url is required")
	}
	if *spoolDir == "" {
		return errors.New("-spool-dir is required")
	}

	// Credential references are stored as-is, never resolved: the Windows
	// Service Control Manager persists this argument list, so a resolved
	// secret value here would be written permanently to the registry.
	serviceArgs := []string{
		"agent", "run",
		"-controller-url", *controllerURL,
		"-api-key-ref", *apiKeyRef,
		"-spool-dir", *spoolDir,
		"-spool-key-ref", *spoolKeyRef,
		"-spool-max-bytes", strconv.FormatInt(*spoolMaxBytes, 10),
		"-interval", interval.String(),
		"-site", *site,
	}
	if *collector != "" {
		serviceArgs = append(serviceArgs, "-collector", *collector)
	}

	if err := installAgentService(windowsServiceName, serviceArgs); err != nil {
		return err
	}
	fmt.Printf("Topo Agent service %q installed. Start it with: sc.exe start %s\n", windowsServiceName, windowsServiceName)
	return nil
}

func agentUninstall(args []string) error {
	fs := flag.NewFlagSet("agent uninstall", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := uninstallAgentService(windowsServiceName); err != nil {
		return err
	}
	fmt.Printf("Topo Agent service %q uninstalled.\n", windowsServiceName)
	return nil
}

func agentEnroll(args []string) error {
	fs := flag.NewFlagSet("agent enroll", flag.ContinueOnError)
	controllerURL := fs.String("controller-url", env("TOPO_AGENT_CONTROLLER_URL", ""), "Topo Hub controller base URL (http:// or https://)")
	tokenRef := fs.String("token-ref", "", "credential reference for the one-time enrollment token (env:, file:, vault:, or k8s:)")
	certDir := fs.String("cert-dir", "", "absolute path to store the issued certificate, private key, and CA certificate")
	collector := fs.String("collector-id", "", "collector ID to request in the certificate; defaults to the local hostname")
	controllerCACert := fs.String("controller-ca-cert", "", "absolute path to the controller's enrollment CA certificate; required when the controller runs `topo serve -mtls` (its TLS certificate is self-signed by that CA, distribute the file out-of-band alongside the enrollment token); omit when TLS is terminated by a reverse proxy with a publicly-trusted certificate, or the controller runs over plain HTTP")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *controllerURL == "" {
		return errors.New("-controller-url is required")
	}
	if *certDir == "" {
		return errors.New("-cert-dir is required")
	}
	if !filepath.IsAbs(*certDir) {
		return errors.New("-cert-dir must be an absolute path")
	}

	token, err := resolveCredential(*tokenRef, "", "TOPO_AGENT_ENROLLMENT_TOKEN", false)
	if err != nil {
		return fmt.Errorf("resolve enrollment token: %w", err)
	}

	var bootstrapCACert []byte
	if *controllerCACert != "" {
		bootstrapCACert, err = os.ReadFile(*controllerCACert)
		if err != nil {
			return fmt.Errorf("read controller CA certificate: %w", err)
		}
	}

	collectorID := *collector
	if collectorID == "" {
		if hostname, hostErr := os.Hostname(); hostErr == nil {
			collectorID = hostname
		} else {
			collectorID = "topo-agent"
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, err := enrollment.Enroll(ctx, *controllerURL, string(token), collectorID, bootstrapCACert)
	if err != nil {
		return fmt.Errorf("enroll: %w", err)
	}

	if err := os.MkdirAll(*certDir, 0o700); err != nil {
		return fmt.Errorf("create cert directory: %w", err)
	}
	if err := os.Chmod(*certDir, 0o700); err != nil {
		return fmt.Errorf("restrict cert directory permissions: %w", err)
	}
	if err := os.WriteFile(filepath.Join(*certDir, "client-cert.pem"), result.CertificatePEM, 0o644); err != nil {
		return fmt.Errorf("write client certificate: %w", err)
	}
	if err := os.WriteFile(filepath.Join(*certDir, "client-key.pem"), result.PrivateKeyPEM, 0o600); err != nil {
		return fmt.Errorf("write client private key: %w", err)
	}
	if err := os.WriteFile(filepath.Join(*certDir, "ca-cert.pem"), result.CACertificatePEM, 0o644); err != nil {
		return fmt.Errorf("write CA certificate: %w", err)
	}

	fmt.Printf("Enrolled collector %q; certificate expires %s. Certificate, key, and CA certificate written to %s\n", collectorID, result.ExpiresAt.Format(time.RFC3339), *certDir)
	return nil
}

func decodeSpoolKey(hexKey []byte) ([]byte, error) {
	trimmed := bytes.TrimSpace(hexKey)
	key := make([]byte, hex.DecodedLen(len(trimmed)))
	n, err := hex.Decode(key, trimmed)
	if err != nil || n != 32 {
		return nil, errors.New("spool encryption key must be 64 hex characters (32 bytes); generate one with `openssl rand -hex 32`")
	}
	return key[:n], nil
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
	passwordRef := fs.String("password-ref", "", "credential reference for the WinRM password (env: or file:)")
	passwordEnv := fs.String("password-env", "", "deprecated: environment variable containing the WinRM password")
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
	password, err := resolveCredential(*passwordRef, *passwordEnv, "TOPO_WINRM_PASSWORD", false)
	if err != nil {
		return fmt.Errorf("resolve WinRM password: %w", err)
	}
	plugin := winrm.Plugin{Config: winrm.Config{
		Username:         selectedUsername,
		Password:         string(password),
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
	passwordRef := fs.String("password-ref", "", "credential reference for the SSH password (env: or file:)")
	passwordEnv := fs.String("password-env", "", "deprecated: environment variable containing the SSH password")
	privateKeyRef := fs.String("private-key-ref", "", "credential reference for the PEM private key (env: or file:)")
	privateKeyPath := fs.String("private-key", "", "deprecated: PEM private key path")
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
	keyReference, err := resolvePrivateKeyReference(*privateKeyRef, *privateKeyPath)
	if err != nil {
		return err
	}
	if keyReference != "" {
		key, err := credentialref.Resolve(keyReference)
		if err != nil {
			return fmt.Errorf("resolve SSH private key: %w", err)
		}
		signer, err = ssh.ParsePrivateKey(key)
		if err != nil {
			return fmt.Errorf("parse private key: %w", err)
		}
	}
	password, err := resolveCredential(*passwordRef, *passwordEnv, "TOPO_SSH_PASSWORD", true)
	if err != nil {
		return fmt.Errorf("resolve SSH password: %w", err)
	}
	if len(password) == 0 && signer == nil {
		return errors.New("no SSH credential: set -password-ref or -private-key-ref")
	}

	plugin := sshlinux.Plugin{Config: sshlinux.Config{Password: string(password), Signer: signer, HostKeyCallback: callback, Concurrency: *concurrency, ConnectTimeout: *connectTimeout, CommandTimeout: *commandTimeout, MaxOutputBytes: *maxOutputBytes}}
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

func resolveCredential(reference, legacyEnvironment, defaultEnvironment string, optionalDefault bool) ([]byte, error) {
	if reference != "" && legacyEnvironment != "" {
		return nil, errors.New("credential reference and deprecated environment flag cannot be combined")
	}
	implicit := false
	switch {
	case reference != "":
	case legacyEnvironment != "":
		reference = "env:" + legacyEnvironment
	default:
		reference = "env:" + defaultEnvironment
		implicit = true
	}
	value, err := credentialref.Resolve(reference)
	if err != nil && implicit && optionalDefault && errors.Is(err, credentialref.ErrUnavailable) {
		return nil, nil
	}
	return value, err
}

func resolvePrivateKeyReference(reference, legacyPath string) (string, error) {
	if reference != "" && legacyPath != "" {
		return "", errors.New("-private-key-ref and deprecated -private-key cannot be combined")
	}
	if legacyPath == "" {
		return reference, nil
	}
	absolutePath, err := filepath.Abs(legacyPath)
	if err != nil {
		return "", fmt.Errorf("resolve private key path: %w", err)
	}
	return "file:" + absolutePath, nil
}
