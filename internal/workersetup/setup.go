// Package workersetup configures the fixed packaged Linux SSH worker service.
// It never creates ServiceNow identities or discovers targets during setup.
package workersetup

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
)

const directory = "/etc/topo-worker"

type settings struct {
	Instance string
	Pool     string
	Site     string
}
type inputs struct {
	Token   string
	Targets string
	Hosts   string
}
type backend interface {
	ready(context.Context) error
	existing(context.Context) (*settings, error)
	install(context.Context, settings, inputs) error
	check(context.Context, settings) error
	start(context.Context) error
}

// Run requires a real terminal, root, systemd and the reviewed packaged unit.
// Only references and non-secret settings are prompted; command arguments are
// deliberately unsupported so secret values cannot become setup CLI options.
func Run(ctx context.Context, in io.Reader, out io.Writer, args []string) error {
	if len(args) != 0 {
		return errors.New("usage: topo worker setup (interactive packaged Linux SSH setup; no arguments)")
	}
	b, err := platformBackend()
	if err != nil {
		return err
	}
	return runBackend(ctx, in, out, b)
}

func runBackend(ctx context.Context, in io.Reader, out io.Writer, b backend) error {
	if closer, ok := b.(io.Closer); ok {
		defer closer.Close()
	}
	return execute(ctx, in, out, b)
}

func execute(ctx context.Context, in io.Reader, out io.Writer, b backend) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if err := b.ready(ctx); err != nil {
		return err
	}
	current, err := b.existing(ctx)
	if err != nil {
		return err
	}
	ask := prompter(ctx, in, out)
	fmt.Fprintln(out, "Linux SSH service setup. Complete ServiceNow app/index/source checks, human roles, seven-resource worker OAuth and profile configuration first. Enter references only, never token/password values.")
	fresh := current == nil
	if fresh {
		var s settings
		if s.Instance, err = ask("ServiceNow HTTPS instance origin: "); err != nil {
			return err
		}
		if s.Pool, err = ask("Existing worker pool ID: "); err != nil {
			return err
		}
		if s.Site, err = ask("Existing site ID: "); err != nil {
			return err
		}
		if err = validate(s); err != nil {
			return err
		}
		var i inputs
		if i.Token, err = ask("OAuth token reference (file:/absolute/path; protected existing file): "); err != nil {
			return err
		}
		if !strings.HasPrefix(i.Token, "file:/") {
			return errors.New("setup supports only an absolute file: credential reference; no token values")
		}
		i.Token = strings.TrimPrefix(i.Token, "file:")
		if i.Targets, err = ask("Approved IPv4 CIDR allowlist file (absolute path; at most 1024 addresses): "); err != nil {
			return err
		}
		if i.Hosts, err = ask("Independently verified SSH known_hosts file (absolute path): "); err != nil {
			return err
		}
		answer, e := ask("Have you independently verified every target SSH fingerprint (not keyscan alone)? Type verified: ")
		if e != nil {
			return e
		}
		if answer != "verified" {
			return errors.New("setup stopped: independently verified SSH trust is required")
		}
		fmt.Fprintln(out, "Creates root-owned 0750 /etc/topo-worker and 0640 service files; SSH only, port 22, concurrency 2, one-second target pacing. Does not modify source files or existing configuration.")
		answer, e = ask("Write this new local configuration? Type yes: ")
		if e != nil {
			return e
		}
		if answer != "yes" {
			return errors.New("setup stopped without writing configuration")
		}
		if ctx.Err() != nil {
			return errors.New("setup cancelled before writing configuration")
		}
		fmt.Fprintln(out, "Checking staged registration and heartbeat as topo-worker before saving configuration (no discovery or IRE).")
		if err = b.install(ctx, s, i); err != nil {
			return err
		}
		current = &s
	} else {
		fmt.Fprintln(out, "Resuming an intact setup-created configuration; existing files will not be rewritten.")
	}
	if err := ctx.Err(); err != nil {
		return errors.New("setup cancelled; rerun to check saved configuration")
	}
	if !fresh {
		fmt.Fprintln(out, "Checking saved registration and heartbeat as topo-worker (no target discovery or IRE).")
		if err = b.check(ctx, *current); err != nil {
			return errors.New("worker preflight failed; saved configuration preserved and service not started; retry after correcting credentials or remote access")
		}
	}
	fmt.Fprintln(out, "Worker ready. Registration is not proof of target access or CMDB delivery.")
	answer, err := ask("Enable and start topo-worker.service now? Type yes: ")
	if err != nil {
		return err
	}
	if answer != "yes" {
		fmt.Fprintln(out, "Configuration saved; service not started. Rerun topo worker setup when ready.")
		return nil
	}
	if err := ctx.Err(); err != nil {
		return errors.New("setup cancelled before service activation")
	}
	if err = b.start(ctx); err != nil {
		return errors.New("service activation failed; configuration preserved; inspect systemctl status topo-worker.service and rerun setup if inactive (enable/start may have partially succeeded)")
	}
	fmt.Fprintf(out, "Service started. In ServiceNow open Nischoy Topo → Discovery Profiles, select the SSH profile for pool %s/site %s, then Run now. Inspect Runs, collection errors, IRE Deliveries and actual CMDB computer/interfaces; repeat before scheduling.\n", current.Pool, current.Site)
	return nil
}

var identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

func validate(s settings) error {
	u, err := url.Parse(s.Instance)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.ForceQuery || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || u.Opaque != "" || strings.ContainsAny(s.Instance, "%\\\"' \t\r\n$") {
		return errors.New("instance must be an HTTPS origin without userinfo, path, query, fragment or interpolation")
	}
	if !identifier.MatchString(s.Pool) || !identifier.MatchString(s.Site) {
		return errors.New("pool and site must be 1–128 safe identifier characters")
	}
	return nil
}

// A single bounded reader serves all prompts. Context expiry ends an idle
// terminal session without per-prompt goroutines; no entered value is echoed.
func prompter(ctx context.Context, in io.Reader, out io.Writer) func(string) (string, error) {
	type result struct {
		line string
		err  error
	}
	values := make(chan result)
	go func() {
		defer close(values)
		scanner := bufio.NewScanner(in)
		scanner.Buffer(make([]byte, 256), 4096)
		for scanner.Scan() {
			select {
			case values <- result{line: scanner.Text()}:
			case <-ctx.Done():
				return
			}
		}
		if scanner.Err() != nil {
			select {
			case values <- result{err: errors.New("input exceeds bounded setup line size or cannot be read")}:
			case <-ctx.Done():
			}
		}
	}()
	return func(prompt string) (string, error) {
		fmt.Fprint(out, prompt)
		select {
		case <-ctx.Done():
			return "", errors.New("setup cancelled or timed out; any saved configuration is preserved")
		case r, ok := <-values:
			if !ok {
				return "", errors.New("setup input ended; any saved configuration is preserved")
			}
			if r.err != nil {
				return "", r.err
			}
			if len(r.line) > 2048 || strings.ContainsAny(r.line, "\x00\r") {
				return "", errors.New("invalid setup input")
			}
			return strings.TrimSpace(r.line), nil
		}
	}
}
