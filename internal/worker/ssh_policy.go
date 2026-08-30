package worker

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

const (
	maxSSHAllowlistBytes  = 64 << 10
	maxSSHKnownHostsBytes = 4 << 20
	maxSSHAllowlistCIDRs  = 256
	maxSSHTargets         = 1024
)

var sshUsername = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

type SSHStartupConfig struct {
	Allowlist        []netip.Prefix
	KnownHostsDigest string
	HostKeyCallback  ssh.HostKeyCallback
}

// LoadSSHStartupConfig reads deployment-owned trust and target authorization
// once at startup. The worker never rewrites either file.
func LoadSSHStartupConfig(allowlistPath, knownHostsPath string) (SSHStartupConfig, error) {
	allowlistBody, err := readBoundedRegularFile(allowlistPath, maxSSHAllowlistBytes, "SSH target allowlist")
	if err != nil {
		return SSHStartupConfig{}, err
	}
	allowlist, err := parseSSHAllowlist(allowlistBody)
	if err != nil {
		return SSHStartupConfig{}, err
	}
	knownHostsBody, err := readBoundedRegularFile(knownHostsPath, maxSSHKnownHostsBytes, "SSH known_hosts")
	if err != nil {
		return SSHStartupConfig{}, err
	}
	callback, err := knownhosts.New(knownHostsPath)
	if err != nil {
		return SSHStartupConfig{}, fmt.Errorf("load SSH known_hosts: %w", err)
	}
	digest := sha256.Sum256(knownHostsBody)
	return SSHStartupConfig{
		Allowlist:        allowlist,
		KnownHostsDigest: hex.EncodeToString(digest[:]),
		HostKeyCallback:  callback,
	}, nil
}

func readBoundedRegularFile(path string, maximum int64, label string) ([]byte, error) {
	if path == "" || !filepath.IsAbs(path) {
		return nil, fmt.Errorf("%s path must be absolute", label)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", label, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect %s: %w", label, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s must be a regular file", label)
	}
	body, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", label, err)
	}
	if int64(len(body)) > maximum {
		return nil, fmt.Errorf("%s exceeds %d bytes", label, maximum)
	}
	return body, nil
}

func parseSSHAllowlist(body []byte) ([]netip.Prefix, error) {
	scanner := bufio.NewScanner(strings.NewReader(string(body)))
	scanner.Buffer(make([]byte, 1024), maxSSHAllowlistBytes)
	seen := map[string]struct{}{}
	values := make([]string, 0)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		prefix, err := netip.ParsePrefix(line)
		if err != nil || !prefix.Addr().Is4() || prefix.Addr().Zone() != "" || prefix != prefix.Masked() || prefix.String() != line {
			return nil, fmt.Errorf("SSH target allowlist entry %q must be a canonical IPv4 CIDR", line)
		}
		if _, ok := seen[line]; ok {
			continue
		}
		seen[line] = struct{}{}
		values = append(values, line)
		if len(values) > maxSSHAllowlistCIDRs {
			return nil, fmt.Errorf("SSH target allowlist contains more than %d CIDRs", maxSSHAllowlistCIDRs)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read SSH target allowlist: %w", err)
	}
	if len(values) == 0 {
		return nil, errors.New("SSH target allowlist contains no CIDRs")
	}
	sort.Strings(values)
	prefixes := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		prefix, _ := netip.ParsePrefix(value)
		prefixes = append(prefixes, prefix)
	}
	return prefixes, nil
}

func safeSSHUsername(value string) bool { return sshUsername.MatchString(value) }

func targetAllowed(target netip.Addr, allowlist []netip.Prefix) bool {
	if !target.Is4() || target.Zone() != "" {
		return false
	}
	for _, prefix := range allowlist {
		if prefix.Contains(target) {
			return true
		}
	}
	return false
}
