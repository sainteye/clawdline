package cloud

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCapacityStatusNeedsAttentionWithoutAskingForAMenuReply(t *testing.T) {
	row := map[string]any{"state": "idle", "provider_block": "model_capacity",
		"source": map[string]any{"freshness": "current"}}
	got := ProjectSessionStatus("machine_a", "%19", row, true, 100)
	if !got.AttentionRequired || got.WaitingForReply || got.ProviderBlock != "model_capacity" {
		t.Fatalf("capacity status = %+v", got)
	}
	row["provider_block"] = "private arbitrary error text"
	got = ProjectSessionStatus("machine_a", "%19", row, true, 100)
	if got.ProviderBlock != "" || got.AttentionRequired {
		t.Fatalf("unrecognized block admitted: %+v", got)
	}
	row["provider_block"] = "model_capacity"
	row["source"] = map[string]any{"freshness": "unverified"}
	got = ProjectSessionStatus("machine_a", "%19", row, false, 101)
	if got.ProviderBlock != "" || got.AttentionRequired {
		t.Fatalf("stale refusal remained actionable: %+v", got)
	}
}

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

func TestNativePermissionStatusRequiresExactOwnedProtocolOffer(t *testing.T) {
	row := map[string]any{"state": "waiting", "assistant": "antigravity", "backend": "owned", "identity": "protocol",
		"source": map[string]any{"freshness": "current", "provenance": "owned"},
		"menu":   map[string]any{"revision": "native-offer", "options": []any{map[string]any{"label": "private native option"}}}}
	got := ProjectSessionStatus("machine_a", "owned:acp:synthetic", row, true, 100)
	if !got.WaitingForReply || got.Assistant != "antigravity" || got.Backend != "owned" || got.Source.Provenance != "owned" {
		t.Fatalf("native offer status=%+v", got)
	}
	raw, _ := json.Marshal(got)
	if strings.Contains(string(raw), "private") || strings.Contains(string(raw), "native-offer") {
		t.Fatalf("status leaked offer: %s", raw)
	}
	for _, patch := range []map[string]any{{"identity": "launch_hook"}, {"backend": "tmux"}, {"source": map[string]any{"freshness": "unverified"}}, {"menu": map[string]any{"options": []any{map[string]any{"label": "private option"}}}}} {
		copy := map[string]any{}
		for k, v := range row {
			copy[k] = v
		}
		for k, v := range patch {
			copy[k] = v
		}
		if ProjectSessionStatus("machine_a", "owned:acp:synthetic", copy, true, 100).WaitingForReply {
			t.Fatalf("unproven native offer accepted: %v", patch)
		}
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

// A Session a webhook schedule opened appeared on the one-machine list as
// "Claude Session" with no Project, and opening it did not help: its title and
// directory settled after its first pass, and nothing told the list to read
// them again.
func TestStatusNotifiesOneMachineListReadWhenItsTitleSettles(t *testing.T) {
	c := &collector{}
	p := &Publisher{MachineID: "mac_a", published: map[string][32]byte{}, sent: map[string]time.Time{}, Publish: c.publish}
	row := map[string]any{"id": "%19", "state": "working"}
	reading := sessionReading{sessions: []map[string]any{row}, at: json.RawMessage(`100`), complete: true}
	p.publishStatuses(context.Background(), reading, []string{"%19"})
	first := c.payload(t, "ss/mac_a/"+InventorySessionID)
	row["label"] = "private settled title"
	row["cwd"] = "/private/project"
	p.publishStatuses(context.Background(), reading, []string{"%19"})
	if got := c.channels(); len(got) != 3 || got[2] != "ss/mac_a/"+InventorySessionID {
		t.Fatalf("a settled title did not refresh only the machine list: %v", got)
	}
	next := c.payload(t, "ss/mac_a/"+InventorySessionID)
	if next["presentation_generation"] == first["presentation_generation"] ||
		next["snapshot_generation"] != first["snapshot_generation"] {
		t.Fatalf("a settled title did not move only the presentation: %v -> %v", first, next)
	}
	for _, channel := range c.channels() {
		body, err := json.Marshal(c.payload(t, channel))
		if err != nil || strings.Contains(string(body), "private") {
			t.Fatalf("status channel %s copied the title: %s (%v)", channel, body, err)
		}
	}
}

// The Board's assignment dialog picks a Session by the Project its machine
// says it belongs to. That answer can settle without the cwd moving (a
// repository appearing around it), so it wakes the machine list the way the
// cwd does, and like the cwd it never rides on ss/.
func TestStatusNotifiesOneMachineListReadWhenItsProjectKeySettles(t *testing.T) {
	c := &collector{}
	p := &Publisher{MachineID: "mac_a", published: map[string][32]byte{}, sent: map[string]time.Time{}, Publish: c.publish}
	row := map[string]any{"id": "%19", "state": "working", "cwd": "/private/project/sub"}
	reading := sessionReading{sessions: []map[string]any{row}, at: json.RawMessage(`100`), complete: true}
	p.publishStatuses(context.Background(), reading, []string{"%19"})
	first := c.payload(t, "ss/mac_a/"+InventorySessionID)
	row["project_key"] = "/private/project"
	p.publishStatuses(context.Background(), reading, []string{"%19"})
	next := c.payload(t, "ss/mac_a/"+InventorySessionID)
	if next["presentation_generation"] == first["presentation_generation"] {
		t.Fatalf("a settled project key did not move the presentation: %v -> %v", first, next)
	}
	for _, channel := range c.channels() {
		body, err := json.Marshal(c.payload(t, channel))
		if err != nil || strings.Contains(string(body), "private") {
			t.Fatalf("status channel %s copied the project key: %s (%v)", channel, body, err)
		}
	}
}

// A box ticked on a multi-select changed nothing the list was told about: the
// waiting row's line names the question, its rows and the caret, not the
// ticks, so the phone's card kept every box empty while the pane showed four
// ticked (2026-10-16). Whatever the card draws from the menu wakes the list.
func TestStatusNotifiesOneMachineListReadWhenABoxIsTicked(t *testing.T) {
	c := &collector{}
	p := &Publisher{MachineID: "mac_a", published: map[string][32]byte{}, sent: map[string]time.Time{}, Publish: c.publish}
	menu := func(ticked bool) map[string]any {
		return map[string]any{"question": "private question", "options": []any{
			map[string]any{"n": float64(1), "label": "private alpha", "can": true, "selected": true, "checked": false},
			map[string]any{"n": float64(2), "label": "private beta", "can": true, "selected": false, "checked": ticked},
		}}
	}
	row := map[string]any{"id": "%19", "state": "waiting", "screen_reading": "read", "line": "private revision", "menu": menu(false)}
	reading := sessionReading{sessions: []map[string]any{row}, at: json.RawMessage(`100`), complete: true}
	p.publishStatuses(context.Background(), reading, []string{"%19"})
	first := c.payload(t, "ss/mac_a/"+InventorySessionID)
	row["menu"] = menu(true)
	p.publishStatuses(context.Background(), reading, []string{"%19"})
	if got := c.channels(); len(got) != 3 || got[2] != "ss/mac_a/"+InventorySessionID {
		t.Fatalf("a ticked box did not refresh only the machine list: %v", got)
	}
	next := c.payload(t, "ss/mac_a/"+InventorySessionID)
	if next["presentation_generation"] == first["presentation_generation"] ||
		next["snapshot_generation"] != first["snapshot_generation"] {
		t.Fatalf("a ticked box did not move only the presentation: %v -> %v", first, next)
	}
	for _, channel := range c.channels() {
		body, err := json.Marshal(c.payload(t, channel))
		if err != nil || strings.Contains(string(body), "private") {
			t.Fatalf("status channel %s copied the menu: %s (%v)", channel, body, err)
		}
	}
}

func TestStatusNotifiesOneMachineListReadWhenItsRoleAppears(t *testing.T) {
	c := &collector{}
	p := &Publisher{MachineID: "mac_a", published: map[string][32]byte{}, sent: map[string]time.Time{}, Publish: c.publish}
	row := map[string]any{"id": "%19", "state": "idle"}
	reading := sessionReading{sessions: []map[string]any{row}, at: json.RawMessage(`100`), complete: true}
	p.publishStatuses(context.Background(), reading, []string{"%19"})
	first := c.payload(t, "ss/mac_a/"+InventorySessionID)
	row["persona"] = "product-manager"
	p.publishStatuses(context.Background(), reading, []string{"%19"})
	if got := c.channels(); len(got) != 3 || got[2] != "ss/mac_a/"+InventorySessionID {
		t.Fatalf("a discovered role did not refresh only the machine list: %v", got)
	}
	next := c.payload(t, "ss/mac_a/"+InventorySessionID)
	if next["presentation_generation"] == first["presentation_generation"] ||
		next["snapshot_generation"] != first["snapshot_generation"] {
		t.Fatalf("role changed the status set instead of its presentation: %v -> %v", first, next)
	}
	for _, channel := range c.channels() {
		body, err := json.Marshal(c.payload(t, channel))
		if err != nil || strings.Contains(string(body), "product-manager") {
			t.Fatalf("status channel %s copied the role: %s (%v)", channel, body, err)
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

func TestStatusCarriesMachineRoleWithoutItsPrivateLabel(t *testing.T) {
	row := map[string]any{
		"state": "idle", "machine_scope": true, "label": "private title",
		"coordinator": map[string]any{"label": "private role label", "status": "online"},
		"source":      map[string]any{"freshness": "current"},
	}
	bound := ProjectSessionStatus("mac_a", "%19", row, true, 100)
	if !bound.MachineScope || !bound.CoordinatorBound {
		t.Fatalf("bound machine identity missing: %+v", bound)
	}
	encoded, err := json.Marshal(bound)
	if err != nil || strings.Contains(string(encoded), "private") {
		t.Fatalf("machine status disclosed a private label: %s (%v)", encoded, err)
	}
	delete(row, "coordinator")
	if pending := ProjectSessionStatus("mac_a", "%19", row, true, 100); !pending.MachineScope || pending.CoordinatorBound {
		t.Fatalf("pending machine identity is not distinct: %+v", pending)
	}
}

func parentOf(rows map[string]map[string]any, tasks []map[string]any) string {
	parents, _ := statusParents(rows, tasks, nil)
	return parents["child"]
}

func TestStatusResolvesTaskRootFromConversationWhenScreenLinkIsMissing(t *testing.T) {
	rows := map[string]map[string]any{
		"root":  {"sessionId": "root-conversation"},
		"child": {},
	}
	tasks := []map[string]any{{
		"child": map[string]any{"terminalId": "child"},
		"root":  map[string]any{"sessionId": "root-conversation", "terminalId": ""},
	}}
	if got := parentOf(rows, tasks); got != "root" {
		t.Fatalf("task root with a missing screen link: got %q, want root", got)
	}
	tasks[0]["root"].(map[string]any)["terminalId"] = "old-root-tab"
	if got := parentOf(rows, tasks); got != "root" {
		t.Fatalf("task root with a stale screen link: got %q, want root", got)
	}
	rows["other"] = map[string]any{"sessionId": "root-conversation"}
	if got := parentOf(rows, tasks); got != "" {
		t.Fatalf("ambiguous conversation invented a parent: %q", got)
	}
}

func TestPublishedStatusUsesTaskConversationWhenProjectedTaskHasNoRootLink(t *testing.T) {
	c := &collector{}
	task := map[string]any{
		"state": "running",
		"child": map[string]any{"terminalId": "child"},
		"root":  map[string]any{"sessionId": "root-conversation", "terminalId": ""},
	}
	p := &Publisher{MachineID: "mac_a", published: map[string][32]byte{}, sent: map[string]time.Time{}, Publish: c.publish,
		taskRows: []map[string]any{task}, tasks: []map[string]any{cloudTaskProjection(task)}}
	reading := sessionReading{sessions: []map[string]any{
		{"id": "root", "sessionId": "root-conversation", "state": "working"},
		{"id": "child", "state": "working"},
	}, at: json.RawMessage(`100`), complete: true}
	p.publishStatuses(context.Background(), reading, []string{"root", "child"})
	if got := c.payload(t, "ss/mac_a/child")["parent_session_id"]; got != "root" {
		t.Fatalf("published child parent = %v, want root", got)
	}
}

func TestTaskAncestryArrivingAfterTheSessionSetStartsANewStatusPass(t *testing.T) {
	c := &collector{}
	p := &Publisher{MachineID: "mac_a", published: map[string][32]byte{}, sent: map[string]time.Time{}, Publish: c.publish}
	reading := sessionReading{sessions: []map[string]any{
		{"id": "root", "state": "working", "execution_generation": "0123456789abcdef0123456789abcdef"},
		{"id": "child", "state": "working", "execution_generation": "abcdef0123456789abcdef0123456789"},
	}, at: json.RawMessage(`100`), complete: true}
	p.publishStatuses(context.Background(), reading, []string{"root", "child"})
	first := c.payload(t, "ss/mac_a/"+InventorySessionID)["snapshot_generation"]
	if _, ok := c.payload(t, "ss/mac_a/child")["parent_session_id"]; ok {
		t.Fatal("a child was linked before the task relationship arrived")
	}

	c.reset()
	p.tasks = []map[string]any{{
		"child": map[string]any{"terminalId": "child"},
		"root":  map[string]any{"terminalId": "root"},
	}}
	p.publishStatuses(context.Background(), reading, []string{"root", "child"})
	if got := c.payload(t, "ss/mac_a/child")["parent_session_id"]; got != "root" {
		t.Fatalf("late task ancestry was not published: got %v, want root", got)
	}
	marker := c.payload(t, "ss/mac_a/"+InventorySessionID)
	if marker["snapshot_generation"] == first {
		t.Fatalf("late task ancestry kept the old status pass: %v", marker)
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
