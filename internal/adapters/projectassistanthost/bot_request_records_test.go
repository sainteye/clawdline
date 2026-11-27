package projectassistanthost

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	br "github.com/sainteye/clawdline/internal/domain/botrequest"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// brRepo is a Project directory holding one commit, and that commit's id.
func brRepo(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"-c", "user.name=Reviewer", "-c", "user.email=reviewer@example.invalid", "commit", "-q", "--allow-empty", "-m", "draft"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	return dir, strings.TrimSpace(string(out))
}

// A card keeps its Project's path, and a verdict's commit is resolved in that
// Project to a full id; one that does not resolve there is refused
// (docs/bot-request-cards.md §4).
func TestBotRequestKeepsItsPathAndResolvesAVerdictCommit(t *testing.T) {
	s := brOpen(t)
	dir, head := brRepo(t)
	look := fakeTurns{
		turns:     map[string]pa.Turn{"t1": {ID: "t1", MessageID: "msg-1", Origin: pa.UserOrigin}},
		exchanges: map[string]pa.Exchange{"x1": {ID: "x1", Source: pa.ExchangeEndpoint{Key: brBot("lead").Key, ConversationID: "c"}, SourceTurnID: "t1"}},
	}
	lead := botRequestTurn{Bot: brBot("lead"), Conversation: "c", Turn: look.turns["t1"], Directory: dir}
	rev := botRequestTurn{Bot: brBot("rev"), Conversation: "c2", Turn: pa.Turn{ID: "t2", MessageID: "m2", Origin: pa.BotRequestOrigin, ExchangeID: "x1"}, Directory: dir}
	out, err := brCall(t, s, look, lead, "bot_request_create", map[string]any{"idempotency_key": "k", "title": "Ship it", "checklist": []string{"review"}})
	if err != nil {
		t.Fatal(err)
	}
	card := out.(struct {
		botRequestReply
		Created bool `json:"created"`
	}).Card
	if card.ProjectPath != dir {
		t.Fatalf("project_path %q, want %q", card.ProjectPath, dir)
	}
	if _, err := brCall(t, s, look, rev, "bot_request_create", map[string]any{"idempotency_key": "j", "title": "Ship it", "checklist": []string{"review"}}); err != nil {
		t.Fatal(err)
	}
	handed, err := brCall(t, s, look, lead, "bot_request_handoff", map[string]any{"idempotency_key": "h", "card_id": card.ID, "expected_version": card.Version + 1, "to_bot": "rev", "role": br.RoleReviewer, "note": "review the draft"})
	if err != nil {
		t.Fatal(err)
	}
	version := handed.(botRequestReply).Card.Version
	verdict := func(key, commit string, version int64) (botRequestReply, error) {
		raw := mustJSON(t, map[string]any{"idempotency_key": key, "card_id": card.ID, "expected_version": version, "commit": commit, "verdict": br.VerdictReject, "text": "missing tests"})
		out, err := botRequestTool(context.Background(), s, look, rev, "m", "verdict-"+key, brNow, "bot_request_verdict", raw)
		if err != nil {
			return botRequestReply{}, err
		}
		return out.(botRequestReply), nil
	}
	if _, err := verdict("v0", "0000000000000000000000000000000000000001", version); br.CodeOf(err) != br.CodeInvalid {
		t.Fatalf("an unknown commit: got %v, want %s", err, br.CodeInvalid)
	}
	got, err := verdict("v1", head[:9], version)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(got.Card.Verdicts); n != 1 || got.Card.Verdicts[0].Commit != head {
		t.Fatalf("verdicts %+v, want one on %s", got.Card.Verdicts, head)
	}
	if _, err := verdict("v2", "", got.Card.Version); err != nil {
		t.Fatalf("a verdict without a commit: %v", err)
	}
	away := rev
	away.Directory = ""
	if _, err := brCall(t, s, look, away, "bot_request_verdict", map[string]any{"idempotency_key": "v3", "card_id": card.ID, "expected_version": got.Card.Version + 1, "commit": head, "verdict": br.VerdictPass}); br.CodeOf(err) != br.CodeInvalid {
		t.Fatalf("a commit in a Project with no directory: got %v", err)
	}
}

// An entry a Bot adds while checking is recorded as checklist.added with its
// label, and only an actual mark is recorded as checklist.marked (§4).
func TestBotRequestCheckRecordsAnAddedEntry(t *testing.T) {
	s := brOpen(t)
	look := fakeTurns{turns: map[string]pa.Turn{}, exchanges: map[string]pa.Exchange{}}
	lead := botRequestTurn{Bot: brBot("lead"), Conversation: "c", Turn: pa.Turn{ID: "t1", MessageID: "msg", Origin: pa.UserOrigin}}
	out, err := brCall(t, s, look, lead, "bot_request_create", map[string]any{"idempotency_key": "k", "title": "Ship it", "checklist": []string{"build"}})
	if err != nil {
		t.Fatal(err)
	}
	card := out.(struct {
		botRequestReply
		Created bool `json:"created"`
	}).Card
	checked, err := brCall(t, s, look, lead, "bot_request_check", map[string]any{"idempotency_key": "c1", "card_id": card.ID, "expected_version": card.Version,
		"entries": []br.Mark{{Label: "docs"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := brCall(t, s, look, lead, "bot_request_check", map[string]any{"idempotency_key": "c2", "card_id": card.ID, "expected_version": checked.(botRequestReply).Card.Version,
		"entries": []br.Mark{{Label: "tests", State: br.EntryDone, Note: "green"}}}); err != nil {
		t.Fatal(err)
	}
	events, err := s.BotRequestEvents(context.Background(), card.ID)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, e := range events {
		kinds = append(kinds, e.Kind+":"+e.Detail)
	}
	want := []string{br.EventCreated + ":", br.EventChecklistAdded + ":docs", br.EventChecklistAdded + ":tests", br.EventChecklistMark + ":"}
	if strings.Join(kinds, " ") != strings.Join(want, " ") {
		t.Fatalf("events %v, want %v", kinds, want)
	}
}
