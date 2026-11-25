package http

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	"github.com/sainteye/clawdline/internal/domain/modelcatalog"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func (s *Server) projectAssistantSharedSettings() projectassistantstore.SharedSettings {
	return projectassistantstore.SharedSettings{Dir: filepath.Join(s.cfg.Dir, "project-assistants")}
}

// projectAssistantDispatchAgents lists agents a Bot dispatch can run on this
// machine, with the effort values each launcher accepts. Gemini and Grok are
// available exactly when the broker would admit their dispatch: their owned
// session host is running.
func (s *Server) projectAssistantDispatchAgents(now time.Time) []pa.DispatchAgentOption {
	var out []pa.DispatchAgentOption
	for _, agent := range []pa.Agent{pa.ClaudeCode, pa.Codex, pa.Gemini, pa.Grok} {
		available := false
		switch agent {
		case pa.Gemini, pa.Grok:
			available = s.ownedHost(session.Assistant(agent)) != nil
		default:
			available = quotaReader().Observe(string(agent), now).Installed
		}
		if !available {
			continue
		}
		out = append(out, pa.DispatchAgentOption{Agent: agent, Efforts: modelcatalog.Efforts(string(agent))})
	}
	return out
}

type sharedSettingsReply struct {
	Settings pa.SharedSettings        `json:"settings"`
	Agents   []pa.DispatchAgentOption `json:"agents"`
	Limits   map[string]int           `json:"limits"`
}

func sharedSettingsLimits() map[string]int {
	return map[string]int{"memory_entries": pa.MaxSharedMemoryEntries, "memory_entry_bytes": pa.MaxSharedMemoryEntryBytes,
		"memory_total_bytes": pa.MaxSharedMemoryTotalBytes, "rules": pa.MaxDispatchRules, "rule_agents": pa.MaxDispatchRuleAgents}
}

// projectAssistantSharedSettingsRoute reads and replaces the machine's
// dispatch rules and shared Bot memory. PUT carries expected_version.
func (s *Server) projectAssistantSharedSettingsRoute(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	store := s.projectAssistantSharedSettings()
	agents := s.projectAssistantDispatchAgents(time.Now())
	if agents == nil {
		agents = []pa.DispatchAgentOption{}
	}
	switch r.Method {
	case http.MethodGet:
		current, err := store.Load()
		if err != nil {
			paWriteHTTPFailure(w, err)
			return
		}
		writeJSON(w, sharedSettingsReply{current, agents, sharedSettingsLimits()})
	case http.MethodPut:
		var body struct {
			ExpectedVersion *int64            `json:"expected_version"`
			Settings        pa.SharedSettings `json:"settings"`
		}
		dec := json.NewDecoder(io.LimitReader(r.Body, pa.MaxSharedSettingsBodyBytes))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil || body.ExpectedVersion == nil {
			paWriteHTTPFailure(w, paHTTPFailure(pa.Invalid, "body"))
			return
		}
		saved, err := store.Save(*body.ExpectedVersion, body.Settings)
		if err != nil {
			paWriteHTTPFailure(w, err)
			return
		}
		s.projectAssistantSharedSettingsChanged(r.Context())
		writeJSON(w, sharedSettingsReply{saved, agents, sharedSettingsLimits()})
	default:
		paWriteHTTPFailure(w, paHTTPFailure(pa.Unsupported, "method"))
	}
}

// projectAssistantSharedSettingsChanged lets running Bots read new shared
// memory on their next turn. Without a running host nothing is pending.
func (s *Server) projectAssistantSharedSettingsChanged(context.Context) {}

// projectAssistantSharedMemory reads the stored document on every prompt so
// a saved change reaches the next fresh, compacted or memory-updated prompt.
func (s *Server) projectAssistantSharedMemory(_ context.Context, bot pa.Bot) string {
	shared, err := s.projectAssistantSharedSettings().Load()
	if err != nil {
		return ""
	}
	return pa.SharedMemoryOpening(shared, bot.Project.ProjectID, shared.UsesSharedMemory(bot.Key.BotID))
}
