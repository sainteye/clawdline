package cloud

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	domain "github.com/sainteye/clawdline/internal/domain/cloud"
)

const viewerInventorySession = "__clawdline_inventory_v1__"
const viewerStatusFreshness = 5 * time.Minute

var viewerGeneration = regexp.MustCompile(`^[0-9a-f]{32}$`)

// ViewerDestination is the indivisible target of a remote Session read or action.
type ViewerDestination struct {
	MachineID           string `json:"machine_id"`
	SessionID           string `json:"session_id"`
	ExecutionGeneration string `json:"execution_generation"`
}

func (d ViewerDestination) Valid() bool {
	return d.MachineID != "" && d.SessionID != "" && viewerGeneration.MatchString(d.ExecutionGeneration)
}

type ViewerSessionStatus struct {
	Destination        ViewerDestination `json:"destination"`
	State              string            `json:"state"`
	Freshness          string            `json:"freshness"`
	ObservedAt         int64             `json:"observed_at"`
	SnapshotGeneration string            `json:"snapshot_generation"`
	NeedsAttention     bool              `json:"needs_attention,omitempty"`
	WaitingForReply    bool              `json:"waiting_for_reply,omitempty"`
}

// ViewerProjection is the authenticated ss/ set for one machine. A marker
// proves the inventory only when every named row arrived from the same pass.
type ViewerProjection struct {
	Kind           string                `json:"kind"`
	Reason         string                `json:"reason,omitempty"`
	ObservedAt     int64                 `json:"observed_at,omitempty"`
	Snapshot       string                `json:"snapshot_generation,omitempty"`
	Rows           []ViewerSessionStatus `json:"rows,omitempty"`
	UnknownTargets int                   `json:"unknown_targets,omitempty"`
}

type heldViewerStatus struct {
	seq  uint64
	body json.RawMessage
}

// ViewerStatusStore only accepts already authenticated envelopes from a
// viewer Transport. A row signed by machine A cannot fill machine B's slot.
type ViewerStatusStore struct {
	mu   sync.Mutex
	rows map[string]map[string]heldViewerStatus
}

func NewViewerStatusStore() *ViewerStatusStore {
	return &ViewerStatusStore{rows: make(map[string]map[string]heldViewerStatus)}
}

func (s *ViewerStatusStore) Apply(envelope domain.Envelope, plaintext []byte) bool {
	parts := strings.Split(envelope.Ch, "/")
	if len(parts) != 3 || parts[0] != "ss" || !json.Valid(plaintext) {
		return false
	}
	machineID, machineErr := url.PathUnescape(parts[1])
	sessionID, sessionErr := url.PathUnescape(parts[2])
	if machineErr != nil || sessionErr != nil || machineID != envelope.Sender || sessionID == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rows[machineID] == nil {
		s.rows[machineID] = make(map[string]heldViewerStatus)
	}
	if prior, ok := s.rows[machineID][sessionID]; ok && envelope.Seq <= prior.seq {
		return false
	}
	s.rows[machineID][sessionID] = heldViewerStatus{seq: envelope.Seq, body: append(json.RawMessage(nil), plaintext...)}
	return true
}

func (s *ViewerStatusStore) Forget(machineID string) {
	s.mu.Lock()
	delete(s.rows, machineID)
	s.mu.Unlock()
}

// GapTarget names one exact retained ss/ row the relay can restate for a
// marker already held locally. It never guesses a generation from an old row.
func (s *ViewerStatusStore) GapTarget(machineID string) (sessionID, snapshot string, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored := s.rows[machineID]
	markerRow, exists := stored[viewerInventorySession]
	if !exists {
		return "", "", false
	}
	var marker struct {
		Inventory struct {
			Sessions []string `json:"sessions"`
		} `json:"inventory"`
		Complete           bool   `json:"complete"`
		SnapshotGeneration string `json:"snapshot_generation"`
	}
	if json.Unmarshal(markerRow.body, &marker) != nil || !marker.Complete ||
		!viewerGeneration.MatchString(marker.SnapshotGeneration) {
		return "", "", false
	}
	for _, id := range marker.Inventory.Sessions {
		if id == "" || id == viewerInventorySession {
			return "", "", false
		}
		row, found := stored[id]
		if !found {
			return id, marker.SnapshotGeneration, true
		}
		var data struct {
			SnapshotGeneration string `json:"snapshot_generation"`
		}
		if json.Unmarshal(row.body, &data) != nil || data.SnapshotGeneration != marker.SnapshotGeneration {
			return id, marker.SnapshotGeneration, true
		}
	}
	return "", "", false
}

func (s *ViewerStatusStore) Project(machineID string, now time.Time) ViewerProjection {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored := s.rows[machineID]
	markerRow, ok := stored[viewerInventorySession]
	if !ok {
		return ViewerProjection{Kind: "unavailable", Reason: "unknown"}
	}
	var marker struct {
		Inventory struct {
			Version  int      `json:"version"`
			Sessions []string `json:"sessions"`
		} `json:"inventory"`
		At                 int64  `json:"at"`
		Complete           bool   `json:"complete"`
		SnapshotGeneration string `json:"snapshot_generation"`
	}
	if json.Unmarshal(markerRow.body, &marker) != nil || marker.Inventory.Version != 1 ||
		marker.Inventory.Sessions == nil || marker.At <= 0 || !viewerGeneration.MatchString(marker.SnapshotGeneration) {
		return ViewerProjection{Kind: "unavailable", Reason: "bad_projection"}
	}
	seen := make(map[string]bool, len(marker.Inventory.Sessions))
	for _, id := range marker.Inventory.Sessions {
		if id == "" || id == viewerInventorySession || seen[id] {
			return ViewerProjection{Kind: "unavailable", Reason: "bad_projection"}
		}
		seen[id] = true
	}
	if !marker.Complete {
		return ViewerProjection{Kind: "unavailable", Reason: "unknown", ObservedAt: marker.At}
	}
	if viewerOutsideFreshWindow(marker.At, now) {
		return ViewerProjection{Kind: "unavailable", Reason: "stale", ObservedAt: marker.At}
	}
	out := ViewerProjection{Kind: "ready", ObservedAt: marker.At, Snapshot: marker.SnapshotGeneration,
		Rows: make([]ViewerSessionStatus, 0, len(marker.Inventory.Sessions))}
	for _, id := range marker.Inventory.Sessions {
		row, ok := stored[id]
		if !ok {
			return ViewerProjection{Kind: "unavailable", Reason: "event_gap", ObservedAt: marker.At}
		}
		var data struct {
			MachineID           string `json:"machine_id"`
			SessionID           string `json:"session_id"`
			ExecutionGeneration string `json:"execution_generation"`
			SnapshotGeneration  string `json:"snapshot_generation"`
			State               string `json:"state"`
			InventoryComplete   bool   `json:"inventory_complete"`
			ProjectedAt         int64  `json:"projected_at"`
			AttentionRequired   bool   `json:"attention_required"`
			WaitingForReply     bool   `json:"waiting_for_reply"`
			Source              struct {
				ObservedAt int64  `json:"observed_at"`
				Freshness  string `json:"freshness"`
			} `json:"source"`
		}
		if json.Unmarshal(row.body, &data) != nil || data.MachineID != machineID || data.SessionID != id ||
			!data.InventoryComplete || data.ProjectedAt <= 0 || data.Source.ObservedAt <= 0 ||
			!viewerStatusState(data.State) || !viewerSourceFreshness(data.Source.Freshness) {
			return ViewerProjection{Kind: "unavailable", Reason: "bad_projection"}
		}
		if data.SnapshotGeneration != marker.SnapshotGeneration {
			return ViewerProjection{Kind: "unavailable", Reason: "event_gap", ObservedAt: marker.At}
		}
		if viewerOutsideFreshWindow(data.ProjectedAt, now) ||
			(data.Source.Freshness == "current" && viewerOutsideFreshWindow(data.Source.ObservedAt, now)) {
			return ViewerProjection{Kind: "unavailable", Reason: "stale", ObservedAt: marker.At}
		}
		if !viewerGeneration.MatchString(data.ExecutionGeneration) {
			out.UnknownTargets++
			continue
		}
		freshness := "unknown"
		if data.Source.Freshness == "current" {
			freshness = "current"
		} else if data.Source.Freshness == "unverified" {
			freshness = "stale"
		}
		out.Rows = append(out.Rows, ViewerSessionStatus{
			Destination: ViewerDestination{MachineID: machineID, SessionID: id, ExecutionGeneration: data.ExecutionGeneration},
			State:       data.State, Freshness: freshness, ObservedAt: data.Source.ObservedAt,
			SnapshotGeneration: data.SnapshotGeneration, NeedsAttention: data.AttentionRequired,
			WaitingForReply: data.WaitingForReply,
		})
	}
	return out
}

func (p ViewerProjection) Available(destination ViewerDestination) string {
	if p.Kind != "ready" || !destination.Valid() {
		return "unknown"
	}
	for _, row := range p.Rows {
		if row.Destination == destination {
			return row.Freshness
		}
	}
	return "changed"
}

func viewerOutsideFreshWindow(seconds int64, now time.Time) bool {
	observed := time.Unix(seconds, 0)
	delta := now.Sub(observed)
	return delta < -viewerStatusFreshness || delta > viewerStatusFreshness
}

func viewerStatusState(state string) bool {
	return state == "working" || state == "waiting" || state == "idle" || state == "unknown"
}

func viewerSourceFreshness(freshness string) bool {
	return freshness == "current" || freshness == "unverified" || freshness == "missing"
}
