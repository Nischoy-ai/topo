//go:build linux

package workersetup

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Nischoy-ai/topo/internal/worker"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"golang.org/x/sys/unix"
)

// Intentionally tied to the reviewed SSH-only package contract. Packaging
// changes must update this pin and its source-equality test together.
const unitSHA256 = "0114ebc575bfcf0b15d007cbdbbd1a048018815c2486a59c6ddc128a9b17d25b"
const binary = "/usr/bin/topo"

var policyFiles = []string{"topo-worker.env", "targets.allow", "known_hosts"}

type receipt struct {
	Version  int
	Settings settings
	Hashes   map[string]string
}
type linuxBackend struct {
	dir                 string
	uid, gid, sourceUID int
	ownerUID            int
	lock                *os.File
	stagedCheck         func(context.Context, settings, string) error
}

func platformBackend() (backend, error) { return &linuxBackend{dir: directory}, nil }

func (b *linuxBackend) Close() error {
	if b.lock != nil {
		return b.lock.Close()
	}
	return nil
}
func (b *linuxBackend) ready(ctx context.Context) error {
	if os.Geteuid() != 0 {
		return errors.New("worker setup requires root on packaged Linux")
	}
	if _, err := unix.IoctlGetTermios(int(os.Stdin.Fd()), unix.TCGETS); err != nil {
		return errors.New("worker setup requires an interactive terminal")
	}
	if _, err := os.Stat("/run/systemd/system"); err != nil {
		return errors.New("worker setup requires a running systemd system")
	}
	u, err := user.Lookup("topo-worker")
	if err != nil {
		return errors.New("packaged topo-worker service account is missing")
	}
	b.uid, err = strconv.Atoi(u.Uid)
	if err != nil || b.uid == 0 {
		return errors.New("invalid packaged service account")
	}
	b.gid, err = strconv.Atoi(u.Gid)
	if err != nil || b.gid == 0 {
		return errors.New("invalid packaged service group")
	}
	if id := os.Getenv("SUDO_UID"); id != "" {
		b.sourceUID, err = strconv.Atoi(id)
		if err != nil || b.sourceUID < 0 {
			return errors.New("invalid invoking user identity")
		}
	}
	// The lock lives under root-owned /run; no files in an existing deployment
	// are opened for writing, including on a failed or simultaneous invocation.
	fd, err := unix.Open("/run/topo-worker-setup.lock", unix.O_CREAT|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_RDWR, 0600)
	if err != nil {
		return errors.New("cannot open setup lock")
	}
	b.lock = os.NewFile(uintptr(fd), "setup-lock")
	if err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return errors.New("another worker setup is running")
	}
	if err = verifyOwned(b.lock, 0, -1, 0600); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := command(ctx, nil, "/usr/bin/systemctl", "show", "topo-worker.service", "--property=FragmentPath", "--property=DropInPaths", "--property=ActiveState")
	if err != nil {
		return errors.New("cannot inspect packaged worker service")
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if ok {
			values[k] = v
		}
	}
	unit := values["FragmentPath"]
	if (unit != "/usr/lib/systemd/system/topo-worker.service" && unit != "/lib/systemd/system/topo-worker.service") || values["DropInPaths"] != "" || (values["ActiveState"] != "inactive" && values["ActiveState"] != "failed") {
		return errors.New("setup requires the unmodified inactive packaged service without overrides; existing service preserved")
	}
	body, err := readSecure(unit, 64<<10, 0, false)
	if err != nil || digest(body) != unitSHA256 {
		return errors.New("packaged worker unit differs from the reviewed SSH-only contract")
	}
	f, err := os.Open(binary)
	if err != nil {
		return errors.New("packaged /usr/bin/topo is missing")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 {
		return errors.New("packaged worker binary is unsafe")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 {
		return errors.New("packaged worker binary must be root owned")
	}
	return nil
}
func verifyOwned(f *os.File, uid, gid int, mode os.FileMode) error {
	info, err := f.Stat()
	if err != nil {
		return errors.New("cannot inspect protected setup file")
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(st.Uid) != uid || (gid >= 0 && int(st.Gid) != gid) || info.Mode().Perm() != mode {
		return errors.New("unsafe setup file ownership or permissions; nothing rewritten")
	}
	return nil
}
func readSecure(path string, max int64, sourceUID int, secret bool) ([]byte, error) {
	if !filepath.IsAbs(path) || strings.ContainsAny(path, "\x00\r\n") {
		return nil, errors.New("input file path must be absolute")
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, errors.New("cannot open protected input file (symlinks are not accepted)")
	}
	f := os.NewFile(uintptr(fd), "setup-input")
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("input must be a bounded regular file")
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || (st.Uid != 0 && int(st.Uid) != sourceUID) || info.Mode().Perm()&0022 != 0 || (secret && info.Mode().Perm()&0077 != 0) {
		return nil, errors.New("input has unsafe owner/permissions; token source must be owner-only and policy files not group/world writable")
	}
	data, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil || len(data) == 0 || int64(len(data)) > max {
		return nil, errors.New("input file is empty, unreadable or exceeds its size bound")
	}
	return data, nil
}
func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func environment(s settings) []byte {
	return []byte(fmt.Sprintf("SERVICENOW_INSTANCE_URL=%s\nTOPO_WORKER_POOL=%s\nTOPO_WORKER_SITE=%s\nTOPO_WORKER_MAX_CONCURRENCY=2\n", s.Instance, s.Pool, s.Site))
}
func (b *linuxBackend) existing(ctx context.Context) (*settings, error) {
	info, err := os.Lstat(b.dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("existing configuration path is unsafe; preserved")
	}
	d, err := os.Open(b.dir)
	if err != nil {
		return nil, errors.New("cannot inspect existing configuration")
	}
	defer d.Close()
	if err = verifyOwned(d, b.ownerUID, b.gid, 0750); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(b.dir)
	if err != nil || len(entries) != 5 {
		return nil, errors.New("existing configuration is not an intact setup-created deployment; preserved")
	}
	body, err := b.saved("setup.json", 0600, 16<<10)
	if err != nil {
		return nil, err
	}
	var r receipt
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if dec.Decode(&r) != nil || r.Version != 1 || len(r.Hashes) != len(policyFiles) || validate(r.Settings) != nil {
		return nil, errors.New("existing setup receipt is invalid; preserved")
	}
	for _, name := range policyFiles {
		data, e := b.saved(name, 0640, 4<<20)
		if e != nil {
			return nil, e
		}
		if digest(data) != r.Hashes[name] {
			return nil, errors.New("existing policy changed; setup will not rewrite or activate it")
		}
	}
	env, _ := b.saved("topo-worker.env", 0640, 16<<10)
	if !bytes.Equal(env, environment(r.Settings)) {
		return nil, errors.New("existing service policy differs from setup receipt")
	}
	if _, err = b.saved("worker-token", 0640, 64<<10); err != nil {
		return nil, err
	}
	if err = validatePolicy(ctx, b.dir); err != nil {
		return nil, err
	}
	return &r.Settings, nil
}
func (b *linuxBackend) saved(name string, mode os.FileMode, max int64) ([]byte, error) {
	fd, err := unix.Open(filepath.Join(b.dir, name), unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, errors.New("saved configuration is incomplete or unsafe; preserved")
	}
	f := os.NewFile(uintptr(fd), name)
	defer f.Close()
	if err = verifyOwned(f, b.ownerUID, b.gid, mode); err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		return nil, errors.New("saved configuration must be regular files")
	}
	data, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil || len(data) == 0 || int64(len(data)) > max {
		return nil, errors.New("saved configuration exceeds bounds or is empty")
	}
	return data, nil
}
func (b *linuxBackend) install(ctx context.Context, s settings, i inputs) error {
	if validate(s) != nil {
		return errors.New("invalid setup settings")
	}
	token, err := readSecure(i.Token, 64<<10, b.sourceUID, true)
	if err != nil {
		return err
	}
	// Reuse the worker's real token/origin validation without any network I/O.
	if _, err = worker.NewClient(s.Instance, strings.TrimSpace(string(token)), nil); err != nil {
		return errors.New("invalid worker token or instance; no configuration saved")
	}
	targets, err := readSecure(i.Targets, 64<<10, b.sourceUID, false)
	if err != nil {
		return err
	}
	hosts, err := readSecure(i.Hosts, 4<<20, b.sourceUID, false)
	if err != nil {
		return err
	}
	stage, err := os.MkdirTemp(filepath.Dir(b.dir), ".topo-worker-setup-")
	if err != nil {
		return errors.New("cannot stage new configuration")
	}
	defer os.RemoveAll(stage)
	contents := map[string][]byte{"topo-worker.env": environment(s), "worker-token": token, "targets.allow": targets, "known_hosts": hosts}
	r := receipt{Version: 1, Settings: s, Hashes: map[string]string{}}
	for name, data := range contents {
		if err = writeNew(stage, name, data, 0640, b.ownerUID, b.gid); err != nil {
			return err
		}
		if name != "worker-token" {
			r.Hashes[name] = digest(data)
		}
	}
	if err = validatePolicy(ctx, stage); err != nil {
		return err
	}
	if os.Chown(stage, b.ownerUID, b.gid) != nil || os.Chmod(stage, 0750) != nil {
		return errors.New("cannot restrict staged configuration directory")
	}
	if b.stagedCheck != nil {
		err = b.stagedCheck(ctx, s, stage)
	} else {
		staged := *b
		staged.dir = stage
		err = staged.check(ctx, s)
	}
	if err != nil {
		return errors.New("initial worker preflight failed; no configuration saved or service started; verify settings, token permissions and ServiceNow access, then rerun setup to re-enter settings")
	}
	body, _ := json.Marshal(r)
	if err = writeNew(stage, "setup.json", body, 0600, b.ownerUID, b.gid); err != nil {
		return err
	}
	stageDir, e := os.Open(stage)
	if e != nil {
		return errors.New("cannot inspect staged directory")
	}
	e = stageDir.Sync()
	stageDir.Close()
	if e != nil {
		return errors.New("cannot sync staged directory")
	}
	// RENAME_NOREPLACE prevents clobbering even an empty existing deployment.
	if ctx.Err() != nil {
		return errors.New("setup cancelled before publishing configuration")
	}
	if err = unix.Renameat2(unix.AT_FDCWD, stage, unix.AT_FDCWD, b.dir, unix.RENAME_NOREPLACE); err != nil {
		return errors.New("cannot publish configuration without replacement; existing files preserved")
	}
	parent, e := os.Open(filepath.Dir(b.dir))
	if e != nil {
		return errors.New("configuration saved but directory sync failed; rerun setup to resume")
	}
	defer parent.Close()
	if parent.Sync() != nil {
		return errors.New("configuration saved but directory sync failed; rerun setup to resume")
	}
	return nil
}
func writeNew(dir, name string, data []byte, mode os.FileMode, uid, gid int) error {
	f, err := os.OpenFile(filepath.Join(dir, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("cannot create new service file")
	}
	defer f.Close()
	if f.Chown(uid, gid) != nil || f.Chmod(mode) != nil {
		return errors.New("cannot restrict new service file")
	}
	if _, err = f.Write(data); err != nil {
		return errors.New("cannot write new service file")
	}
	if f.Sync() != nil {
		return errors.New("cannot sync new service file")
	}
	return nil
}
func validatePolicy(ctx context.Context, dir string) error {
	if ctx.Err() != nil {
		return errors.New("setup policy validation cancelled")
	}
	cfg, err := worker.LoadSSHStartupConfig(filepath.Join(dir, "targets.allow"), filepath.Join(dir, "known_hosts"))
	if err != nil {
		return errors.New("invalid SSH allowlist or known_hosts; no policy saved")
	}
	return validateTargets(ctx, cfg.Allowlist, cfg.HostKeyCallback)
}
func validateTargets(ctx context.Context, prefixes []netip.Prefix, callback ssh.HostKeyCallback) error {
	probe, _ := ssh.NewPublicKey(ed25519.PublicKey(make([]byte, ed25519.PublicKeySize)))
	selected := map[string]bool{}
	for _, prefix := range prefixes {
		for addr := prefix.Addr(); prefix.Contains(addr); addr = addr.Next() {
			if ctx.Err() != nil {
				return errors.New("setup policy validation cancelled")
			}
			if selected[addr.String()] {
				continue
			}
			selected[addr.String()] = true
			if len(selected) > 1024 {
				return errors.New("setup allowlist exceeds 1024 selected IPv4 addresses")
			}
			err := callback(net.JoinHostPort(addr.String(), "22"), &net.TCPAddr{IP: net.ParseIP(addr.String()), Port: 22}, probe)
			var mismatch *knownhosts.KeyError
			if err != nil && (!errors.As(err, &mismatch) || len(mismatch.Want) == 0) {
				return errors.New("verified SSH host key is required for every allowlisted address on port 22")
			}
		}
	}
	return nil
}
func checkArgs(s settings, dir string) []string {
	return []string{"worker", "check", "-servicenow-instance", s.Instance, "-token-ref", "file:" + filepath.Join(dir, "worker-token"), "-worker-pool", s.Pool, "-site", s.Site, "-allow-ssh-linux", "-ssh-target-allowlist", filepath.Join(dir, "targets.allow"), "-ssh-known-hosts", filepath.Join(dir, "known_hosts"), "-max-concurrency", "2"}
}
func (b *linuxBackend) check(ctx context.Context, s settings) error {
	// The same executable, service identity, fixed files and flags as the pinned
	// unit; its omitted remote-start-interval has the same one-second default.
	out, err := command(ctx, &syscall.Credential{Uid: uint32(b.uid), Gid: uint32(b.gid), Groups: []uint32{}}, binary, checkArgs(s, b.dir)...)
	if err != nil {
		return errors.New("preflight failed")
	}
	var result struct {
		Status string `json:"status"`
	}
	if json.Unmarshal(out, &result) != nil || result.Status != "ready" {
		return errors.New("preflight did not return ready")
	}
	return nil
}
func (b *linuxBackend) start(ctx context.Context) error {
	_, err := command(ctx, nil, "/usr/bin/systemctl", "enable", "--now", "topo-worker.service")
	if err != nil {
		return err
	}
	_, err = command(ctx, nil, "/usr/bin/systemctl", "is-active", "--quiet", "topo-worker.service")
	return err
}

type boundedOutput struct {
	data     []byte
	overflow bool
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	n := len(p)
	remaining := (16 << 10) - len(b.data)
	if len(p) > remaining {
		b.overflow = true
		p = p[:remaining]
	}
	b.data = append(b.data, p...)
	return n, nil
}
func command(ctx context.Context, identity *syscall.Credential, program string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.WaitDelay = 2 * time.Second
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C", "HOME=/nonexistent"}
	if identity != nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{Credential: identity}
	}
	var stdout, stderr boundedOutput
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if cmd.Run() != nil || stdout.overflow || stderr.overflow {
		return nil, errors.New("bounded setup operation failed (process output withheld)")
	}
	return stdout.data, nil
}
