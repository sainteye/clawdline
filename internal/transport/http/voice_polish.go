package http

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline/internal/adapters/planner"
	"github.com/sainteye/clawdline/internal/adapters/whisper"
	"github.com/sainteye/clawdline/internal/contract"
)

// Dictation cleanup (docs/voice.md): after Whisper, and only when the person
// turned it on, one tool-less turn with a small model punctuates the
// transcript and corrects certain mis-hearings.
//
// **This is the one place a transcript leaves the machine**, and only to the
// provider of the assistant the person chose in Settings. It is off unless
// set. Whatever happens to the turn, the transcript is still answered: a
// cleanup that is slow, refused, unavailable or that changed the words is
// discarded and the Whisper text is used as it came, with the reason in
// `polish.status` so the composer can say so.

// voiceProjectTermLimit is how many project folder names join the vocabulary:
// the same forty places the project picker lists.
const voiceProjectTermLimit = 40

// voicePolisher runs one cleanup turn. A variable so a test can answer without
// an assistant installed.
var voicePolisher = func(ctx context.Context, text string, terms []string, level, script, assistant string) (string, error) {
	return planner.New().Polish(ctx, text, terms, level, script, assistant)
}

// voicePolishAvailable is which assistants could clean up a transcript here.
var voicePolishAvailable = func() []string {
	p := planner.New()
	out := []string{}
	for _, assistant := range []string{"claude", "codex"} {
		if p.Installed(assistant) {
			out = append(out, assistant)
		}
	}
	return out
}

// voiceOptions is the voice tab's settings as the transcription path reads
// them. A file that cannot be read means every key has its default.
type voiceOptions struct {
	polish    bool
	assistant string
	level     string
	custom    []string
	projects  bool
}

func readVoiceOptions(values nextconfig.Values, ok bool) voiceOptions {
	out := voiceOptions{assistant: "claude", level: planner.PolishTidy}
	if !ok {
		return out
	}
	out.polish, _ = values.Bool("voice_polish")
	if v, found := values.String("voice_polish_assistant"); found && v == "codex" {
		out.assistant = "codex"
	}
	if v, found := values.String("voice_polish_level"); found && v == planner.PolishPunctuation {
		out.level = planner.PolishPunctuation
	}
	if raw, found := values.Raw["voice_vocabulary"]; found {
		var words []string
		// A hand-edited list with a number in it is not a reason to refuse to
		// transcribe: the key is left at its default.
		if json.Unmarshal(raw, &words) == nil {
			out.custom = cleanTerms(words, nextconfig.MaxVoiceVocabularyTerms)
		}
	}
	out.projects, _ = values.Bool("voice_vocabulary_projects")
	return out
}

// voiceProjectTerms is this machine's project folder names, the ones the
// project picker lists. Only read when the person ticked the box, and for the
// voice tab's preview.
func (s *Server) voiceProjectTerms() []string { return voiceProjectNames(s) }

// voiceProjectNames is a variable so a test server, which has no icon store
// for the project readers, can name its projects.
var voiceProjectNames = func(s *Server) []string {
	readers := s.projectReaders()
	if readers == nil || readers.places == nil {
		return nil
	}
	var names []string
	for _, place := range readers.places.List(nil, voiceProjectTermLimit) {
		if name := filepath.Base(place.Path); spokenName(name) {
			names = append(names, name)
		}
	}
	return cleanTerms(names, voiceProjectTermLimit)
}

// spokenName is a folder name somebody could say: not a generated identifier
// such as a UUID or a long hexadecimal digest, which no transcript will contain.
func spokenName(name string) bool {
	letters, hex := 0, true
	for _, r := range name {
		switch {
		case (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F') || r == '-':
		default:
			hex = false
		}
		if unicode.IsLetter(r) {
			letters++
		}
	}
	return letters > 0 && !(hex && len(name) >= 12)
}

// cleanTerms keeps one copy of each usable term, in order, up to limit.
func cleanTerms(words []string, limit int) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, w := range words {
		w = strings.TrimSpace(w)
		key := strings.ToLower(w)
		if w == "" || seen[key] || !nextconfig.ValidListEntry(w, nextconfig.MaxVoiceVocabularyRunes) {
			continue
		}
		seen[key] = true
		out = append(out, w)
		if len(out) == limit {
			break
		}
	}
	return out
}

// vocabulary is every term a transcript is repaired against: the built-in
// names, the person's own, and the projects when ticked.
func (s *Server) vocabulary(o voiceOptions) []string {
	terms := append([]string{}, o.custom...)
	if o.projects {
		terms = append(terms, s.voiceProjectTerms()...)
	}
	return cleanTerms(terms, nextconfig.MaxVoiceVocabularyTerms+voiceProjectTermLimit)
}

// polishTranscript is the cleanup step. It never fails the transcription: the
// answer is either the cleaned text with the original beside it, or the
// original with the reason it was kept.
func polishTranscript(ctx context.Context, o voiceOptions, text, script string, terms []string) (string, string, *contract.VoicePolish) {
	if !o.polish || strings.TrimSpace(text) == "" {
		return text, "", nil
	}
	started := time.Now()
	all := append(append([]string{}, whisper.AlwaysExpected...), terms...)
	cleaned, err := voicePolisher(ctx, text, all, o.level, script, o.assistant)
	result := &contract.VoicePolish{Assistant: o.assistant, Ms: time.Since(started).Milliseconds()}
	switch {
	case err == nil && cleaned == text:
		result.Status = "unchanged"
		return text, "", result
	case err == nil:
		result.Status = "applied"
		return cleaned, text, result
	case errors.Is(err, planner.ErrNoPlanner):
		result.Status = "unavailable"
	case errors.Is(err, planner.ErrPolishUnfaithful):
		result.Status = "unfaithful"
	case errors.Is(err, planner.ErrPolishTooLong):
		result.Status = "too_long"
	case errors.Is(err, planner.ErrOutOfQuota):
		result.Status = "quota"
	case errors.Is(err, planner.ErrPolishTimeout) || ctx.Err() != nil:
		result.Status = "timeout"
	default:
		result.Status = "failed"
	}
	return text, "", result
}

// polishRunes is what the audit line records about a cleanup: its size, never
// its words.
func polishRunes(s string) int { return utf8.RuneCountInString(s) }
