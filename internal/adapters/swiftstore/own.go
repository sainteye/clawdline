package swiftstore

import "sort"

// Own is this daemon's own records, spelled as the Swift store's so that the
// projections in project.go — the Swift app's rules, restated once — read
// both (cutover B1).
//
// The broker's tables are the source now: a task this daemon dispatched, a
// wait registered here, a delivery a session reported here, a handoff or a
// Feature Root opened here. The Swift store is history laid beside them — it
// fills in what this daemon never held, and after the Swift app retires it is
// only ever older. One set of rules over both is the point: a second
// projection written for this daemon's records would be a second answer to
// "what is this session doing", and the two would drift.
//
// The caller converts its records and is responsible for the identities: a
// record is only given the process facts of a live session when this
// daemon's own evidence ties it to that session (the conversion says which).
// Nothing here invents one.
type Own struct {
	Tasks           []Task
	Waits           []CoordinationWait
	Deliveries      []SessionDelivery
	Handoffs        []Handoff
	HandoffLabels   []HandoffLabel
	RootAssignments []RootAssignment
}

// With lays own records over the snapshot and returns the merged one; the
// snapshot itself is not changed (its slices belong to a cached reading).
//
// A task id already in the Swift store is the Swift app's and is not
// repeated: the two brokers mint ids in different namespaces, so a collision
// is a record copied between them, and the Swift one is the original.
// Deliveries are ordered by when they were reported, because the projection
// takes the last one for a terminal and the newest receipt is the one that
// speaks for it now.
func (s Snapshot) With(own Own) Snapshot {
	out := s
	seen := make(map[string]bool, len(s.Tasks))
	for _, t := range s.Tasks {
		seen[t.ID] = true
	}
	tasks := append([]Task(nil), s.Tasks...)
	for _, t := range own.Tasks {
		if !seen[t.ID] {
			seen[t.ID] = true
			tasks = append(tasks, t)
		}
	}
	out.Tasks = tasks
	out.CoordinationWaits = append(append([]CoordinationWait(nil), s.CoordinationWaits...), own.Waits...)
	deliveries := append(append([]SessionDelivery(nil), s.SessionDeliveries...), own.Deliveries...)
	sort.SliceStable(deliveries, func(i, j int) bool { return deliveries[i].ReportedAt < deliveries[j].ReportedAt })
	out.SessionDeliveries = deliveries
	out.Handoffs = append(append([]Handoff(nil), s.Handoffs...), own.Handoffs...)
	out.HandoffLabels = append(append([]HandoffLabel(nil), s.HandoffLabels...), own.HandoffLabels...)
	out.RootAssignments = append(append([]RootAssignment(nil), s.RootAssignments...), own.RootAssignments...)
	return out
}

// ScreenTerminal is the one session on screen whose conversation is id, as
// Orchestrator.rootTerminalID finds a root: none, or more than one, is "".
func ScreenTerminal(screen []OnScreen, assistant, id string) string {
	if id == "" {
		return ""
	}
	if assistant == "" {
		assistant = "claude"
	}
	found, n := "", 0
	for _, s := range screen {
		if s.Assistant == assistant && s.ConversationID == id {
			found = s.TerminalID
			n++
		}
	}
	if n != 1 {
		return ""
	}
	return found
}
