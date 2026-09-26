package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/transcript"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	"github.com/sainteye/clawdline/internal/domain/icon"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// A Feature Root's first message is a launch line that says where its brief
// is. Named from that line, every root was "read the task brief"; named from
// the assignment the daemon stored, it is the feature.
func TestNamingReadsTheAssignmentNotItsLaunchLine(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := context.Background()
	at := time.Unix(1_790_000_000, 0)
	id := "10000000-0000-4000-8000-00000000a001"
	a := orchestrator.RootAssignment{ID: id, Assistant: "claude", ProjectDir: "/project-a", Label: "Board label",
		State: "briefed", CreatedAt: at.Unix(),
		Assignment: orchestrator.Assignment{Objective: "Smart naming does not work", Scope: "The error in the picture",
			Constraints: "Own only this item", RelevantReferences: "none", Acceptance: "Land and deploy"}}
	body, _ := json.Marshal(a)
	if err := st.CreateOpened(ctx, store.TableRootAssignments, store.Opened{ID: id, State: a.State,
		Record: body, CreatedAt: at, UpdatedAt: at}, nil); err != nil {
		t.Fatal(err)
	}
	s := &Server{store: st, broker: &orchestrator.Broker{Store: st, Dir: dir}}

	line := orchestrator.AssignmentLine(id, filepath.Join(dir, "root-assignments", id, "ASSIGNMENT.md"))
	got := s.resolveWrapper(ctx, line)
	for _, want := range []string{"Board label", "Smart naming does not work", "The error in the picture"} {
		if !strings.Contains(got, want) {
			t.Fatalf("resolved %q, missing %q", got, want)
		}
	}
	if strings.Contains(got, "ASSIGNMENT.md") || strings.Contains(got, "Land and deploy") {
		t.Fatalf("resolved %q still carries the launch line or the acceptance", got)
	}

	unknown := orchestrator.AssignmentLine("10000000-0000-4000-8000-00000000a002", "/nowhere/ASSIGNMENT.md")
	if got := s.resolveWrapper(ctx, unknown); got != unknown {
		t.Fatalf("an assignment this daemon never stored was rewritten to %q", got)
	}
	if got := s.resolveWrapper(ctx, "Fix the login page"); got != "Fix the login page" {
		t.Fatalf("an ordinary request was rewritten to %q", got)
	}
}

func TestNamingTextKeepsTheNewestRequestsAndNoSecret(t *testing.T) {
	limit := int(capacity.Default(capacity.NamingContextBytes))
	if limit != namingContextLimit {
		t.Fatalf("registered %d, source says %d", limit, namingContextLimit)
	}
	var later []string
	for i := 0; i < 40; i++ {
		later = append(later, fmt.Sprintf("request %02d %s", i, strings.Repeat(".", 600)))
	}
	later[len(later)-1] = "the newest request <system-reminder>machine words</system-reminder>"
	text := namingText(namingContext{
		First:     "You are a Clawdline CHILD agent for task x. TASK_SECRET=s3cr3t-value",
		Later:     later,
		LastReply: "the latest reply",
	})
	if len(text) > limit+128 {
		t.Fatalf("naming text is %d bytes; the budget is %d", len(text), limit)
	}
	for _, gone := range []string{"s3cr3t-value", "machine words", later[0]} {
		if strings.Contains(text, gone) {
			t.Fatalf("naming text still carries %.40q", gone)
		}
	}
	for _, kept := range []string{"the newest request", "the latest reply", "<opening_request>"} {
		if !strings.Contains(text, kept) {
			t.Fatalf("naming text lost %q", kept)
		}
	}
	if strings.Index(text, later[len(later)-2]) > strings.Index(text, "the newest request") {
		t.Fatal("later requests are not oldest first")
	}
}

// Through the route: the model reads what the session became, the opening
// request once, and only the newest reply.
func TestSmartTitleNamesFromTheWholeSession(t *testing.T) {
	item := session.Session{ID: "%44", Backend: session.BackendTmux, Assistant: session.AssistantClaude,
		ConversationID: "conversation-44", State: session.StateIdle}
	s := paneServer(t, &pane{s: item})
	s.cfg = config.Config{Dir: filepath.Join(t.TempDir(), "clawdline-next")}
	s.icons = &icon.Registry{}
	if _, err := nextconfig.Open(s.cfg.Dir).Set(map[string]any{"auto_name_assistant": "claude"}); err != nil {
		t.Fatal(err)
	}
	s.firstSessionRequest = func(session.Session) (string, error) { return "look at this bug", nil }
	s.sessionTailRead = func(session.Session) (transcript.Page, error) {
		return transcript.Page{Entries: []transcript.Entry{
			{Kind: transcript.KindUser, Text: "look at this bug"},
			{Kind: transcript.KindAssistant, Text: "an early reply"},
			{Kind: transcript.KindUser, Text: "the titles are poor; fix the naming model"},
			{Kind: transcript.KindAssistant, Text: "naming now reads the whole session"},
		}}, nil
	}
	var asked string
	s.nameSession = func(_ context.Context, text, _ string) (string, error) {
		asked = text
		return "Better session titles", nil
	}
	if got := act(t, s, "smart-title", item.ID, "smart-44", `{}`); got.Code != http.StatusOK {
		t.Fatalf("smart-title: %d %s", got.Code, got.Body)
	}
	if strings.Count(asked, "look at this bug") != 1 {
		t.Fatalf("the opening request is not there exactly once:\n%s", asked)
	}
	for _, want := range []string{"fix the naming model", "naming now reads the whole session"} {
		if !strings.Contains(asked, want) {
			t.Fatalf("the model was not given %q:\n%s", want, asked)
		}
	}
	if strings.Contains(asked, "an early reply") {
		t.Fatalf("an older reply was sent:\n%s", asked)
	}
}
