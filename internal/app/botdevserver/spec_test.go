package botdevserver

import (
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/capacity"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func specFixture() Spec {
	return Spec{Key: pa.BotKey{MachineID: "synthetic-machine", BotID: "synthetic-bot"},
		Project: pa.ProjectScope{MachineID: "synthetic-machine", ProjectID: "synthetic-project", CanonicalID: "synthetic-repository", Kind: pa.RepositoryProject},
		Name:    "Synthetic server", Command: "synthetic-server --foreground", Directory: "/synthetic/project"}
}

func TestServerFingerprintBindsScopeCommandAndDirectoryButNotLabel(t *testing.T) {
	s := specFixture()
	original, err := s.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	s.Name = "Another label"
	if got, _ := s.Fingerprint(); got != original {
		t.Fatal("a label created different command authority")
	}
	for _, mutate := range []func(*Spec){
		func(s *Spec) { s.Command += " --other" }, func(s *Spec) { s.Directory += "/other" },
		func(s *Spec) { s.Key.BotID += "-other" }, func(s *Spec) { s.Project.CanonicalID += "-other" },
		func(s *Spec) { s.Project.Kind = pa.PlaceProject },
	} {
		other := specFixture()
		mutate(&other)
		if got, err := other.Fingerprint(); err != nil || got == original {
			t.Fatalf("different command authority shared a fingerprint: %s %v", got, err)
		}
	}
}

func TestServerSpecRefusesIncompleteAndOversizedReviewData(t *testing.T) {
	for _, mutate := range []func(*Spec){
		func(s *Spec) { s.Key.MachineID = "other" }, func(s *Spec) { s.Key.BotID = "" },
		func(s *Spec) { s.Project.CanonicalID = "" }, func(s *Spec) { s.Command = " " },
		func(s *Spec) { s.Name = "bad\x00label" }, func(s *Spec) { s.Command = "bad\xff" },
		func(s *Spec) { s.Directory = "relative" }, func(s *Spec) { s.Directory = "/synthetic/../project" },
	} {
		s := specFixture()
		mutate(&s)
		if _, err := s.Fingerprint(); err == nil {
			t.Fatalf("invalid command review admitted: %+v", s)
		}
	}
	s := specFixture()
	s.Command = strings.Repeat("x", capacity.MaximumBotServerSpecBytes)
	if pa.Code(s.Validate()) != pa.CapacityFull {
		t.Fatal("the complete serialized review exceeded its bound")
	}
}
