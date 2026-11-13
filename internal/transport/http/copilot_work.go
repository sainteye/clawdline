package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
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

const voiceWorkProtocol = 3

type voiceWorkRequest struct {
	Batch           *voicework.BatchOffer    `json:"batch"`
	Context         string                   `json:"context"`
	Deferred        bool                     `json:"deferred"`
	Focus           *voicework.Focus         `json:"focus"`
	ExecutorVersion int                      `json:"executor_version"`
	Tools           []string                 `json:"tools"`
	ToolContracts   json.RawMessage          `json:"tool_contracts"`
	InputIDs        []string                 `json:"input_ids"`
	Retry           bool                     `json:"retry"`
	Action          string                   `json:"action"`
	ID              string                   `json:"id"`
	Call            string                   `json:"call"`
	Text            string                   `json:"text"`
	Version         int64                    `json:"version"`
	Runner          string                   `json:"runner"`
	Key             string                   `json:"key"`
	Result          string                   `json:"result"`
	Error           string                   `json:"error"`
	Unknown         bool                     `json:"unknown"`
	Sessions        []copilotDecisionSession `json:"sessions"`
}

func (s *Server) copilotWorkFailure(w http.ResponseWriter, err error) {
	if errors.Is(err, voicework.ErrAmbiguous) {
		writeRawRefusal(w, http.StatusConflict, err.Error(), s.voiceWorkText("More than one Session has that name. Choose the exact recipient on screen before preparing a voice message.", "有不只一個同名 Session，請先從畫面選定確切收件對象，再準備語音訊息。"))
		return
	}
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
		writeJSON(w, map[string]any{"state": state, "planning": planning, "protocol": voiceWorkProtocol, "message_confirmation": true, "continuation": s.voiceContinuation(state), "auto_continue": true})
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
		var prepared *voicework.Task
		state, err := s.store.UpdateVoiceWork(r.Context(), func(state *voicework.State) error {
			switch in.Action {
			case "input":
				_, err := state.AddInput(voicework.Input{ID: in.ID, Call: in.Call, Text: in.Text, At: now, Ready: in.Sessions != nil && !in.Deferred, Deferred: in.Deferred, Focus: in.Focus, Context: in.Context})
				return err
			case "prepare_message":
				settings, err := s.readCopilot()
				if err != nil || !settings.WorkEnabled || len(in.InputIDs) != 1 {
					return voicework.ErrConflict
				}
				var target *copilotDecisionSession
				for i := range in.Sessions {
					if in.Sessions[i].ID == in.ID {
						if target != nil {
							return voicework.ErrInvalid
						}
						target = &in.Sessions[i]
					}
				}
				if target == nil || len(target.Options) > 0 || target.MenuFingerprint != "" {
					return voicework.ErrInvalid
				}
				for _, row := range in.Sessions {
					if target.Label != "" && row.ID != target.ID && row.Label == target.Label {
						return voicework.ErrAmbiguous
					}
				}
				label := target.Label
				if label == "" {
					label = target.ID
				}
				taskID := uuid.NewString()
				if err := state.PrepareMessage(taskID, in.Call, in.InputIDs[0], voicework.Focus{ID: target.ID, Label: label, Machine: target.Machine, Conversation: target.Conversation}, in.Text, now); err != nil {
					return err
				}
				for i := range state.Tasks {
					if state.Tasks[i].ID == taskID {
						copy := state.Tasks[i]
						prepared = &copy
					}
				}
				return nil
			case "continue_batch":
				settings, err := s.readCopilot()
				if err != nil || !settings.WorkEnabled || in.Batch == nil || len(in.InputIDs) > 1 {
					return voicework.ErrConflict
				}
				input := ""
				if len(in.InputIDs) == 1 {
					input = in.InputIDs[0]
				}
				return state.Continue(*in.Batch, settings.Effective().WorkAutoContinue, in.Call, input)
			case "confirm_message":
				settings, err := s.readCopilot()
				if err != nil || !settings.WorkEnabled || len(in.InputIDs) != 1 {
					return voicework.ErrConflict
				}
				return state.ConfirmMessage(in.ID, in.Version, in.Call, in.InputIDs[0], now)
			case "dismiss_input":
				return state.Dismiss(in.ID)
			case "park":
				return state.Park(in.ID, in.Call)
			case "park_call", "end_call":
				return state.EndCall(in.Call)
			case "answer":
				return state.Answer(in.ID, in.Call)
			case "plan":
				if in.Retry {
					if err := state.RetryPlanning(in.InputIDs); err != nil {
						return err
					}
				}
				return state.Ready(in.InputIDs)
			case "claim":
				settings, err := s.readCopilot()
				if err != nil || !settings.WorkEnabled {
					return nil
				}
				if in.ExecutorVersion != 2 || in.Tools == nil {
					return voicework.ErrInvalid
				}
				claimed = state.ClaimBatch(in.Runner, in.Call, in.Tools, now, settings.Effective().WorkAutoContinue)
				return nil
			case "begin_effect":
				return state.BeginEffect(in.ID, in.Runner, in.Key, now)
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
		if in.Action == "finish" && len(state.ReadyPending()) > 0 {
			s.voiceWorkMu.Lock()
			hasContext := len(s.voiceWorkContracts) > 0
			s.voiceWorkMu.Unlock()
			if !hasContext {
				state, err = s.store.UpdateVoiceWork(r.Context(), func(next *voicework.State) error {
					next.PlannerError = "voice_work_context_needed"
					next.Revision++
					return nil
				})
				if err != nil {
					s.copilotWorkFailure(w, err)
					return
				}
			}
		}
		// Persist and acknowledge first. No handler waits for a model to answer.
		writeJSON(w, map[string]any{"state": state, "claimed": claimed, "prepared": prepared, "protocol": voiceWorkProtocol, "message_confirmation": true, "continuation": s.voiceContinuation(state), "auto_continue": true})
		if in.Action == "plan" || in.Action == "input" && in.Sessions != nil {
			s.beginVoiceWork(in.Sessions, in.ToolContracts)
		} else if in.Action == "finish" && len(state.ReadyPending()) > 0 {
			s.beginVoiceWork(nil)
		}
	})
}

func (s *Server) beginVoiceWork(sessions []copilotDecisionSession, contracts ...json.RawMessage) {
	settings, err := s.readCopilot()
	if err != nil || !settings.WorkEnabled {
		log.Printf("voice-work: stage=admission outcome=disabled")
		return
	}
	s.voiceWorkMu.Lock()
	s.voiceWorkWake++
	if sessions != nil {
		s.voiceWorkSessions = append([]copilotDecisionSession(nil), sessions...)
	}
	if len(contracts) > 0 && len(contracts[0]) > 0 {
		s.voiceWorkContracts = append(json.RawMessage(nil), contracts[0]...)
	}
	if s.voiceWorkRunning {
		log.Printf("voice-work: stage=admission outcome=wake_running")
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
				log.Printf("voice-work: stage=admission outcome=context_stopped")
				return
			}
			settings, err := s.readCopilot()
			if err != nil || !settings.WorkEnabled {
				log.Printf("voice-work: stage=admission outcome=disabled")
				return
			}
			s.voiceWorkMu.Lock()
			wake := s.voiceWorkWake
			sessions := append([]copilotDecisionSession(nil), s.voiceWorkSessions...)
			contracts := append(json.RawMessage(nil), s.voiceWorkContracts...)
			s.voiceWorkMu.Unlock()
			state, err := s.store.VoiceWork(ctx)
			if err != nil {
				log.Printf("voice-work: stage=store outcome=read_failed type=%T", err)
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
			exhausted := false
			for _, in := range pending {
				if in.PlanRounds >= voicework.MaxPlanRounds {
					exhausted = true
				}
			}
			if exhausted {
				_, _ = s.store.UpdateVoiceWork(ctx, func(next *voicework.State) error {
					next.FailPlanning(ids, "voice_work_continuation_limit", "admission")
					return nil
				})
				return
			}
			stale := false
			for _, input := range pending {
				if input.Focus == nil {
					continue
				}
				found := false
				for _, row := range sessions {
					if input.Focus.ID == row.ID && input.Focus.Machine == row.Machine && input.Focus.Conversation == row.Conversation {
						found = true
						break
					}
				}
				if !found {
					stale = true
					_, failureErr := s.store.UpdateVoiceWork(ctx, func(next *voicework.State) error {
						next.FailPlanning([]string{input.ID}, "voice_work_focus_changed", "admission")
						return nil
					})
					if failureErr != nil {
						log.Printf("voice-work: stage=store outcome=failure_not_saved type=%T", failureErr)
						return
					}
				}
			}
			if stale {
				log.Printf("voice-work: stage=admission outcome=focus_changed inputs=%v", ids)
				continue
			}
			raw, _ := json.Marshal(map[string]any{"auto_continue_limit": settings.Effective().WorkAutoContinue, "inputs": pending, "original_inputs": state.Inputs, "tasks": state.Tasks, "sessions": sessions, "tool_contracts": contracts, "language": s.productLanguage()})
			log.Printf("voice-work: stage=provider outcome=start inputs=%v", ids)
			stage := "provider"
			plan, err := voiceWorkPlan(ctx, string(raw), settings.Effective().WorkAssistant)
			if err == nil {
				stage = "validate"
			}
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
						delete(c.Args, "_voice_question")
						delete(c.Args, "expected_question")
						if wanted, ok := c.Args["session_id"].(string); ok && wanted != "" {
							for _, row := range sessions {
								if wanted == row.ID || wanted == row.Label {
									c.Args["session_id"] = row.ID
									c.Args["session_label"] = row.Label
									c.Args["_voice_target"] = map[string]any{"id": row.ID, "label": row.Label, "conversation": row.Conversation, "machine": row.Machine}
									if c.Tool == "answer_session_question" {
										choice, _ := c.Args["choice"].(string)
										matched := []copilotDecisionOption{}
										for _, option := range row.Options {
											if choice == strconv.Itoa(option.N) || strings.EqualFold(strings.TrimSpace(choice), option.Label) {
												matched = append(matched, option)
											}
										}
										if len(row.MenuFingerprint) != 64 || len(matched) != 1 {
											err = voicework.ErrInvalid
											break
										}
										c.Args["choice"] = strconv.Itoa(matched[0].N)
										c.Args["expected_question"] = row.MenuFingerprint
										c.Args["_voice_question"] = map[string]any{"question": row.Question, "option": matched[0].Label}
									}
									break
								}
							}
							if c.Args["_voice_target"] == nil {
								err = voicework.ErrInvalid
								break
							}
						}
						if err != nil {
							break
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
					for i, id := range plan.WaitFor {
						if n := names[id]; n != "" {
							plan.WaitFor[i] = n
						}
					}
					if err == nil {
						stage = "apply"
						_, err = s.store.UpdateVoiceWork(ctx, func(next *voicework.State) error {
							current, readErr := s.readCopilot()
							if readErr != nil || !current.WorkEnabled {
								return voicework.ErrConflict
							}
							return next.ApplyPlan(state.PlanVersion, ids, plan.Changes, plan.Reply, plan.WaitFor, time.Now().Unix())
						})
					}
				}
			}
			if err != nil {
				code := "voice_work_planning_failed"
				reason := "operation_failed"
				var failure *planner.VoiceWorkFailure
				if errors.As(err, &failure) {
					code = failure.Code
					stage = failure.Stage
					reason = failure.Reason
				}
				if errors.Is(err, voicework.ErrInvalid) {
					code = "voice_work_invalid_plan"
				}
				if errors.Is(err, context.DeadlineExceeded) {
					code = "voice_work_timeout"
				}
				if errors.Is(err, planner.ErrOutOfQuota) {
					code = "voice_work_quota"
				}
				if errors.Is(err, planner.ErrNoPlanner) {
					code = "voice_work_provider_missing"
				}
				if errors.Is(err, voicework.ErrConflict) {
					code = "voice_work_plan_changed"
				}
				log.Printf("voice-work: stage=%s outcome=failed inputs=%v code=%s reason=%s type=%T", stage, ids, code, reason, err)
				_, failureErr := s.store.UpdateVoiceWork(ctx, func(next *voicework.State) error { next.FailPlanning(ids, code, stage); return nil })
				if failureErr != nil {
					log.Printf("voice-work: stage=store outcome=failure_not_saved type=%T", failureErr)
					return
				}
				continue
			}
			log.Printf("voice-work: stage=apply outcome=planned inputs=%v changes=%d", ids, len(plan.Changes))
		}
	}()
}

func (s *Server) voiceContinuation(state voicework.State) *voicework.BatchOffer {
	settings, err := s.readCopilot()
	if err != nil || !settings.WorkEnabled {
		return nil
	}
	return state.Continuation(settings.Effective().WorkAutoContinue)
}
