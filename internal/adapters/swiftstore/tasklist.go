package swiftstore

import (
	"github.com/sainteye/clawdline/internal/contract"
)

// OnScreen is one session on screen, for resolving a task's root terminal.
type OnScreen struct {
	TerminalID     string
	Assistant      string
	ConversationID string
}

// rootTerminal is Orchestrator.rootTerminalID: the parent task's child tab
// first, otherwise the one session on screen whose conversation is the root's.
func rootTerminal(t Task, byID map[string]*Task, screen []OnScreen) string {
	if t.ParentTask != nil {
		if parent, ok := byID[*t.ParentTask]; ok && parent.ChildTerminal != nil {
			return *parent.ChildTerminal
		}
	}
	if t.RootSession == nil {
		return ""
	}
	assistant := "claude"
	if t.RootAssistant != nil {
		assistant = *t.RootAssistant
	}
	found := ""
	n := 0
	for _, s := range screen {
		if s.Assistant != assistant || s.ConversationID == "" || s.ConversationID != *t.RootSession {
			continue
		}
		found = s.TerminalID
		n++
	}
	if n != 1 {
		return ""
	}
	return found
}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func unixOf(p *Seconds) int64 {
	if p == nil {
		return 0
	}
	return p.Unix()
}

// taskRow is Orchestrator.shape with RemoteServer.taskListProjection's
// deny-list applied, reduced to the keys the schema carries.
func taskRow(t Task, byID map[string]*Task, screen []OnScreen) contract.TaskRow {
	claims := t.Claims
	if claims == nil {
		claims = []string{}
	}
	row := contract.TaskRow{
		ID:               t.ID,
		TaskID:           t.ID,
		State:            contract.TaskState(t.State),
		Kind:             t.Kind,
		Title:            t.Title,
		Assistant:        contract.Assistant(t.Assistant),
		ProjectDir:       t.ProjectDir,
		Created:          t.Created.Unix(),
		CreatedAt:        t.Created.Unix(),
		Depth:            t.Depth,
		Dir:              "/tmp/.clawdline/" + t.ID,
		Model:            str(t.Model),
		WorkItemID:       str(t.WorkItemID),
		WorkPhase:        str(t.WorkPhase),
		ReasoningEffort:  str(t.ReasoningEffort),
		ScheduleID:       str(t.ScheduleID),
		SessionRoot:      t.SessionRoot,
		Permission:       t.Permission,
		SpawnedAt:        unixOf(t.SpawnedAt),
		BriefedAt:        unixOf(t.BriefedAt),
		FinishedAt:       unixOf(t.FinishedAt),
		ResultVerifiedAt: unixOf(t.ResultVerifiedAt),
		Claims:           claims,
		ClaimsDeclared:   t.Claims != nil,
		UntouchedClaims:  t.UntouchedClaims,
	}
	if len(t.Artifacts) > 0 {
		row.Artifacts = t.Artifacts
	}
	root := contract.TaskRoot{
		SessionID:  str(t.RootSession),
		Assistant:  str(t.RootAssistant),
		Label:      str(t.RootLabel),
		TerminalID: rootTerminal(t, byID, screen),
		TaskID:     str(t.ParentTask),
	}
	if root != (contract.TaskRoot{}) {
		row.Root = &root
	}
	child := contract.TaskChild{
		TerminalID: str(t.ChildTerminal),
		Backend:    str(t.ChildBackend),
		SessionID:  str(t.ChildSession),
	}
	if child != (contract.TaskChild{}) {
		row.Child = &child
	}
	if t.AttachSession != nil {
		row.Attached = true
		row.AttachSession = *t.AttachSession
	}
	if t.Worktree != nil {
		row.Isolation = "worktree"
	}
	if u := t.Usage; u != nil {
		usage := contract.TaskUsage{
			Input: u.Input, Output: u.Output, CacheRead: u.CacheRead, CacheWrite: u.CacheWrite,
			Total: u.Total, Model: str(u.Model),
		}
		if u.CostUSD != nil {
			usage.CostUsd = *u.CostUSD
		}
		row.Usage = &usage
	}
	return row
}

// TaskPage is OrchestratorTaskList.body: every unfinished task, then the page
// of finished ones starting at cursor, newest first. `extra` are tasks from
// another source (this daemon's own), placed by the same rule; an id already
// in the store is not repeated.
func (s Snapshot) TaskPage(screen []OnScreen, extra []contract.TaskRow, cursor, limit int) ([]contract.TaskRow, contract.TaskPage) {
	if cursor < 0 {
		cursor = 0
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	byID := make(map[string]*Task, len(s.Tasks))
	for i := range s.Tasks {
		byID[s.Tasks[i].ID] = &s.Tasks[i]
	}
	all := make([]contract.TaskRow, 0, len(s.Tasks)+len(extra))
	for _, t := range tasksNewestFirst(s.Tasks) {
		all = append(all, taskRow(t, byID, screen))
	}
	for _, e := range extra {
		if _, dup := byID[e.ID]; dup {
			continue
		}
		all = append(all, e)
	}
	sortRowsNewestFirst(all)

	live := []contract.TaskRow{}
	finished := []contract.TaskRow{}
	for _, r := range all {
		if isTerminalState(string(r.State)) {
			finished = append(finished, r)
		} else {
			live = append(live, r)
		}
	}
	start := cursor
	if start > len(finished) {
		start = len(finished)
	}
	end := start + limit
	if end > len(finished) {
		end = len(finished)
	}
	page := contract.TaskPage{
		Cursor:     int64(start),
		Limit:      int64(limit),
		Fields:     "list",
		Unfinished: int64(len(live)),
		Finished:   int64(len(finished)),
	}
	if end < len(finished) {
		page.NextCursor = int64(end)
	}
	return append(live, finished[start:end]...), page
}

func sortRowsNewestFirst(rows []contract.TaskRow) {
	// Insertion order is already newest first for the store's rows; the extra
	// rows are merged in by the same key.
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0 && newer(rows[j], rows[j-1]); j-- {
			rows[j], rows[j-1] = rows[j-1], rows[j]
		}
	}
}

// newer is strict, so the insertion sort above is stable: two rows in the
// same second keep the order the store's fractional times gave them.
func newer(a, b contract.TaskRow) bool { return a.Created > b.Created }
