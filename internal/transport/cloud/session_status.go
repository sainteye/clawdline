package cloud

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	CloseBlocked         *bool  `json:"close_blocked,omitempty"`
	FailedAgentCount     *int64 `json:"failed_agent_count,omitempty"`
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
	if v, _ := row["assistant"].(string); v == "claude" || v == "codex" {
		out.Assistant = v
	}
	if v, _ := row["backend"].(string); v == "tmux" || v == "iterm" || v == "ps" {
		out.Backend = v
	}
	out.MachineScope, _ = row["machine_scope"].(bool)
	if v, _ := row["state"].(string); v == "working" || v == "waiting" || v == "idle" || v == "unknown" {
		out.State = v
	}
	if source, ok := row["source"].(map[string]any); ok {
		if v, _ := source["provenance"].(string); v == "tmux" || v == "iterm" || v == "ps" {
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
				out.WaitingForReply = screen == "read" && menuSource == "" ||
					screen == "unavailable" && menuSource == "transcript"
			}
		}
	}
	if acceptance, ok := row["acceptance"].(map[string]any); ok && acceptance["state"] == "pending" && acceptance["phase"] == "done" {
		out.CompletedUnconfirmed = true
	}
	if row["work_person_needed"] == true {
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
	if reading, ok := row["agents_reading"].(map[string]any); ok && reading["state"] == "complete" {
		if truncated, _ := reading["truncated"].(float64); truncated == 0 {
			count := int64(0)
			if agents, ok := row["agents"].([]any); ok {
				for _, agent := range agents {
					if info, ok := agent.(map[string]any); ok && info["state"] == "failed" {
						count++
					}
				}
			}
			out.FailedAgentCount = &count
		}
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
	// The viewer fetches one pinned list per machine when the presentation id
	// changes. Include the original row's display facts in that id, but never
	// in ss/: a line or shell update must wake the list without disclosing its
	// words to a read_sessions-only viewer.
	displays := make([]map[string]any, 0, len(ids))
	steadyDisplays := make([]map[string]any, 0, len(ids))
	parents := statusParents(rows, p.tasks)
	for _, id := range ids {
		row := rows[id]
		status := ProjectSessionStatus(p.MachineID, id, row, true, at)
		status.ParentSessionID = parents[id]
		statuses = append(statuses, status)
		executions = append(executions, status.ExecutionGeneration)
		// The working clock is not news here either (withoutFreshness); the
		// token count waits for the window with the rest of the volatile
		// fields.
		line, _ := row["line"].(string)
		if row["state"] == "working" {
			line = session.WithoutElapsed(line)
		}
		display := map[string]any{
			"line": line, "work_state": row["work_state"],
			"work_note": row["work_note"], "work_provenance": row["work_provenance"],
			"work_moved_by": row["work_moved_by"], "work_person_needed": row["work_person_needed"],
			"shells": row["shells"], "heavy_work": row["heavy_work"],
			"attention_count": row["attention_count"], "owed": row["owed"],
			"acceptance": row["acceptance"], "disposition": row["disposition"],
			"coordination": row["coordination"],
		}
		displays = append(displays, display)
		steadyDisplays = append(steadyDisplays, volatileFree(display))
	}
	// The pass id the rows and the marker share. It names the set: which
	// Sessions are listed and which execution each row speaks for. While both
	// stand still an unchanged row that the relay still retains is exactly
	// what the newest marker means, so it is not re-sent; a closed Session's
	// id leaves the set and a restarted one brings a new execution, and either
	// way the id changes and every row states the new one before the marker
	// does. That is the property the marker was introduced for: a relay-cached
	// row cannot satisfy a marker of a different set merely because it has the
	// same terminal id.
	pass := statusGeneration(struct {
		IDs        []string `json:"ids"`
		Executions []string `json:"executions"`
	}{IDs: ids, Executions: executions})
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
	}
}

// statusParents projects only terminal ancestry. The signed orch task list
// already resolves dispatch roots; Epic roots use the same unique conversation
// match as the local Session list. No task title or conversation text enters ss/.
func statusParents(rows map[string]map[string]any, tasks []map[string]any) map[string]string {
	parents := make(map[string]string)
	conversations := make(map[string]string)
	for id, row := range rows {
		if conversation, _ := row["sessionId"].(string); conversation != "" {
			if _, exists := conversations[conversation]; exists {
				conversations[conversation] = ""
			} else {
				conversations[conversation] = id
			}
		}
	}
	for _, task := range tasks {
		child, _ := task["child"].(map[string]any)
		root, _ := task["root"].(map[string]any)
		kid, _ := child["terminalId"].(string)
		owner, _ := root["terminalId"].(string)
		if kid != "" && owner != "" && kid != owner && rows[kid] != nil && rows[owner] != nil && rows[kid]["machine_scope"] != true {
			parents[kid] = owner
		}
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
	return parents
}
