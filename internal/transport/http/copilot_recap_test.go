package http

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/planner"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// recapPages is a Session's record as /v1/transcript pages it: newest page
// first, each page oldest first, older pages reached through a falling nextBefore.
func recapPages(pages ...[]map[string]any) func(target string, into any) bool {
	return func(target string, into any) bool {
		n := 0
		if i := strings.Index(target, "&before="); i >= 0 {
			before, _ := strconv.Atoi(target[i+len("&before="):])
			n = 1000 - before
		}
		if n >= len(pages) {
			return false
		}
		page := map[string]any{"entries": pages[n]}
		if n+1 < len(pages) {
			page["nextBefore"] = 1000 - (n + 1) // offsets fall as pages get older
		}
		raw, _ := json.Marshal(page)
		return json.Unmarshal(raw, into) == nil
	}
}

// The 2026-11-13 call: the one report said 41/56 an hour earlier, a later
// ordinary reply said 46/56, and the last reply carried no count. The recap
// must be shown the later reply, with when each line was written.
func TestARecapReadsEveryReplyNotOnlyReportsAndTheLastReply(t *testing.T) {
	now := time.Unix(1_794_563_000, 0)
	older := []map[string]any{
		{"role": "user", "text": "Align with Codync.", "at": now.Add(-3 * time.Hour).Unix()},
		{"role": "tool", "text": `clawdline session report --summary "Five cards 41/56, 15 left."`, "at": now.Add(-70 * time.Minute).Unix()},
	}
	newer := []map[string]any{
		{"role": "assistant", "text": "Documents committed; progress is 46/56, 10 left.", "at": now.Add(-14 * time.Minute).Unix()},
		{"role": "tool", "text": "go test ./...", "at": now.Add(-13 * time.Minute).Unix()},
		{"role": "assistant", "text": "Approval received; installing the two adapters.", "at": now.Add(-6 * time.Minute).Unix()},
	}
	lines, complete, ok := copilotRecapTimeline(recapPages(newer, older), "%2", now)
	if !ok || !complete {
		t.Fatalf("ok=%v complete=%v", ok, complete)
	}
	var kinds, texts []string
	for _, l := range lines {
		kinds = append(kinds, l.Kind)
		texts = append(texts, l.When+" "+l.Text)
	}
	if strings.Join(kinds, ",") != "request,report,reply,reply" {
		t.Fatalf("kinds %v", kinds)
	}
	joined := strings.Join(texts, "\n")
	if !strings.Contains(joined, "14 分鐘前 Documents committed; progress is 46/56") ||
		!strings.Contains(joined, "1 小時前 Five cards 41/56") {
		t.Fatalf("the recap was not shown each line with when:\n%s", joined)
	}
	if strings.Contains(joined, "go test") {
		t.Fatalf("a tool line that is not a report reached the recap:\n%s", joined)
	}
}

func TestARecapOfALongSessionSaysItWasNotReadWhole(t *testing.T) {
	pages := make([][]map[string]any, maxCopilotRecapPages+3)
	for i := range pages {
		pages[i] = []map[string]any{{"role": "assistant", "text": "page " + strconv.Itoa(i), "at": 1}}
	}
	lines, complete, ok := copilotRecapTimeline(recapPages(pages...), "%1", time.Unix(100, 0))
	if !ok || complete || len(lines) != maxCopilotRecapPages {
		t.Fatalf("ok=%v complete=%v lines=%d", ok, complete, len(lines))
	}
}

func TestARecapThatIsTooLongKeepsTheFirstRequestAndTheNewestLines(t *testing.T) {
	big := strings.Repeat("字", maxCopilotRecapReplyRunes)
	lines := []copilotRecapLine{{Kind: "request", Text: "the goal"}}
	for i := 0; i < maxCopilotRecapInputRunes/maxCopilotRecapReplyRunes+10; i++ {
		lines = append(lines, copilotRecapLine{Kind: "reply", Text: big + strconv.Itoa(i)})
	}
	lines = append(lines, copilotRecapLine{Kind: "reply", Text: "the newest"})
	kept, clipped := copilotRecapFit(lines)
	if !clipped || kept[0].Text != "the goal" || kept[len(kept)-1].Text != "the newest" || len(kept) >= len(lines) {
		t.Fatalf("clipped=%v first=%q last=%q kept=%d of %d", clipped, kept[0].Text, kept[len(kept)-1].Text, len(kept), len(lines))
	}
	total := 0
	for _, l := range kept {
		total += len([]rune(l.Text)) + 24
	}
	if total > maxCopilotRecapInputRunes {
		t.Fatalf("kept %d runes", total)
	}
}

// The Board's own count: cancelled steps are not steps.
func TestARecapCountsBoardStepsAsTheBoardDoes(t *testing.T) {
	steps := func(done, open, cancelled int) []work.StepV2 {
		var out []work.StepV2
		for i := 0; i < done; i++ {
			out = append(out, work.StepV2{Done: true})
		}
		for i := 0; i < open; i++ {
			out = append(out, work.StepV2{})
		}
		for i := 0; i < cancelled; i++ {
			out = append(out, work.StepV2{CancelledAt: time.Unix(1, 0)})
		}
		return out
	}
	items := []app.WorkV2View{
		{Item: work.ItemV2{Title: "PA-05", Phase: "implementing"}, Steps: steps(12, 6, 0)},
		{Item: work.ItemV2{Title: "PA-02"}, Steps: steps(6, 3, 0)},
		{Item: work.ItemV2{Title: "PA-03"}, Steps: steps(11, 0, 0)},
		{Item: work.ItemV2{Title: "PA-04"}, Steps: steps(9, 1, 1)},
		{Item: work.ItemV2{Title: "PA-01"}, Steps: steps(8, 0, 0)},
	}
	todos := []work.DirectTodoV2{{CompletedAt: time.Unix(5, 0)}, {}}
	p := copilotRecapProgressOf(items, todos, false, time.Unix(9, 0))
	if p.DoneSteps != 46 || p.TotalSteps != 56 || p.LeftSteps != 10 || p.TodosDone != 1 || p.TodosOpen != 1 || len(p.Cards) != 5 {
		t.Fatalf("%+v", p)
	}
	if p.Cards[3].Done != 9 || p.Cards[3].Total != 10 {
		t.Fatalf("PA-04 %+v", p.Cards[3])
	}
}

func TestARecapIsWrittenFromTheBoardCountAndTheRecord(t *testing.T) {
	s := &Server{cfg: config.Config{Dir: t.TempDir()}}
	for body, code := range map[string]string{
		`not json`:         "invalid_body",
		`{"session":""}`:   "session_missing",
		`{"session":"%2"}`: "copilot_key_missing",
	} {
		rec := copilotCall(t, s, s.copilotRecapRoute, http.MethodPost, "/v1/voice/copilot-recap", body)
		if !strings.Contains(rec.Body.String(), code) {
			t.Errorf("%s: %d %s, want %s", body, rec.Code, rec.Body, code)
		}
	}
	seen := fakeDecision(t, `{"recap":"It aligns with Codync. The Board says 46 of 56 steps are done, 10 left."}`)
	input := []byte(`{"progress":{"done_steps":46,"total_steps":56,"remaining_steps":10},"timeline":[],"complete":true}`)
	content, _, detail := copilotChatJSON(t.Context(), testCopilotKey, copilotRecapRequest(input, "zh-Hant"))
	if detail != "" || !strings.Contains(content, "46 of 56") {
		t.Fatalf("%q %q", content, detail)
	}
	sent := (*seen)[0]
	if sent["model"] != copilotRecapModel {
		t.Fatalf("model %v", sent["model"])
	}
	msgs, _ := sent["messages"].([]any)
	system, _ := msgs[0].(map[string]any)
	if !strings.Contains(system["content"].(string), "進度數字只能用 progress 的數字") {
		t.Fatal("the recap prompt does not say where counts come from")
	}
}

func TestARecapIsWrittenInTheConsolesLanguage(t *testing.T) {
	system := func(language string) string {
		messages, _ := copilotRecapRequest([]byte(`{}`), language)["messages"].([]map[string]string)
		if len(messages) == 0 {
			t.Fatal("no messages")
		}
		return messages[0]["content"]
	}
	if en := system("en"); strings.Contains(en, planner.ChineseOutput) || !strings.Contains(en, "BCP 47：en") {
		t.Fatalf("an English console's recap rule: %q", en)
	}
	if !strings.Contains(system("zh-Hant"), planner.ChineseOutput) {
		t.Fatal("a Taiwan Traditional Chinese console lost its rule")
	}
}
