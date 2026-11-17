package botnetwork

import (
	"context"
	"encoding/json"

	app "github.com/sainteye/clawdline/internal/app/botnetwork"
	wire "github.com/sainteye/clawdline/internal/domain/agenthandoff"
)

func (s *Store) Outgoing(ctx context.Context, source wire.BotEndpoint) ([]app.Grant, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT data FROM project_assistant_network_grants WHERE machine=? ORDER BY id`, s.machine)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []app.Grant{}
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		var grant app.Grant
		if err = json.Unmarshal(raw, &grant); err != nil {
			return nil, err
		}
		a := grant.Source
		if a.AccountID == source.AccountID && a.MachineID == source.MachineID && a.ProjectID == source.ProjectID && a.BotID == source.BotID && a.ConversationID == source.ConversationID {
			out = append(out, grant)
		}
	}
	return out, rows.Err()
}
