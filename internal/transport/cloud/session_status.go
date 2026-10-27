package cloud

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"

	"github.com/sainteye/clawdline/internal/app/cloudops"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// SessionNoMovementSecondsLimit is the fleet-list policy for recent record
// movement. It says nothing about the quality or completion of the work.
const SessionNoMovementSecondsLimit = 1800

// SessionStatus is the complete allowlist for ss/. Neither its type nor the
// constructor has a field for transcript, screen, menu, shell, Git, labels,
// working directory, or any text derived from a session's content.
type SessionStatus struct {
	MachineID           string `json:"machine_id"`
	SessionID           string `json:"session_id"`
	ExecutionGeneration string `json:"execution_generation,omitempty"`
	SnapshotGeneration  string `json:"snapshot_generation"`
	Assistant           string `json:"assistant,omitempty"`
	Backend             string `json:"backend,omitempty"`
	ParentSessionID     string `json:"parent_session_id,omitempty"`
	MachineScope        bool   `json:"machine_scope,omitempty"`
	CoordinatorBound    bool   `json:"coordinator_bound,omitempty"`
	State               string `json:"state"`
	Source              struct {
		Provenance string `json:"provenance"`
		ObservedAt int64  `json:"observed_at"`
		Freshness  string `json:"freshness"`
	} `json:"source"`
	InventoryComplete    bool   `json:"inventory_complete"`
	ProjectedAt          int64  `json:"projected_at"`
	LastMovementAt       int64  `json:"last_movement_at,omitempty"`
	NoProgressAfterMS    int64  `json:"no_progress_after_ms"`
	NoMovement           *bool  `json:"no_movement,omitempty"`
	WaitingForReply      bool   `json:"waiting_for_reply,omitempty"`
	CompletedUnconfirmed bool   `json:"completed_unconfirmed,omitempty"`
	AttentionRequired    bool   `json:"attention_required,omitempty"`
	ProviderBlock        string `json:"provider_block,omitempty"`
	CloseBlocked         *bool  `json:"close_blocked,omitempty"`
	FailedAgentCount     *int64 `json:"failed_agent_count,omitempty"`
	RunningAgentCount    *int64 `json:"running_agent_count,omitempty"`
	AgentCountUnknown    bool   `json:"agent_count_unknown,omitempty"`
}

// ProjectSessionStatus copies scalar facts from a full local row, never the
// row or one of its nested content objects. An unknown carried row has no
// generation, so a status viewer cannot mistake it for an actionable target.
func ProjectSessionStatus(machine, id string, row map[string]any, complete bool, at int64) SessionStatus {
	out := SessionStatus{MachineID: machine, SessionID: id, State: "unknown", InventoryComplete: complete,
		ProjectedAt: at, NoProgressAfterMS: SessionNoMovementSecondsLimit * 1000}
	out.Source.Freshness = "missing"
	if row == nil {
		out.InventoryComplete = false
		return out
	}
	if v, _ := row["execution_generation"].(string); len(v) == 32 && strings.Trim(v, "0123456789abcdef") == "" {
		out.ExecutionGeneration = v
	}
	if v, _ := row["assistant"].(string); v == "claude" || v == "codex" || v == "antigravity" {
		out.Assistant = v
	}
	if v, _ := row["backend"].(string); v == "tmux" || v == "iterm" || v == "ps" || v == "owned" {
		out.Backend = v
	}
	out.MachineScope, _ = row["machine_scope"].(bool)
	_, out.CoordinatorBound = row["coordinator"].(map[string]any)
	if v, _ := row["state"].(string); v == "working" || v == "waiting" || v == "idle" || v == "unknown" {
		out.State = v
	}
	if source, ok := row["source"].(map[string]any); ok {
		if v, _ := source["provenance"].(string); v == "tmux" || v == "iterm" || v == "ps" || v == "owned" {
			out.Source.Provenance = v
		}
		if v, _ := source["observed_at"].(float64); v > 0 {
			out.Source.ObservedAt = int64(v)
		}
		if v, _ := source["freshness"].(string); v == "current" || v == "unverified" || v == "missing" {
			out.Source.Freshness = v
		}
	}
	if activity, ok := row["activity"].(map[string]any); ok && activity["known"] == true {
		if v, ok := activity["at"].(float64); ok && v > 0 {
			out.LastMovementAt = int64(v)
		}
	}
	if out.Source.Freshness == "current" && out.LastMovementAt > 0 && at >= out.LastMovementAt {
		quiet := at-out.LastMovementAt >= SessionNoMovementSecondsLimit
		out.NoMovement = &quiet
	}
	if out.Source.Freshness != "current" {
		out.ExecutionGeneration = ""
		return out
	}
	// A waiting state alone can be an unrecognised dialog. A reply signal
	// needs the question's options and the evidence that named their source;
	// none of their words enter this status projection.
	if out.State == "waiting" {
		if menu, ok := row["menu"].(map[string]any); ok {
			if options, ok := menu["options"].([]any); ok && len(options) > 0 {
				screen, _ := row["screen_reading"].(string)
				menuSource, _ := menu["source"].(string)
				revision, _ := menu["revision"].(string)
				native := row["backend"] == "owned" && row["identity"] == "protocol" && revision != "" && menuSource == ""
				out.WaitingForReply = screen == "read" && menuSource == "" ||
					screen == "unavailable" && menuSource == "transcript" || native
			}
		}
	}
	if acceptance, ok := row["acceptance"].(map[string]any); ok && acceptance["state"] == "pending" && acceptance["phase"] == "done" {
		out.CompletedUnconfirmed = true
	}
	if row["work_person_needed"] == true {
		out.AttentionRequired = true
	}
	if row["provider_block"] == string(session.ProviderBlockModelCapacity) {
		out.ProviderBlock = string(session.ProviderBlockModelCapacity)
		out.AttentionRequired = true
	}
	if count, ok := row["attention_count"].(float64); ok && count > 0 {
		out.AttentionRequired = true
	}
	if closeability, ok := row["closeability"].(map[string]any); ok {
		if source, ok := closeability["source"].(map[string]any); ok && source["freshness"] == "current" {
			if state, _ := closeability["state"].(string); state == "blocked" || state == "safe" || state == "needs_attestation" {
				blocked := state == "blocked"
				out.CloseBlocked = &blocked
			}
		}
	}
	// Count the already-read provider rows, without another provider read or
	// transcript subscription. A partial reading cannot claim an exact total.
	reading, _ := row["agents_reading"].(map[string]any)
	agents, _ := row["agents"].([]any)
	truncated, _ := reading["truncated"].(float64)
	known := reading["state"] == "complete" && truncated == 0
	running, failed := int64(0), int64(0)
	for _, agent := range agents {
		info, ok := agent.(map[string]any)
		if !ok {
			known = false
			continue
		}
		switch info["state"] {
		case "running":
			running++
		case "failed":
			failed++
		case "done":
		default:
			known = false
		}
	}
	if reading["state"] == "complete" && truncated == 0 {
		out.FailedAgentCount = &failed
	}
	if known {
		out.RunningAgentCount = &running
	} else {
		out.AgentCountUnknown = len(agents) > 0 || truncated > 0
	}

	return out
}

// statusRowKey is where the publisher remembers one status row. It is
// namespaced because the same Session's full s/ row is remembered under the
// bare terminal id (publishSessions), and the two must not evict each other.
func statusRowKey(id string) string { return "ss/" + id }

// statusMarkerKey is where the status inventory marker is remembered.
var statusMarkerKey = statusRowKey(InventorySessionID)

// statusGeneration is a 128-bit id derived from what it names, so the same
// answer states the same id every pass.
//
// The pass id used to be a fresh random value, and every row carried it, so a
// pass had to restate the whole set: one row's clock moving re-sent nine rows
// that said exactly what the viewer already held (266 status frames per
// Session-hour in the replay below). Derived, it changes only when the thing
// it names changes, which is what lets a row be left alone.
func statusGeneration(value any) string {
	sum := sha256.Sum256(mustJSON(value))
	return hex.EncodeToString(sum[:16])
}

// publishStatuses uses its own failure lane: an older relay that does not yet
// accept ss/ must not prevent the existing s/ inventory marker from moving.
func (p *Publisher) publishStatuses(ctx context.Context, reading sessionReading, ids []string) {
	if p.Publish == nil {
		return
	}
	// A partial source scan cannot replace the last complete ss/ set. Keep its
	// original observation time so the viewer eventually marks it stale; a
	// later authoritative pass will publish a fresh set and removal barrier.
	if !reading.whole() {
		return
	}
	rows := make(map[string]map[string]any, len(reading.sessions))
	for _, row := range reading.sessions {
		if id, _ := row["id"].(string); id != "" {
			rows[id] = row
		}
	}
	var at int64
	_ = json.Unmarshal(reading.at, &at)
	statuses := make([]SessionStatus, 0, len(ids))
	executions := make([]string, 0, len(ids))
	parentIDs := make([]string, 0, len(ids))
	// The viewer fetches one pinned list per machine when the presentation id
	// changes. Include the original row's display facts in that id, but never
	// in ss/: a line or shell update must wake the list without disclosing its
	// words to a read_sessions-only viewer.
	displays := make([]map[string]any, 0, len(ids))
	steadyDisplays := make([]map[string]any, 0, len(ids))
	// The descriptor's task projection omits root.sessionId, so the rows
	// read from the route are used when there are any: their record still
	// names the owning conversation, which resolves a root the screen scan
	// at read time could not (taskRootIn). A root resolved on an earlier
	// pass and still listed, under the task's own conversation, is kept for
	// the pass where neither can say (carriedRootIn).
	tasks := p.taskRows
	if tasks == nil {
		tasks = p.tasks
	}
	parents, causes := statusParents(rows, tasks, p.taskRoots)
	p.noteChildParents(rows, parents, causes)
	for _, id := range ids {
		row := rows[id]
		status := ProjectSessionStatus(p.MachineID, id, row, true, at)
		status.ParentSessionID = parents[id]
		statuses = append(statuses, status)
		executions = append(executions, status.ExecutionGeneration)
		parentIDs = append(parentIDs, status.ParentSessionID)
		// The working clock is not news here either (withoutFreshness); the
		// token count waits for the window with the rest of the volatile
		// fields.
		line, _ := row["line"].(string)
		if row["state"] == "working" {
			line = session.WithoutElapsed(line)
		}
		display := map[string]any{
			"line": line, "persona": row["persona"], "work_state": row["work_state"],
			"work_note": row["work_note"], "work_provenance": row["work_provenance"],
			"work_moved_by": row["work_moved_by"], "work_person_needed": row["work_person_needed"],
			"shells": row["shells"], "heavy_work": row["heavy_work"],
			"background_reading": row["background_reading"], "background_omitted": row["background_omitted"],
			"attention_count": row["attention_count"], "owed": row["owed"],
			"acceptance": row["acceptance"], "disposition": row["disposition"],
			"coordination": row["coordination"],
			// What the list titles the row by. A new child's label and
			// Project can settle a pass after the Session first appears;
			// without them here the list never learned to read its title
			// again and kept drawing "Claude Session" with no icon.
			"label": row["label"], "cwd": row["cwd"], "icon": row["icon"],
			// The Project the row would be accepted into moves with its cwd
			// (or with a repository appearing around it), and the Board's
			// assignment dialog picks by it.
			"project_key": row["project_key"],
			// What a waiting card draws. Its line names the question, the
			// rows and the caret but not a multi-select's ticks, so a box
			// ticked from the phone never woke the list and the card kept
			// it empty. Hashed into the id here, never sent on ss/.
			"menu": row["menu"],
		}
		displays = append(displays, display)
		steadyDisplays = append(steadyDisplays, volatileFree(display))
	}
	// The pass id the rows and the marker share. It names the set: which
	// Sessions are listed, which execution each row speaks for, and their
	// visual ancestry. The task row that links a newly opened child can arrive
	// one pass after the Session itself. Giving that relationship a new pass
	// makes the corrected row and its marker one complete receipt instead of
	// leaving a viewer on the first, flat projection. While all three stand
	// still an unchanged row that the relay still retains is exactly what the
	// newest marker means, so it is not re-sent; a closed Session, restarted
	// execution, or changed parent republishes every row before the marker.
	pass := statusGeneration(struct {
		IDs        []string `json:"ids"`
		Executions []string `json:"executions"`
		Parents    []string `json:"parents"`
	}{IDs: ids, Executions: executions, Parents: parentIDs})
	presentation := statusGeneration(displays)
	// A row of a Session that has left the set is forgotten, so the id that
	// comes back is stated again rather than left to whatever the relay still
	// retains under it.
	if p.statusRows == nil {
		p.statusRows = map[string]bool{}
	}
	listed := make(map[string]bool, len(ids))
	for _, id := range ids {
		listed[id] = true
	}
	for id := range p.statusRows {
		if !listed[id] {
			p.forget(statusRowKey(id))
			delete(p.statusRows, id)
		}
	}
	for i, id := range ids {
		status := statuses[i]
		status.SnapshotGeneration = pass
		p.statusRows[id] = true
		// The pass id is part of each row's identity, so a set that changed
		// republishes every row without being asked to.
		identity := status
		identity.ProjectedAt = 0
		identity.Source.ObservedAt = 0
		steady := identity
		steady.LastMovementAt = 0
		// Counts share the existing provider-work coalescing window.
		steady.RunningAgentCount = nil
		steady.AgentCountUnknown = false
		if !p.changedCoalesced(statusRowKey(id), mustJSON(identity), mustJSON(steady)) {
			continue
		}
		body, err := json.Marshal(status)
		if err != nil {
			p.forget(statusRowKey(id))
			return
		}
		channel := "ss/" + cloudops.ChannelSegment(p.MachineID) + "/" + cloudops.ChannelSegment(id)
		if err := p.Publish(ctx, Outbound{Channel: channel, Class: string(domaincloud.ClassStream), Payload: body}); err != nil {
			p.forget(statusRowKey(id))
			p.logf("cloud: the status row was not published: %v", err)
			return
		}
	}
	// A status-only viewer cannot read the old s/ inventory after the relay
	// tightens its capability. Publish the same id barrier on ss/ after rows.
	marker := struct {
		Inventory struct {
			Version  int      `json:"version"`
			Sessions []string `json:"sessions"`
		} `json:"inventory"`
		At                     int64  `json:"at"`
		Complete               bool   `json:"complete"`
		SnapshotGeneration     string `json:"snapshot_generation"`
		PresentationGeneration string `json:"presentation_generation"`
	}{At: at, Complete: true, SnapshotGeneration: pass, PresentationGeneration: presentation}
	marker.Inventory.Version = 1
	marker.Inventory.Sessions = append([]string{}, ids...)
	// `at` is left out of the marker's identity: a pass where only the clock
	// moved says nothing, and the heartbeat restates it before any viewer's
	// freshness window closes. The presentation id is the volatile half, so a
	// token count alone waits for the row window while a line that says
	// something new wakes the list at once.
	type markerIdentity struct {
		IDs          []string `json:"ids"`
		Pass         string   `json:"pass"`
		Presentation string   `json:"presentation"`
		Complete     bool     `json:"complete"`
	}
	if !p.changedCoalesced(statusMarkerKey,
		mustJSON(markerIdentity{IDs: ids, Pass: pass, Presentation: presentation, Complete: true}),
		mustJSON(markerIdentity{IDs: ids, Pass: pass, Presentation: statusGeneration(steadyDisplays), Complete: true})) {
		// Unchanged and still retained: the viewer's marker already names it.
		p.statusPass = pass
		return
	}
	body, err := json.Marshal(marker)
	if err != nil {
		p.forget(statusMarkerKey)
		return
	}
	channel := "ss/" + cloudops.ChannelSegment(p.MachineID) + "/" + InventorySessionID
	if err := p.Publish(ctx, Outbound{Channel: channel, Class: string(domaincloud.ClassStream), Payload: body}); err != nil {
		p.forget(statusMarkerKey)
		p.logf("cloud: the status inventory was not published: %v", err)
		return
	}
	p.statusPass = pass
}

// statusParents projects only terminal ancestry. The signed orch task list
// already resolves dispatch roots; Epic roots use the same unique conversation
// match as the local Session list. No task title or conversation text enters ss/.
//
// kept is each task's root as resolveTaskRoots last placed it, by task id,
// used when this reading cannot place it itself. It also answers, for each
// listed child no task could place, why the first task naming it could not; a
// task with no session root (a scheduled run) is not a missing parent.
func statusParents(rows map[string]map[string]any, tasks []map[string]any, kept map[string]string) (map[string]string, map[string]string) {
	parents := make(map[string]string)
	causes := make(map[string]string)
	conversations := conversationOwners(rows)
	for _, task := range tasks {
		child, _ := task["child"].(map[string]any)
		kid, _ := child["terminalId"].(string)
		if kid == "" || rows[kid] == nil || rows[kid]["machine_scope"] == true {
			continue
		}
		owner, cause := taskRootIn(task, rows, conversations)
		if owner == "" {
			owner = carriedRootIn(task, kept, rows)
		}
		if owner == kid {
			owner, cause = "", rootCauseIsTheChild
		}
		// A later task naming the same child places it over an earlier one,
		// as the local Session list's own walk does.
		if owner != "" {
			parents[kid] = owner
			continue
		}
		if _, noted := causes[kid]; !noted && cause != rootCauseNoSession {
			causes[kid] = cause
		}
	}
	for kid := range parents {
		delete(causes, kid)
	}
	for id, row := range rows {
		if parents[id] != "" || row["machine_scope"] == true || row["coordinator"] != nil {
			continue
		}
		epic, _ := row["epic_parent"].(map[string]any)
		conversation, _ := epic["owner_session_id"].(string)
		if owner := conversations[conversation]; conversation != "" && owner != "" && owner != id {
			parents[id] = owner
		}
	}
	return parents, causes
}

// noteChildParents logs a dispatched child a viewer will draw without its
// root, once per change of cause, and once more when it is placed again — so
// the next child drawn flat on a phone can be read off daemon.log.
func (p *Publisher) noteChildParents(rows map[string]map[string]any, parents, causes map[string]string) {
	if p.childCauses == nil {
		p.childCauses = map[string]string{}
	}
	for kid := range p.childCauses {
		switch {
		case parents[kid] != "" && rows[kid] != nil:
			p.logf("cloud: child %s is listed under %s again", kid, parents[kid])
			delete(p.childCauses, kid)
		case causes[kid] == "":
			// Gone from the list, or no longer a child a task places.
			delete(p.childCauses, kid)
		}
	}
	ids := make([]string, 0, len(causes))
	for kid := range causes {
		ids = append(ids, kid)
	}
	sort.Strings(ids)
	for _, kid := range ids {
		if p.childCauses[kid] == causes[kid] {
			continue
		}
		p.childCauses[kid] = causes[kid]
		p.logf("cloud: child %s has no listed parent: %s", kid, causes[kid])
	}
}
