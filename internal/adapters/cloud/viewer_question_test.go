package cloud

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	domain "github.com/sainteye/clawdline/internal/domain/cloud"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func TestViewerQuestionRequiresPinnedFreshSignedDetail(t *testing.T) {
	now := time.Now()
	target := ViewerDestination{MachineID: "machine-a", SessionID: "same", ExecutionGeneration: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	client := &ViewerClient{rich: make(map[string]viewerRich), richActive: make(map[string]bool), richUpdate: make(chan struct{}, 1)}
	client.openRich(target)
	row := func(generation, freshness string, observedAt int64) []byte {
		body, _ := json.Marshal(map[string]any{"session": map[string]any{
			"id":                   target.SessionID,
			"execution_generation": generation,
			"source":               map[string]any{"freshness": freshness, "observed_at": observedAt},
			"menu": map[string]any{"question": "Allow this command?", "options": []any{
				map[string]any{"n": 1, "label": "Allow", "can": true},
				map[string]any{"n": 2, "label": "Deny", "can": true},
			}},
		}})
		return body
	}
	wrongSession := row(target.ExecutionGeneration, "current", now.Unix())
	wrongSession = []byte(strings.Replace(string(wrongSession), `"id":"same"`, `"id":"other"`, 1))
	client.acceptRich(domain.Envelope{Ch: "s/machine-a/same", Sender: "machine-a", Seq: 1}, wrongSession)
	if client.currentQuestion(target, now) != nil {
		t.Fatal("another session filled the selected question")
	}
	client.acceptRich(domain.Envelope{Ch: "s/machine-b/same", Sender: "machine-a", Seq: 1}, row(target.ExecutionGeneration, "current", now.Unix()))
	if client.currentQuestion(target, now) != nil {
		t.Fatal("another machine filled the selected question")
	}
	client.acceptRich(domain.Envelope{Ch: "s/machine-a/same", Sender: "machine-a", Seq: 2}, row("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "current", now.Unix()))
	if client.currentQuestion(target, now) != nil {
		t.Fatal("an old execution filled the selected question")
	}
	client.acceptRich(domain.Envelope{Ch: "s/machine-a/same", Sender: "machine-a", Seq: 3}, row(target.ExecutionGeneration, "unverified", now.Unix()))
	if client.currentQuestion(target, now) != nil {
		t.Fatal("an unverified row offered an answer")
	}
	client.acceptRich(domain.Envelope{Ch: "s/machine-a/same", Sender: "machine-a", Seq: 4}, row(target.ExecutionGeneration, "current", now.Add(-10*time.Minute).Unix()))
	if client.currentQuestion(target, now) != nil {
		t.Fatal("a stale row offered an answer")
	}
	client.acceptRich(domain.Envelope{Ch: "s/machine-a/same", Sender: "machine-a", Seq: 5}, row(target.ExecutionGeneration, "current", now.Unix()))
	question := client.currentQuestion(target, now)
	want := session.MenuFingerprint(session.Menu{Question: "Allow this command?", Options: []session.MenuOption{
		{Number: 1, Label: "Allow"}, {Number: 2, Label: "Deny"}}})
	if question == nil || question.Fingerprint != want || len(question.Options) != 2 || question.Options[0].Key != "1" {
		t.Fatalf("verified question: %+v, want %s", question, want)
	}
	client.closeRich(target)
	if client.currentQuestion(target, now) != nil {
		t.Fatal("closed detail kept content")
	}
}
