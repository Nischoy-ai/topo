package enrollment

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEnrollRejectsInvalidCollectorIDInThisPackage(t *testing.T) {
	if _, err := Enroll(context.Background(), "https://example.test", "token", "", nil); err == nil {
		t.Fatal("expected an empty collector ID to be rejected")
	}
}

func TestEnrollRejectsInvalidControllerURLInThisPackage(t *testing.T) {
	if _, err := Enroll(context.Background(), "not-a-url", "token", "collector-1", nil); err == nil {
		t.Fatal("expected a malformed controller URL to be rejected")
	}
}

// TestValidControllerURLRejectsUserinfoPathQueryFragment matches the
// stricter contract already used by pkg/credentialref/vault: Enroll and
// Rotate themselves append the fixed /v1/enroll and /v1/rotate paths, so
// controllerURL must be bare scheme+host. TSR-2026-004.
func TestValidControllerURLRejectsUserinfoPathQueryFragment(t *testing.T) {
	for _, controllerURL := range []string{
		"https://user:pass@example.test",
		"https://example.test/extra/path",
		"https://example.test/?query=1",
		"https://example.test/#frag",
	} {
		if err := validControllerURL(controllerURL); err == nil {
			t.Errorf("controllerURL %q: expected validation error, got nil", controllerURL)
		}
	}
}

// TestEnrollDoesNotFollowRedirect proves the one-time enrollment token is
// never replayed against a redirect target: Enroll's HTTP client must not
// follow a 302 from the configured controller. TSR-2026-004.
func TestEnrollDoesNotFollowRedirect(t *testing.T) {
	var redirectTargetHit bool
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redirected":
			redirectTargetHit = true
			w.WriteHeader(http.StatusOK)
		default:
			http.Redirect(w, r, server.URL+"/redirected", http.StatusFound)
		}
	}))
	defer server.Close()

	if _, err := Enroll(context.Background(), server.URL, "token", "collector-1", nil); err == nil {
		t.Fatal("expected an unfollowed redirect to surface as an enrollment error")
	}
	if redirectTargetHit {
		t.Fatal("redirect target must never be contacted")
	}
}

func TestRotateRejectsInvalidCollectorID(t *testing.T) {
	if _, err := Rotate(context.Background(), "https://example.test", nil, ""); err == nil {
		t.Fatal("expected an empty collector ID to be rejected")
	}
}

func TestRotateRejectsInvalidControllerURL(t *testing.T) {
	if _, err := Rotate(context.Background(), "not-a-url", nil, "collector-1"); err == nil {
		t.Fatal("expected a malformed controller URL to be rejected")
	}
}

func TestRotateRejectsNilTLSConfig(t *testing.T) {
	if _, err := Rotate(context.Background(), "https://example.test", nil, "collector-1"); err == nil {
		t.Fatal("expected a nil TLS config to be rejected")
	}
}
