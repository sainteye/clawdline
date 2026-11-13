// Package copilot holds the voice copilot's settings as this machine keeps
// them: the OpenAI key that never leaves the machine, the Realtime model, how
// patiently the copilot waits for the person to finish, and the person's own
// extra instructions.
//
// The key is written, never read back: every answer carries only whether one
// is set and its last four characters. A phone starts a call with a short-lived
// client secret the machine mints from it (internal/transport/http/copilot.go).
package copilot

import (
	"errors"
	"github.com/sainteye/clawdline/internal/domain/voicework"
	"strings"
	"unicode/utf8"
)

// The limits a settings change is held to. Each is registered in
// internal/domain/capacity (register_console.go).
const (
	// MaxKeyBytes bounds a pasted OpenAI key; real keys are well under it.
	MaxKeyBytes = 400
	// MaxExtraInstructionsRunes bounds the person's own instructions, which
	// ride in every response's input.
	MaxExtraInstructionsRunes = 2000
	// ClientSecretSeconds is how long a minted client secret can open a call.
	ClientSecretSeconds = 600
)

// Models a call may use, cheapest first; the first is the default.
var Models = []string{"gpt-realtime-mini", "gpt-realtime"}

// Eagerness is semantic_vad's patience; "low" waits longest and is the default.
var Eagerness = []string{"low", "auto", "high"}

// Voices are the Realtime output voices; the first is the default. OpenAI
// recommends marin and cedar for quality, and a call cannot change its voice
// once it has spoken, so the choice applies from the next call.
var Voices = []string{"marin", "cedar", "alloy", "ash", "ballad", "coral", "echo", "sage", "shimmer", "verse"}

// Speeds are Realtime's playback rates offered, slowest first; "0.9", a
// little slower than OpenAI's 1.0, is the default, because a report read out
// at full speed was too fast to follow (2026-11-12).
var Speeds = []string{"0.8", "0.9", "1.0", "1.1"}

// BriefingAssistants are the CLIs the machine can run to prepare a briefing.
var BriefingAssistants = []string{"claude", "codex"}

// Settings is the stored file. Key is never sent in any answer.
type Settings struct {
	WorkAutoContinue  int    `json:"work_auto_continue,omitempty"`
	Key               string `json:"openai_key,omitempty"`
	Model             string `json:"model,omitempty"`
	Eagerness         string `json:"eagerness,omitempty"`
	Voice             string `json:"voice,omitempty"`
	Speed             string `json:"speed,omitempty"`
	ExtraInstructions string `json:"extra_instructions,omitempty"`
	BriefingAssistant string `json:"briefing_assistant,omitempty"`
	WorkEnabled       bool   `json:"work_enabled,omitempty"`
	WorkAssistant     string `json:"work_assistant,omitempty"`
	// BriefingOff turns the machine's prepared briefing off; on unless set.
	BriefingOff bool `json:"briefing_off,omitempty"`
	// LongAnswers lifts the three-sentence limit on ordinary answers; off unless set.
	LongAnswers bool `json:"long_answers,omitempty"`
}

// Effective fills every unset choice with its default.
func (s Settings) Effective() Settings {
	if s.WorkAutoContinue < 1 || s.WorkAutoContinue > voicework.MaxAutoContinue {
		s.WorkAutoContinue = voicework.DefaultAutoContinue
	}
	if !contains(Models, s.Model) {
		s.Model = Models[0]
	}
	if !contains(Eagerness, s.Eagerness) {
		s.Eagerness = Eagerness[0]
	}
	if !contains(Voices, s.Voice) {
		s.Voice = Voices[0]
	}
	if !contains(Speeds, s.Speed) {
		s.Speed = Speeds[1]
	}
	if !contains(BriefingAssistants, s.BriefingAssistant) {
		s.BriefingAssistant = BriefingAssistants[0]
	}
	if !contains(BriefingAssistants, s.WorkAssistant) {
		s.WorkAssistant = s.BriefingAssistant
	}
	return s
}

// KeyHint is what an answer may say about the key: its last four characters.
func (s Settings) KeyHint() string {
	k := strings.TrimSpace(s.Key)
	if len(k) < 8 {
		return ""
	}
	return k[len(k)-4:]
}

// Change is one settings write; a nil field is left as it is.
type Change struct {
	WorkAutoContinue  *int    `json:"work_auto_continue"`
	Key               *string `json:"openai_key"`
	Model             *string `json:"model"`
	Eagerness         *string `json:"eagerness"`
	Voice             *string `json:"voice"`
	Speed             *string `json:"speed"`
	ExtraInstructions *string `json:"extra_instructions"`
	BriefingAssistant *string `json:"briefing_assistant"`
	WorkEnabled       *bool   `json:"work_enabled"`
	WorkAssistant     *string `json:"work_assistant"`
	Briefing          *bool   `json:"briefing"`
	Brief             *bool   `json:"brief"`
}

// Errors a change is refused with; each is the refusal's code.
var (
	ErrBadContinuation = errors.New("copilot_bad_auto_continue")
	ErrBadKey          = errors.New("copilot_bad_key")
	ErrBadModel        = errors.New("copilot_bad_model")
	ErrBadEagerness    = errors.New("copilot_bad_eagerness")
	ErrBadVoice        = errors.New("copilot_bad_voice")
	ErrBadSpeed        = errors.New("copilot_bad_speed")
	ErrBadInstructions = errors.New("copilot_bad_instructions")
	ErrBadAssistant    = errors.New("copilot_bad_assistant")
)

// Apply returns the settings with the change made, or why it is refused.
// An empty key removes the stored one.
func Apply(original Settings, c Change) (Settings, error) {
	s, err := apply(original, c)
	if err != nil {
		return original, err
	}
	return s, nil
}

func apply(s Settings, c Change) (Settings, error) {
	if c.WorkAutoContinue != nil {
		if *c.WorkAutoContinue < 1 || *c.WorkAutoContinue > voicework.MaxAutoContinue {
			return s, ErrBadContinuation
		}
		s.WorkAutoContinue = *c.WorkAutoContinue
	}
	if c.Key != nil {
		k := strings.TrimSpace(*c.Key)
		if k != "" && (len(k) > MaxKeyBytes || !strings.HasPrefix(k, "sk-") || strings.ContainsAny(k, " \t\r\n\x00")) {
			return s, ErrBadKey
		}
		s.Key = k
	}
	if c.Model != nil {
		if !contains(Models, *c.Model) {
			return s, ErrBadModel
		}
		s.Model = *c.Model
	}
	if c.Eagerness != nil {
		if !contains(Eagerness, *c.Eagerness) {
			return s, ErrBadEagerness
		}
		s.Eagerness = *c.Eagerness
	}
	if c.Voice != nil {
		if !contains(Voices, *c.Voice) {
			return s, ErrBadVoice
		}
		s.Voice = *c.Voice
	}
	if c.Speed != nil {
		if !contains(Speeds, *c.Speed) {
			return s, ErrBadSpeed
		}
		s.Speed = *c.Speed
	}
	if c.ExtraInstructions != nil {
		v := strings.TrimSpace(*c.ExtraInstructions)
		if utf8.RuneCountInString(v) > MaxExtraInstructionsRunes || strings.ContainsRune(v, 0) || !utf8.ValidString(v) {
			return s, ErrBadInstructions
		}
		s.ExtraInstructions = v
	}
	if c.BriefingAssistant != nil {
		if !contains(BriefingAssistants, *c.BriefingAssistant) {
			return s, ErrBadAssistant
		}
		s.BriefingAssistant = *c.BriefingAssistant
	}
	if c.Briefing != nil {
		s.BriefingOff = !*c.Briefing
	}
	if c.WorkEnabled != nil {
		s.WorkEnabled = *c.WorkEnabled
	}
	if c.WorkAssistant != nil {
		if !contains(BriefingAssistants, *c.WorkAssistant) {
			return s, ErrBadAssistant
		}
		s.WorkAssistant = *c.WorkAssistant
	}
	if c.Brief != nil {
		s.LongAnswers = !*c.Brief
	}
	return s, nil
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
