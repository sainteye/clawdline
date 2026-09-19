package cloud

// What this machine says without being asked.
//
// The three waves before this one built the half where a viewer asks and this
// machine answers. That half alone shows a hosted console **nothing**: the
// machine list is not an API route — `cloud-client.js`'s `machines()` is a
// local computation over the `orch/` and `s/` envelopes it has decrypted — and
// the session list is the `s/<machine>/__clawdline_inventory_v1__` snapshot
// plus one `s/<machine>/<session>` row per session. A machine that publishes
// neither is signed in, connected, acked, and invisible.
//
// So this file is the other half, and it is a port of
// `CloudAppBridge.publishSessionsOwned` (`:1958-2050`) and
// `OrchestratorPersistence.orchestratorSnapshot` (`:532-577`), narrowed to
// what this daemon can state truthfully today.
//
// Three properties carried across deliberately:
//
//   - **A snapshot is whole, never a diff** (docs/cloud-wire.md §2.2). `s/` and
//     `orch/` are latest-value channels: the spool coalesces a newer one over
//     an older queued one, and nothing is lost because the newer one says
//     everything.
//   - **The inventory names ids whose rows were published.** It is published
//     after the rows, not before, so a viewer that reads the marker and then
//     asks for those rows never asks for one this machine has not sent.
//   - **An unchanged row is not re-sent.** The Swift bridge compares each row's
//     identity bytes and skips the ones nobody would read differently; a Mac
//     with nine idle sessions otherwise republishes nine envelopes a tick, for
//     ever, and every one of them is billed and fanned out.

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"sort"
	"time"

	"github.com/sainteye/clawdline-go/internal/app/cloudops"
	domaincloud "github.com/sainteye/clawdline-go/internal/domain/cloud"
)

// The inventory marker's literal session id and the bound on how many ids it
// may carry, `CloudAppBridge.swift:1373-1374`. A page holds this same literal
// (`cloud-client.js`), so it is spelled here exactly once.
const (
	InventorySessionID = "__clawdline_inventory_v1__"
	InventoryLimit     = 512
)

// SnapshotInterval is how often the publisher looks. The Swift bridge publishes
// on change and rate-limits refreshes to one pass per five seconds
// (`CloudAppBridge.swift:1438`); this daemon has no change feed into the Cloud
// path yet, so it polls at that same rate and leans on the unchanged-row skip
// to keep the wire quiet.
const SnapshotInterval = 5 * time.Second

// FeatureWords is `CloudAppBridge.swift:1436`'s `cloudFeatures`: the words a
// newer Mac answers that an older one does not. A page sends such a word only
// to a machine that listed it.
var FeatureWords = []string{"sessions.snapshot", "board.items"}

// Features is the subset of those this daemon can actually answer.
//
// It is **computed, not copied**, and today it is empty: `sessions.snapshot`
// is not in this daemon's vocabulary (this publisher polls instead of being
// asked) and `board.items` is one of the nine words `cloudops` answers
// `unknown_command`. Advertising either would buy a refusal per tap rather
// than a feature — and worse than a refusal, because a page that has been told
// a machine implements a word and is then refused records it as a fault rather
// than as an absence.
//
// An empty list is published as no `features` key at all, which is what the
// Linux runtime already does and what the consumer treats as "this machine has
// none of them".
func Features() []string {
	var out []string
	for _, word := range FeatureWords {
		for _, implemented := range cloudops.Implemented() {
			if word == implemented {
				out = append(out, word)
				break
			}
		}
	}
	return out
}

// Publisher keeps the snapshot channels current.
type Publisher struct {
	// MachineID is the channel owner: `s/<machine>/…` and `orch/<machine>`.
	MachineID string
	// MachineName is the display name a person picks this Mac out by. It is
	// display metadata and never routes anything.
	MachineName string
	// Platform and Version fill the descriptor's `platform` and the app stamp.
	Platform string
	Version  string
	// Router is this daemon's own routes — the same in-process dispatch a Cloud
	// read goes through, so the rows a viewer sees are the rows the local
	// console sees.
	Router cloudops.LocalRouter
	// Publish is where a snapshot goes. It is the Relay in production.
	Publish func(context.Context, Outbound) error
	// Every is the poll interval; zero is SnapshotInterval.
	Every time.Duration
	Log   func(format string, args ...any)

	// published is each row's identity bytes as last sent, so an unchanged row
	// is skipped. Keyed by session id; the inventory and the descriptor have
	// their own entries.
	published map[string][32]byte
	// sent is when each key last went out, so a value that has not changed for
	// a long time is still re-stated. A viewer treats a machine whose snapshot
	// is older than five minutes as stale and will not auto-select it
	// (`cloud-client.js`'s MACHINE_INVENTORY_FRESH_MS), so silence is not free.
	sent map[string]time.Time

	// listed is the sessions a viewer holds rows for, which decides whether a
	// finished task is still reachable (tasklist.go).
	listed map[string]bool
	// tasks is the task list as last projected, carried on the descriptor.
	// Nil or empty is no `tasks` key at all.
	tasks []map[string]any
	// omitted is how many reachable records the last projection's bounds left
	// out, so the log says it when it changes rather than every pass.
	omitted int
}

// Heartbeat is how often an unchanged value is published anyway. The Linux
// runtime re-states its descriptor every 240 seconds for this reason; the
// viewer's staleness threshold is 300.
const Heartbeat = 240 * time.Second

// freshnessOnlySessionRowFields are the row paths that move with every reading
// of this machine whether or not anything a viewer reads moved
// (`CloudAppBridge.swift:1418-1422`). They are removed before a row is
// compared, and kept in the row that is published.
var freshnessOnlySessionRowFields = [][]string{
	{"closeability", "observed_at"},
	{"closeability", "session_generation"},
	{"closeability", "source", "observed_at"},
}

// descriptorKey names the descriptor's entry in the change map. It cannot
// collide with a session id: a session id is a terminal's, and `orch` is not
// one.
const descriptorKey = "orch"

// Run publishes until ctx is done.
//
// The descriptor goes out first and once per pass: a viewer that has no
// descriptor for this machine will not send it most words at all
// (`cloud-client.js`'s `_machineImplements` falls back to two universal
// commands), so a session list that arrived before the descriptor would be a
// list nobody could open.
func (p *Publisher) Run(ctx context.Context) error {
	every := p.Every
	if every <= 0 {
		every = SnapshotInterval
	}
	p.published = map[string][32]byte{}
	p.sent = map[string]time.Time{}
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	p.pass(ctx)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			p.pass(ctx)
		}
	}
}

// Pass publishes one round. It is exported so that a `sessions.snapshot`
// request, when this daemon answers one, has something to call.
func (p *Publisher) Pass(ctx context.Context) { p.pass(ctx) }

// The sessions are read before the descriptor goes out and published after it:
// which finished tasks the descriptor's task list carries depends on which
// sessions a viewer will hold, and the descriptor still has to arrive first.
func (p *Publisher) pass(ctx context.Context) {
	reading, ok := p.readSessions(ctx)
	if ok {
		p.noteListed(reading.ids, reading.authoritative())
	}
	p.refreshTasks(ctx)
	p.publishDescriptor(ctx)
	if ok {
		p.publishSessions(ctx, reading)
	}
}

// publishDescriptor puts this machine in the viewer's machine list, with the
// task list a viewer groups its sessions by (tasklist.go).
func (p *Publisher) publishDescriptor(ctx context.Context) {
	// `commands` is stated explicitly rather than left to be guessed from
	// `platform`. A page with no `commands` array decides what to send by
	// platform string, and anything it does not recognise gets **nothing** —
	// no request, no refusal, no network traffic to read afterwards. Naming
	// the words is both the honest answer and the debuggable one.
	snapshot := map[string]any{
		"at":  time.Now().Unix(),
		"app": map[string]any{"version": p.Version, "build": p.Version, "protocol": domaincloud.EnvelopeVersion},
		"machine": map[string]any{
			"name":     p.MachineName,
			"platform": p.platform(),
			"commands": cloudops.Implemented(),
		},
	}
	// A machine with no task a viewer can reach publishes the descriptor it
	// always did. The hosted console replaces a machine's whole snapshot with
	// each one it receives, so a missing `tasks` is read as none, the same as
	// an empty list, and costs nothing on a machine that never dispatches.
	if len(p.tasks) > 0 {
		snapshot["tasks"] = p.tasks
	}
	body, err := json.Marshal(snapshot)
	if err != nil {
		p.logf("cloud: the machine descriptor could not be encoded: %v", err)
		return
	}
	// `at` moves every pass and nothing a viewer reads moves with it
	// (`freshnessOnlyOrchestratorFields`), so the comparison is made without
	// it and the heartbeat is what keeps the machine from going stale.
	identity := map[string]any{"app": snapshot["app"], "machine": snapshot["machine"], "tasks": snapshot["tasks"]}
	if !p.changed(descriptorKey, mustJSON(identity)) {
		return
	}
	p.send(ctx, "orch/"+cloudops.ChannelSegment(p.MachineID), body, "orch")
}

// sessionReading is one reading of this machine's own session list.
type sessionReading struct {
	sessions []map[string]any
	at, scan json.RawMessage
	// ids are the sessions that will be published, in the order read.
	ids []string
	// complete and emptyAuthoritative are the scan's own words for whether
	// the list is the whole set.
	complete, emptyAuthoritative bool
}

// authoritative is whether this reading publishes the inventory, which is what
// tombstones a row a viewer holds.
func (r sessionReading) authoritative() bool {
	return (r.complete || r.emptyAuthoritative) && len(r.ids) <= InventoryLimit
}

// readSessions reads this machine's sessions, or answers false.
func (p *Publisher) readSessions(ctx context.Context) (sessionReading, bool) {
	res, err := p.Router.Do(ctx, cloudops.LocalRequest{Method: http.MethodGet, Path: "/v1/sessions"})
	if err != nil || res.Status != http.StatusOK {
		// A machine that cannot read its own sessions publishes nothing rather
		// than an empty list: an empty inventory is a claim, and the claim
		// "this Mac has no sessions" would tombstone every row a viewer holds.
		p.logf("cloud: this machine's own session list could not be read: status=%d err=%v", res.Status, err)
		return sessionReading{}, false
	}
	var root struct {
		Sessions []map[string]any `json:"sessions"`
		At       json.RawMessage  `json:"at"`
		Scan     json.RawMessage  `json:"scan"`
	}
	var scan struct {
		Complete           bool `json:"complete"`
		EmptyAuthoritative bool `json:"emptyAuthoritative"`
	}
	if err := json.Unmarshal(res.Body, &root); err != nil {
		p.logf("cloud: this machine's own session list was unreadable: %v", err)
		return sessionReading{}, false
	}
	_ = json.Unmarshal(root.Scan, &scan)
	reading := sessionReading{sessions: root.Sessions, at: root.At, scan: root.Scan,
		complete: scan.Complete, emptyAuthoritative: scan.EmptyAuthoritative}
	for _, session := range root.Sessions {
		id, _ := session["id"].(string)
		if id == "" || id == InventorySessionID {
			continue
		}
		reading.ids = append(reading.ids, id)
	}
	return reading, true
}

// publishSessions puts this machine's sessions in the viewer's list.
func (p *Publisher) publishSessions(ctx context.Context, reading sessionReading) {
	ids := make([]string, 0, len(reading.sessions))
	for _, session := range reading.sessions {
		id, _ := session["id"].(string)
		if id == "" || id == InventorySessionID {
			continue
		}
		ids = append(ids, id)
		row, err := json.Marshal(map[string]any{"session": session, "at": reading.at, "scan": reading.scan})
		if err != nil {
			continue
		}
		// The comparison drops the fields that move with every reading of this
		// machine and that no viewer reads; the row that goes out keeps them.
		if !p.changed(id, mustJSON(withoutFreshness(session))) {
			continue
		}
		p.send(ctx, "s/"+cloudops.ChannelSegment(p.MachineID)+"/"+cloudops.ChannelSegment(id), row, "session "+id)
	}
	sort.Strings(ids)
	// **The inventory is a deletion barrier, not a hint.** A viewer drops every
	// row of this machine that the marker does not name, so a scan that missed
	// a session would delete a session the person is looking at. The Swift
	// bridge publishes it only from an authoritative reading
	// (`CloudAppBridge.swift:1974-2031`) and so does this: a partial scan
	// publishes its rows and says nothing about the set.
	if !reading.complete && !reading.emptyAuthoritative {
		return
	}
	if len(ids) > InventoryLimit {
		// The bound is the Swift bridge's and is a refusal rather than a
		// truncation: half an inventory is a list that says sessions were
		// removed, which is worse than saying nothing this pass.
		p.logf("cloud: %d sessions is past the %d the inventory may name", len(ids), InventoryLimit)
		return
	}
	marker := map[string]any{
		// `inventory` must hold exactly `version` and `sessions`: the consumer
		// compares the sorted key list literally and throws `bad_payload` on
		// the whole envelope for a third key. `features` therefore sits beside
		// it, never inside it (`CloudAppBridge.swift:1881-1888`).
		"inventory": map[string]any{"version": 1, "sessions": ids},
	}
	if words := Features(); len(words) > 0 {
		marker["features"] = words
	}
	inventory, err := json.Marshal(marker)
	if err != nil {
		return
	}
	if !p.changed(InventorySessionID, inventory) {
		return
	}
	p.send(ctx, "s/"+cloudops.ChannelSegment(p.MachineID)+"/"+InventorySessionID, inventory, "inventory")
}

// changed reports whether this key is due to be published: because what a
// viewer reads differs from last time, or because the heartbeat came due.
func (p *Publisher) changed(key string, identity []byte) bool {
	sum := sha256.Sum256(identity)
	now := time.Now()
	if p.published[key] == sum && now.Sub(p.sent[key]) < Heartbeat {
		return false
	}
	p.published[key], p.sent[key] = sum, now
	return true
}

// withoutFreshness copies a row with the freshness-only paths removed.
func withoutFreshness(row map[string]any) map[string]any {
	out := make(map[string]any, len(row))
	for key, value := range row {
		out[key] = value
	}
	for _, path := range freshnessOnlySessionRowFields {
		removePath(out, path)
	}
	return out
}

// removePath deletes one nested key, copying each object it descends into so
// that the row about to be published is not the one being edited.
func removePath(object map[string]any, path []string) {
	if len(path) == 0 {
		return
	}
	if len(path) == 1 {
		delete(object, path[0])
		return
	}
	child, ok := object[path[0]].(map[string]any)
	if !ok {
		return
	}
	copied := make(map[string]any, len(child))
	for key, value := range child {
		copied[key] = value
	}
	removePath(copied, path[1:])
	object[path[0]] = copied
}

// mustJSON encodes for comparison only. An object that will not encode
// compares as the empty document, which makes it look unchanged — which is the
// safe direction: the row itself is encoded separately and a failure there is
// reported.
func mustJSON(value any) []byte {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return encoded
}

func (p *Publisher) send(ctx context.Context, channel string, body []byte, what string) {
	if p.Publish == nil {
		return
	}
	out := Outbound{Channel: channel, Class: string(domaincloud.ClassStream), Payload: body}
	if err := p.Publish(ctx, out); err != nil {
		p.logf("cloud: the %s snapshot was not published: %v", what, err)
	}
}

func (p *Publisher) platform() string {
	if p.Platform != "" {
		return p.Platform
	}
	return "unknown"
}

func (p *Publisher) logf(format string, args ...any) {
	if p.Log != nil {
		p.Log(format, args...)
	}
}
