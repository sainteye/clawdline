package projects

import (
	"os"
	"path/filepath"
	"testing"
)

// The line a new terminal is typed is the one thing on the start route a
// person's machine executes, so its exact spelling is pinned here.
func TestLaunchLineIsTheSwiftAppsLine(t *testing.T) {
	l, err := Admit(LaunchRequest{ProjectRoot: "/tmp/it's here", Assistant: AssistantClaude})
	if err != nil {
		t.Fatal(err)
	}
	want := "cd '/tmp/it'\\''s here' && env -u CLAUDECODE -u CLAUDE_CODE_SESSION_ID -u CLAUDE_CODE_CHILD_SESSION " +
		"-u CLAUDE_PID -u CLAUDE_CODE_MESSAGING_SOCKET -u CLAUDE_CODE_MESSAGING_TOKEN " +
		"-u CLAUDE_CODE_BRIDGE_SESSION_ID -u CLAUDE_EFFORT -u AI_AGENT claude"
	if got := l.ShellLine(); got != want {
		t.Fatalf("line\n got %q\nwant %q", got, want)
	}
	r, err := Admit(LaunchRequest{ProjectRoot: "/p", Assistant: AssistantCodex, Model: "opus",
		Resume: "0f1e0000-0000-4000-8000-000000000001"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := r.ShellCommand(), "env -u CODEX_THREAD_ID -u CODEX_SESSION_ID -u CODEX_SANDBOX "+
		"-u CODEX_SANDBOX_NETWORK_DISABLED codex resume 0f1e0000-0000-4000-8000-000000000001 --model opus"; got != want {
		t.Fatalf("resume first\n got %q\nwant %q", got, want)
	}
}

func TestLaunchRefusesWhatIsNotAName(t *testing.T) {
	for _, req := range []LaunchRequest{
		{ProjectRoot: "relative", Assistant: AssistantClaude},
		{ProjectRoot: "/a\nb", Assistant: AssistantClaude},
		{ProjectRoot: "/a", Assistant: "sh"},
		{ProjectRoot: "/a", Assistant: AssistantClaude, Model: "opus; rm -rf ~"},
		{ProjectRoot: "/a", Assistant: AssistantClaude, Resume: "0F1E0000-0000-4000-8000-000000000001"},
		{ProjectRoot: "/a", Assistant: AssistantClaude, Resume: "--dangerously"},
	} {
		if _, err := Admit(req); err == nil {
			t.Errorf("admitted %+v", req)
		}
	}
}

func TestPlanTable(t *testing.T) {
	cases := []struct {
		choice TerminalChoice
		iterm  bool
		tmux   TmuxReach
		want   PlanKind
	}{
		{TerminalAuto, true, TmuxRunning, PlanITerm},
		{TerminalAuto, false, TmuxRunning, PlanTmux},
		{TerminalAuto, false, TmuxInstalled, PlanNotRunning},
		{TerminalITerm, false, TmuxRunning, PlanNotRunning},
		{TerminalTmux, true, TmuxRunning, PlanTmux},
		{TerminalTmux, true, TmuxInstalled, PlanTmuxDetached},
		{TerminalTmux, true, TmuxAbsent, PlanNoTmux},
		{ParseTerminalChoice("ghostty"), false, TmuxRunning, PlanTmux},
	}
	for _, c := range cases {
		if got := ChoosePlan(c.choice, c.iterm, c.tmux); got != c.want {
			t.Errorf("%v %v %v: got %v want %v", c.choice, c.iterm, c.tmux, got, c.want)
		}
	}
}

// A dispatched child and a `-p` probe are not conversations anybody had.
func TestClaudePastKeepsOnlyConversations(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	place := Place{ID: "x", Path: "/work/app"}
	dir := filepath.Join(home, ".claude", "projects", Slug(place.Path))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(id, body string) {
		if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("11111111-1111-4111-8111-111111111111",
		`{"type":"file-history-snapshot","x":"\"type\":\"user\""}`+"\n"+
			`{"type":"user","message":{"content":"fix the login page\nmore"}}`+"\n")
	write("22222222-2222-4222-8222-222222222222",
		`{"type":"user","message":{"content":[{"type":"text","text":"You are a Clawdline CHILD agent for task 1"}]}}`+"\n")
	write("33333333-3333-4333-8333-333333333333",
		`{"type":"user","entrypoint":"sdk-cli","message":{"content":"what is 2+2"}}`+"\n")
	write("not-a-uuid", `{"type":"user","message":{"content":"hi"}}`+"\n")
	got := ClaudePast(place, nil, PastTitles{}, 10, 10)
	if len(got) != 1 || got[0].Title != "fix the login page" {
		t.Fatalf("got %+v", got)
	}
}

func TestCodexListedDropsChildrenAndOtherDirectories(t *testing.T) {
	body := []byte(`{"id":1,"result":{"data":[
		{"id":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa","cwd":"/p","updatedAt":20,"preview":"first line\nsecond","name":null},
		{"id":"bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb","cwd":"/p","updatedAt":30,"preview":"x","name":" Named "},
		{"id":"cccccccc-cccc-4ccc-8ccc-cccccccccccc","cwd":"/p","updatedAt":40,"preview":"You are a Clawdline CHILD agent for task 9"},
		{"id":"dddddddd-dddd-4ddd-8ddd-dddddddddddd","cwd":"/q","updatedAt":50,"preview":"elsewhere"}
	]}}`)
	got := codexListed(body, "/p", map[string]bool{"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa": true})
	if len(got) != 2 || got[0].Title != "Named" || got[1].Title != "first line" || !got[1].Live {
		t.Fatalf("got %+v", got)
	}
}

// A title Claude Code wrote from nothing but pasted images gives way to the
// fallback name; one a person typed never does.
func TestWeakTitleStepsAsideOnlyForAFallback(t *testing.T) {
	images := front{opening: "[Image #1] [Image #2]", said: "[Image #1] [Image #2]"}
	titles := func(custom, fallback string) PastTitles {
		return PastTitles{
			Recorded:  func(string) (string, string) { return "Image review", custom },
			Automatic: func(string) string { return fallback },
		}
	}
	if got := displayedTitle("p", "id", images, titles("", "檢視點滴素材")); got != "檢視點滴素材" {
		t.Fatalf("weak title kept: %q", got)
	}
	if got := displayedTitle("p", "id", images, titles("", "")); got != "Image review" {
		t.Fatalf("no fallback, title must stay: %q", got)
	}
	if !titleIsWeak("Ship today", "", "ship today!", true) || titleIsWeak("Fix the parser bug", "", "fix the bug", true) {
		t.Fatal("restatement is one-way containment")
	}
	if titleIsWeak("Image review", "Image review", "[Image #1]", true) {
		t.Fatal("a /rename is never weak")
	}
}
