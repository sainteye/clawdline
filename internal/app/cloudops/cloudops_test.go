package cloudops

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"sort"
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
		// The work system, five words. What a phone showed before these was
		// `cloud_not_carried` on every one of them, which is the whole board,
		// the Backlog, the proposals, the decisions and the digests at once.
		word: "work.board",
		body: map[string]any{"type": "work.board", "session": machine, "request": "req-work-board",
			"project": "/Users/sean/code/clawdline-go", "cursor": ""},
		session: machine, name: "read:req-work-board",
		method: "GET", path: "/v1/work/board",
		query: map[string]string{"project": "/Users/sean/code/clawdline-go"},
	}, {
		word: "work.backlog",
		body: map[string]any{"type": "work.backlog", "session": machine, "request": "req-work-backlog",
			"project": "", "cursor": "c-2"},
		session: machine, name: "read:req-work-backlog",
		method: "GET", path: "/v1/work/backlog",
		query: map[string]string{"cursor": "c-2"},
	}, {
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
		word: "work.digests",
		body: map[string]any{"type": "work.digests", "session": machine,
			"request": "req-work-digests", "kind": "daily"},
		session: machine, name: "read:req-work-digests",
		method: "GET", path: "/v1/work/digests",
		query: map[string]string{"kind": "daily"},
	}, {
		word: "work.v2.items",
		body: map[string]any{"type": "work.v2.items", "session": machine,
			"request": "req-work-v2-items", "project": "p1"},
		session: machine, name: "read:req-work-v2-items",
		method: "GET", path: "/v1/work/v2/items", query: map[string]string{"project": "p1"},
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
		word: "work.v2.assign",
		body: map[string]any{"type": "work.v2.assign", "session": machine, "request": "req-work-v2-assign", "id": "w1",
			"item": map[string]any{"expected_version": 1, "mode": "new_session", "assistant": "codex", "model": "default"}},
		session: machine, name: "action:req-work-v2-assign", method: "POST", path: "/v1/work/v2/items/w1/assign",
		body2: `{"assistant":"codex","expected_version":1,"mode":"new_session","model":"default"}`,
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
		word:    "projects",
		body:    map[string]any{"type": "projects", "session": machine, "request": "req-projects"},
		session: machine, name: "read:req-projects",
		method: "GET", path: "/v1/projects",
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
		// Machine-wide and so parameterless: the landing ledger is not one
		// repository's debt, and a word that took a project would let a page
		// show one repository's rows as the whole answer.
		word:    "landings",
		body:    map[string]any{"type": "landings", "session": machine, "request": "req-landings"},
		session: machine, name: "read:req-landings",
		method: "GET", path: "/v1/orchestrator/landings",
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
		// The words this daemon knows and cannot answer. `unknown_command` is
		// not a guess at a code: it is the one the hosted console learns from
		// (`machineLacks` in net/cloud-client.js), so a machine that says it stops
		// being asked.
		word:    "shell",
		body:    map[string]any{"type": "shell", "session": pane, "shell": "sh_2", "bytes": 65536},
		session: pane, name: "shell:sh_2", code: "unknown_command", status: 400,
	}, {
		word:    "skills",
		body:    map[string]any{"type": "skills", "session": pane},
		session: pane, name: "skills", code: "unknown_command", status: 400,
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

// TestEveryChangeCarriesAnIdempotencyKey is the difference between a retry and
// a second effect. The viewer's own request id is the key, because that is the
// identity it retries under.
func TestEveryChangeCarriesAnIdempotencyKey(t *testing.T) {
	r := &router{}
	open(r).Handle(context.Background(), request(t, ClassCtl, map[string]any{
		"type": "focus", "session": pane, "request": "req-focus"}))
	if key := r.last().Header["Idempotency-Key"]; key != "req-focus" {
		t.Fatalf("the key is %q, wanted the request id", key)
	}
	// A read is not an effect, and a GET that is retried is not a second
	// anything.
	r = &router{}
	open(r).Handle(context.Background(), request(t, ClassCtl, map[string]any{
		"type": "git", "session": pane}))
	if _, keyed := r.last().Header["Idempotency-Key"]; keyed {
		t.Fatalf("a read was given an idempotency key: %v", r.last().Header)
	}
	// An older page sends no request id. The envelope's own identity is then
	// the key, because the protocol already makes it unique per viewer.
	r = &router{}
	open(r).Handle(context.Background(), request(t, ClassCtl, map[string]any{
		"type": "send", "session": pane, "text": "hello", "images": []any{}}))
	if key := r.last().Header["Idempotency-Key"]; key != "cloud:viewer-device-01:411" {
		t.Fatalf("the key is %q, wanted the envelope's identity", key)
	}
}

// TestTheWriteSwitchIsOffUntilSomebodySaysOtherwise is the default this whole
// package is built around: a daemon that was never told anybody may write has
// not been told anybody may write.
func TestTheWriteSwitchIsOffUntilSomebodySaysOtherwise(t *testing.T) {
	r := &router{}
	closed := Bridge{MachineID: "mac-01", Router: r}
	for _, word := range []string{"send", "answer", "end", "focus", "start", "resume", "voice", "intents",
		"schedule-create", "schedule-update", "schedule-delete", "schedule-run",
		"schedule-webhook-bind-v1",
		"snippet-create", "snippet-update", "snippet-delete", "snippet-order", "dispatch"} {
		body := map[string]any{"type": word, "session": pane, "request": "req-" + word}
		switch word {
		case "send":
			body["text"], body["images"] = "hello", []any{}
		case "answer":
			body["answer"] = "2"
		case "end":
			body["accept_loss"], body["expected_closeability_version"] = true, ""
		case "start":
			body["session"] = MachineReplySession
			body["place"], body["assistant"], body["model"] = "/tmp", "claude", ""
		case "resume":
			body["session"] = MachineReplySession
			body["place"], body["past"], body["assistant"] = "/tmp", "abc", "claude"
		case "voice":
			body["session"] = MachineReplySession
			body["audio"], body["rate"] = "AAAA", 16000
		case "intents":
			body["session"] = MachineReplySession
			body["text"] = "start the review"
		case "snippet-create":
			body["session"] = MachineReplySession
			body["snippet"] = map[string]any{"title": "a title", "body": "a body", "scope": "global"}
		case "snippet-update":
			body["session"] = MachineReplySession
			body["id"] = snippetID
			body["snippet"] = map[string]any{"title": "a title", "body": "a body", "scope": "global"}
		case "snippet-delete":
			body["session"] = MachineReplySession
			body["id"] = snippetID
		case "snippet-order":
			body["session"] = MachineReplySession
			body["ordering"] = map[string]any{"scope": "global", "order": []any{snippetID}}
		case "schedule-create":
			body["session"] = MachineReplySession
			body["schedule"] = map[string]any{"title": "morning"}
		case "schedule-update":
			body["session"] = MachineReplySession
			body["id"], body["schedule"] = scheduleID, map[string]any{"title": "morning"}
		case "schedule-delete", "schedule-run":
			body["session"] = MachineReplySession
			body["id"] = scheduleID
		case "schedule-webhook-bind-v1":
			body = map[string]any{"type": word, "request_id": scheduleWebhookRequestID,
				"hook_id": scheduleWebhookHook, "schedule_id": scheduleID, "replace_hook_id": nil}
		case "dispatch":
			body["session"] = MachineReplySession
			body["task"] = map[string]any{}
		}
		class := ClassCtl
		if word == "dispatch" {
			class = ClassDispatch
		}
		answer := closed.Handle(context.Background(), request(t, class, body))
		if answer.Code != "cloud_commands_disabled" || answer.Status != 403 {
			t.Fatalf("%s: refused %q/%d, wanted cloud_commands_disabled/403",
				word, answer.Code, answer.Status)
		}
		if !answer.Published() {
			t.Fatalf("%s: the refusal reaches nobody", word)
		}
		// The gate comes before the body, so nothing above reads it. Each body
		// is sent again with the switch on, where it has to decode: otherwise
		// this walk proves only that the switch refuses what is refused anyway.
		if answer := open(&router{}).Handle(context.Background(), request(t, class, body)); answer.Code == "malformed_command" {
			t.Errorf("%s: with the switch on this body is malformed, so its refusal says nothing about the switch", word)
		}
	}
	if len(r.seen) != 0 {
		t.Fatalf("a refused command still asked this machine: %+v", r.seen)
	}
}

// TestAReadDoesNotNeedTheWriteSwitch is the other half of that, and the reason
// reads are a separate list: a transcript read types into nothing, and a
// viewer that could see every session row and not the messages inside one
// would be showing less than the same device sees over the tunnel.
func TestAReadDoesNotNeedTheWriteSwitch(t *testing.T) {
	r := &router{}
	closed := Bridge{MachineID: "mac-01", Router: r}
	answer := closed.Handle(context.Background(), request(t, ClassCtl, map[string]any{
		"type": "transcript", "session": pane, "limit": 200}))
	if answer.Status != 200 || len(r.seen) != 1 {
		t.Fatalf("a read was refused with the switch off: %+v", answer)
	}
}

// TestAMalformedBodyIsRefusedWhereItSafelyNames walks the shapes a viewer can
// get wrong. Each one is refused, and each refusal goes only where the body's
// own identity fields safely say it would have gone.
func TestAMalformedBodyIsRefusedWhereItSafelyNames(t *testing.T) {
	cases := []struct {
		name      string
		body      map[string]any
		class     Class
		code      string
		published bool
	}{{
		name: "a read with an extra field is a different word",
		body: map[string]any{"type": "git", "session": pane, "limit": 10},
		code: "malformed_read",
	}, {
		name: "a transcript window outside the route's own bounds",
		body: map[string]any{"type": "transcript", "session": pane, "limit": 5000},
		code: "malformed_read",
	}, {
		name: "a machine read that names a session instead",
		body: map[string]any{"type": "places", "session": pane, "request": "req"},
		code: "malformed_read",
	}, {
		name: "a document path that climbs",
		body: map[string]any{"type": "document", "session": pane, "request": "req",
			"scope": "project", "task": "", "path": "../secrets.md"},
		code: "malformed_read", published: true,
	}, {
		name: "a document that is not inert text",
		body: map[string]any{"type": "document", "session": pane, "request": "req",
			"scope": "project", "task": "", "path": "index.html"},
		code: "malformed_read", published: true,
	}, {
		name: "a voice rate this machine does not transcribe",
		body: map[string]any{"type": "voice", "session": MachineReplySession,
			"request": "req", "audio": "AAAA", "rate": 48000},
		code: "malformed_command", published: true,
	}, {
		name: "a key that is not a string",
		body: map[string]any{"type": "answer", "session": pane, "request": "req", "answer": 2},
		code: "malformed_command", published: true,
	}, {
		name:  "a read that arrived under the dispatch class",
		body:  map[string]any{"type": "git", "session": pane},
		class: ClassDispatch, code: "malformed_read",
	}, {
		name: "a command that arrived under the dispatch class",
		body: map[string]any{"type": "focus", "session": pane, "request": "req"},
		// The answer channel is named only from a `ctl` body, so a command
		// that arrived under another class is refused as a notice: naming a
		// waiter from a body this machine would not have accepted is how a
		// malformed request becomes an arbitrary publish.
		class: ClassDispatch, code: "malformed_command",
	}, {
		name: "a word nobody knows",
		body: map[string]any{"type": "rm-rf", "session": MachineReplySession, "request": "req"},
		code: "unknown_command", published: true,
	}, {
		// The key is required and its value may be empty; a body that leaves
		// it out is the older word, not this one.
		name: "a past-sessions read that names no assistant at all",
		body: map[string]any{"type": "past-sessions", "session": MachineReplySession,
			"request": "req", "place": "/tmp"},
		code: "malformed_read", published: true,
	}, {
		name: "a schedule that is not an object",
		body: map[string]any{"type": "schedule-create", "session": MachineReplySession,
			"request": "req", "schedule": "morning at nine"},
		code: "malformed_command", published: true,
	}, {
		name: "a save that names no schedule to save over",
		body: map[string]any{"type": "schedule-update", "session": MachineReplySession,
			"request": "req", "id": "", "schedule": map[string]any{"title": "morning"}},
		code: "malformed_command", published: true,
	}, {
		name: "a subscription that is not an object",
		body: map[string]any{"type": "push-subscribe", "session": MachineReplySession,
			"request": "req", "subscription": "https://web.push.apple.com/QW"},
		code: "malformed_command", published: true,
	}, {
		name: "a removal that names no subscription",
		body: map[string]any{"type": "push-unsubscribe", "session": MachineReplySession,
			"request": "req", "id": ""},
		code: "malformed_command", published: true,
	}, {
		// The key is required and its value may be empty: a test with nothing
		// to tap back to is the account's, and sends to the list. A body that
		// leaves the key out is a different word's shape.
		name: "a test that names no target at all",
		body: map[string]any{"type": "push-test", "session": MachineReplySession,
			"request": "req"},
		code: "malformed_command", published: true,
	}}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			class := c.class
			if class == "" {
				class = ClassCtl
			}
			r := &router{}
			answer := open(r).Handle(context.Background(), request(t, class, c.body))
			if answer.Code != c.code {
				t.Fatalf("refused %q, wanted %q", answer.Code, c.code)
			}
			if answer.Published() != c.published {
				t.Fatalf("published=%v, wanted %v (%s)", answer.Published(), c.published, answer.Payload)
			}
			if len(r.seen) != 0 {
				t.Fatalf("a malformed body still reached this machine: %+v", r.seen)
			}
		})
	}
}

// TestARequestForAnotherMacIsNotAnswered: the viewer listens on the channel it
// addressed, and an answer on this machine's own channel would be read by
// nobody. The notice is the reply.
func TestARequestForAnotherMacIsNotAnswered(t *testing.T) {
	r := &router{}
	cmd := request(t, ClassCtl, map[string]any{"type": "git", "session": pane})
	cmd.Channel = "ctl/mac-02"
	answer := open(r).Handle(context.Background(), cmd)
	if answer.Code != "wrong_machine" || answer.Status != 409 {
		t.Fatalf("refused %q/%d, wanted wrong_machine/409", answer.Code, answer.Status)
	}
	if answer.Published() || len(r.seen) != 0 {
		t.Fatalf("a request for another machine was answered or routed: %+v", answer)
	}
}

// TestARouteRefusalCrossesWithItsOwnCode is what lets the page that says "this
// session is not inside a Git repository" over the tunnel say it over the
// relay too, in the same branch.
func TestARouteRefusalCrossesWithItsOwnCode(t *testing.T) {
	r := &router{status: 409,
		body: `{"error":{"code":"not_a_repo","message":"That session is not inside a Git repository."}}`}
	answer := open(r).Handle(context.Background(), request(t, ClassCtl, map[string]any{
		"type": "git", "session": pane}))
	if answer.Status != 409 || answer.Code != "not_a_repo" {
		t.Fatalf("answered %d/%q, wanted 409/not_a_repo", answer.Status, answer.Code)
	}
	failure := errorOf(t, answer)
	if failure["code"] != "not_a_repo" || failure["layer"] != layerRoute {
		t.Fatalf("the error is %v", failure)
	}
	if failure["message"] != "That session is not inside a Git repository." {
		t.Fatalf("the route's own sentence did not cross: %v", failure["message"])
	}
}

// TestThisDaemonsOwnRefusalSpellingAlsoCrosses. Most routes here answer
// `{"error":"<code>","detail":"…"}` rather than the nested object the gate and
// the documents route answer, and a viewer shown `command_failed` for a
// `session_unknown` would be given a shrug where there was an explanation.
func TestThisDaemonsOwnRefusalSpellingAlsoCrosses(t *testing.T) {
	r := &router{status: 409,
		body: `{"detail":"this reading of the machine was incomplete (merged), so %19 is not absent, it is unseen","error":"session_unknown"}`}
	answer := open(r).Handle(context.Background(), request(t, ClassCtl, map[string]any{
		"type": "screen", "session": pane}))
	if answer.Status != 409 || answer.Code != "session_unknown" {
		t.Fatalf("answered %d/%q, wanted 409/session_unknown", answer.Status, answer.Code)
	}
	failure := errorOf(t, answer)
	if failure["code"] != "session_unknown" {
		t.Fatalf("the code did not cross: %v", failure)
	}
	if message, _ := failure["message"].(string); !strings.Contains(message, "it is unseen") {
		t.Fatalf("the sentence did not cross: %v", failure["message"])
	}

	// A blocked close carries what is in the way, because a card that can only
	// say "refused" sends a person to the terminal to find out why.
	blocked := &router{status: 409, body: `{"error":"close_blocked","detail":"still owed: landing",` +
		`"reasons":[{"kind":"obligation","code":"landing","subject_id":"t1"}]}`}
	answer = open(blocked).Handle(context.Background(), request(t, ClassCtl, map[string]any{
		"type": "end", "session": pane, "request": "req-end", "accept_loss": false,
		"expected_closeability_version": ""}))
	failure = errorOf(t, answer)
	if failure["code"] != "close_blocked" {
		t.Fatalf("the code did not cross: %v", failure)
	}
	reasons, ok := failure["reasons"].([]any)
	if !ok || len(reasons) != 1 {
		t.Fatalf("the reasons did not cross: %v", failure)
	}
}

// TestAnUnreachableRouteIsSaidOutLoud: a bridge with nothing behind it answers
// a code rather than a success with an empty body.
func TestAnUnreachableRouteIsSaidOutLoud(t *testing.T) {
	answer := Bridge{MachineID: "mac-01", AllowCommands: func() bool { return true }}.
		Handle(context.Background(), request(t, ClassCtl, map[string]any{
			"type": "git", "session": pane}))
	if answer.Code != "router_unavailable" || answer.Status != 503 {
		t.Fatalf("answered %q/%d, wanted router_unavailable/503", answer.Code, answer.Status)
	}
}

// TestTheAuthorityIsRereadAtThePointOfNoReturn: admission may have waited on
// disk. Every revocable permission is asked again before an effect, and each
// one has its own sentence because each sends the person somewhere different.
func TestTheAuthorityIsRereadAtThePointOfNoReturn(t *testing.T) {
	ready := Authority{ClockReady: true, RosterReadable: true, RosterAllowsSender: true,
		WriteGateAllows: true}
	cases := []struct {
		name   string
		say    Authority
		code   string
		status int
	}{
		{"the clock is not confirmed", Authority{RosterReadable: true, RosterAllowsSender: true,
			WriteGateAllows: true, ClockReason: "offer_rejected", ClockClearsInMS: 1500},
			"command_clock_uncertain", 503},
		{"the roster could not be read", Authority{ClockReady: true, RosterAllowsSender: true,
			WriteGateAllows: true}, "command_roster_unreadable", 503},
		{"this device is not on it", Authority{ClockReady: true, RosterReadable: true,
			WriteGateAllows: true}, "unknown_sender", 403},
		{"the switch went off while this waited", Authority{ClockReady: true, RosterReadable: true,
			RosterAllowsSender: true}, "cloud_commands_disabled", 403},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := &router{}
			b := open(r)
			b.Authority = func(context.Context, string, bool) Authority { return c.say }
			answer := b.Handle(context.Background(), request(t, ClassCtl, map[string]any{
				"type": "focus", "session": pane, "request": "req"}))
			if answer.Code != c.code || answer.Status != c.status {
				t.Fatalf("refused %q/%d, wanted %q/%d", answer.Code, answer.Status, c.code, c.status)
			}
			if len(r.seen) != 0 {
				t.Fatalf("a denied command still reached this machine: %+v", r.seen)
			}
			if c.code == "command_clock_uncertain" {
				detail, ok := errorOf(t, answer)["detail"].(map[string]any)
				if !ok || detail["reason"] != "offer_rejected" {
					t.Fatalf("the clock's reason did not cross: %v", errorOf(t, answer))
				}
			}
		})
	}
	// And with every permission in hand it routes.
	r := &router{}
	b := open(r)
	b.Authority = func(context.Context, string, bool) Authority { return ready }
	if answer := b.Handle(context.Background(), request(t, ClassCtl, map[string]any{
		"type": "focus", "session": pane, "request": "req"})); answer.Status != 200 {
		t.Fatalf("a permitted command was refused: %+v", answer)
	}
}

// TestOnlyTheWriteWordsAskForTheWriteGate: a diagnostic report is read-level,
// so a paired device may hand one over with remote writes switched off — and
// it is still asked whether the roster still holds that device.
func TestOnlyTheWriteWordsAskForTheWriteGate(t *testing.T) {
	var asked []bool
	b := Bridge{MachineID: "mac-01", Router: &router{}}
	b.Authority = func(_ context.Context, _ string, requiresWriteGate bool) Authority {
		asked = append(asked, requiresWriteGate)
		return Authority{ClockReady: true, RosterReadable: true, RosterAllowsSender: true,
			WriteGateAllows: !requiresWriteGate}
	}
	answer := b.Handle(context.Background(), request(t, ClassCtl, map[string]any{
		"type": "diagnostics.report", "session": MachineReplySession, "request": "req",
		"report": map[string]any{"page": "session"}}))
	if len(asked) != 1 || asked[0] {
		t.Fatalf("the write gate was asked for a read-level command: %v", asked)
	}
	// It has no local capability here, which is the honest answer and not a
	// silent success.
	if answer.Code != "unknown_command" {
		t.Fatalf("answered %q, wanted unknown_command", answer.Code)
	}
}

// TestAPictureCrossesAsBytesOrAsASentence. A URL cannot cross: this machine is
// not reachable from the console, which is the entire reason a relay exists.
func TestAPictureCrossesAsBytesOrAsASentence(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	ask := func(r *router) Answer {
		return open(r).Handle(context.Background(), request(t, ClassCtl, map[string]any{
			"type": "image", "session": pane, "id": "img_7f3a"}))
	}
	answer := ask(&router{body: string(png), media: "image/png"})
	body, ok := answerOf(t, answer)["body"].(map[string]any)
	if !ok {
		t.Fatalf("no body: %s", answer.Payload)
	}
	if body["id"] != "img_7f3a" || body["media_type"] != "image/png" {
		t.Fatalf("the picture lost its reference: %v", body)
	}
	if body["data"] != base64.StdEncoding.EncodeToString(png) {
		t.Fatalf("the bytes did not cross: %v", body["data"])
	}
	if count, _ := body["byte_count"].(float64); int(count) != len(png) {
		t.Fatalf("byte_count is %v, wanted %d", body["byte_count"], len(png))
	}

	if answer := ask(&router{body: "GIF89a", media: "image/gif"}); answer.Code != "image_media_type_unsupported" {
		t.Fatalf("a GIF was carried: %+v", answer)
	}
	if answer := ask(&router{empty: true, media: "image/png"}); answer.Code != "image_empty" {
		t.Fatalf("an empty picture was carried: %+v", answer)
	}
	huge := &router{body: strings.Repeat("x", imageMaxEncodedBytes()+1), media: "image/png"}
	answer = ask(huge)
	if answer.Code != "image_too_large_for_cloud" || answer.Status != 413 {
		t.Fatalf("answered %q/%d, wanted image_too_large_for_cloud/413", answer.Code, answer.Status)
	}
	detail, ok := errorOf(t, answer)["detail"].(map[string]any)
	if !ok || detail["limit_bytes"] == nil {
		t.Fatalf("the refusal does not say what the limit was: %v", errorOf(t, answer))
	}
}

// TestADocumentCrossesOnlyAsInertText.
func TestADocumentCrossesOnlyAsInertText(t *testing.T) {
	ask := func(r *router) Answer {
		return open(r).Handle(context.Background(), request(t, ClassCtl, map[string]any{
			"type": "document", "session": pane, "request": "req", "scope": "task",
			"task": taskID, "path": "report.md"}))
	}
	answer := ask(&router{body: "# report\n", media: "text/markdown; charset=utf-8"})
	body, ok := answerOf(t, answer)["body"].(map[string]any)
	if !ok || body["scope"] != "task" || body["task"] != taskID || body["path"] != "report.md" {
		t.Fatalf("the document lost its identity: %s", answer.Payload)
	}
	if body["data"] != base64.StdEncoding.EncodeToString([]byte("# report\n")) {
		t.Fatalf("the text did not cross: %v", body["data"])
	}
	if answer := ask(&router{body: "<p>", media: "text/html; charset=utf-8"}); answer.Code != "document_media_type_unsupported" {
		t.Fatalf("HTML crossed: %+v", answer)
	}
	if answer := ask(&router{body: "\xff\xfe", media: "text/plain; charset=utf-8"}); answer.Code != "document_not_utf8" {
		t.Fatalf("bytes that are not text crossed: %+v", answer)
	}
	big := &router{body: strings.Repeat("a", documentMaximumBytes+1), media: "text/plain; charset=utf-8"}
	if answer := ask(big); answer.Code != "document_too_large" || answer.Status != 413 {
		t.Fatalf("answered %q/%d, wanted document_too_large/413", answer.Code, answer.Status)
	}
}

// TestADocumentListingLosesThisMachinesAddress: the relative path is the
// requested identity and stays; the local URL and the duplicate label do not.
func TestADocumentListingLosesThisMachinesAddress(t *testing.T) {
	listing := `{"documents":[
	  {"source":"project","path":"notes.md","label":"notes.md","bytes":12,"modified":1787817600.5,
	   "url":"http://127.0.0.1:7727/v1/sessions/%2519/documents/project/notes.md"},
	  {"source":"task","path":"report.md","label":"report.md","bytes":40,"modified":1787817601.0,
	   "url":"http://127.0.0.1:7727/v1/sessions/%2519/documents/task/` + taskID + `/report.md",
	   "task":{"id":"` + taskID + `","title":"Cloud"}}]}`
	answer := open(&router{body: listing}).Handle(context.Background(),
		request(t, ClassCtl, map[string]any{"type": "documents", "session": pane}))
	body, ok := answerOf(t, answer)["body"].(map[string]any)
	if !ok {
		t.Fatalf("no body: %s", answer.Payload)
	}
	rows, ok := body["documents"].([]any)
	if !ok || len(rows) != 2 {
		t.Fatalf("the listing is %v", body)
	}
	if strings.Contains(string(answer.Payload), "127.0.0.1") {
		t.Fatalf("this machine's address crossed: %s", answer.Payload)
	}
	first := rows[0].(map[string]any)
	if first["scope"] != "project" || first["path"] != "notes.md" {
		t.Fatalf("the first row is %v", first)
	}
	second := rows[1].(map[string]any)
	if second["scope"] != "task" || second["task"] != taskID || second["title"] != "Cloud" {
		t.Fatalf("the second row is %v", second)
	}
	// A listing this machine could not have produced is refused rather than
	// forwarded: a label that disagrees with its path is the shape of
	// something else answering.
	bad := open(&router{body: `{"documents":[{"source":"project","path":"a.md","label":"b.md",` +
		`"bytes":1,"modified":1.0,"url":"x"}]}`}).Handle(context.Background(),
		request(t, ClassCtl, map[string]any{"type": "documents", "session": pane}))
	if bad.Code != "document_listing_invalid" || bad.Status != 502 {
		t.Fatalf("answered %q/%d, wanted document_listing_invalid/502", bad.Code, bad.Status)
	}
}

// TestAnOlderPageStillSends: a page open on a previous hosted build sends no
// request id. Reloading must improve delivery semantics, not become a
// prerequisite for sending at all.
func TestAnOlderPageStillSends(t *testing.T) {
	r := &router{}
	answer := open(r).Handle(context.Background(), request(t, ClassCtl, map[string]any{
		"type": "send", "session": pane, "text": "hello", "images": []any{}}))
	if len(r.seen) != 1 || r.last().Path != "/v1/sessions/%2519/send" {
		t.Fatalf("the send did not reach this machine: %+v", r.seen)
	}
	if answer.Published() {
		t.Fatalf("an answer was published to a page that named no channel: %s", answer.Payload)
	}
	if answer.Status != 200 {
		t.Fatalf("status %d, wanted 200", answer.Status)
	}
}

// TestTheVocabularyAndTheImplementedListAgreeWithTheCatalog. Two lists that
// have to agree, which is why one is derived from the other rather than
// written down twice.
func TestTheVocabularyAndTheImplementedListAgreeWithTheCatalog(t *testing.T) {
	words := Vocabulary()
	if !sort.StringsAreSorted(words) {
		t.Fatalf("the vocabulary is not sorted: %v", words)
	}
	for _, word := range words {
		if !Knows(word) {
			t.Fatalf("%s is in the vocabulary and unknown to Knows", word)
		}
	}
	implemented := map[string]bool{}
	for _, word := range Implemented() {
		implemented[word] = true
		if !Knows(word) {
			t.Fatalf("%s is implemented and not in the vocabulary", word)
		}
	}
	// The daemon's published list must not promise what it refuses.
	for _, word := range []string{"shell", "skills",
		"diagnostics.report", "diagnostics.events", "dispatch"} {
		if implemented[word] {
			t.Fatalf("%s is advertised and has no local capability", word)
		}
	}
	for _, word := range []string{"send", "answer", "end", "focus", "start", "resume", "voice", "intents", "agent",
		"transcript", "info", "git", "git-diff", "screen", "image", "documents", "document", "places",
		"past-sessions", "schedules", "schedule", "schedule-create", "schedule-update", "schedule-delete",
		"schedule-run", "schedule-webhook-bind-v1", "snippets", "snippet-create", "snippet-update", "snippet-delete",
		"snippet-order", "push-key", "push-subscribe", "push-unsubscribe", "push-test",
		"board", "board.items", "timeline", "projects", "project-worktree-lifecycle",
		"project-worktree-lifecycle-refresh", "landings",
		"work.board", "work.backlog", "work.proposals", "work.decisions", "work.digests",
		"work.v2.items", "work.v2.proposals", "work.v2.session-todos", "work.v2.image", "work.v2.create",
		"work.v2.assign", "work.v2.edit", "work.v2.cancel", "work.v2.image-create", "work.v2.image-delete", "work.v2.proposal-resolve",
		"work.v2.todo-create", "work.v2.todo-image-create", "work.v2.todo-action"} {
		if !implemented[word] {
			t.Fatalf("%s has a local capability and is not advertised", word)
		}
	}
}

func workV2Writes() map[string]map[string]any {
	return map[string]map[string]any{
		"work.v2.create": {"type": "work.v2.create", "session": MachineReplySession,
			"request": "req", "item": map[string]any{"project_id": "p1", "kind": "feature", "title": "A"}},
		"work.v2.assign": {"type": "work.v2.assign", "session": MachineReplySession,
			"request": "req", "id": "w1", "item": map[string]any{"mode": "existing", "terminal": pane}},
		"work.v2.edit": {"type": "work.v2.edit", "session": MachineReplySession,
			"request": "req", "id": "w1", "item": map[string]any{"expected_version": 2, "title": "Edited"}},
		"work.v2.cancel": {"type": "work.v2.cancel", "session": MachineReplySession,
			"request": "req", "id": "w1", "item": map[string]any{"expected_version": 3, "reason": "Deleted"}},
		"work.v2.image-create": {"type": "work.v2.image-create", "session": MachineReplySession,
			"request": "req", "id": "w1", "item": map[string]any{"expected_version": 1, "data_url": "data:image/png;base64,cG5n"}},
		"work.v2.image-delete": {"type": "work.v2.image-delete", "session": MachineReplySession,
			"request": "req", "id": "w1", "image": "img1", "item": map[string]any{"expected_version": 2}},
		"work.v2.proposal-resolve": {"type": "work.v2.proposal-resolve", "session": MachineReplySession,
			"request": "req", "id": "pr1", "decision": "accept", "item": map[string]any{}},
		"work.v2.todo-create": {"type": "work.v2.todo-create", "session": MachineReplySession,
			"request": "req", "terminal": pane, "item": map[string]any{"text": "Ship it"}},
		"work.v2.todo-image-create": {"type": "work.v2.todo-image-create", "session": MachineReplySession,
			"request": "req", "terminal": pane, "id": "td1",
			"item": map[string]any{"expected_version": 1, "data_url": "data:image/png;base64,cG5n"}},
		"work.v2.todo-action": {"type": "work.v2.todo-action", "session": MachineReplySession,
			"request": "req", "terminal": pane, "id": "td1", "action": "send", "item": map[string]any{}},
	}
}

// TestWorkV2WritesArePersonActions is the authority boundary for the hosted
// Board. The viewer may create and assign an item or operate a direct to-do,
// but must not inherit the machine/orchestrator identity that the in-process
// router uses for transport. The device header takes that identity away.
func TestWorkV2WritesArePersonActions(t *testing.T) {
	for word, body := range workV2Writes() {
		t.Run(word, func(t *testing.T) {
			r := &router{}
			if answer := open(r).Handle(context.Background(), request(t, ClassCtl, body)); !answer.OK() {
				t.Fatalf("%s was refused: %+v", word, answer)
			}
			if got := r.last().Header[actorHeader]; got != actorDevice {
				t.Fatalf("%s carried %s=%q, wanted %q", word, actorHeader, got, actorDevice)
			}
			if got := r.last().Header["Idempotency-Key"]; got != "req" {
				t.Fatalf("%s carried the key %q, wanted the viewer request id", word, got)
			}
		})
	}
}

func TestWorkV2WritesRespectTheCloudWriteSwitch(t *testing.T) {
	for word, body := range workV2Writes() {
		t.Run(word, func(t *testing.T) {
			r := &router{}
			answer := (Bridge{MachineID: "mac-01", Router: r}).Handle(
				context.Background(), request(t, ClassCtl, body))
			if answer.Code != "cloud_commands_disabled" {
				t.Fatalf("%s with the switch off answered %q", word, answer.Code)
			}
			if len(r.seen) != 0 {
				t.Fatalf("%s reached the machine with the switch off", word)
			}
		})
	}
}

// TestAScheduleWriteIsJudgedAsTheDeviceThatSentIt is the one rule these
// schedule-mutating words have that the other commands do not.
//
// A schedule write reaches a route under `/v1/orchestrator/`, and the credential
// this daemon stamps on its own in-process requests covers every path under
// `/v1/orchestrator/` — so without a word from the request itself, a person
// changing a daily schedule from their phone would be judged as this
// machine's own automation and refused by `MachineRefusal`, which exists to
// keep cron jobs out of a standing arrangement rather than people. The header
// can only take the machine door away, never open it.
func TestAScheduleWriteIsJudgedAsTheDeviceThatSentIt(t *testing.T) {
	writes := map[string]map[string]any{
		"schedule-create": {"type": "schedule-create", "session": MachineReplySession,
			"request": "req", "schedule": map[string]any{"title": "morning"}},
		"schedule-update": {"type": "schedule-update", "session": MachineReplySession,
			"request": "req", "id": scheduleID, "schedule": map[string]any{"title": "morning"}},
		"schedule-delete": {"type": "schedule-delete", "session": MachineReplySession,
			"request": "req", "id": scheduleID},
		"schedule-run": {"type": "schedule-run", "session": MachineReplySession,
			"request": "req", "id": scheduleID},
		"schedule-webhook-bind-v1": {"type": "schedule-webhook-bind-v1",
			"request_id": scheduleWebhookRequestID, "hook_id": scheduleWebhookHook,
			"schedule_id": scheduleID, "replace_hook_id": nil},
	}
	for word, body := range writes {
		t.Run(word, func(t *testing.T) {
			r := &router{}
			if answer := open(r).Handle(context.Background(), request(t, ClassCtl, body)); !answer.OK() {
				t.Fatalf("%s was refused: %+v", word, answer)
			}
			if got := r.last().Header[actorHeader]; got != actorDevice {
				t.Fatalf("%s carried %s=%q, wanted %q", word, actorHeader, got, actorDevice)
			}
			// And the key is still the viewer's own request id: a retried
			// save is not a second schedule.
			wantKey := "req"
			if word == "schedule-webhook-bind-v1" {
				wantKey = scheduleWebhookRequestID
			}
			if got := r.last().Header["Idempotency-Key"]; got != wantKey {
				t.Fatalf("%s carried the key %q, wanted %q", word, got, wantKey)
			}
		})
	}
	// A read of the same schedules asks for nothing of the sort: it changes
	// nothing, so which door it came in by does not arise.
	r := &router{}
	open(r).Handle(context.Background(), request(t, ClassCtl, map[string]any{
		"type": "schedules", "session": MachineReplySession, "request": "req"}))
	if _, named := r.last().Header[actorHeader]; named {
		t.Fatalf("a read named an actor: %v", r.last().Header)
	}
}

// pushWrites is the three words that change something about notifications,
// each with the body the hosted console really sends.
func pushWrites() map[string]map[string]any {
	return map[string]map[string]any{
		"push-subscribe": {"type": "push-subscribe", "session": MachineReplySession,
			"request": "req", "subscription": map[string]any{
				"endpoint": "https://web.push.apple.com/QW",
				"keys":     map[string]any{"p256dh": "BPk", "auth": "c2VjcmV0"}}},
		"push-unsubscribe": {"type": "push-unsubscribe", "session": MachineReplySession,
			"request": "req", "id": pushID},
		"push-test": {"type": "push-test", "session": MachineReplySession,
			"request": "req", "target": ""},
	}
}

// TestAskingToBeNotifiedIsReadLevel is why these three are not behind the
// write switch.
//
// That switch is about typing into somebody's session. Asking to be told when
// a session needs an answer is the reading half arriving by a different road,
// and a phone paired read-only is exactly the device the feature exists for —
// the Swift bridge says the same in one line, listing all three in
// `readLevelCommandTypes` beside the two diagnostics words. A machine with remote
// writes off would otherwise refuse the registration and never say why the
// notifications never came.
func TestAskingToBeNotifiedIsReadLevel(t *testing.T) {
	for word, body := range pushWrites() {
		t.Run(word, func(t *testing.T) {
			r := &router{}
			// The zero value's write switch: nobody has said anybody may
			// write, which is the default everywhere else in this package.
			closed := Bridge{MachineID: "mac-01", Router: r}
			answer := closed.Handle(context.Background(), request(t, ClassCtl, body))
			if !answer.OK() {
				t.Fatalf("%s was refused with the write switch off: %d/%q", word,
					answer.Status, answer.Code)
			}
			if len(r.seen) != 1 {
				t.Fatalf("%s asked this machine %d times, wanted once", word, len(r.seen))
			}
		})
	}
	// And the switch still holds every word that types into a session.
	r := &router{}
	closed := Bridge{MachineID: "mac-01", Router: r}
	answer := closed.Handle(context.Background(), request(t, ClassCtl, map[string]any{
		"type": "send", "session": pane, "request": "req", "text": "hi", "images": []any{}}))
	if answer.Code != "cloud_commands_disabled" {
		t.Fatalf("a send with the switch off answered %q", answer.Code)
	}
}

// TestAPushWriteIsJudgedAsTheDeviceThatSentIt is the rule the schedule words
// have, said for these three.
//
// `/v1/push/unsubscribe` asks *which door* the caller came in by: this
// machine's own token is exempt from the check that a subscription belongs to
// the device removing it, because that exemption is how a script cleans up
// after itself. A Cloud viewer reaches these routes in process under this
// machine's own local credential (`internal/transport/cloud.LocalAuthorizer`),
// so without a word from the request itself a person on their phone would
// inherit that script's exemption and be able to remove a subscription that is
// not theirs. The header can only take a door away, never open one.
func TestAPushWriteIsJudgedAsTheDeviceThatSentIt(t *testing.T) {
	for word, body := range pushWrites() {
		t.Run(word, func(t *testing.T) {
			r := &router{}
			if answer := open(r).Handle(context.Background(), request(t, ClassCtl, body)); !answer.OK() {
				t.Fatalf("%s was refused: %+v", word, answer)
			}
			if got := r.last().Header[actorHeader]; got != actorDevice {
				t.Fatalf("%s carried %s=%q, wanted %q", word, actorHeader, got, actorDevice)
			}
			// The viewer's own request id is the key: a retried registration
			// is not a second subscription.
			if got := r.last().Header["Idempotency-Key"]; got != "req" {
				t.Fatalf("%s carried the key %q, wanted the request id", word, got)
			}
		})
	}
	// Asking for the key changes nothing, so which door it came in by does
	// not arise.
	r := &router{}
	open(r).Handle(context.Background(), request(t, ClassCtl, map[string]any{
		"type": "push-key", "session": MachineReplySession, "request": "req"}))
	if _, named := r.last().Header[actorHeader]; named {
		t.Fatalf("a read named an actor: %v", r.last().Header)
	}
}

// TestEveryDivergenceIsAboutAWordThisDaemonActuallyAnswers. A divergence on a
// word with no route would be a note about nothing, and the list is read at
// the moment somebody decides what to advertise.
func TestEveryDivergenceIsAboutAWordThisDaemonActuallyAnswers(t *testing.T) {
	implemented := map[string]bool{}
	for _, word := range Implemented() {
		implemented[word] = true
	}
	found := Divergences()
	if len(found) == 0 {
		t.Fatal("no divergences at all, which this port has not earned yet")
	}
	for word, why := range found {
		if !implemented[word] {
			t.Fatalf("%s diverges and is not routed", word)
		}
		if len(why) < 40 {
			t.Fatalf("%s: %q says too little to act on", word, why)
		}
	}
	// The four measured on 2026-09-18 against this daemon's own routes.
	for _, word := range []string{"board", "transcript", "info", "end"} {
		if found[word] == "" {
			t.Fatalf("%s answers differently from the hosted contract and says nothing about it", word)
		}
	}
}

// TestChannelSegmentIsEncodeURIComponent. A tmux pane is `%19`, and the same
// function names both a relay channel's segment and a local route's path
// segment, exactly as the Swift bridge uses it.
func TestChannelSegmentIsEncodeURIComponent(t *testing.T) {
	cases := map[string]string{
		"%19":                     "%2519",
		"/Users/sean/code":        "%2FUsers%2Fsean%2Fcode",
		"mac-01":                  "mac-01",
		"a b":                     "a%20b",
		"~_.!*'()-":               "~_.!*'()-",
		MachineReplySession:       "__clawdline_machine__",
		"session|with|separators": "session%7Cwith%7Cseparators",
	}
	for in, want := range cases {
		if got := ChannelSegment(in); got != want {
			t.Fatalf("%q encoded to %q, wanted %q", in, got, want)
		}
	}
}

// TestBoardItemsRoutesTheWholeQuery holds the one rule board.items has that
// board does not: the Swift app sends all four parameters, always, and the two
// words share one path. A route that dropped a zero cursor would read page one
// as "no cursor" — the same answer today, a different one the day a default
// changes.
func TestBoardItemsRoutesTheWholeQuery(t *testing.T) {
	o := catalog["board.items"]
	if o.route == nil {
		t.Fatal("board.items has no route")
	}
	got := o.route(plan{project: "project-b542053a6fe9f75722f3a24a", audience: "human", offset: 0, limit: 30})
	want := map[string]string{"project": "project-b542053a6fe9f75722f3a24a", "audience": "human",
		"cursor": "0", "limit": "30"}
	if got.Method != "GET" || got.Path != "/v1/board" {
		t.Fatalf("route = %s %s, want GET /v1/board", got.Method, got.Path)
	}
	for k, v := range want {
		if got.Query[k] != v {
			t.Fatalf("query[%s] = %q, want %q (whole query %v)", k, got.Query[k], v, got.Query)
		}
	}
	if len(got.Query) != len(want) {
		t.Fatalf("query carries %v, want exactly %v", got.Query, want)
	}
}
