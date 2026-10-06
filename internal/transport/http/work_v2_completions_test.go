package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/taskdir"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/domain/work"
	"github.com/sainteye/clawdline/internal/productcopy"
)

func TestBoardCompletionPushUsesNineProductLanguagesAndPreservesItemTitle(t *testing.T) {
	v := app.WorkV2View{Item: work.ItemV2{ID: "work-1", Title: "修好通知"},
		Assignments: []work.AssignmentV2{{State: "active", SessionID: "session-1", TerminalID: "terminal-1"}}}
	for _, language := range productcopy.Languages {
		e := workV2CompletionEffect(v, "session-1", language)
		var p orchestrator.WorkItemCompletedPush
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			t.Fatal(err)
		}
		if p.Title == "" || p.Body == "" || p.WorkID != "work-1" || p.Terminal != "terminal-1" ||
			p.Tag != "work-item-work-1" || !strings.Contains(p.Body, v.Item.Title) {
			t.Errorf("%s: %+v", language, p)
		}
	}
	var ambiguous orchestrator.WorkItemCompletedPush
	if err := json.Unmarshal(workV2CompletionEffect(v, "session-1", "zh").Payload, &ambiguous); err != nil ||
		ambiguous.Title != "Board item completed" {
		t.Fatalf("ambiguous language did not use English: %+v %v", ambiguous, err)
	}
}

// The pull path for a finished child. A root reads its own session-todos at
// every turn boundary; a completion it has not acknowledged is on that answer
// until it is, so a root that never saw the typed line — it was showing a
// question, or the notice gave up — still learns of it at its next turn.
func TestSessionTodosListAnUnacknowledgedCompletionUntilItIsAcked(t *testing.T) {
	s, p := ownTodoServer(t)
	ctx := context.Background()
	b := s.broker
	const id = "c6f30000-0000-4000-8000-0000000000a1"
	r := orchestrator.Record{Protocol: orchestrator.Protocol, ID: id, Kind: "custom", Assistant: "claude",
		Title: "the relay reconnects", ProjectDir: "/p", State: orchestrator.StateBriefed, CreatedAt: time.Now(),
		Root: &orchestrator.RootRef{SessionID: p.s.ConversationID, Assistant: "claude"}}
	body, _ := json.Marshal(r)
	if _, err := s.store.CreateBrokerTask(ctx, store.BrokerRow{ID: id, Project: "/p", Assistant: "claude",
		State: string(r.State), CreatedAt: r.CreatedAt, SecretHash: orchestrator.HashSecret("s"),
		Record: body}, nil); err != nil {
		t.Fatal(err)
	}
	done, err := b.Settle(ctx, id, orchestrator.StateSuccess, "done", &taskdir.Result{Status: "success", Summary: "done"})
	if err != nil || done.Notice == nil {
		t.Fatalf("settle: %v %+v", err, done.Notice)
	}

	type page struct {
		Completions []unacknowledgedCompletionWire `json:"unacknowledged_completions"`
		Unknown     bool                           `json:"unacknowledged_completions_unknown"`
	}
	read := func() page {
		t.Helper()
		rec := httptest.NewRecorder()
		s.workV2Route(rec, agentWorkV2Request(http.MethodGet, "/v1/work/v2/agent/session-todos/"+p.s.ConversationID, "", ""))
		if rec.Code != http.StatusOK {
			t.Fatalf("read: %d %s", rec.Code, rec.Body)
		}
		var out page
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}

	got := read()
	if got.Unknown || len(got.Completions) != 1 {
		t.Fatalf("an unacknowledged completion is not on its root's list: %+v", got)
	}
	c := got.Completions[0]
	if c.TaskID != id || c.Title != "the relay reconnects" || c.State != string(orchestrator.StateSuccess) ||
		c.NoticeID != done.Notice.ID || c.NoticeState != string(orchestrator.NoticePending) ||
		c.ResultPath != b.ResultPath(id) || c.AckPath != "/v1/orchestrator/tasks/"+id+"/completion/ack" {
		t.Fatalf("the listed completion: %+v", c)
	}

	if _, err := b.Acknowledge(ctx, id, done.Notice.ID); err != nil {
		t.Fatal(err)
	}
	if after := read(); after.Unknown || len(after.Completions) != 0 {
		t.Fatalf("an acknowledged completion is still listed: %+v", after)
	}
}
