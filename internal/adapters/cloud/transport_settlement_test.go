package cloud

import (
	"encoding/json"
	"testing"
	"time"
)

func TestTerminalPublishFailurePreservesSettlementKind(t *testing.T) {
	now := time.Now()
	spool := testSpool(t, &now)
	channel := "term/machine/viewer/AAAAAAAAAAAAAAAAAAAAAA"
	seq, err := spool.ReserveLatestValue(SpoolChannelTerm, channel, channel, 100)
	if err != nil {
		t.Fatal(err)
	}
	sealRow(t, spool, seq, now)
	if row := spool.SendNext().Row; row == nil || row.Seq != seq {
		t.Fatal("terminal frame was not sent")
	}
	called := 0
	transport := &Transport{opts: Options{Spool: spool, Status: NewStatusRecorder(now),
		OnSettled: func(ch string, got uint64, kind SettleKind) {
			called++
			if ch != channel || got != seq || kind != SettlePeerError {
				t.Fatalf("settlement: %s %d %s", ch, got, kind)
			}
		}}}
	body, err := json.Marshal(PublishErrorFrame{Type: FramePublishError, Ch: channel, Seq: seq, Code: "forbidden", Field: "ch"})
	if err != nil {
		t.Fatal(err)
	}
	transport.handlePublishError(body)
	transport.handlePublishError(body)
	if called != 1 {
		t.Fatalf("terminal refusal called settlement %d times", called)
	}
}

func TestTerminalRateLimitKeepsDistinctSettlement(t *testing.T) {
	now := time.Now()
	spool := testSpool(t, &now)
	channel := "term/machine/viewer/AAAAAAAAAAAAAAAAAAAAAA"
	seq, err := spool.ReserveLatestValue(SpoolChannelTerm, channel, channel, 100)
	if err != nil {
		t.Fatal(err)
	}
	sealRow(t, spool, seq, now)
	if row := spool.SendNext().Row; row == nil || row.Seq != seq {
		t.Fatal("frame was not sent")
	}
	called := 0
	transport := &Transport{opts: Options{Spool: spool, Status: NewStatusRecorder(now),
		OnSettled: func(ch string, got uint64, kind SettleKind) {
			called++
			if ch != channel || got != seq || kind != SettleRateLimited {
				t.Fatalf("settlement: %s %d %s", ch, got, kind)
			}
		}}}
	body, err := json.Marshal(PublishErrorFrame{Type: FramePublishError, Ch: channel, Seq: seq, Code: "rate_limited", Field: "ch"})
	if err != nil {
		t.Fatal(err)
	}
	transport.handlePublishError(body)
	transport.handlePublishError(body)
	if called != 1 {
		t.Fatalf("rate limit settled %d times", called)
	}
	if row, ok := spool.Row(seq); !ok || row.State != SpoolRejected {
		t.Fatalf("rate limited row: %+v", row)
	}
}
