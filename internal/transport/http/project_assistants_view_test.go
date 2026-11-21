package http

import (
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/app/cloudops"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type paViewPort struct {
	view   app.ReadView
	query  app.ReadViewQuery
	calls  int
	during func()
}

type paViewBodyProbe struct{ reads int }

func (p *paViewBodyProbe) Read([]byte) (int, error) { p.reads++; return 0, io.EOF }

func (p *paViewPort) Read(_ context.Context, q app.ReadViewQuery) (app.ReadView, error) {
	p.calls++
	p.query = q
	if p.during != nil {
		p.during()
	}
	return p.view, nil
}

func paViewFixture(t *testing.T) (*paHTTPFixture, *paViewPort) {
	t.Helper()
	f := newPAHTTPFixture(t)
	f.create(t)
	c := f.repo.conversation
	p := &paViewPort{view: app.ReadView{Mode: pa.ReadViewSnapshot, SourceEpoch: "incarnation", Head: app.ReadViewHead{Key: f.key, Project: f.project, Conversation: c,
		CurrentFence: pa.Fence{Key: f.key, Project: f.project, ConversationID: c.ID}, EventFrontier: 602}, Snapshot: &app.ReadSnapshot{Bot: f.repo.bot}}}
	var err error
	f.service, err = app.NewService(app.ServiceOptions{Repository: f.repo, ReadViews: p, ReadViewByteLimit: cloudops.ReadPlaintextBytesLimit, Memory: f.memory, Events: f.events, Runtime: f.runtime, Projects: f.projects,
		Authorize: func(context.Context, pa.BotKey, bool) (app.Authority, error) { return f.authority, nil }, Clock: func() time.Time { return f.now }, ID: func() string { return "unused" }})
	if err != nil {
		t.Fatal(err)
	}
	f.handler = newProjectAssistantHTTP(projectAssistantHTTPOptions{Service: f.service, MachineID: f.key.MachineID, Status: func(context.Context) (pa.SourceStatus, error) { return f.source, nil }})
	return f, p
}

func paViewURL(key pa.BotKey, conversation string, mode pa.ReadViewMode, epoch, cursor string, after int64, size int) string {
	q := url.Values{"machine_id": {key.MachineID}, "conversation_id": {conversation}, "mode": {string(mode)}, "source_epoch": {epoch}, "cursor": {cursor}, "after_seq": {strconv.FormatInt(after, 10)}, "page_size": {strconv.Itoa(size)}}
	return "/v1/project-assistants/" + url.PathEscape(key.BotID) + "/view?" + q.Encode()
}

func TestProjectAssistantViewStrictHTTPAndFinalPublicationFence(t *testing.T) {
	f, p := paViewFixture(t)
	path := paViewURL(f.key, f.repo.conversation.ID, pa.ReadViewSnapshot, "", "", 0, 100)
	w := f.request(t, "GET", path, nil)
	paAssertHTTPContract(t, w, 200, "ProjectAssistantReadViewResponse")
	if p.query.AuthorizationBinding == "" || p.query.ByteLimit != cloudops.ReadPlaintextBytesLimit || f.repo.exports != 0 || len(f.runtime.submissions) != 0 {
		t.Fatal("read bypassed trusted bounded port")
	}
	for _, test := range []struct{ name, path, body string }{
		{"unknown", path + "&extra=1", ""}, {"duplicate", path + "&cursor=", ""}, {"missing", strings.Replace(path, "cursor=&", "", 1), ""},
		{"body", path, "x"}, {"snapshot epoch", strings.Replace(path, "source_epoch=", "source_epoch=x", 1), ""},
		{"negative", strings.Replace(path, "after_seq=0", "after_seq=-1", 1), ""}, {"fraction", strings.Replace(path, "page_size=100", "page_size=1.5", 1), ""},
		{"snapshot page", strings.Replace(path, "page_size=100", "page_size=256", 1), ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := p.calls
			r := httptest.NewRequest("GET", test.path, strings.NewReader(test.body))
			w := httptest.NewRecorder()
			f.handler.ServeHTTP(w, r)
			paAssertHTTPFailure(t, w, 400, pa.Invalid)
			if p.calls != before {
				t.Fatal("bad query reached SQL port")
			}
		})
	}
	for _, length := range []int64{-1, 1} {
		body := &paViewBodyProbe{}
		r := httptest.NewRequest("GET", path, nil)
		r.Body, r.ContentLength = io.NopCloser(body), length
		if length == -1 {
			r.TransferEncoding = []string{"chunked"}
		}
		w := httptest.NewRecorder()
		before := p.calls
		f.handler.ServeHTTP(w, r)
		paAssertHTTPFailure(t, w, 400, pa.Invalid)
		if body.reads != 0 || p.calls != before {
			t.Fatal("forbidden GET body framing started stream/SQL reads")
		}
	}
	// The final status callback runs after complete JSON packing. Change only
	// principal/reader/revision/ceiling, leaving MachineID and readability true.
	for _, change := range []string{"principal", "reader", "revision", "downgrade"} {
		t.Run("publication/"+change, func(t *testing.T) {
			f, _ := paViewFixture(t)
			calls := 0
			f.handler = newProjectAssistantHTTP(projectAssistantHTTPOptions{Service: f.service, MachineID: f.key.MachineID, Status: func(context.Context) (pa.SourceStatus, error) {
				calls++
				if calls == 2 {
					switch change {
					case "principal":
						f.authority.PrincipalID = "switched"
					case "reader":
						f.authority.ReaderID = "switched"
					case "revision":
						f.authority.AuthorizationRevision = "changed"
					case "downgrade":
						f.authority.Source.Writable = false
					}
				}
				return f.source, nil
			}})
			w := f.request(t, "GET", path, nil)
			paAssertHTTPFailure(t, w, 403, pa.Revoked)
			if strings.Contains(w.Body.String(), "Synthetic instructions") || strings.Contains(w.Body.String(), `"head"`) {
				t.Fatal("encoded content escaped publication fence")
			}
		})
	}
}

func paPackFixture(t *testing.T, mode pa.ReadViewMode) (pa.ReadViewRequest, app.ReadViewResult) {
	t.Helper()
	f, p := paViewFixture(t)
	p.view.Mode = mode
	if mode != pa.ReadViewSnapshot {
		p.view.Snapshot = nil
	}
	return pa.ReadViewRequest{Mode: mode, AfterSeq: 602}, app.ReadViewResult{View: p.view, Source: f.source}
}

func paPackMessage(id, text, cursor string) app.ReadViewItem {
	return app.ReadViewItem{ID: id, Kind: app.ReadItemMessage, Message: &pa.Message{ID: id, Text: text}, AfterCursor: cursor}
}
func paPackDecode(t *testing.T, q pa.ReadViewRequest, r app.ReadViewResult) contract.ProjectAssistantReadViewResponse {
	t.Helper()
	b, err := paPackReadView(q, r)
	if err != nil {
		t.Fatal(err)
	}
	size, err := cloudops.ProjectAssistantReadReplySize("read:"+strings.Repeat("<", 128), 200, b)
	if err != nil || len(b) > cloudops.ReadPlaintextBytesLimit || size > cloudops.ReadPlaintextBytesLimit {
		t.Fatalf("oversized body: %d/%d %v", len(b), size, err)
	}
	var out contract.ProjectAssistantReadViewResponse
	if err = json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestProjectAssistantViewPackingKeepsWholeRowsAndHonestContinuation(t *testing.T) {
	q, r := paPackFixture(t, pa.ReadViewHistory)
	r.View.Items = []app.ReadViewItem{paPackMessage("first", "small", "after-first"), paPackMessage("huge", strings.Repeat("x", cloudops.ReadPlaintextBytesLimit), "after-huge")}
	out := paPackDecode(t, q, r)
	if len(out.Items) != 1 || !out.HasMore || out.NextCursor != "after-first" {
		t.Fatalf("bad prefix: %+v", out)
	}
	r.View.Items = r.View.Items[1:]
	if _, err := paPackReadView(q, r); pa.Code(err) != pa.CapacityFull {
		t.Fatalf("oversized indivisible row: %v", err)
	}
	r.View.Items = []app.ReadViewItem{paPackMessage("first", "small", "after-first"), {Failure: &pa.Failure{Code: pa.CapacityFull, Field: "raw_item"}}}
	out = paPackDecode(t, q, r)
	if len(out.Items) != 1 || !out.HasMore || out.NextCursor != "after-first" {
		t.Fatal("lost producer failure prefix")
	}
	for _, code := range []pa.ErrorCode{pa.ScopeDenied, pa.StaleGeneration, pa.CursorExpired, pa.Revoked} {
		r.View.Items[1].Failure = &pa.Failure{Code: code, Field: "foreign_or_stale"}
		if _, err := paPackReadView(q, r); pa.Code(err) != code {
			t.Fatalf("typed stale/scope failure swallowed by preceding prefix: %v", err)
		}
	}
	r.View.Items = []app.ReadViewItem{paPackMessage("a", "a", ""), paPackMessage("b", "b", "good")}
	r.View.HasMore = true
	r.View.Items[0].CursorFailure = &pa.Failure{Code: pa.CapacityFull, Field: "cursor"}
	out = paPackDecode(t, q, r)
	if len(out.Items) != 2 || out.NextCursor != "good" || !out.HasMore {
		t.Fatal("later fitting cursor did not rescue prefix")
	}
	r.View.Items[0].Message = nil
	r.View.Items[0].Work = &pa.WorkLink{}
	if _, err := paPackReadView(q, r); pa.Code(err) != pa.Invalid {
		t.Fatal("kind did not match sole payload")
	}
}

func TestProjectAssistantViewMandatorySnapshotDetailAndEventFrontier(t *testing.T) {
	q, r := paPackFixture(t, pa.ReadViewSnapshot)
	out := paPackDecode(t, q, r)
	if out.Items == nil || out.Snapshot == nil || out.Snapshot.PendingTurns == nil || out.Snapshot.QueuedMessages == nil || out.Head.EventFrontier != 602 {
		t.Fatal("null or missing required snapshot data")
	}
	r.View.Snapshot.LastTurn = &app.ReadTurnSummary{ID: "interrupted", State: pa.TurnUnknown, FailureCode: pa.ExecutionEffectUnknown, FailureField: "prompt_timeout"}
	out = paPackDecode(t, q, r)
	if out.Snapshot.LastTurn == nil || out.Snapshot.LastTurn.FailureField != "prompt_timeout" || out.Snapshot.LastTurn.State != contract.ProjectAssistantTurnStateUnknown {
		t.Fatal("Cloud snapshot lost the interruption reason or changed its effect state")
	}
	r.View.Snapshot.Bot.Instructions.Text = strings.Repeat("x", cloudops.ReadPlaintextBytesLimit)
	if _, err := paPackReadView(q, r); pa.Code(err) != pa.CapacityFull {
		t.Fatalf("mandatory snapshot truncated: %v", err)
	}
	q, r = paPackFixture(t, pa.ReadViewDetail)
	r.View.Items = []app.ReadViewItem{{ID: "trace", Kind: app.ReadItemTrace, Trace: &pa.Trace{Text: strings.Repeat("x", cloudops.ReadPlaintextBytesLimit)}}}
	if _, err := paPackReadView(q, r); pa.Code(err) != pa.CapacityFull {
		t.Fatalf("detail truncated: %v", err)
	}
	q, r = paPackFixture(t, pa.ReadViewEvents)
	r.View.Head.EventFrontier = 900
	r.View.Items = []app.ReadViewItem{{ID: "603", Kind: app.ReadItemEvent, Event: &pa.Event{Seq: 603}}, {ID: "604", Kind: app.ReadItemEvent, Event: &pa.Event{Seq: 604, Message: &pa.Message{Text: strings.Repeat("x", cloudops.ReadPlaintextBytesLimit)}}}}
	out = paPackDecode(t, q, r)
	if len(out.Items) != 1 || out.NextSeq != 603 || !out.HasMore || out.Head.EventFrontier != 900 {
		t.Fatal("event prefix advanced to head frontier")
	}
	r.View.Items = nil
	r.View.NextSeq = 900
	out = paPackDecode(t, q, r)
	if out.NextSeq != 602 || out.HasMore {
		t.Fatal("empty event page advanced past request cursor")
	}
}

func TestProjectAssistantViewExactCloudByteBoundary(t *testing.T) {
	q, r := paPackFixture(t, pa.ReadViewHistory)
	r.View.Items = []app.ReadViewItem{paPackMessage("edge", "", "unused")}
	b, err := paPackReadView(q, r)
	if err != nil {
		t.Fatal(err)
	}
	size, err := cloudops.ProjectAssistantReadReplySize("read:"+strings.Repeat("<", 128), 200, b)
	if err != nil {
		t.Fatal(err)
	}
	remaining := cloudops.ReadPlaintextBytesLimit - size
	r.View.Items[0].Message.Text = strings.Repeat("x", remaining)
	paPackDecode(t, q, r)
	r.View.Items[0].Message.Text += "x"
	if _, err = paPackReadView(q, r); pa.Code(err) != pa.CapacityFull {
		t.Fatalf("one-byte overflow published: %v", err)
	}

	escaped := "<台灣>&\u2028\u2029\n"
	r.View.Items = []app.ReadViewItem{paPackMessage("first", escaped, "next"), paPackMessage("second", escaped, "")}
	b, err = paPackReadView(q, r)
	if err != nil {
		t.Fatal(err)
	}
	size, err = cloudops.ProjectAssistantReadReplySize("read:"+strings.Repeat("<", 128), 200, b)
	if err != nil {
		t.Fatal(err)
	}
	r.View.Items[1].Message.Text += strings.Repeat("x", cloudops.ReadPlaintextBytesLimit-size)
	if out := paPackDecode(t, q, r); len(out.Items) != 2 || out.Items[0].Message.Text != escaped || out.HasMore {
		t.Fatal("exactly fitting escaped page lost an item or added a continuation")
	}
	r.View.Items[1].Message.Text += "x"
	if out := paPackDecode(t, q, r); len(out.Items) != 1 || out.NextCursor != "next" || !out.HasMore {
		t.Fatal("one-byte overflow did not stop at the previous continuation")
	}
}

func TestProjectAssistantViewPublishesOnlyMatchingNativeContext(t *testing.T) {
	f, port := paViewFixture(t)
	port.view.Head.Conversation.ActiveTurnID = "running"
	state := app.NativeContextState{Fence: port.view.Head.CurrentFence, TurnID: "running", Model: "Fixture Pro", UsageKnown: true, UsedTokens: 0, WindowTokens: 6000}
	f.handler = newProjectAssistantHTTP(projectAssistantHTTPOptions{Service: f.service, MachineID: f.key.MachineID,
		NativeContext: func(_ context.Context, fence pa.Fence) (app.NativeContextState, error) {
			if fence != port.view.Head.CurrentFence {
				t.Fatalf("usage read for wrong fence: %+v", fence)
			}
			return state, nil
		},
		Status: func(context.Context) (pa.SourceStatus, error) { return f.source, nil },
	})
	path := paViewURL(f.key, f.repo.conversation.ID, pa.ReadViewSnapshot, "", "", 0, 100)
	read := func() contract.ProjectAssistantReadViewHead {
		w := f.request(t, "GET", path, nil)
		paAssertHTTPContract(t, w, 200, "ProjectAssistantReadViewResponse")
		var reply contract.ProjectAssistantReadViewResponse
		if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil {
			t.Fatal(err)
		}
		return reply.Head
	}
	if got := read().ContextUsage; got == nil || *got != (contract.ProjectAssistantContextUsage{Live: true, TurnID: "running", UsedTokens: 0, WindowTokens: 6000}) {
		t.Fatalf("known zero reading was lost: %+v", got)
	}
	state.UsageKnown = false
	if got := read(); got.ContextUsage != nil || got.ActiveModel == nil || *got.ActiveModel != (contract.ProjectAssistantActiveModel{TurnID: "running", Name: "Fixture Pro"}) {
		t.Fatalf("the observed model was lost without token metadata: %+v", got)
	}
	state.UsageKnown = true
	state.TurnID = "older-turn"
	if got := read(); got.ContextUsage != nil || got.ActiveModel != nil {
		t.Fatalf("metadata from a different execution escaped: %+v", got)
	}
	port.view.Head.Conversation.ActiveTurnID = ""
	port.view.Head.ContextUsage = &app.ReadContextUsage{TurnID: "completed", UsedTokens: 42, WindowTokens: 6000}
	if got := read().ContextUsage; got == nil || *got != (contract.ProjectAssistantContextUsage{TurnID: "completed", UsedTokens: 42, WindowTokens: 6000}) {
		t.Fatalf("last source reading was lost: %+v", got)
	}
}
