package antigravitysession

import (
	"context"
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
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return 0
	}
	var rows []session.Session
	for _, c := range h.rows {
		c.mu.Lock()
		if c.interrupted {
			c.interrupted = false
			if c.saveState() == nil && !c.hidden {
				rows = append(rows, c.row)
			}
		}
		c.mu.Unlock()
	}
	h.jobs.Add(len(rows))
	h.mu.Unlock()
	for _, row := range rows {
		go func(row session.Session) {
			defer h.jobs.Done()
			if err := h.SendProtocol(ctx, row, ContinueText, nil); err != nil {
				log.Printf("antigravity: continue_after_restart_failed %s", errorCode(err))
			}
		}(row)
	}
	return len(rows)
}

func errorCode(err error) string {
	if e, ok := err.(*Error); ok {
		return e.Code
	}
	return "unknown"
}
