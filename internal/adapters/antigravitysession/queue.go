package antigravitysession

import (
	"log"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// QueuedNotSent prefixes the transcript line that keeps the words of a queued
// message the conversation could not send, so the person can send them again.
const QueuedNotSent = "antigravity_queued_prompt_not_sent: "

const (
	queueRetries    = 60
	queueRetryDelay = time.Second
)

func retryable(err error) bool {
	code := errorCode(err)
	return code == "busy" || code == "unavailable"
}

func configCommand(text string) bool {
	return text == "/model" || strings.HasPrefix(text, "/model ") || text == "/effort" || strings.HasPrefix(text, "/effort ")
}

// enqueue accepts a message for after the running turn. The receipt means the
// machine saved it with the conversation, not that the model has read it; the
// user turn appears in the transcript when the queued prompt is written.
func (h *Host) enqueue(c *conversation, text string, pictures []string, before func() error) error {
	if len(text) > MaxQueuedPromptBytes {
		return refusal("text_too_large")
	}
	if before != nil {
		if err := before(); err != nil {
			return err
		}
	}
	h.mu.Lock()
	if h.closed || h.ctx.Err() != nil {
		h.mu.Unlock()
		return refusal("closed")
	}
	c.mu.Lock()
	if c.hidden {
		c.mu.Unlock()
		h.mu.Unlock()
		return refusal("closed")
	}
	if c.storeErr != nil {
		err := c.storeErr
		c.mu.Unlock()
		h.mu.Unlock()
		return err
	}
	bytes := len(text)
	for _, q := range c.queued {
		bytes += len(q)
	}
	if len(c.queued) >= MaxQueuedPrompts || bytes > MaxQueuedPromptBytes {
		c.mu.Unlock()
		h.mu.Unlock()
		return refusal("queue_full")
	}
	c.queuedPictures = alignedPictures(c.queuedPictures, len(c.queued))
	c.queued = append(c.queued, text)
	c.queuedPictures = append(c.queuedPictures, pictures)
	if err := c.saveState(); err != nil {
		c.queued = c.queued[:len(c.queued)-1]
		c.queuedPictures = c.queuedPictures[:len(c.queuedPictures)-1]
		c.mu.Unlock()
		h.mu.Unlock()
		return err
	}
	c.changed()
	// A queue left behind by a stopped or closed conversation has no running
	// turn to end and start it; the first new message does.
	idle := !c.busy && !c.draining
	if idle {
		h.jobs.Add(1)
	}
	c.mu.Unlock()
	h.mu.Unlock()
	if idle {
		go func() {
			defer h.jobs.Done()
			h.drainNext(c)
		}()
	}
	return nil
}

// afterTurn runs with c.mu held when a turn's job ends. An idle conversation
// sends its next queued message; any other ending keeps the words visible in
// the transcript instead of holding them where nothing will send them.
// A turn ended by the daemon shutting down keeps its queue for the next start.
func (c *conversation) afterTurn(stopping bool) bool {
	if len(c.queued) == 0 || c.draining || c.hidden || stopping {
		return false
	}
	if c.row.State == session.StateIdle {
		return true
	}
	c.dropQueued()
	return false
}

func (c *conversation) dropQueued() {
	pictures := alignedPictures(c.queuedPictures, len(c.queued))
	for i, text := range c.queued {
		c.append(contract.TranscriptEntry{Role: "error", Text: QueuedNotSent + text, ImageCount: int64(len(pictures[i])), At: time.Now().Unix()})
	}
	c.queued, c.queuedPictures = nil, nil
	_ = c.saveState()
}

// drainNext sends the oldest queued message as one ordinary prompt. It is
// removed from the saved queue in the same save that marks the prompt
// uncertain, immediately before the pipe write, so a crash never sends it
// twice. The next one is sent when this turn ends.
func (h *Host) drainNext(c *conversation) {
	c.mu.Lock()
	if c.draining || c.busy || len(c.queued) == 0 || c.hidden {
		c.mu.Unlock()
		return
	}
	c.draining = true
	c.queuedPictures = alignedPictures(c.queuedPictures, len(c.queued))
	text, pictures, row := c.queued[0], c.queuedPictures[0], c.row
	c.mu.Unlock()
	taken := false
	take := func() error {
		c.mu.Lock()
		defer c.mu.Unlock()
		if len(c.queued) > 0 && c.queued[0] == text {
			c.queued = c.queued[1:]
			c.queuedPictures = alignedPictures(c.queuedPictures, len(c.queued)+1)[1:]
			taken = true
		}
		return nil
	}
	var err error
	for attempt := 0; ; attempt++ {
		err = h.sendProtocol(h.ctx, row, text, pictures, take, true)
		// Another conversation holding the single opening slot, or this one's
		// previous turn still unwinding, is a wait, not a reason to drop.
		if err == nil || taken || attempt >= queueRetries || !retryable(err) {
			break
		}
		select {
		case <-h.ctx.Done():
		case <-time.After(queueRetryDelay):
		}
		if h.ctx.Err() != nil {
			break
		}
	}
	c.mu.Lock()
	c.draining = false
	if err == nil || taken {
		c.mu.Unlock()
		return
	}
	log.Printf("antigravity: queued_prompt_not_sent %s", errorCode(err))
	if h.ctx.Err() != nil {
		// Shutting down: the saved queue is sent after the next start.
		c.mu.Unlock()
		return
	}
	c.dropQueued()
	c.mu.Unlock()
}

// QueueUse answers the largest queue any conversation holds, in messages and
// in bytes of text. A message being sent leaves the saved queue just before
// its pipe write but reaches the transcript only on acceptance; it is still
// counted until then, so an empty queue means every accepted message is
// visible (CI 42346882753 read five of six inputs in that gap).
func (h *Host) QueueUse() (rows, bytes int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, c := range h.rows {
		c.mu.Lock()
		n := 0
		for _, q := range c.queued {
			n += len(q)
		}
		held := len(c.queued)
		if c.draining {
			held++
		}
		rows, bytes = max(rows, held), max(bytes, n)
		c.mu.Unlock()
	}
	return rows, bytes
}

// alignedPictures pads or trims saved picture lists to one per queued message.
// A record written before pictures were queued has none for any of them.
func alignedPictures(pictures [][]string, n int) [][]string {
	if len(pictures) > n {
		return pictures[:n]
	}
	for len(pictures) < n {
		pictures = append(pictures, nil)
	}
	return pictures
}

// savedQueuedPictures omits the field when no queued message has pictures.
func (c *conversation) savedQueuedPictures() [][]string {
	for _, p := range c.queuedPictures {
		if len(p) > 0 {
			return alignedPictures(c.queuedPictures, len(c.queued))
		}
	}
	return nil
}
