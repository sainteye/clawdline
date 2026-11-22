package projectassistantstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

var _ app.ManagedRepository = (*Store)(nil)

// The target's settings, the manager's grant and its active execution share
// one transaction. A concurrent revocation either precedes this transaction
// and refuses it, or follows the already committed, explicitly approved plan.
func (s *Store) CommitManaged(ctx context.Context, execution app.ManagerExecution, m app.Mutation, maxCreated int) (pa.Receipt, error) {
	unlock := s.lock(m.Command.Key)
	defer unlock()
	if m.Bot == nil || maxCreated < 1 || m.Command.Key.MachineID != execution.Fence.Key.MachineID {
		return pa.Receipt{}, failure(pa.Invalid, "managed_mutation")
	}
	before, err := load(ctx, s.db, m.Command.Key)
	if m.ExpectedVersion > 0 && err != nil {
		return pa.Receipt{}, err
	}
	if m.ExpectedVersion == 0 && err != nil && pa.Code(err) != pa.NotFound {
		return pa.Receipt{}, err
	}
	if m.ExpectedVersion > 0 && execution.TargetSettingsVersion > 0 {
		if max(1, before.Bot.SettingsVersion) != execution.TargetSettingsVersion {
			return pa.Receipt{}, failure(pa.StaleVersion, "manager_target")
		}
		m.ExpectedVersion = before.Bot.Version
		updated := *m.Bot
		updated.Version = before.Bot.Version + 1
		m.Bot = &updated
		m.Events = append([]pa.Event(nil), m.Events...)
		for i, event := range m.Events {
			if event.Bot != nil {
				summary := *event.Bot
				summary.Bot.Version, summary.Version = updated.Version, updated.Version
				m.Events[i].Bot = &summary
			}
		}
	}
	return s.commitWith(ctx, m, func(ctx context.Context, tx *sql.Tx, target aggregate) error {
		manager, err := load(ctx, tx, execution.Fence.Key)
		if err != nil {
			return err
		}
		// A self-edit has just persisted its own next version. The target
		// CAS proves the pre-edit version is still the approved one.
		if m.Command.Key == execution.Fence.Key {
			if max(1, before.Bot.SettingsVersion) != execution.Under.Revision {
				return failure(pa.Revoked, "manage_team")
			}
			manager.Bot = before.Bot
		}
		current := app.ManagerContext{Caller: manager.Bot, Grant: app.GrantFor(manager.Bot), Project: manager.Bot.Project}
		if err = app.ConfirmManagerPlan(execution.Under, current); err != nil {
			return err
		}
		turn, err := exchangeActive(manager, execution.Fence, execution.TurnID)
		if err != nil {
			return err
		}
		binding, ok := manager.Bindings[execution.Fence.BindingID]
		if !ok || binding.Fence != execution.Fence || binding.Revoked {
			return failure(pa.Revoked, "manager_binding")
		}
		if turn.Origin == pa.BotRequestOrigin {
			return failure(pa.ScopeDenied, "bot_request_tools")
		}
		if turn.Room != nil {
			g, err := groupRead(ctx, tx, turn.Room.Group)
			if err != nil {
				return err
			}
			room, err := roomRead(ctx, tx, turn.Room.Group)
			if err != nil {
				return err
			}
			if err = pa.CheckRoomTicket(g, room, *turn.Room); err != nil {
				return err
			}
			if !pa.GroupAllowsProject(g.Project, manager.Bot.Project) {
				return failure(pa.ScopeDenied, "group_member_project")
			}
		}
		if m.ExpectedVersion == 0 {
			project := execution.CreationProject
			if project == (pa.ProjectScope{}) {
				project = manager.Bot.Project
			} else if project.MachineID != manager.Bot.Key.MachineID || project.Kind == pa.PersonalWorkspace {
				return failure(pa.ScopeDenied, "manager_target_project")
			}
			if target.Bot.Project != project {
				return failure(pa.ScopeDenied, "manager_target_project")
			}
			if target.Bot.CreatedBy == nil || *target.Bot.CreatedBy != manager.Bot.Key || target.Bot.Lead || target.Bot.ManageTeam != manager.Bot.ManageTeam || target.Bot.Agent != manager.Bot.Agent || target.Bot.PermissionPolicy != manager.Bot.PermissionPolicy || target.Bot.Mode != manager.Bot.Mode || target.Bot.FallbackAgents != (pa.FallbackConfig{}) || target.Bot.FallbackSteps != (pa.FallbackStepConfig{}) {
				return failure(pa.ScopeDenied, "manager_inheritance")
			}
			rows, err := tx.QueryContext(ctx, `SELECT data FROM project_assistant_bots WHERE machine=? AND bot<>?`, manager.Bot.Key.MachineID, target.Bot.Key.BotID)
			if err != nil {
				return err
			}
			created := 0
			for rows.Next() {
				var raw []byte
				if err = rows.Scan(&raw); err != nil {
					rows.Close()
					return err
				}
				var a aggregate
				if err = json.Unmarshal(raw, &a); err != nil {
					rows.Close()
					return err
				}
				if a.Bot.CreatedBy != nil {
					created++
				}
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return err
			}
			if created >= maxCreated {
				return failure(pa.CapacityFull, "manager_created")
			}
		} else {
			if before.BackgroundOwner != nil {
				return failure(pa.NotFound, "bot")
			}
			if !current.CanManageBotSetup(before.Bot) || target.Bot.Project != before.Bot.Project {
				return failure(pa.ScopeDenied, "manager_target_project")
			}
			allowed, err := pa.ApplyEdit(before.Bot, pa.EditBot{ExpectedVersion: m.ExpectedVersion, Name: target.Bot.Name, Instructions: target.Bot.Instructions.Text, Appearance: target.Bot.Appearance})
			if err != nil {
				return err
			}
			allowed.Hidden, allowed.UpdatedAt, allowed.SettingsVersion = target.Bot.Hidden, target.Bot.UpdatedAt, target.Bot.SettingsVersion
			if !reflect.DeepEqual(allowed, target.Bot) {
				return failure(pa.ScopeDenied, "manager_fields")
			}
		}
		return nil
	})
}
