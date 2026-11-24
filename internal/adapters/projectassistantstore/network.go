package projectassistantstore

import (
	"context"
	"database/sql"
	"errors"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Network settings live outside the Bot aggregate. Migration never decodes or
// rewrites historical aggregate, event, receipt or canonical archive bytes.
const networkSettingsSchema = `CREATE TABLE IF NOT EXISTS project_assistant_network_settings (
 account TEXT NOT NULL, machine TEXT NOT NULL, bot TEXT NOT NULL,
 project TEXT NOT NULL, canonical TEXT NOT NULL, kind TEXT NOT NULL,
 revision INTEGER NOT NULL CHECK(revision>0), offered INTEGER NOT NULL CHECK(offered IN (0,1)),
 assistants INTEGER NOT NULL CHECK(assistants IN (0,1)), PRIMARY KEY(account,machine,bot))`

func readNetworkSettings(ctx context.Context, r reader, scope pa.NetworkScope) (pa.NetworkSettings, error) {
	out := pa.NetworkSettings{Scope: scope}
	var project, canonical, kind string
	err := r.QueryRowContext(ctx, `SELECT project,canonical,kind,revision,offered,assistants FROM project_assistant_network_settings WHERE account=? AND machine=? AND bot=?`, scope.AccountID, scope.Key.MachineID, scope.Key.BotID).
		Scan(&project, &canonical, &kind, &out.Revision, &out.OfferedToNetwork, &out.ReachableByAssistants)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	if project != scope.Project.ProjectID || canonical != scope.Project.CanonicalID || kind != string(scope.Project.Kind) {
		return out, failure(pa.ScopeDenied, "network_project")
	}
	return out, nil
}

func networkAggregate(ctx context.Context, r reader, scope pa.NetworkScope) (aggregate, error) {
	if err := pa.ValidateNetworkScope(scope); err != nil {
		return aggregate{}, err
	}
	a, err := load(ctx, r, scope.Key)
	if err == nil && (a.Bot.Key != scope.Key || a.Bot.Project != scope.Project) {
		err = failure(pa.ScopeDenied, "network_bot")
	}
	return a, err
}

// NetworkSettings returns the account-specific default-off preference only for
// an actual current Bot. Deleted Bots cannot be rediscovered through old rows.
func (s *Store) NetworkSettings(ctx context.Context, scope pa.NetworkScope) (pa.NetworkSettings, error) {
	if _, err := networkAggregate(ctx, s.db, scope); err != nil {
		return pa.NetworkSettings{}, err
	}
	return readNetworkSettings(ctx, s.db, scope)
}

// SaveNetworkSettings is a persistence seam, not authentication. The human
// management service owns authority checks before and after this transaction.
func (s *Store) SaveNetworkSettings(ctx context.Context, write pa.NetworkSettingsWrite, capabilities pa.NetworkCapabilities) (pa.NetworkSettings, error) {
	unlock := s.lock(write.Scope.Key)
	defer unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return pa.NetworkSettings{}, err
	}
	defer tx.Rollback()
	a, err := networkAggregate(ctx, tx, write.Scope)
	if err != nil {
		return pa.NetworkSettings{}, err
	}
	// Bot.Version advances with every chat turn, so it is not compared here;
	// the settings revision below is the edit guard and the fence the boundary.
	currentFence := pa.Fence{Key: a.Bot.Key, Project: a.Bot.Project, ConversationID: a.Conversation.ID, BindingID: a.Conversation.BindingID, Generation: a.Conversation.Generation}
	if write.Fence != currentFence {
		return pa.NetworkSettings{}, failure(pa.StaleGeneration, "network_fence")
	}
	current, err := readNetworkSettings(ctx, tx, write.Scope)
	if err != nil {
		return current, err
	}
	if current.Revision != write.ExpectedRevision {
		return current, failure(pa.StaleVersion, "network_revision")
	}
	next, err := pa.ApplyNetworkSettings(current, write.Patch, capabilities)
	if err != nil {
		return current, err
	}
	if next.Revision != current.Revision {
		_, err = tx.ExecContext(ctx, `INSERT INTO project_assistant_network_settings(account,machine,bot,project,canonical,kind,revision,offered,assistants) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(account,machine,bot) DO UPDATE SET revision=excluded.revision,offered=excluded.offered,assistants=excluded.assistants`,
			write.Scope.AccountID, write.Scope.Key.MachineID, write.Scope.Key.BotID, write.Scope.Project.ProjectID, write.Scope.Project.CanonicalID, write.Scope.Project.Kind, next.Revision, next.OfferedToNetwork, next.ReachableByAssistants)
		if err != nil {
			return current, err
		}
	}
	if err = tx.Commit(); err != nil {
		return current, err
	}
	return next, nil
}
