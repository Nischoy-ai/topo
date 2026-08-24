package lab

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
)

// The following are the fixed AWS credentials the Topo Lab AWS
// Organizations server accepts. They are not real credentials — Topo Lab
// is loopback-only and every other Lab fixture (Kubernetes, SSH, WinRM,
// SNMP) similarly fixes its accepted credential rather than generating one
// per run.
const (
	LabAWSAccessKeyID     = "AKIATOPOLABFIXTURE00"
	LabAWSSecretAccessKey = "topo-lab-aws-secret-access-key-0123456789ab"
	LabAWSSessionToken    = "topo-lab-aws-session-token"
	LabAWSRegion          = "us-east-1"

	labOrganizationID = "o-topolab0001"
	labManagementID   = "111111111111"
	labRootID         = "r-topo"
)

// AWSOrganizationsServer exposes one simulated AWS Organization — one
// account per Estate host, nested two levels of organizational units below
// a single root, plus one account attached directly to the root — over the
// real AWS Organizations AWS-JSON-1.1 wire protocol, covering only the
// four read-only actions the production plugin actually calls:
// DescribeOrganization, ListRoots, ListOrganizationalUnitsForParent, and
// ListAccountsForParent. It is not a general Organizations API
// implementation — no write, invite, move, tag, or policy action is ever
// served — the same scoped-fixture posture as the SNMP and Kubernetes Lab
// agents.
//
// Unlike the Kubernetes fixture, which encodes real k8s.io/api Go types
// directly (those carry public JSON struct tags), aws-sdk-go-v2 generates
// its (de)serializers from a service model rather than JSON struct tags,
// so its types cannot be encoding/json-marshaled directly. This fixture
// instead defines minimal local structs mirroring the exact wire field
// names the generated deserializer expects (confirmed by reading
// aws-sdk-go-v2's own deserializers.go) — the wire format and the
// plugin's real client-side request construction and response decoding
// are still genuinely exercised, only the server-side encoding uses local
// types rather than the SDK's own.
//
// Authentication is verified as real AWS SigV4 — not a simplified string
// comparison — by re-deriving the expected Authorization header with the
// SDK's own v4.Signer against the known Lab credential and comparing it to
// what the client sent, the same technique a from-scratch signature
// verifier would use, without hand-rolling the HMAC-SHA256 canonicalization
// itself.
type AWSOrganizationsServer struct {
	Estate Estate

	roots    []labRoot
	ous      map[string][]labOU      // keyed by parent ID
	accounts map[string][]labAccount // keyed by parent ID
}

type labRoot struct{ id, name string }
type labOU struct{ id, name, path, parentID string }
type labAccount struct {
	id, name, email, state, joinedMethod, parentID string
	joinedAt                                       time.Time
}

func NewAWSOrganizationsServer(estate Estate) *AWSOrganizationsServer {
	server := &AWSOrganizationsServer{
		Estate: estate,
		roots:  []labRoot{{id: labRootID, name: "Root"}},
		ous:    map[string][]labOU{},
	}

	const (
		prodOU        = "ou-topo-prod00001"
		nonProdOU     = "ou-topo-nonprod01"
		prodWorkOU    = "ou-topo-prodwork1"
		nonProdWorkOU = "ou-topo-nprodwrk1"
	)
	server.ous[labRootID] = []labOU{
		{id: prodOU, name: "Production", path: "Root/Production", parentID: labRootID},
		{id: nonProdOU, name: "NonProduction", path: "Root/NonProduction", parentID: labRootID},
	}
	server.ous[prodOU] = []labOU{{id: prodWorkOU, name: "Workloads", path: "Root/Production/Workloads", parentID: prodOU}}
	server.ous[nonProdOU] = []labOU{{id: nonProdWorkOU, name: "Workloads", path: "Root/NonProduction/Workloads", parentID: nonProdOU}}

	accounts := map[string][]labAccount{}
	joined := time.Unix(1700000000, 0).UTC()
	for i, host := range server.Estate.Hosts {
		id := fmt.Sprintf("%012d", 100000000001+i)
		account := labAccount{id: id, name: host.Name, email: host.Name + "@topo-lab.example", state: "ACTIVE", joinedMethod: "CREATED", joinedAt: joined}
		var parent string
		switch {
		case i == 0:
			parent = labRootID
		case i%2 == 0:
			parent = prodWorkOU
		default:
			parent = nonProdWorkOU
		}
		account.parentID = parent
		accounts[parent] = append(accounts[parent], account)
	}
	server.accounts = accounts
	return server
}

func (server *AWSOrganizationsServer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /", server.handleRequest)
	return mux
}

func (server *AWSOrganizationsServer) handleRequest(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeAWSError(w, http.StatusBadRequest, "SerializationException", "failed to read request body")
		return
	}
	if err := verifySigV4(r, body); err != nil {
		writeAWSError(w, http.StatusForbidden, "AccessDeniedException", err.Error())
		return
	}

	target := r.Header.Get("X-Amz-Target")
	action := target
	if idx := strings.LastIndex(target, "."); idx >= 0 {
		action = target[idx+1:]
	}

	switch action {
	case "DescribeOrganization":
		server.handleDescribeOrganization(w)
	case "ListRoots":
		server.handleListRoots(w)
	case "ListOrganizationalUnitsForParent":
		server.handleListOUsForParent(w, body)
	case "ListAccountsForParent":
		server.handleListAccountsForParent(w, body)
	default:
		writeAWSError(w, http.StatusBadRequest, "UnknownOperationException", "unsupported operation "+action)
	}
}

func (server *AWSOrganizationsServer) handleDescribeOrganization(w http.ResponseWriter) {
	writeAWSJSON(w, http.StatusOK, map[string]any{
		"Organization": map[string]any{
			"Id":                 labOrganizationID,
			"Arn":                "arn:aws:organizations::" + labManagementID + ":organization/" + labOrganizationID,
			"FeatureSet":         "ALL",
			"MasterAccountArn":   "arn:aws:organizations::" + labManagementID + ":account/" + labOrganizationID + "/" + labManagementID,
			"MasterAccountEmail": "management@topo-lab.example",
			"MasterAccountId":    labManagementID,
		},
	})
}

func (server *AWSOrganizationsServer) handleListRoots(w http.ResponseWriter) {
	roots := make([]map[string]any, 0, len(server.roots))
	for _, root := range server.roots {
		roots = append(roots, map[string]any{
			"Id":   root.id,
			"Arn":  "arn:aws:organizations::" + labManagementID + ":root/" + labOrganizationID + "/" + root.id,
			"Name": root.name,
		})
	}
	writeAWSJSON(w, http.StatusOK, map[string]any{"Roots": roots})
}

func (server *AWSOrganizationsServer) handleListOUsForParent(w http.ResponseWriter, body []byte) {
	parentID, err := parseParentID(body)
	if err != nil {
		writeAWSError(w, http.StatusBadRequest, "InvalidInputException", err.Error())
		return
	}
	children := server.ous[parentID]
	out := make([]map[string]any, 0, len(children))
	for _, ou := range children {
		out = append(out, map[string]any{
			"Id":   ou.id,
			"Arn":  "arn:aws:organizations::" + labManagementID + ":ou/" + labOrganizationID + "/" + ou.id,
			"Name": ou.name,
			"Path": ou.path,
		})
	}
	writeAWSJSON(w, http.StatusOK, map[string]any{"OrganizationalUnits": out})
}

func (server *AWSOrganizationsServer) handleListAccountsForParent(w http.ResponseWriter, body []byte) {
	parentID, err := parseParentID(body)
	if err != nil {
		writeAWSError(w, http.StatusBadRequest, "InvalidInputException", err.Error())
		return
	}
	children := server.accounts[parentID]
	out := make([]map[string]any, 0, len(children))
	for _, account := range children {
		out = append(out, map[string]any{
			"Id":              account.id,
			"Arn":             "arn:aws:organizations::" + labManagementID + ":account/" + labOrganizationID + "/" + account.id,
			"Name":            account.name,
			"Email":           account.email,
			"State":           account.state,
			"JoinedMethod":    account.joinedMethod,
			"JoinedTimestamp": json.Number(strconv.FormatInt(account.joinedAt.Unix(), 10)),
		})
	}
	writeAWSJSON(w, http.StatusOK, map[string]any{"Accounts": out})
}

func parseParentID(body []byte) (string, error) {
	var input struct {
		ParentId string
	}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &input); err != nil {
			return "", errors.New("malformed request body")
		}
	}
	if input.ParentId == "" {
		return "", errors.New("ParentId is required")
	}
	return input.ParentId, nil
}

// verifySigV4 re-derives the Authorization header the request should carry
// — using the real AWS SDK v4 signer against the known Lab credential, over
// exactly the header set the client's own Authorization header claims it
// signed (its SignedHeaders component) — and compares it to what the
// request actually presented. A mismatch, an unknown access key, or a
// malformed header is rejected the same way a real AWS endpoint would
// reject an invalid signature.
func verifySigV4(r *http.Request, body []byte) error {
	authHeader := r.Header.Get("Authorization")
	parsed, err := parseAuthorizationHeader(authHeader)
	if err != nil {
		return err
	}
	if parsed.accessKeyID != LabAWSAccessKeyID {
		return errors.New("unknown access key")
	}
	dateHeader := r.Header.Get("X-Amz-Date")
	signingTime, err := time.Parse("20060102T150405Z", dateHeader)
	if err != nil {
		return fmt.Errorf("invalid or missing X-Amz-Date: %w", err)
	}

	reconstructed := &http.Request{
		Method:        r.Method,
		URL:           r.URL,
		Host:          r.Host,
		ContentLength: int64(len(body)),
		Header:        make(http.Header, len(parsed.signedHeaders)),
	}
	for _, name := range parsed.signedHeaders {
		switch {
		case strings.EqualFold(name, "host"):
			continue // the signer derives "host" from req.Host, not a Header entry
		case strings.EqualFold(name, "content-length"):
			continue // the signer derives "content-length" from req.ContentLength, not a Header entry
		}
		values, ok := r.Header[http.CanonicalHeaderKey(name)]
		if !ok {
			return fmt.Errorf("missing signed header %q", name)
		}
		reconstructed.Header[http.CanonicalHeaderKey(name)] = values
	}

	payloadHash := sha256Hex(body)
	signer := v4.NewSigner()
	creds := awssdk.Credentials{AccessKeyID: LabAWSAccessKeyID, SecretAccessKey: LabAWSSecretAccessKey, SessionToken: LabAWSSessionToken}
	if err := signer.SignHTTP(r.Context(), creds, reconstructed, payloadHash, parsed.service, parsed.region, signingTime); err != nil {
		return fmt.Errorf("re-signing failed: %w", err)
	}
	if reconstructed.Header.Get("Authorization") != authHeader {
		return errors.New("signature mismatch")
	}
	return nil
}

type sigV4Authorization struct {
	accessKeyID   string
	region        string
	service       string
	signedHeaders []string
}

func parseAuthorizationHeader(value string) (sigV4Authorization, error) {
	const scheme = "AWS4-HMAC-SHA256 "
	if !strings.HasPrefix(value, scheme) {
		return sigV4Authorization{}, errors.New("unsupported or missing Authorization scheme")
	}
	var out sigV4Authorization
	for _, part := range strings.Split(strings.TrimPrefix(value, scheme), ", ") {
		key, val, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			return sigV4Authorization{}, errors.New("malformed Authorization header")
		}
		switch key {
		case "Credential":
			scope := strings.Split(val, "/")
			if len(scope) != 5 {
				return sigV4Authorization{}, errors.New("malformed Authorization credential scope")
			}
			out.accessKeyID, out.region, out.service = scope[0], scope[2], scope[3]
		case "SignedHeaders":
			out.signedHeaders = strings.Split(val, ";")
		}
	}
	if out.accessKeyID == "" || out.service == "" || len(out.signedHeaders) == 0 {
		return sigV4Authorization{}, errors.New("incomplete Authorization header")
	}
	return out, nil
}

func sha256Hex(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func writeAWSJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/x-amz-json-1.1")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// writeAWSError mirrors the shape of a real AWS JSON-protocol error
// response closely enough for the generated client's own error decoding
// to recognize it and construct the corresponding typed exception.
func writeAWSError(w http.ResponseWriter, status int, errorType, message string) {
	w.Header().Set("Content-Type", "application/x-amz-json-1.1")
	w.Header().Set("X-Amzn-ErrorType", errorType)
	w.WriteHeader(status)
	var buf bytes.Buffer
	_ = json.NewEncoder(&buf).Encode(map[string]any{"__type": errorType, "message": message})
	_, _ = w.Write(buf.Bytes())
}
