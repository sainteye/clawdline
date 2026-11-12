package store

import (
	"context"
	"errors"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/voicework"
)

func TestVoiceWorkSurvivesReopenAndRefusesPartialUpdates(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	state, err := s.UpdateVoiceWork(ctx, func(v *voicework.State) error {
		_, err := v.AddInput(voicework.Input{ID: "utterance", Text: "Inspect the work", At: 1})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	id := state.ID
	_, err = s.UpdateVoiceWork(ctx, func(v *voicework.State) error { v.Inputs = nil; return errors.New("injected failure") })
	if err == nil {
		t.Fatal("injection succeeded")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	state, err = s.VoiceWork(ctx)
	if err != nil || state.ID != id || len(state.Inputs) != 1 || state.Inputs[0].Text != "Inspect the work" {
		t.Fatalf("state %+v, %v", state, err)
	}
}
