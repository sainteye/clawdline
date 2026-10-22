package app

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"runtime"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/adapters/squadfiles"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app/tmuxview"
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

// startAntigravity prepares immutable context before any process is opened.
// The root supplies executable validation, hook installation and launch
// commitment; this app layer never reads vendor settings or account files.
func (s Starter) startAntigravity(ctx context.Context, place projects.Place, request projects.LaunchRequest) (Started, error) {
	if !placeIsDirectory(place.Path) {
		return Started{}, notFound("No place named that")
	}
	if s.PrepareAntigravity == nil {
		return Started{}, StartRefusal{Status: http.StatusServiceUnavailable, Code: "squad_context_unavailable", Message: "Antigravity launch context is not connected on this machine."}
	}
	itermOpen, _ := s.Launcher.ITermRunning(ctx)
	reach := projects.TmuxReach(s.Launcher.TmuxReach(ctx))
	choice := s.Terminal()
	plan := projects.ChoosePlan(choice, itermOpen, reach)
	if plan == projects.PlanNotRunning {
		return Started{}, unavailableTerminal(choice, runtime.GOOS)
	}
	if plan == projects.PlanNoTmux {
		return Started{}, StartRefusal{Status: http.StatusConflict, Code: "terminal_unsupported", Message: "tmux is not installed on this machine."}
	}
	prepared, err := s.prepareSquadLaunch(ctx, place.Path, request.Persona, request.Resume)
	if err != nil {
		return Started{}, err
	}
	request.SquadPromptPath = prepared.files.PromptPath
	if request.Persona != "" && request.SquadPromptPath == "" {
		return Started{}, projects.ErrSquadContextUnavailable
	}
	admitted, env, err := s.PrepareAntigravity(ctx, request)
	if err == nil && (admitted.Assistant != request.Assistant || admitted.ProjectRoot != request.ProjectRoot || admitted.Resume != request.Resume || admitted.SquadPromptPath != request.SquadPromptPath) {
		err = projects.ErrSquadContextUnavailable
	}
	var launch projects.Launch
	if err == nil {
		launch, err = projects.Admit(admitted)
	}
	if err != nil {
		if prepared.launch.ID != "" {
			_ = s.Squad.FailSquadLaunch(ctx, prepared.launch.ID)
		}
		return Started{}, err
	}
	if prepared.launch.ID != "" {
		env = append(env, prepared.files.PrivateEnv())
	}
	command := launch.ShellCommandWithEnv(env)
	if prepared.launch.ID != "" {
		return s.openSquadTerminal(ctx, place, request.Model, plan, prepared, command)
	}
	var result Started
	switch plan {
	case projects.PlanITerm:
		result.ID, err = s.Launcher.NewITermTab(ctx, "cd "+projects.ShellQuoted(place.Path)+" && "+command)
		result.Backend = "iterm"
	case projects.PlanITermTmux:
		return s.openViewed(ctx, place, request.Model, command)
	case projects.PlanTmux:
		result.ID, err = s.Launcher.NewTmuxWindow(ctx, place.Path, command)
		result.Backend = "tmux"
	case projects.PlanTmuxDetached:
		result.ID, err = s.Launcher.NewTmuxSession(ctx, place.Path, projects.TmuxStartedSessionName, command)
		result.Backend = "tmux"
		result.Attach = projects.TmuxAttachCommand()
	}
	if err != nil {
		return Started{}, openRefusal(err, "")
	}
	result.Model = request.Model
	return result, nil
}

func (s Starter) openSquadTerminal(ctx context.Context, place projects.Place, model string,
	plan projects.PlanKind, prepared preparedSquadLaunch, providerCommand string) (Started, error) {
	command := providerCommand
	var result Started
	var err error
	switch plan {
	case projects.PlanITerm:
		result.ID, err = s.Launcher.NewITermTab(ctx, "cd "+projects.ShellQuoted(place.Path)+" && "+command)
		result.Backend = "iterm"
	case projects.PlanITermTmux:
		var opened tmuxview.Opened
		opened, err = tmuxview.Open(ctx, s.Launcher, place.Path, tmuxview.SessionName("session"), command)
		result.ID, result.Backend, result.Attach = opened.PaneID, tmuxview.Backend, opened.Attach
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
