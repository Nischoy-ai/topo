//go:build !linux

package workersetup

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestUnsupportedPlatform(t *testing.T) {
	if err := Run(context.Background(), strings.NewReader(""), io.Discard, nil); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatal("unsupported platform accepted")
	}
}
