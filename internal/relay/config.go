package relay

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	maxConfigBytes     = 1 << 20
	maxProfiles        = 128
	maxTargets         = 10_000
	maxTargetsBytes    = 4 << 20
	maxTargetLine      = 4 << 10
	defaultConcurrency = 32
)

var safeID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// FileConfig is the non-secret Relay definition stored on the Relay host.
// Credential fields contain references only; the referenced values are
// resolved afresh when a job starts.
type FileConfig struct {
	SchemaVersion string          `json:"schema_version"`
	RelayID       string          `json:"relay_id"`
	SiteID        string          `json:"site_id"`
	Profiles      []ProfileConfig `json:"profiles"`
}

type ProfileConfig struct {
	ID          string     `json:"id"`
	Plugin      string     `json:"plugin"`
	TargetsFile string     `json:"targets_file,omitempty"`
	SSH         *SSHConfig `json:"ssh,omitempty"`
}

type SSHConfig struct {
	PasswordRef    string `json:"password_ref,omitempty"`
	PrivateKeyRef  string `json:"private_key_ref,omitempty"`
	KnownHostsFile string `json:"known_hosts_file"`
	Concurrency    int    `json:"concurrency,omitempty"`
	ConnectTimeout string `json:"connect_timeout,omitempty"`
	CommandTimeout string `json:"command_timeout,omitempty"`
	MaxOutputBytes int64  `json:"max_output_bytes,omitempty"`
}

func LoadConfig(path string) (FileConfig, error) {
	if !filepath.IsAbs(path) {
		return FileConfig{}, errors.New("relay config path must be absolute")
	}
	file, err := os.Open(path)
	if err != nil {
		return FileConfig{}, fmt.Errorf("open relay config: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return FileConfig{}, fmt.Errorf("inspect relay config: %w", err)
	}
	if !info.Mode().IsRegular() {
		return FileConfig{}, errors.New("relay config must be a regular file")
	}
	body, err := io.ReadAll(io.LimitReader(file, maxConfigBytes+1))
	if err != nil {
		return FileConfig{}, fmt.Errorf("read relay config: %w", err)
	}
	if len(body) > maxConfigBytes {
		return FileConfig{}, fmt.Errorf("relay config exceeds %d bytes", maxConfigBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var config FileConfig
	if err := decoder.Decode(&config); err != nil {
		return FileConfig{}, fmt.Errorf("decode relay config: %w", err)
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return FileConfig{}, errors.New("decode relay config: multiple JSON values")
	}
	if err := config.Validate(); err != nil {
		return FileConfig{}, err
	}
	return config, nil
}

func (c FileConfig) Validate() error {
	if c.SchemaVersion != ContractVersion {
		return fmt.Errorf("relay config schema_version must be %q", ContractVersion)
	}
	if !safeID.MatchString(c.RelayID) {
		return errors.New("relay_id must use 1-128 letters, digits, dots, underscores, or hyphens")
	}
	if !safeID.MatchString(c.SiteID) {
		return errors.New("site_id must use 1-128 letters, digits, dots, underscores, or hyphens")
	}
	if len(c.Profiles) == 0 || len(c.Profiles) > maxProfiles {
		return fmt.Errorf("relay config must contain between 1 and %d profiles", maxProfiles)
	}
	seen := make(map[string]struct{}, len(c.Profiles))
	for index := range c.Profiles {
		profile := &c.Profiles[index]
		if !safeID.MatchString(profile.ID) {
			return fmt.Errorf("profile %d id must use 1-128 letters, digits, dots, underscores, or hyphens", index)
		}
		if _, exists := seen[profile.ID]; exists {
			return fmt.Errorf("duplicate relay profile %q", profile.ID)
		}
		seen[profile.ID] = struct{}{}
		if err := profile.validate(); err != nil {
			return fmt.Errorf("profile %q: %w", profile.ID, err)
		}
	}
	return nil
}

func (p ProfileConfig) validate() error {
	switch p.Plugin {
	case "local":
		if p.TargetsFile != "" || p.SSH != nil {
			return errors.New("local profile must not configure targets_file or ssh")
		}
	case "ssh-linux":
		if !filepath.IsAbs(p.TargetsFile) {
			return errors.New("ssh-linux targets_file must be absolute")
		}
		if p.SSH == nil {
			return errors.New("ssh-linux profile requires ssh configuration")
		}
		if !filepath.IsAbs(p.SSH.KnownHostsFile) {
			return errors.New("ssh known_hosts_file must be absolute")
		}
		if p.SSH.PasswordRef == "" && p.SSH.PrivateKeyRef == "" {
			return errors.New("ssh profile requires password_ref or private_key_ref")
		}
		if p.SSH.Concurrency < 0 || p.SSH.Concurrency > 1024 {
			return errors.New("ssh concurrency must be between 0 and 1024")
		}
		if p.SSH.MaxOutputBytes < 0 || p.SSH.MaxOutputBytes > 64<<20 {
			return errors.New("ssh max_output_bytes must be between 0 and 67108864")
		}
		if _, err := parseDuration(p.SSH.ConnectTimeout, 10*time.Second); err != nil {
			return fmt.Errorf("connect_timeout: %w", err)
		}
		if _, err := parseDuration(p.SSH.CommandTimeout, 10*time.Second); err != nil {
			return fmt.Errorf("command_timeout: %w", err)
		}
	default:
		return fmt.Errorf("unsupported plugin %q", p.Plugin)
	}
	return nil
}

func (c FileConfig) Capabilities() []ProfileCapability {
	capabilities := make([]ProfileCapability, 0, len(c.Profiles))
	for _, profile := range c.Profiles {
		capabilities = append(capabilities, ProfileCapability{ID: profile.ID, Plugin: profile.Plugin})
	}
	return capabilities
}

func (c FileConfig) Profile(id string) (ProfileConfig, bool) {
	for _, profile := range c.Profiles {
		if profile.ID == id {
			return profile, true
		}
	}
	return ProfileConfig{}, false
}

func parseDuration(raw string, fallback time.Duration) (time.Duration, error) {
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 || value > 10*time.Minute {
		return 0, errors.New("must be a positive Go duration no greater than 10m")
	}
	return value, nil
}

func readTargets(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open targets file: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect targets file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("targets file must be a regular file")
	}
	body, err := io.ReadAll(io.LimitReader(file, maxTargetsBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read targets file: %w", err)
	}
	if len(body) > maxTargetsBytes {
		return nil, fmt.Errorf("targets file exceeds %d bytes", maxTargetsBytes)
	}
	lines := strings.Split(string(body), "\n")
	targets := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if len(line) > maxTargetLine {
			return nil, fmt.Errorf("target line exceeds %d bytes", maxTargetLine)
		}
		targets = append(targets, line)
		if len(targets) > maxTargets {
			return nil, fmt.Errorf("targets file contains more than %d targets", maxTargets)
		}
	}
	if len(targets) == 0 {
		return nil, errors.New("targets file contains no targets")
	}
	return targets, nil
}
