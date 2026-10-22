package http

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/capacity"
)

func TestAntigravityConsentCapacityReading(t *testing.T) {
	s := capacityServer(t)
	measure := s.storeCapacityMeasures()[capacity.StoreAntigravityProjectConsents]
	if r := measure(); !r.Known || r.Used != 0 {
		t.Fatal(r)
	}
	cmd := store.AntigravityConsentCommand{ProjectRoot: filepath.Join(t.TempDir(), "project"), Action: store.AntigravityConsentDisable, Actor: "person", Key: "request", OperationID: "revoke"}
	if _, err := s.store.ApplyAntigravityProjectConsent(context.Background(), cmd); err != nil {
		t.Fatal(err)
	}
	if r := measure(); !r.Known || r.Used != 1 {
		t.Fatal("disabled tombstone not measured", r)
	}
	if err := s.store.Close(); err != nil {
		t.Fatal(err)
	}
	if r := measure(); r.Known || r.Err == "" {
		t.Fatal("unreadable store became diagnostic zero", r)
	}
	s.store = nil
	if r := measure(); r.Known || r.Err == "" {
		t.Fatal("missing store became diagnostic zero", r)
	}
}
