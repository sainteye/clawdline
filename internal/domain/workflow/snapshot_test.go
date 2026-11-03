package workflow

import (
	"encoding/json"
	"testing"
)

func testSnapshot() Snapshot {
	return Snapshot{ProfileID: "custom.example", Version: "1", Source: "local", Stages: []Stage{{ID: "verify", Title: "Verify", Output: "Evidence", Acceptance: "Existing checks pass"}}}
}

func TestSnapshotSealsCanonicalCopyAndDetectsEveryFieldChange(t *testing.T) {
	original := testSnapshot()
	sealed, err := Seal(original)
	if err != nil {
		t.Fatal(err)
	}
	if original.Digest != "" || original.Schema != 0 {
		t.Fatal("input was changed")
	}
	if sealed.Schema != 1 || sealed.Validate() != nil {
		t.Fatalf("invalid seal: %+v", sealed)
	}
	again, err := Seal(sealed)
	if err != nil || again.Digest != sealed.Digest {
		t.Fatal("seal was not stable")
	}
	changes := []func(*Snapshot){func(s *Snapshot) { s.ProfileID = "other" }, func(s *Snapshot) { s.Version = "2" }, func(s *Snapshot) { s.Source = "remote" }, func(s *Snapshot) { s.Instructions = "changed" }, func(s *Snapshot) { s.GraphifyEnabled = true }, func(s *Snapshot) { s.Stages[0].Acceptance = "changed" }, func(s *Snapshot) { s.Stages[0].ID = "changed" }, func(s *Snapshot) { s.Stages[0].Title = "changed" }, func(s *Snapshot) { s.Stages[0].Output = "changed" }, func(s *Snapshot) { s.Schema = 2 }}
	for i, change := range changes {
		s := sealed
		s.Stages = append([]Stage(nil), sealed.Stages...)
		change(&s)
		if s.Validate() == nil {
			t.Errorf("change %d retained validity", i)
		}
	}
	encoded, _ := json.Marshal(sealed)
	var decoded Snapshot
	if err := json.Unmarshal(encoded, &decoded); err != nil || decoded.Validate() != nil {
		t.Fatalf("round trip: %v", err)
	}
}

func TestSnapshotAllowsStandardWithoutInventedStagesAndRejectsMalformedDefinitions(t *testing.T) {
	standard, err := Seal(Snapshot{ProfileID: "clawdline", Version: "1", Source: "builtin"})
	if err != nil || standard.Instructions != "" || len(standard.Stages) != 0 || standard.Validate() != nil {
		t.Fatalf("standard: %+v %v", standard, err)
	}
	for _, s := range []Snapshot{{ProfileID: "", Version: "1", Source: "builtin"}, {ProfileID: "x", Version: "", Source: "builtin"}, {ProfileID: "x", Version: "1", Source: ""}, {Schema: 2, ProfileID: "x", Version: "1", Source: "builtin"}, {ProfileID: "x", Version: "1", Source: "builtin", Stages: []Stage{{ID: "x"}}}, {ProfileID: "x", Version: "1", Source: "builtin", Stages: []Stage{{ID: "x", Title: "X", Output: "X", Acceptance: "X"}, {ID: "x", Title: "Y", Output: "Y", Acceptance: "Y"}}}} {
		if _, err := Seal(s); err == nil {
			t.Fatalf("accepted malformed: %+v", s)
		}
	}
}
