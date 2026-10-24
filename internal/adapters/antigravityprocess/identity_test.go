//go:build darwin || linux

package antigravityprocess

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIdentityEnvironmentDoesNotBorrowParent(t *testing.T) {
	for _, key := range []string{"CLAWDLINE_PROTOCOL_SESSION_FILE", "CLAWDLINE_SQUAD_CAPABILITY_FILE", "CLAUDE_CODE_SESSION_ID", "CODEX_THREAD_ID", "CODEX_SESSION_ID", "CLAWDLINE_SESSION_ID", "CLAWDLINE_CONVERSATION_ID", "CLAWDLINE_TASK_SECRET"} {
		t.Setenv(key, "parent-identity")
	}
	env := strings.Join(personalEnv(t.TempDir()), "\n")
	if strings.Contains(env, "parent-identity") {
		t.Fatal("parent identity or actor leaked")
	}
}

func TestIdentityOptionsRequireAbsolutePath(t *testing.T) {
	dir := t.TempDir()
	_, err := Start(context.Background(), Options{Executable: filepath.Join(dir, "missing"), AccountDir: dir, WorkingDir: dir, MaxFrameBytes: 4096, CallTimeout: time.Second, SessionIdentityPath: "relative.json"})
	if e, ok := err.(*Error); !ok || e.Code != "invalid_options" {
		t.Fatalf("got %v", err)
	}
}

// This peer inspects the actual exec environment, without provider RPC or auth.
func TestIdentityEnvironmentPeer(t *testing.T) {
	if os.Getenv("CLW_IDENTITY_PEER") != "yes" {
		return
	}
	raw := strings.Join([]string{os.Getenv("CLAWDLINE_PROTOCOL_SESSION_FILE"), os.Getenv("CLAWDLINE_SQUAD_CAPABILITY_FILE"), os.Getenv("CODEX_THREAD_ID"), os.Getenv("CLAUDE_CODE_SESSION_ID")}, "\n")
	if err := os.WriteFile(filepath.Join(os.Getenv("GEMINI_HOME"), "identity-env.tmp"), []byte(raw), 0600); err != nil {
		os.Exit(31)
	}
	if err := os.Rename(filepath.Join(os.Getenv("GEMINI_HOME"), "identity-env.tmp"), filepath.Join(os.Getenv("GEMINI_HOME"), "identity-env")); err != nil {
		os.Exit(32)
	}
	// Stay alive until the owned process is closed.
	_, _ = os.Stdin.Read(make([]byte, 1))
	os.Exit(0)
}

func TestIdentityExecReceivesOnlyItsOwnPointer(t *testing.T) {
	t.Setenv("CLW_IDENTITY_PEER", "yes")
	t.Setenv("CLAWDLINE_PROTOCOL_SESSION_FILE", "parent-file")
	t.Setenv("CLAWDLINE_SQUAD_CAPABILITY_FILE", "parent-actor")
	t.Setenv("CODEX_THREAD_ID", "parent-codex")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "parent-claude")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, own := range []bool{false, true} {
		dir := t.TempDir()
		path := ""
		if own {
			path = filepath.Join(dir, "own.json")
		}
		c, err := Start(context.Background(), Options{Executable: exe, Args: []string{"-test.run=^TestIdentityEnvironmentPeer$"}, AccountDir: dir, WorkingDir: dir, MaxFrameBytes: 4096, CallTimeout: time.Second, SessionIdentityPath: path})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = c.Close() })
		deadline := time.Now().Add(3 * time.Second)
		for {
			raw, err := os.ReadFile(filepath.Join(dir, "identity-env"))
			if err == nil {
				if string(raw) != path+"\n\n\n" {
					t.Fatalf("exec environment %q", raw)
				}
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("peer never observed environment")
			}
			time.Sleep(time.Millisecond)
		}
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
