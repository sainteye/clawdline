package cloud

import (
	"encoding/json"
	"errors"
	"testing"

	domain "github.com/sainteye/clawdline/internal/domain/cloud"
)

func TestViewerPendingRefusalsAreBoundToChannelAndUncertainEffectsStayQueryable(t *testing.T) {
	action := &viewerPending{key: "action", channel: "ctl/machine-a", seq: 8, action: true, answer: make(chan viewerReply, 1)}
	read := &viewerPending{key: "read", channel: "r/machine-a", seq: 9, answer: make(chan viewerReply, 1)}
	client := &ViewerClient{pending: map[string]*viewerPending{action.key: action, read.key: read},
		pendingSeq: map[uint64]*viewerPending{action.seq: action, read.seq: read}, update: make(chan struct{}, 1)}
	client.onAck(AckFrame{Ch: "ctl/machine-b", Seq: action.seq, Status: AckMachineOffline})
	select {
	case <-action.answer:
		t.Fatal("an unrelated machine's ack settled the action")
	default:
	}
	client.onPublishRefusal(PublishErrorFrame{Ch: "ctl/machine-b", Seq: action.seq, Code: "no_permission"})
	select {
	case <-action.answer:
		t.Fatal("an unrelated machine's refusal settled the action")
	default:
	}
	client.onDisconnect()
	if result := <-action.answer; result.err == nil || result.err.Error() != "receipt_outcome_unknown" {
		t.Fatalf("an interrupted action was claimed to have failed: %+v", result)
	}
	if result := <-read.answer; !errors.Is(result.err, ErrViewerOffline) {
		t.Fatalf("effect-free read was not offline: %+v", result)
	}
}

func TestViewerPinnedReadRequiresTheExactReplyProof(t *testing.T) {
	target := ViewerDestination{MachineID: "machine-a", SessionID: "same", ExecutionGeneration: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	base := map[string]any{"read": "info.full", "machine_id": target.MachineID,
		"session_id": target.SessionID, "expected_generation": target.ExecutionGeneration,
		"seq": uint64(17), "status": 200, "body": map[string]any{"title": "current"}}
	for _, tc := range []struct {
		name string
		edit func(map[string]any)
		good bool
	}{
		{"matching", func(map[string]any) {}, true},
		{"missing machine", func(v map[string]any) { delete(v, "machine_id") }, false},
		{"different session", func(v map[string]any) { v["session_id"] = "other" }, false},
		{"old execution", func(v map[string]any) { v["expected_generation"] = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" }, false},
		{"old sequence", func(v map[string]any) { v["seq"] = uint64(16) }, false},
		{"missing sequence", func(v map[string]any) { delete(v, "seq") }, false},
		{"wrong read name", func(v map[string]any) { v["read"] = "transcript.before.512" }, false},
		{"missing read name", func(v map[string]any) { delete(v, "read") }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := make(map[string]any, len(base))
			for key, value := range base {
				payload[key] = value
			}
			tc.edit(payload)
			body, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			p := &viewerPending{key: viewerReplyKey(target.MachineID, target.SessionID, "info.full"),
				channel: "r/machine-a", seq: 17, pinnedRead: true, destination: target, answer: make(chan viewerReply, 1)}
			client := &ViewerClient{pending: map[string]*viewerPending{p.key: p}, pendingSeq: map[uint64]*viewerPending{p.seq: p}}
			client.acceptReply(domain.Envelope{Ch: "t/machine-a/same", Sender: "machine-a"}, body)
			select {
			case result := <-p.answer:
				if tc.good {
					if result.err != nil || len(result.body) == 0 {
						t.Fatalf("matching proof failed: %+v", result)
					}
				} else {
					var refusal ViewerRefusal
					if !errors.As(result.err, &refusal) || refusal.Code != "read_reply_mismatch" {
						t.Fatalf("unproven reply accepted: %+v", result)
					}
				}
			default:
				t.Fatal("pinned reply did not settle")
			}
		})
	}
}

func TestViewerOlderTranscriptPageKeepsThePinnedTargetAndCursor(t *testing.T) {
	target := ViewerDestination{MachineID: "machine-a", SessionID: "same", ExecutionGeneration: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	name, command := viewerTranscriptCommand(target, 512)
	if name != "transcript.before.512" || command["before"] != int64(512) ||
		command["machine_id"] != target.MachineID || command["session"] != target.SessionID ||
		command["expected_generation"] != target.ExecutionGeneration || command["limit"] != 200 {
		t.Fatalf("older page lost its pinned target: %s %+v", name, command)
	}
	for _, tc := range []struct {
		body string
		want int64
		ok   bool
	}{
		{`{"id":"same","entries":[],"nextBefore":256}`, 256, true},
		{`{"id":"same","entries":[]}`, 0, true},
		{`{"id":"other","entries":[],"nextBefore":256}`, 0, false},
		{`{"id":"same","entries":[],"nextBefore":0}`, 0, false},
		{`{"id":"same","entries":null,"nextBefore":256}`, 0, false},
		{`{"id":"same","entries":[],"nextBefore":"256"}`, 0, false},
	} {
		cursor, err := viewerTranscriptCursor(json.RawMessage(tc.body), target)
		if (err == nil) != tc.ok || tc.ok && ((cursor == nil && tc.want != 0) || (cursor != nil && *cursor != tc.want)) {
			t.Errorf("cursor %s: got %v, %v; want %d, ok %v", tc.body, cursor, err, tc.want, tc.ok)
		}
	}
}
