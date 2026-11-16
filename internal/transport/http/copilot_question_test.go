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

	"github.com/sainteye/clawdline/internal/app"
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
	c, key, err := b.context(questionPages(t, "v1", &reads), "%1", "conversation-a", "", 2, now)
	if err != nil || c.Replies != 2 || !c.Complete || len(reads) != 2 {
		t.Fatalf("%+v %v reads %v", c, err, reads)
	}
	if !strings.Contains(c.Messages[len(c.Messages)-1].Text, "| A | faster | higher |") {
		t.Fatal("table structure lost")
	}
	_, again, err := b.context(questionPages(t, "v1", &reads), "%1", "conversation-a", key, 2, now.Add(time.Second))
	if err != nil || again != key || len(reads) != 3 {
		t.Fatalf("context not reused: %v %v", err, reads)
	}
	_, changed, err := b.context(questionPages(t, "v2", &reads), "%1", "conversation-a", key, 2, now.Add(2*time.Second))
	if err != nil || changed == key || len(reads) != 5 {
		t.Fatalf("changed source reused: %v %v", err, reads)
	}
	_, other, err := b.context(questionPages(t, "v1", &reads), "%1", "conversation-b", key, 2, now.Add(3*time.Second))
	if err != nil || other == key {
		t.Fatal("context crossed conversations")
	}
}

func TestQuestionContextLimitsAndCoverageAreObservable(t *testing.T) {
	var b copilotQuestionBook
	var reads []string
	now := time.Unix(1000, 0)
	c, _, err := b.context(questionPages(t, "v1", &reads), "%1", "c", "", 5, now)
	if err != nil || c.Complete || c.Replies != 2 {
		t.Fatalf("missing replies not marked: %+v %v", c, err)
	}
	b.contexts = map[string]copilotQuestionContext{}
	for i := 0; i < maxCopilotQuestionContexts; i++ {
		b.contexts[strings.Repeat("x", i+1)] = copilotQuestionContext{At: now}
	}
	if _, _, err = b.context(questionPages(t, "v1", &reads), "%1", "c", "", 1, now); err == nil {
		t.Fatal("full context store accepted another entry")
	}
	if _, _, err = b.context(questionPages(t, "v1", &reads), "%1", "c", "", 1, now.Add(maxCopilotQuestionAge+time.Second)); err != nil {
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
	if !validQuestionAnswer(a, c) {
		t.Fatal("valid evidence refused")
	}
	a.Evidence[0].Quote = "A is cheaper"
	if validQuestionAnswer(a, c) {
		t.Fatal("invented evidence accepted")
	}
	a.Evidence[0] = copilotQuestionEvidence{Ref: 2, Quote: "Option A is faster"}
	if validQuestionAnswer(a, c) {
		t.Fatal("unknown source accepted")
	}
}

type questionTransport func(*http.Request) (*http.Response, error)

func (f questionTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type questionRemote struct{ reads int }

func (*questionRemote) SendRemote(context.Context, session.Session, string, func() error) error {
	panic("analysis cannot send")
}
func (r *questionRemote) Page(_ context.Context, s session.Session, _ int, _, _ int64, _ string) (contract.TranscriptPage, error) {
	r.reads++
	return contract.TranscriptPage{ID: s.ID, Signature: "question-v1", Entries: []contract.TranscriptEntry{{Role: "user", Text: "Compare A and B"}, {Role: "assistant", Text: "A is faster; cost is unknown."}}}, nil
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
