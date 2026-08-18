// Package agent implements the outbound-only Topo Agent run loop: periodic
// local discovery, delivery to a Topo Hub controller, and encrypted local
// buffering while the controller is unreachable.
package agent

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Nischoy-ai/topo/pkg/model"
)

const (
	spoolKeyBytes = 32 // AES-256
	spoolSuffix   = ".spool"
	tmpPrefix     = ".tmp-"

	// maxSpoolFileBytes bounds a single decrypted spool entry, matching the
	// resolved-credential bound used elsewhere in this project.
	maxSpoolFileBytes = 8 << 20
)

// ErrSpoolFull indicates the offline buffer has reached its configured byte
// bound. The caller decides how to handle the observation that did not fit;
// Spool never grows without limit.
var ErrSpoolFull = errors.New("spool exceeds configured byte bound")

// Spool is a bounded, AES-256-GCM-encrypted on-disk queue of observation
// envelopes, used while the controller is unreachable. Entries are read back
// in the order they were written.
type Spool struct {
	dir      string
	aead     cipher.AEAD
	maxBytes int64
	mu       sync.Mutex
	seq      uint64
}

// NewSpool opens (creating if necessary) a spool rooted at dir, encrypted
// with key. key must be exactly 32 bytes (AES-256); generate one with
// `openssl rand -hex 32` and decode it before calling NewSpool. dir must be
// an absolute path so agent behavior does not depend on the working
// directory.
func NewSpool(dir string, key []byte, maxBytes int64) (*Spool, error) {
	if !filepath.IsAbs(dir) {
		return nil, errors.New("spool directory must be an absolute path")
	}
	if len(key) != spoolKeyBytes {
		return nil, fmt.Errorf("spool encryption key must be exactly %d bytes (AES-256)", spoolKeyBytes)
	}
	if maxBytes <= 0 {
		return nil, errors.New("spool max bytes must be positive")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("initialize spool cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("initialize spool cipher: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create spool directory: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, fmt.Errorf("restrict spool directory permissions: %w", err)
	}
	return &Spool{dir: dir, aead: aead, maxBytes: maxBytes}, nil
}

// Enqueue encrypts and durably writes one observation envelope. It returns
// ErrSpoolFull without writing if doing so would exceed the configured byte
// bound.
func (s *Spool) Enqueue(e model.ObservationEnvelope) error {
	plaintext, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("marshal observation for spool: %w", err)
	}

	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return fmt.Errorf("generate spool nonce: %w", err)
	}
	ciphertext := s.aead.Seal(nonce, nonce, plaintext, nil)

	s.mu.Lock()
	defer s.mu.Unlock()

	used, err := s.totalBytesLocked()
	if err != nil {
		return fmt.Errorf("measure spool size: %w", err)
	}
	if used+int64(len(ciphertext)) > s.maxBytes {
		return ErrSpoolFull
	}

	s.seq++
	name := fmt.Sprintf("%020d-%08d%s", time.Now().UnixNano(), s.seq, spoolSuffix)
	tmpPath := filepath.Join(s.dir, tmpPrefix+name)
	finalPath := filepath.Join(s.dir, name)
	if err := os.WriteFile(tmpPath, ciphertext, 0o600); err != nil {
		return fmt.Errorf("write spool entry: %w", err)
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("commit spool entry: %w", err)
	}
	return nil
}

// Pending lists queued entry names in the order they were written.
func (s *Spool) Pending() ([]string, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("list spool directory: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), tmpPrefix) || !strings.HasSuffix(entry.Name(), spoolSuffix) {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names, nil
}

// Read decrypts and decodes one spool entry by the name returned from
// Pending. A tampered or corrupted entry returns a clear error rather than
// silently returning altered data.
func (s *Spool) Read(name string) (model.ObservationEnvelope, error) {
	path, err := s.entryPath(name)
	if err != nil {
		return model.ObservationEnvelope{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		return model.ObservationEnvelope{}, fmt.Errorf("open spool entry %q: %w", name, err)
	}
	defer file.Close()

	ciphertext, err := io.ReadAll(io.LimitReader(file, maxSpoolFileBytes+1))
	if err != nil {
		return model.ObservationEnvelope{}, fmt.Errorf("read spool entry %q: %w", name, err)
	}
	if len(ciphertext) > maxSpoolFileBytes {
		return model.ObservationEnvelope{}, fmt.Errorf("spool entry %q exceeds %d bytes", name, maxSpoolFileBytes)
	}
	nonceSize := s.aead.NonceSize()
	if len(ciphertext) < nonceSize {
		return model.ObservationEnvelope{}, fmt.Errorf("spool entry %q is truncated", name)
	}
	nonce, sealed := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := s.aead.Open(nil, nonce, sealed, nil)
	if err != nil {
		return model.ObservationEnvelope{}, fmt.Errorf("spool entry %q failed integrity verification: %w", name, err)
	}
	var envelope model.ObservationEnvelope
	if err := json.Unmarshal(plaintext, &envelope); err != nil {
		return model.ObservationEnvelope{}, fmt.Errorf("decode spool entry %q: %w", name, err)
	}
	return envelope, nil
}

// Remove deletes one spool entry after it has been delivered or dropped.
func (s *Spool) Remove(name string) error {
	path, err := s.entryPath(name)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove spool entry %q: %w", name, err)
	}
	return nil
}

func (s *Spool) entryPath(name string) (string, error) {
	if strings.ContainsAny(name, "/\\") || !strings.HasSuffix(name, spoolSuffix) {
		return "", fmt.Errorf("invalid spool entry name %q", name)
	}
	return filepath.Join(s.dir, name), nil
}

func (s *Spool) totalBytesLocked() (int64, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		total += info.Size()
	}
	return total, nil
}
