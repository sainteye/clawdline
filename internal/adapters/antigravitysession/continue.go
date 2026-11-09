package antigravitysession

import (
	"context"
	"errors"
	"log"

	"github.com/sainteye/clawdline/internal/domain/session"
)

// ContinueText is the message a Gemini conversation receives after the daemon
// restarted during its turn. A tmux Session keeps running across a restart;
// an owned ACP process is the daemon's child and is closed with it, so its turn
// ends as antigravity_process_cancelled and nothing woke it again
// (2026-11-07: a dev install stopped a Session that was waiting on a child).
// The prompt is never replayed: the conversation is told what happened and
// rechecks state itself before repeating anything. It is sent as one ordinary
// session/prompt, and the transparency page lists it (gemini-restart-continue).
const ContinueText = "[Clawdline] The Clawdline daemon restarted while your previous turn was still running, so that turn stopped before it finished. Continue the work from where you stopped. Re-check the state of anything you started or were waiting on before repeating a command."

// ContinueInterrupted wakes, once, each conversation whose turn the previous
// daemon stopped. The mark is cleared and saved before the message is sent,
// so a failed or repeated start never sends it twice. It answers how many
// conversations it started to wake; each send runs as a host job.
func (h *Host) ContinueInterrupted(ctx context.Context) int {
	// Recovery never creates new Grok work or drains inputs after an uncertain edit.
	if h.assistant() == session.AssistantGrok {
		return 0
	}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return 0
	}
	var rows []session.Session
	var queued []*conversation
	for _, c := range h.rows {
		c.mu.Lock()
		if c.interrupted {
			c.interrupted = false
			if c.saveState() == nil && !c.hidden {
				rows = append(rows, c.row)
			}
		} else if len(c.queued) > 0 && !c.hidden {
			// Messages accepted before the restart, with no turn left to end
			// and send them. An interrupted conversation sends its own after
			// the continuation's turn.
			queued = append(queued, c)
		}
		c.mu.Unlock()
	}
	if len(rows) == 0 && len(queued) == 0 {
		h.mu.Unlock()
		return 0
	}
	h.jobs.Add(1)
	h.continuing.Store(true)
	h.mu.Unlock()
	// One after another: resuming holds the host's single opening slot, so
	// resuming every conversation at once would refuse all but one of them,
	// and any open the person makes in the same moment.
	go func() {
		defer h.jobs.Done()
		defer h.continuing.Store(false)
		for _, row := range rows {
			if err := h.sendProtocol(ctx, row, ContinueText, nil, true); err != nil {
				log.Printf("antigravity: continue_after_restart_failed %s", errorCode(err))
			}
		}
		for _, c := range queued {
			h.drainNext(c)
		}
	}()
	return len(rows)
}

// Continuing reports whether the continuations ContinueInterrupted started
// are still being sent.
func (h *Host) Continuing() bool { return h.continuing.Load() }

func errorCode(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return "unknown"
}
