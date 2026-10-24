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
	owned, err := s.OwnedContextFromPublished(ctx, place, persona, prepared.launch.ID, prepared.files)
	if err != nil {
		return fail(err)
	}
	return owned, nil
}

// OwnedContextFromPublished reads the immutable publication already prepared by
// the broker, preserving its task claims and actor instead of publishing twice.
func (s Starter) OwnedContextFromPublished(ctx context.Context, place projects.Place, persona, launchID string, files squadfiles.Files) (OwnedContext, error) {
	if err := ctx.Err(); err != nil {
		return OwnedContext{}, err
	}
	if persona != "" && files.PromptPath == "" {
		return OwnedContext{}, projects.ErrSquadContextUnavailable
	}
	prompt := ""
	if files.PromptPath != "" {
		f, err := os.Open(files.PromptPath)
		if err != nil {
			return OwnedContext{}, err
		}
		b, err := io.ReadAll(io.LimitReader(f, squad.MaxSquadRequestBytes+1))
		f.Close()
		if err != nil || len(b) > squad.MaxSquadRequestBytes {
			return OwnedContext{}, projects.ErrSquadContextUnavailable
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
	bound, err := projects.PrepareAntigravityContext(files.PromptPath, prompt, memory, language)
	if err != nil {
		return OwnedContext{}, err
	}
	return OwnedContext{Context: bound, Files: files, LaunchID: launchID, Persona: persona}, nil
}
