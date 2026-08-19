package enrollment

import (
	"context"
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
