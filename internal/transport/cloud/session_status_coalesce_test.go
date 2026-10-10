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

// statusSession is one Session of the fleet this replay publishes: a working
// one whose clock, token count and sub-agents move, or an idle one whose every
// published fact stands still. The idle rows are the point of the measurement:
// nothing about them changes for half an hour, so a pass that states them is a
// pass that said nothing.
type statusSession struct {
	id       string
	working  bool
	verb     string
	activity int64
	tokens   int
	agents   int
	state    string
}

func (s statusSession) row(clock int) map[string]any {
	agents := make([]any, 0, s.agents)
	for i := 0; i < s.agents; i++ {
		agents = append(agents, map[string]any{"id": fmt.Sprintf("a%d", i), "state": "running"})
	}
	row := map[string]any{
		"id": s.id, "assistant": "claude", "tty": "tty" + s.id[1:], "backend": "tmux",
		"execution_generation": strings.Repeat(s.id[1:2], 32),
		"state":                s.state,
		"activity":             map[string]any{"at": s.activity, "known": true, "evidence": "transcript"},
		"agents":               agents, "agents_reading": map[string]any{"state": "complete"},
		"source": map[string]any{"freshness": "current", "observed_at": 1000 + clock, "provenance": "tmux"},
	}
	if s.working {
		row["work_state"] = "working"
		row["line"] = fmt.Sprintf("%s… (%dm %ds · ↓ %d tokens)", s.verb, clock/60, clock%60, s.tokens)
		row["working_since"] = 1000
	}
	return row
}

func statusScan(at int, fleet []statusSession) []byte {
	sessions := make([]map[string]any, 0, len(fleet))
	for _, s := range fleet {
		sessions = append(sessions, s.row(at))
	}
	body, _ := json.Marshal(map[string]any{"at": 1000 + at, "scan": map[string]any{"complete": true}, "sessions": sessions})
	return body
}

// Half an hour of ten Sessions — two working, eight idle — on the five-second
// pass, counting the status frames (ss/) that leave.
//
// Measured on the running daemon's log for 2026-10-10: in the busiest hour
// (13:00) 3,725 ss/ frames left for 13 listed Sessions — 287 per Session-hour
// — because one working row's clock moved the whole set's identity and every
// pass that changed restated all 13 rows and the marker.
//
// This replay against that publisher: 1,331 status frames in half an hour,
// 266 per Session-hour, of which 968 were the eight idle rows restating what
// the viewer already held (121 passes × 8). Against this one: 337 frames, 67
// per Session-hour, the idle rows down to their 56 heartbeats.
func TestHalfAnHourOfTenSessionsSendsOnlyTheStatusRowsThatChanged(t *testing.T) {
	ctx := context.Background()
	random := rand.New(rand.NewSource(11))
	now := time.Unix(5000, 0)
	fleet := []statusSession{
		{id: "%10", working: true, state: "working", verb: "Thinking", activity: 100},
		{id: "%11", working: true, state: "working", verb: "Reading", activity: 100},
	}
	for i := 2; i < 10; i++ {
		fleet = append(fleet, statusSession{id: fmt.Sprintf("%%%d", 10+i), state: "idle", activity: 100})
	}
	idle := map[string]bool{}
	for _, s := range fleet[2:] {
		idle[s.id] = true
	}
	body := statusScan(0, fleet)
	out := &collector{}
	p := drivenPublisher(out, &body, &now)
	p.firstPass(ctx)
	first := countPrefix(out.channels(), "ss/mac-01/")
	if first != len(fleet)+1 {
		t.Fatalf("the first status pass sent %d frames for %d Sessions: %v", first, len(fleet), out.channels())
	}
	out.reset()

	verbs := []string{"Thinking", "Reading", "Editing", "Running"}
	var statuses, idleStatuses, markers, legacy, passes int
	for second := 5; second <= 1800; second += 5 {
		now = now.Add(5 * time.Second)
		// The churn the running daemon showed on 2026-10-09: machine-wide, the
		// activity time moves on 78% of five-second passes, the token count on
		// 54% and the sub-agents on 42%. Only the working rows have any.
		if random.Float64() < 0.78 {
			fleet[random.Intn(2)].activity = int64(100 + second)
		}
		if random.Float64() < 0.54 {
			fleet[random.Intn(2)].tokens += 1 + random.Intn(400)
		}
		if random.Float64() < 0.42 {
			fleet[random.Intn(2)].agents = random.Intn(4)
		}
		changed := map[string]bool{}
		for i := 0; i < 2; i++ {
			s := &fleet[i]
			switch roll := random.Float64(); {
			case roll < 0.004:
				changed[s.id] = true
				if s.state == "working" {
					s.state, s.working = "waiting", false
				} else {
					s.state, s.working = "working", true
				}
			case roll < 0.01 && s.state == "working":
				changed[s.id] = true
				next := verbs[random.Intn(len(verbs))]
				for next == s.verb {
					next = verbs[random.Intn(len(verbs))]
				}
				s.verb = next
			}
		}
		body = statusScan(second, fleet)
		p.Pass(ctx)
		names := out.channels()
		for id := range changed {
			channel := "ss/mac-01/" + strings.Replace(id, "%", "%25", 1)
			if countPrefix(names, channel) != 1 {
				t.Errorf("at %d s the changed state of %s was not stated on the pass that read it: %v", second, id, names)
			}
		}
		for id := range idle {
			idleStatuses += countPrefix(names, "ss/mac-01/"+strings.Replace(id, "%", "%25", 1))
		}
		statuses += countPrefix(names, "ss/mac-01/")
		markers += countPrefix(names, "ss/mac-01/"+InventorySessionID)
		legacy += countPrefix(names, "s/mac-01/%25")
		out.reset()
		passes++
	}
	perSessionHour := float64(statuses) * 2 / float64(len(fleet))
	t.Logf("%d passes: %d status frames (%d rows, %d markers, %d of them idle rows) = %.0f per Session-hour; %d legacy s/ rows",
		passes, statuses, statuses-markers, markers, idleStatuses, perSessionHour, legacy)
	// The eight idle rows change nothing after the first pass. Their only
	// status frames are the heartbeat: at most one per 240 s each.
	if want := 8 * (1800/int(Heartbeat.Seconds()) + 1); idleStatuses > want {
		t.Errorf("the idle rows sent %d status frames, more than the %d their heartbeat allows", idleStatuses, want)
	}
	if perSessionHour > 150 {
		t.Errorf("%.0f status frames per Session-hour is not well below the 486 measured before this change", perSessionHour)
	}
}
