package http

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/swiftstore"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// ExecutionRefusal is a stable machine-side admission answer. Callers must
// branch on Code, never on the human-facing wording of an HTTP response.
type ExecutionRefusal struct{ Code string }

func (e *ExecutionRefusal) Error() string { return e.Code }

func (s *Server) executionMachine() string {
	if s.executionMachineID != "" {
		return s.executionMachineID
	}
	if line, ok := cloudLines.Load(s.cfg.Dir); ok {
		return line.(CloudLine).Status().MachineID
	}
	return ""
}

func executionFingerprint(item session.Session, live swiftstore.Live) string {
	if item.PID <= 0 || live.ProcessStart.IsZero() || !item.IsAssistant() {
		return ""
	}
	// The kernel's process start keeps an old PID from naming a new process.
	// A conversation id is deliberately excluded: binding can be discovered
	// later without changing the already observed execution.
	return fmt.Sprintf("%s:%d:%d", item.Assistant, item.PID, live.ProcessStart.UnixNano())
}

func currentExecution(item session.Session, inv session.Inventory) bool {
	if item.Observation.Freshness != "" {
		return item.Observation.Freshness == session.FreshnessCurrent
	}
	return sourceCompleteRow(inv, item)
}

// observeExecutions uses only current, kernel-pinned rows. The previous
// generation is withheld from the response whenever a source is unverified.
func (s *Server) observeExecutions(ctx context.Context, inv session.Inventory,
	items []session.Session, lives []swiftstore.Live) map[string]string {
	if s.store == nil || s.executionMachine() == "" {
		return nil
	}
	seen := make([]store.ExecutionSeen, 0, len(items))
	present := make(map[string]bool, len(items))
	for i, item := range items {
		present[item.ID] = true
		fp := executionFingerprint(item, lives[i])
		if fp == "" || !currentExecution(item, inv) {
			continue
		}
		seen = append(seen, store.ExecutionSeen{ID: item.ID, Source: rowSource(item), Fingerprint: fp})
	}
	// A scan the store already holds commits nothing, so it is not asked
	// (execution_memo.go). The memo is held across the store call.
	key, memoable := executionScanKey(s.executionMachine(), seen, present, inv)
	s.executions.mu.Lock()
	defer s.executions.mu.Unlock()
	if memoable {
		if generations, ok := s.executions.recall(key); ok {
			return generations
		}
	}
	generations, err := s.store.ObserveExecutions(ctx, s.executionMachine(), seen, func(id, source string) bool {
		if present[id] {
			return false
		}
		proved, _ := inv.ProvesAbsence(source)
		return proved
	})
	if err != nil {
		s.executions.remember("", false, nil)
		return nil
	}
	s.executions.remember(key, memoable, generations)
	return generations
}

// AdmitExecutionTarget checks a pinned target against a fresh terminal scan.
// It fails closed on missing Cloud identity, unknown process start, partial
// source reads, a replaced process, a missing database or a changed generation.
// Authorization and capability revocation must still be checked by the caller.
func (s *Server) AdmitExecutionTarget(ctx context.Context, machine, id, expected string) error {
	if machine == "" || id == "" || expected == "" {
		return &ExecutionRefusal{Code: "execution_target_required"}
	}
	if machine != s.executionMachine() {
		return &ExecutionRefusal{Code: "execution_machine_mismatch"}
	}
	if s.store == nil {
		return &ExecutionRefusal{Code: "execution_check_unavailable"}
	}
	readCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	inv := s.freshReading(readCtx)
	var found *session.Session
	var live swiftstore.Live
	items := make([]session.Session, 0, len(inv.Sessions))
	lives := make([]swiftstore.Live, 0, len(inv.Sessions))
	for _, item := range inv.Sessions {
		if !item.IsAssistant() {
			continue
		}
		items = append(items, item)
		itemLive := liveOf(item)
		lives = append(lives, itemLive)
		if item.ID != id {
			continue
		}
		if found != nil {
			return &ExecutionRefusal{Code: "execution_source_unknown"}
		}
		copy := item
		found = &copy
		live = itemLive
	}
	if found == nil {
		proved, _ := inv.ProvesAbsence(session.SourceForID(id))
		if proved {
			return &ExecutionRefusal{Code: "execution_target_missing"}
		}
		return &ExecutionRefusal{Code: "execution_source_unknown"}
	}
	if !currentExecution(*found, inv) || executionFingerprint(*found, live) == "" {
		return &ExecutionRefusal{Code: "execution_source_unknown"}
	}
	seen := s.observeExecutions(readCtx, inv, items, lives)
	if seen[id] == "" {
		return &ExecutionRefusal{Code: "execution_check_unavailable"}
	}
	if seen[id] != expected {
		return &ExecutionRefusal{Code: "execution_generation_changed"}
	}
	if err := s.store.AdmitExecution(readCtx, machine, id, expected); err != nil {
		for _, known := range []error{store.ErrExecutionTargetMissing, store.ErrExecutionGenerationChanged, store.ErrExecutionTargetRequired} {
			if errors.Is(err, known) {
				return &ExecutionRefusal{Code: known.Error()}
			}
		}
		if errors.Is(err, store.ErrExecutionsFull) {
			return &ExecutionRefusal{Code: "execution_records_full"}
		}
		return &ExecutionRefusal{Code: "execution_check_unavailable"}
	}
	return nil
}

// admitPinnedSessionRequest is the per-session HTTP admission for callers
// carrying the new target contract. Both headers must be present together.
// Existing clients without either header keep their legacy behavior.
func (s *Server) admitPinnedSessionRequest(w http.ResponseWriter, r *http.Request) bool {
	path := strings.TrimPrefix(routePath(r), "/v1/sessions/")
	segment, _, _ := strings.Cut(path, "/")
	id := decodeSegment(segment)
	return s.admitPinnedTarget(w, r, id)
}

func (s *Server) admitPinnedTarget(w http.ResponseWriter, r *http.Request, id string) bool {
	machine := r.Header.Get("X-Clawdline-Target-Machine")
	generation := r.Header.Get("X-Clawdline-Execution-Generation")
	if machine == "" && generation == "" {
		return true
	}
	err := s.AdmitExecutionTarget(r.Context(), machine, id, generation)
	if err == nil {
		return true
	}
	var refusal *ExecutionRefusal
	if !errors.As(err, &refusal) {
		refusal = &ExecutionRefusal{Code: "execution_check_unavailable"}
	}
	status := http.StatusConflict
	switch refusal.Code {
	case "execution_target_required":
		status = http.StatusBadRequest
	case "execution_target_missing":
		status = http.StatusNotFound
	case "execution_source_unknown", "execution_check_unavailable", "execution_records_full":
		status = http.StatusServiceUnavailable
	}
	writeRefusal(w, status, refusal.Code, "The selected Session execution could not be confirmed on this machine.")
	return false
}
