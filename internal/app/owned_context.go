package app

import (
	"context"
	"io"
	"os"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/adapters/squadfiles"
	"github.com/sainteye/clawdline/internal/adapters/workflowlaunch"
	"github.com/sainteye/clawdline/internal/domain/squad"
)

// OwnedContext reuses the same immutable Squad snapshot as terminal launches.
// The protocol owner sends its instructions with the first real user input.
type OwnedContext struct {
	Context  projects.AntigravityContext
	Files    squadfiles.Files
	LaunchID string
	Persona  string
	Workflow workflowlaunch.Prepared
}

func (s Starter) PrepareOwnedContext(ctx context.Context, place projects.Place, persona string) (OwnedContext, error) {
	var workflow workflowlaunch.Prepared
	if s.PrepareWorkflow != nil {
		var err error
		workflow, err = s.PrepareWorkflow(ctx, place.Path, "", "")
		if err != nil {
			return OwnedContext{}, err
		}
	}
	prepared, err := s.prepareSquadLaunch(ctx, place.Path, persona, "")
	if err != nil {
		workflow.Fail(ctx)
		return OwnedContext{}, err
	}
	fail := func(err error) (OwnedContext, error) {
		workflow.Fail(ctx)
		if prepared.launch.ID != "" {
			_ = s.Squad.FailSquadLaunch(ctx, prepared.launch.ID)
		}
		return OwnedContext{}, err
	}
	owned, err := s.OwnedContextFromPublished(ctx, place, persona, prepared.launch.ID, prepared.files)
	if err != nil {
		return fail(err)
	}
	owned, err = owned.WithWorkflow(workflow)
	if err != nil {
		return fail(err)
	}
	return owned, nil
}

func (o OwnedContext) WithWorkflow(prepared workflowlaunch.Prepared) (OwnedContext, error) {
	o.Workflow = prepared
	o.Context.WorkflowInstructions = prepared.Text
	if _, _, err := o.Context.Document(); err != nil {
		return OwnedContext{}, err
	}
	return o, nil
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
	var explainSteps *bool
	if s.GeminiStepExplanations != nil {
		on := s.GeminiStepExplanations()
		explainSteps = &on
	}
	bound, err := projects.PrepareAntigravityContextWithExplanations(files.PromptPath, prompt, memory, language, explainSteps)
	if err != nil {
		return OwnedContext{}, err
	}
	return OwnedContext{Context: bound, Files: files, LaunchID: launchID, Persona: persona}, nil
}
