package cloud

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"
)

// workingRow is one working Session as the shared list states it.
type workingRow struct {
	id, state, verb string
	activity        int64
	tokens          int
	agents          int
}

func (r workingRow) row(clock int) map[string]any {
	agents := make([]any, 0, r.agents)
	for i := 0; i < r.agents; i++ {
		agents = append(agents, map[string]any{"id": fmt.Sprintf("a%d", i), "state": "running"})
	}
	line := fmt.Sprintf("%s… (%dm %ds · ↓ %d tokens)", r.verb, clock/60, clock%60, r.tokens)
	if r.state != "working" {
		line = ""
	}
	return map[string]any{
		"id": r.id, "assistant": "claude", "tty": "tty" + r.id[1:], "backend": "tmux",
		"state": r.state, "work_state": "working", "line": line, "working_since": 1000,
		"activity": map[string]any{"at": r.activity, "known": true, "evidence": "transcript"},
		"agents":   agents, "agents_reading": map[string]any{"state": "complete"},
		"source": map[string]any{"freshness": "current", "observed_at": 1000 + clock, "provenance": "tmux"},
	}
}

func scanOf(at int, rows []workingRow) []byte {
	sessions := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		sessions = append(sessions, r.row(at))
	}
	body, _ := json.Marshal(map[string]any{"at": 1000 + at, "scan": map[string]any{"complete": true}, "sessions": sessions})
	return body
}

// drivenPublisher reads rows from a variable and runs on a clock the test moves.
func drivenPublisher(out *collector, body *[]byte, now *time.Time) *Publisher {
	p := newPublisher(&fixedRouter{status: 503}, out)
	p.Sessions = func(context.Context) ([]byte, bool) { return *body, true }
	p.Now = func() time.Time { return *now }
	return p
}

func countPrefix(names []string, prefix string) int {
	n := 0
	for _, name := range names {
		if strings.HasPrefix(name, prefix) {
			n++
		}
	}
	return n
}

// The activity time, the sub-agents and the token count move on nearly every
// pass while a Session works. A change in them alone waits for the row's
// window; the pass after it carries the newest value; any other change goes
// at once and starts the window again.
func TestAVolatileOnlyChangeWaitsForTheRowsWindow(t *testing.T) {
	ctx := context.Background()
	now := time.Unix(5000, 0)
	row := workingRow{id: "%7", state: "working", verb: "Thinking", activity: 100, tokens: 50}
	body := scanOf(0, []workingRow{row})
	out := &collector{}
	p := drivenPublisher(out, &body, &now)
	p.firstPass(ctx)
	if n := countPrefix(out.channels(), "s/mac-01/%257"); n != 1 {
		t.Fatalf("the first pass sent %d row(s): %v", n, out.channels())
	}

	step := func(seconds int, change func(*workingRow)) (rows, statuses int) {
		out.reset()
		now = now.Add(5 * time.Second)
		change(&row)
		body = scanOf(seconds, []workingRow{row})
		p.Pass(ctx)
		names := out.channels()
		return countPrefix(names, "s/mac-01/%257"), countPrefix(names, "ss/mac-01/%257")
	}
	if rows, statuses := step(5, func(r *workingRow) { r.activity = 105; r.tokens = 90 }); rows+statuses != 0 {
		t.Errorf("a volatile change 5 s after the last send went out: rows=%d statuses=%d", rows, statuses)
	}
	if rows, statuses := step(10, func(r *workingRow) { r.agents = 2 }); rows+statuses != 0 {
		t.Errorf("a sub-agent change 10 s after the last send went out: rows=%d statuses=%d", rows, statuses)
	}
	rows, statuses := step(15, func(r *workingRow) { r.activity = 115 })
	if rows != 1 || statuses != 1 {
		t.Fatalf("the pass at the window's end did not carry the held change: rows=%d statuses=%d", rows, statuses)
	}
	sent := out.payload(t, "s/mac-01/%257")["session"].(map[string]any)
	if sent["activity"].(map[string]any)["at"] != float64(115) || len(sent["agents"].([]any)) != 2 ||
		!strings.Contains(sent["line"].(string), "↓ 90 tokens") {
		t.Errorf("the row sent at the window's end is not the newest reading: %v", sent)
	}

	// A state change is never held, and it starts the window again.
	if rows, statuses := step(20, func(r *workingRow) { r.state = "waiting"; r.activity = 120 }); rows != 1 || statuses != 1 {
		t.Errorf("a state change 5 s into the window was held: rows=%d statuses=%d", rows, statuses)
	}
	if rows, statuses := step(25, func(r *workingRow) { r.state = "working"; r.verb = "Reading" }); rows != 1 || statuses != 1 {
		t.Errorf("a line that says something new was held: rows=%d statuses=%d", rows, statuses)
	}
	if rows, statuses := step(30, func(r *workingRow) { r.tokens = 400 }); rows+statuses != 0 {
		t.Errorf("a token count 5 s after a real change went out: rows=%d statuses=%d", rows, statuses)
	}
}

// An hour of six working Sessions on the five-second pass, with the churn the
// running daemon showed on 2026-10-09 (commit 49e2a961: of 150 two-second
// frames, the activity time moved in 68, the token count in 40 and the
// sub-agents in 29, machine-wide). Per five-second pass that is the activity
// time of some row on 78% of passes, a token count on 54% and the sub-agents
// on 42%; on top, each row changes state or verb about once in eight minutes.
// Every real change has to leave on the pass that read it.
func TestAnHourOfWorkingRowsIsCoalesced(t *testing.T) {
	run := func(volatileEvery time.Duration) (rows, statuses, passes int) {
		ctx := context.Background()
		random := rand.New(rand.NewSource(7))
		now := time.Unix(5000, 0)
		var current []workingRow
		for i := 0; i < 6; i++ {
			current = append(current, workingRow{id: fmt.Sprintf("%%%d", 10+i), state: "working", verb: "Thinking", activity: 100})
		}
		body := scanOf(0, current)
		out := &collector{}
		p := drivenPublisher(out, &body, &now)
		p.VolatileEvery = volatileEvery
		p.firstPass(ctx)
		out.reset()
		verbs := []string{"Thinking", "Reading", "Editing", "Running"}
		for second := 5; second <= 3600; second += 5 {
			now = now.Add(5 * time.Second)
			if random.Float64() < 0.78 {
				current[random.Intn(len(current))].activity = int64(100 + second)
			}
			if random.Float64() < 0.54 {
				current[random.Intn(len(current))].tokens += 1 + random.Intn(400)
			}
			if random.Float64() < 0.42 {
				current[random.Intn(len(current))].agents = random.Intn(4)
			}
			real := map[string]bool{}
			for i := range current {
				r := &current[i]
				switch roll := random.Float64(); {
				case roll < 0.004:
					real[r.id] = true
					if r.state == "working" {
						r.state = "waiting"
					} else {
						r.state = "working"
					}
				case roll < 0.01 && r.state == "working":
					real[r.id] = true
					next := verbs[random.Intn(len(verbs))]
					for next == r.verb {
						next = verbs[random.Intn(len(verbs))]
					}
					r.verb = next
				}
			}
			body = scanOf(second, current)
			p.Pass(ctx)
			names := out.channels()
			for id := range real {
				if countPrefix(names, "s/mac-01/"+strings.Replace(id, "%", "%25", 1)) != 1 {
					t.Errorf("at %d s a real change to %s was not sent on the pass that read it (volatileEvery=%v)", second, id, volatileEvery)
				}
			}
			rows += countPrefix(names, "s/mac-01/%25")
			statuses += countPrefix(names, "ss/mac-01/")
			out.reset()
			passes++
		}
		return rows, statuses, passes
	}
	beforeRows, beforeStatuses, passes := run(-1)
	afterRows, afterStatuses, _ := run(0)
	before, after := beforeRows+beforeStatuses, afterRows+afterStatuses
	// Before step C the clock alone made every working row differ on every
	// pass: each pass sent all six rows and a seven-frame status set.
	preRedesign := passes * (6 + 7)
	t.Logf("frames per hour: pre-redesign %d; uncoalesced rows %d + status %d = %d; coalesced rows %d + status %d = %d (−%.0f%% vs uncoalesced, −%.0f%% vs pre-redesign)",
		preRedesign, beforeRows, beforeStatuses, before, afterRows, afterStatuses, after,
		100*float64(before-after)/float64(before), 100*float64(preRedesign-after)/float64(preRedesign))
	if after*10 > preRedesign*3 {
		t.Errorf("coalesced frames %d are more than 30%% of the pre-redesign %d", after, preRedesign)
	}
	if after >= before {
		t.Errorf("coalescing saved nothing: %d before, %d after", before, after)
	}
}
