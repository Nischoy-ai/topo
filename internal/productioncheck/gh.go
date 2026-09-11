package productioncheck

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
)

// GHAPI queries GitHub through an already-authenticated gh CLI process. It
// discards stderr and bounds stdout so authentication material or an
// unexpectedly large response cannot enter the preflight report.
type GHAPI struct {
	Binary string
}

func (g GHAPI) Get(ctx context.Context, path string) ([]byte, error) {
	binary := g.Binary
	if binary == "" {
		binary = "gh"
	}
	command := exec.CommandContext(ctx, binary, "api", path)
	var output limitedBuffer
	command.Stdout = &output
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		return nil, errors.New("GitHub CLI request failed")
	}
	if output.overflow {
		return nil, errors.New("GitHub CLI response exceeded the limit")
	}
	return output.buffer.Bytes(), nil
}

type limitedBuffer struct {
	buffer   bytes.Buffer
	overflow bool
}

func (b *limitedBuffer) Write(contents []byte) (int, error) {
	original := len(contents)
	remaining := maxResponseBytes + 1 - b.buffer.Len()
	if remaining <= 0 {
		b.overflow = true
		return original, nil
	}
	if len(contents) > remaining {
		contents = contents[:remaining]
		b.overflow = true
	}
	_, _ = b.buffer.Write(contents)
	return original, nil
}
