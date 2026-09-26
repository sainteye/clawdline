package orchestrator

import (
	"context"
	"strings"

	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/adapters/whisper"
)

// DisplayLanguage follows the same answers as dictation: Clawdline's setting,
// the machine-language reader, then the catalog this daemon was given. It is
// read at notification time so changing language needs no daemon restart.
func (b *Broker) DisplayLanguage() string {
	values, err := nextconfig.Open(b.Dir).Read()
	setting := ""
	if err == nil {
		if language, ok := values.String("language"); ok {
			setting = language
		}
	}
	return displayLanguage(setting, (whisper.Machine{}).Languages(context.Background()), b.Language)
}

func displayLanguage(setting string, machine []whisper.Answer, catalog string) string {
	if whisper.Usable(setting) {
		return setting
	}
	for _, answer := range machine {
		if whisper.Usable(answer.Tag) {
			return answer.Tag
		}
	}
	if fallback := strings.TrimSpace(catalog); whisper.Usable(fallback) {
		return fallback
	}
	return "zh-Hant"
}

// ClaudeLanguage is the response language a session opened with assistant is
// launched with (projects.ClaudeLanguage): DisplayLanguage, for Claude, unless
// the person chose one in Claude Code's own settings. Read at every launch, so
// changing either restarts nothing.
func (b *Broker) ClaudeLanguage(assistant string) string {
	if assistant != projects.AssistantClaude || b.ClaudeSetsLanguage == nil || b.ClaudeSetsLanguage() {
		return ""
	}
	return projects.ClaudeLanguage(b.DisplayLanguage())
}
