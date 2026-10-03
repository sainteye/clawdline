package swiftstore

import (
	"testing"
	"time"
)

// tmux renumbers pane ids after a restart, so a finished task's terminal id
// can come back on a new session. A task whose recorded process contradicts
// the session now at that terminal lends it no title; one whose recorded
// process is that session still does.
func TestAReusedPaneIdLendsNoOldTaskTitle(t *testing.T) {
	pid, start := int64(70), Seconds(1000)
	old := Task{ID: "old-task", State: "success", Assistant: "claude", Title: "Old task title",
		ChildTerminal: strp("%7"), ChildPID: &pid, ChildProcStart: &start, ChildSession: strp("old-conversation")}
	snap := Snapshot{Orchestrator: Orchestrator{Tasks: []Task{old}}}

	newRoot := Live{TerminalID: "%7", TTY: "ttys007", Assistant: "claude", PID: 71,
		ProcessStart: time.Unix(2000, 0), ConversationID: "new-conversation"}
	if got := snap.TitleOf(newRoot, "", []Live{newRoot}).Orchestrator; got == "Old task title" {
		t.Fatalf("new session at reused pane %%7 was lent the old task title %q", got)
	}

	// Control: the task's own process at that terminal keeps its title.
	ownChild := Live{TerminalID: "%7", TTY: "ttys007", Assistant: "claude", PID: pid,
		ProcessStart: time.Unix(1000, 0), ConversationID: "old-conversation"}
	if got := snap.TitleOf(ownChild, "", []Live{ownChild}).Orchestrator; got != "Old task title" {
		t.Fatalf("control: the task's own child shows %q, want its title", got)
	}
}
