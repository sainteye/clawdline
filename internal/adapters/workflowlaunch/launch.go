// Package workflowlaunch freezes optional methods without changing provider permissions.
package workflowlaunch

import (
	"context"
	"errors"
	"github.com/sainteye/clawdline/internal/adapters/graphify"
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

// Prepare never consults current settings for a resume or an owned child.
// A legacy parent has no provable profile and cannot acquire today's choice.
func (s Service) Prepare(ctx context.Context, root, resume, parent string) (Prepared, error) {
	if s.Store == nil {
		return Prepared{}, errors.New("workflow_store_unavailable")
	}
	var snap workflow.Snapshot
	var err error
	inherited := resume
	if inherited == "" {
		inherited = parent
	}
	if inherited != "" {
		var found bool
		snap, found, err = s.Store.WorkflowSnapshotForConversation(ctx, inherited)
		if err != nil || !found {
			return Prepared{}, err
		}
	} else {
		values, readErr := nextconfig.Open(s.Dir).Read()
		if readErr != nil {
			return Prepared{}, readErr
		}
		graph, _ := values.Bool("graphify_enabled")
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
	text := ""
	if resume == "" {
		var parts []string
		if snap.Instructions != "" {
			parts = append(parts, snap.Instructions)
		}
		if snap.GraphifyEnabled {
			graph, err := graphify.LaunchInstructions(ctx, root)
			if err != nil {
				return Prepared{}, err
			}
			parts = append(parts, graph)
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
func (p Prepared) Fail(ctx context.Context) {
	if p.ID != "" {
		_ = p.store.FailWorkflowLaunch(ctx, p.ID)
	}
}
