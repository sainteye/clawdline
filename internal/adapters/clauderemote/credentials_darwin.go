//go:build darwin

package clauderemote

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
)

func keychainCredentials(ctx context.Context) ([]byte, error) {
	// Output is secret-bearing: it never becomes an error, log or command argument.
	cmd := exec.CommandContext(ctx, "/usr/bin/security", "find-generic-password", "-s", "Claude Code-credentials", "-w")
	var out bytes.Buffer
	cmd.Stdout = &limitedOutput{out: &out}
	cmd.Stderr = nil
	if cmd.Run() != nil {
		return nil, errors.New("Claude keychain is unavailable")
	}
	return out.Bytes(), nil
}

type limitedOutput struct{ out *bytes.Buffer }

func (w *limitedOutput) Write(p []byte) (int, error) {
	if len(p) > ResponseBytesLimit-w.out.Len() {
		return 0, errors.New("Claude keychain data is oversized")
	}
	return w.out.Write(p)
}
