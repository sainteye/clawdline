package projectassistanthost

import (
	"context"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func (s *toolScope) bindPlaybookTools(tools *NativeWork) {
	if s.turn.Origin != pa.BotRequestOrigin && (s.taskOwner.BotID != "" || !pa.DirectUserTurn(s.turn)) {
		return
	}
	store, fence, turnID := s.h.store, s.q.Binding.Fence, s.turnID
	tools.playbookAccess = tools.charterAvailable
	tools.readCharter = func(ctx context.Context) (app.CharterRead, error) {
		ctx, done, err := s.bound(ctx)
		if err != nil {
			return app.CharterRead{}, err
		}
		defer done()
		v, err := store.ReadBotCharter(ctx, fence, turnID)
		return app.CharterRead{ProjectID: fence.Project.ProjectID, Version: v.Version, Text: v.Text, Workflow: v.Workflow}, err
	}
	tools.readPlaybooks = func(ctx context.Context, id string) (app.PlaybookRead, error) {
		ctx, done, err := s.bound(ctx)
		if err != nil {
			return app.PlaybookRead{}, err
		}
		defer done()
		v, err := store.ReadBotCharter(ctx, fence, turnID)
		if err != nil {
			return app.PlaybookRead{}, err
		}
		result := app.PlaybookRead{Version: v.Version, Playbooks: []pa.CharterPlaybook{}}
		for _, p := range v.Playbooks {
			if id == p.ID {
				result.Playbooks = append(result.Playbooks, p)
				return result, nil
			}
			if id == "" {
				result.Playbooks = append(result.Playbooks, pa.CharterPlaybook{ID: p.ID, Title: p.Title})
			}
		}
		if id != "" {
			return app.PlaybookRead{}, failure(pa.NotFound, "charter_playbook")
		}
		return result, nil
	}
}
