package mid

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const (
	journalVersion  = 1
	maxJournalBytes = 8 << 20
)

var ErrMIDLocked = errors.New("another local process already owns this MID identity")

type journalEntry struct {
	Version       int     `json:"version"`
	RecordID      string  `json:"record_id"`
	RecordDigest  string  `json:"record_digest"`
	Result        *Record `json:"result,omitempty"`
	ResponseSysID string  `json:"response_sys_id,omitempty"`
}

// State owns the local exclusive lock and append-only crash journal for one
// configured MID identity. The OS releases the lock after a crash; the
// journal remains for the next process to resume.
type State struct {
	dir         string
	journalPath string
	lockFile    *os.File
}

func OpenState(stateDir, midName string) (*State, error) {
	if !filepath.IsAbs(stateDir) {
		return nil, errors.New("MID state directory must be an absolute path")
	}
	if _, err := AgentName(midName); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, fmt.Errorf("create MID state directory: %w", err)
	}
	info, err := os.Lstat(stateDir)
	if err != nil {
		return nil, fmt.Errorf("inspect MID state directory: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("MID state directory must be a real directory, not a symlink")
	}
	if err := os.Chmod(stateDir, 0o700); err != nil {
		return nil, fmt.Errorf("restrict MID state directory permissions: %w", err)
	}
	digest := sha256.Sum256([]byte(midName))
	key := hex.EncodeToString(digest[:16])
	lockPath := filepath.Join(stateDir, "mid-"+key+".lock")
	lockHandle, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open MID identity lock: %w", err)
	}
	if err := os.Chmod(lockPath, 0o600); err != nil {
		lockHandle.Close()
		return nil, fmt.Errorf("restrict MID identity lock permissions: %w", err)
	}
	if err := lockPlatformFile(lockHandle); err != nil {
		lockHandle.Close()
		return nil, err
	}
	return &State{
		dir:         stateDir,
		journalPath: filepath.Join(stateDir, "mid-"+key+".claim.jsonl"),
		lockFile:    lockHandle,
	}, nil
}

func (s *State) Close() error {
	if s == nil || s.lockFile == nil {
		return nil
	}
	unlockErr := unlockPlatformFile(s.lockFile)
	closeErr := s.lockFile.Close()
	s.lockFile = nil
	return errors.Join(unlockErr, closeErr)
}

func (s *State) Load() (*journalEntry, error) {
	file, err := os.Open(s.journalPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open MID claim journal: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect MID claim journal: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("MID claim journal is not a regular file")
	}
	if info.Size() > maxJournalBytes {
		return nil, fmt.Errorf("MID claim journal exceeds %d bytes", maxJournalBytes)
	}
	reader := bufio.NewReader(io.LimitReader(file, maxJournalBytes+1))
	var last *journalEntry
	lineNumber := 0
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) != 0 {
			lineNumber++
			var entry journalEntry
			decoder := json.NewDecoder(bytes.NewReader(line))
			decoder.DisallowUnknownFields()
			decodeErr := decoder.Decode(&entry)
			if decodeErr != nil || decoder.Decode(&struct{}{}) != io.EOF {
				if errors.Is(readErr, io.EOF) {
					break // ignore only a final partial append after a crash
				}
				return nil, fmt.Errorf("decode MID claim journal line %d", lineNumber)
			}
			if err := validateJournalEntry(entry); err != nil {
				return nil, fmt.Errorf("validate MID claim journal line %d: %w", lineNumber, err)
			}
			last = &entry
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("read MID claim journal: %w", readErr)
		}
	}
	if last == nil {
		return nil, errors.New("MID claim journal contains no complete entry")
	}
	return last, nil
}

func (s *State) Save(entry journalEntry) error {
	if err := validateJournalEntry(entry); err != nil {
		return err
	}
	encoded, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("encode MID claim journal: %w", err)
	}
	if len(encoded)+1 > maxECCRecordJSONBytes {
		return errors.New("MID claim journal entry exceeds its bound")
	}
	file, err := os.OpenFile(s.journalPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open MID claim journal for append: %w", err)
	}
	defer file.Close()
	if err := os.Chmod(s.journalPath, 0o600); err != nil {
		return fmt.Errorf("restrict MID claim journal permissions: %w", err)
	}
	if _, err := file.Write(append(encoded, '\n')); err != nil {
		return fmt.Errorf("append MID claim journal: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync MID claim journal: %w", err)
	}
	return nil
}

func (s *State) Clear() error {
	err := os.Remove(s.journalPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("remove completed MID claim journal: %w", err)
	}
	if err := syncPlatformDirectory(s.dir); err != nil {
		return fmt.Errorf("sync MID state directory: %w", err)
	}
	return nil
}

func validateJournalEntry(entry journalEntry) error {
	if entry.Version != journalVersion {
		return fmt.Errorf("MID claim journal version must be %d", journalVersion)
	}
	if !sysIDPattern.MatchString(entry.RecordID) {
		return errors.New("MID claim journal record_id is invalid")
	}
	if len(entry.RecordDigest) != sha256.Size*2 {
		return errors.New("MID claim journal record_digest is invalid")
	}
	if _, err := hex.DecodeString(entry.RecordDigest); err != nil {
		return errors.New("MID claim journal record_digest is invalid")
	}
	if entry.ResponseSysID != "" && !sysIDPattern.MatchString(entry.ResponseSysID) {
		return errors.New("MID claim journal response_sys_id is invalid")
	}
	if entry.Result != nil {
		if err := validateInputRecord(*entry.Result, entry.Result.Agent); err != nil {
			return fmt.Errorf("MID claim journal result: %w", err)
		}
		if entry.Result.ResponseTo != entry.RecordID {
			return errors.New("MID claim journal result does not match record_id")
		}
	}
	return nil
}
