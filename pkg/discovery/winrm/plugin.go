package winrm

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Nischoy-ai/topo/pkg/discovery"
	"github.com/Nischoy-ai/topo/pkg/model"
)

const maxEnumerationPages = 16

type Config struct {
	// Basic authentication is restricted to explicit loopback-only LabMode.
	// Production callers must supply an authenticated HTTPClient and HTTPS
	// targets; built-in NTLM/Negotiate is a follow-on slice.
	Username, Password               string
	LabMode                          bool
	Concurrency                      int
	ConnectTimeout, OperationTimeout time.Duration
	MaxResponseBytes                 int64
	HTTPClient                       *http.Client
}

type Plugin struct{ Config Config }

func (p Plugin) DescribeCapabilities(context.Context) discovery.Capability {
	return discovery.Capability{
		Name:       "winrm-windows",
		Version:    "0.1.0",
		AssetTypes: []model.AssetType{model.AssetHost, model.AssetNetworkInterface},
		RequiredPermissions: []string{
			"WS-Management access to Win32_ComputerSystem, Win32_ComputerSystemProduct, Win32_BIOS, and Win32_OperatingSystem",
			"optional WS-Management access to Win32_NetworkAdapterConfiguration",
		},
	}
}

func (p Plugin) ValidateConfiguration(_ context.Context, request discovery.Request) error {
	if len(request.Targets) == 0 {
		return errors.New("at least one WinRM target is required")
	}
	if p.Config.Concurrency < 0 || p.Config.Concurrency > 1024 {
		return errors.New("WinRM concurrency must be between 0 (default) and 1024")
	}
	if p.Config.ConnectTimeout < 0 || p.Config.OperationTimeout < 0 {
		return errors.New("WinRM timeouts cannot be negative")
	}
	if p.Config.MaxResponseBytes < 0 {
		return errors.New("WinRM maximum response size cannot be negative")
	}
	for key := range request.Options {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "password") || strings.Contains(lower, "secret") || strings.Contains(lower, "token") || strings.Contains(lower, "credential") {
			return fmt.Errorf("WinRM secrets are not accepted in request option %q", key)
		}
	}
	if p.Config.LabMode {
		if p.Config.Username == "" || p.Config.Password == "" {
			return errors.New("Topo Lab WinRM username and password are required")
		}
	} else if p.Config.Username != "" || p.Config.Password != "" {
		return errors.New("built-in Basic authentication is restricted to Topo Lab")
	}
	for _, target := range request.Targets {
		if err := validateTarget(target, p.Config.LabMode); err != nil {
			return err
		}
	}
	return nil
}

func (p Plugin) CheckConnectivity(ctx context.Context, request discovery.Request) error {
	if err := p.ValidateConfiguration(ctx, request); err != nil {
		return err
	}
	p = p.withHTTPClient()
	_, err := p.enumerate(ctx, request.Targets[0], auditedOperations[0])
	return err
}

func (p Plugin) Discover(ctx context.Context, request discovery.Request) (model.ObservationEnvelope, error) {
	if err := p.ValidateConfiguration(ctx, request); err != nil {
		return model.ObservationEnvelope{}, err
	}
	p = p.withHTTPClient()
	now := time.Now().UTC()
	observation := model.ObservationEnvelope{
		SchemaVersion: model.SchemaVersion,
		ObservationID: observationID(),
		SiteID:        valueOr(request.SiteID, "default"),
		CollectorID:   valueOr(request.CollectorID, "winrm-relay"),
		Plugin:        "winrm-windows",
		JobID:         request.JobID,
		ObservedAt:    now,
	}
	concurrency := p.Config.Concurrency
	if concurrency < 1 {
		concurrency = 32
	}
	jobs := make(chan string)
	var workers sync.WaitGroup
	var mu sync.Mutex
	for range concurrency {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for target := range jobs {
				inventory, collectionErrors := p.discoverTarget(ctx, target)
				mu.Lock()
				if inventory != nil {
					assets, relationships := inventory.Assets(now)
					observation.Assets = append(observation.Assets, assets...)
					observation.Relationships = append(observation.Relationships, relationships...)
				}
				observation.Errors = append(observation.Errors, collectionErrors...)
				mu.Unlock()
			}
		}()
	}
	for _, target := range request.Targets {
		select {
		case <-ctx.Done():
			close(jobs)
			workers.Wait()
			return model.ObservationEnvelope{}, ctx.Err()
		case jobs <- target:
		}
	}
	close(jobs)
	workers.Wait()
	if err := ctx.Err(); err != nil {
		return model.ObservationEnvelope{}, err
	}
	return observation, nil
}

func (p Plugin) discoverTarget(ctx context.Context, target string) (*Inventory, []model.CollectionError) {
	results := map[string][]object{}
	var collectionErrors []model.CollectionError
	for _, operation := range auditedOperations {
		objects, err := p.enumerate(ctx, target, operation)
		if err != nil {
			if operation.Required {
				return nil, append(collectionErrors, model.CollectionError{Code: "winrm_operation", Message: target + ": " + operation.Name + ": " + err.Error(), Retryable: retryable(err)})
			}
			collectionErrors = append(collectionErrors, model.CollectionError{Code: "winrm_partial", Message: target + ": " + operation.Name + ": " + err.Error(), Retryable: false})
			continue
		}
		results[operation.Name] = objects
	}
	inventory, err := parseInventory(results)
	if err != nil {
		return nil, append(collectionErrors, model.CollectionError{Code: "winrm_parse", Message: target + ": " + err.Error()})
	}
	if networkObjects, ok := results[OperationNetwork]; ok {
		interfaces, err := parseInterfaces(networkObjects)
		if err != nil {
			collectionErrors = append(collectionErrors, model.CollectionError{Code: "winrm_partial", Message: target + ": network: " + err.Error()})
		} else {
			inventory.Interfaces = interfaces
		}
	}
	return &inventory, collectionErrors
}

func (p Plugin) enumerate(ctx context.Context, target string, operation Operation) ([]object, error) {
	timeout := p.operationTimeout()
	operationCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	page, err := p.request(operationCtx, target, enumerateEnvelope(target, operation, timeout))
	if err != nil {
		return nil, err
	}
	if page.Context == "" && !page.Done {
		return nil, errors.New("enumeration response omitted context and end marker")
	}
	items := append([]object(nil), page.Items...)
	if len(items) > 65536 {
		return nil, errors.New("enumeration exceeded 65536 objects")
	}
	pages := 1
	for page.Context != "" && !page.Done {
		if len(items) > 65536 {
			return nil, errors.New("enumeration exceeded 65536 objects")
		}
		if pages >= maxEnumerationPages {
			return nil, fmt.Errorf("enumeration exceeded %d pages", maxEnumerationPages)
		}
		page, err = p.request(operationCtx, target, pullEnvelope(target, operation, page.Context, timeout))
		if err != nil {
			return nil, err
		}
		if page.Context == "" && !page.Done {
			return nil, errors.New("pull response omitted context and end marker")
		}
		pages++
		items = append(items, page.Items...)
		if len(items) > 65536 {
			return nil, errors.New("enumeration exceeded 65536 objects")
		}
	}
	if !page.Done && page.Context != "" {
		return nil, fmt.Errorf("enumeration exceeded %d pages", maxEnumerationPages)
	}
	return items, nil
}

func (p Plugin) request(ctx context.Context, target string, envelope []byte) (enumerationPage, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(envelope))
	if err != nil {
		return enumerationPage{}, err
	}
	request.Header.Set("Content-Type", "application/soap+xml;charset=UTF-8")
	request.Header.Set("User-Agent", "Nischoy-Topo/0.1")
	if p.Config.LabMode {
		request.SetBasicAuth(p.Config.Username, p.Config.Password)
	}
	response, err := p.Config.HTTPClient.Do(request)
	if err != nil {
		return enumerationPage{}, err
	}
	if err := ctx.Err(); err != nil {
		_ = response.Body.Close()
		return enumerationPage{}, err
	}
	defer response.Body.Close()
	maximum := p.Config.MaxResponseBytes
	if maximum == 0 {
		maximum = 4 << 20
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maximum+1))
	if err != nil {
		return enumerationPage{}, fmt.Errorf("read WS-Management response: %w", err)
	}
	if int64(len(body)) > maximum {
		return enumerationPage{}, fmt.Errorf("WS-Management response exceeded %d bytes", maximum)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if _, parseErr := parseEnumerationPage(body); parseErr != nil {
			return enumerationPage{}, fmt.Errorf("HTTP %s: %w", response.Status, parseErr)
		}
		return enumerationPage{}, fmt.Errorf("HTTP %s", response.Status)
	}
	return parseEnumerationPage(body)
}

func (p Plugin) withHTTPClient() Plugin {
	if p.Config.HTTPClient != nil {
		client := *p.Config.HTTPClient
		client.CheckRedirect = rejectRedirect
		p.Config.HTTPClient = &client
		return p
	}
	connectTimeout := p.Config.ConnectTimeout
	if connectTimeout <= 0 {
		connectTimeout = 10 * time.Second
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = (&net.Dialer{Timeout: connectTimeout, KeepAlive: 30 * time.Second}).DialContext
	transport.TLSHandshakeTimeout = connectTimeout
	p.Config.HTTPClient = &http.Client{Transport: transport, CheckRedirect: rejectRedirect}
	return p
}

func (p Plugin) operationTimeout() time.Duration {
	if p.Config.OperationTimeout > 0 {
		return p.Config.OperationTimeout
	}
	return 10 * time.Second
}

func validateTarget(raw string, labMode bool) error {
	if len(raw) > 2048 {
		return errors.New("WinRM target exceeds 2048 bytes")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return fmt.Errorf("invalid WinRM target %q", raw)
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("WinRM target %q must not contain credentials, a query, or a fragment", raw)
	}
	if labMode {
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return fmt.Errorf("Topo Lab WinRM target %q must use HTTP or HTTPS", raw)
		}
		hostname := parsed.Hostname()
		ip := net.ParseIP(hostname)
		if !strings.EqualFold(hostname, "localhost") && (ip == nil || !ip.IsLoopback()) {
			return fmt.Errorf("Topo Lab WinRM target %q must be loopback", raw)
		}
	} else if parsed.Scheme != "https" {
		return fmt.Errorf("WinRM target %q must use HTTPS", raw)
	}
	return nil
}

func rejectRedirect(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }

func retryable(err error) bool {
	var netError net.Error
	return errors.As(err, &netError) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func observationID() string {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return fmt.Sprintf("obs-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(value)
}
