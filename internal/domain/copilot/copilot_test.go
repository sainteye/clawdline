package copilot

import (
	"errors"
	"strings"
	"testing"
)

func ptr[T any](v T) *T { return &v }

// testKey is assembled so no line of this file reads as a real credential.
var testKey = "sk-" + "proj-abcdefghijklmnopWXYZ"

func TestUnsetChoicesTakeTheirDefaults(t *testing.T) {
	e := Settings{}.Effective()
	if e.Model != "gpt-realtime-mini" || e.Eagerness != "low" || e.Voice != "marin" || e.BriefingAssistant != "claude" || e.BriefingOff {
		t.Fatalf("defaults: %+v", e)
	}
	if (Settings{Model: "gpt-5"}).Effective().Model != "gpt-realtime-mini" {
		t.Fatal("an unknown stored model falls back to the default")
	}
}

func TestTheKeyIsWrittenAndOnlyItsEndIsEverSaid(t *testing.T) {
	s, err := Apply(Settings{}, Change{Key: ptr("  " + testKey + "  ")})
	if err != nil {
		t.Fatal(err)
	}
	if s.Key != testKey || s.KeyHint() != "WXYZ" {
		t.Fatalf("key %q hint %q", s.Key, s.KeyHint())
	}
	s, _ = Apply(s, Change{Key: ptr("")})
	if s.Key != "" || s.KeyHint() != "" {
		t.Fatal("an empty key removes the stored one")
	}
}

func TestAChangeOutsideWhatIsOfferedIsRefusedAndChangesNothing(t *testing.T) {
	base := Settings{Model: "gpt-realtime", Eagerness: "auto"}
	for name, c := range map[string]struct {
		change Change
		err    error
	}{
		"key without sk-":     {Change{Key: ptr("pk-123456789")}, ErrBadKey},
		"good key, bad model": {Change{Key: ptr(testKey), Model: ptr("gpt-4o")}, ErrBadModel},
		"key with a space":    {Change{Key: ptr("sk-12 3456789")}, ErrBadKey},
		"key too long":        {Change{Key: ptr("sk-" + strings.Repeat("a", MaxKeyBytes))}, ErrBadKey},
		"model":               {Change{Model: ptr("gpt-4o")}, ErrBadModel},
		"eagerness":           {Change{Eagerness: ptr("medium")}, ErrBadEagerness},
		"voice":               {Change{Voice: ptr("fable")}, ErrBadVoice},
		"instructions":        {Change{ExtraInstructions: ptr(strings.Repeat("字", MaxExtraInstructionsRunes+1))}, ErrBadInstructions},
		"briefing assistant":  {Change{BriefingAssistant: ptr("gemini")}, ErrBadAssistant},
	} {
		got, err := Apply(base, c.change)
		if !errors.Is(err, c.err) {
			t.Errorf("%s: err %v, want %v", name, err, c.err)
		}
		if got != base {
			t.Errorf("%s: a refused change altered the settings: %+v", name, got)
		}
	}
}

func TestInstructionsAtTheLimitAreKept(t *testing.T) {
	s, err := Apply(Settings{}, Change{ExtraInstructions: ptr(strings.Repeat("字", MaxExtraInstructionsRunes)), Briefing: ptr(false)})
	if err != nil || !s.BriefingOff {
		t.Fatalf("%v %+v", err, s.BriefingOff)
	}
}

func TestEveryOfferedVoiceIsKept(t *testing.T) {
	for _, v := range Voices {
		s, err := Apply(Settings{}, Change{Voice: ptr(v)})
		if err != nil || s.Effective().Voice != v {
			t.Errorf("%s: %v %q", v, err, s.Voice)
		}
	}
}
