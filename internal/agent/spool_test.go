package agent

import (
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Nischoy-ai/topo/pkg/model"
)

func testKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, spoolKeyBytes)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	return key
}

func testEnvelope(id string) model.ObservationEnvelope {
	return model.ObservationEnvelope{
		SchemaVersion: model.SchemaVersion,
		ObservationID: id,
		SiteID:        "default",
		CollectorID:   "test-collector",
		Plugin:        "local-host",
		ObservedAt:    time.Now().UTC(),
		Assets:        []model.Asset{{Type: model.AssetHost, NativeID: "host-" + id}},
	}
}

func TestSpoolEnqueueReadRoundTrip(t *testing.T) {
	spool, err := NewSpool(t.TempDir(), testKey(t), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	envelope := testEnvelope("obs-1")
	if err := spool.Enqueue(envelope); err != nil {
		t.Fatal(err)
	}

	pending, err := spool.Pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("pending = %v", pending)
	}

	got, err := spool.Read(pending[0])
	if err != nil {
		t.Fatal(err)
	}
	if got.ObservationID != envelope.ObservationID {
		t.Fatalf("observation id = %q, want %q", got.ObservationID, envelope.ObservationID)
	}

	if err := spool.Remove(pending[0]); err != nil {
		t.Fatal(err)
	}
	pending, err = spool.Pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("pending after remove = %v", pending)
	}
}

func TestSpoolPreservesOrder(t *testing.T) {
	spool, err := NewSpool(t.TempDir(), testKey(t), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if err := spool.Enqueue(testEnvelope(string(rune('a' + i)))); err != nil {
			t.Fatal(err)
		}
	}
	pending, err := spool.Pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 5 {
		t.Fatalf("pending = %v", pending)
	}
	for i, name := range pending {
		envelope, err := spool.Read(name)
		if err != nil {
			t.Fatal(err)
		}
		want := "host-" + string(rune('a'+i))
		if envelope.Assets[0].NativeID != want {
			t.Fatalf("entry %d native id = %q, want %q", i, envelope.Assets[0].NativeID, want)
		}
	}
}

func TestSpoolDetectsTampering(t *testing.T) {
	dir := t.TempDir()
	spool, err := NewSpool(dir, testKey(t), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if err := spool.Enqueue(testEnvelope("obs-1")); err != nil {
		t.Fatal(err)
	}
	pending, err := spool.Pending()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, pending[0])
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)-1] ^= 0xFF
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := spool.Read(pending[0]); err == nil {
		t.Fatal("expected integrity verification failure")
	}
}

func TestSpoolEnforcesByteBound(t *testing.T) {
	spool, err := NewSpool(t.TempDir(), testKey(t), 200)
	if err != nil {
		t.Fatal(err)
	}
	var lastErr error
	for i := 0; i < 50; i++ {
		lastErr = spool.Enqueue(testEnvelope("obs"))
		if lastErr != nil {
			break
		}
	}
	if lastErr == nil {
		t.Fatal("expected the spool to eventually report ErrSpoolFull")
	}
	if lastErr != ErrSpoolFull {
		t.Fatalf("error = %v, want ErrSpoolFull", lastErr)
	}
}

func TestNewSpoolRejectsBadInputs(t *testing.T) {
	if _, err := NewSpool("relative", testKey(t), 1<<20); err == nil {
		t.Fatal("expected relative directory to be rejected")
	}
	if _, err := NewSpool(t.TempDir(), []byte("too-short"), 1<<20); err == nil {
		t.Fatal("expected short key to be rejected")
	}
	if _, err := NewSpool(t.TempDir(), testKey(t), 0); err == nil {
		t.Fatal("expected non-positive max bytes to be rejected")
	}
}

func TestSpoolReadRejectsPathTraversal(t *testing.T) {
	spool, err := NewSpool(t.TempDir(), testKey(t), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := spool.Read("../escape.spool"); err == nil {
		t.Fatal("expected path traversal entry name to be rejected")
	}
}
