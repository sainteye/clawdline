package terminal

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"

	"github.com/sainteye/clawdline/internal/adapters/antigravity"
	"github.com/sainteye/clawdline/internal/app/ports"
)

func identityUnavailable() error {
	return &antigravity.NativeIdentityError{Code: "antigravity_native_terminal_unavailable"}
}

// metadataOutput shares the registered local CLI probe output/deadline bounds.
// Overflow is a refusal, never a truncated authoritative listing.
type metadataOutput struct{ buffer bytes.Buffer }

func (b *metadataOutput) Write(p []byte) (int, error) {
	if len(p) > antigravity.CLIProbeOutputLimit-b.buffer.Len() {
		return 0, identityUnavailable()
	}
	return b.buffer.Write(p)
}
func nativeMetadata(cmd *exec.Cmd) ([]byte, error) {
	if cmd == nil {
		return nil, identityUnavailable()
	}
	cmd.Env = append(cmd.Environ(), "LC_ALL=C")
	cmd.WaitDelay = antigravity.CLIProbeTimeLimit
	b := &metadataOutput{}
	cmd.Stdout = b
	if cmd.Run() != nil {
		return nil, identityUnavailable()
	}
	return b.buffer.Bytes(), nil
}

// ExactAntigravityTerminal never calls Inventory or transcript resolution.
// Backend root identity and the complete ancestry are rechecked after mapping.
func (t *Tmux) ExactAntigravityTerminal(ctx context.Context, pid int, tty string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, antigravity.MaxProcessReadTime)
	defer cancel()
	choice := t.choice(ctx)
	if !choice.Found() {
		return "", identityUnavailable()
	}
	// A daemon may have no inherited TMUX client flags. Pin UTF-8 before
	// reading control-byte-separated fields, just as inventory does.
	query := func() ([]byte, error) {
		return nativeMetadata(choice.Command(ctx, "-u", "list-panes", "-a", "-F", "#{pane_id}"+paneSeparator+"#{pane_tty}"+paneSeparator+"#{pane_pid}"))
	}
	before, err := query()
	if err != nil {
		return "", err
	}
	id, root := "", 0
	for _, line := range strings.Split(strings.TrimSpace(string(before)), "\n") {
		f := splitPaneFields(line)
		if len(f) != 3 {
			return "", identityUnavailable()
		}
		if f[1] != tty {
			continue
		}
		if id != "" {
			return "", identityUnavailable()
		}
		root, err = strconv.Atoi(f[2])
		if err != nil || root <= 0 {
			return "", identityUnavailable()
		}
		id = f[0]
	}
	if id == "" {
		return "", nil
	}
	r := antigravity.NewNativeProcessReader(nil)
	var chain []ports.AntigravityProcess
	for current := pid; len(chain) < antigravity.MaxAncestry; {
		p, err := r.ReadAntigravityProcess(ctx, current)
		if err != nil || p.TTY != tty {
			return "", identityUnavailable()
		}
		chain = append(chain, p)
		if current == root {
			break
		}
		if p.ParentPID <= 0 || p.ParentPID == current {
			return "", identityUnavailable()
		}
		current = p.ParentPID
	}
	if len(chain) == 0 || chain[len(chain)-1].PID != root {
		return "", identityUnavailable()
	}
	after, err := query()
	if err != nil || !bytes.Equal(before, after) {
		return "", identityUnavailable()
	}
	for _, p := range chain {
		fresh, err := r.ReadAntigravityProcess(ctx, p.PID)
		if err != nil || fresh != p {
			return "", identityUnavailable()
		}
	}
	return id, nil
}

// AntigravityPaneProcess enumerates bounded candidates only. Each candidate
// needs independent pinned executable, kernel ancestry and foreground proof.
func (t *Tmux) AntigravityPaneProcess(ctx context.Context, id, tty string, verifier ports.AntigravityProcessVerifier) (ports.AntigravityProcess, error) {
	ctx, cancel := context.WithTimeout(ctx, antigravity.MaxProcessReadTime)
	defer cancel()
	if tty == "" || strings.HasPrefix(tty, "-") || strings.ContainsAny(tty, "\x00\r\n") {
		return ports.AntigravityProcess{}, identityUnavailable()
	}
	data, err := nativeMetadata(exec.CommandContext(ctx, "/bin/ps", "-t", tty, "-o", "pid="))
	if err != nil {
		return ports.AntigravityProcess{}, err
	}
	var found ports.AntigravityProcess
	for _, field := range strings.Fields(string(data)) {
		pid, err := strconv.Atoi(field)
		if err != nil {
			return ports.AntigravityProcess{}, identityUnavailable()
		}
		p, err := verifier.VerifyAntigravityProcess(ctx, pid)
		if err != nil {
			var nonCLI ports.AntigravityNonCLI
			if errors.As(err, &nonCLI) && nonCLI.AntigravityNotStandalone() {
				continue
			}
			return ports.AntigravityProcess{}, identityUnavailable()
		}
		if p.TerminalID != id || p.TTY != tty {
			continue
		}
		raw := p
		raw.TTY = "/dev/" + p.TTY
		if err := antigravity.VerifyNativeForeground(ctx, raw); err != nil {
			continue
		}
		if found.PID != 0 {
			return ports.AntigravityProcess{}, identityUnavailable()
		}
		found = p
	}
	if found.PID == 0 {
		return found, identityUnavailable()
	}
	fresh, err := verifier.VerifyAntigravityProcess(ctx, found.PID)
	if err != nil || fresh != found {
		return ports.AntigravityProcess{}, identityUnavailable()
	}
	raw := fresh
	raw.TTY = "/dev/" + fresh.TTY
	if err := antigravity.VerifyNativeForeground(ctx, raw); err != nil {
		return ports.AntigravityProcess{}, err
	}
	return found, nil
}

// PreparedAntigravityPaneProcess resolves only the exact attached pane before
// a native conversation exists. Pane TTY/root metadata and native foreground
// proof are rechecked; a target name is never substituted for its actual ID.
func (t *Tmux) PreparedAntigravityPaneProcess(ctx context.Context, id string, verifier ports.AntigravityProcessVerifier) (ports.AntigravityProcess, error) {
	ctx, cancel := context.WithTimeout(ctx, antigravity.MaxProcessReadTime)
	defer cancel()
	if id == "" || verifier == nil {
		return ports.AntigravityProcess{}, identityUnavailable()
	}
	choice := t.choice(ctx)
	if !choice.Found() {
		return ports.AntigravityProcess{}, identityUnavailable()
	}
	query := func() ([]byte, error) {
		return nativeMetadata(choice.Command(ctx, "-u", "display-message", "-p", "-t", id, "#{pane_id}"+paneSeparator+"#{pane_tty}"+paneSeparator+"#{pane_pid}"))
	}
	before, err := query()
	if err != nil {
		return ports.AntigravityProcess{}, err
	}
	fields := splitPaneFields(strings.TrimSpace(string(before)))
	if len(fields) != 3 || fields[0] != id || !strings.HasPrefix(fields[1], "/dev/") {
		return ports.AntigravityProcess{}, identityUnavailable()
	}
	root, err := strconv.Atoi(fields[2])
	if err != nil || root <= 0 {
		return ports.AntigravityProcess{}, identityUnavailable()
	}
	process, err := t.AntigravityPaneProcess(ctx, id, strings.TrimPrefix(fields[1], "/dev/"), verifier)
	if err != nil {
		return ports.AntigravityProcess{}, err
	}
	after, err := query()
	if err != nil || !bytes.Equal(before, after) {
		return ports.AntigravityProcess{}, identityUnavailable()
	}
	return process, ctx.Err()
}
