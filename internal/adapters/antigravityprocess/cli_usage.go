package antigravityprocess

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
)

// PublicCLI distinguishes the installed CLI entry point from the legacy ACP
// executable. Custom arguments are never translated into print-mode prompts.
func PublicCLI(args []string) bool { return len(args) > 0 && args[0] == "acp" }

// ReadCLIUsage asks the native CLI itself for its fixed numeric report. Public
// ACP does not implement /usage. Never send that string through session/prompt.
// Both supported ACP configurations use the CLI's default personal sign-in;
// custom profiles must remain unknown rather than reading another account.
func ReadCLIUsage(ctx context.Context, opts Options) (string, error) {
	if ctx == nil {
		return "", failure("invalid_context", NotWritten)
	}
	if !filepath.IsAbs(opts.Executable) || !filepath.IsAbs(opts.AccountDir) || !filepath.IsAbs(opts.WorkingDir) || opts.MaxFrameBytes <= 0 || opts.CallTimeout <= 0 {
		return "", failure("invalid_options", NotWritten)
	}
	if !slices.Equal(opts.Args, []string{"acp"}) && !slices.Equal(opts.Args, []string{"acp", "--app-data-dir", "antigravity-acp"}) {
		return "", failure("usage_unsupported", NotWritten)
	}
	if err := (&Client{opts: opts}).checkPersonal(); err != nil {
		return "", err
	}
	life, cancel := context.WithTimeout(ctx, opts.CallTimeout)
	defer cancel()
	// This documented print-mode command is answered by the CLI, before any
	// model turn. No conversation, input stream or custom slash expansion flag.
	cmd := exec.CommandContext(life, opts.Executable, "--settings", `{"useG1Credits":false}`, "--print", "/usage")
	if err := prepareProcess(cmd); err != nil {
		return "", err
	}
	cmd.Cancel = func() error { return killProcess(cmd) }
	cmd.WaitDelay = opts.CallTimeout
	stderr, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return "", failure("pipe_failed", NotWritten)
	}
	defer stderr.Close()
	var output quotaOutput
	output.limit = opts.MaxFrameBytes
	cmd.Dir, cmd.Env, cmd.Stderr, cmd.Stdout = opts.WorkingDir, personalEnv(opts.AccountDir), stderr, &output
	if err := cmd.Run(); err != nil {
		if output.overflow {
			return "", failure("usage_too_large", Acknowledged)
		}
		if life.Err() != nil {
			return "", failure("cancelled", Uncertain)
		}
		return "", failure("usage_failed", Uncertain)
	}
	return output.buffer.String(), nil
}

type quotaOutput struct {
	buffer   bytes.Buffer
	limit    int
	overflow bool
}

func (b *quotaOutput) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.buffer.Len() {
		b.overflow = true
		return 0, io.ErrShortBuffer
	}
	return b.buffer.Write(p)
}
