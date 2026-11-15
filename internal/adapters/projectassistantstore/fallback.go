package projectassistantstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func (s *Store) AgentRest(ctx context.Context, machine string, agent pa.Agent) (*pa.AgentRest, error) {
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT data FROM project_assistant_agent_rests WHERE machine=? AND agent=?`, machine, agent).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var rest pa.AgentRest
	if err = json.Unmarshal(raw, &rest); err != nil {
		return nil, err
	}
	if rest.MachineID != machine || rest.Agent != agent || rest.Failures < 1 {
		return nil, failure(pa.Invalid, "agent_rest")
	}
	return &rest, nil
}

func applyAgentRest(ctx context.Context, tx *sql.Tx, a aggregate, m app.Mutation) error {
	if m.AgentRest != nil {
		rest := m.AgentRest
		if rest.MachineID != a.Bot.Key.MachineID || m.Turn == nil || !pa.SafeAttemptClosed(*m.Turn) {
			return failure(pa.ScopeDenied, "agent_rest")
		}
		fact := m.Turn.Attempts[len(m.Turn.Attempts)-1]
		if fact.Agent != rest.Agent || fact.Reason != rest.Reason || rest.Failures < 1 {
			return failure(pa.ScopeDenied, "agent_rest_attempt")
		}
		var before *pa.AgentRest
		var prior []byte
		e := tx.QueryRowContext(ctx, `SELECT data FROM project_assistant_agent_rests WHERE machine=? AND agent=?`, rest.MachineID, rest.Agent).Scan(&prior)
		if e == nil {
			var saved pa.AgentRest
			if e = json.Unmarshal(prior, &saved); e != nil {
				return e
			}
			before = &saved
		} else if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		rest = pa.RestAfter(rest.MachineID, rest.Agent, rest.Reason, rest.RecordedAt, before)
		if rest == nil {
			return failure(pa.Invalid, "agent_rest_reason")
		}
		raw, err := json.Marshal(rest)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO project_assistant_agent_rests(machine,agent,data) VALUES(?,?,?) ON CONFLICT(machine,agent) DO UPDATE SET data=excluded.data`, rest.MachineID, rest.Agent, raw)
		if err != nil {
			return err
		}
	}
	if m.ClearAgentRest != "" {
		if m.Turn == nil || m.Turn.State != pa.TurnSucceeded || len(m.Turn.Attempts) == 0 || m.Turn.Attempts[len(m.Turn.Attempts)-1].Agent != m.ClearAgentRest {
			return failure(pa.ScopeDenied, "clear_agent_rest")
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM project_assistant_agent_rests WHERE machine=? AND agent=?`, a.Bot.Key.MachineID, m.ClearAgentRest)
		return err
	}
	return nil
}

func checkAttemptAppend(old, next pa.Turn) error {
	if len(next.Attempts) < len(old.Attempts) || !reflect.DeepEqual(old.Attempts, next.Attempts[:len(old.Attempts)]) && len(old.Attempts) > 0 {
		return failure(pa.ScopeDenied, "attempt_immutable")
	}
	// A mutation adds exactly one fact. Existing clients may preserve no facts.
	if len(next.Attempts) > len(old.Attempts)+1 {
		return failure(pa.Invalid, "attempt_append")
	}
	return checkAttemptFacts(next)
}

func checkAttemptFacts(next pa.Turn) error {
	if len(next.Attempts) > 0 && next.Attempts[len(next.Attempts)-1].Fence != next.Fence {
		return failure(pa.ScopeDenied, "attempt_current_fence")
	}
	tried := map[pa.Agent]bool{}
	for i, f := range next.Attempts {
		if f.Ordinal < 1 || f.Ordinal > pa.MaxFallbackAgents+1 || f.Agent != pa.ClaudeCode && f.Agent != pa.Codex || f.Fence.Key != next.Fence.Key || f.Fence.Project != next.Fence.Project || f.Fence.ConversationID != next.Fence.ConversationID {
			return failure(pa.Invalid, "attempt")
		}
		if i == 0 {
			if f.Ordinal != 1 || f.Stage != pa.AttemptOpening || f.SendStarted || f.CleanupConfirmed || f.Effect != "unknown" || f.Reason != "" {
				return failure(pa.Invalid, "attempt_start")
			}
			tried[f.Agent] = true
			continue
		}
		p := next.Attempts[i-1]
		if f.Ordinal == p.Ordinal+1 {
			if p.Stage != pa.AttemptClosed || !p.CleanupConfirmed || p.SendStarted || p.Effect != "none" || p.Reason == "" || f.Stage != pa.AttemptOpening || f.Fence.Generation <= p.Fence.Generation || f.Fence.BindingID == p.Fence.BindingID || tried[f.Agent] || f.SendStarted || f.CleanupConfirmed || f.Effect != "unknown" || f.Reason != "" {
				return failure(pa.ScopeDenied, "attempt_transition")
			}
			tried[f.Agent] = true
			continue
		}
		if f.Ordinal != p.Ordinal || f.Agent != p.Agent || f.Fence != p.Fence || p.SendStarted && !f.SendStarted || p.CleanupConfirmed {
			return failure(pa.ScopeDenied, "attempt_identity")
		}
		valid := validAttemptStage(p, f)
		if !valid {
			return failure(pa.ScopeDenied, "attempt_stage")
		}
	}
	return nil
}

func validAttemptStage(before, after pa.AttemptFact) bool {
	switch after.Stage {
	case pa.AttemptHeld:
		return before.Stage != pa.AttemptClosed && after.SendStarted == before.SendStarted && after.Effect == "unknown"
	case pa.AttemptRefused:
		return before.Stage == pa.AttemptOpening && !after.SendStarted && !after.CleanupConfirmed && after.Effect == "none" && after.Reason != ""
	case pa.AttemptSending:
		return before.Stage == pa.AttemptOpening && after.SendStarted && !after.CleanupConfirmed && after.Effect == "unknown" && after.Reason == ""
	case pa.AttemptClosed:
		if !after.CleanupConfirmed || after.SendStarted != before.SendStarted || after.Reason != before.Reason {
			return false
		}
		return before.Stage == pa.AttemptRefused && !after.SendStarted && after.Effect == "none" || before.Stage == pa.AttemptSending && after.SendStarted && after.Effect == "unknown"
	}
	return false
}
