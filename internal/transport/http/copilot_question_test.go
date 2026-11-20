package http

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/taskdir"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func questionPages(t *testing.T, revision string, reads *[]string) func(string, any) bool {
	return func(path string, into any) bool {
		*reads = append(*reads, path)
		table := "| option | speed | cost |\n|---|---|---|\n| A | faster | higher |"
		data := map[string]any{"signature": revision, "entries": []map[string]any{{"role": "user", "text": "Compare options"}, {"role": "assistant", "text": "First reason"}}}
		if !strings.Contains(path, "before=") {
			data["entries"] = []map[string]any{{"role": "assistant", "text": table}}
			data["nextBefore"] = 100
		}
		raw, _ := json.Marshal(data)
		return json.Unmarshal(raw, into) == nil
	}
}

func TestQuestionContextReusesSourcesAndInvalidatesChangedRevisions(t *testing.T) {
	var b copilotQuestionBook
	var reads []string
	now := time.Unix(1000, 0)
	c, key, err := b.context(questionPages(t, "v1", &reads), "%1", "conversation-a", "", 2, now, nil)
	if err != nil || c.Replies != 2 || !c.Complete || len(reads) != 2 {
		t.Fatalf("%+v %v reads %v", c, err, reads)
	}
	if !strings.Contains(c.Messages[len(c.Messages)-1].Text, "| A | faster | higher |") {
		t.Fatal("table structure lost")
	}
	_, again, err := b.context(questionPages(t, "v1", &reads), "%1", "conversation-a", key, 2, now.Add(time.Second), nil)
	if err != nil || again != key || len(reads) != 3 {
		t.Fatalf("context not reused: %v %v", err, reads)
	}
	_, changed, err := b.context(questionPages(t, "v2", &reads), "%1", "conversation-a", key, 2, now.Add(2*time.Second), nil)
	if err != nil || changed == key || len(reads) != 5 {
		t.Fatalf("changed source reused: %v %v", err, reads)
	}
	_, other, err := b.context(questionPages(t, "v1", &reads), "%1", "conversation-b", key, 2, now.Add(3*time.Second), nil)
	if err != nil || other == key {
		t.Fatal("context crossed conversations")
	}
}

func TestQuestionContextLimitsAndCoverageAreObservable(t *testing.T) {
	var b copilotQuestionBook
	var reads []string
	now := time.Unix(1000, 0)
	c, _, err := b.context(questionPages(t, "v1", &reads), "%1", "c", "", 5, now, nil)
	if err != nil || c.Complete || c.Replies != 2 {
		t.Fatalf("missing replies not marked: %+v %v", c, err)
	}
	b.contexts = map[string]copilotQuestionContext{}
	for i := 0; i < maxCopilotQuestionContexts; i++ {
		b.contexts[strings.Repeat("x", i+1)] = copilotQuestionContext{At: now}
	}
	if _, _, err = b.context(questionPages(t, "v1", &reads), "%1", "c", "", 1, now, nil); err == nil {
		t.Fatal("full context store accepted another entry")
	}
	if _, _, err = b.context(questionPages(t, "v1", &reads), "%1", "c", "", 1, now.Add(maxCopilotQuestionAge+time.Second), nil); err != nil {
		t.Fatal("expired contexts were not reclaimed", err)
	}
}

func TestQuestionInputBudgetAndEvidenceRejectUnsupportedQuotes(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{"entries": []map[string]any{{"role": "user", "text": "Why?"}, {"role": "assistant", "text": strings.Repeat("字", maxCopilotQuestionInputRunes+1)}}})
	var page copilotQuestionPage
	json.Unmarshal(raw, &page)
	messages, _, complete := questionMessages([]copilotQuestionPage{page}, 1)
	total := 0
	for _, m := range messages {
		total += utf8.RuneCountInString(m.Text)
	}
	if complete || total > maxCopilotQuestionInputRunes {
		t.Fatalf("complete=%v total=%d", complete, total)
	}
	c := copilotQuestionContext{Messages: []copilotQuestionMessage{{Ref: 1, Text: "Option A is faster; cost is unknown."}}}
	a := copilotQuestionAnswer{Answer: "A is faster, but cost is unknown.", Evidence: []copilotQuestionEvidence{{Ref: 1, Quote: "Option A is faster"}}}
	if questionAnswerIssue(a, c) != "" {
		t.Fatal("valid evidence refused")
	}
	a.Evidence[0].Quote = "A is cheaper"
	if questionAnswerIssue(a, c) != "quote_not_found" {
		t.Fatal("invented evidence accepted")
	}
	a.Evidence[0] = copilotQuestionEvidence{Ref: 2, Quote: "Option A is faster"}
	if questionAnswerIssue(a, c) != "reference_invalid" {
		t.Fatal("unknown source accepted")
	}
}

type questionTransport func(*http.Request) (*http.Response, error)

func (f questionTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type questionRemote struct {
	reads int
	pages map[int64]contract.TranscriptPage
}

func (*questionRemote) SendRemote(context.Context, session.Session, string, func() error) error {
	panic("analysis cannot send")
}
func (r *questionRemote) Page(_ context.Context, s session.Session, _ int, before, _ int64, _ string) (contract.TranscriptPage, error) {
	r.reads++
	if page, ok := r.pages[before]; ok {
		return page, nil
	}
	return contract.TranscriptPage{ID: s.ID, Signature: "question-v1", Entries: []contract.TranscriptEntry{{Role: "user", Text: "Compare A and B"}, {Role: "assistant", Text: "A is faster; cost is unknown."}}}, nil
}

func TestQuestionUsesTheOwningReviewReceiptAndFallsBackToItsExactSource(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	dir := t.TempDir()
	broker := &orchestrator.Broker{Store: st, Tasks: taskdir.New(dir), Dir: dir}
	receipt := func(id, owner, summary string) contract.TranscriptEntry {
		t.Helper()
		r := orchestrator.Record{ID: id, Kind: "review", Title: "Independent interface review",
			Protocol: orchestrator.Protocol, State: orchestrator.StateBriefed, CreatedAt: time.Now(),
			Root: &orchestrator.RootRef{SessionID: owner}}
		if _, err := st.CreateBrokerTask(ctx, store.BrokerRow{ID: id, Project: "/project",
			State: string(r.State), CreatedAt: r.CreatedAt, Record: mustJSON(t, r)}, nil); err != nil {
			t.Fatal(err)
		}
		settled, err := broker.Settle(ctx, id, orchestrator.StateSuccess, "done", &taskdir.Result{Status: "success", Summary: summary})
		if err != nil {
			t.Fatal(err)
		}
		return contract.TranscriptEntry{Role: "notice", Notice: &contract.TranscriptNotice{
			Kind: "task_finished", NoticeID: settled.Notice.ID, Task: &contract.TranscriptNoticeTask{ID: id},
		}}
	}
	want := "The reviewer found two stop controls and measured the shift at 40 px."
	owned := receipt("own-review", "conversation-a", want)
	foreign := receipt("foreign-review", "conversation-b", "Other Session review")
	forged := owned
	mismatched := *owned.Notice
	mismatched.NoticeID = "forged-receipt"
	forged.Notice = &mismatched

	row := session.Session{ID: "claude-remote:42:question", RemoteSessionID: "question", ConversationID: "conversation-a", PID: 42, Backend: session.BackendClaudeRemote, Assistant: session.AssistantClaude}
	remote := &questionRemote{pages: map[int64]contract.TranscriptPage{
		0: {ID: row.ID, Signature: "question-v1", NextBefore: 100, Entries: []contract.TranscriptEntry{
			{Role: "user", Text: "Fix the review findings."},
			{Role: "assistant", Text: "I fixed the controls and installed the build."},
		}},
		100: {ID: row.ID, Signature: "question-v1", Entries: []contract.TranscriptEntry{owned, foreign, forged}},
	}}
	s := &Server{cfg: config.Config{Dir: t.TempDir()}, store: st, broker: broker, inventory: app.Inventory{Process: rcProcesses{row}}, claudeRemote: remote}
	copilotCall(t, s, s.copilotSettingsRoute, http.MethodPost, "/v1/settings/copilot", `{"openai_key":"`+testCopilotKey+`"}`)
	original := http.DefaultClient
	defer func() { http.DefaultClient = original }()
	calls := 0
	http.DefaultClient = &http.Client{Transport: questionTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		var sent struct {
			Messages []struct{ Content string } `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&sent); err != nil {
			t.Fatal(err)
		}
		var input struct {
			Messages []copilotQuestionMessage `json:"messages"`
		}
		if err := json.Unmarshal([]byte(sent.Messages[1].Content), &input); err != nil {
			t.Fatal(err)
		}
		if len(input.Messages) != 3 || input.Messages[2].Role != "review" || input.Messages[2].Text != want {
			t.Fatalf("analysis did not receive exactly the owning review and main conversation: %+v", input.Messages)
		}
		answer := `{"answer":"It found an invented result.","evidence":[{"ref":3,"quote":"an invented quote"}],"unknowns":[],"needs_older":false}`
		reply, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": answer}}}, "usage": map[string]int{"prompt_tokens": 100, "completion_tokens": 30}})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(reply))), Header: http.Header{}}, nil
	})}
	rec := copilotCall(t, s, s.copilotQuestionRequestRoute, http.MethodPost, "/v1/voice/copilot-question",
		`{"session":"`+row.ID+`","conversation":"conversation-a","question":"What did the reviewer originally find?","replies":1,"allow_source_fallback":true}`)
	var got struct {
		Analysis json.RawMessage          `json:"analysis"`
		Failure  string                   `json:"analysis_failure"`
		Fallback []copilotQuestionMessage `json:"source_fallback"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || calls != 1 || remote.reads != 2 ||
		got.Analysis != nil || got.Failure != "quote_not_found" || len(got.Fallback) != 2 ||
		got.Fallback[0].Role != "review" || got.Fallback[0].Text != want {
		t.Fatalf("status %d calls %d reads %d: %s", rec.Code, calls, remote.reads, rec.Body)
	}
}
func TestQuestionRouteUsesOneModelRequestAndKeepsRawMessagesOutOfItsAnswer(t *testing.T) {
	row := session.Session{ID: "claude-remote:42:question", RemoteSessionID: "question", ConversationID: "conversation-a", PID: 42, Backend: session.BackendClaudeRemote, Assistant: session.AssistantClaude}
	remote := &questionRemote{}
	s := &Server{cfg: config.Config{Dir: t.TempDir()}, inventory: app.Inventory{Process: rcProcesses{row}}, claudeRemote: remote}
	copilotCall(t, s, s.copilotSettingsRoute, http.MethodPost, "/v1/settings/copilot", `{"openai_key":"`+testCopilotKey+`"}`)
	original := http.DefaultClient
	defer func() { http.DefaultClient = original }()
	calls := 0
	http.DefaultClient = &http.Client{Transport: questionTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		var sent map[string]any
		json.NewDecoder(r.Body).Decode(&sent)
		msgs := sent["messages"].([]any)
		input := msgs[1].(map[string]any)["content"].(string)
		if !strings.Contains(input, "Why A?") || !strings.Contains(input, "cost is unknown") {
			t.Error("question or originals missing", input)
		}
		answer := `{"answer":"A is faster, but the cost is unknown.","evidence":[{"ref":2,"quote":"A is faster"}],"unknowns":["cost"],"needs_older":false}`
		reply, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": answer}}}, "usage": map[string]int{"prompt_tokens": 100, "completion_tokens": 30}})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(reply))), Header: http.Header{}}, nil
	})}
	rec := copilotCall(t, s, s.copilotQuestionRequestRoute, http.MethodPost, "/v1/voice/copilot-question", `{"session":"`+row.ID+`","conversation":"conversation-a","question":"Why A?","replies":1,"language":"en"}`)
	if rec.Code != 200 || calls != 1 {
		t.Fatalf("status %d calls %d: %s", rec.Code, calls, rec.Body)
	}
	var got map[string]any
	json.Unmarshal(rec.Body.Bytes(), &got)
	if strings.Contains(string(mustJSON(t, got["sources"])), "cost is unknown") {
		t.Fatal("raw message body returned to voice model")
	}
	if got["usage"].(map[string]any)["prompt_tokens"] != float64(100) {
		t.Fatal("usage lost")
	}
	http.DefaultClient = &http.Client{Transport: questionTransport(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"unavailable"}}`)), Header: http.Header{}}, nil
	})}
	rec = copilotCall(t, s, s.copilotQuestionRequestRoute, http.MethodPost, "/v1/voice/copilot-question", `{"session":"`+row.ID+`","conversation":"conversation-a","question":"Why A?","replies":1}`)
	if rec.Code != 502 || calls != 2 {
		t.Fatalf("failed call retried: status %d calls %d", rec.Code, calls)
	}
	rec = copilotCall(t, s, s.copilotQuestionRequestRoute, http.MethodPost, "/v1/voice/copilot-question", `{"session":"`+row.ID+`","conversation":"replacement","question":"Why A?","replies":1}`)
	if rec.Code != 409 || calls != 2 {
		t.Fatal("changed identity reached model")
	}
}
func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return b
}

func TestQuestionCountsDoNotPullInEarlierAssistantFragments(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{"entries": []map[string]any{{"role": "user", "text": "Compare"}, {"role": "assistant", "text": "old"}, {"role": "assistant", "text": "new"}}})
	var page copilotQuestionPage
	json.Unmarshal(raw, &page)
	messages, replies, complete := questionMessages([]copilotQuestionPage{page}, 1)
	if replies != 1 || !complete || len(messages) != 2 || messages[0].Role != "user" || messages[1].Text != "new" {
		t.Fatalf("%+v replies=%d complete=%v", messages, replies, complete)
	}
}

func TestQuestionMessagesKeepQuestionsBeginningWithMarkup(t *testing.T) {
	var page copilotQuestionPage
	if err := json.Unmarshal([]byte(`{"entries":[{"role":"user","text":"<image>Compare the costs in this table."},{"role":"assistant","text":"A costs less under this assumption."}]}`), &page); err != nil {
		t.Fatal(err)
	}
	messages, replies, complete := questionMessages([]copilotQuestionPage{page}, 1)
	if !complete || replies != 1 || len(messages) != 2 || messages[0].Text != "<image>Compare the costs in this table." {
		t.Fatalf("question lost: %+v, %d, %v", messages, replies, complete)
	}
}
