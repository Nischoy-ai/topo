package relay

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
	relaySpoolKeyBytes = 32
	relaySpoolSuffix   = ".relay-spool"
	relaySpoolTmp      = ".tmp-relay-"
	maxRelayEntryBytes = 16 << 20
	maxRelayFileBytes  = maxRelayEntryBytes + 64
)

var ErrSpoolFull = errors.New("relay spool exceeds configured byte bound")

// PendingDelivery is durably retained before the first IRE request. Published
// is itself persisted before the job result is reported, preventing an
// uncertain result-report retry from needlessly publishing the same payload
// again (though IRE source identity also makes such repetition idempotent).
type PendingDelivery struct {
	Job              Job                        `json:"job"`
	RelayID          string                     `json:"relay_id"`
	StartedAt        time.Time                  `json:"started_at"`
	CompletedAt      time.Time                  `json:"completed_at"`
	Observation      *model.ObservationEnvelope `json:"observation,omitempty"`
	DiscoveryError   string                     `json:"discovery_error,omitempty"`
	Published        bool                       `json:"published,omitempty"`
	PublishAttempts  int                        `json:"publish_attempts,omitempty"`
	PublicationError string                     `json:"publication_error,omitempty"`
}

type Spool struct {
	dir      string
	aead     cipher.AEAD
	maxBytes int64
	mu       sync.Mutex
	seq      uint64
}

func NewSpool(dir string, key []byte, maxBytes int64) (*Spool, error) {
	if !filepath.IsAbs(dir) {
		return nil, errors.New("relay spool directory must be absolute")
	}
	if len(key) != relaySpoolKeyBytes {
		return nil, fmt.Errorf("relay spool encryption key must be exactly %d bytes", relaySpoolKeyBytes)
	}
	if maxBytes <= 0 {
		return nil, errors.New("relay spool max bytes must be positive")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("initialize relay spool cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("initialize relay spool cipher: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create relay spool directory: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, fmt.Errorf("restrict relay spool directory permissions: %w", err)
	}
	return &Spool{dir: dir, aead: aead, maxBytes: maxBytes}, nil
}

func (s *Spool) Enqueue(delivery PendingDelivery) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	name := fmt.Sprintf("%020d-%08d%s", time.Now().UnixNano(), s.seq, relaySpoolSuffix)
	return s.writeLocked(name, delivery, 0)
}

// Replace atomically persists delivery's updated publication state.
func (s *Spool) Replace(name string, delivery PendingDelivery) error {
	path, err := s.entryPath(name)
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("inspect relay spool entry %q: %w", name, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writeLocked(name, delivery, info.Size())
}

func (s *Spool) writeLocked(name string, delivery PendingDelivery, replacedBytes int64) error {
	plaintext, err := json.Marshal(delivery)
	if err != nil {
		return fmt.Errorf("marshal relay delivery: %w", err)
	}
	if len(plaintext) > maxRelayEntryBytes {
		return fmt.Errorf("relay delivery exceeds %d bytes", maxRelayEntryBytes)
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return fmt.Errorf("generate relay spool nonce: %w", err)
	}
	ciphertext := s.aead.Seal(nonce, nonce, plaintext, nil)
	used, err := s.totalBytesLocked()
	if err != nil {
		return fmt.Errorf("measure relay spool size: %w", err)
	}
	if used-replacedBytes+int64(len(ciphertext)) > s.maxBytes {
		return ErrSpoolFull
	}
	tmpPath := filepath.Join(s.dir, relaySpoolTmp+name)
	finalPath := filepath.Join(s.dir, name)
	if err := os.WriteFile(tmpPath, ciphertext, 0o600); err != nil {
		return fmt.Errorf("write relay spool entry: %w", err)
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("commit relay spool entry: %w", err)
	}
	return nil
}

func (s *Spool) Pending() ([]string, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("list relay spool: %w", err)
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), relaySpoolSuffix) {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

func (s *Spool) Read(name string) (PendingDelivery, error) {
	path, err := s.entryPath(name)
	if err != nil {
		return PendingDelivery{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		return PendingDelivery{}, fmt.Errorf("open relay spool entry %q: %w", name, err)
	}
	defer file.Close()
	ciphertext, err := io.ReadAll(io.LimitReader(file, maxRelayFileBytes+1))
	if err != nil {
		return PendingDelivery{}, fmt.Errorf("read relay spool entry %q: %w", name, err)
	}
	if len(ciphertext) > maxRelayFileBytes {
		return PendingDelivery{}, fmt.Errorf("relay spool entry %q exceeds %d bytes", name, maxRelayFileBytes)
	}
	if len(ciphertext) < s.aead.NonceSize() {
		return PendingDelivery{}, fmt.Errorf("relay spool entry %q is truncated", name)
	}
	plaintext, err := s.aead.Open(nil, ciphertext[:s.aead.NonceSize()], ciphertext[s.aead.NonceSize():], nil)
	if err != nil {
		return PendingDelivery{}, fmt.Errorf("relay spool entry %q failed integrity verification: %w", name, err)
	}
	var delivery PendingDelivery
	if err := json.Unmarshal(plaintext, &delivery); err != nil {
		return PendingDelivery{}, fmt.Errorf("decode relay spool entry %q: %w", name, err)
	}
	return delivery, nil
}

func (s *Spool) Remove(name string) error {
	path, err := s.entryPath(name)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove relay spool entry %q: %w", name, err)
	}
	return nil
}

func (s *Spool) entryPath(name string) (string, error) {
	if strings.ContainsAny(name, "/\\") || !strings.HasSuffix(name, relaySpoolSuffix) {
		return "", fmt.Errorf("invalid relay spool entry name %q", name)
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
		if err == nil {
			total += info.Size()
		}
	}
	return total, nil
}
