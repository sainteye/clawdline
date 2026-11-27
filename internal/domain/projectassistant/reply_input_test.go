package projectassistant

import (
	"slices"
	"testing"
)

// Only an explicit queue makes a Bot queue; the absent value stays absent in
// storage and means steer.
func TestReplyInputWithoutAChoiceSteers(t *testing.T) {
	for _, tt := range []struct {
		stored ReplyInput
		want   ReplyInput
		valid  bool
	}{
		{stored: "", want: ReplyInputSteer, valid: true},
		{stored: ReplyInputSteer, want: ReplyInputSteer, valid: true},
		{stored: ReplyInputQueue, want: ReplyInputQueue, valid: true},
		{stored: "later", want: ReplyInputSteer},
	} {
		if got := tt.stored.Effective(); got != tt.want || tt.stored.Valid() != tt.valid {
			t.Errorf("%q: effective %q valid %v, want %q %v", tt.stored, got, tt.stored.Valid(), tt.want, tt.valid)
		}
	}
}

// A Bot saved before the setting existed keeps no stored value when some
// other setting is saved, and a person's explicit queue is stored.
func TestReplyInputEditKeepsTheThreeStates(t *testing.T) {
	original := rebaseBot()
	got, err := save(original, EditBot{ExpectedVersion: original.Version, Name: original.Name, Appearance: original.Appearance, Instructions: "mine", Base: baseOf(original)})
	if err != nil || got.ReplyInput != "" {
		t.Fatalf("an unrelated save wrote reply_input %q: %v", got.ReplyInput, err)
	}
	got, err = save(original, EditBot{ExpectedVersion: original.Version, Name: original.Name, Appearance: original.Appearance, Instructions: original.Instructions.Text, ReplyInput: ReplyInputQueue, Base: baseOf(original)})
	if err != nil || got.ReplyInput != ReplyInputQueue {
		t.Fatalf("explicit queue: %q, %v", got.ReplyInput, err)
	}
	// Another writer chose queue while this draft, read as steering, chose
	// steer: both changed the same field to different values.
	current := got
	current.Version++
	_, err = save(current, EditBot{ExpectedVersion: original.Version, Name: original.Name, Appearance: original.Appearance, Instructions: original.Instructions.Text, ReplyInput: ReplyInputSteer, Base: func() *EditBase {
		b := baseOf(original)
		b.ReplyInput = ReplyInputQueue
		return b
	}()})
	if err != nil {
		t.Fatalf("a draft that saw queue and chose steer: %v", err)
	}
	// A draft read from the unset Bot shows steering; switching it to queue
	// while another writer also chose queue agrees and is no conflict.
	_, err = save(current, EditBot{ExpectedVersion: original.Version, Name: original.Name, Appearance: original.Appearance, Instructions: original.Instructions.Text, ReplyInput: ReplyInputQueue, Base: baseOf(original)})
	if slices.Contains(SettingsConflicts(err), ConflictReplyInput) || err != nil {
		t.Fatalf("an agreeing queue was refused: %v", err)
	}
}

func TestSteerFallbackReasonsAreClosed(t *testing.T) {
	for _, f := range SteerFallbacks {
		if !f.Valid() {
			t.Errorf("%q is not valid", f)
		}
	}
	if !SteerFallback("").Valid() || SteerFallback("guessed").Valid() {
		t.Fatal("only absent and listed reasons are valid")
	}
}
