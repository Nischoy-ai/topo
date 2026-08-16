package winrm

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestNTLMTransportCompletesChallengeWithoutSendingBasic(t *testing.T) {
	base := &ntlmHandshakeTransport{t: t}
	transport := ntlmRoundTripper{base: base, username: "user@example.test", password: "secret"}
	request, err := http.NewRequest(http.MethodPost, "https://windows.example/wsman", bytes.NewReader([]byte("soap-body")))
	if err != nil {
		t.Fatal(err)
	}
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || base.calls != 3 {
		t.Fatalf("status=%d calls=%d, want successful three-message handshake", response.StatusCode, base.calls)
	}
	for _, authorization := range base.authorizations {
		if strings.HasPrefix(strings.ToLower(authorization), "basic ") {
			t.Fatalf("transport sent Basic credentials: %q", authorization)
		}
	}
}

func TestNTLMTransportPreservesTLSConnectionAffinity(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	remoteAddress := ""
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		if request.ProtoMajor != 1 {
			t.Errorf("NTLM request used HTTP/%d", request.ProtoMajor)
		}
		if remoteAddress == "" {
			remoteAddress = request.RemoteAddr
		} else if request.RemoteAddr != remoteAddress {
			t.Errorf("NTLM handshake moved connections: got %s, want %s", request.RemoteAddr, remoteAddress)
		}
		switch calls {
		case 1:
			response.Header().Set("WWW-Authenticate", "NTLM")
			response.WriteHeader(http.StatusUnauthorized)
		case 2:
			assertNTLMMessageType(t, request.Header.Get("Authorization"), "NTLM", 1)
			response.Header().Set("WWW-Authenticate", "NTLM "+base64.StdEncoding.EncodeToString(testNTLMChallenge()))
			response.WriteHeader(http.StatusUnauthorized)
		case 3:
			assertNTLMMessageType(t, request.Header.Get("Authorization"), "NTLM", 3)
			response.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected request %d", calls)
			response.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	certificatePool := x509.NewCertPool()
	certificatePool.AddCert(server.Certificate())
	base := http.DefaultTransport.(*http.Transport).Clone()
	base.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: certificatePool}
	client := &http.Client{Transport: ntlmRoundTripper{base: ntlmBaseTransport(base), username: "user@example.test", password: "secret"}}
	request, err := http.NewRequest(http.MethodPost, server.URL+"/wsman", bytes.NewReader([]byte("soap")))
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer result.Body.Close()
	mu.Lock()
	finalCalls := calls
	mu.Unlock()
	if result.StatusCode != http.StatusOK || finalCalls != 3 {
		t.Fatalf("status=%d calls=%d, want successful same-connection handshake", result.StatusCode, finalCalls)
	}
}

func TestNTLMTransportRejectsBasicOnlyAndMalformedChallenges(t *testing.T) {
	for _, test := range []struct {
		name      string
		base      http.RoundTripper
		wantError string
		wantCode  int
	}{
		{name: "basic only", base: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.Header.Get("Authorization") != "" {
				t.Fatalf("Basic-only server received authorization %q", request.Header.Get("Authorization"))
			}
			return authResponse(request, http.StatusUnauthorized, `Basic realm="windows"`), nil
		}), wantCode: http.StatusUnauthorized},
		{name: "malformed token", base: &challengeSequenceTransport{}, wantError: "malformed or oversized"},
	} {
		t.Run(test.name, func(t *testing.T) {
			transport := ntlmRoundTripper{base: test.base, username: "user@example.test", password: "secret"}
			request, err := http.NewRequest(http.MethodPost, "https://windows.example/wsman", bytes.NewReader([]byte("soap")))
			if err != nil {
				t.Fatal(err)
			}
			response, err := transport.RoundTrip(request)
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("got error %v, want %q", err, test.wantError)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != test.wantCode {
				t.Fatalf("got status %d, want %d", response.StatusCode, test.wantCode)
			}
		})
	}
}

func TestNTLMTransportRequiresHTTPSAndReplayableBodies(t *testing.T) {
	base := roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("base transport should not be called")
		return nil, nil
	})
	transport := ntlmRoundTripper{base: base, username: "user", password: "secret"}
	request, err := http.NewRequest(http.MethodPost, "http://windows.example/wsman", strings.NewReader("soap"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transport.RoundTrip(request); err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("HTTP request was not rejected: %v", err)
	}

	request, err = http.NewRequest(http.MethodPost, "https://windows.example/wsman", io.NopCloser(strings.NewReader("soap")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transport.RoundTrip(request); err == nil || !strings.Contains(err.Error(), "replayable") {
		t.Fatalf("non-replayable body was not rejected: %v", err)
	}
}

func TestNTLMTransportBoundsAuthenticationResponseBodies(t *testing.T) {
	base := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		response := authResponse(request, http.StatusUnauthorized, "NTLM")
		response.Body = io.NopCloser(strings.NewReader(strings.Repeat("x", maxAuthDrainBytes+1)))
		return response, nil
	})
	transport := ntlmRoundTripper{base: base, username: "user", password: "secret"}
	request, err := http.NewRequest(http.MethodPost, "https://windows.example/wsman", bytes.NewReader([]byte("soap")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transport.RoundTrip(request); err == nil || !strings.Contains(err.Error(), "exceeded") {
		t.Fatalf("oversized authentication body was not rejected: %v", err)
	}
}

func TestNTLMChallengeParsingAndHTTP2Disablement(t *testing.T) {
	header := http.Header{"Www-Authenticate": {`Basic realm="windows", Negotiate`}}
	scheme, token, offered, err := ntlmChallenge(header)
	if err != nil || !offered || scheme != "Negotiate" || token != nil {
		t.Fatalf("unexpected parsed challenge: scheme=%q token=%x offered=%t err=%v", scheme, token, offered, err)
	}
	header.Set("WWW-Authenticate", "Negotiate, NTLM")
	scheme, token, offered, err = ntlmChallenge(header)
	if err != nil || !offered || scheme != "NTLM" || token != nil {
		t.Fatalf("explicit NTLM was not preferred: scheme=%q token=%x offered=%t err=%v", scheme, token, offered, err)
	}

	base := &http.Transport{ForceAttemptHTTP2: true, MaxResponseHeaderBytes: 1 << 20}
	prepared, ok := ntlmBaseTransport(base).(*http.Transport)
	if !ok || prepared.ForceAttemptHTTP2 || prepared.TLSNextProto == nil || prepared.MaxResponseHeaderBytes != maxAuthResponseHeader {
		t.Fatalf("NTLM base transport was not hardened: %#v", prepared)
	}
	if !base.ForceAttemptHTTP2 || base.MaxResponseHeaderBytes != 1<<20 {
		t.Fatal("NTLM transport mutated the caller's base transport")
	}
}

type ntlmHandshakeTransport struct {
	t              *testing.T
	calls          int
	authorizations []string
}

func (transport *ntlmHandshakeTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.t.Helper()
	body, err := io.ReadAll(request.Body)
	if err != nil {
		transport.t.Fatal(err)
	}
	_ = request.Body.Close()
	if string(body) != "soap-body" {
		transport.t.Fatalf("replayed body %q, want soap-body", body)
	}
	authorization := request.Header.Get("Authorization")
	transport.authorizations = append(transport.authorizations, authorization)
	transport.calls++
	switch transport.calls {
	case 1:
		if authorization != "" {
			transport.t.Fatalf("initial request sent authorization %q", authorization)
		}
		return authResponse(request, http.StatusUnauthorized, "Negotiate"), nil
	case 2:
		assertNTLMMessageType(transport.t, authorization, "Negotiate", 1)
		challenge := base64.StdEncoding.EncodeToString(testNTLMChallenge())
		return authResponse(request, http.StatusUnauthorized, "Negotiate "+challenge), nil
	case 3:
		assertNTLMMessageType(transport.t, authorization, "Negotiate", 3)
		return authResponse(request, http.StatusOK, ""), nil
	default:
		transport.t.Fatalf("unexpected NTLM request %d", transport.calls)
		return nil, nil
	}
}

type challengeSequenceTransport struct{ calls int }

func (transport *challengeSequenceTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.calls++
	if request.Body != nil {
		_ = request.Body.Close()
	}
	if transport.calls == 1 {
		return authResponse(request, http.StatusUnauthorized, "Negotiate"), nil
	}
	return authResponse(request, http.StatusUnauthorized, "Negotiate !!!"), nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Body != nil {
		defer request.Body.Close()
	}
	return function(request)
}

func authResponse(request *http.Request, status int, challenge string) *http.Response {
	header := http.Header{}
	if challenge != "" {
		header.Set("WWW-Authenticate", challenge)
	}
	return &http.Response{StatusCode: status, Status: http.StatusText(status), Header: header, Body: io.NopCloser(strings.NewReader("auth-response")), Request: request}
}

func assertNTLMMessageType(t *testing.T, authorization, scheme string, messageType uint32) {
	t.Helper()
	prefix := scheme + " "
	if !strings.HasPrefix(authorization, prefix) {
		t.Fatalf("authorization %q does not use %s", authorization, scheme)
	}
	message, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(authorization, prefix))
	if err != nil {
		t.Fatal(err)
	}
	if len(message) < 12 || string(message[:8]) != "NTLMSSP\x00" || binary.LittleEndian.Uint32(message[8:12]) != messageType {
		t.Fatalf("unexpected NTLM message type: %x", message)
	}
}

func testNTLMChallenge() []byte {
	challenge := make([]byte, 48)
	copy(challenge[:8], "NTLMSSP\x00")
	binary.LittleEndian.PutUint32(challenge[8:12], 2)
	binary.LittleEndian.PutUint32(challenge[20:24], 1<<0|1<<9|1<<15|1<<19)
	copy(challenge[24:32], []byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef})
	return challenge
}
