package http

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/sainteye/clawdline/internal/adapters/planner"

	"github.com/sainteye/clawdline/internal/config"
)

// The two reads a briefing makes, as this daemon answers them.
func fakeBriefingReads(t *testing.T, list string, pages map[string]string) func(string, any) bool {
	t.Helper()
	return func(target string, into any) bool {
		body := list
		if strings.HasPrefix(target, "/v1/transcript?session=") {
			id := strings.TrimPrefix(target, "/v1/transcript?session=")
			p, ok := pages[id]
			if !ok {
				return false
			}
			body = p
		}
		return json.Unmarshal([]byte(body), into) == nil
	}
}

const briefingList = `{"sessions":[
 {"id":"%2","label":"Prepare screenshots","assistant":"codex","state":"idle","work_state":"unknown"},
 {"id":"%7","label":"Fix the flaky test","assistant":"claude","state":"waiting","work_state":"waiting",
  "menu":{"question":"Push to main?","options":[{"label":"Yes"},{"label":"No"}]}},
 {"id":"%9","label":"Wardrobe images","assistant":"claude","state":"working","work_state":"working",
  "disposition":{"title":"Two product images are ready for review."}}]}`

func TestABriefingIsGivenTheSessionsMostUrgentFirstWithWhatWasAskedAndDelivered(t *testing.T) {
	read := fakeBriefingReads(t, briefingList, map[string]string{
		"%257": `{"entries":[
		  {"role":"user","text":"Make the login test pass."},
		  {"role":"user","text":"<clawdline-notice>{}</clawdline-notice>"},
		  {"role":"tool","text":"go test ./... && clawdline session report --summary \"Login test passes 20 of 20; d1e2f3a pushed.\""},
		  {"role":"assistant","text":"Twenty runs pass. Should I push?"}]}`,
		"%259": `{"entries":[{"role":"tool","text":"clawdline session report --summary 'First image done.'"}]}`,
	})
	got, err := copilotBriefingFrom(read)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, s := range got {
		names = append(names, s.Name)
	}
	if strings.Join(names, ",") != "Fix the flaky test,Wardrobe images,Prepare screenshots" {
		t.Fatalf("order: %v", names)
	}
	waiting := got[0]
	if waiting.Waiting != "Push to main?" || strings.Join(waiting.Options, "/") != "Yes/No" {
		t.Fatalf("waiting: %+v", waiting)
	}
	if strings.Join(waiting.Requests, "|") != "Make the login test pass." {
		t.Fatalf("a notice was taken for the person: %v", waiting.Requests)
	}
	if strings.Join(waiting.Deliveries, "|") != "Login test passes 20 of 20; d1e2f3a pushed." || waiting.LastReply != "Twenty runs pass. Should I push?" {
		t.Fatalf("delivered: %+v", waiting)
	}
	if strings.Join(got[1].Deliveries, "|") != "First image done.|Two product images are ready for review." {
		t.Fatalf("the row's own delivery summary is added once: %v", got[1].Deliveries)
	}
	if len(got[2].Requests) != 0 || got[2].LastReply != "" {
		t.Fatalf("an unreadable transcript leaves a Session with its row only: %+v", got[2])
	}
}

func TestABriefingReadsAtMostItsSessionsAndKeepsTheNewestAsks(t *testing.T) {
	var rows []string
	for i := 0; i < copilotBriefingSessions+5; i++ {
		rows = append(rows, `{"id":"s`+string(rune('a'+i))+`","label":"S","state":"idle"}`)
	}
	var entries []string
	for i := 0; i < copilotBriefingAsks+3; i++ {
		entries = append(entries, `{"role":"user","text":"ask `+string(rune('0'+i))+`"}`)
	}
	page := `{"entries":[` + strings.Join(entries, ",") + `]}`
	pages := map[string]string{}
	for i := 0; i < copilotBriefingSessions+5; i++ {
		pages["s"+string(rune('a'+i))] = page
	}
	got, err := copilotBriefingFrom(fakeBriefingReads(t, `{"sessions":[`+strings.Join(rows, ",")+`]}`, pages))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != copilotBriefingSessions {
		t.Fatalf("read %d Sessions", len(got))
	}
	if len(got[0].Requests) != copilotBriefingAsks || got[0].Requests[len(got[0].Requests)-1] != "ask 6" {
		t.Fatalf("asks: %v", got[0].Requests)
	}
}

func TestABriefingThatIsOffOrFailsIsATypedRefusal(t *testing.T) {
	s := &Server{cfg: config.Config{Dir: t.TempDir()}}
	copilotCall(t, s, s.copilotSettingsRoute, http.MethodPost, "/v1/settings/copilot", `{"briefing":false}`)
	rec := copilotCall(t, s, s.copilotBriefingRoute, http.MethodPost, "/v1/voice/copilot-briefing", "")
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "copilot_briefing_off") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

func stubBriefing(t *testing.T, run func() (string, error)) *int32 {
	t.Helper()
	var calls int32
	wasRun, wasRead, wasWait := copilotBriefRun, copilotBriefingSessionsOf, copilotBriefingWait
	copilotBriefRun = func(context.Context, string, string) (string, error) {
		atomic.AddInt32(&calls, 1)
		return run()
	}
	copilotBriefingSessionsOf = func(*Server, context.Context) ([]copilotBriefingSession, error) {
		return []copilotBriefingSession{{Name: "Fix the flaky test", State: "waiting"}}, nil
	}
	copilotBriefingWait = 200 * time.Millisecond
	copilotBriefing.Lock()
	copilotBriefing.held, copilotBriefing.running = copilotBriefingAnswer{}, nil
	copilotBriefing.Unlock()
	t.Cleanup(func() {
		copilotBriefRun, copilotBriefingSessionsOf, copilotBriefingWait = wasRun, wasRead, wasWait
		copilotBriefing.Lock()
		copilotBriefing.held, copilotBriefing.running = copilotBriefingAnswer{}, nil
		copilotBriefing.Unlock()
	})
	return &calls
}

func TestASlowBriefingIsPendingAndTheNextAskIsHandedItFromTheSameTurn(t *testing.T) {
	release := make(chan struct{})
	calls := stubBriefing(t, func() (string, error) {
		<-release
		return "一件事要你處理：Fix the flaky test 在等你決定要不要推。", nil
	})
	s := &Server{cfg: config.Config{Dir: t.TempDir()}}
	first := copilotCall(t, s, s.copilotBriefingRoute, http.MethodPost, "/v1/voice/copilot-briefing", "")
	if first.Code != http.StatusAccepted || !strings.Contains(first.Body.String(), `"pending":true`) {
		t.Fatalf("first: %d %s", first.Code, first.Body)
	}
	close(release)
	var answer copilotBriefingAnswer
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		rec := copilotCall(t, s, s.copilotBriefingRoute, http.MethodPost, "/v1/voice/copilot-briefing", "")
		if rec.Code == http.StatusOK {
			_ = json.Unmarshal(rec.Body.Bytes(), &answer)
			break
		}
	}
	if !strings.Contains(answer.Briefing, "Fix the flaky test") || answer.Sessions != 1 || answer.Assistant != "claude" {
		t.Fatalf("answer: %+v", answer)
	}
	again := copilotCall(t, s, s.copilotBriefingRoute, http.MethodPost, "/v1/voice/copilot-briefing", "")
	if again.Code != http.StatusOK || atomic.LoadInt32(calls) != 1 {
		t.Fatalf("a fresh briefing ran %d turns, answered %d", atomic.LoadInt32(calls), again.Code)
	}
}

func TestTwoAsksAtOnceShareOneBriefingTurn(t *testing.T) {
	calls := stubBriefing(t, func() (string, error) { time.Sleep(50 * time.Millisecond); return "沒有事需要你處理。", nil })
	s := &Server{cfg: config.Config{Dir: t.TempDir()}}
	var wg sync.WaitGroup
	codes := make([]int, 4)
	for i := range codes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			codes[i] = copilotCall(t, s, s.copilotBriefingRoute, http.MethodPost, "/v1/voice/copilot-briefing", "").Code
		}(i)
	}
	wg.Wait()
	if atomic.LoadInt32(calls) != 1 {
		t.Fatalf("%d turns for four asks", atomic.LoadInt32(calls))
	}
	for _, c := range codes {
		if c != http.StatusOK {
			t.Fatalf("codes %v", codes)
		}
	}
}

func TestAFailedBriefingSaysWhy(t *testing.T) {
	stubBriefing(t, func() (string, error) { return "", planner.ErrNoPlanner })
	s := &Server{cfg: config.Config{Dir: t.TempDir()}}
	rec := copilotCall(t, s, s.copilotBriefingRoute, http.MethodPost, "/v1/voice/copilot-briefing", "")
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "copilot_briefing_no_assistant") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

// A diagnosis runs to thousands of characters and ends on what the person is
// asked to decide; the briefing must still see that question.
func TestABriefingKeepsTheQuestionALongReplyEndsOn(t *testing.T) {
	long := "Root cause found. " + strings.Repeat("The start path skips the default model. ", 120) +
		"Shall I open a fix branch for both problems?"
	raw, _ := json.Marshal(map[string]any{"entries": []map[string]string{{"role": "assistant", "text": long}}})
	read := fakeBriefingReads(t, briefingList, map[string]string{"%257": string(raw)})
	got, err := copilotBriefingFrom(read)
	if err != nil {
		t.Fatal(err)
	}
	reply := got[0].LastReply
	if !strings.HasPrefix(reply, "Root cause found.") || !strings.HasSuffix(reply, "Shall I open a fix branch for both problems?") {
		t.Fatalf("the reply lost an end: %q … %q", reply[:40], reply[len(reply)-60:])
	}
	if n := utf8.RuneCountInString(reply); n > maxBriefingReplyRunes+3 {
		t.Fatalf("the reply is %d runes, over its bound", n)
	}
}
