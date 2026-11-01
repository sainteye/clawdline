//go:build darwin || linux

package antigravitysession

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/antigravityprocess"
)

func TestRawTraceIsOptInPrivateAndSeparateFromFacts(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "disabled", true: "enabled"}[enabled], func(t *testing.T) {
			h, row, opts := persistentFixture(t, enabled)
			if err := os.WriteFile(filepath.Join(opts.AccountDir, "release"), nil, 0600); err != nil {
				t.Fatal(err)
			}
			if err := h.Send(context.Background(), row, "synthetic private prompt"); err != nil {
				t.Fatal(err)
			}
			idleHost(t, h, row)
			facts, err := h.Facts(row)
			if err != nil || facts.Protocol == nil || facts.Protocol.Phase != "idle" || facts.Protocol.StopReason != "end_turn" || facts.Protocol.TraceEnabled != enabled {
				t.Fatal(facts, err)
			}
			raw, _ := json.Marshal(facts.Protocol)
			if strings.Contains(string(raw), "synthetic private prompt") {
				t.Fatal("raw content reached summary")
			}
			st := waitWire(t, h)
			if !st.Known || (st.Records > 0) != enabled {
				t.Fatal(st)
			}
			if !enabled {
				return
			}
			// Decode the stored binary field, not a spelling of its base64 encoding.
			rows, err := h.trace.db.Query(`SELECT record FROM acp_wire WHERE owned_id=? ORDER BY seq`, row.ID)
			if err != nil {
				t.Fatal(err)
			}
			foundIn, foundOut := false, false
			for rows.Next() {
				var body []byte
				if err := rows.Scan(&body); err != nil {
					t.Fatal(err)
				}
				var e antigravityprocess.WireEvent
				if json.Unmarshal(body, &e) != nil {
					t.Fatal("invalid wire record")
				}
				if e.Direction == "in" && len(e.Frame) > 0 {
					foundIn = true
				}
				if e.Direction == "out" && bytes.Contains(e.Frame, []byte("synthetic private prompt")) {
					foundOut = true
				}
			}
			rows.Close()
			if !foundIn || !foundOut {
				t.Fatal("missing bidirectional raw frames", foundIn, foundOut)
			}
			info, err := os.Stat(filepath.Join(opts.StateDir, "acp-wire.sqlite"))
			if err != nil || info.Mode().Perm() != 0600 {
				t.Fatal(info, err)
			}
			stopHost(t, h)
			h = reopenHost(t, opts)
			if st = h.WireTraceStats(); !st.Known || st.Records == 0 {
				t.Fatal("trace lost on restart", st)
			}
		})
	}
}

func TestRawTraceBoundsAcrossWorkersAndExpires(t *testing.T) {
	r, err := openWireRecorder(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.close()
	e := antigravityprocess.WireEvent{At: time.Now().Add(-MaxWireTraceAge - time.Second).UnixNano(), Direction: "in", Stage: "received", Frame: []byte("old")}
	if err := writeWire(t, r, "a", "old", e); err != nil {
		t.Fatal(err)
	}
	if st := r.snapshot(); st.Records != 0 {
		t.Fatal("expired trace retained", st)
	}
	e.At = time.Now().UnixNano()
	e.Frame = bytes.Repeat([]byte("x"), MaxFrameBytes)
	for i := 0; i < MaxWireTraceBytes/MaxFrameBytes+1; i++ {
		e.Sequence = uint64(i + 1)
		if err := writeWire(t, r, "a", map[bool]string{false: "one", true: "two"}[i%2 == 0], e); err != nil {
			t.Fatal(err)
		}
	}
	if st := r.snapshot(); !st.Known || st.Bytes > MaxWireTraceBytes || st.Records == 0 {
		t.Fatal(st)
	}
	if _, err := r.db.Exec(`DELETE FROM acp_wire`); err != nil {
		t.Fatal(err)
	}
	if _, err := r.db.Exec(`WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<?) INSERT INTO acp_wire(owned_id,worker,at_ns,record,bytes) SELECT 'a','seed',?,'{}',2 FROM n`, MaxWireTraceRecords, e.At); err != nil {
		t.Fatal(err)
	}
	e.Frame = []byte("newest")
	if err := writeWire(t, r, "b", "new", e); err != nil {
		t.Fatal(err)
	}
	if st := r.snapshot(); st.Records != MaxWireTraceRecords {
		t.Fatal(st)
	}
	var worker string
	if err := r.db.QueryRow(`SELECT worker FROM acp_wire ORDER BY seq DESC LIMIT 1`).Scan(&worker); err != nil || worker != "new" {
		t.Fatal(worker, err)
	}
}

func TestBrokenRawRecorderDoesNotStrandConversation(t *testing.T) {
	h, row, opts := persistentFixture(t, true)
	if err := os.WriteFile(filepath.Join(opts.AccountDir, "release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := h.trace.db.Exec(`DROP TABLE acp_wire`); err != nil {
		t.Fatal(err)
	}
	if err := h.Send(context.Background(), row, "synthetic input"); err != nil {
		t.Fatal(err)
	}
	idleHost(t, h, row)
	waitWire(t, h)
	facts, err := h.Facts(row)
	if err != nil || facts.Protocol.Phase != "idle" || facts.Protocol.TraceError != "trace_write_failed" {
		t.Fatal(facts, err)
	}
}

func writeWire(t *testing.T, r *wireRecorder, owner, worker string, e antigravityprocess.WireEvent) error {
	t.Helper()
	raw, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	return r.write(context.Background(), wireRecord{owner: owner, worker: worker, at: e.At, raw: raw})
}

func TestBlockedRawDiskAndFullQueueDoNotBlockConversation(t *testing.T) {
	for _, large := range []bool{false, true} {
		t.Run(map[bool]string{false: "records", true: "bytes"}[large], func(t *testing.T) {
			h, row, opts := persistentFixture(t, true)
			waitWire(t, h)
			tx, err := h.trace.db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if err := os.WriteFile(filepath.Join(opts.AccountDir, "release"), nil, 0600); err != nil {
				t.Fatal(err)
			}
			if err := h.Send(context.Background(), row, "synthetic request while trace disk is blocked"); err != nil {
				t.Fatal(err)
			}
			idleHost(t, h, row)
			e := antigravityprocess.WireEvent{At: time.Now().UnixNano(), Direction: "in", Stage: "received", Frame: []byte("synthetic")}
			if large {
				e.Frame = bytes.Repeat([]byte("x"), MaxFrameBytes)
			}
			var refused bool
			for i := 0; i <= MaxWireQueueRecords; i++ {
				if err := h.trace.enqueue(row.ID, "overload", e); err != nil {
					refused = true
					break
				}
			}
			st := h.WireTraceStats()
			if !refused || st.QueueRecords > MaxWireQueueRecords || st.QueueBytes > MaxWireQueueBytes || st.Dropped == 0 {
				t.Fatal(st)
			}
			if large && st.QueueRecords >= MaxWireQueueRecords {
				t.Fatal("byte limit was not exercised", st)
			}
			facts, err := h.Facts(row)
			if err != nil || facts.Protocol.Phase != "idle" || facts.Protocol.TraceError != "trace_write_failed" {
				t.Fatal(facts, err)
			}
			if err := tx.Rollback(); err != nil {
				t.Fatal(err)
			}
			waitWire(t, h)
		})
	}
}
func waitWire(t *testing.T, h *Host) WireTraceStats {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		st := h.WireTraceStats()
		if st.QueueRecords == 0 {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatal("trace did not drain", st)
		}
		time.Sleep(time.Millisecond)
	}
}
