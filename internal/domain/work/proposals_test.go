package work

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// The gate of board-redesign §4.4 as a pure function: each refusal and each
// reason not to ask, and beside each the smallest change of facts that turns
// the answer — so a gate that said the same thing to everything would fail
// here (DG-8).

var gateNow = time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)

func gateCode(err error) string {
	var r *Refusal
	if errors.As(err, &r) {
		return r.Code
	}
	return ""
}

func TestTheGateRefusesAndItsControlsPass(t *testing.T) {
	p := DefaultProposalPolicy()
	here := gateNow.Add(-time.Minute)
	ok := ProposalFacts{Signals: []Signal{SignalCrossSession}, HeardAt: here}
	cases := []struct {
		name   string
		facts  ProposalFacts
		code   string
		ask    bool
		reason string
	}{
		{"control: asked", ok, "", true, AskHumanPresent},
		{"below threshold", ProposalFacts{HeardAt: here}, RefuseBelowThreshold, false, ""},
		{"already tracked: board", ProposalFacts{Signals: ok.Signals, HasItem: true,
			Item: Item{Place: PlaceBoard, State: ItemActive}}, RefuseAlreadyTracked, false, ""},
		{"already tracked: answered track", ProposalFacts{Signals: ok.Signals,
			Prior: []Proposal{{State: ProposalAnswered, Answer: AnswerTrack}}}, RefuseAlreadyTracked, false, ""},
		{"duplicate: pending", ProposalFacts{Signals: ok.Signals, Prior: []Proposal{{State: ProposalPending}}},
			RefuseDuplicate, false, ""},
		{"duplicate: untracked by a person", ProposalFacts{Signals: ok.Signals, HasItem: true, Item: Item{Place: PlaceTodo}},
			RefuseDuplicate, false, ""},
		{"duplicate: no new signal after a no", ProposalFacts{Signals: ok.Signals, HeardAt: here,
			Prior: []Proposal{{State: ProposalAnswered, Answer: AnswerNo, Signals: ok.Signals}}}, RefuseDuplicate, false, ""},
		{"control: a new signal after a no", ProposalFacts{Signals: []Signal{SignalCrossSession, SignalLongLived},
			HeardAt: here, Prior: []Proposal{{State: ProposalExpired, Signals: ok.Signals}}}, "", true, AskHumanPresent},
		{"duplicate: only once more", ProposalFacts{Signals: []Signal{SignalExternalEffect}, HeardAt: here,
			Prior: []Proposal{{State: ProposalExpired, Signals: ok.Signals},
				{State: ProposalAnswered, Answer: AnswerNo, Signals: []Signal{SignalLongLived}}}}, RefuseDuplicate, false, ""},
		{"from a child", ProposalFacts{Signals: ok.Signals, HeardAt: here, FromChild: true}, "", false, AskFromChild},
		{"by the rules", ProposalFacts{Signals: []Signal{SignalCrossSession, SignalLongLived}, HeardAt: here,
			ByRule: true}, "", false, AskByRule},
		{"by the rules, below threshold", ProposalFacts{HeardAt: here, ByRule: true}, RefuseBelowThreshold, false, ""},
		// I1 on its own is every dispatch there is; a rule resting on it
		// would ask once per dispatch (RuleWorthy). The control above is the
		// same facts with a to-do of it owed past a day.
		{"by the rules, on a dispatch alone", ProposalFacts{Signals: ok.Signals, HeardAt: here, ByRule: true},
			RefuseBelowThreshold, false, ""},
		{"nobody heard", ProposalFacts{Signals: ok.Signals}, "", false, AskHumanAbsent},
		{"heard too long ago", ProposalFacts{Signals: ok.Signals, HeardAt: gateNow.Add(-31 * time.Minute)}, "", false,
			AskHumanAbsent},
		{"asked this turn", ProposalFacts{Signals: ok.Signals, HeardAt: here, AskedThisTurn: 1}, "", false,
			AskBudgetExhausted},
		{"three today", ProposalFacts{Signals: ok.Signals, HeardAt: here, AskedToday: 3}, "", false, AskBudgetExhausted},
		{"control: two today", ProposalFacts{Signals: ok.Signals, HeardAt: here, AskedToday: 2}, "", true, AskHumanPresent},
	}
	for _, c := range cases {
		v, err := GateProposal(c.facts, p, gateNow)
		if gateCode(err) != c.code {
			t.Errorf("%s: refusal %q, want %q", c.name, gateCode(err), c.code)
			continue
		}
		if err != nil {
			continue
		}
		if v.Ask != c.ask || v.Reason != c.reason || (v.Channel == ChannelSession) != c.ask {
			t.Errorf("%s: %+v, want ask %v %s", c.name, v, c.ask, c.reason)
		}
	}
}

// Resolving is the evidence-backed answer to a different question than `no`:
// the subject was checked and no longer exists. It therefore closes the row
// and the line, even when a later proposal carries a signal the old one did
// not. A note without its source is only an opinion and is refused.
func TestAResolvedProposalNeverReturns(t *testing.T) {
	pending := Proposal{ID: "proposal", State: ProposalPending}
	if _, err := ResolveProposal(pending, "The defect was fixed.", "", "root:session", gateNow); gateCode(err) != "proposal_evidence_required" {
		t.Fatalf("resolution without evidence: %v", err)
	}
	resolved, err := ResolveProposal(pending, "The defect was fixed.", "internal/worker/retry.go:84", "root:session", gateNow)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.State != ProposalResolved || resolved.Resolution != "The defect was fixed." ||
		resolved.ResolutionEvidence != "internal/worker/retry.go:84" || resolved.ResolvedBy != "root:session" ||
		!resolved.ResolvedAt.Equal(gateNow) {
		t.Fatalf("resolved row: %+v", resolved)
	}

	facts := ProposalFacts{Signals: []Signal{SignalCrossSession, SignalLongLived},
		Prior: []Proposal{resolved}, HeardAt: gateNow.Add(-time.Minute)}
	if _, err := GateProposal(facts, DefaultProposalPolicy(), gateNow); gateCode(err) != RefuseResolved {
		t.Fatalf("the resolved line was proposed again: %v", err)
	}
}

func TestTheSignalsNeverCountStepsOrRunsNobodyOwns(t *testing.T) {
	tasks := []TaskFacts{{Task: "r", Kind: "code-review", Owner: "root"}, {Task: "s", Kind: "custom"},
		{Task: "q", Kind: "question", Owner: "root"}}
	old := []Todo{{Task: "r", State: TodoStateOpen, CreatedAt: gateNow.Add(-48 * time.Hour)}}
	signals, ignored := SignalsOf(tasks, old, nil, gateNow)
	if len(signals) != 0 || len(ignored) != 3 {
		t.Fatalf("steps and runs nobody owns: %v %v", signals, ignored)
	}
	// Control: a task a root sent, its to-do two days old, a declared effect.
	tasks = append(tasks, TaskFacts{Task: "c", Kind: "custom", Owner: "root"})
	old = append(old, Todo{Task: "c", State: TodoStateOpen, CreatedAt: gateNow.Add(-48 * time.Hour)})
	signals, _ = SignalsOf(tasks, old, []Effect{EffectPublish}, gateNow)
	if len(signals) != 3 {
		t.Fatalf("control: %v", signals)
	}
}

// A title is the promised result, not the first observation its author made.
// These are the four titles measured from the board on 2026-09-22, plus the
// tempting near-solution that is tidy but still says nothing a reader can
// verify changed.
func TestAnOutcomeTitleRefusesObservedAndGenericShapes(t *testing.T) {
	bad := map[string]string{
		"使用者問了三次『現在到底是什麼狀況』，而沒有一頁在答這個問題": "使用者",
		"還在照舊 app 的規則走的地方，要改成照現在的":       "的地方",
		"`activity` 只影響排序，沒有畫在列上":        "`activity`",
		"剛開的 Codex 認不出來：沒有 resume 就沒有身分": "冒號",
		"修正 session 列表顯示問題":              "完成後",
	}
	for title, teaching := range bad {
		if said := OutcomeTitleRefusal(title); !strings.Contains(said, teaching) {
			t.Errorf("%q was refused as %q; want teaching about %q", title, said, teaching)
		}
	}
	for _, title := range []string{
		"狀態頁會直接回答目前進度與下一步",
		"退役 app 的規則不再決定新 daemon 的行為",
		"Session 列會顯示最後活動時間",
		"新開的 Codex 會在沒有 resume 時取得身分",
	} {
		if said := OutcomeTitleRefusal(title); said != "" {
			t.Errorf("%q was refused: %s", title, said)
		}
	}
	if said := OutcomeTitleRefusal(strings.Repeat("字", OutcomeTitleLimit+1)); !strings.Contains(said, "at most") {
		t.Errorf("a title past the bound was refused as %q", said)
	}
}

func TestADecisionNamesItsDefault(t *testing.T) {
	p := DefaultDecisionPolicy()
	opts := func() []Option { return []Option{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}} }
	for name, c := range map[string]struct {
		d    Decision
		due  time.Duration
		code string
	}{
		"no default":      {Decision{Question: "q", Options: opts()}, 0, "decision_default_required"},
		"unknown default": {Decision{Question: "q", Options: opts(), Default: "c"}, 0, "decision_default_unknown"},
		"one option":      {Decision{Question: "q", Options: opts()[:1], Default: "a"}, 0, "invalid_options"},
		"too soon":        {Decision{Question: "q", Options: opts(), Default: "a"}, time.Minute, "invalid_due"},
		"too late":        {Decision{Question: "q", Options: opts(), Default: "a"}, 8 * 24 * time.Hour, "invalid_due"},
		"no question":     {Decision{Question: " ", Options: opts(), Default: "a"}, 0, "invalid_question"},
		"control":         {Decision{Question: "q", Options: opts(), Default: "a", Blocking: true}, 0, ""},
	} {
		d, err := NewDecision(c.d, c.due, p, gateNow)
		if gateCode(err) != c.code {
			t.Errorf("%s: %v, want %q", name, err, c.code)
		}
		if err == nil && (d.DueAt != gateNow.Add(p.Due) || d.Push != PushPending || d.State != DecisionOpen) {
			t.Errorf("%s: %+v", name, d)
		}
	}
	d, _ := NewDecision(Decision{Question: "q", Options: opts(), Default: "b"}, time.Hour, p, gateNow)
	if _, ok := DefaultDecision(d, gateNow.Add(59*time.Minute)); ok {
		t.Fatal("defaulted before its due")
	}
	if got, ok := DefaultDecision(d, gateNow.Add(time.Hour)); !ok || got.Answer != "b" || got.AnsweredBy != ActorRule ||
		got.Push != PushNone {
		t.Fatalf("at its due: %+v", got)
	}
}

func TestDigestWindows(t *testing.T) {
	p := DigestPolicy{Location: time.UTC}
	key, from, to := DigestWindow(DigestDaily, gateNow, p)
	if key != "daily:2026-09-18" || !from.Equal(time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC)) ||
		!to.Equal(time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("daily %s %v %v", key, from, to)
	}
	// 2026-09-19 is a Saturday: the week began on Monday the 14th.
	key, from, to = DigestWindow(DigestWeekly, gateNow, p)
	if key != "weekly:2026-W38" || !to.Equal(time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)) ||
		!from.Equal(time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("weekly %s %v %v", key, from, to)
	}
}
