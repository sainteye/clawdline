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
	return s.ProfileRest(ctx, machine, agent, "")
}
func (s *Store) ProfileRest(ctx context.Context, machine string, agent pa.Agent, profile string) (*pa.AgentRest, error) {
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT data FROM project_assistant_agent_rests WHERE machine=? AND agent=?`, machine, pa.LoginKey(agent, profile)).Scan(&raw)
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
	if rest.MachineID != machine || rest.Agent != agent || rest.LoginProfile != profile || rest.Failures < 1 {
		return nil, failure(pa.Invalid, "agent_rest")
	}
	return &rest, nil
}

func applyAgentRest(ctx context.Context, tx *sql.Tx, a aggregate, m app.Mutation) error {
	if m.Turn != nil {
		for _, fact := range m.Turn.Attempts {
			if fact.ResumeBindingID == "" {
				continue
			}
			b, ok := a.Bindings[fact.ResumeBindingID]
			if !ok || b.Agent != fact.Agent || b.LoginProfile != fact.LoginProfile || b.SessionSlot != "" || b.NativeSessionID == "" || b.Fence.Key != fact.Fence.Key || b.Fence.Project != fact.Fence.Project || b.Fence.ConversationID != fact.Fence.ConversationID || b.Fence.Generation >= fact.Fence.Generation || fact.CarryAfterSeq > a.Conversation.LastMessageSeq {
				return failure(pa.ScopeDenied, "attempt_resume_provenance")
			}
		}
	}
	if m.AgentRest != nil {
		rest := m.AgentRest
		if rest.MachineID != a.Bot.Key.MachineID || m.Turn == nil || len(m.Turn.Attempts) == 0 {
			return failure(pa.ScopeDenied, "agent_rest")
		}
		fact := m.Turn.Attempts[len(m.Turn.Attempts)-1]
		// A held quota failure can rest a login for a future explicit turn.
		// Only SafeAttemptClosed can advance the current turn to another Agent.
		if !pa.SafeAttemptClosed(*m.Turn) &&
			!(fact.Stage == pa.AttemptHeld && fact.SendReturned && fact.CleanupConfirmed && pa.IsQuotaReason(fact.Reason)) {
			return failure(pa.ScopeDenied, "agent_rest")
		}
		if fact.Agent != rest.Agent || fact.LoginProfile != rest.LoginProfile || fact.Reason != rest.Reason || rest.Failures < 1 {
			return failure(pa.ScopeDenied, "agent_rest_attempt")
		}
		var before *pa.AgentRest
		var prior []byte
		e := tx.QueryRowContext(ctx, `SELECT data FROM project_assistant_agent_rests WHERE machine=? AND agent=?`, rest.MachineID, pa.LoginKey(rest.Agent, rest.LoginProfile)).Scan(&prior)
		if e == nil {
			var saved pa.AgentRest
			if e = json.Unmarshal(prior, &saved); e != nil {
				return e
			}
			before = &saved
		} else if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		profile := rest.LoginProfile
		rest = pa.RestAfter(rest.MachineID, rest.Agent, rest.Reason, rest.RecordedAt, before)
		if rest != nil {
			rest.LoginProfile = profile
		}
		if rest == nil {
			return failure(pa.Invalid, "agent_rest_reason")
		}
		raw, err := json.Marshal(rest)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO project_assistant_agent_rests(machine,agent,data) VALUES(?,?,?) ON CONFLICT(machine,agent) DO UPDATE SET data=excluded.data`, rest.MachineID, pa.LoginKey(rest.Agent, rest.LoginProfile), raw)
		if err != nil {
			return err
		}
	}
	if m.ClearAgentRest != "" {
		if m.Turn == nil || m.Turn.State != pa.TurnSucceeded || len(m.Turn.Attempts) == 0 || m.Turn.Attempts[len(m.Turn.Attempts)-1].Agent != m.ClearAgentRest || m.Turn.Attempts[len(m.Turn.Attempts)-1].LoginProfile != m.ClearLoginProfile {
			return failure(pa.ScopeDenied, "clear_agent_rest")
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM project_assistant_agent_rests WHERE machine=? AND agent=?`, a.Bot.Key.MachineID, pa.LoginKey(m.ClearAgentRest, m.ClearLoginProfile))
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
	tried := map[pa.FallbackStep]bool{}
	for i, f := range next.Attempts {
		if !pa.ValidLoginProfileID(f.LoginProfile) || f.Ordinal < 1 || f.Ordinal > pa.MaxFallbackAgents+1 || !f.Agent.Valid() || f.Fence.Key != next.Fence.Key || f.Fence.Project != next.Fence.Project || f.Fence.ConversationID != next.Fence.ConversationID {
			return failure(pa.Invalid, "attempt")
		}
		if f.ToolActivity && !f.NativeActivity || f.SendReturned && !f.SendStarted || f.AnswerObserved && !f.SendReturned || f.CarryAfterSeq < 0 || f.CarryKind != "" && f.CarryKind != "recap" && f.CarryKind != "catch_up" || f.CarryKind == "catch_up" && f.ResumeBindingID == "" {
			return failure(pa.Invalid, "attempt_observation")
		}
		if i == 0 {
			if f.Ordinal != 1 || !validAttemptOpening(f) {
				return failure(pa.Invalid, "attempt_start")
			}
			tried[pa.FallbackStep{Agent: f.Agent, LoginProfile: f.LoginProfile}] = true
			continue
		}
		p := next.Attempts[i-1]
		if f.Ordinal == p.Ordinal+1 {
			if !pa.SafeAttemptClosed(pa.Turn{Attempts: []pa.AttemptFact{p}}) || !validAttemptOpening(f) || f.Fence.Generation <= p.Fence.Generation || f.Fence.BindingID == p.Fence.BindingID || tried[pa.FallbackStep{Agent: f.Agent, LoginProfile: f.LoginProfile}] {
				return failure(pa.ScopeDenied, "attempt_transition")
			}
			tried[pa.FallbackStep{Agent: f.Agent, LoginProfile: f.LoginProfile}] = true
			continue
		}
		if f.Ordinal != p.Ordinal || f.Agent != p.Agent || f.LoginProfile != p.LoginProfile || f.Fence != p.Fence || p.SendStarted && !f.SendStarted || p.Opened && !f.Opened || p.SendReturned && !f.SendReturned || p.NativeActivity && !f.NativeActivity || p.ToolActivity && !f.ToolActivity || p.AnswerObserved && !f.AnswerObserved || p.ResumeBindingID != f.ResumeBindingID || p.CarryKind != f.CarryKind || p.CarryAfterSeq != f.CarryAfterSeq || p.CleanupConfirmed {
			return failure(pa.ScopeDenied, "attempt_identity")
		}
		valid := validAttemptStage(p, f)
		if !valid {
			return failure(pa.ScopeDenied, "attempt_stage")
		}
	}
	return nil
}

func validAttemptOpening(f pa.AttemptFact) bool {
	return f.Stage == pa.AttemptOpening && !f.SendStarted && !f.Opened && !f.SendReturned && !f.NativeActivity && !f.ToolActivity && !f.AnswerObserved && !f.CleanupConfirmed && f.Effect == "unknown" && f.Reason == ""
}

func validAttemptStage(before, after pa.AttemptFact) bool {
	// Native events append a sticky observation without changing the worker's
	// lifecycle phase. Neither a tool title nor its claimed completion clears
	// the unknown side-effect barrier.
	if after.Stage == before.Stage && (!before.NativeActivity && after.NativeActivity || !before.ToolActivity && after.ToolActivity) {
		copy := after
		copy.NativeActivity, copy.ToolActivity, copy.Effect = before.NativeActivity, before.ToolActivity, before.Effect
		return copy == before && after.Effect == "unknown" && before.Stage != pa.AttemptRefused && before.Stage != pa.AttemptClosed && before.Stage != pa.AttemptHeld
	}
	switch after.Stage {
	case pa.AttemptHeld:
		return before.Stage != pa.AttemptClosed && after.SendStarted == before.SendStarted && after.Effect == "unknown" && after.Opened == before.Opened && after.SendReturned == before.SendReturned && after.AnswerObserved == before.AnswerObserved
	case pa.AttemptRefused:
		return before.Stage == pa.AttemptOpening && !after.SendStarted && !after.NativeActivity && !after.ToolActivity && !after.SendReturned && !after.AnswerObserved && !after.Opened && !after.CleanupConfirmed && after.Effect == "none" && after.Reason != ""
	case pa.AttemptOpened:
		return before.Stage == pa.AttemptOpening && after.Opened && !after.SendStarted && !after.SendReturned && !after.AnswerObserved && !after.CleanupConfirmed && after.Effect == "unknown" && after.Reason == ""
	case pa.AttemptSending:
		return (before.Stage == pa.AttemptOpening || before.Stage == pa.AttemptOpened) && after.SendStarted && !after.SendReturned && !after.AnswerObserved && !after.CleanupConfirmed && after.Effect == "unknown" && after.Reason == ""
	case pa.AttemptSent:
		return before.Stage == pa.AttemptSending && after.SendReturned && !after.AnswerObserved && !after.CleanupConfirmed && after.Effect == "unknown" && after.Reason == ""
	case pa.AttemptAnswered:
		return before.Stage == pa.AttemptSent && after.SendReturned && after.AnswerObserved && !after.CleanupConfirmed && after.Effect == "unknown" && after.Reason == ""
	case pa.AttemptClosed:
		if before.Stage == pa.AttemptSent && before.Reason == "" && after.Reason != "" {
			expected := before
			expected.Stage, expected.Reason, expected.Effect, expected.CleanupConfirmed = pa.AttemptClosed, after.Reason, "none", true
			return after == expected && pa.SafeAttemptClosed(pa.Turn{Attempts: []pa.AttemptFact{after}})
		}
		if !after.CleanupConfirmed || after.SendStarted != before.SendStarted || after.Opened != before.Opened || after.SendReturned != before.SendReturned || after.AnswerObserved != before.AnswerObserved || after.NativeActivity != before.NativeActivity || after.ToolActivity != before.ToolActivity || after.Reason != before.Reason {
			return false
		}
		return before.Stage == pa.AttemptRefused && !after.SendStarted && after.Effect == "none" || (before.Stage == pa.AttemptSending || before.Stage == pa.AttemptAnswered) && after.SendStarted && after.Effect == "unknown"
	}
	return false
}
