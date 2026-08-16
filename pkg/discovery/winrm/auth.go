package winrm

import (
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Azure/go-ntlmssp"
)

const (
	AuthModeNTLM          = "ntlm"
	maxNTLMTokenBytes     = 64 << 10
	maxAuthDrainBytes     = 64 << 10
	maxAuthResponseHeader = 64 << 10
)

// ntlmRoundTripper performs only NTLMv2 over the HTTP NTLM or Negotiate
// challenge schemes. It never sends Basic credentials and requires TLS even
// though configuration validation independently enforces HTTPS targets.
type ntlmRoundTripper struct {
	base               http.RoundTripper
	username, password string
}

func (transport ntlmRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL == nil || request.URL.Scheme != "https" {
		return nil, errors.New("NTLM authentication requires HTTPS")
	}
	if request.Header.Get("Authorization") != "" {
		return nil, errors.New("NTLM transport rejects caller-supplied authorization")
	}
	if request.Body != nil {
		defer request.Body.Close()
		if request.GetBody == nil {
			return nil, errors.New("NTLM authentication requires a replayable request body")
		}
	}
	username, domain, domainNeeded := ntlmssp.GetDomain(transport.username)

	response, err := transport.roundTrip(request, "")
	if err != nil || response.StatusCode != http.StatusUnauthorized {
		return response, err
	}
	scheme, challenge, offered, err := ntlmChallenge(response.Header)
	if err != nil {
		closeAuthResponse(response)
		return nil, err
	}
	if !offered {
		return response, nil
	}

	if len(challenge) == 0 {
		negotiate, err := ntlmssp.NewNegotiateMessage(domain, "")
		if err != nil {
			closeAuthResponse(response)
			return nil, fmt.Errorf("create NTLM negotiate message: %w", err)
		}
		if err := drainAuthResponse(response); err != nil {
			return nil, err
		}
		response, err = transport.roundTrip(request, scheme+" "+base64.StdEncoding.EncodeToString(negotiate))
		if err != nil {
			return nil, err
		}
		if response.StatusCode != http.StatusUnauthorized {
			return response, nil
		}
		scheme, challenge, offered, err = ntlmChallenge(response.Header)
		if err != nil {
			closeAuthResponse(response)
			return nil, err
		}
		if !offered || len(challenge) == 0 {
			return response, nil
		}
	}

	authenticate, err := ntlmssp.ProcessChallenge(challenge, username, transport.password, domainNeeded)
	if err != nil {
		closeAuthResponse(response)
		return nil, fmt.Errorf("process NTLM challenge: %w", err)
	}
	if err := drainAuthResponse(response); err != nil {
		return nil, err
	}
	return transport.roundTrip(request, scheme+" "+base64.StdEncoding.EncodeToString(authenticate))
}

func (transport ntlmRoundTripper) roundTrip(request *http.Request, authorization string) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.Header = request.Header.Clone()
	clone.Header.Del("Authorization")
	if authorization != "" {
		clone.Header.Set("Authorization", authorization)
	}
	if request.Body != nil {
		body, err := request.GetBody()
		if err != nil {
			return nil, fmt.Errorf("replay NTLM request body: %w", err)
		}
		clone.Body = body
	}
	response, err := transport.base.RoundTrip(clone)
	if err != nil {
		closeAuthResponse(response)
		return nil, err
	}
	if response == nil || response.Body == nil {
		closeAuthResponse(response)
		return nil, errors.New("NTLM base transport returned an invalid response")
	}
	return response, nil
}

func ntlmChallenge(header http.Header) (string, []byte, bool, error) {
	var negotiateToken []byte
	negotiateOffered := false
	for _, value := range header.Values("WWW-Authenticate") {
		for _, candidate := range strings.Split(value, ",") {
			fields := strings.Fields(candidate)
			if len(fields) == 0 || (!strings.EqualFold(fields[0], "NTLM") && !strings.EqualFold(fields[0], "Negotiate")) {
				continue
			}
			scheme := fields[0]
			var challenge []byte
			if len(fields) == 1 {
				challenge = nil
			} else {
				if len(fields) != 2 || len(fields[1]) > base64.StdEncoding.EncodedLen(maxNTLMTokenBytes) {
					return "", nil, false, errors.New("malformed or oversized NTLM challenge")
				}
				var err error
				challenge, err = base64.StdEncoding.DecodeString(fields[1])
				if err != nil || len(challenge) > maxNTLMTokenBytes {
					return "", nil, false, errors.New("malformed or oversized NTLM challenge")
				}
			}
			if strings.EqualFold(scheme, "NTLM") {
				return scheme, challenge, true, nil
			}
			negotiateOffered = true
			negotiateToken = challenge
		}
	}
	if negotiateOffered {
		return "Negotiate", negotiateToken, true, nil
	}
	return "", nil, false, nil
}

func closeAuthResponse(response *http.Response) {
	if response == nil || response.Body == nil {
		return
	}
	_, _ = io.CopyN(io.Discard, response.Body, maxAuthDrainBytes)
	_ = response.Body.Close()
}

func drainAuthResponse(response *http.Response) error {
	if response == nil || response.Body == nil {
		return errors.New("NTLM authentication response omitted a body")
	}
	written, err := io.CopyN(io.Discard, response.Body, maxAuthDrainBytes+1)
	_ = response.Body.Close()
	if written > maxAuthDrainBytes {
		return fmt.Errorf("NTLM authentication response exceeded %d bytes", maxAuthDrainBytes)
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("read NTLM authentication response: %w", err)
	}
	return nil
}

func ntlmBaseTransport(roundTripper http.RoundTripper) http.RoundTripper {
	if roundTripper == nil {
		roundTripper = http.DefaultTransport
	}
	transport, ok := roundTripper.(*http.Transport)
	if !ok {
		return roundTripper
	}
	clone := transport.Clone()
	clone.ForceAttemptHTTP2 = false
	clone.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
	if clone.MaxResponseHeaderBytes == 0 || clone.MaxResponseHeaderBytes > maxAuthResponseHeader {
		clone.MaxResponseHeaderBytes = maxAuthResponseHeader
	}
	return clone
}
