package orchestrator

import (
	"context"
	"log"
	"net/http"

	"github.com/sainteye/clawdline/internal/adapters/squadfiles"
	"github.com/sainteye/clawdline/internal/adapters/store"
)

// checkSquadDispatchActor ties an automatic assignment to the bound root
// terminal and provider conversation before opening a child. The target Project
// may differ from the actor's launch snapshot.
func (b *Broker) checkSquadDispatchActor(ctx context.Context, req DispatchRequest, record Record, rootTerminal string) error {
	if !b.SquadActorRequired || record.Root == nil || req.Respawn != nil || req.Schedule != nil || req.InternalGate {
		return nil
	}
	if record.Persona != "" {
		if b.SquadAutoDispatchEnabled == nil {
			return refuse(http.StatusServiceUnavailable, "squad_policy_unavailable", "Machine role dispatch setting is unavailable.")
		}
		enabled, err := b.SquadAutoDispatchEnabled(ctx)
		if err != nil {
			return err
		}
		if !enabled {
			return refuse(http.StatusConflict, "squad_auto_dispatch_disabled", "Automatic role selection is off on this machine.")
		}
	}
	actor, bound, err := b.Store.AuthenticateSquadActor(ctx, req.ActorCapability)
	if err != nil {
		return err
	}
	if !bound && req.ActorCapability == "" {
		// A Session opened before squad snapshots, or without a role, has no
		// capability. Preserve its existing dispatch route. A pending squad
		// launch cannot use this compatibility path.
		hasLaunch, err := b.Store.SquadHasLaunchForSession(ctx, rootTerminal, record.Root.SessionID)
		if err != nil {
			return err
		}
		if !hasLaunch {
			return nil
		}
	}
	if !bound || actor.TerminalID != rootTerminal || actor.ConversationID != record.Root.SessionID {
		return refuse(http.StatusForbidden, "session_actor_required", "This dispatch needs the current root Session's squad capability.")
	}
	if record.Persona != "" && (actor.DefinitionID == "clawdline.persona.product-manager" || actor.DefinitionID == "clawdline.persona.architect") {
		if b.SquadAutoAssignable == nil {
			return refuse(http.StatusServiceUnavailable, "squad_policy_unavailable", "Role assignment settings are unavailable.")
		}
		allowed, err := b.SquadAutoAssignable(ctx, record.Persona, record.ProjectDir)
		if err != nil {
			return err
		}
		if !allowed {
			return refuse(http.StatusConflict, "persona_disabled_for_auto_assignment", "This role is disabled for automatic assignment in the target Project.")
		}
	}
	return nil
}

type brokerSquadLaunch struct {
	launch store.SquadLaunch
	files  squadfiles.Files
}

// prepareSquadLaunch publishes a persona's launch files. claims are the
// task's declared writes, nil when there is no task (a handoff), and decide
// which role skills the prompt lists (squadfiles.PublishFor).
func (b *Broker) prepareSquadLaunch(ctx context.Context, personaID, projectPath string, claims []string) (brokerSquadLaunch, error) {
	if personaID == "" || b.ResolveSquadSnapshot == nil || b.Store == nil {
		return brokerSquadLaunch{}, nil
	}
	document, err := b.ResolveSquadSnapshot(ctx, personaID, projectPath)
	if err != nil {
		return brokerSquadLaunch{}, err
	}
	launch, err := b.Store.PrepareSquadLaunch(ctx, document)
	if err != nil {
		return brokerSquadLaunch{}, err
	}
	files, err := squadfiles.PublishFor(b.Dir, launch.ID, launch.SnapshotID, launch.ActorCapability, document,
		squadfiles.Task{Claims: claims})
	if err != nil {
		_ = b.Store.FailSquadLaunch(ctx, launch.ID)
		return brokerSquadLaunch{}, err
	}
	return brokerSquadLaunch{launch: launch, files: files}, nil
}

func (b *Broker) recordSquadTerminal(ctx context.Context, prepared brokerSquadLaunch, terminalID string) error {
	if prepared.launch.ID == "" {
		return nil
	}
	// The provider already opened. Both records are retried by the inventory
	// observer; an error here must not turn a running child into an unowned
	// second launch on retry.
	if err := squadfiles.RecordTerminal(prepared.files, terminalID); err != nil {
		log.Printf("squad launch %s: terminal receipt could not be written: %v", prepared.launch.ID, err)
	}
	if err := b.Store.RecordSquadTerminal(ctx, prepared.launch.ID, terminalID); err != nil {
		log.Printf("squad launch %s: terminal binding pending recovery: %v", prepared.launch.ID, err)
		return err
	}
	return nil
}
