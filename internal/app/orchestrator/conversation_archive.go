package orchestrator

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/adapters/store"
)

// Archiving a scheduled Codex run's conversation (docs/schedules.md
// "Codex runs leave Codex's list").
//
// Every run of a schedule is a new conversation, and Codex lists each one as
// a chat of its own: an hourly schedule adds twenty-four a day to the list a
// person works from. A schedule with archive_codex_conversation set has its
// run's conversation archived in Codex once the broker has closed the run's
// tab (or seen that tab gone) by the schedule's close_tab. A tab left open —
// close_tab never, a timeout, a success under on_success's failure rule — is
// left in the list with it: an open tab is one a person may still be reading.
//
// The archive is an effect recorded in the same transaction that takes the
// linger, so a restart between the close and the archive still archives. It
// is not run by the pass that closes the tab. Codex writes the end of its
// rollout as it exits, and the close only asks it to; the effect is left to
// the beat's recovery, which runs a pending effect of this broker's once it
// has stood unheld for unattendedPending (thirty seconds), by which time the
// process has gone. Codex's thread/archive is answered again by finding the
// conversation already archived, so a second attempt after a crash is safe.

// EffectArchiveConversation archives one scheduled Codex run's conversation.
const EffectArchiveConversation = "child.conversation.archive"

type archiveConversationEffect struct {
	Assistant    string `json:"assistant"`
	Conversation string `json:"conversation"`
}

// archiveEffectFor is the archive a run's closed or vanished tab owes, if it
// owes one.
func archiveEffectFor(r Record) (store.Effect, bool) {
	if r.ID == "" || r.ScheduleID == "" || !r.ScheduleArchiveConversation ||
		r.Assistant != projects.AssistantCodex || r.ChildConversationID == "" ||
		r.ChildBackend == "owned" || !r.ChildConversationArchivedAt.IsZero() {
		return store.Effect{}, false
	}
	raw, _ := json.Marshal(archiveConversationEffect{Assistant: r.Assistant, Conversation: r.ChildConversationID})
	return store.Effect{Kind: EffectArchiveConversation, Subject: r.ID, Payload: raw}, true
}

func runArchiveConversation(ctx context.Context, b *Broker, e store.Effect) effectResult {
	var w archiveConversationEffect
	if err := json.Unmarshal(e.Payload, &w); err != nil || w.Conversation == "" {
		return effectResult{state: store.EffectFailed, outcome: "unreadable effect"}
	}
	payload := func(extra map[string]any) json.RawMessage {
		body := map[string]any{"task": e.Subject, "assistant": w.Assistant, "conversation": w.Conversation}
		for k, v := range extra {
			body[k] = v
		}
		out, _ := json.Marshal(body)
		return out
	}
	if b.ArchiveConversation == nil {
		return effectResult{state: store.EffectFailed, outcome: "this daemon cannot archive a conversation",
			events: []store.Event{{Kind: "task.child.conversation.archive_failed", Subject: e.Subject,
				Payload: payload(map[string]any{"error": "unsupported"})}}}
	}
	// The close it follows may not have happened: a tab still showing this
	// conversation is one Codex is running in, and it is left in the list.
	if r, _, err := b.Record(ctx, e.Subject); err == nil && r.ChildTerminalID != "" {
		if s, open := b.read(ctx).session(r.ChildTerminalID); open && s.ConversationID == w.Conversation {
			return effectResult{state: store.EffectFailed, outcome: "the run's tab is still open",
				events: []store.Event{{Kind: "task.child.conversation.archive_failed", Subject: e.Subject,
					Payload: payload(map[string]any{"error": "tab_open"})}}}
		}
	}
	if err := b.ArchiveConversation(ctx, w.Assistant, w.Conversation); err != nil {
		log.Printf("orchestrator: task %s: conversation %s was not archived: %v", e.Subject, w.Conversation, err)
		return effectResult{state: store.EffectFailed, outcome: err.Error(), err: err,
			events: []store.Event{{Kind: "task.child.conversation.archive_failed", Subject: e.Subject,
				Payload: payload(map[string]any{"error": err.Error()})}}}
	}
	at := b.now()
	if _, err := b.mutate(ctx, e.Subject, "task.child.conversation.archived", func(t *Record) error {
		if t.ChildConversationID != w.Conversation || !t.ChildConversationArchivedAt.IsZero() {
			return errUnchanged
		}
		t.ChildConversationArchivedAt = at
		return nil
	}); err != nil {
		// The conversation is archived; only the record of it is missing,
		// and the run's history reads the archive itself (schedule_resume.go).
		log.Printf("orchestrator: task %s: archive of %s not recorded: %v", e.Subject, w.Conversation, err)
	}
	return effectResult{state: store.EffectDone, outcome: "archived " + w.Conversation + " at " + at.UTC().Format(time.RFC3339)}
}
