package relay

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSpoolEncryptsReplacesAndRemovesDelivery(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	spool, err := NewSpool(dir, make([]byte, 32), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	delivery := PendingDelivery{Job: Job{JobID: "job-1", ProfileID: "local"}, RelayID: "relay", StartedAt: time.Now()}
	if err := spool.Enqueue(delivery); err != nil {
		t.Fatal(err)
	}
	names, err := spool.Pending()
	if err != nil || len(names) != 1 {
		t.Fatalf("Pending() = %#v, %v", names, err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, names[0]))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) == "" || contains(raw, []byte("job-1")) {
		t.Fatal("spool entry exposes plaintext")
	}
	decoded, err := spool.Read(names[0])
	if err != nil || decoded.Job.JobID != "job-1" {
		t.Fatalf("Read() = %#v, %v", decoded, err)
	}
	decoded.Published = true
	if err := spool.Replace(names[0], decoded); err != nil {
		t.Fatal(err)
	}
	decoded, err = spool.Read(names[0])
	if err != nil || !decoded.Published {
		t.Fatalf("replaced Read() = %#v, %v", decoded, err)
	}
	if err := spool.Remove(names[0]); err != nil {
		t.Fatal(err)
	}
	if names, _ := spool.Pending(); len(names) != 0 {
		t.Fatalf("pending after remove = %#v", names)
	}
}

func TestSpoolDetectsTampering(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	spool, err := NewSpool(dir, make([]byte, 32), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if err := spool.Enqueue(PendingDelivery{Job: Job{JobID: "job"}, RelayID: "relay"}); err != nil {
		t.Fatal(err)
	}
	names, _ := spool.Pending()
	path := filepath.Join(dir, names[0])
	raw, _ := os.ReadFile(path)
	raw[len(raw)-1] ^= 0xff
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := spool.Read(names[0]); err == nil {
		t.Fatal("Read accepted tampered entry")
	}
}

func contains(haystack, needle []byte) bool {
	for index := 0; index+len(needle) <= len(haystack); index++ {
		match := true
		for offset := range needle {
			if haystack[index+offset] != needle[offset] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
