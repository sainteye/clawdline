//go:build darwin || linux

package process

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/session"
)

func TestRemoteControlWorkersDoNotBecomeTheirLaunchersTerminal(t *testing.T) {
	worker := "claude --print --sdk-url https://api.anthropic.com/v1/code/sessions/cse_fixture --session-id cse_fixture --input-format stream-json --output-format stream-json"
	home, _ := os.UserHomeDir()
	native := filepath.Join(home, ".local", "share", "claude", "versions", "2.1.312")
	var asked []int
	p := &PS{
		table: func(context.Context) ([]byte, error) {
			return []byte(strings.Join([]string{
				"ttys001 10 10 10 1 claude remote-control --spawn same-dir",
				"?? 11 10 -1 10 " + strings.Replace(worker, "claude ", native+" ", 1),
				"ttys001 12 10 10 10 " + worker,
				"?? 13 10 -1 10 claude -p ordinary SDK prompt",
				"ttys002 14 14 14 1 claude --remote-control",
			}, "\n")), nil
		},
		RemoteControl: func(_ context.Context, pid int, remote string) (session.Session, bool) {
			asked = append(asked, pid)
			return session.Session{ID: "remote-" + string(rune('a'+pid)), PID: pid, Backend: session.BackendClaudeRemote}, true
		},
	}
	inv, err := p.Scan(context.Background())
	if err != nil || !inv.Complete || len(inv.Sessions) != 3 || len(asked) != 2 || asked[0] != 11 || asked[1] != 12 {
		t.Fatalf("inventory=%+v asked=%v err=%v", inv, asked, err)
	}
	if inv.Sessions[0].TTY != "" || inv.Sessions[1].TTY != "" || inv.Sessions[2].PID != 14 {
		t.Fatalf("remote workers borrowed a tty or interactive RC disappeared: %+v", inv.Sessions)
	}
	asked = nil
	inv, err = p.ScanTTYs(context.Background(), map[string]bool{"ttys001": true})
	if err != nil || len(asked) != 0 || len(inv.Sessions) != 0 {
		t.Fatalf("managed-only discovery read external worker records: %+v %v %v", inv, asked, err)
	}
}

func TestRemoteControlWorkerRequiresTheNativeTransport(t *testing.T) {
	for _, tc := range []struct {
		args string
		want bool
	}{
		{"-p --sdk-url=https://api.anthropic.com/v1/code/sessions/cse_fixture --session-id=cse_fixture", true},
		{"-p --sdk-url=https://api.anthropic.com/v1/code/sessions/cse_fixture?token=x --session-id=cse_fixture", false},
		{"-p --sdk-url=https://api.anthropic.com/v1/code/sessions/cse_fixture#x --session-id=cse_fixture", false},
		{"-p --sdk-url=https://api.anthropic.com/v1/code/sessions/../../escape --session-id=../../escape", false},
		{"--sdk-url https://api.anthropic.com/v1/code/sessions/cse_fixture --session-id cse_fixture", false},
		{"-p --sdk-url https://example.com/v1/code/sessions/cse_fixture --session-id cse_fixture", false},
		{"-p --sdk-url https://api.anthropic.com/v1/code/sessions/other --session-id cse_fixture", false},
		{"-p --sdk-url https://api.anthropic.com/v1/code/sessions/cse_fixture", false},
	} {
		if got := claudeRemoteWorker(strings.Fields(tc.args)); got != tc.want {
			t.Errorf("%q = %v, want %v", tc.args, got, tc.want)
		}
	}
}
