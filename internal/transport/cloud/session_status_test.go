package cloud

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestStatusProjectionCannotCarryContentAndWithholdsUnverifiedTarget(t *testing.T) {
	row := map[string]any{
		"id": "%19", "state": "waiting", "assistant": "claude", "backend": "tmux",
		"execution_generation": "0123456789abcdef0123456789abcdef",
		"line":                 "private prompt", "label": "private label", "cwd": "/private/path",
		"menu":   map[string]any{"question": "private menu"},
		"shells": []any{"private shell"}, "git": "private git",
		"activity":        map[string]any{"known": true, "at": float64(100), "detail": "private activity"},
		"acceptance":      map[string]any{"state": "pending", "phase": "done", "title": "private delivery"},
		"attention_count": float64(1),
		"source":          map[string]any{"provenance": "tmux", "observed_at": float64(100), "freshness": "current"},
		"closeability":    map[string]any{"state": "blocked", "source": map[string]any{"freshness": "current"}, "reasons": []any{"private obligation"}},
		"agents":          []any{map[string]any{"state": "failed", "what": "private agent"}},
		"agents_reading":  map[string]any{"state": "complete"},
	}
	got := ProjectSessionStatus("mac_a", "%19", row, true, 1900)
	if got.ExecutionGeneration == "" || !got.InventoryComplete || got.Source.ObservedAt != 100 ||
		got.LastMovementAt != 100 || got.NoProgressAfterMS != 1800000 || got.NoMovement == nil || !*got.NoMovement ||
		!got.CompletedUnconfirmed || !got.AttentionRequired || got.CloseBlocked == nil || !*got.CloseBlocked || got.FailedAgentCount == nil || *got.FailedAgentCount != 1 {
		t.Fatalf("current status: %+v", got)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"private", "cwd", "menu", "shells", "git", "label", "line"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("status leaked %s: %s", secret, encoded)
		}
	}
	row["source"].(map[string]any)["freshness"] = "unverified"
	stale := ProjectSessionStatus("mac_a", "%19", row, false, 102)
	if stale.ExecutionGeneration != "" || stale.Source.Freshness != "unverified" || stale.CompletedUnconfirmed || stale.CloseBlocked != nil || stale.FailedAgentCount != nil {
		t.Fatalf("stale status: %+v", stale)
	}
	missing := ProjectSessionStatus("mac_a", "%19", nil, false, 103)
	if missing.ExecutionGeneration != "" || missing.State != "unknown" || missing.Source.Freshness != "missing" {
		t.Fatalf("missing status: %+v", missing)
	}
}

func TestStatusDoesNotCallDeployingCompletedOrInventSessionFailure(t *testing.T) {
	row := map[string]any{
		"state": "working", "source": map[string]any{"freshness": "current"},
		"acceptance":   map[string]any{"state": "pending", "phase": "deploying"},
		"closeability": map[string]any{"state": "blocked", "source": map[string]any{"freshness": "missing"}},
		"agents":       []any{}, "agents_reading": map[string]any{"state": "complete"},
	}
	got := ProjectSessionStatus("mac_a", "%19", row, true, 100)
	if got.State != "working" || got.CompletedUnconfirmed || got.CloseBlocked != nil || got.FailedAgentCount == nil || *got.FailedAgentCount != 0 {
		t.Fatalf("deployed work is not confirmed done and stale closeability is not blocked: %+v", got)
	}
	row["closeability"] = map[string]any{"state": "safe", "source": map[string]any{"freshness": "current"}}
	delete(row, "agents") // The full row omits an empty provider-native list.
	got = ProjectSessionStatus("mac_a", "%19", row, true, 100)
	if got.CloseBlocked == nil || *got.CloseBlocked || got.FailedAgentCount == nil || *got.FailedAgentCount != 0 {
		t.Fatalf("complete empty readings must publish measured false and zero: %+v", got)
	}
}

func TestStatusReplySignalRequiresAProvenCurrentQuestion(t *testing.T) {
	row := map[string]any{
		"state": "waiting", "work_person_needed": true,
		"source": map[string]any{"freshness": "current"},
	}
	if got := ProjectSessionStatus("mac_a", "%19", row, true, 100); got.WaitingForReply {
		t.Fatalf("a waiting state and person-needed work invented a question: %+v", got)
	}
	row["screen_reading"] = "read"
	row["menu"] = map[string]any{"question": "private question", "options": []any{map[string]any{"label": "private option"}}}
	got := ProjectSessionStatus("mac_a", "%19", row, true, 100)
	if !got.WaitingForReply {
		t.Fatalf("current captured menu lost its reply signal: %+v", got)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "private") {
		t.Fatalf("reply signal copied menu content: %s", encoded)
	}
	row["screen_reading"] = "unavailable"
	row["menu"].(map[string]any)["source"] = "transcript"
	if got := ProjectSessionStatus("mac_a", "%19", row, true, 100); !got.WaitingForReply {
		t.Fatalf("open single-question transcript menu lost its signal: %+v", got)
	}
	row["menu"].(map[string]any)["source"] = ""
	if got := ProjectSessionStatus("mac_a", "%19", row, true, 100); got.WaitingForReply {
		t.Fatalf("an unavailable screen without transcript proof invented a question: %+v", got)
	}
	row["source"].(map[string]any)["freshness"] = "unverified"
	row["screen_reading"] = "read"
	if got := ProjectSessionStatus("mac_a", "%19", row, false, 101); got.WaitingForReply {
		t.Fatalf("a carried old question remained actionable: %+v", got)
	}
}

func TestStatusRelayFailureDoesNotBlockLegacyInventory(t *testing.T) {
	p := &Publisher{MachineID: "mac_a", published: map[string][32]byte{}, sent: map[string]time.Time{}}
	p.Publish = func(_ context.Context, out Outbound) error {
		if strings.HasPrefix(out.Channel, "ss/") {
			return errors.New("relay does not accept ss yet")
		}
		return nil
	}
	reading := sessionReading{sessions: []map[string]any{{"id": "%19", "state": "idle"}}, at: json.RawMessage(`100`), complete: true}
	p.publishStatuses(context.Background(), reading, []string{"%19"})
	if p.unsent != 0 || p.published["status_snapshot"] != ([32]byte{}) {
		t.Fatalf("status failure blocked legacy inventory: unsent=%d published=%v", p.unsent, p.published)
	}
}

func TestStatusInventoryFollowsTheRows(t *testing.T) {
	c := &collector{}
	p := &Publisher{MachineID: "mac_a", published: map[string][32]byte{}, sent: map[string]time.Time{}, Publish: c.publish}
	reading := sessionReading{sessions: []map[string]any{{"id": "%19", "state": "idle"}}, at: json.RawMessage(`100`), complete: true}
	p.publishStatuses(context.Background(), reading, []string{"%19"})
	channels := c.channels()
	if len(channels) != 2 || channels[0] != "ss/mac_a/%2519" || channels[1] != "ss/mac_a/"+InventorySessionID {
		t.Fatalf("status publication order: %v", channels)
	}
	marker := c.payload(t, channels[1])
	row := c.payload(t, channels[0])
	if marker["at"] != float64(100) || marker["complete"] != true ||
		marker["snapshot_generation"] == "" || marker["snapshot_generation"] != row["snapshot_generation"] {
		t.Fatalf("status marker: %v", marker)
	}
}

// A work line the viewer must re-read is not a status change: the row says the
// same thing, and only the marker's presentation id moves. It used to restate
// every row of the machine for it, which is what made one working Session cost
// its nine idle neighbours a frame each every fifteen seconds.
func TestStatusNotifiesOneMachineListReadWhenOnlyItsWorkLineChanges(t *testing.T) {
	c := &collector{}
	p := &Publisher{MachineID: "mac_a", published: map[string][32]byte{}, sent: map[string]time.Time{}, Publish: c.publish}
	row := map[string]any{"id": "%19", "state": "working", "line": "private first line"}
	reading := sessionReading{sessions: []map[string]any{row}, at: json.RawMessage(`100`), complete: true}
	p.publishStatuses(context.Background(), reading, []string{"%19"})
	if len(c.channels()) != 2 {
		t.Fatalf("initial pass: %v", c.channels())
	}
	first := c.payload(t, "ss/mac_a/"+InventorySessionID)
	reading.at = json.RawMessage(`200`)
	p.publishStatuses(context.Background(), reading, []string{"%19"})
	if len(c.channels()) != 2 {
		t.Fatalf("unchanged display republished before heartbeat: %v", c.channels())
	}
	row["line"] = "private next line"
	p.publishStatuses(context.Background(), reading, []string{"%19"})
	if got := c.channels(); len(got) != 3 || got[2] != "ss/mac_a/"+InventorySessionID {
		t.Fatalf("a changed work line did not state the marker alone: %v", got)
	}
	next := c.payload(t, "ss/mac_a/"+InventorySessionID)
	if next["presentation_generation"] == first["presentation_generation"] {
		t.Fatalf("the machine list was not told to read again: %v", next)
	}
	if next["snapshot_generation"] != first["snapshot_generation"] {
		t.Fatalf("a work line changed the pass the rows are trusted by: %v -> %v", first, next)
	}
	if c.payload(t, "ss/mac_a/%2519")["snapshot_generation"] != next["snapshot_generation"] {
		t.Fatal("the retained row no longer matches the newest marker")
	}
	for _, channel := range c.channels() {
		body, err := json.Marshal(c.payload(t, channel))
		if err != nil || strings.Contains(string(body), "private") {
			t.Fatalf("status channel %s copied work content: %s (%v)", channel, body, err)
		}
	}
}

// What the pass id is for: a row the relay still retains for a Session that
// has closed, or that is running a different execution, cannot satisfy the
// newest marker. The id is derived from the set rather than drawn fresh every
// pass, so this is the property that had to be proved again.
func TestARetainedStatusRowOfAnotherSetCannotSatisfyTheMarker(t *testing.T) {
	c := &collector{}
	p := &Publisher{MachineID: "mac_a", published: map[string][32]byte{}, sent: map[string]time.Time{}, Publish: c.publish}
	first := "0123456789abcdef0123456789abcdef"
	second := "fedcba9876543210fedcba9876543210"
	alive := map[string]any{"id": "%19", "state": "idle", "execution_generation": first,
		"source": map[string]any{"freshness": "current", "observed_at": float64(90), "provenance": "tmux"}}
	gone := map[string]any{"id": "%20", "state": "idle", "execution_generation": second,
		"source": map[string]any{"freshness": "current", "observed_at": float64(90), "provenance": "tmux"}}
	reading := sessionReading{sessions: []map[string]any{alive, gone}, at: json.RawMessage(`100`), complete: true}
	p.publishStatuses(context.Background(), reading, []string{"%19", "%20"})
	retained := c.payload(t, "ss/mac_a/%2520")
	marker := c.payload(t, "ss/mac_a/"+InventorySessionID)
	if retained["snapshot_generation"] != marker["snapshot_generation"] {
		t.Fatalf("the first pass did not state one set: %v %v", retained, marker)
	}

	// %20 closes. Its row stays on the relay; the marker stops naming it and
	// says a different pass, so a viewer holding that row cannot count it.
	reading.sessions = []map[string]any{alive}
	reading.at = json.RawMessage(`105`)
	p.publishStatuses(context.Background(), reading, []string{"%19"})
	smaller := c.payload(t, "ss/mac_a/"+InventorySessionID)
	if smaller["snapshot_generation"] == marker["snapshot_generation"] {
		t.Fatalf("a Session leaving the set kept its pass id: %v", smaller)
	}
	if c.payload(t, "ss/mac_a/%2519")["snapshot_generation"] != smaller["snapshot_generation"] {
		t.Fatal("the remaining row was not restated for the new set")
	}
	if retained["snapshot_generation"] == smaller["snapshot_generation"] {
		t.Fatal("the retained row of the closed Session still matches the marker")
	}

	// %19 restarts. The set is the same ids as the pass before it, and the row
	// a viewer may still hold is the old execution's, so the id has to move.
	alive["execution_generation"] = second
	reading.at = json.RawMessage(`110`)
	p.publishStatuses(context.Background(), reading, []string{"%19"})
	restarted := c.payload(t, "ss/mac_a/"+InventorySessionID)
	if restarted["snapshot_generation"] == smaller["snapshot_generation"] {
		t.Fatalf("a restarted execution kept its pass id: %v", restarted)
	}
	row := c.payload(t, "ss/mac_a/%2519")
	if row["execution_generation"] != second || row["snapshot_generation"] != restarted["snapshot_generation"] {
		t.Fatalf("the restarted row did not state the new set: %v", row)
	}
}

// A Session that leaves the set and comes back is stated again. Its row is
// forgotten when the marker stops naming it, so an id whose execution never
// changed cannot be skipped against a row the relay has held since before it
// went away.
func TestAReturningSessionIsStatedAgainRatherThanLeftToTheRelay(t *testing.T) {
	c := &collector{}
	p := &Publisher{MachineID: "mac_a", published: map[string][32]byte{}, sent: map[string]time.Time{}, Publish: c.publish}
	row := map[string]any{"id": "%19", "state": "idle", "execution_generation": "0123456789abcdef0123456789abcdef",
		"source": map[string]any{"freshness": "current", "observed_at": float64(90), "provenance": "tmux"}}
	other := map[string]any{"id": "%20", "state": "idle"}
	reading := sessionReading{sessions: []map[string]any{row, other}, at: json.RawMessage(`100`), complete: true}
	p.publishStatuses(context.Background(), reading, []string{"%19", "%20"})
	reading.sessions = []map[string]any{other}
	reading.at = json.RawMessage(`105`)
	p.publishStatuses(context.Background(), reading, []string{"%20"})
	c.reset()
	reading.sessions = []map[string]any{row, other}
	reading.at = json.RawMessage(`110`)
	p.publishStatuses(context.Background(), reading, []string{"%19", "%20"})
	if got := c.payload(t, "ss/mac_a/%2519"); got["projected_at"] != float64(110) {
		t.Fatalf("the returning Session was not stated again: %v", got)
	}
}

func TestStatusCarriesOnlyExistingTaskAndEpicAncestry(t *testing.T) {
	c := &collector{}
	p := &Publisher{MachineID: "mac_a", published: map[string][32]byte{}, sent: map[string]time.Time{}, Publish: c.publish,
		tasks: []map[string]any{{"child": map[string]any{"terminalId": "child"}, "root": map[string]any{"terminalId": "root"}}}}
	reading := sessionReading{sessions: []map[string]any{
		{"id": "root", "sessionId": "root-conversation", "state": "working", "machine_scope": true},
		{"id": "child", "state": "working", "label": "private child title"},
		{"id": "epic", "epic_parent": map[string]any{"owner_session_id": "root-conversation", "epic_id": "private epic"}},
		{"id": "orphan", "epic_parent": map[string]any{"owner_session_id": "missing", "epic_id": "private epic"}},
	}, at: json.RawMessage(`100`), complete: true}
	p.publishStatuses(context.Background(), reading, []string{"root", "child", "epic", "orphan"})
	if c.payload(t, "ss/mac_a/child")["parent_session_id"] != "root" ||
		c.payload(t, "ss/mac_a/epic")["parent_session_id"] != "root" ||
		c.payload(t, "ss/mac_a/root")["machine_scope"] != true {
		t.Fatal("listed task, Epic, or machine workspace relationship was lost")
	}
	if _, ok := c.payload(t, "ss/mac_a/orphan")["parent_session_id"]; ok {
		t.Fatal("an absent parent invented an indent")
	}
	for _, id := range []string{"root", "child", "epic", "orphan"} {
		body, _ := json.Marshal(c.payload(t, "ss/mac_a/"+id))
		if strings.Contains(string(body), "private") {
			t.Fatalf("status ancestry copied content for %s", id)
		}
	}
}

func TestStatusInventoryPublishesAnEmptySet(t *testing.T) {
	c := &collector{}
	p := &Publisher{MachineID: "mac_a", published: map[string][32]byte{}, sent: map[string]time.Time{}, Publish: c.publish}
	p.publishStatuses(context.Background(), sessionReading{at: json.RawMessage(`100`), complete: true}, nil)
	channels := c.channels()
	if len(channels) != 1 || channels[0] != "ss/mac_a/"+InventorySessionID {
		t.Fatalf("empty status publication: %v", channels)
	}
	inventory, ok := c.payload(t, channels[0])["inventory"].(map[string]any)
	if !ok {
		t.Fatal("empty marker has no inventory")
	}
	if sessions, ok := inventory["sessions"].([]any); !ok || len(sessions) != 0 {
		t.Fatalf("empty snapshot was not a proved empty set: %v", inventory["sessions"])
	}
}

func TestPartialScanCannotReplaceTheLastCompleteStatusSet(t *testing.T) {
	c := &collector{}
	p := &Publisher{MachineID: "mac_a", published: map[string][32]byte{}, sent: map[string]time.Time{}, Publish: c.publish}
	id := "%19"
	complete := sessionReading{sessions: []map[string]any{{"id": id, "state": "idle"}}, at: json.RawMessage(`100`), complete: true}
	p.publishStatuses(context.Background(), complete, []string{id})
	first := c.payload(t, "ss/mac_a/"+InventorySessionID)
	if len(c.channels()) != 2 {
		t.Fatalf("first complete pass published %v", c.channels())
	}
	partial := sessionReading{sessions: []map[string]any{{"id": id, "state": "working"}}, at: json.RawMessage(`200`)}
	p.publishStatuses(context.Background(), partial, []string{id})
	if len(c.channels()) != 2 {
		t.Fatalf("partial pass replaced the complete status set: %v", c.channels())
	}
	if got := c.payload(t, "ss/mac_a/"+InventorySessionID); got["snapshot_generation"] != first["snapshot_generation"] {
		t.Fatalf("partial pass changed the status barrier: %v", got)
	}
	partial.complete = true
	p.publishStatuses(context.Background(), partial, []string{id})
	// The set is the same one Session, so the barrier stands and only the row
	// that now says something else goes out.
	if got := c.channels(); len(got) != 3 || got[2] != "ss/mac_a/%2519" {
		t.Fatalf("next complete pass did not replace the row: %v", got)
	}
	if got := c.payload(t, "ss/mac_a/%2519"); got["state"] != "working" ||
		got["snapshot_generation"] != first["snapshot_generation"] {
		t.Fatalf("the replaced row is not the newest reading of this set: %v", got)
	}
}

func TestAuthoritativeEmptyScanPublishesACompleteStatusBarrier(t *testing.T) {
	c := &collector{}
	p := &Publisher{MachineID: "mac_a", published: map[string][32]byte{}, sent: map[string]time.Time{}, Publish: c.publish}
	p.publishStatuses(context.Background(), sessionReading{at: json.RawMessage(`100`), emptyAuthoritative: true}, nil)
	if got := c.payload(t, "ss/mac_a/"+InventorySessionID); got["complete"] != true {
		t.Fatalf("authoritative empty scan did not establish completeness: %v", got)
	}
}

// Turning the iTerm2 scan off leaves that source incomplete on purpose. On
// 2026-10-09 that made every reading partial, so ss/ was never published again
// and the Cloud list said it was waiting for this machine until the scan was
// turned back on. A source that failed to answer still holds the set back.
func TestAScanTurnedOffStillPublishesTheStatusSet(t *testing.T) {
	read := func(sources string) sessionReading {
		body := []byte(`{"at":100,"sessions":[{"id":"%7","state":"idle"}],"scan":{"complete":false,"sources":` + sources + `}}`)
		p := &Publisher{Sessions: func(context.Context) ([]byte, bool) { return body, true }}
		reading, ok := p.readSessions(context.Background())
		if !ok {
			t.Fatal("the list was not read")
		}
		return reading
	}
	off := read(`[{"source":"iterm","complete":false,"disabled":"setting"},{"source":"ps","complete":true},{"source":"tmux","complete":true}]`)
	c := &collector{}
	p := &Publisher{MachineID: "mac_a", published: map[string][32]byte{}, sent: map[string]time.Time{}, Publish: c.publish}
	p.publishStatuses(context.Background(), off, off.ids)
	if got := c.payload(t, "ss/mac_a/"+InventorySessionID); got["complete"] != true {
		t.Fatalf("a scan with iTerm2 turned off published no complete status set: %v", c.channels())
	}

	failed := read(`[{"source":"iterm","complete":false,"disabled":"setting"},{"source":"ps","complete":true},{"source":"tmux","complete":false}]`)
	if failed.whole() {
		t.Fatal("a source that failed to answer was counted as the whole set because another was turned off")
	}
}
