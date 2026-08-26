package mid

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestStateLocksOneLocalMIDIdentity(t *testing.T) {
	directory := t.TempDir()
	first, err := OpenState(directory, "topo-one")
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := OpenState(directory, "topo-one")
	if !errors.Is(err, ErrMIDLocked) {
		if second != nil {
			second.Close()
		}
		t.Fatalf("second lock error = %v", err)
	}
	other, err := OpenState(directory, "topo-two")
	if err != nil {
		t.Fatalf("different MID identity lock: %v", err)
	}
	if err := other.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestStateJournalRoundTripAndCrashTail(t *testing.T) {
	state, err := OpenState(t.TempDir(), "journal")
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	entry := journalEntry{
		Version:      journalVersion,
		RecordID:     "0123456789abcdef0123456789abcdef",
		RecordDigest: strings.Repeat("a", 64),
	}
	if err := state.Save(entry); err != nil {
		t.Fatal(err)
	}
	loaded, err := state.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded == nil || *loaded != entry {
		t.Fatalf("loaded = %#v, want %#v", loaded, entry)
	}
	file, err := os.OpenFile(state.journalPath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(`{"version":1,"record_id":`); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	loaded, err = state.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded == nil || loaded.RecordID != entry.RecordID {
		t.Fatalf("journal did not recover the last complete append: %#v", loaded)
	}
	if err := state.Clear(); err != nil {
		t.Fatal(err)
	}
	if loaded, err := state.Load(); err != nil || loaded != nil {
		t.Fatalf("cleared journal = %#v, %v", loaded, err)
	}
}

func TestStateRejectsRelativeAndSymlinkDirectories(t *testing.T) {
	if _, err := OpenState("relative", "mid"); err == nil {
		t.Fatal("relative state directory was accepted")
	}
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires privileges on some Windows runners")
	}
	root := t.TempDir()
	realDirectory := filepath.Join(root, "real")
	if err := os.Mkdir(realDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(realDirectory, link); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenState(link, "mid"); err == nil {
		t.Fatal("symlink state directory was accepted")
	}
}
