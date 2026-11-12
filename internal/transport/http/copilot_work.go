package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/sainteye/clawdline/internal/adapters/planner"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/voicework"
)

func (s *Server) voiceWorkText(en, zh string) string {
	if s.productLanguage() == "zh-Hant" {
		return zh
	}
	return en
}

// StartVoiceWork attaches explicitly started planning to daemon shutdown. It
// never starts a model, recovers an action or resumes a queue by itself.
func (s *Server) StartVoiceWork(ctx context.Context) {
	s.voiceWorkMu.Lock()
	s.voiceWorkContext = ctx
	s.voiceWorkMu.Unlock()
}

var voiceWorkPlan = func(ctx context.Context, input, assistant string) (planner.VoiceWorkPlan, error) {
	return planner.New().VoiceWork(ctx, input, assistant)
}

type voiceWorkRequest struct {
	InputIDs []string                 `json:"input_ids"`
	Retry    bool                     `json:"retry"`
	Action   string                   `json:"action"`
	ID       string                   `json:"id"`
	Call     string                   `json:"call"`
	Text     string                   `json:"text"`
	Version  int64                    `json:"version"`
	Runner   string                   `json:"runner"`
	Key      string                   `json:"key"`
	Result   string                   `json:"result"`
	Error    string                   `json:"error"`
	Unknown  bool                     `json:"unknown"`
	Sessions []copilotDecisionSession `json:"sessions"`
}

func (s *Server) copilotWorkFailure(w http.ResponseWriter, err error) {
	status := http.StatusServiceUnavailable
	code := "voice_work_unavailable"
	if errors.Is(err, voicework.ErrConflict) || errors.Is(err, voicework.ErrUnknown) {
		status = http.StatusConflict
		code = err.Error()
	}
	if errors.Is(err, voicework.ErrInvalid) {
		status = http.StatusBadRequest
		code = err.Error()
	}
	if errors.Is(err, voicework.ErrFull) {
		status = http.StatusConflict
		code = err.Error()
	}
	writeRawRefusal(w, status, code, s.voiceWorkText("The voice assistant work list could not be updated. Reload it before trying again.", "語音助理工作清單無法更新，請先重新讀取再試。"))
}

func (s *Server) copilotWorkRoute(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if s.store == nil {
		s.copilotWorkFailure(w, errors.New("missing store"))
		return
	}
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		state, err := s.store.VoiceWork(r.Context())
		if err != nil {
			s.copilotWorkFailure(w, err)
			return
		}
		s.voiceWorkMu.Lock()
		planning := s.voiceWorkRunning
		s.voiceWorkMu.Unlock()
		writeJSON(w, map[string]any{"state": state, "planning": planning})
		return
	}
	if r.Method != http.MethodPost {
		writeRefusal(w, 405, "method_not_allowed", "Use GET or POST.")
		return
	}
	if !maySend(r) {
		writeAuthRefusal(w, 403, "forbidden", s.voiceWorkText("This device may read, and not change assistant work.", "這個裝置只能讀取，不能變更語音助理工作。"))
		return
	}
	raw, readErr := io.ReadAll(io.LimitReader(r.Body, voicework.MaxRequestBytes+1))
	if readErr != nil || len(raw) > voicework.MaxRequestBytes {
		s.copilotWorkFailure(w, voicework.ErrInvalid)
		return
	}
	var in voiceWorkRequest
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(&in) != nil || dec.Decode(new(any)) != io.EOF || len(in.Sessions) > maxCopilotDecisionSessions || len(in.InputIDs) > voicework.MaxRecords {
		s.copilotWorkFailure(w, voicework.ErrInvalid)
		return
	}
	requestKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if requestKey == "" {
		writeRefusal(w, 400, "idempotency_key_required", "This change needs an Idempotency-Key.")
		return
	}
	if in.Runner != "" {
		in.Runner = actorOf(r) + ":" + in.Runner
	}
	s.receipted(w, r, store.ReceiptKey{Scope: "voice-assistant-work", Actor: actorOf(r), Key: requestKey}, requestDigest(raw), func(status int) bool { return status < 500 }, func(w http.ResponseWriter) {
		now := time.Now().Unix()
		var claimed *voicework.Task
		state, err := s.store.UpdateVoiceWork(r.Context(), func(state *voicework.State) error {
			switch in.Action {
			case "input":
				_, err := state.AddInput(voicework.Input{ID: in.ID, Call: in.Call, Text: in.Text, At: now, Ready: in.Sessions != nil})
				return err
			case "plan":
				if in.Retry {
					state.PlannerError = ""
					state.Revision++
				}
				return state.Ready(in.InputIDs)
			case "claim":
				settings, err := s.readCopilot()
				if err != nil || !settings.WorkEnabled {
					return nil
				}
				claimed = state.Claim(in.Runner, now)
				return nil
			case "heartbeat":
				state.Heartbeat(in.Runner, now)
				return nil
			case "progress":
				return state.Progress(in.ID, in.Runner, in.Key, in.Result, now)
			case "finish":
				return state.Finish(in.ID, in.Runner, in.Key, in.Result, in.Error, in.Unknown, now)
			default:
				return state.Edit(in.ID, in.Version, in.Action, now)
			}
		})
		if err != nil {
			s.copilotWorkFailure(w, err)
			return
		}
		// Persist and acknowledge first. No handler waits for a model to answer.
		writeJSON(w, map[string]any{"state": state, "claimed": claimed})
		if in.Action == "plan" || in.Action == "input" && in.Sessions != nil {
			s.beginVoiceWork(in.Sessions)
		}
	})
}

func (s *Server) beginVoiceWork(sessions []copilotDecisionSession) {
	settings, err := s.readCopilot()
	if err != nil || !settings.WorkEnabled {
		return
	}
	s.voiceWorkMu.Lock()
	s.voiceWorkWake++
	s.voiceWorkSessions = append([]copilotDecisionSession(nil), sessions...)
	if s.voiceWorkRunning {
		s.voiceWorkMu.Unlock()
		return
	}
	s.voiceWorkRunning = true
	ctx := s.voiceWorkContext
	if ctx == nil {
		ctx = context.Background()
	}
	s.voiceWorkMu.Unlock()
	go func() {
		released := false
		defer func() {
			if !released {
				s.voiceWorkMu.Lock()
				s.voiceWorkRunning = false
				s.voiceWorkMu.Unlock()
			}
		}()
		for {
			if ctx.Err() != nil {
				return
			}
			settings, err := s.readCopilot()
			if err != nil || !settings.WorkEnabled {
				return
			}
			s.voiceWorkMu.Lock()
			wake := s.voiceWorkWake
			sessions := append([]copilotDecisionSession(nil), s.voiceWorkSessions...)
			s.voiceWorkMu.Unlock()
			state, err := s.store.VoiceWork(ctx)
			if err != nil {
				return
			}
			if state.PlannerError != "" {
				return
			}
			if len(state.ReadyPending()) == 0 {
				s.voiceWorkMu.Lock()
				changed := wake != s.voiceWorkWake
				if !changed {
					s.voiceWorkRunning = false
					released = true
				}
				s.voiceWorkMu.Unlock()
				if changed {
					continue
				}
				return
			}
			pending := state.ReadyPending()
			ids := make([]string, len(pending))
			for i, in := range pending {
				ids[i] = in.ID
			}
			raw, _ := json.Marshal(map[string]any{"inputs": pending, "tasks": state.Tasks, "sessions": sessions, "language": s.productLanguage()})
			plan, err := voiceWorkPlan(ctx, string(raw), settings.Effective().WorkAssistant)
			if err == nil {
				// Temporary ids are only model-local names; storage assigns identities.
				names := map[string]string{}
				for _, c := range plan.Changes {
					if c.Op == "add" {
						if names[c.ID] != "" {
							err = voicework.ErrInvalid
							break
						}
						names[c.ID] = uuid.NewString()
					}
				}
				if err == nil {
					for i := range plan.Changes {
						c := &plan.Changes[i]
						delete(c.Args, "force_confirmed")
						delete(c.Args, "switch_confirmed")
						delete(c.Args, "_voice_target")
						if wanted, ok := c.Args["session_id"].(string); ok && wanted != "" {
							for _, row := range sessions {
								if wanted == row.ID || wanted == row.Label {
									c.Args["session_id"] = row.ID
									c.Args["_voice_target"] = map[string]any{"id": row.ID, "label": row.Label, "conversation": row.Conversation, "machine": row.Machine}
									break
								}
							}
							if c.Args["_voice_target"] == nil {
								err = voicework.ErrInvalid
								break
							}
						}
						if id := names[c.ID]; id != "" {
							c.ID = id
						}
						for j, id := range c.DependsOn {
							if n := names[id]; n != "" {
								c.DependsOn[j] = n
							}
						}
					}
					if err == nil {
						_, err = s.store.UpdateVoiceWork(ctx, func(next *voicework.State) error {
							current, readErr := s.readCopilot()
							if readErr != nil || !current.WorkEnabled {
								return voicework.ErrConflict
							}
							return next.Apply(state.PlanVersion, ids, plan.Changes, plan.Reply, time.Now().Unix())
						})
					}
				}
			}
			if err != nil {
				code := "voice_work_planning_failed"
				if errors.Is(err, planner.ErrOutOfQuota) {
					code = "voice_work_quota"
				}
				if errors.Is(err, planner.ErrNoPlanner) {
					code = "voice_work_provider_missing"
				}
				if errors.Is(err, voicework.ErrConflict) {
					code = "voice_work_plan_changed"
				}
				_, _ = s.store.UpdateVoiceWork(ctx, func(next *voicework.State) error { next.PlannerError = code; next.Revision++; return nil })
				return
			}
		}
	}()
}
