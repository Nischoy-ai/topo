package workersetup

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

type fakeBackend struct {
	saved                                                 *settings
	readyErr, existingErr, installErr, checkErr, startErr error
	installs, checks, starts                              int
}

func (b *fakeBackend) ready(context.Context) error                 { return b.readyErr }
func (b *fakeBackend) existing(context.Context) (*settings, error) { return b.saved, b.existingErr }
func (b *fakeBackend) install(_ context.Context, s settings, _ inputs) error {
	b.installs++
	if b.installErr != nil {
		return b.installErr
	}
	b.checks++
	if b.checkErr != nil {
		return errors.New("initial preflight failed; no configuration saved")
	}
	b.saved = &s
	return nil
}
func (b *fakeBackend) check(context.Context, settings) error { b.checks++; return b.checkErr }
func (b *fakeBackend) start(context.Context) error           { b.starts++; return b.startErr }

const answers = "https://example.service-now.com\npool-a\nsite-a\nfile:/private/token\n/private/allow\n/private/hosts\nverified\nyes\n"

func TestSetupLifecycleAndFailureResume(t *testing.T) {
	for _, tc := range []struct {
		name, tail         string
		checkErr, startErr error
		wantErr            bool
		starts             int
	}{
		{name: "decline start", tail: "no\n"},
		{name: "approve start", tail: "yes\n", starts: 1},
		{name: "preflight failure", checkErr: errors.New("sensitive-output"), wantErr: true},
		{name: "start failure", tail: "yes\n", startErr: errors.New("sensitive-output"), wantErr: true, starts: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := &fakeBackend{checkErr: tc.checkErr, startErr: tc.startErr}
			var out bytes.Buffer
			err := execute(context.Background(), strings.NewReader(answers+tc.tail), &out, b)
			if (err != nil) != tc.wantErr || b.installs != 1 || b.checks != 1 || b.starts != tc.starts {
				t.Fatalf("err=%v calls=%+v", err, b)
			}
			if strings.Contains(out.String(), "sensitive-output") || (err != nil && strings.Contains(err.Error(), "sensitive-output")) {
				t.Fatal("subprocess output leaked")
			}
			b.checkErr = nil
			b.startErr = nil
			retryInput := "yes\n"
			wantInstalls := 1
			if tc.checkErr != nil {
				retryInput = answers + "yes\n"
				wantInstalls = 2
			}
			if err = execute(context.Background(), strings.NewReader(retryInput), io.Discard, b); err != nil {
				t.Fatal(err)
			}
			if b.installs != wantInstalls || b.checks != 2 || b.starts != tc.starts+1 {
				t.Fatal("resume rewrote files or skipped check")
			}
		})
	}
}
func TestSetupRefusesUnsafeInputsAndExistingConfig(t *testing.T) {
	for _, input := range []string{
		strings.Replace(answers, "https://example.service-now.com", "https://user:secret@example.com", 1),
		strings.Replace(answers, "pool-a", "pool $HOME", 1),
		strings.Replace(answers, "pool-a", "pool:bad", 1),
		strings.Replace(answers, "site-a", "site:bad", 1),
		strings.Replace(answers, "https://example.service-now.com", "https://example.service-now.com?", 1),
		strings.Replace(answers, "file:/private/token", "not-a-reference", 1),
		strings.Replace(answers, "verified", "yes", 1),
		strings.Replace(answers, "verified\nyes", "verified\nno", 1),
		strings.Repeat("x", 5000) + "\n", ""} {
		b := &fakeBackend{}
		var out bytes.Buffer
		if err := execute(context.Background(), strings.NewReader(input), &out, b); err == nil || b.installs != 0 || b.checks != 0 || b.starts != 0 {
			t.Fatalf("unsafe input accepted: calls %+v", b)
		}
		if strings.Contains(out.String(), "secret") {
			t.Fatal("input echoed")
		}
	}
	for _, b := range []*fakeBackend{{readyErr: errors.New("unsupported")}, {existingErr: errors.New("existing unmanaged deployment")}} {
		if err := execute(context.Background(), strings.NewReader(answers+"yes\n"), io.Discard, b); err == nil || b.installs != 0 || b.checks != 0 {
			t.Fatal("existing/platform failure changed deployment")
		}
	}
}
func TestSetupCancellationAtIdlePrompt(t *testing.T) {
	r, w := io.Pipe()
	defer r.Close()
	defer w.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	b := &fakeBackend{}
	if err := execute(ctx, r, io.Discard, b); err == nil || b.installs != 0 || b.starts != 0 {
		t.Fatal("cancelled prompt changed deployment")
	}
}
func TestSettingsCannotInjectServiceEnvironment(t *testing.T) {
	good := settings{Instance: "https://example.service-now.com", Pool: "pool-a", Site: "site-a"}
	if err := validate(good); err != nil {
		t.Fatal(err)
	}
	for _, origin := range []string{"http://example.com", "https://example.com/", "https://example.com?token=secret", "https://example.com#part", "https://user@example.com", "https://example.com/%n", "https://example.com\nX=1"} {
		s := good
		s.Instance = origin
		if validate(s) == nil {
			t.Fatal("unsafe origin accepted")
		}
	}
}
func TestSetupRejectsArgumentsWithoutEcho(t *testing.T) {
	err := Run(context.Background(), strings.NewReader(""), io.Discard, []string{"secret-value"})
	if err == nil || strings.Contains(err.Error(), "secret-value") {
		t.Fatal("setup arguments accepted or echoed")
	}
}

func TestInstallFailureDoesNotPreflightOrStart(t *testing.T) {
	b := &fakeBackend{installErr: errors.New("cannot publish without replacement")}
	if err := execute(context.Background(), strings.NewReader(answers+"yes\n"), io.Discard, b); err == nil || b.checks != 0 || b.starts != 0 {
		t.Fatal("continued after install failure")
	}
}

type closingBackend struct {
	fakeBackend
	closed bool
}

func (b *closingBackend) Close() error { b.closed = true; return nil }
func TestBackendClosedOnReadinessFailureAndSuccess(t *testing.T) {
	for _, fails := range []bool{false, true} {
		b := &closingBackend{fakeBackend: fakeBackend{saved: &settings{Instance: "https://example.service-now.com", Pool: "pool-a", Site: "site-a"}}}
		if fails {
			b.readyErr = errors.New("locked or unsupported")
		}
		_ = runBackend(context.Background(), strings.NewReader("no\n"), io.Discard, b)
		if !b.closed {
			t.Fatal("backend/lock not closed")
		}
	}
}
