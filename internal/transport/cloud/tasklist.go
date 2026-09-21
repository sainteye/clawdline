package cloud

// The task list a Cloud viewer draws the main and child sessions from.
//
// A hosted console groups a child session under the session that dispatched
// it, indents it, and puts the "child session · running" chip on the root —
// and every one of those reads `tasks` out of the machine's `orch/` snapshot
// (`view/derive.js`: `taskOfChild`, `taskShaping`, `rowDepth`, `tasksOfRoot`,
// `grouped`, `featureRootChip`, `lostIfClosed`). A machine whose snapshot
// carries no `tasks` is drawn as a flat list of sessions, whatever it has
// dispatched. This file is that list, ported from the Swift app's
// `RemoteServer.cloudOrchestratorProjection` (`OrchestratorPersistence.swift`)
// and docs/cloud.md §"The `orch/` snapshot a Cloud viewer is sent" in that
// repository. The hosted console is the Swift app's and is not changed here, so
// the keys and the shape are its own.
//
// **Records.** A task goes out while it is unfinished, or while its child
// terminal is a session this machine has published and not tombstoned. Every
// Cloud reader of a finished task reaches it through its child: the header and
// the row through `taskOfChild` of a listed session, and the chips, indents and
// grouping through `taskShaping`, which is false for a finished task whose
// child is not a listed session. A root's own chip counts its children through
// `taskShaping` too, so the root side keeps nothing. A record with no state, or
// one this build does not know, is kept: one task too many costs bytes, one
// dropped that a viewer still wanted is a row that silently loses its header.
//
// **Fields.** Of a record only the nine paths in cloudTaskFields. The Swift
// app's own list route started as a deny-list — it dropped `summary`,
// `review`, `graph` and `progress` — and everything else rode along: measured
// there on 2026-09-16, a connect snapshot was 202,105 bytes for 91 tasks, of
// which `completion_delivery` was 317 bytes a task, `landing` 276, `executor`
// 240, `verification` 167 and `worktree` 115. No viewer opens one of those. Its
// conclusion was an allow-list of what the viewer reads, checked reader by
// reader, and that is what is ported: a field this daemon adds to a task row
// later does not reach Cloud until somebody adds it here on purpose.
//
// **Bounds** (docs/design-guidelines.md DG-2), measured in tasklist_test.go
// through the real relay's sealing:
//
//   - The rows read are the page the local console's own stream reads: every
//     unfinished task and the newest taskReadLimit finished ones. A viewer is
//     therefore never shown a finished task the console on this machine does
//     not show. A finished task older than that page whose child tab is still
//     open draws on Cloud without its header, as it does locally.
//   - At most TaskListLimit records and TaskListBytesLimit bytes of them go
//     out. Past either, the oldest finished records are left out first and
//     then the oldest unfinished ones — an unfinished task is what the running
//     chip and "lost if closed" warning are drawn from, and a finished one is
//     only an indent. Nothing accumulates: each snapshot is recomputed whole
//     from the route, so being over a bound this pass costs nothing next pass.
//     The log says so when the number left out changes, not every pass.
//   - The normal case is far under both: the broker runs at most 20 tasks at
//     once on a machine, and a finished task is only kept while its child tab
//     is open. Measured through the relay's sealing: a projected record is
//     about 318 bytes with a typical title; 10 tasks add 3,181 bytes to the
//     snapshot (315 → 3,505 plaintext, 680 → 4,932 sealed), 100 add 31,891
//     (32,215 plaintext, 43,212 sealed), and 100 with 200-character titles
//     85,301 (114,424 sealed). The task route's own rows for those 100 are
//     92,209 bytes.
//
// **Cadence.** The list rides on the descriptor, which is compared without
// `at` and re-stated on the heartbeat, so it goes out when what a viewer reads
// in it changed — at most once a pass, five seconds — and every 240 seconds
// otherwise. That is the Swift bridge's rule (change-only, five-second floor,
// a presence re-send every three minutes) on this daemon's clock. An executor's
// `observed_at` or a record's `landing` moving is not a change here, because
// the projection does not carry them.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/sainteye/clawdline/internal/app/cloudops"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
)

const (
	// TaskListLimit is how many task records one `orch/` snapshot carries.
	TaskListLimit = 100
	// TaskListBytesLimit is how many bytes those records may take, encoded.
	// With the row limit reached and every title at the broker's 200-character
	// limit the list is 85,301 bytes (tasklist_test.go), so this binds only on
	// records whose titles were not admitted by this daemon.
	TaskListBytesLimit = 128 << 10
	// taskReadLimit is the finished tasks one pass reads: the page the local
	// console's stream reads (transport/http's tasksPayload(ctx, 0, 50)).
	taskReadLimit = 50
)

// cloudTaskFields are the task-record paths a Cloud viewer reads
// (`RemoteServer.cloudTaskFields`, checked there against every reader in the
// hosted console on 2026-09-15): `view/derive.js` reads `state`, `finishedAt`,
// `child.terminalId`, `created`, `root.terminalId`, `title` and `id`;
// `view/list.js` reads `id` and `title`; `view/transcript.js` reads `title`
// and `usage`'s `total` and `costUsd`.
var cloudTaskFields = [][]string{
	{"id"}, {"state"}, {"title"}, {"created"}, {"finishedAt"},
	{"child", "terminalId"}, {"root", "terminalId"},
	{"usage", "total"}, {"usage", "costUsd"},
}

// readTasks is this machine's task list as its own route answers it, or false
// when the route could not be read. **A list that could not be read is not an
// empty list** (DG-7): the caller keeps the last one it projected, because a
// snapshot without `tasks` tells a viewer this machine has none running.
func (p *Publisher) readTasks(ctx context.Context) ([]map[string]any, bool) {
	res, err := p.Router.Do(ctx, cloudops.LocalRequest{
		Method: http.MethodGet, Path: "/v1/orchestrator/tasks",
		Query: map[string]string{"limit": strconv.Itoa(taskReadLimit)},
	})
	if err != nil || res.Status != http.StatusOK {
		p.logf("cloud: this machine's own task list could not be read: status=%d err=%v", res.Status, err)
		return nil, false
	}
	var root struct {
		Tasks []map[string]any `json:"tasks"`
	}
	// Numbers are copied as the route wrote them: a projection does not
	// round a timestamp or a token count on its way through.
	decoder := json.NewDecoder(bytes.NewReader(res.Body))
	decoder.UseNumber()
	if err := decoder.Decode(&root); err != nil {
		p.logf("cloud: this machine's own task list was unreadable: %v", err)
		return nil, false
	}
	return root.Tasks, true
}

// projectTasks is the list a viewer is sent: the records it can reach, each cut
// to the fields it reads, in the route's order (unfinished first, then
// finished, each newest first), bounded. It answers how many relevant records
// the bounds left out.
func projectTasks(rows []map[string]any, listed map[string]bool) ([]map[string]any, int) {
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if cloudTaskRelevant(row, listed) {
			out = append(out, cloudTaskProjection(row))
		}
	}
	relevant := len(out)
	if len(out) > TaskListLimit {
		out = out[:TaskListLimit]
	}
	// The byte bound counts each record as it will be encoded plus the comma
	// between two, which is what the records add to the snapshot.
	size := 0
	for i, row := range out {
		size += len(mustJSON(row))
		if i > 0 {
			size++
		}
		if size > TaskListBytesLimit {
			out = out[:i]
			break
		}
	}
	return out, relevant - len(out)
}

// cloudTaskRelevant is `RemoteServer.cloudTaskRelevant`: kept while unfinished
// (or of a state this build does not know), otherwise only while its child
// terminal is a session a viewer holds a row for.
func cloudTaskRelevant(row map[string]any, listed map[string]bool) bool {
	state, _ := row["state"].(string)
	if !orchestrator.State(state).Terminal() {
		return true
	}
	child, _ := row["child"].(map[string]any)
	terminal, _ := child["terminalId"].(string)
	return terminal != "" && listed[terminal]
}

// cloudTaskProjection is `RemoteServer.cloudTaskProjection`: only
// cloudTaskFields. A path the record does not have stays absent — `root` with
// no resolved terminal is no `root` at all, and `usage.costUsd` is left out
// rather than zero for work nothing priced — and a value is copied as it is.
func cloudTaskProjection(row map[string]any) map[string]any {
	out := map[string]any{}
	for _, path := range cloudTaskFields {
		if len(path) == 1 {
			if value, ok := row[path[0]]; ok {
				out[path[0]] = value
			}
			continue
		}
		parent, ok := row[path[0]].(map[string]any)
		if !ok {
			continue
		}
		value, ok := parent[path[1]]
		if !ok {
			continue
		}
		nested, _ := out[path[0]].(map[string]any)
		if nested == nil {
			nested = map[string]any{}
			out[path[0]] = nested
		}
		nested[path[1]] = value
	}
	return out
}

// noteListed records which sessions a viewer now holds rows for, from one
// reading of this machine's sessions. A complete reading published the
// inventory, which tombstones every row it does not name, so it replaces the
// set. A partial one — or one past the inventory's limit — published rows and
// no inventory, so a viewer still holds every row it had: it adds to the set.
func (p *Publisher) noteListed(ids []string, authoritative bool) {
	if authoritative || p.listed == nil {
		p.listed = map[string]bool{}
	}
	for _, id := range ids {
		p.listed[id] = true
	}
}

// refreshTasks reads and projects the task list for this pass. A failed read
// keeps the last projection; a changed number of records left out is logged.
func (p *Publisher) refreshTasks(ctx context.Context) {
	rows, ok := p.readTasks(ctx)
	if !ok {
		return
	}
	projected, omitted := projectTasks(rows, p.listed)
	if omitted != p.omitted {
		if omitted > 0 {
			p.logf("cloud: the orch snapshot carries %d task records and leaves out the %d oldest, past %d records or %d bytes",
				len(projected), omitted, TaskListLimit, TaskListBytesLimit)
		} else {
			p.logf("cloud: the orch snapshot carries every task record a viewer can reach again (%d)", len(projected))
		}
		p.omitted = omitted
	}
	p.tasks = projected
}
