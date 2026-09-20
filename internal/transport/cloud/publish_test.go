package cloud

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sainteye/clawdline-go/internal/app/cloudops"
)

// A fake local router that answers one canned `/v1/sessions` body.
type fixedRouter struct {
	mu     sync.Mutex
	body   string
	status int
	calls  int
}

func (r *fixedRouter) Do(_ context.Context, req cloudops.LocalRequest) (cloudops.LocalResponse, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	status := r.status
	if status == 0 {
		status = http.StatusOK
	}
	return cloudops.LocalResponse{Status: status, Body: []byte(r.body), ContentType: "application/json"}, nil
}

func (r *fixedRouter) set(body string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.body = body
}

// A collector standing in for the relay.
type collector struct {
	mu  sync.Mutex
	out []Outbound
}

func (c *collector) publish(_ context.Context, out Outbound) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.out = append(c.out, out)
	return nil
}

func (c *collector) channels() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.channelsLocked()
}

func (c *collector) channelsLocked() []string {
	var names []string
	for _, out := range c.out {
		names = append(names, out.Channel)
	}
	return names
}

func (c *collector) payload(t *testing.T, channel string) map[string]any {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := len(c.out) - 1; i >= 0; i-- {
		if c.out[i].Channel != channel {
			continue
		}
		var body map[string]any
		if err := json.Unmarshal(c.out[i].Payload, &body); err != nil {
			t.Fatalf("%s carried unreadable JSON: %v", channel, err)
		}
		return body
	}
	// The lock is already held: asking channels() here would wait on it for
	// ever, and a missing publication would hang the suite instead of failing.
	t.Fatalf("nothing was published on %s; saw %v", channel, c.channelsLocked())
	return nil
}

func (c *collector) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.out = nil
}

const completeScan = `{"at":17,"scan":{"complete":true},"sessions":[
  {"id":"%19","assistant":"claude","tty":"ttys001",
   "closeability":{"state":"open","observed_at":1,"session_generation":2,"source":{"freshness":"fresh","observed_at":3}}}]}`

func newPublisher(router cloudops.LocalRouter, out *collector) *Publisher {
	return &Publisher{
		MachineID: "mac-01", MachineName: "Test Mac", Platform: "darwin", Version: "0.0.1-test",
		Router: router, Publish: out.publish,
	}
	// Run is not started: each test drives passes itself, so nothing here
	// depends on a ticker.
}

func (p *Publisher) firstPass(ctx context.Context) {
	p.published = map[string][32]byte{}
	p.sent = map[string]time.Time{}
	p.inventoried = map[string]inventoriedRow{}
	p.Pass(ctx)
}

func TestTheDescriptorNamesTheWordsThisMacAnswers(t *testing.T) {
	out := &collector{}
	publisher := newPublisher(&fixedRouter{body: completeScan}, out)
	publisher.firstPass(context.Background())

	body := out.payload(t, "orch/mac-01")
	machine, _ := body["machine"].(map[string]any)
	if machine["name"] != "Test Mac" || machine["platform"] != "darwin" {
		t.Errorf("the descriptor lost its display metadata: %v", machine)
	}
	// A page with a `commands` array treats it as the whole truth, so a word
	// this daemon cannot answer must not be in it, and every word it can
	// answer must be.
	var advertised []string
	for _, word := range machine["commands"].([]any) {
		advertised = append(advertised, word.(string))
	}
	sort.Strings(advertised)
	want := append([]string(nil), cloudops.Implemented()...)
	sort.Strings(want)
	if strings.Join(advertised, ",") != strings.Join(want, ",") {
		t.Errorf("the descriptor advertised %v, this daemon implements %v", advertised, want)
	}
	if _, ok := body["at"]; !ok {
		// A payload whose only key is `cloud_status` is read as a notice
		// rather than a snapshot; `at` is what makes this one a snapshot.
		t.Error("the descriptor carried no `at`")
	}
}

func TestTheInventoryNamesEveryRowItPublished(t *testing.T) {
	out := &collector{}
	publisher := newPublisher(&fixedRouter{body: completeScan}, out)
	publisher.firstPass(context.Background())

	names := out.channels()
	if len(names) != 3 {
		t.Fatalf("expected the descriptor, one row and the marker; got %v", names)
	}
	// The marker is published last: a viewer that reads it and then asks for
	// those rows must never ask for one this machine has not sent.
	if names[len(names)-1] != "s/mac-01/__clawdline_inventory_v1__" {
		t.Errorf("the inventory was not published last: %v", names)
	}
	row := out.payload(t, "s/mac-01/%2519")
	if _, ok := row["session"].(map[string]any); !ok {
		t.Errorf("a row was not wrapped as {session,at,scan}: %v", row)
	}

	marker := out.payload(t, "s/mac-01/__clawdline_inventory_v1__")
	inventory, _ := marker["inventory"].(map[string]any)
	if len(inventory) != 2 || inventory["version"] != float64(1) {
		// The consumer compares the sorted key list literally and throws
		// `bad_payload` on the whole envelope for a third key.
		t.Errorf("the inventory object must hold exactly version and sessions: %v", inventory)
	}
	ids, _ := inventory["sessions"].([]any)
	if len(ids) != 1 || ids[0] != "%19" {
		t.Errorf("the inventory named %v", ids)
	}
	// `features` is only carried when this daemon can answer one, and it never
	// goes inside `inventory`.
	if _, inside := inventory["features"]; inside {
		t.Error("features was published inside the inventory object")
	}
	if words, ok := marker["features"]; ok && len(Features()) == 0 {
		t.Errorf("features was published while this daemon implements none: %v", words)
	}
}

// A partial reading publishes its rows and the marker both. What it may not do
// is name fewer ids than a viewer can account for: the ids it did not see are
// carried unless the source that owns them answered for itself this pass
// (inventoryIDs, and the tests under "the deletion barrier" below).
func TestAPartialScanPublishesRowsAndAnInventoryOfWhatItCanAccountFor(t *testing.T) {
	partial := strings.Replace(completeScan, `"complete":true`, `"complete":false`, 1)
	out := &collector{}
	publisher := newPublisher(&fixedRouter{body: partial}, out)
	publisher.firstPass(context.Background())

	if len(out.channels()) != 3 {
		t.Errorf("expected the descriptor, one row and the marker: %v", out.channels())
	}
	if names := inventoryNames(t, out); len(names) != 1 || names[0] != "%19" {
		t.Errorf("the marker named %v", names)
	}
}

func TestAFreshnessOnlyChangeIsNotRepublished(t *testing.T) {
	router := &fixedRouter{body: completeScan}
	out := &collector{}
	publisher := newPublisher(router, out)
	publisher.firstPass(context.Background())
	out.reset()

	// Only the three paths that move with every reading of this machine.
	router.set(strings.NewReplacer(
		`"observed_at":1`, `"observed_at":99`,
		`"session_generation":2`, `"session_generation":98`,
		`"observed_at":3`, `"observed_at":97`,
	).Replace(completeScan))
	publisher.Pass(context.Background())
	if names := out.channels(); len(names) != 0 {
		t.Errorf("a row nobody reads differently was republished: %v", names)
	}

	// Something a viewer does read.
	out.reset()
	router.set(strings.Replace(completeScan, `"state":"open"`, `"state":"closing"`, 1))
	publisher.Pass(context.Background())
	if names := out.channels(); len(names) != 1 || names[0] != "s/mac-01/%2519" {
		t.Errorf("a changed row was not republished: %v", names)
	}
}

func TestASessionListThisMacCannotReadPublishesNothing(t *testing.T) {
	out := &collector{}
	publisher := newPublisher(&fixedRouter{body: `{}`, status: http.StatusServiceUnavailable}, out)
	publisher.firstPass(context.Background())

	for _, name := range out.channels() {
		if strings.HasPrefix(name, "s/") {
			// An empty inventory is a claim, and the claim "this Mac has no
			// sessions" tombstones every row a viewer holds.
			t.Fatalf("a refused session list still published %s", name)
		}
	}
}

// ---------- the deletion barrier when a source cannot be read ----------

// scanRow is one row of a built `/v1/sessions` body.
type scanRow struct{ id, tty, backend string }

// scanBody builds a reading: the rows, and the scan's own words about itself.
// `sources` is each source's own completeness, which is the only thing that
// can prove one of these ids absent.
func scanBody(t *testing.T, complete bool, sources map[string]bool, rows ...scanRow) string {
	t.Helper()
	var names []string
	for name := range sources {
		names = append(names, name)
	}
	sort.Strings(names)
	listed := make([]map[string]any, 0, len(names))
	for _, name := range names {
		listed = append(listed, map[string]any{"source": name, "complete": sources[name]})
	}
	sessions := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		sessions = append(sessions, map[string]any{
			"id": row.id, "tty": row.tty, "backend": row.backend, "assistant": "claude",
			"closeability": map[string]any{"state": "open"},
		})
	}
	body, err := json.Marshal(map[string]any{
		"at": 17,
		"scan": map[string]any{
			"complete": complete, "emptyAuthoritative": false, "sources": listed,
		},
		"sessions": sessions,
	})
	if err != nil {
		t.Fatalf("the fixture would not encode: %v", err)
	}
	return string(body)
}

// inventoryNames is the ids the last published marker names.
func inventoryNames(t *testing.T, out *collector) []string {
	t.Helper()
	marker := out.payload(t, "s/mac-01/"+InventorySessionID)
	inventory, _ := marker["inventory"].(map[string]any)
	ids, _ := inventory["sessions"].([]any)
	var names []string
	for _, id := range ids {
		names = append(names, id.(string))
	}
	return names
}

func holds(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}

// The core behaviour change. One unreadable iTerm2 window made every reading
// on this machine incomplete, and a reading that says nothing about the set
// leaves a viewer holding rows nothing can ever take back.
func TestAPartialScanStillPublishesTheInventory(t *testing.T) {
	out := &collector{}
	body := scanBody(t, false, map[string]bool{"ps": true, "tmux": true, "iterm": false},
		scanRow{id: "GUID-A", tty: "ttys001", backend: "iterm"})
	publisher := newPublisher(&fixedRouter{body: body}, out)
	publisher.firstPass(context.Background())

	if names := inventoryNames(t, out); len(names) != 1 || names[0] != "GUID-A" {
		t.Errorf("a partial reading with nothing remembered named %v", names)
	}
}

// An id whose own source could not be read this pass is not disproved by this
// pass, so it is carried: this is the half of the barrier that has to stay.
func TestAnIDIsKeptWhileItsOwnSourceIsUnreadable(t *testing.T) {
	out := &collector{}
	router := &fixedRouter{body: scanBody(t, true, map[string]bool{"ps": true, "tmux": true, "iterm": true},
		scanRow{id: "GUID-A", tty: "ttys001", backend: "iterm"},
		scanRow{id: "GUID-B", tty: "ttys002", backend: "iterm"})}
	publisher := newPublisher(router, out)
	publisher.firstPass(context.Background())

	// iTerm2 can no longer be read and GUID-B is not in this reading, while a
	// tmux pane that was not there before is. The new pane is what makes this
	// a pass that *must* state the set: a publisher that says nothing about
	// it leaves a viewer holding a list with no row for a live session, which
	// is the failure this whole barrier exists to prevent. The reset is so
	// that only this pass's marker is read.
	out.reset()
	router.set(scanBody(t, false, map[string]bool{"ps": true, "tmux": true, "iterm": false},
		scanRow{id: "GUID-A", tty: "ttys001", backend: "iterm"},
		scanRow{id: "%42", tty: "ttys003", backend: "tmux"}))
	publisher.Pass(context.Background())

	names := inventoryNames(t, out)
	if !holds(names, "GUID-B") {
		t.Errorf("an unseen iTerm2 row was tombstoned while iTerm2 could not be read: %v", names)
	}
	if !holds(names, "GUID-A") || !holds(names, "%42") {
		t.Errorf("a row this reading did see was left out: %v", names)
	}
}

// The other half, and the one the old barrier refused to say: a source that
// answered completely and did not name the id has proved it gone. tmux and
// the process table keep working while iTerm2 does not.
func TestAnIDIsDroppedWhenItsOwnSourceAnsweredCompletely(t *testing.T) {
	out := &collector{}
	router := &fixedRouter{body: scanBody(t, true, map[string]bool{"ps": true, "tmux": true, "iterm": true},
		scanRow{id: "%19", tty: "ttys001", backend: "tmux"},
		scanRow{id: "GUID-A", tty: "ttys002", backend: "iterm"})}
	publisher := newPublisher(router, out)
	publisher.firstPass(context.Background())

	// The reading as a whole is incomplete, but tmux answered for itself and
	// its pane is gone. A row the process table found is new this pass, so
	// the set changes whether or not the pane is dropped and this pass has to
	// state it either way — which is what makes the assertion below about the
	// pane and not merely about whether anything was published. The reset is
	// so that only this pass's marker is read.
	out.reset()
	router.set(scanBody(t, false, map[string]bool{"ps": true, "tmux": true, "iterm": false},
		scanRow{id: "GUID-A", tty: "ttys002", backend: "iterm"},
		scanRow{id: "ttys009", tty: "ttys009", backend: "iterm"}))
	publisher.Pass(context.Background())

	names := inventoryNames(t, out)
	if holds(names, "%19") {
		t.Errorf("a pane tmux answered completely about was kept alive by another source's failure: %v", names)
	}
	if !holds(names, "GUID-A") || !holds(names, "ttys009") {
		t.Errorf("a row this reading did see was left out: %v", names)
	}
}

// Identity degrades: when iTerm2 cannot be read the process table publishes
// the same session again under its tty. Carrying the remembered GUID as well
// would split one session into two rows on screen.
func TestACarriedIDIsDroppedWhenItsTerminalIsAlreadyListed(t *testing.T) {
	out := &collector{}
	router := &fixedRouter{body: scanBody(t, true, map[string]bool{"ps": true, "tmux": true, "iterm": true},
		scanRow{id: "GUID-A", tty: "ttys001", backend: "iterm"})}
	publisher := newPublisher(router, out)
	publisher.firstPass(context.Background())

	// iTerm2 is unreadable, so the same session arrives as the process
	// table's degraded row: its id is its tty. The set changes, so this pass
	// must state it; the reset is so that only this pass's marker is read.
	out.reset()
	router.set(scanBody(t, false, map[string]bool{"ps": true, "tmux": true, "iterm": false},
		scanRow{id: "ttys001", tty: "ttys001", backend: "iterm"}))
	publisher.Pass(context.Background())

	names := inventoryNames(t, out)
	if holds(names, "GUID-A") {
		t.Errorf("one session was named twice, once per identity: %v", names)
	}
	if len(names) != 1 || names[0] != "ttys001" {
		t.Errorf("the degraded row was not the one named: %v", names)
	}
}
