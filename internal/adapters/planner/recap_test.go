package planner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func fakeLook(name string) (string, error) { return "/bin/" + name, nil }

func TestRecapForksClaudeWithoutKeepingOrTools(t *testing.T) {
	var gotArgs []string
	var gotDir, gotStdin string
	p := Planner{LookPath: fakeLook, Run: func(_ context.Context, exe string, args []string, stdin, dir string, _ []string) ([]byte, error) {
		gotArgs, gotDir, gotStdin = args, dir, stdin
		return []byte(`{"result":"它在修手機鍵盤的問題，修好了，**等你**用 iPhone 確認。","is_error":false,"num_turns":1}`), nil
	}}
	got, err := p.Recap(context.Background(), RecapTarget{Assistant: "claude", Conversation: "c1a00001-0000-4000-8000-000000000001", Dir: "/work/repo", Language: "zh-Hant"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "它在修手機鍵盤的問題，修好了，等你用 iPhone 確認。" || got.Detail != "" {
		t.Fatalf("recap = %+v", got)
	}
	if !strings.Contains(gotStdin, "Traditional Chinese as used in Taiwan") {
		t.Fatalf("the language is not named: %q", gotStdin)
	}
	for _, want := range [][]string{
		{"--resume", "c1a00001-0000-4000-8000-000000000001"}, {"--fork-session"}, {"--no-session-persistence"},
		{"--tools", ""}, {"--disable-slash-commands"}, {"--model", "haiku"},
	} {
		if i := slices.Index(gotArgs, want[0]); i < 0 || (len(want) == 2 && (i+1 >= len(gotArgs) || gotArgs[i+1] != want[1])) {
			t.Fatalf("args %q lack %q", gotArgs, want)
		}
	}
	if slices.Contains(gotArgs, "/recap") || strings.Contains(gotStdin, "/recap") {
		t.Fatal("a recap must not send /recap: a person's own skill of that name answers instead")
	}
	if gotDir != "/work/repo" || !strings.HasPrefix(gotStdin, "Stop:") {
		t.Fatalf("dir %q stdin %q", gotDir, gotStdin)
	}
}

func TestRecapForksCodexEphemerallyInAScratchDirectory(t *testing.T) {
	var gotArgs, gotEnv []string
	var gotDir string
	p := Planner{LookPath: fakeLook, Home: "/home/me", Run: func(_ context.Context, exe string, args []string, stdin, dir string, env []string) ([]byte, error) {
		gotArgs, gotDir, gotEnv = args, dir, env
		out := args[slices.Index(args, "-o")+1]
		return nil, os.WriteFile(out, []byte("It fixed the keyboard jump.\nWaiting for you to check on the phone.\n"), 0o600)
	}}
	t.Setenv("CODEX_HOME", "")
	os.Unsetenv("CODEX_HOME")
	got, err := p.Recap(context.Background(), RecapTarget{Assistant: "codex", Conversation: "c0de0001-0000-4000-8000-000000000001", Dir: "/work/repo"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "It fixed the keyboard jump. Waiting for you to check on the phone." {
		t.Fatalf("recap = %+v", got)
	}
	if gotArgs[0] != "exec" || gotArgs[1] != "fork" || !slices.Contains(gotArgs, "--ephemeral") ||
		!slices.Contains(gotArgs, `sandbox_mode="read-only"`) || !slices.Contains(gotArgs, "shell_tool") {
		t.Fatalf("args %q", gotArgs)
	}
	if gotDir == "/work/repo" || gotDir == "" {
		t.Fatalf("a Codex fork runs in a scratch directory, not %q", gotDir)
	}
	if _, err := os.Stat(gotDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the scratch directory %s was left behind", gotDir)
	}
	if !slices.Contains(gotEnv, "CODEX_HOME="+filepath.Join("/home/me", ".codex")) {
		t.Fatalf("env %q", gotEnv)
	}
}

func TestRecapRefusesWhatIsNotAnAnswer(t *testing.T) {
	answer := func(raw string) Planner {
		return Planner{LookPath: fakeLook, Run: func(context.Context, string, []string, string, string, []string) ([]byte, error) {
			return []byte(raw), nil
		}}
	}
	target := RecapTarget{Assistant: "claude", Conversation: "c1a00001-0000-4000-8000-000000000001", Dir: "/w"}
	for name, raw := range map[string]string{
		"error":      `{"result":"No conversation found","is_error":true,"num_turns":1}`,
		"more turns": `{"result":"I ran the tests again","is_error":false,"num_turns":3}`,
		"empty":      `{"result":"   ","is_error":false,"num_turns":1}`,
		"not json":   `oops`,
	} {
		if _, err := answer(raw).Recap(context.Background(), target); !errors.Is(err, ErrNotARecap) {
			t.Errorf("%s: err = %v, want ErrNotARecap", name, err)
		}
	}
	quota := Planner{LookPath: fakeLook, Run: func(context.Context, string, []string, string, string, []string) ([]byte, error) {
		return []byte(`{"result":"You've hit your weekly limit","is_error":true}`), &ExitError{Err: errors.New("exit 1")}
	}}
	if _, err := quota.Recap(context.Background(), target); !errors.Is(err, ErrOutOfQuota) {
		t.Errorf("quota: err = %v", err)
	}
}

func TestRecapOnlyForksAConversationIDAndAKnownAssistant(t *testing.T) {
	ran := false
	p := Planner{LookPath: fakeLook, Run: func(context.Context, string, []string, string, string, []string) ([]byte, error) {
		ran = true
		return nil, nil
	}}
	for _, target := range []RecapTarget{
		{Assistant: "grok", Conversation: "c1a00001-0000-4000-8000-000000000001", Dir: "/w"},
		{Assistant: "claude", Conversation: "--dangerously-skip-permissions", Dir: "/w"},
		{Assistant: "claude", Conversation: "../../etc/passwd", Dir: "/w"},
		{Assistant: "claude", Conversation: "c1a00001-0000-4000-8000-000000000001", Dir: ""},
	} {
		if _, err := p.Recap(context.Background(), target); !errors.Is(err, ErrNoPlanner) {
			t.Errorf("%+v: err = %v", target, err)
		}
	}
	if ran {
		t.Fatal("a refused target started a process")
	}
}

func TestCleanRecapCutsALongAnswer(t *testing.T) {
	got, err := cleanRecap(strings.Repeat("字", MaxRecapRunes+50), MaxRecapRunes)
	if err != nil {
		t.Fatal(err)
	}
	if n := len([]rune(got)); n != MaxRecapRunes || !strings.HasSuffix(got, "…") {
		t.Fatalf("len %d, %q", n, got[len(got)-6:])
	}
}

func TestARecapHasAShortLineAndTheCopilotsFullerAccount(t *testing.T) {
	got, err := splitRecap("It fixed the keyboard jump; your move: check on the phone.\n---\nThe goal was a page that stays put. It finished the fix and the tests. It needs you to check on the iPhone.")
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "It fixed the keyboard jump; your move: check on the phone." || !strings.HasPrefix(got.Detail, "The goal was a page that stays put.") {
		t.Fatalf("%+v", got)
	}
}

func TestAForkThatAnsweredAsTheSessionIsNotKept(t *testing.T) {
	for _, s := range []string{
		"I can't give you that status summary in the requested format right now. Two things block it.",
		"Sorry, I cannot do that.",
		"我無法提供這個摘要。",
	} {
		if _, err := splitRecap(s); !errors.Is(err, ErrNotARecap) {
			t.Errorf("%q: err = %v", s, err)
		}
	}
}
