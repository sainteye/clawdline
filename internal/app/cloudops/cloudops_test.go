package cloudops

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// The pane id is `%19` in every one of these on purpose. It is the id a tmux
// session really has, it is the one character that makes a path a different
// path when it is not escaped, and reading it raw once turned a session id
// into a control character and answered `not_found` — the wrong answer to the
// wrong question.
const pane = "%19"

const taskID = "7a000000-0000-4000-8000-000000000001"

// scheduleID is a schedule file's own id, which is a uuid too and is not a
// task's: the two travel on neighbouring words and a test that used one
// constant for both would pass while they disagreed.
const scheduleID = "5c000000-0000-4000-8000-000000000002"
const scheduleWebhookRequestID = "5c000000-0000-4000-8000-000000000003"
const scheduleWebhookHook = "swh_00000000000000000000000000"

// snippetID is one stored snippet's own id, as `orchestrator.NewUUID` mints
// one. It is a made-up id and there is nothing of anybody's in it — which is
// the whole of what a snippet fixture in this repository may be.
const snippetID = "3b000000-0000-4000-8000-000000000004"

// pushID is one stored subscription's name, as `newPushID` writes one: this
// machine's name for a row, and not a uuid like the two above.
const pushID = "9f1c0b3a4d5e6f708192a3b4c5d6e7f8"

// verificationFixture is one verification record's id, made up here.
const verificationFixture = "7e000000-0000-4000-8000-000000000006"

// router records what it was asked and answers what the test told it to.
type router struct {
	seen     []LocalRequest
	status   int
	body     string
	media    string
	empty    bool
	fail     error
	byPrefix map[string]LocalResponse
}

func (r *router) Do(_ context.Context, req LocalRequest) (LocalResponse, error) {
	r.seen = append(r.seen, req)
	if r.fail != nil {
		return LocalResponse{}, r.fail
	}
	for prefix, res := range r.byPrefix {
		if strings.HasPrefix(req.Path, prefix) {
			return res, nil
		}
	}
	status := r.status
	if status == 0 {
		status = 200
	}
	body := r.body
	if body == "" && !r.empty {
		body = `{"ok":true}`
	}
	return LocalResponse{Status: status, Body: []byte(body), ContentType: r.media}, nil
}

func (r *router) last() LocalRequest {
	if len(r.seen) == 0 {
		return LocalRequest{}
	}
	return r.seen[len(r.seen)-1]
}

// open is a bridge with the write switch on, which is not the default anywhere
// else in this package: every test that wants an effect has to say so.
func open(r LocalRouter) Bridge {
	return Bridge{MachineID: "mac-01", Router: r, AllowCommands: func() bool { return true }}
}

func request(t *testing.T, class Class, object map[string]any) Command {
	t.Helper()
	plaintext, err := json.Marshal(object)
	if err != nil {
		t.Fatalf("that body is not JSON: %v", err)
	}
	return Command{Channel: "ctl/mac-01", Class: class, Sender: "viewer-device-01",
		Sequence: 411, Plaintext: plaintext}
}

// answerOf reads the payload back the way the hosted console reads it.
func answerOf(t *testing.T, a Answer) map[string]any {
	t.Helper()
	if len(a.Payload) == 0 {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(a.Payload, &out); err != nil {
		t.Fatalf("that payload is not JSON: %v", err)
	}
	return out
}

func errorOf(t *testing.T, a Answer) map[string]any {
	t.Helper()
	payload := answerOf(t, a)
	failure, ok := payload["error"].(map[string]any)
	if !ok {
		t.Fatalf("that answer carries no error: %s", a.Payload)
	}
	return failure
}

// TestEveryOperationIsAnsweredAsItself walks the whole vocabulary with a body
// the hosted console really sends, and asserts the two things a caller acts
// on: where the answer is published, and what this machine did with it — the
// exact local route, or the exact refusal.
//
// It is one table rather than one test per word because the failure it is
// written against is a word that quietly answers as a different word: the
// Swift bridge's own comment records that a misspelled `info` would have
// become a transcript if its switch had fallen through.
func TestEveryOperationIsAnsweredAsItself(t *testing.T) {
	machine := MachineReplySession
	cases := []struct {
		word string
		body map[string]any
		// where the answer goes
		session string
		name    string
		// what this machine did
		method string
		path   string
		query  map[string]string
		body2  string
		// or refused with
		code   string
		status int
		// what this machine answers, when the default `{"ok":true}` is not
		// something this word may carry.
		answers *router
	}{{
		word: "transcript",
		body: map[string]any{"type": "transcript", "session": pane, "limit": 200,
			"priority": "foreground"},
		session: pane, name: "transcript",
		method: "GET", path: "/v1/transcript",
		query: map[string]string{"session": pane, "limit": "200"},
	}, {
		word:    "info",
		body:    map[string]any{"type": "info", "session": pane, "parts": "summary"},
		session: pane, name: "info.summary",
		method: "GET", path: "/v1/sessions/%2519/info",
		query: map[string]string{"parts": "summary"},
	}, {
		word:    "git",
		body:    map[string]any{"type": "git", "session": pane},
		session: pane, name: "git",
		method: "GET", path: "/v1/sessions/%2519/git",
	}, {
		word: "git-diff",
		body: map[string]any{"type": "git-diff", "session": pane,
			"request": "req-diff", "path": "web/console/src/session/Detail.tsx"},
		session: pane, name: "read:req-diff",
		method: "GET", path: "/v1/sessions/%2519/git/diff",
		query: map[string]string{"path": "web/console/src/session/Detail.tsx"},
	}, {
		word:    "screen",
		body:    map[string]any{"type": "screen", "session": pane},
		session: pane, name: "screen",
		method: "GET", path: "/v1/sessions/%2519/screen",
	}, {
		word:    "image",
		body:    map[string]any{"type": "image", "session": pane, "id": "img_7f3a"},
		session: pane, name: "image.img_7f3a",
		method: "GET", path: "/v1/artifacts/images/img_7f3a",
		answers: &router{body: "\x89PNG", media: "image/png"},
	}, {
		word:    "documents",
		body:    map[string]any{"type": "documents", "session": pane},
		session: pane, name: "documents",
		method: "GET", path: "/v1/sessions/%2519/documents",
		answers: &router{body: `{"documents":[]}`},
	}, {
		word: "document",
		body: map[string]any{"type": "document", "session": pane, "request": "req-doc",
			"scope": "task", "task": taskID, "path": "report.md"},
		session: pane, name: "read:req-doc",
		method: "GET", path: "/v1/sessions/%2519/documents/task/" + taskID + "/report.md",
		answers: &router{body: "# report\n", media: "text/markdown; charset=utf-8"},
	}, {
		word:    "places",
		body:    map[string]any{"type": "places", "session": machine, "request": "req-places"},
		session: machine, name: "read:req-places",
		method: "GET", path: "/v1/places",
	}, {
		word:    "squad.catalog",
		body:    map[string]any{"type": "squad.catalog", "session": machine, "request": "req-squad-catalog"},
		session: machine, name: "read:req-squad-catalog",
		method: "GET", path: "/v1/squad/catalog",
	}, {
		word: "squad.definition",
		body: map[string]any{"type": "squad.definition", "session": machine,
			"request": "req-squad-definition", "definition_id": "backend"},
		session: machine, name: "read:req-squad-definition",
		method: "GET", path: "/v1/squad/definitions/backend",
	}, {
		word:    "squad.scopes",
		body:    map[string]any{"type": "squad.scopes", "session": machine, "request": "req-squad-scopes"},
		session: machine, name: "read:req-squad-scopes",
		method: "GET", path: "/v1/squad/scopes",
	}, {
		word: "squad.skill-sources",
		body: map[string]any{"type": "squad.skill-sources", "session": machine,
			"request": "req-squad-sources", "provider": "codex", "place_id": "place-a"},
		session: machine, name: "read:req-squad-sources",
		method: "GET", path: "/v1/squad/skill-sources",
		query: map[string]string{"provider": "codex", "place_id": "place-a"},
	}, {
		word: "squad.settings",
		body: map[string]any{"type": "squad.settings", "session": machine,
			"request": "req-squad-settings", "place_id": "place-a"},
		session: machine, name: "read:req-squad-settings",
		method: "GET", path: "/v1/squad/settings", query: map[string]string{"place_id": "place-a"},
	}, {
		word: "squad.catalog.update",
		body: map[string]any{"type": "squad.catalog.update", "session": machine,
			"request": "req-squad-catalog-write", "changes": map[string]any{"expected_version": 0}},
		session: machine, name: "action:req-squad-catalog-write",
		method: "POST", path: "/v1/squad/catalog", body2: `{"expected_version":0}`,
	}, {
		word: "squad.settings.update",
		body: map[string]any{"type": "squad.settings.update", "session": machine,
			"request": "req-squad-settings-write", "changes": map[string]any{"expected_version": 0}},
		session: machine, name: "action:req-squad-settings-write",
		method: "PUT", path: "/v1/squad/settings", body2: `{"expected_version":0}`,
	}, {
		word: "squad.motion.update",
		body: map[string]any{"type": "squad.motion.update", "session": machine,
			"request": "req-squad-motion-write", "changes": map[string]any{"expected_version": 0}},
		session: machine, name: "action:req-squad-motion-write",
		method: "PUT", path: "/v1/squad/motion", body2: `{"expected_version":0}`,
	}, {
		word: "squad.packages.preview",
		body: map[string]any{"type": "squad.packages.preview", "session": machine,
			"request": "req-package-preview", "package": map[string]any{}},
		session: machine, name: "read:req-package-preview",
		method: "POST", path: "/v1/squad-packages/preview", body2: `{}`,
	}, {
		word: "squad.packages.adopt",
		body: map[string]any{"type": "squad.packages.adopt", "session": machine,
			"request": "req-package-adopt", "package": map[string]any{}},
		session: machine, name: "action:req-package-adopt",
		method: "POST", path: "/v1/squad-packages/adopt", body2: `{}`,
	}, {
		word: "squad.packages.export",
		body: map[string]any{"type": "squad.packages.export", "session": machine,
			"request": "req-package-export", "package": map[string]any{"private_scopes": []any{}, "confirm_private": false}},
		session: machine, name: "read:req-package-export",
		method: "POST", path: "/v1/squad-packages/export", body2: `{"confirm_private":false,"private_scopes":[]}`,
	}, {
		word: "squad.packages.export.private",
		body: map[string]any{"type": "squad.packages.export.private", "session": machine,
			"request": "req-package-export-private", "package": map[string]any{"private_scopes": []any{"global"}, "confirm_private": true}},
		session: machine, name: "action:req-package-export-private",
		method: "POST", path: "/v1/squad-packages/export", body2: `{"confirm_private":true,"private_scopes":["global"]}`,
	}, {
		word:    "squad-session-bindings",
		body:    map[string]any{"type": "squad-session-bindings", "session": machine, "request": "req-bindings"},
		session: machine, name: "read:req-bindings",
		method: "GET", path: "/v1/squad/session-bindings",
	}, {
		word:    "squad-event-head",
		body:    map[string]any{"type": "squad-event-head", "session": machine, "request": "req-event-head"},
		session: machine, name: "read:req-event-head",
		method: "GET", path: "/v1/squad/events/head",
	}, {
		word:    "squad-events",
		body:    map[string]any{"type": "squad-events", "session": machine, "request": "req-events", "after": 7, "limit": 50},
		session: machine, name: "read:req-events",
		method: "GET", path: "/v1/squad/events", query: map[string]string{"after": "7", "limit": "50"},
	}, {
		word: "past-sessions",
		body: map[string]any{"type": "past-sessions", "session": machine, "request": "req-past",
			"place": "/Users/sean/code/clawdline-go", "assistant": "codex"},
		session: machine, name: "read:req-past",
		method: "GET",
		path:   "/v1/places/%2FUsers%2Fsean%2Fcode%2Fclawdline-go/sessions/codex",
	}, {
		word:    "schedules",
		body:    map[string]any{"type": "schedules", "session": machine, "request": "req-schedules"},
		session: machine, name: "read:req-schedules",
		method: "GET", path: "/v1/orchestrator/schedules",
	}, {
		// The whole machine's list, and no `?session=` on it: the wire has
		// nowhere to put a session, so this read must not put one in the query
		// either — a list filtered under a guessed session would be somebody
		// else's snippets.
		word:    "snippets",
		body:    map[string]any{"type": "snippets", "session": machine, "request": "req-snippets"},
		session: machine, name: "read:req-snippets",
		method: "GET", path: "/v1/snippets",
		answers: &router{body: `{"snippets":[]}`},
	}, {
		// Asking for the key is the first thing a phone does when somebody
		// presses "notify me", and the whole registration stops here when it
		// is not carried.
		word:    "push-key",
		body:    map[string]any{"type": "push-key", "session": machine, "request": "req-push-key"},
		session: machine, name: "read:req-push-key",
		method: "GET", path: "/v1/push/key",
	}, {
		word: "board",
		body: map[string]any{"type": "board", "session": machine, "request": "req-board",
			"project": "clawdline-go", "item": ""},
		session: machine, name: "read:req-board",
		method: "GET", path: "/v1/board",
		query: map[string]string{"project": "clawdline-go"},
	}, {
		word: "board-command",
		body: map[string]any{"type": "board-command", "session": machine, "request": "req-board-write",
			"command": map[string]any{"operation": "set_ai_consent", "provider": "codex", "enabled": true,
				"policy": "board-reading-v1", "expectedRevision": 4, "requestId": "req-board-write"}},
		session: machine, name: "action:req-board-write",
		method: "POST", path: "/v1/board",
		body2: `{"enabled":true,"expectedRevision":4,"operation":"set_ai_consent","policy":"board-reading-v1","provider":"codex","requestId":"req-board-write"}`,
	}, {
		// The same path as board; the query shape is the whole difference, and
		// the Swift app sends all four parameters every time.
		word: "board.items",
		body: map[string]any{"type": "board.items", "session": machine, "request": "req-items",
			"project": "clawdline-go", "audience": "human", "cursor": 0, "limit": 50},
		session: machine, name: "read:req-items",
		method: "GET", path: "/v1/board",
		query: map[string]string{"project": "clawdline-go", "audience": "human",
			"cursor": "0", "limit": "50"},
	}, {
		// Participation reads remain carried after the v1 work page retires.
		word: "work.proposals",
		body: map[string]any{"type": "work.proposals", "session": machine,
			"request": "req-work-proposals", "project": ""},
		session: machine, name: "read:req-work-proposals",
		method: "GET", path: "/v1/work/proposals",
	}, {
		word: "work.decisions",
		body: map[string]any{"type": "work.decisions", "session": machine,
			"request": "req-work-decisions"},
		session: machine, name: "read:req-work-decisions",
		method: "GET", path: "/v1/work/decisions",
	}, {
		word: "work.decision",
		body: map[string]any{"type": "work.decision", "session": machine,
			"request": "req-work-decision", "id": "10000000-0000-4000-8000-000000000001"},
		session: machine, name: "read:req-work-decision",
		method: "GET", path: "/v1/work/decisions/10000000-0000-4000-8000-000000000001",
	}, {
		word: "work.decision-answer",
		body: map[string]any{"type": "work.decision-answer", "session": machine,
			"request": "press-decision-1", "id": "10000000-0000-4000-8000-000000000001", "answer": "Proceed"},
		session: machine, name: "action:press-decision-1",
		method: "POST", path: "/v1/work/decisions/10000000-0000-4000-8000-000000000001",
		body2: `{"answer":"Proceed"}`,
	}, {
		word: "work.digests",
		body: map[string]any{"type": "work.digests", "session": machine,
			"request": "req-work-digests", "kind": "daily"},
		session: machine, name: "read:req-work-digests",
		method: "GET", path: "/v1/work/digests",
		query: map[string]string{"kind": "daily"},
	}, {
		word: "work.v2.item",
		body: map[string]any{"type": "work.v2.item", "session": machine,
			"request": "req-work-v2-item", "id": "w1"},
		session: machine, name: "read:req-work-v2-item",
		method: "GET", path: "/v1/work/v2/items/w1",
	}, {
		word: "work.v2.gate-export",
		body: map[string]any{"type": "work.v2.gate-export", "session": machine,
			"request": "req-gate-export", "id": "w1"},
		session: machine, name: "read:req-gate-export",
		method: "GET", path: "/v1/work/v2/items/w1/gate-export",
	}, {
		word: "usage.session",
		body: map[string]any{"type": "usage.session", "session": machine,
			"request": "req-usage-session", "id": "c0ffee00-0000-4000-8000-000000000005"},
		session: machine, name: "read:req-usage-session",
		method: "GET", path: "/v1/usage/sessions/c0ffee00-0000-4000-8000-000000000005",
	}, {
		word: "usage.task",
		body: map[string]any{"type": "usage.task", "session": machine,
			"request": "req-usage-task", "id": taskID},
		session: machine, name: "read:req-usage-task",
		method: "GET", path: "/v1/usage/tasks/" + taskID,
	}, {
		word: "usage.item",
		body: map[string]any{"type": "usage.item", "session": machine,
			"request": "req-usage-item", "id": "w1"},
		session: machine, name: "read:req-usage-item",
		method: "GET", path: "/v1/usage/items/w1",
	}, {
		word: "usage.compare-compaction",
		body: map[string]any{"type": "usage.compare-compaction", "session": machine,
			"request": "req-usage-compare", "since": "14d"},
		session: machine, name: "read:req-usage-compare",
		method: "GET", path: "/v1/usage/compare-compaction",
		query: map[string]string{"since": "14d"},
	}, {
		word: "usage.compare-handoff",
		body: map[string]any{"type": "usage.compare-handoff", "session": machine,
			"request": "req-usage-handoff", "since": "14d"},
		session: machine, name: "read:req-usage-handoff",
		method: "GET", path: "/v1/usage/compare-handoff",
		query: map[string]string{"since": "14d"},
	}, {
		word: "usage.work-units",
		body: map[string]any{"type": "usage.work-units", "session": machine,
			"request": "req-usage-work", "since": "36h"},
		session: machine, name: "read:req-usage-work",
		method: "GET", path: "/v1/usage/work-units",
		query: map[string]string{"since": "36h"},
	}, {
		word: "usage.work-samples",
		body: map[string]any{"type": "usage.work-samples", "session": machine,
			"request": "req-usage-samples", "since": "14d", "until": "1790000000"},
		session: machine, name: "read:req-usage-samples",
		method: "GET", path: "/v1/usage/work-samples",
		query: map[string]string{"since": "14d", "until": "1790000000"},
	}, {
		// Things waiting to be verified: two reads on the machine's channel
		// and five commands, each a device's press, each with its document
		// handed to the route whole.
		word:    "verification.list",
		body:    map[string]any{"type": "verification.list", "session": machine, "request": "req-verify-list"},
		session: machine, name: "read:req-verify-list",
		method: "GET", path: "/v1/verifications",
	}, {
		word: "verification.get",
		body: map[string]any{"type": "verification.get", "session": machine, "request": "req-verify-get",
			"id": verificationFixture},
		session: machine, name: "read:req-verify-get",
		method: "GET", path: "/v1/verifications/" + verificationFixture,
	}, {
		word: "verification.create",
		body: map[string]any{"type": "verification.create", "session": machine, "request": "req-verify-add",
			"verification": map[string]any{"title": "a check", "due_at": 1_790_000_000, "criteria": []any{"it holds"}}},
		session: machine, name: "action:req-verify-add",
		method: "POST", path: "/v1/verifications",
		body2: `{"criteria":["it holds"],"due_at":1790000000,"title":"a check"}`,
	}, {
		word: "verification.note",
		body: map[string]any{"type": "verification.note", "session": machine, "request": "req-verify-note",
			"id": verificationFixture, "verification": map[string]any{"text": "looked"}},
		session: machine, name: "action:req-verify-note",
		method: "POST", path: "/v1/verifications/" + verificationFixture + "/notes",
		body2: `{"text":"looked"}`,
	}, {
		word: "verification.criterion",
		body: map[string]any{"type": "verification.criterion", "session": machine, "request": "req-verify-mark",
			"id": verificationFixture, "index": 2, "verification": map[string]any{"state": "passed"}},
		session: machine, name: "action:req-verify-mark",
		method: "POST", path: "/v1/verifications/" + verificationFixture + "/criteria/2",
		body2: `{"state":"passed"}`,
	}, {
		word: "verification.close",
		body: map[string]any{"type": "verification.close", "session": machine, "request": "req-verify-close",
			"id": verificationFixture, "verification": map[string]any{"status": "accepted", "reason": "it held"}},
		session: machine, name: "action:req-verify-close",
		method: "POST", path: "/v1/verifications/" + verificationFixture + "/close",
		body2: `{"reason":"it held","status":"accepted"}`,
	}, {
		word: "verification.delete",
		body: map[string]any{"type": "verification.delete", "session": machine, "request": "req-verify-gone",
			"id": verificationFixture, "force": true},
		session: machine, name: "action:req-verify-gone",
		method: "DELETE", path: "/v1/verifications/" + verificationFixture,
		query: map[string]string{"force": "1"},
	}, {
		word: "work.v2.items",
		body: map[string]any{"type": "work.v2.items", "session": machine,
			"request": "req-work-v2-items", "project": "p1", "cursor": "page-2"},
		session: machine, name: "read:req-work-v2-items",
		method: "GET", path: "/v1/work/v2/items", query: map[string]string{"project": "p1", "cursor": "page-2"},
	}, {
		word: "work.v2.search",
		body: map[string]any{"type": "work.v2.search", "session": machine,
			"request": "req-work-v2-search", "project": "p1", "status": "done", "query": "needle", "cursor": "page-3"},
		session: machine, name: "read:req-work-v2-search",
		method: "GET", path: "/v1/work/v2/items",
		query: map[string]string{"project": "p1", "status": "done", "q": "needle", "cursor": "page-3"},
	}, {
		word: "work.v2.proposals",
		body: map[string]any{"type": "work.v2.proposals", "session": machine,
			"request": "req-work-v2-proposals", "state": "pending"},
		session: machine, name: "read:req-work-v2-proposals",
		method: "GET", path: "/v1/work/v2/proposals", query: map[string]string{"state": "pending"},
	}, {
		word: "work.v2.session-todos",
		body: map[string]any{"type": "work.v2.session-todos", "session": machine,
			"request": "req-work-v2-todos", "terminal": pane},
		session: machine, name: "read:req-work-v2-todos",
		method: "GET", path: "/v1/work/v2/session-todos/%2519",
	}, {
		word: "work.v2.human-interventions",
		body: map[string]any{"type": "work.v2.human-interventions", "session": machine,
			"request": "req-human-interventions", "terminal": "conversation:10000000-0000-4000-8000-000000000002"},
		session: machine, name: "read:req-human-interventions",
		method: "GET", path: "/v1/work/v2/human-interventions/conversation%3A10000000-0000-4000-8000-000000000002",
	}, {
		word: "work.v2.image",
		body: map[string]any{"type": "work.v2.image", "session": machine,
			"request": "req-work-v2-image", "id": "img1"},
		session: machine, name: "read:req-work-v2-image",
		method: "GET", path: "/v1/work/v2/images/img1",
		answers: &router{body: "png", media: "image/png"},
	}, {
		word: "work.v2.create",
		body: map[string]any{"type": "work.v2.create", "session": machine, "request": "req-work-v2-create",
			"item": map[string]any{"project_id": "p1", "kind": "feature", "title": "A", "description": "B", "deployment_policy": "agent_decides"}},
		session: machine, name: "action:req-work-v2-create", method: "POST", path: "/v1/work/v2/items",
		body2: `{"deployment_policy":"agent_decides","description":"B","kind":"feature","project_id":"p1","title":"A"}`,
	}, {
		word: "work.v2.gate-decision",
		body: map[string]any{"type": "work.v2.gate-decision", "session": machine, "request": "req-gate-decision", "id": "w1",
			"item": map[string]any{"expected_version": 5, "action": "override", "reason": "Reviewed"}},
		session: machine, name: "action:req-gate-decision", method: "POST", path: "/v1/work/v2/items/w1/gate-decision",
		body2: `{"action":"override","expected_version":5,"reason":"Reviewed"}`,
	}, {
		word: "work.v2.gate-purge",
		body: map[string]any{"type": "work.v2.gate-purge", "session": machine, "request": "req-gate-purge", "id": "w1",
			"item": map[string]any{"expected_version": 5, "sha256": "abc"}},
		session: machine, name: "action:req-gate-purge", method: "POST", path: "/v1/work/v2/items/w1/gate-purge",
		body2: `{"expected_version":5,"sha256":"abc"}`,
	}, {
		word: "work.v2.assign",
		body: map[string]any{"type": "work.v2.assign", "session": machine, "request": "req-work-v2-assign", "id": "w1",
			"item": map[string]any{"expected_version": 1, "mode": "new_session", "assistant": "codex", "model": "default"}},
		session: machine, name: "action:req-work-v2-assign", method: "POST", path: "/v1/work/v2/items/w1/assign",
		body2: `{"assistant":"codex","expected_version":1,"mode":"new_session","model":"default"}`,
	}, {
		word: "work.v2.convert",
		body: map[string]any{"type": "work.v2.convert", "session": machine, "request": "req-work-v2-convert", "id": "w1",
			"item": map[string]any{"expected_version": 2, "kind": "plan"}},
		session: machine, name: "action:req-work-v2-convert", method: "POST", path: "/v1/work/v2/items/w1/convert",
		body2: `{"expected_version":2,"kind":"plan"}`,
	}, {
		word: "work.v2.persona-suggestion",
		body: map[string]any{"type": "work.v2.persona-suggestion", "session": machine,
			"request": "req-work-v2-persona-suggestion", "id": "w1", "item": map[string]any{"expected_version": 1}},
		session: machine, name: "action:req-work-v2-persona-suggestion", method: "POST",
		path: "/v1/work/v2/items/w1/persona-suggestion", body2: `{"expected_version":1}`,
	}, {
		word: "work.v2.remind",
		body: map[string]any{"type": "work.v2.remind", "session": machine, "request": "req-work-v2-remind", "id": "w1",
			"item": map[string]any{"expected_version": 2}},
		session: machine, name: "action:req-work-v2-remind", method: "POST", path: "/v1/work/v2/items/w1/remind",
		body2: `{"expected_version":2}`,
	}, {
		word: "work.v2.edit",
		body: map[string]any{"type": "work.v2.edit", "session": machine, "request": "req-work-v2-edit", "id": "w1",
			"item": map[string]any{"expected_version": 2, "title": "Edited", "description": "Changed"}},
		session: machine, name: "action:req-work-v2-edit", method: "PATCH", path: "/v1/work/v2/items/w1",
		body2: `{"description":"Changed","expected_version":2,"title":"Edited"}`,
	}, {
		word: "work.v2.cancel",
		body: map[string]any{"type": "work.v2.cancel", "session": machine, "request": "req-work-v2-cancel", "id": "w1",
			"item": map[string]any{"expected_version": 3, "reason": "Deleted by the person from the Board."}},
		session: machine, name: "action:req-work-v2-cancel", method: "POST", path: "/v1/work/v2/items/w1/cancel",
		body2: `{"expected_version":3,"reason":"Deleted by the person from the Board."}`,
	}, {
		word: "work.v2.complete",
		body: map[string]any{"type": "work.v2.complete", "session": machine, "request": "req-work-v2-complete", "id": "w1",
			"item": map[string]any{"expected_version": 4, "note": "Finished by hand."}},
		session: machine, name: "action:req-work-v2-complete", method: "POST", path: "/v1/work/v2/items/w1/complete",
		body2: `{"expected_version":4,"note":"Finished by hand."}`,
	}, {
		word: "work.v2.seen",
		body: map[string]any{"type": "work.v2.seen", "session": machine, "request": "req-work-v2-seen", "id": "w1",
			"item": map[string]any{"phase": "deploying"}},
		session: machine, name: "action:req-work-v2-seen", method: "POST", path: "/v1/work/v2/items/w1/seen",
		body2: `{"phase":"deploying"}`,
	}, {
		word: "work.v2.image-create",
		body: map[string]any{"type": "work.v2.image-create", "session": machine, "request": "req-work-v2-image-create", "id": "w1",
			"item": map[string]any{"expected_version": 2, "title": "state.png", "data_url": "data:image/png;base64,cG5n"}},
		session: machine, name: "action:req-work-v2-image-create", method: "POST", path: "/v1/work/v2/items/w1/images",
		body2: `{"data_url":"data:image/png;base64,cG5n","expected_version":2,"title":"state.png"}`,
	}, {
		word: "work.v2.image-delete",
		body: map[string]any{"type": "work.v2.image-delete", "session": machine, "request": "req-work-v2-image-delete",
			"id": "w1", "image": "img1", "item": map[string]any{"expected_version": 3}},
		session: machine, name: "action:req-work-v2-image-delete", method: "DELETE", path: "/v1/work/v2/items/w1/images/img1",
		body2: `{"expected_version":3}`,
	}, {
		word: "work.v2.proposal-resolve",
		body: map[string]any{"type": "work.v2.proposal-resolve", "session": machine, "request": "req-work-v2-resolve",
			"id": "pr1", "decision": "accept", "item": map[string]any{}},
		session: machine, name: "action:req-work-v2-resolve", method: "POST", path: "/v1/work/v2/proposals/pr1/accept", body2: `{}`,
	}, {
		word: "work.v2.todo-create",
		body: map[string]any{"type": "work.v2.todo-create", "session": machine, "request": "req-work-v2-todo-create",
			"terminal": pane, "item": map[string]any{"text": "Ship it"}},
		session: machine, name: "action:req-work-v2-todo-create", method: "POST", path: "/v1/work/v2/session-todos/%2519",
		body2: `{"text":"Ship it"}`,
	}, {
		word: "work.v2.todo-image-create",
		body: map[string]any{"type": "work.v2.todo-image-create", "session": machine,
			"request": "req-work-v2-todo-image-create", "terminal": pane, "id": "td1",
			"item": map[string]any{"expected_version": 1, "title": "screen.png", "data_url": "data:image/png;base64,cG5n"}},
		session: machine, name: "action:req-work-v2-todo-image-create", method: "POST",
		path:  "/v1/work/v2/session-todos/%2519/td1/images",
		body2: `{"data_url":"data:image/png;base64,cG5n","expected_version":1,"title":"screen.png"}`,
	}, {
		word: "work.v2.todo-action",
		body: map[string]any{"type": "work.v2.todo-action", "session": machine, "request": "req-work-v2-todo-action",
			"terminal": pane, "id": "td1", "action": "send", "item": map[string]any{}},
		session: machine, name: "action:req-work-v2-todo-action", method: "POST", path: "/v1/work/v2/session-todos/%2519/td1/send", body2: `{}`,
	}, {
		word: "work.v2.human-intervention-action",
		body: map[string]any{"type": "work.v2.human-intervention-action", "session": machine,
			"request": "req-human-intervention-action", "terminal": "conversation:10000000-0000-4000-8000-000000000002",
			"id": "10000000-0000-4000-8000-000000000003", "action": "read", "item": map[string]any{"expected_version": 1}},
		session: machine, name: "action:req-human-intervention-action", method: "POST",
		path:  "/v1/work/v2/human-interventions/conversation%3A10000000-0000-4000-8000-000000000002/10000000-0000-4000-8000-000000000003/read",
		body2: `{"expected_version":1}`,
	}, {
		word:    "project-icon-copy",
		body:    map[string]any{"type": "project-icon-copy", "session": machine, "request": "req-icon", "id": "p1", "item": map[string]any{}},
		session: machine, name: "action:req-icon", method: "PUT", path: "/v1/projects/p1/icon", body2: `{}`,
	}, {
		word:    "project-manifest",
		body:    map[string]any{"type": "project-manifest", "session": machine, "request": "req-manifest"},
		session: machine, name: "read:req-manifest", method: "GET", path: "/v1/project-sync/manifest",
	}, {
		word:    "project-entry",
		body:    map[string]any{"type": "project-entry", "session": machine, "request": "req-entry", "repo": "github.com/o/n"},
		session: machine, name: "read:req-entry", method: "GET", path: "/v1/project-sync/entry", query: map[string]string{"repo": "github.com/o/n"},
	}, {
		word:    "project-mirror",
		body:    map[string]any{"type": "project-mirror", "session": machine, "request": "req-mirror"},
		session: machine, name: "read:req-mirror", method: "GET", path: "/v1/project-sync/mirror",
	}, {
		word:    "project-mirror-apply",
		body:    map[string]any{"type": "project-mirror-apply", "session": machine, "request": "req-apply", "item": map[string]any{}},
		session: machine, name: "action:req-apply", method: "POST", path: "/v1/project-sync/mirror", body2: `{}`,
	}, {
		word:    "project-mirror-detach",
		body:    map[string]any{"type": "project-mirror-detach", "session": machine, "request": "req-detach", "repo": "github.com/o/n"},
		session: machine, name: "action:req-detach", method: "DELETE", path: "/v1/project-sync/mirror", query: map[string]string{"repo": "github.com/o/n"},
	}, {
		word:    "projects",
		body:    map[string]any{"type": "projects", "session": machine, "request": "req-projects"},
		session: machine, name: "read:req-projects",
		method: "GET", path: "/v1/projects",
	}, {
		word:    "project-file-list",
		body:    map[string]any{"type": "project-file-list", "session": machine, "request": "req-file-list", "project": "p1"},
		session: machine, name: "read:req-file-list", method: "GET", path: "/v1/projects/p1/files",
	}, {
		word:    "project-file-read",
		body:    map[string]any{"type": "project-file-read", "session": machine, "request": "req-file-read", "project": "p1", "file": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		session: machine, name: "read:req-file-read", method: "GET", path: "/v1/projects/p1/files/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, {
		word:    "project-tree-list",
		body:    map[string]any{"type": "project-tree-list", "session": machine, "request": "req-tree-list", "project": "p1", "directory": "src/nested"},
		session: machine, name: "read:req-tree-list", method: "GET", path: "/v1/projects/p1/tree", query: map[string]string{"directory": "src/nested"},
	}, {
		word:    "project-tree-read",
		body:    map[string]any{"type": "project-tree-read", "session": machine, "request": "req-tree-read", "project": "p1", "path": "src/page.ts"},
		session: machine, name: "read:req-tree-read", method: "GET", path: "/v1/projects/p1/tree/file", query: map[string]string{"path": "src/page.ts"},
	}, {
		word: "project-file-save",
		body: map[string]any{"type": "project-file-save", "session": machine, "request": "req-file-save", "project": "p1", "file": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"item": map[string]any{"expected_version": "v1", "content": "updated"}},
		session: machine, name: "action:req-file-save", method: "PUT", path: "/v1/projects/p1/files/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		body2: `{"content":"updated","expected_version":"v1"}`,
	}, {
		word:    "project-unify-plan",
		body:    map[string]any{"type": "project-unify-plan", "session": machine, "request": "req-unify-plan", "project": "p1"},
		session: machine, name: "read:req-unify-plan", method: "GET", path: "/v1/projects/p1/unify",
	}, {
		word: "project-unify-apply",
		body: map[string]any{"type": "project-unify-apply", "session": machine, "request": "req-unify-apply", "project": "p1",
			"item": map[string]any{"version": "v1"}},
		session: machine, name: "action:req-unify-apply", method: "POST", path: "/v1/projects/p1/unify",
		body2: `{"version":"v1"}`,
	}, {
		// The Project id is a path segment here, so the escaping is the
		// answer to a different question than the query above's.
		word: "project-worktree-lifecycle",
		body: map[string]any{"type": "project-worktree-lifecycle", "session": machine,
			"request": "req-lifecycle", "project": "/Users/sean/code/clawdline-go"},
		session: machine, name: "read:req-lifecycle",
		method: "GET", path: "/v1/projects/%2FUsers%2Fsean%2Fcode%2Fclawdline-go/worktrees",
	}, {
		word: "project-worktree-lifecycle-refresh",
		body: map[string]any{"type": "project-worktree-lifecycle-refresh", "session": machine,
			"request": "req-lifecycle-refresh", "project": "/workspace/example"},
		session: machine, name: "action:req-lifecycle-refresh",
		method: "POST", path: "/v1/projects/%2Fworkspace%2Fexample/worktrees/refresh",
		body2: `{}`,
	}, {
		// `upcoming` travels on every read because both of its values mean
		// something; the empty filters do not travel at all, because this
		// route reads an absent parameter and an empty one differently.
		word: "timeline",
		body: map[string]any{"type": "timeline", "session": machine, "request": "req-timeline",
			"project": "clawdline-go", "entry": "", "cursor": "", "environment": "",
			"category": "", "upcoming": false},
		session: machine, name: "read:req-timeline",
		method: "GET", path: "/v1/timeline",
		query: map[string]string{"project": "clawdline-go", "upcoming": "false"},
	}, {
		// The Settings page's capacity block, which every capacity push names:
		// machine-wide and parameterless, as the local route is.
		word:    "capacity",
		body:    map[string]any{"type": "capacity", "session": machine, "request": "req-capacity"},
		session: machine, name: "read:req-capacity",
		method: "GET", path: "/v1/capacity",
	}, {
		word:    "default-models",
		body:    map[string]any{"type": "default-models", "session": machine, "request": "req-models"},
		session: machine, name: "read:req-models",
		method: "GET", path: "/v1/settings/default-models",
	}, {
		word: "default-models-update",
		body: map[string]any{"type": "default-models-update", "session": machine, "request": "req-models-write",
			"changes": map[string]any{"codex_default_model": "gpt-6"}},
		session: machine, name: "action:req-models-write",
		method: "POST", path: "/v1/settings/default-models",
		body2: `{"codex_default_model":"gpt-6"}`,
	}, {
		word:    "work-gate-settings",
		body:    map[string]any{"type": "work-gate-settings", "session": machine, "request": "req-gates"},
		session: machine, name: "read:req-gates",
		method: "GET", path: "/v1/settings/work-gates",
	}, {
		word: "work-gate-settings-update",
		body: map[string]any{"type": "work-gate-settings-update", "session": machine, "request": "req-gates-write",
			"changes": map[string]any{"planning_gate": true, "verify_gate": true}},
		session: machine, name: "action:req-gates-write",
		method: "POST", path: "/v1/settings/work-gates",
		body2: `{"planning_gate":true,"verify_gate":true}`,
	}, {
		// The dashboard behind the session counts: machine-wide and
		// parameterless, as the local route is.
		word:    "machine-usage",
		body:    map[string]any{"type": "machine-usage", "session": machine, "request": "req-usage"},
		session: machine, name: "read:req-usage",
		method: "GET", path: "/v1/machine/usage",
	}, {
		// Whether this machine trails the cloud's latest build: machine-wide
		// and parameterless, as the local route is.
		word:    "update",
		body:    map[string]any{"type": "update", "session": machine, "request": "req-update"},
		session: machine, name: "read:req-update",
		method: "GET", path: "/v1/update",
	}, {
		// The Settings page's 「立即更新」: a machine command with no parameter.
		word:    "update-apply",
		body:    map[string]any{"type": "update-apply", "session": machine, "request": "req-apply"},
		session: machine, name: "action:req-apply",
		method: "POST", path: "/v1/update/apply",
		body2: `{}`,
	}, {
		// The built-in personas a start may name: machine-wide and
		// parameterless, as the local route is.
		word:    "personas",
		body:    map[string]any{"type": "personas", "session": machine, "request": "req-personas"},
		session: machine, name: "read:req-personas",
		method: "GET", path: "/v1/personas",
	}, {
		word: "squad-session-snapshot",
		body: map[string]any{"type": "squad-session-snapshot", "session": machine,
			"request": "req-role-snapshot", "conversation": "conversation-a"},
		session: machine, name: "read:req-role-snapshot", method: "GET",
		path: "/v1/squad/session-snapshots/conversation-a",
	}, {
		word: "send",
		body: map[string]any{"type": "send", "session": pane, "request": "req-send",
			"text": "hello", "images": []any{}},
		session: pane, name: "action:req-send",
		method: "POST", path: "/v1/sessions/%2519/send",
		body2: `{"images":[],"text":"hello"}`,
	}, {
		word: "answer",
		body: map[string]any{"type": "answer", "session": pane, "request": "req-answer",
			"answer": "2", "expect": fingerprint},
		session: pane, name: "action:req-answer",
		method: "POST", path: "/v1/sessions/%2519/key",
		body2: `{"expect":"` + fingerprint + `","key":"2"}`,
	}, {
		word: "key",
		body: map[string]any{"type": "key", "session": pane, "request": "req-key", "key": "submit",
			"expect": fingerprint},
		session: pane, name: "action:req-key",
		method: "POST", path: "/v1/sessions/%2519/key",
		body2: `{"expect":"` + fingerprint + `","key":"submit"}`,
	}, {
		word: "end",
		body: map[string]any{"type": "end", "session": pane, "request": "req-end",
			"accept_loss": false, "expected_closeability_version": "cl1_" + strings.Repeat("a", 32)},
		session: pane, name: "action:req-end",
		method: "POST", path: "/v1/sessions/%2519/close",
		body2: `{"expected_closeability_version":"cl1_` + strings.Repeat("a", 32) + `","force":false}`,
	}, {
		word:    "focus",
		body:    map[string]any{"type": "focus", "session": pane, "request": "req-focus"},
		session: pane, name: "action:req-focus",
		method: "POST", path: "/v1/sessions/%2519/focus",
	}, {
		word:    "interrupt",
		body:    map[string]any{"type": "interrupt", "session": pane, "request": "req-interrupt"},
		session: pane, name: "action:req-interrupt",
		method: "POST", path: "/v1/sessions/%2519/interrupt", body2: `{}`,
	}, {
		word:    "smart-title",
		body:    map[string]any{"type": "smart-title", "session": pane, "request": "req-smart-title"},
		session: pane, name: "action:req-smart-title",
		method: "POST", path: "/v1/sessions/%2519/smart-title", body2: `{}`,
	}, {
		word: "start",
		body: map[string]any{"type": "start", "session": machine, "request": "req-start",
			"place": "/Users/sean/code/clawdline-go", "assistant": "claude", "model": "opus"},
		session: machine, name: "action:req-start",
		method: "POST",
		path:   "/v1/places/%2FUsers%2Fsean%2Fcode%2Fclawdline-go/start/claude/opus",
	}, {
		word: "resume",
		body: map[string]any{"type": "resume", "session": machine, "request": "req-resume",
			"place": "/Users/sean/code/clawdline-go", "past": "018f2f7a", "assistant": "claude"},
		session: machine, name: "action:req-resume",
		method: "POST",
		path:   "/v1/places/%2FUsers%2Fsean%2Fcode%2Fclawdline-go/resume/claude/018f2f7a",
	}, {
		// The sessions a reboot took away (docs/session-restore.md).
		word:    "restorable-sessions",
		body:    map[string]any{"type": "restorable-sessions", "session": machine, "request": "req-offer"},
		session: machine, name: "read:req-offer",
		method: "GET", path: "/v1/sessions/restorable",
	}, {
		word: "restore-sessions",
		body: map[string]any{"type": "restore-sessions", "session": machine, "request": "req-restore",
			"conversations": []any{"018f2f7a", "cx-9"}},
		session: machine, name: "action:req-restore",
		method: "POST", path: "/v1/sessions/restorable/restore",
		body2: `{"conversations":["018f2f7a","cx-9"]}`,
	}, {
		// Without `conversations` it is every one on offer, and the route is
		// sent no list at all rather than an empty one.
		word:    "dismiss-restorable",
		body:    map[string]any{"type": "dismiss-restorable", "session": machine, "request": "req-dismiss"},
		session: machine, name: "action:req-dismiss",
		method: "POST", path: "/v1/sessions/restorable/dismiss",
		body2: `{}`,
	}, {
		// The Sessions a person archived (docs/session-archive.md). The
		// archive rides the session's channel, as `end` does.
		word: "archive-session",
		body: map[string]any{"type": "archive-session", "session": pane, "request": "req-archive",
			"force": true},
		session: pane, name: "action:req-archive",
		method: "POST", path: "/v1/sessions/%2519/archive",
		body2: `{"force":true}`,
	}, {
		word:    "archived-sessions",
		body:    map[string]any{"type": "archived-sessions", "session": machine, "request": "req-archived"},
		session: machine, name: "read:req-archived",
		method: "GET", path: "/v1/sessions/archived",
	}, {
		word: "restore-archived",
		body: map[string]any{"type": "restore-archived", "session": machine, "request": "req-unarchive",
			"conversations": []any{"018f2f7a"}},
		session: machine, name: "action:req-unarchive",
		method: "POST", path: "/v1/sessions/archived/restore",
		body2: `{"conversations":["018f2f7a"]}`}, {
		word: "voice",
		body: map[string]any{"type": "voice", "session": machine, "request": "req-voice",
			"audio": "AAAAAA==", "rate": 16000},
		session: machine, name: "action:req-voice",
		method: "POST", path: "/v1/voice",
		body2: `{"audio":"AAAAAA==","rate":16000}`,
	}, {
		word: "intents",
		body: map[string]any{"type": "intents", "session": machine, "request": "req-intent",
			"text": "start the review"},
		session: machine, name: "action:req-intent",
		method: "POST", path: "/v1/intents",
		body2: `{"text":"start the review"}`,
	}, {
		// The schedule the form sends, handed to the route whole: this bridge
		// knows what a request looks like, not what a schedule looks like.
		word: "schedule-create",
		body: map[string]any{"type": "schedule-create", "session": machine,
			"request": "req-make", "schedule": map[string]any{
				"title": "morning", "at": "09:00", "days": "daily",
				"place_id": "place-1", "assistant": "claude", "instructions": "look"}},
		session: machine, name: "action:req-make",
		method: "POST", path: "/v1/orchestrator/schedules",
		body2: `{"assistant":"claude","at":"09:00","days":"daily","instructions":"look",` +
			`"place_id":"place-1","title":"morning"}`,
	}, {
		word: "schedule-update",
		body: map[string]any{"type": "schedule-update", "session": machine, "request": "req-save",
			"id": scheduleID, "schedule": map[string]any{"title": "morning", "at": "10:00",
				"days": "daily", "place_id": "place-1", "assistant": "claude", "instructions": "look"}},
		session: machine, name: "action:req-save",
		method: "PATCH", path: "/v1/orchestrator/schedules/" + scheduleID,
		body2: `{"assistant":"claude","at":"10:00","days":"daily","instructions":"look",` +
			`"place_id":"place-1","title":"morning"}`,
	}, {
		word: "schedule-delete",
		body: map[string]any{"type": "schedule-delete", "session": machine, "request": "req-gone",
			"id": scheduleID},
		session: machine, name: "action:req-gone",
		method: "DELETE", path: "/v1/orchestrator/schedules/" + scheduleID,
	}, {
		word: "schedule-run",
		body: map[string]any{"type": "schedule-run", "session": machine, "request": "req-now",
			"id": scheduleID},
		session: machine, name: "action:req-now",
		method: "POST", path: "/v1/orchestrator/schedules/" + scheduleID + "/run",
		body2: `{}`,
	}, {
		word: "schedule-webhook-bind-v1",
		body: map[string]any{"type": "schedule-webhook-bind-v1",
			"request_id": scheduleWebhookRequestID, "hook_id": scheduleWebhookHook,
			"schedule_id": scheduleID, "replace_hook_id": nil},
		session: machine, name: "action:" + scheduleWebhookRequestID,
		method: "POST", path: "/v1/orchestrator/schedule-webhooks/bind",
		body2: `{"hook_id":"` + scheduleWebhookHook + `","replace_hook_id":null,` +
			`"request_id":"` + scheduleWebhookRequestID + `","schedule_id":"` + scheduleID + `"}`,
	}, {
		// The four snippet writes, in the producer's own key sets. Nothing of
		// the person's is in these: the fields are invented here and the machine
		// is what decides whether they are a snippet.
		word: "snippet-create",
		body: map[string]any{"type": "snippet-create", "session": machine, "request": "req-new",
			"snippet": map[string]any{"title": "stand up the branch", "body": "git switch -c",
				"scope": "global"}},
		session: machine, name: "action:req-new",
		method: "POST", path: "/v1/snippets",
		body2: `{"body":"git switch -c","scope":"global","title":"stand up the branch"}`,
	}, {
		word: "snippet-update",
		body: map[string]any{"type": "snippet-update", "session": machine, "request": "req-save",
			"id": snippetID, "snippet": map[string]any{"title": "stand up the branch",
				"body": "git switch -c", "scope": "global"}},
		session: machine, name: "action:req-save",
		method: "PATCH", path: "/v1/snippets/" + snippetID,
		body2: `{"body":"git switch -c","scope":"global","title":"stand up the branch"}`,
	}, {
		word: "snippet-delete",
		body: map[string]any{"type": "snippet-delete", "session": machine, "request": "req-gone",
			"id": snippetID},
		session: machine, name: "action:req-gone",
		method: "DELETE", path: "/v1/snippets/" + snippetID,
	}, {
		// `ordering` on the wire, and the same three fields under no name at
		// all on the route: the one place the two spellings differ.
		word: "snippet-order",
		body: map[string]any{"type": "snippet-order", "session": machine, "request": "req-order",
			"ordering": map[string]any{"scope": "global", "order": []any{snippetID}}},
		session: machine, name: "action:req-order",
		method: "POST", path: "/v1/snippets/order",
		body2: `{"order":["` + snippetID + `"],"scope":"global"}`,
	}, {
		// The browser's own subscription object, handed to the route whole:
		// its endpoint and its keys are the browser's, and anything reshaped
		// on the way past is a chance to get a credential wrong.
		word: "push-subscribe",
		body: map[string]any{"type": "push-subscribe", "session": machine, "request": "req-sub",
			"subscription": map[string]any{"endpoint": "https://web.push.apple.com/QW",
				"keys": map[string]any{"p256dh": "BPk", "auth": "c2VjcmV0"}}},
		session: machine, name: "action:req-sub",
		method: "POST", path: "/v1/push/subscribe",
		body2: `{"endpoint":"https://web.push.apple.com/QW",` +
			`"keys":{"auth":"c2VjcmV0","p256dh":"BPk"}}`,
	}, {
		word: "push-unsubscribe",
		body: map[string]any{"type": "push-unsubscribe", "session": machine,
			"request": "req-drop", "id": pushID},
		session: machine, name: "action:req-drop",
		method: "POST", path: "/v1/push/unsubscribe",
		body2: `{"id":"` + pushID + `"}`,
	}, {
		// `target` is the session the notification should tap back to, and
		// the local route's field for it is `session_id`.
		word: "push-test",
		body: map[string]any{"type": "push-test", "session": machine, "request": "req-buzz",
			"target": pane},
		session: machine, name: "action:req-buzz",
		method: "POST", path: "/v1/push/test",
		body2: `{"session_id":"%19"}`,
	}, {
		word:    "agent",
		body:    map[string]any{"type": "agent", "session": pane, "agent": "ag_4", "limit": 200},
		session: pane, name: "agent:ag_4", method: "GET",
		path: "/v1/sessions/%2519/agents/ag_4", query: map[string]string{"limit": "200"},
	}, {
		word:    "shell",
		body:    map[string]any{"type": "shell", "session": pane, "shell": "b0aau3e6s", "bytes": 65536},
		session: pane, name: "shell:b0aau3e6s", method: "GET",
		path: "/v1/sessions/%2519/shells/b0aau3e6s", query: map[string]string{"bytes": "65536"},
	}, {
		// The words this daemon knows and cannot answer. `unknown_command` is
		// not a guess at a code: it is the one the hosted console learns from
		// (`machineLacks` in net/cloud-client.js), so a machine that says it stops
		// being asked.
		word:    "skills",
		body:    map[string]any{"type": "skills", "session": pane},
		session: pane, name: "skills", code: "unknown_command", status: 400,
	}, {
		// Answered by the Session publisher, not by a route; a bridge with no
		// publisher behind it says it does not know the word, which is what
		// stops a page asking (TestSessionsSnapshotAnswersTheIdsThePublisherStated).
		word: "sessions.snapshot",
		body: map[string]any{"type": "sessions.snapshot", "session": machine,
			"request": "req-rows"},
		session: machine, name: "read:req-rows", code: "unknown_command", status: 400,
	}, {
		word: "sessions.snapshot.initial",
		body: map[string]any{"type": "sessions.snapshot.initial", "session": machine,
			"request": "req-initial-rows"},
		session: machine, name: "read:req-initial-rows", code: "unknown_command", status: 400,
	}, {
		word: "schedule",
		body: map[string]any{"type": "schedule", "session": machine, "request": "req-schedule",
			"id": scheduleID},
		session: machine, name: "read:req-schedule",
		method: "GET", path: "/v1/orchestrator/schedules/" + scheduleID,
	}, {
		word: "diagnostics.report",
		body: map[string]any{"type": "diagnostics.report", "session": machine,
			"request": "req-report", "report": map[string]any{"page": "session"}},
		session: machine, name: "action:req-report", code: "unknown_command", status: 400,
	}, {
		word: "diagnostics.events",
		body: map[string]any{"type": "diagnostics.events", "session": machine,
			"request": "req-events", "batch": map[string]any{"rows": []any{}}},
		session: machine, name: "action:req-events", code: "unknown_command", status: 400,
	}, {
		// The one word this machine could serve and deliberately does not.
		word: "dispatch",
		body: map[string]any{"type": "dispatch", "session": machine, "request": "req-dispatch",
			"task": map[string]any{"title": "a task"}},
		session: machine, name: "action:req-dispatch",
		code: "cloud_dispatch_unpinned", status: 409,
	}}

	if len(cases) != len(Vocabulary()) {
		t.Fatalf("the table covers %d words and the vocabulary has %d: %v",
			len(cases), len(Vocabulary()), Vocabulary())
	}
	for _, c := range cases {
		t.Run(c.word, func(t *testing.T) {
			class := ClassCtl
			if c.word == "dispatch" {
				class = ClassDispatch
			}
			r := c.answers
			if r == nil {
				r = &router{}
			}
			answer := open(r).Handle(context.Background(), request(t, class, c.body))
			if answer.Session != c.session || answer.Name != c.name {
				t.Fatalf("answered on %q/%q, wanted %q/%q",
					answer.Session, answer.Name, c.session, c.name)
			}
			if !answer.Published() {
				t.Fatalf("that answer reaches nobody: %+v", answer)
			}
			payload := answerOf(t, answer)
			if payload["read"] != c.name {
				t.Fatalf("the payload names %v, wanted %q", payload["read"], c.name)
			}
			if c.code != "" {
				if len(r.seen) != 0 {
					t.Fatalf("a refused word still asked this machine: %+v", r.seen)
				}
				if answer.Code != c.code || answer.Status != c.status {
					t.Fatalf("refused %q/%d, wanted %q/%d",
						answer.Code, answer.Status, c.code, c.status)
				}
				failure := errorOf(t, answer)
				if failure["code"] != c.code {
					t.Fatalf("the payload's code is %v, wanted %q", failure["code"], c.code)
				}
				if failure["seq"] != json.Number("411") && failure["seq"] != float64(411) {
					t.Fatalf("the refusal lost its sequence: %v", failure["seq"])
				}
				return
			}
			if len(r.seen) != 1 {
				t.Fatalf("asked this machine %d times, wanted once", len(r.seen))
			}
			got := r.last()
			if got.Method != c.method || got.Path != c.path {
				t.Fatalf("routed %s %s, wanted %s %s", got.Method, got.Path, c.method, c.path)
			}
			for key, want := range c.query {
				if got.Query[key] != want {
					t.Fatalf("query %s=%q, wanted %q", key, got.Query[key], want)
				}
			}
			if len(got.Query) != len(c.query) {
				t.Fatalf("the query is %v, wanted %v", got.Query, c.query)
			}
			if c.body2 != "" && string(got.Body) != c.body2 {
				t.Fatalf("the body is %s, wanted %s", got.Body, c.body2)
			}
			if answer.Status != 200 || answer.Code != "" {
				t.Fatalf("answered %d/%q, wanted 200", answer.Status, answer.Code)
			}
			if _, ok := payload["body"]; !ok {
				t.Fatalf("a successful answer carries no body: %s", answer.Payload)
			}
		})
	}
}
