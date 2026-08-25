package lab

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"strings"
	"time"
)

// The following are the fixed Azure service-principal credentials the
// Topo Lab Azure server accepts. They are not real credentials — Topo Lab
// is loopback-only and every other Lab fixture (AWS, Kubernetes, SSH,
// WinRM, SNMP) similarly fixes its accepted credential rather than
// generating one per run.
const (
	LabAzureTenantID     = "11111111-1111-1111-1111-111111111111"
	LabAzureClientID     = "22222222-2222-2222-2222-222222222222"
	LabAzureClientSecret = "topo-lab-azure-client-secret-0123456789ab"

	labAzureAccessToken   = "topo-lab-azure-access-token"
	labAzureRootGroupName = LabAzureTenantID // Azure's own convention: the automatically created "Tenant Root Group" has the tenant's GUID as its name.
)

// AzureServer exposes one simulated Azure AD tenant — one subscription per
// Estate host, nested two levels of management groups below the tenant
// root group (the first host attached directly to the root, matching the
// AWS Organizations fixture's "one account directly under root" case) —
// over the real Azure Resource Manager wire protocol, covering only the
// endpoints the production plugin actually calls: the tenant's OpenID
// Connect discovery document, the OAuth2 client-credentials token
// endpoint, GET /tenants, GET .../managementGroups/{id} with
// $expand=children&$recurse=true, and GET /subscriptions. It is not a
// general ARM API implementation — no write, move, or delete action is
// ever served — the same scoped-fixture posture as the AWS and
// Kubernetes Lab agents.
//
// Unlike the AWS fixture, which must independently re-derive a SigV4
// signature to authenticate a request, Azure's Resource Manager API has
// no per-request signing scheme: a client obtains a bearer token once
// from Azure AD via the real OAuth2 client-credentials grant, then
// presents it on every ARM call. Verifying the client_id/client_secret
// pair at the token endpoint and the bearer token on every ARM call by
// equality is therefore not a simplification here — it is the real
// protocol; there is nothing further to cryptographically re-derive.
//
// azidentity, the Azure SDK's credential package, unconditionally refuses
// a non-HTTPS authority host (no client option can override this), so
// unlike the Kubernetes and AWS Lab fixtures this server cannot fall back
// to plain HTTP even on loopback: it always serves HTTPS with a freshly
// generated, self-signed, loopback-only certificate.
type AzureServer struct {
	Estate Estate

	// BaseURL is this server's own externally reachable base URL
	// (scheme://host:port, no path) — Azure AD's OpenID Connect discovery
	// document must advertise absolute endpoint URLs, so callers must set
	// this before the discovery endpoint is first requested. ServeAzureTLS
	// sets it automatically; direct httptest.NewTLSServer callers set it
	// from the returned test server's URL immediately after starting it,
	// before issuing any request (see the package's own tests).
	BaseURL string

	rootProperties managementGroupProperties
	subscriptions  []azureSubscription
}

type azureSubscription struct {
	subscriptionID, name, parentARMID string
}

type managementGroupChild struct {
	ID          string                  `json:"id"`
	Name        string                  `json:"name"`
	Type        string                  `json:"type"`
	DisplayName string                  `json:"displayName,omitempty"`
	Children    []*managementGroupChild `json:"children,omitempty"`
}

type managementGroupProperties struct {
	DisplayName string                  `json:"displayName"`
	TenantID    string                  `json:"tenantId"`
	Children    []*managementGroupChild `json:"children"`
}

func NewAzureServer(estate Estate) *AzureServer {
	server := &AzureServer{Estate: estate}

	const (
		prodMG        = "/providers/Microsoft.Management/managementGroups/topo-lab-prod"
		nonProdMG     = "/providers/Microsoft.Management/managementGroups/topo-lab-nonprod"
		prodWorkMG    = "/providers/Microsoft.Management/managementGroups/topo-lab-prod-workloads"
		nonProdWorkMG = "/providers/Microsoft.Management/managementGroups/topo-lab-nonprod-workloads"
		rootARMID     = "/providers/Microsoft.Management/managementGroups/" + labAzureRootGroupName
	)

	prodWorkloads := &managementGroupChild{ID: prodWorkMG, Name: "topo-lab-prod-workloads", Type: "Microsoft.Management/managementGroups", DisplayName: "Workloads"}
	nonProdWorkloads := &managementGroupChild{ID: nonProdWorkMG, Name: "topo-lab-nonprod-workloads", Type: "Microsoft.Management/managementGroups", DisplayName: "Workloads"}

	var subscriptions []azureSubscription
	for i, host := range server.Estate.Hosts {
		subID := stableToken(server.Estate.Scenario.Seed, "azure-subscription", host.ID)[:32]
		subID = formatGUID(subID)
		armID := "/subscriptions/" + subID
		var parent *managementGroupChild
		var parentARMID string
		switch {
		case i == 0:
			parent = nil // attached directly under the root group, exercising that case
			parentARMID = rootARMID
		case i%2 == 0:
			parent = prodWorkloads
			parentARMID = prodWorkMG
		default:
			parent = nonProdWorkloads
			parentARMID = nonProdWorkMG
		}
		child := &managementGroupChild{ID: armID, Name: subID, Type: "/subscriptions", DisplayName: host.Name}
		if parent != nil {
			parent.Children = append(parent.Children, child)
		}
		subscriptions = append(subscriptions, azureSubscription{subscriptionID: subID, name: host.Name, parentARMID: parentARMID})
	}

	server.rootProperties = managementGroupProperties{
		DisplayName: "Tenant Root Group",
		TenantID:    LabAzureTenantID,
		Children: []*managementGroupChild{
			{ID: prodMG, Name: "topo-lab-prod", Type: "Microsoft.Management/managementGroups", DisplayName: "Production", Children: []*managementGroupChild{prodWorkloads}},
			{ID: nonProdMG, Name: "topo-lab-nonprod", Type: "Microsoft.Management/managementGroups", DisplayName: "NonProduction", Children: []*managementGroupChild{nonProdWorkloads}},
		},
	}
	// The first host's subscription attaches directly under the root
	// group, appended after Production/NonProduction so its position is
	// deterministic regardless of host count.
	if len(server.Estate.Hosts) > 0 {
		host := server.Estate.Hosts[0]
		subID := subscriptions[0].subscriptionID
		server.rootProperties.Children = append(server.rootProperties.Children, &managementGroupChild{
			ID: "/subscriptions/" + subID, Name: subID, Type: "/subscriptions", DisplayName: host.Name,
		})
	}
	server.subscriptions = subscriptions
	return server
}

// Handler returns the HTTPS handler. BaseURL must be set (see its doc
// comment) before the discovery endpoint is requested, but need not be
// set before calling Handler itself.
func (server *AzureServer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{tenantID}/v2.0/.well-known/openid-configuration", server.handleDiscovery)
	mux.HandleFunc("POST /{tenantID}/oauth2/v2.0/token", server.handleToken)
	mux.HandleFunc("GET /tenants", server.authenticated(server.handleTenants))
	mux.HandleFunc("GET /providers/Microsoft.Management/managementGroups/{groupID}", server.authenticated(server.handleManagementGroup))
	mux.HandleFunc("GET /subscriptions", server.authenticated(server.handleSubscriptions))
	return mux
}

// ServeAzureTLS generates a fresh loopback-only self-signed certificate,
// binds addr, and starts serving server's Handler over HTTPS in the
// background. It returns the server's base URL (with BaseURL already set)
// and the underlying *http.Server for the caller to Shutdown.
func ServeAzureTLS(server *AzureServer, addr string) (string, *http.Server, error) {
	cert, err := generateLoopbackCertificate()
	if err != nil {
		return "", nil, err
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return "", nil, err
	}
	baseURL := "https://" + listener.Addr().String()
	server.BaseURL = baseURL
	httpServer := &http.Server{
		Handler:           server.Handler(),
		TLSConfig:         &tls.Config{Certificates: []tls.Certificate{cert}},
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	go func() { _ = httpServer.ServeTLS(listener, "", "") }()
	return baseURL, httpServer, nil
}

func (server *AzureServer) authenticated(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+labAzureAccessToken {
			writeARMError(w, http.StatusUnauthorized, "AuthenticationFailed", "bearer token is missing or invalid")
			return
		}
		next(w, r)
	}
}

func (server *AzureServer) handleDiscovery(w http.ResponseWriter, r *http.Request) {
	tenantID := r.PathValue("tenantID")
	base := strings.TrimSuffix(server.BaseURL, "/") + "/" + tenantID
	writeAzureJSON(w, http.StatusOK, map[string]any{
		"issuer":                 base + "/v2.0",
		"authorization_endpoint": base + "/oauth2/v2.0/authorize",
		"token_endpoint":         base + "/oauth2/v2.0/token",
	})
}

func (server *AzureServer) handleToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "malformed token request body")
		return
	}
	if r.PathValue("tenantID") != LabAzureTenantID ||
		r.FormValue("grant_type") != "client_credentials" ||
		r.FormValue("client_id") != LabAzureClientID ||
		r.FormValue("client_secret") != LabAzureClientSecret {
		writeOAuthError(w, http.StatusUnauthorized, "invalid_client", "client authentication failed")
		return
	}
	writeAzureJSON(w, http.StatusOK, map[string]any{
		"token_type":   "Bearer",
		"expires_in":   3599,
		"access_token": labAzureAccessToken,
	})
}

func (server *AzureServer) handleTenants(w http.ResponseWriter, _ *http.Request) {
	writeAzureJSON(w, http.StatusOK, map[string]any{
		"value": []map[string]any{
			{
				"id": "/tenants/" + LabAzureTenantID, "tenantId": LabAzureTenantID,
				"displayName": "Topo Lab Tenant", "defaultDomain": "topolab.onmicrosoft.com",
			},
		},
	})
}

func (server *AzureServer) handleManagementGroup(w http.ResponseWriter, r *http.Request) {
	groupID := r.PathValue("groupID")
	if groupID != labAzureRootGroupName {
		writeARMError(w, http.StatusNotFound, "ManagementGroupNotFound", fmt.Sprintf("management group %q was not found", groupID))
		return
	}
	writeAzureJSON(w, http.StatusOK, map[string]any{
		"id":         "/providers/Microsoft.Management/managementGroups/" + labAzureRootGroupName,
		"name":       labAzureRootGroupName,
		"type":       "Microsoft.Management/managementGroups",
		"properties": server.rootProperties,
	})
}

func (server *AzureServer) handleSubscriptions(w http.ResponseWriter, _ *http.Request) {
	value := make([]map[string]any, 0, len(server.subscriptions))
	for _, sub := range server.subscriptions {
		value = append(value, map[string]any{
			"id": "/subscriptions/" + sub.subscriptionID, "subscriptionId": sub.subscriptionID,
			"displayName": sub.name, "state": "Enabled", "tenantId": LabAzureTenantID,
		})
	}
	writeAzureJSON(w, http.StatusOK, map[string]any{"value": value})
}

func writeAzureJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// writeARMError mirrors the shape of a real Azure Resource Manager error
// response closely enough to be a meaningful, real HTTP failure for the
// generated client's response handling.
func writeARMError(w http.ResponseWriter, status int, code, message string) {
	writeAzureJSON(w, status, map[string]any{"error": map[string]any{"code": code, "message": message}})
}

// writeOAuthError mirrors the RFC 6749 OAuth2 token-error response shape
// azidentity/MSAL expects from the token endpoint.
func writeOAuthError(w http.ResponseWriter, status int, code, description string) {
	writeAzureJSON(w, status, map[string]any{"error": code, "error_description": description})
}

// formatGUID reshapes a 32-character hex string into GUID form so
// generated subscription IDs look like real Azure subscription IDs.
func formatGUID(hex32 string) string {
	if len(hex32) != 32 {
		return hex32
	}
	return strings.Join([]string{hex32[0:8], hex32[8:12], hex32[12:16], hex32[16:20], hex32[20:32]}, "-")
}

// generateLoopbackCertificate creates a fresh, short-lived, self-signed
// ECDSA certificate valid for 127.0.0.1 and localhost — used only to
// satisfy azidentity's unconditional requirement that the Azure AD
// authority host be HTTPS, never presented as a trusted certificate.
func generateLoopbackCertificate() (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, err
	}
	template := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "topo-lab-azure"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, nil
}
