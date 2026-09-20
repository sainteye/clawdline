package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/store"
	"github.com/sainteye/clawdline-go/internal/app/orchestrator"
	"github.com/sainteye/clawdline-go/internal/contract"
)

// What a task's end does to its child's tab used to be readable only in the
// child's own CHILD.md, so "is this tab still being open normal or not" could
// only be answered by opening somebody else's files. It is a field on the task
// now, and it names the rule *this* task's end chose rather than repeating the
// machine's setting.

func brokerWith(linger time.Duration) *orchestrator.Broker {
	return &orchestrator.Broker{ChildLinger: func() time.Duration { return linger }}
}

// Three ends, three different answers.
func TestTheTabPlanNamesThisTasksRule(t *testing.T) {
	end := time.Date(2026, 9, 20, 6, 0, 0, 0, time.UTC)
	linger := 3 * time.Minute
	ended := func(r orchestrator.Record, state orchestrator.State) orchestrator.Record {
		r.State, r.FinishedAt, r.ChildTerminalID = state, end, "%1"
		return r
	}
	scheduled := orchestrator.Record{ScheduleID: "5c000000-0000-4000-8000-000000000004",
		ScheduleCloseTab: "always"}

	cases := []struct {
		name    string
		r       orchestrator.Record
		linger  time.Duration
		rule    contract.BrokerTabRule
		close   bool
		what    string
		closeAt int64
	}{
		{"it ended normally", ended(orchestrator.Record{}, orchestrator.StateSuccess), linger,
			contract.BrokerTabRuleChildLinger, true, "closed about 180 seconds after it ends", end.Add(linger).Unix()},
		{"it timed out", ended(orchestrator.Record{}, orchestrator.StateTimeout), linger,
			contract.BrokerTabRuleUnfinishedLeftOpen, false, "left open", 0},
		{"its schedule says close_tab: always", ended(scheduled, orchestrator.StateTimeout), linger,
			contract.BrokerTabRuleScheduleAlways, true, "closed as soon as it is at rest", end.Unix()},
	}
	answers := map[contract.BrokerTabRule]bool{}
	for _, c := range cases {
		tab := brokerTab(brokerWith(c.linger).TabPolicy(c.r))
		if tab.Applied == nil {
			t.Errorf("%s: a task that ended carries no rule", c.name)
			continue
		}
		if tab.Applied.Rule != c.rule || tab.Applied.Close != c.close || tab.Applied.What != c.what {
			t.Errorf("%s: applied %+v, want rule %s close %v what %q",
				c.name, *tab.Applied, c.rule, c.close, c.what)
		}
		if tab.Applied.End != contract.TaskState(c.r.State) {
			t.Errorf("%s: applied to %s, want %s", c.name, tab.Applied.End, c.r.State)
		}
		if tab.CloseAt != c.closeAt {
			t.Errorf("%s: close_at %d, want %d", c.name, tab.CloseAt, c.closeAt)
		}
		if len(tab.Ends) == 0 {
			t.Errorf("%s: the answer states no ends", c.name)
		}
		answers[c.rule] = true
	}
	if len(answers) != len(cases) {
		t.Errorf("%d answers for %d tasks: the field repeats itself", len(answers), len(cases))
	}
}

// A task still running carries the table and no applied rule: which rule
// applies is decided by how it ends, and `applied` is absent rather than a
// guess. A key that is absent is checked on the bytes, because that is what a
// reader gets.
func TestARunningTasksTabPlanNamesNoRuleYet(t *testing.T) {
	tab := brokerTab(brokerWith(3 * time.Minute).TabPolicy(
		orchestrator.Record{State: orchestrator.StateBriefed, ChildTerminalID: "%1"}))
	raw, err := json.Marshal(tab)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if _, named := body["applied"]; named {
		t.Errorf("a running task already named a rule: %s", raw)
	}
	if _, owed := body["close_at"]; owed {
		t.Errorf("a running task is already owed a close: %s", raw)
	}
	if body["setting"] != orchestrator.TabSettingLinger || body["value"] != "180" {
		t.Errorf("the answer does not say which setting decided it: %s", raw)
	}
	ends, _ := body["ends"].([]any)
	if len(ends) != 4 {
		t.Fatalf("the answer states %d ends: %s", len(ends), raw)
	}
	first, _ := ends[0].(map[string]any)
	if first["end"] != "success" || first["rule"] != string(contract.BrokerTabRuleChildLinger) {
		t.Errorf("the first end is not this task's success row: %s", raw)
	}
}

// Every rule the broker can reach is a value the contract allows, and every
// value the contract allows is one the broker can reach. A rule added on one
// side only is a task answering with a word its readers do not have.
func TestTheTabRulesAndTheContractNameTheSameRules(t *testing.T) {
	scheduled := func(closeTab string) orchestrator.Record {
		return orchestrator.Record{ScheduleID: "5c000000-0000-4000-8000-000000000005",
			ScheduleCloseTab: closeTab}
	}
	shapes := []orchestrator.Record{{}, scheduled("on_success"), scheduled("always"), scheduled("never")}
	ends := []orchestrator.State{orchestrator.StateSuccess, orchestrator.StateFailure,
		orchestrator.StateTimeout, orchestrator.StateCancelled, orchestrator.StateSpawnFailed}

	reached := map[contract.BrokerTabRule]bool{}
	for _, linger := range []time.Duration{3 * time.Minute, -1} {
		b := brokerWith(linger)
		for _, shape := range shapes {
			for _, end := range ends {
				r := shape
				r.State = end
				tab := brokerTab(b.TabPolicy(r))
				for _, e := range append(tab.Ends, *tab.Applied) {
					reached[e.Rule] = true
				}
			}
		}
	}
	allowed := map[contract.BrokerTabRule]bool{}
	for _, v := range contract.BrokerTabRuleValues {
		allowed[v] = true
		if !reached[v] {
			t.Errorf("the contract allows %q and no task reaches it", v)
		}
	}
	for rule := range reached {
		if !allowed[rule] {
			t.Errorf("a task answers %q, which the contract does not allow", rule)
		}
	}
}

// The whole way through: a task read back from the store answers GET
// /v1/orchestrator/tasks/{id} with its tab plan, so a root judging "should
// this tab be gone by now" reads the task rather than the child's CHILD.md.
func TestTheTaskAnswerCarriesItsTabPlan(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	end := time.Date(2026, 9, 20, 6, 0, 0, 0, time.UTC)
	id := "7ab00002-0000-4000-8000-000000000001"
	rec := orchestrator.Record{ID: id, Kind: "custom", Title: "a task that ran out of time",
		Assistant: "claude", State: orchestrator.StateTimeout, CreatedAt: end.Add(-time.Hour),
		FinishedAt: end, ChildTerminalID: "%1", ChildBackend: "tmux"}
	raw, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	row := store.BrokerRow{ID: id, Assistant: "claude", State: string(rec.State),
		CreatedAt: rec.CreatedAt, Record: raw}
	if err := st.SaveBrokerTask(ctx, row, nil); err != nil {
		t.Fatal(err)
	}

	s := &Server{broker: &orchestrator.Broker{Store: st,
		ChildLinger: func() time.Duration { return 3 * time.Minute }}}
	w := httptest.NewRecorder()
	s.orchestratorTaskRoute(w, httptest.NewRequest(http.MethodGet, "/v1/orchestrator/tasks/"+id, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("the task answered %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		Task struct {
			Tab *contract.BrokerTab `json:"tab"`
		} `json:"task"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	tab := body.Task.Tab
	if tab == nil {
		t.Fatalf("the task's answer carries no tab plan: %s", w.Body.String())
	}
	if tab.Applied == nil || tab.Applied.Rule != contract.BrokerTabRuleUnfinishedLeftOpen ||
		tab.Applied.Close || tab.CloseAt != 0 {
		t.Errorf("a timed-out task does not read as one whose tab is left open: %s", w.Body.String())
	}
	if tab.Setting != orchestrator.TabSettingLinger || tab.Value != "180" || len(tab.Ends) != 4 {
		t.Errorf("the answer does not state which setting decided it: %s", w.Body.String())
	}
}
