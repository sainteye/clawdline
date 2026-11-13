// Package workflowlaunch freezes optional methods without changing provider permissions.
package workflowlaunch

import (
	"context"
	"errors"
	"github.com/sainteye/clawdline/internal/adapters/codegraph"
	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/workflow"
	"strings"
)

type Service struct {
	Store   *store.Store
	Dir     string
	Resolve func(string, bool) (workflow.Snapshot, error)
}
type Prepared struct {
	ID, Text string
	store    *store.Store
}

// Prepare preserves workflow inheritance. Image-review consent is sampled for
// each new execution, including children, and never sampled for a resume.
func (s Service) Prepare(ctx context.Context, root, resume, parent string) (Prepared, error) {
	if s.Store == nil {
		return Prepared{}, errors.New("workflow_store_unavailable")
	}
	var snap workflow.Snapshot
	var err error
	var values nextconfig.Values
	if resume == "" {
		values, err = nextconfig.Open(s.Dir).Read()
		if err != nil {
			return Prepared{}, err
		}
	}
	inherited := resume
	if inherited == "" {
		inherited = parent
	}
	if inherited != "" {
		var found bool
		snap, found, err = s.Store.WorkflowSnapshotForConversation(ctx, inherited)
		if err != nil || !found {
			if err != nil || resume != "" {
				return Prepared{}, err
			}
		}
	} else {
		graph, _ := values.Bool("codegraph_enabled")
		profile, profileErr := nextconfig.WorkflowProfile(values)
		if profileErr != nil {
			return Prepared{}, profileErr
		}
		if s.Resolve == nil {
			return Prepared{}, errors.New("workflow_content_unavailable")
		}
		snap, err = s.Resolve(profile, graph)
		if err != nil {
			return Prepared{}, err
		}
	}
	if resume == "" {
		on, _ := values.Bool("space_saving_images")
		browser, _ := values.Bool("browser_enabled")
		if _, configured := values.Raw["browser_policy"]; configured {
			// Ordered policy also teaches native-first and explicit Session choices.
			// It grants no browser permission by itself.
			if _, err := values.BrowserPolicy(); err != nil {
				return Prepared{}, err
			}
			browser = true
		}
		if snap.ProfileID == "" {
			// Legacy parents still cannot acquire today's workflow choice.
			if !on && !browser {
				return Prepared{}, nil
			}
			snap, err = workflow.Seal(workflow.Snapshot{ProfileID: "clawdline", Version: "1", Source: "Clawdline"})
			if err != nil {
				return Prepared{}, err
			}
		}
		if snap.SpaceSavingImages != on || snap.BrowserUse != browser {
			snap.SpaceSavingImages = on
			snap.BrowserUse = browser
			// Historical schema 1 cannot encode new opt-ins.
			snap.Schema = 2
			snap, err = workflow.Seal(snap)
			if err != nil {
				return Prepared{}, err
			}
		}
	}
	text := ""
	if resume == "" {
		var parts []string
		if snap.Instructions != "" {
			parts = append(parts, snap.Instructions)
		}
		if snap.CodeGraphEnabled {
			graph, err := codegraph.LaunchInstructions(ctx, root)
			if err != nil {
				return Prepared{}, err
			}
			parts = append(parts, graph)
		}
		if snap.SpaceSavingImages {
			parts = append(parts, workflow.SpaceSavingImageInstructions())
		}
		if snap.BrowserUse {
			parts = append(parts, workflow.BrowserUseInstructions())
		}
		text = strings.Join(parts, "\n\n")
	}
	launch, err := s.Store.PrepareWorkflowLaunch(ctx, snap)
	if err != nil {
		return Prepared{}, err
	}
	return Prepared{ID: launch.ID, Text: text, store: s.Store}, nil
}

// Wrap keeps the provider's argv, environment and prompt intact. The waiting
// parent carries only a launch ID so process observation can recover after a crash.
func (p Prepared) Wrap(command string) string {
	if p.ID == "" {
		return command
	}
	return "sh -c " + projects.ShellQuoted(command+"; clawdline_workflow_status=$?; exit \"$clawdline_workflow_status\"") + " " + projects.ShellQuoted("clawdline-workflow:"+p.ID)
}
func (p Prepared) Record(ctx context.Context, terminal string) error {
	if p.ID == "" {
		return nil
	}
	return p.store.RecordWorkflowTerminal(ctx, p.ID, terminal)
}

// Bind records a daemon-owned provider's exact native conversation before its first input.
// The same persisted launch ID is retained in its private context for recovery.
func (p Prepared) Bind(ctx context.Context, terminal, conversation string) error {
	if p.ID == "" {
		return nil
	}
	if err := p.Record(ctx, terminal); err != nil {
		return err
	}
	return p.store.BindWorkflowConversation(ctx, p.ID, terminal, conversation)
}
func (p Prepared) Fail(ctx context.Context) {
	if p.ID != "" {
		_ = p.store.FailWorkflowLaunch(ctx, p.ID)
	}
}
