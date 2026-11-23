package projectassistant

import (
	"testing"
	"time"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type compactionReader struct {
	ProviderSession
	state NativeContextState
}

func (s compactionReader) NativeContext() NativeContextState { return s.state }

func TestCompactionNoticeIsWrittenOnlyForTheReportedTurn(t *testing.T) {
	turn := pa.Turn{ID: "t1", Fence: pa.Fence{BindingID: "b"}}
	at := time.Unix(1, 0)
	s := compactionReader{state: NativeContextState{Fence: turn.Fence, CompactedTurnID: "t1", Compactions: 1, CompactedBefore: 345000, CompactedAfter: 41000}}
	m := compactionNotice(s, turn, at)
	if m == nil || m.ID != "compacted-t1" || m.Kind != pa.NoticeMessage || m.Text != "Earlier context was summarized to make room: 345K → 41K" {
		t.Fatalf("%+v", m)
	}
	s.state.CompactedTurnID = "t0"
	if compactionNotice(s, turn, at) != nil {
		t.Fatal("an earlier turn's compaction was written for this one")
	}
	s.state.CompactedTurnID, s.state.Compactions = "t1", 0
	if compactionNotice(s, turn, at) != nil {
		t.Fatal("a notice without a reported compaction")
	}
}

func TestCompactionIsRedeliveredOnlyOnTheNextTurn(t *testing.T) {
	ts := func(n int64) *time.Time { v := time.Unix(n, 0); return &v }
	f := pa.Fence{BindingID: "b"}
	x := Export{
		Turns:    []pa.Turn{{ID: "t1", Fence: f, State: pa.TurnSucceeded, StartedAt: ts(1)}},
		Messages: []pa.Message{{ID: "compacted-t1", TurnID: "t1", Kind: pa.NoticeMessage}},
	}
	next := pa.Turn{ID: "t2", Fence: f}
	if !compactedBeforeTurn(x, next, "b") {
		t.Fatal("the turn after a compaction does not repeat memory")
	}
	if compactedBeforeTurn(x, next, "other") {
		t.Fatal("another binding repeated memory")
	}
	x.Turns = append(x.Turns, pa.Turn{ID: "t2", Fence: f, State: pa.TurnSucceeded, StartedAt: ts(2)})
	if compactedBeforeTurn(x, pa.Turn{ID: "t3", Fence: f}, "b") {
		t.Fatal("memory was repeated twice for one compaction")
	}
}
