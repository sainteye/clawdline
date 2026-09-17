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
	t.Fatalf("nothing was published on %s; saw %v", channel, c.channels())
	return nil
}

func (c *collector) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.out = nil
}

const completeScan = `{"at":17,"scan":{"complete":true},"sessions":[
  {"id":"%195","assistant":"claude","tty":"ttys001",
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
	row := out.payload(t, "s/mac-01/%25195")
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
	if len(ids) != 1 || ids[0] != "%195" {
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

func TestAPartialScanPublishesRowsAndNoInventory(t *testing.T) {
	partial := strings.Replace(completeScan, `"complete":true`, `"complete":false`, 1)
	out := &collector{}
	publisher := newPublisher(&fixedRouter{body: partial}, out)
	publisher.firstPass(context.Background())

	for _, name := range out.channels() {
		if strings.HasSuffix(name, InventorySessionID) {
			// The marker is a deletion barrier: a viewer drops every row it
			// does not name, so a scan that missed a session would delete the
			// session somebody is looking at.
			t.Fatalf("an incomplete scan published the inventory: %v", out.channels())
		}
	}
	if len(out.channels()) != 2 {
		t.Errorf("expected the descriptor and one row: %v", out.channels())
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
	if names := out.channels(); len(names) != 1 || names[0] != "s/mac-01/%25195" {
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
