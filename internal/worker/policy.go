package worker

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"sort"
	"time"
)

const DefaultMaxTaskDuration = 30 * time.Second

const (
	DefaultMaxConcurrency = 1
	MaxWorkerConcurrency  = 32
)

var safeID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// Policy is read-only deployment configuration. The worker never rewrites it
// or treats ServiceNow configuration as authority to expand it.
type Policy struct {
	WorkerPool       string
	SiteID           string
	AllowLocal       bool
	AllowSSHLinux    bool
	SSHAllowlist     []netip.Prefix
	SSHHostKeyDigest string
	MaxTaskDuration  time.Duration
	MaxConcurrency   int
}

func (p Policy) Validate() error {
	if !safeID.MatchString(p.WorkerPool) {
		return errors.New("worker pool must use 1-128 letters, digits, dots, underscores, or hyphens")
	}
	if !safeID.MatchString(p.SiteID) {
		return errors.New("site ID must use 1-128 letters, digits, dots, underscores, or hyphens")
	}
	if !p.AllowLocal && !p.AllowSSHLinux {
		return errors.New("worker policy must explicitly allow at least one compiled-in operation")
	}
	if p.AllowSSHLinux {
		if len(p.SSHAllowlist) == 0 || len(p.SSHAllowlist) > maxSSHAllowlistCIDRs {
			return fmt.Errorf("SSH target allowlist must contain 1-%d canonical IPv4 CIDRs", maxSSHAllowlistCIDRs)
		}
		for _, prefix := range p.SSHAllowlist {
			if !prefix.Addr().Is4() || prefix.Addr().Zone() != "" || prefix != prefix.Masked() {
				return errors.New("SSH target allowlist contains a noncanonical IPv4 CIDR")
			}
		}
		if len(p.SSHHostKeyDigest) != sha256HexLength {
			return errors.New("SSH known_hosts digest is invalid")
		}
		if _, err := hex.DecodeString(p.SSHHostKeyDigest); err != nil {
			return errors.New("SSH known_hosts digest is invalid")
		}
	} else if len(p.SSHAllowlist) != 0 || p.SSHHostKeyDigest != "" {
		return errors.New("SSH policy data requires explicit ssh_linux.v1 enablement")
	}
	if p.MaxTaskDuration == 0 {
		p.MaxTaskDuration = DefaultMaxTaskDuration
	}
	if p.MaxTaskDuration < time.Second || p.MaxTaskDuration > 10*time.Minute {
		return errors.New("maximum task duration must be between 1s and 10m")
	}
	if p.concurrency() < 1 || p.concurrency() > MaxWorkerConcurrency {
		return fmt.Errorf("maximum worker concurrency must be between 1 and %d", MaxWorkerConcurrency)
	}
	return nil
}

func (p Policy) concurrency() int {
	if p.MaxConcurrency == 0 {
		return DefaultMaxConcurrency
	}
	return p.MaxConcurrency
}

func (p Policy) taskDuration() time.Duration {
	if p.MaxTaskDuration == 0 {
		return DefaultMaxTaskDuration
	}
	return p.MaxTaskDuration
}

func (p Policy) Capabilities() []string {
	capabilities := make([]string, 0, 2)
	if p.AllowLocal {
		capabilities = append(capabilities, OperationLocalV1)
	}
	if p.AllowSSHLinux {
		capabilities = append(capabilities, OperationSSHLinuxV1)
	}
	return capabilities
}

func (p Policy) Digest() (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(struct {
		SchemaVersion    string   `json:"schema_version"`
		WorkerPool       string   `json:"worker_pool"`
		SiteID           string   `json:"site_id"`
		Operations       []string `json:"operations"`
		MaxTaskSeconds   int64    `json:"max_task_seconds"`
		MaxConcurrency   int      `json:"max_concurrency"`
		SSHAllowlist     []string `json:"ssh_allowlist,omitempty"`
		SSHHostKeyDigest string   `json:"ssh_host_key_digest,omitempty"`
	}{
		SchemaVersion:    ContractVersion,
		WorkerPool:       p.WorkerPool,
		SiteID:           p.SiteID,
		Operations:       p.Capabilities(),
		MaxTaskSeconds:   int64(p.taskDuration() / time.Second),
		MaxConcurrency:   p.concurrency(),
		SSHAllowlist:     canonicalPrefixStrings(p.SSHAllowlist),
		SSHHostKeyDigest: p.SSHHostKeyDigest,
	})
	if err != nil {
		return "", fmt.Errorf("encode worker policy: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func canonicalPrefixStrings(prefixes []netip.Prefix) []string {
	values := make([]string, 0, len(prefixes))
	for _, prefix := range prefixes {
		values = append(values, prefix.String())
	}
	sort.Strings(values)
	return values
}
