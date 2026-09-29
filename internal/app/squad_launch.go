package app

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/adapters/squadfiles"
	"github.com/sainteye/clawdline/internal/adapters/store"
)

type preparedSquadLaunch struct {
	launch store.SquadLaunch
	files  squadfiles.Files
}

func (s Starter) prepareSquadLaunch(ctx context.Context, projectPath, personaID, resume string) (preparedSquadLaunch, error) {
	if s.Squad == nil || s.SquadDir == "" || s.ResolveSquadSnapshot == nil {
		return preparedSquadLaunch{}, nil
	}
	var document json.RawMessage
	var launch store.SquadLaunch
	var err error
	if resume != "" {
		var found bool
		document, _, found, err = s.Squad.SquadSnapshotForConversation(ctx, resume)
		if err != nil {
			return preparedSquadLaunch{}, err
		}
		if !found {
			// Pre-feature conversations retain their explicit legacy mode. They
			// cannot acquire a snapshot from settings that did not exist when
			// they first opened.
			return preparedSquadLaunch{}, nil
		}
		var identity struct {
			DefinitionID string `json:"definition_id"`
		}
		if err := json.Unmarshal(document, &identity); err != nil || identity.DefinitionID == "" {
			return preparedSquadLaunch{}, errors.New("invalid_squad_snapshot_identity")
		}
		if personaID != "" && personaID != identity.DefinitionID &&
			"clawdline.persona."+personaID != identity.DefinitionID {
			return preparedSquadLaunch{}, StartRefusal{Status: http.StatusConflict,
				Code: "snapshot_persona_conflict", Message: "A resumed conversation keeps its original role snapshot."}
		}
		launch, err = s.Squad.PrepareSquadResume(ctx, resume)
	} else {
		if personaID == "" {
			return preparedSquadLaunch{}, nil
		}
		document, err = s.ResolveSquadSnapshot(ctx, personaID, projectPath)
		if err == nil {
			launch, err = s.Squad.PrepareSquadLaunch(ctx, document)
		}
	}
	if err != nil {
		if errors.Is(err, store.ErrSquadLaunchCapacity) {
			return preparedSquadLaunch{}, StartRefusal{Status: http.StatusTooManyRequests,
				Code: "squad_launch_capacity", Message: "Too many squad launches are waiting for Session identity; check this machine's diagnostics."}
		}
		if errors.Is(err, store.ErrSquadSnapshotTooLarge) {
			return preparedSquadLaunch{}, StartRefusal{Status: http.StatusRequestEntityTooLarge,
				Code: "squad_snapshot_too_large", Message: "The role, handbook, and skill snapshot exceeds this machine's limit."}
		}
		return preparedSquadLaunch{}, err
	}
	files, err := squadfiles.Publish(s.SquadDir, launch.ID, launch.SnapshotID, launch.ActorCapability, document)
	if err != nil {
		_ = s.Squad.FailSquadLaunch(ctx, launch.ID)
		return preparedSquadLaunch{}, err
	}
	return preparedSquadLaunch{launch: launch, files: files}, nil
}

func (s Starter) openSquadTerminal(ctx context.Context, place projects.Place, model string,
	plan projects.PlanKind, prepared preparedSquadLaunch, providerCommand string) (Started, error) {
	command := "env " + prepared.files.PrivateEnv() + " " + providerCommand
	var result Started
	var err error
	switch plan {
	case projects.PlanITerm:
		result.ID, err = s.Launcher.NewITermTab(ctx, "cd "+projects.ShellQuoted(place.Path)+" && "+command)
		result.Backend = "iterm"
	case projects.PlanTmux:
		result.ID, err = s.Launcher.NewTmuxWindow(ctx, place.Path, command)
		result.Backend = "tmux"
	case projects.PlanTmuxDetached:
		result.ID, err = s.Launcher.NewTmuxSession(ctx, place.Path, projects.TmuxStartedSessionName, command)
		result.Backend = "tmux"
		result.Attach = projects.TmuxAttachCommand()
	default:
		return Started{}, errors.New("invalid_squad_terminal_plan")
	}
	if err != nil {
		_ = s.Squad.FailSquadLaunch(ctx, prepared.launch.ID)
		appName := ""
		if plan == projects.PlanITerm {
			appName = "iTerm2"
		}
		return Started{}, openRefusal(err, appName)
	}
	result.Model = model
	if err := squadfiles.RecordTerminal(prepared.files, result.ID); err != nil {
		log.Printf("squad launch %s: terminal receipt could not be written: %v", prepared.launch.ID, err)
	}
	if err := s.Squad.RecordSquadTerminal(ctx, prepared.launch.ID, result.ID); err != nil {
		// The provider is already running with its immutable prompt. The
		// inventory observer will retry the binding from the terminal receipt;
		// returning a refusal here would invite a second terminal on retry.
		log.Printf("squad launch %s: terminal binding pending recovery: %v", prepared.launch.ID, err)
	}
	return result, nil
}
