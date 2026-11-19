package botdevserverstore

import (
	"context"
	"database/sql"
	"math"

	servers "github.com/sainteye/clawdline/internal/app/botdevserver"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Rule reads only known installed metadata. One rule per definition shares the
// existing definition bound; rules cannot allocate definitions or processes.
func (c *Catalog) Rule(ctx context.Context, saved servers.SavedServer) (servers.Rule, error) {
	tx, err := c.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return servers.Rule{}, err
	}
	defer tx.Rollback()
	if err := catalogReady(ctx, tx); err != nil {
		return servers.Rule{}, err
	}
	return ruleAt(ctx, tx, saved.ID, saved.Spec)
}

func ruleAt(ctx context.Context, tx *sql.Tx, id string, spec servers.Spec) (servers.Rule, error) {
	saved, _, err := definition(ctx, tx, id)
	if err != nil {
		return servers.Rule{}, err
	}
	if saved.Spec != spec {
		return servers.Rule{}, refusal(pa.ScopeDenied, "server_rule")
	}
	r := servers.Rule{ServerID: id, Spec: spec}
	err = tx.QueryRowContext(ctx, `SELECT rule_version,rule_decision FROM bot_devserver_definitions WHERE id=? AND removed=0`, id).Scan(&r.Version, &r.Decision)
	return r, err
}

// SaveRule returns a grant only after a known commit. A missing acknowledgement
// is an error; callers must not launch on an uncertain saved approval.
func (c *Catalog) SaveRule(ctx context.Context, expected servers.Rule, decision servers.Decision) (servers.Rule, error) {
	if expected.Version < 0 || expected.Version == math.MaxInt64 || (decision != "" && decision != servers.Allow && decision != servers.Deny) {
		return servers.Rule{}, refusal(pa.Invalid, "server_rule")
	}
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return servers.Rule{}, err
	}
	defer tx.Rollback()
	if err := catalogReady(ctx, tx); err != nil {
		return servers.Rule{}, err
	}
	got, err := ruleAt(ctx, tx, expected.ServerID, expected.Spec)
	if err != nil {
		return servers.Rule{}, err
	}
	if got != expected {
		return servers.Rule{}, refusal(pa.StaleVersion, "server_rule")
	}
	next := got
	next.Version++
	next.Decision = decision
	result, err := tx.ExecContext(ctx, `UPDATE bot_devserver_definitions SET rule_version=?,rule_decision=? WHERE id=? AND rule_version=? AND removed=0`, next.Version, next.Decision, next.ServerID, expected.Version)
	if err != nil {
		return servers.Rule{}, err
	}
	if n, err := result.RowsAffected(); err != nil {
		return servers.Rule{}, err
	} else if n != 1 {
		return servers.Rule{}, refusal(pa.StaleVersion, "server_rule")
	}
	if err := tx.Commit(); err != nil {
		return servers.Rule{}, err
	}
	return next, nil
}
