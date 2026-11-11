package app

import (
	"context"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
)

// Runs are conversations too (docs/schedules.md): a finished scheduled run
// whose tab has gone is resumed as an ordinary Session in the schedule's
// project, by the conversation its child had. Until 2026-10-15 this daemon
// recorded no such conversation, so every run in the history sheet was a
// row that could not be pressed.
//
// The conversation is known two ways. A run the broker watched since then
// carries the id a reading bound in the child's tab
// (Record.ChildConversationID). An older Claude run is matched by its
// transcript, whose first message is the line the broker typed into that tab
// and names the task (projects.ChildFirstLinePrefix). Either way the
// transcript has to still be on disk, and the run has to have worked in the
// project directory itself: a run in a worktree of its own had a conversation
// the project directory's resume cannot reach.

// RunConversations is how a schedule's runs are matched to the conversations
// their children had. Each nil field answers nothing.
type RunConversations struct {
	// Find is projects.ClaudeChildConversations.
	Find func(projectDir string, since time.Time, want map[string]bool) map[string]string
	// TaskOf is projects.ClaudeChildTask.
	TaskOf func(projectDir, id string) (string, bool)
	// Recorded is whether the assistant's transcript of id is on disk for
	// projectDir (projects.Recorded).
	Recorded func(projectDir, assistant, id string) bool
	// Archived is whether the assistant holds id archived (projects
	// .CodexArchived): a scheduled Codex run whose conversation the broker
	// archived is still a run that can be resumed, once it is unarchived.
	Archived func(assistant, id string) bool
}

// present is whether a run's conversation is on disk, in the assistant's
// list or in its archive.
func (c RunConversations) present(projectDir, assistant, id string) bool {
	if c.Recorded != nil && c.Recorded(projectDir, assistant, id) {
		return true
	}
	return c.Archived != nil && c.Archived(assistant, id)
}

// resumable is whether a run could be resumed at all: it has ended, and its
// child worked in the project directory.
func resumable(r store.ScheduleRun) bool {
	return orchestrator.State(r.State).Terminal() && r.ProjectDir != "" && r.Assistant != "" &&
		(r.Isolation == "" || r.Isolation == orchestrator.IsolationNone)
}

// runConversations answers task id → conversation id for the runs that can
// be resumed now.
func (b *ScheduleBook) runConversations(runs []store.ScheduleRun) map[string]string {
	out := map[string]string{}
	c := b.Conversations
	if c.Recorded == nil {
		return out
	}
	type lookup struct {
		since time.Time
		want  map[string]bool
	}
	missing := map[string]*lookup{}
	for _, r := range runs {
		if !resumable(r) {
			continue
		}
		if r.ConversationID != "" {
			if c.present(r.ProjectDir, r.Assistant, r.ConversationID) {
				out[r.TaskID] = r.ConversationID
			}
			continue
		}
		if r.Assistant != "claude" {
			continue
		}
		l := missing[r.ProjectDir]
		if l == nil {
			l = &lookup{since: r.Created, want: map[string]bool{}}
			missing[r.ProjectDir] = l
		}
		if r.Created.Before(l.since) {
			l.since = r.Created
		}
		l.want[r.TaskID] = true
	}
	if c.Find == nil {
		return out
	}
	for dir, l := range missing {
		for task, id := range c.Find(dir, l.since, l.want) {
			out[task] = id
		}
	}
	return out
}

// ScheduledConversation is whether id is the conversation of a finished
// scheduled run that worked in projectDir with assistant — the second source
// the place resume route admits a conversation from, beside the project's
// own history (docs/schedules.md "Runs are conversations too").
func (b *ScheduleBook) ScheduledConversation(ctx context.Context, projectDir, assistant, id string) bool {
	if b == nil || b.Store == nil {
		return false
	}
	c := b.Conversations
	if c.Recorded == nil || id == "" || !c.present(projectDir, assistant, id) {
		return false
	}
	runs, err := b.Store.ScheduleRuns(ctx, "")
	if err != nil {
		return false
	}
	task := ""
	if assistant == "claude" && c.TaskOf != nil {
		task, _ = c.TaskOf(projectDir, id)
	}
	for _, r := range runs {
		if !resumable(r) || r.ProjectDir != projectDir || r.Assistant != assistant {
			continue
		}
		if r.ConversationID == id || (task != "" && r.TaskID == task && r.ConversationID == "") {
			return true
		}
	}
	return false
}
