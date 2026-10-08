package cloud

import (
	"encoding/json"
	"testing"
	"time"

	domain "github.com/sainteye/clawdline/internal/domain/cloud"
)

func TestViewerStatusRequiresOneFreshGenerationForEachMachine(t *testing.T) {
	now := time.Unix(1791480000, 0)
	store := NewViewerStatusStore()
	const generationA = "0123456789abcdef0123456789abcdef"
	const generationB = "abcdef0123456789abcdef0123456789"
	const passA = "11111111111111111111111111111111"
	const passB = "22222222222222222222222222222222"
	put := func(machine, session string, seq uint64, value any) {
		t.Helper()
		body, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if !store.Apply(domain.Envelope{Ch: "ss/" + machine + "/" + session, Sender: machine, Seq: seq}, body) {
			t.Fatalf("did not accept %s/%s", machine, session)
		}
	}
	row := func(machine, generation, pass string) any {
		return map[string]any{"machine_id": machine, "session_id": "same", "execution_generation": generation,
			"snapshot_generation": pass, "inventory_complete": true, "projected_at": now.Unix(),
			"state": "waiting", "waiting_for_reply": true,
			"source": map[string]any{"observed_at": now.Unix(), "freshness": "current"}}
	}
	marker := func(pass string) any {
		return map[string]any{"inventory": map[string]any{"version": 1, "sessions": []string{"same"}},
			"at": now.Unix(), "complete": true, "snapshot_generation": pass}
	}
	put("machine-a", "same", 1, row("machine-a", generationA, passA))
	put("machine-a", viewerInventorySession, 2, marker(passA))
	put("machine-b", "same", 1, row("machine-b", generationB, passB))
	put("machine-b", viewerInventorySession, 2, marker(passB))
	a := ViewerDestination{MachineID: "machine-a", SessionID: "same", ExecutionGeneration: generationA}
	b := ViewerDestination{MachineID: "machine-b", SessionID: "same", ExecutionGeneration: generationB}
	if got := store.Project("machine-a", now); got.Available(a) != "current" || got.Available(b) != "changed" {
		t.Fatalf("A projection: %+v", got)
	}
	if got := store.Project("machine-b", now); got.Available(b) != "current" || got.Available(a) != "changed" {
		t.Fatalf("B projection: %+v", got)
	}
	put("machine-a", viewerInventorySession, 3, marker(passB))
	if got := store.Project("machine-a", now); got.Reason != "event_gap" {
		t.Fatalf("mixed passes: %+v", got)
	}
	put("machine-a", "same", 4, row("machine-a", generationB, passB))
	if got := store.Project("machine-a", now); got.Available(a) != "changed" ||
		got.Available(ViewerDestination{MachineID: "machine-a", SessionID: "same", ExecutionGeneration: generationB}) != "current" {
		t.Fatalf("new execution: %+v", got)
	}
	if got := store.Project("machine-a", now.Add(6*time.Minute)); got.Reason != "stale" {
		t.Fatalf("stale projection: %+v", got)
	}
}

func TestViewerStatusAcceptsAuthoritativeEmptySnapshot(t *testing.T) {
	now := time.Now()
	store := NewViewerStatusStore()
	marker := map[string]any{"inventory": map[string]any{"version": 1, "sessions": []string{}},
		"at": now.Unix(), "complete": true, "snapshot_generation": "11111111111111111111111111111111"}
	body, _ := json.Marshal(marker)
	if !store.Apply(domain.Envelope{Ch: "ss/machine-a/" + viewerInventorySession, Sender: "machine-a", Seq: 1}, body) {
		t.Fatal("empty marker was not accepted")
	}
	projection := store.Project("machine-a", now)
	if projection.Kind != "ready" || len(projection.Rows) != 0 {
		t.Fatalf("authoritative empty snapshot: %+v", projection)
	}
}
