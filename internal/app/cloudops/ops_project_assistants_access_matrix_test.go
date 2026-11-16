package cloudops

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"strings"
	"testing"
)

// Matrix rows E1-E5 in docs/project-assistants-cloud-access-verification.md.
// Every Bot word's executable vocabulary example goes through the real
// generated decoder and Bridge admission. The router and Authority are
// recording doubles at the Bridge's own ports: the source's pin grants and
// the relay link's roster/key checks are exercised elsewhere, not here.
func TestProjectAssistantCloudWordAdmissionMatrix(t *testing.T) {
	type outcome struct {
		routed     int
		writeGates []bool
		viewer     string
		idem       string
		code       string
	}
	run := func(t *testing.T, word string, b body, allow bool, authority func(requiresWriteGate bool) Authority, mutate func(Command) Command) outcome {
		t.Helper()
		var out outcome
		bridge := Bridge{MachineID: "mac-01", AllowCommands: func() bool { return allow },
			Router: projectAssistantRouter(func(_ context.Context, r LocalRequest) (LocalResponse, error) {
				out.routed++
				out.viewer, out.idem = r.Viewer, r.Header["Idempotency-Key"]
				if word == "project-assistants.attachment" {
					return LocalResponse{Status: 200, ContentType: "image/png", Body: []byte("picture")}, nil
				}
				return LocalResponse{Status: 200, Body: []byte(`{"contract_version":"project-assistants.v1"}`)}, nil
			})}
		if authority != nil {
			bridge.Authority = func(_ context.Context, _ string, _ ed25519.PublicKey, requiresWriteGate bool) Authority {
				out.writeGates = append(out.writeGates, requiresWriteGate)
				return authority(requiresWriteGate)
			}
		}
		cmd := request(t, ClassCtl, b)
		if mutate != nil {
			cmd = mutate(cmd)
		}
		a := bridge.Handle(context.Background(), cmd)
		out.code = a.Code
		if !a.OK() && out.code == "" {
			var failure struct {
				Error struct{ Code string } `json:"error"`
			}
			_ = json.Unmarshal(a.Payload, &failure)
			out.code = failure.Error.Code
		}
		return out
	}
	allowAll := func(bool) Authority {
		return Authority{ClockReady: true, RosterReadable: true, RosterAllowsSender: true, WriteGateAllows: true}
	}
	// The relay link marks a read-only pin only when a write gate is asked.
	readOnlyViewer := func(requiresWriteGate bool) Authority {
		a := allowAll(requiresWriteGate)
		a.ViewerReadOnly = requiresWriteGate
		return a
	}
	unknownSender := func(bool) Authority {
		return Authority{ClockReady: true, RosterReadable: true}
	}
	for _, row := range projectAssistantRows(t) {
		row := row
		o := catalog[row.Op]
		t.Run(strings.TrimPrefix(row.Op, "project-assistants."), func(t *testing.T) {
			write := !o.read && !o.readLevel
			// E1: admitted words reach the source exactly once as the verified
			// sender, and commands carry their own idempotency identity.
			got := run(t, row.Op, projectAssistantExample(t, row), true, allowAll, nil)
			if got.routed != 1 || got.viewer != "viewer-device-01" {
				t.Fatalf("E1 routed=%d viewer=%q code=%s", got.routed, got.viewer, got.code)
			}
			if !o.read {
				example := projectAssistantExample(t, row)
				command, _ := example["command"].(map[string]any)
				want := example["request"]
				if command != nil && want == nil {
					want = command["idempotency_key"]
				}
				if got.idem == "" || (want != nil && got.idem != want) {
					t.Fatalf("E1 idempotency %q want %v", got.idem, want)
				}
				// Only a real command asks for the write gate; read/observed are
				// a viewer's own metadata.
				if len(got.writeGates) != 1 || got.writeGates[0] != write {
					t.Fatalf("E1 write gates %v", got.writeGates)
				}
			} else if len(got.writeGates) != 0 {
				t.Fatalf("E1 read consulted command authority %v", got.writeGates)
			}
			// E2: the machine write switch stops commands before routing and
			// never stops reads or read/observed metadata.
			got = run(t, row.Op, projectAssistantExample(t, row), false, allowAll, nil)
			if write && (got.routed != 0 || got.code != "cloud_commands_disabled") {
				t.Fatalf("E2 disabled write routed=%d code=%s", got.routed, got.code)
			}
			if !write && got.routed != 1 {
				t.Fatalf("E2 disabled switch blocked a read: %s", got.code)
			}
			// E3: a read-only viewer is refused every command before routing,
			// and keeps reads and its own read/observed metadata.
			got = run(t, row.Op, projectAssistantExample(t, row), true, readOnlyViewer, nil)
			if write && (got.routed != 0 || got.code != "viewer_read_only") {
				t.Fatalf("E3 read-only write routed=%d code=%s", got.routed, got.code)
			}
			if !write && got.routed != 1 {
				t.Fatalf("E3 read-only viewer lost a read: %s", got.code)
			}
			// E4: a sender the roster no longer holds is refused every command;
			// reads are not refused by the Bridge and remain the source pin
			// grant's decision (transport/http matrix rows A1 and B1).
			got = run(t, row.Op, projectAssistantExample(t, row), true, unknownSender, nil)
			if !o.read && (got.routed != 0 || got.code != "unknown_sender") {
				t.Fatalf("E4 unknown sender routed=%d code=%s", got.routed, got.code)
			}
			if o.read && got.routed != 1 {
				t.Fatalf("E4 Bridge-level read refusal changed: %s", got.code)
			}
			// E5: a body cannot name its own viewer, and a request addressed to
			// another machine is not answered here.
			spoofed := projectAssistantExample(t, row)
			spoofed["viewer"] = "owner-device"
			if got = run(t, row.Op, spoofed, true, allowAll, nil); got.routed != 0 {
				t.Fatalf("E5 body viewer accepted: %s", got.code)
			}
			other := func(c Command) Command { c.Channel = "ctl/mac-02"; return c }
			if got = run(t, row.Op, projectAssistantExample(t, row), true, allowAll, other); got.routed != 0 || got.code != "wrong_machine" {
				t.Fatalf("E5 foreign machine routed=%d code=%s", got.routed, got.code)
			}
		})
	}
}
