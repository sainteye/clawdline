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
	if sealed.Schema != 2 || sealed.Validate() != nil {
		t.Fatalf("invalid seal: %+v", sealed)
	}
	again, err := Seal(sealed)
	if err != nil || again.Digest != sealed.Digest {
		t.Fatal("seal was not stable")
	}
	changes := []func(*Snapshot){func(s *Snapshot) { s.ProfileID = "other" }, func(s *Snapshot) { s.Version = "2" }, func(s *Snapshot) { s.Source = "remote" }, func(s *Snapshot) { s.Instructions = "changed" }, func(s *Snapshot) { s.CodeGraphEnabled = true }, func(s *Snapshot) { s.Stages[0].Acceptance = "changed" }, func(s *Snapshot) { s.Stages[0].ID = "changed" }, func(s *Snapshot) { s.Stages[0].Title = "changed" }, func(s *Snapshot) { s.Stages[0].Output = "changed" }, func(s *Snapshot) { s.Schema = 3 }}
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
	for _, s := range []Snapshot{{ProfileID: "", Version: "1", Source: "builtin"}, {ProfileID: "x", Version: "", Source: "builtin"}, {ProfileID: "x", Version: "1", Source: ""}, {Schema: 3, ProfileID: "x", Version: "1", Source: "builtin"}, {ProfileID: "x", Version: "1", Source: "builtin", Stages: []Stage{{ID: "x"}}}, {ProfileID: "x", Version: "1", Source: "builtin", Stages: []Stage{{ID: "x", Title: "X", Output: "X", Acceptance: "X"}, {ID: "x", Title: "Y", Output: "Y", Acceptance: "Y"}}}} {
		if _, err := Seal(s); err == nil {
			t.Fatalf("accepted malformed: %+v", s)
		}
	}
}

func TestHistoricalGraphifySnapshotsRetainDigestWithoutCodeGraphConsent(t *testing.T) {
	for _, raw := range []string{
		`{"schema":1,"profile_id":"clawdline","version":"1","source":"Clawdline","digest":"a1a213ab46b98ab69027f2cc6f6528de344bbc7de78fa111d515ca767ec7b9f3","instructions":"","stages":[],"graphify_enabled":true}`,
		`{"schema":1,"profile_id":"clawdline","version":"1","source":"Clawdline","digest":"b03fc909717417b51fa2987048fed0c0346ec807869614356715c98e0079e4aa","instructions":"","stages":[],"graphify_enabled":false}`,
	} {
		var s Snapshot
		if err := json.Unmarshal([]byte(raw), &s); err != nil || s.Validate() != nil || s.CodeGraphEnabled {
			t.Fatalf("historical receipt changed: %+v %v", s, err)
		}
		inherited, err := Seal(s)
		if err != nil || inherited.Digest != s.Digest || inherited.CodeGraphEnabled {
			t.Fatal("inheritance changed historical choice", err)
		}
		encoded, _ := json.Marshal(inherited)
		if string(encoded) != raw {
			t.Fatal("historical canonical bytes changed")
		}
		s.CodeGraphEnabled = true
		if s.Validate() == nil {
			t.Fatal("old consent enabled replacement")
		}
	}
	var s Snapshot
	if json.Unmarshal([]byte(`{"schema":1,"codegraph_enabled":true}`), &s) == nil {
		t.Fatal("accepted ambiguous schema")
	}
}
