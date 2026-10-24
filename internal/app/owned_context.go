package app

import (
	"context"
	"io"
	"os"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/adapters/squadfiles"
	"github.com/sainteye/clawdline/internal/domain/squad"
)

// OwnedContext reuses the same immutable Squad snapshot as terminal launches.
// The protocol owner sends its instructions with the first real user input.
type OwnedContext struct {
	Context  projects.AntigravityContext
	Files    squadfiles.Files
	LaunchID string
	Persona  string
}

func (s Starter) PrepareOwnedContext(ctx context.Context, place projects.Place, persona string) (OwnedContext, error) {
	prepared, err := s.prepareSquadLaunch(ctx, place.Path, persona, "")
	if err != nil {
		return OwnedContext{}, err
	}
	fail := func(err error) (OwnedContext, error) {
		if prepared.launch.ID != "" {
			_ = s.Squad.FailSquadLaunch(ctx, prepared.launch.ID)
		}
		return OwnedContext{}, err
	}
	if persona != "" && prepared.files.PromptPath == "" {
		return fail(projects.ErrSquadContextUnavailable)
	}
	prompt := ""
	if prepared.files.PromptPath != "" {
		f, err := os.Open(prepared.files.PromptPath)
		if err != nil {
			return fail(err)
		}
		b, err := io.ReadAll(io.LimitReader(f, squad.MaxSquadRequestBytes+1))
		f.Close()
		if err != nil || len(b) > squad.MaxSquadRequestBytes {
			return fail(projects.ErrSquadContextUnavailable)
		}
		prompt = string(b)
	}
	memory, language := "", ""
	if s.Memory != nil {
		memory = s.Memory(place.Path)
	}
	if s.Language != nil {
		language = s.Language(projects.AssistantAntigravity)
	}
	bound, err := projects.PrepareAntigravityContext(prepared.files.PromptPath, prompt, memory, language)
	if err != nil {
		return fail(err)
	}
	return OwnedContext{Context: bound, Files: prepared.files, LaunchID: prepared.launch.ID, Persona: persona}, nil
}
