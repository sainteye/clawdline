package projectassistantstore

import (
	"context"
	"database/sql"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func markLeadSetup(ctx context.Context, tx *sql.Tx, machine string) error {
	_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO project_assistant_lead_setup(machine) VALUES(?)`, machine)
	return err
}

func (s *Store) LeadSetupNeeded(ctx context.Context, machine string) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var done, hasLead bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM project_assistant_lead_setup WHERE machine=?),
		EXISTS(SELECT 1 FROM project_assistant_bots WHERE machine=?
		AND json_extract(CAST(data AS TEXT),'$.BackgroundOwner') IS NULL
		AND json_extract(CAST(data AS TEXT),'$.Bot.lead')=1)`, machine, machine).Scan(&done, &hasLead)
	if err != nil {
		return false, err
	}
	if !done && hasLead {
		if err = markLeadSetup(ctx, tx, machine); err != nil {
			return false, err
		}
	}
	return !done && !hasLead, tx.Commit()
}

// SetupLead commits the first lead and its routine together. A concurrent
// designation or earlier setup wins, including a setup whose Bot was deleted.
func (s *Store) SetupLead(ctx context.Context, mutation app.Mutation) error {
	key := mutation.Command.Key
	if mutation.Bot == nil || !mutation.Bot.Lead || mutation.Bot.Project.Kind != pa.PersonalWorkspace ||
		mutation.ExpectedVersion != 0 || mutation.Routine == nil || !pa.IsLeadCheckIn(mutation.Routine.ID) {
		return failure(pa.Invalid, "lead_setup")
	}
	unlock := s.lock(key)
	defer unlock()
	_, err := s.commitWith(ctx, mutation, func(ctx context.Context, tx *sql.Tx, a aggregate) error {
		result, err := tx.ExecContext(ctx, `INSERT INTO project_assistant_lead_setup(machine)
			SELECT ? WHERE NOT EXISTS(SELECT 1 FROM project_assistant_bots WHERE machine=? AND bot<>?
			AND json_extract(CAST(data AS TEXT),'$.BackgroundOwner') IS NULL
			AND json_extract(CAST(data AS TEXT),'$.Bot.lead')=1)
			ON CONFLICT DO NOTHING`, key.MachineID, key.MachineID, key.BotID)
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err == nil && changed != 1 {
			err = failure(pa.StaleVersion, "lead_setup")
		}
		return err
	})
	return err
}
