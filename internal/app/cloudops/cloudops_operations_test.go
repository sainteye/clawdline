package cloudops

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestWorkV2TodoActionCarriesReopen(t *testing.T) {
	r := &router{}
	answer := open(r).Handle(context.Background(), request(t, ClassCtl, map[string]any{
		"type": "work.v2.todo-action", "session": MachineReplySession, "request": "req-work-v2-todo-reopen",
		"terminal": pane, "id": "td1", "action": "reopen", "item": map[string]any{},
	}))
	if answer.Status != 200 || len(r.seen) != 1 {
		t.Fatalf("reopen answer: %+v; routes: %+v", answer, r.seen)
	}
	if got := r.last(); got.Method != "POST" || got.Path != "/v1/work/v2/session-todos/%2519/td1/reopen" || string(got.Body) != `{}` {
		t.Fatalf("reopen route: %+v", got)
	}
}

// TestAMovedHookBindCarriesItsRevision: a moved hook is bound on the target
// at the revision the move answered, so `hook_revision` reaches the local route
// as the integer it was; a body without it is the plain bind, unchanged.
func TestAMovedHookBindCarriesItsRevision(t *testing.T) {
	r := &router{}
	answer := open(r).Handle(context.Background(), request(t, ClassCtl, map[string]any{
		"type": "schedule-webhook-bind-v1", "request_id": scheduleWebhookRequestID,
		"hook_id": scheduleWebhookHook, "schedule_id": scheduleID, "replace_hook_id": nil,
		"hook_revision": 2}))
	if !answer.OK() || len(r.seen) != 1 {
		t.Fatalf("the bind with a revision was refused: %+v", answer)
	}
	want := `{"hook_id":"` + scheduleWebhookHook + `","hook_revision":2,"replace_hook_id":null,` +
		`"request_id":"` + scheduleWebhookRequestID + `","schedule_id":"` + scheduleID + `"}`
	if got := string(r.last().Body); got != want {
		t.Fatalf("the body is %s, wanted %s", got, want)
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
	for _, word := range []string{"send", "answer", "end", "focus", "interrupt", "smart-title", "start", "resume", "voice", "intents",
		"schedule-create", "schedule-update", "schedule-delete", "schedule-run",
		"schedule-webhook-bind-v1", "pair-agent-start",
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
		case "pair-agent-start":
			body["session"] = MachineReplySession
			body["offer"], body["machine_id"], body["machine_name"] = "opaque", "mac_target", "Target"
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

func TestProjectFileCloudReadsDoNotGrantRemoteWrites(t *testing.T) {
	r := &router{}
	closed := Bridge{MachineID: "mac-01", Router: r}
	file := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	read := closed.Handle(context.Background(), request(t, ClassCtl, map[string]any{
		"type": "project-file-read", "session": MachineReplySession, "request": "read-1", "project": "p1", "file": file}))
	if read.Status != 200 || len(r.seen) != 1 {
		t.Fatalf("read with remote writes off: %+v; requests: %+v", read, r.seen)
	}
	tree := closed.Handle(context.Background(), request(t, ClassCtl, map[string]any{
		"type": "project-tree-read", "session": MachineReplySession, "request": "tree-1", "project": "p1", "path": "src/page.ts"}))
	if tree.Status != 200 || len(r.seen) != 2 {
		t.Fatalf("tree read with remote writes off: %+v; requests: %+v", tree, r.seen)
	}
	save := closed.Handle(context.Background(), request(t, ClassCtl, map[string]any{
		"type": "project-file-save", "session": MachineReplySession, "request": "save-1", "project": "p1", "file": file,
		"item": map[string]any{"expected_version": "v1", "content": "updated"}}))
	if save.Code != "cloud_commands_disabled" || len(r.seen) != 2 {
		t.Fatalf("write with remote writes off: %+v; requests: %+v", save, r.seen)
	}
}

func TestProjectUnifyCloudPlanIsAReadAndApplyNeedsTheWriteGate(t *testing.T) {
	r := &router{}
	closed := Bridge{MachineID: "mac-01", Router: r}
	read := closed.Handle(context.Background(), request(t, ClassCtl, map[string]any{
		"type": "project-unify-plan", "session": MachineReplySession, "request": "plan-1", "project": "p1"}))
	if read.Status != 200 || len(r.seen) != 1 {
		t.Fatalf("plan with remote writes off: %+v; requests: %+v", read, r.seen)
	}
	apply := map[string]any{"type": "project-unify-apply", "session": MachineReplySession, "request": "apply-1", "project": "p1",
		"item": map[string]any{"version": "v1"}}
	refused := closed.Handle(context.Background(), request(t, ClassCtl, apply))
	if refused.Code != "cloud_commands_disabled" || len(r.seen) != 1 {
		t.Fatalf("apply with remote writes off: %+v; requests: %+v", refused, r.seen)
	}
	open := Bridge{MachineID: "mac-01", Router: r, AllowCommands: func() bool { return true }}
	if answer := open.Handle(context.Background(), request(t, ClassCtl, apply)); answer.Status != 200 || len(r.seen) != 2 {
		t.Fatalf("apply with remote writes on: %+v; requests: %+v", answer, r.seen)
	}
	if key := r.last().Header["Idempotency-Key"]; key != "apply-1" {
		t.Fatalf("apply carried idempotency key %q", key)
	}
	if answer := open.Handle(context.Background(), request(t, ClassCtl, map[string]any{"type": "project-unify-apply",
		"session": MachineReplySession, "request": "apply-2", "project": "p1"})); answer.Code != "malformed_command" {
		t.Fatalf("apply without a version body: %+v", answer)
	}
}

// A unify apply that stopped after one action answers what ran, what failed
// and the plan read again, as the local route does. Over Cloud that crosses as
// the body beside the refusal's status: an `error` would reach the phone as
// the code alone, and the person would see only that it failed.
func TestAStoppedUnifyApplyCrossesWithWhatRanAndThePlan(t *testing.T) {
	stopped := `{"outcome":"stopped",` +
		`"ran":[{"kind":"rules_add_import","paths":["CLAUDE.md"],"description":"Add the import.","edits":[]}],` +
		`"failed":{"kind":"skill_link","paths":[".claude/skills/review"],"description":"Link review.","link_target":"../../.agents/skills/review","edits":[]},` +
		`"plan":{"status":"drifting","version":"v2","links_available":true,"rules":{},"skills":[],"actions":[],"conflicts":[]},` +
		`"error":"file_permission","detail":"Unify stopped: A file or directory could not be written with this machine's permissions."}`
	r := &router{status: 500, body: stopped}
	open := Bridge{MachineID: "mac-01", Router: r, AllowCommands: func() bool { return true }}
	answer := open.Handle(context.Background(), request(t, ClassCtl, map[string]any{"type": "project-unify-apply",
		"session": MachineReplySession, "request": "apply-1", "project": "p1", "item": map[string]any{"version": "v1"}}))
	if answer.Status != 500 || answer.Code != "file_permission" {
		t.Fatalf("stopped apply: status %d code %q", answer.Status, answer.Code)
	}
	var payload struct {
		Status int            `json:"status"`
		Error  map[string]any `json:"error"`
		Body   struct {
			Outcome string           `json:"outcome"`
			Ran     []map[string]any `json:"ran"`
			Failed  map[string]any   `json:"failed"`
			Plan    map[string]any   `json:"plan"`
			Error   string           `json:"error"`
			Detail  string           `json:"detail"`
		} `json:"body"`
	}
	if err := json.Unmarshal(answer.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Status != 500 || payload.Error != nil {
		t.Fatalf("payload status %d error %v", payload.Status, payload.Error)
	}
	got := payload.Body
	if got.Outcome != "stopped" || len(got.Ran) != 1 || got.Ran[0]["kind"] != "rules_add_import" ||
		got.Failed["kind"] != "skill_link" || got.Plan["version"] != "v2" ||
		got.Error != "file_permission" || !strings.HasPrefix(got.Detail, "Unify stopped: ") {
		t.Fatalf("stopped body did not cross whole: %s", answer.Payload)
	}

	// Any other refusal of the same word is still a refusal, filtered by code.
	r.body = `{"error":"plan_changed","detail":"The plan changed.","plan":{"version":"v3"}}`
	r.status = 409
	refused := open.Handle(context.Background(), request(t, ClassCtl, map[string]any{"type": "project-unify-apply",
		"session": MachineReplySession, "request": "apply-2", "project": "p1", "item": map[string]any{"version": "v1"}}))
	if refused.Code != "plan_changed" || strings.Contains(string(refused.Payload), `"body"`) ||
		strings.Contains(string(refused.Payload), `"v3"`) {
		t.Fatalf("plan_changed: %s", refused.Payload)
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
		name: "a decision read must name a work-shaped id",
		body: map[string]any{"type": "work.decision", "session": MachineReplySession,
			"request": "req", "id": "../other"},
		code: "malformed_read", published: true,
	}, {
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
		name: "a Project file read cannot name a path",
		body: map[string]any{"type": "project-file-read", "session": MachineReplySession,
			"request": "req", "project": "p1", "file": "../../secret"},
		code: "malformed_read", published: true,
	}, {
		name: "a Project tree read cannot climb outside the Project",
		body: map[string]any{"type": "project-tree-read", "session": MachineReplySession,
			"request": "req", "project": "p1", "path": "../secret"},
		code: "malformed_read", published: true,
	}, {
		name: "a Project tree directory cannot name Git metadata",
		body: map[string]any{"type": "project-tree-list", "session": MachineReplySession,
			"request": "req", "project": "p1", "directory": ".git"},
		code: "malformed_read", published: true,
	}, {
		name: "a Project file save cannot name a path",
		body: map[string]any{"type": "project-file-save", "session": MachineReplySession,
			"request": "req", "project": "p1", "file": "../../secret",
			"item": map[string]any{"expected_version": "v1", "content": "updated"}},
		code: "malformed_command", published: true,
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
		name: "a webhook bind at a revision below zero",
		body: map[string]any{"type": "schedule-webhook-bind-v1",
			"request_id": scheduleWebhookRequestID, "hook_id": scheduleWebhookHook,
			"schedule_id": scheduleID, "replace_hook_id": nil, "hook_revision": -1},
		code: "malformed_command", published: true,
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
	for _, word := range []string{"skills",
		"diagnostics.report", "diagnostics.events", "dispatch"} {
		if implemented[word] {
			t.Fatalf("%s is advertised and has no local capability", word)
		}
	}
	for _, word := range []string{"send", "answer", "end", "focus", "interrupt", "smart-title", "start", "resume", "voice", "intents", "agent", "shell",
		"transcript", "info", "git", "git-diff", "screen", "image", "documents", "document", "places",
		"past-sessions", "schedules", "coordination.leases", "coordination.waits", "coordination.pauses", "schedule", "schedule-create", "schedule-update", "schedule-delete",
		"schedule-run", "schedule-webhook-bind-v1", "snippets", "snippet-create", "snippet-update", "snippet-delete",
		"snippet-order", "push-key", "push-subscribe", "push-unsubscribe", "push-test",
		"board", "board-command", "board.items", "timeline", "projects", "project-file-list", "project-file-read", "project-file-save", "project-tree-list", "project-tree-read", "project-unify-plan", "project-unify-apply", "project-worktree-lifecycle",
		"project-worktree-lifecycle-refresh", "capacity", "default-models", "default-models-update", "work-gate-settings", "work-gate-settings-update", "machine-usage", "update", "update-apply", "personas",
		"work.proposals", "work.decisions", "work.decision", "work.digests",
		"work.v2.item", "work.v2.items", "work.v2.search", "work.v2.proposals", "work.v2.session-todos", "work.v2.image", "work.v2.create",
		"work.v2.gate-export", "work.v2.gate-decision", "work.v2.gate-purge",
		"work.v2.assign", "work.v2.persona-suggestion", "work.v2.remind", "work.v2.edit", "work.v2.cancel", "work.v2.complete", "work.v2.seen", "work.v2.image-create", "work.v2.image-delete", "work.v2.proposal-resolve",
		"work.v2.convert",
		"work.v2.todo-create", "work.v2.todo-image-create", "work.v2.todo-action",
		"usage.session", "usage.task", "usage.item", "usage.compare-compaction", "usage.compare-handoff", "usage.work-units", "usage.work-samples",
		"verification.list", "verification.get", "verification.create", "verification.note",
		"verification.criterion", "verification.close", "verification.delete"} {
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
		"work.v2.convert": {"type": "work.v2.convert", "session": MachineReplySession,
			"request": "req", "id": "w1", "item": map[string]any{"expected_version": 2, "kind": "plan"}},
		"work.v2.persona-suggestion": {"type": "work.v2.persona-suggestion", "session": MachineReplySession,
			"request": "req", "id": "w1", "item": map[string]any{"expected_version": 1}},
		"work.v2.remind": {"type": "work.v2.remind", "session": MachineReplySession,
			"request": "req", "id": "w1", "item": map[string]any{"expected_version": 2}},
		"work.v2.edit": {"type": "work.v2.edit", "session": MachineReplySession,
			"request": "req", "id": "w1", "item": map[string]any{"expected_version": 2, "title": "Edited"}},
		"work.v2.gate-decision": {"type": "work.v2.gate-decision", "session": MachineReplySession,
			"request": "req", "id": "w1", "item": map[string]any{"expected_version": 5, "action": "direction", "reason": "Explain", "direction": "Repair"}},
		"work.v2.gate-purge": {"type": "work.v2.gate-purge", "session": MachineReplySession,
			"request": "req", "id": "w1", "item": map[string]any{"expected_version": 5, "sha256": "abc"}},
		"work.v2.cancel": {"type": "work.v2.cancel", "session": MachineReplySession,
			"request": "req", "id": "w1", "item": map[string]any{"expected_version": 3, "reason": "Deleted"}},
		"work.v2.complete": {"type": "work.v2.complete", "session": MachineReplySession,
			"request": "req", "id": "w1", "item": map[string]any{"expected_version": 4}},
		"work.v2.seen": {"type": "work.v2.seen", "session": MachineReplySession,
			"request": "req", "id": "w1", "item": map[string]any{"phase": "done"}},
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

func TestDecisionAnswerIsOneDeviceAction(t *testing.T) {
	id := "10000000-0000-4000-8000-000000000001"
	valid := map[string]any{"type": "work.decision-answer", "session": MachineReplySession,
		"request": "press-1", "id": id, "answer": "Proceed"}
	r := &router{}
	if got := open(r).Handle(context.Background(), request(t, ClassCtl, valid)); !got.OK() {
		t.Fatalf("decision answer refused: %+v", got)
	}
	if got := r.last(); got.Method != "POST" || got.Path != "/v1/work/decisions/"+id ||
		got.Header[actorHeader] != actorDevice || got.Header["Idempotency-Key"] != "press-1" ||
		string(got.Body) != `{"answer":"Proceed"}` {
		t.Fatalf("decision answer route: %+v", got)
	}
	for _, change := range []func(map[string]any){
		func(b map[string]any) { b["id"] = "../other" },
		func(b map[string]any) { b["answer"] = " " },
		func(b map[string]any) { b["extra"] = true },
	} {
		bad := make(map[string]any, len(valid)+1)
		for k, v := range valid {
			bad[k] = v
		}
		change(bad)
		if got := open(&router{}).Handle(context.Background(), request(t, ClassCtl, bad)); got.OK() {
			t.Fatalf("malformed decision answer accepted: %+v", bad)
		}
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

// TestACloudSubscriptionCarriesItsCloudIDToTheRoute: a browser that
// subscribed with Clawdline Cloud's key sends Cloud's id beside its own object,
// and the route reads both in one document. An id inside the object as well
// is two answers to one question, and is refused.
func TestACloudSubscriptionCarriesItsCloudIDToTheRoute(t *testing.T) {
	const cloudID = "c1000000-0000-4000-8000-000000000001"
	subscription := func() map[string]any {
		return map[string]any{"endpoint": "https://web.push.apple.com/QW",
			"keys": map[string]any{"p256dh": "BPk", "auth": "c2VjcmV0"}}
	}
	r := &router{}
	answer := open(r).Handle(context.Background(), request(t, ClassCtl, map[string]any{
		"type": "push-subscribe", "session": MachineReplySession, "request": "req",
		"subscription": subscription(), "cloud_subscription_id": cloudID}))
	if !answer.OK() {
		t.Fatalf("refused: %+v", answer)
	}
	want := `{"cloud_subscription_id":"` + cloudID + `","endpoint":"https://web.push.apple.com/QW",` +
		`"keys":{"auth":"c2VjcmV0","p256dh":"BPk"}}`
	if got := r.last(); got.Path != "/v1/push/subscribe" || string(got.Body) != want {
		t.Fatalf("routed %s %s", got.Path, got.Body)
	}

	twice := subscription()
	twice["cloud_subscription_id"] = cloudID
	r = &router{}
	answer = open(r).Handle(context.Background(), request(t, ClassCtl, map[string]any{
		"type": "push-subscribe", "session": MachineReplySession, "request": "req",
		"subscription": twice, "cloud_subscription_id": cloudID}))
	if answer.OK() || len(r.seen) != 0 {
		t.Fatalf("an id given twice was routed: %+v", answer)
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
	// The remaining differences measured against this daemon's own routes.
	for _, word := range []string{"board", "transcript", "info"} {
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

// TestSessionsSnapshotAnswersTheIdsThePublisherStated. The page reads
// `{"sessions":[ids],"complete":bool}` under `read:<request>` on the machine's
// reply channel (`_askSessionSnapshot` in net/cloud-client.js), and accepts
// `orchestrator: true` beside the three keys. It is a read: the write switch,
// off here, does not gate it.
func TestSessionsSnapshotAnswersTheIdsThePublisherStated(t *testing.T) {
	var firsts []bool
	bridge := Bridge{MachineID: "mac-01", SessionsFor: func(_ context.Context, _ string, first bool) (SessionsStated, *Refusal) {
		firsts = append(firsts, first)
		return SessionsStated{IDs: []string{"%19", "GUID-2"}, Complete: true}, nil
	}}
	for _, body := range []map[string]any{
		{"type": "sessions.snapshot", "session": MachineReplySession, "request": "req-1"},
		{"type": "sessions.snapshot", "session": MachineReplySession, "request": "req-1", "orchestrator": true},
		{"type": "sessions.snapshot", "session": MachineReplySession, "request": "req-1", "initial": false},
		{"type": "sessions.snapshot.initial", "session": MachineReplySession, "request": "req-1"},
	} {
		answer := bridge.Handle(context.Background(), request(t, ClassCtl, body))
		if answer.Session != MachineReplySession || answer.Name != "read:req-1" || !answer.OK() {
			t.Fatalf("answered %+v", answer)
		}
		payload := answerOf(t, answer)
		got, _ := json.Marshal(payload["body"])
		if string(got) != `{"complete":true,"sessions":["%19","GUID-2"]}` {
			t.Fatalf("the answer said %s", got)
		}
	}
	if !reflect.DeepEqual(firsts, []bool{true, true, false, true}) {
		t.Fatalf("the publisher saw first attempts %v", firsts)
	}

	// A body this word does not have is malformed, and the publisher is not
	// asked to re-state anything for it.
	for _, body := range []map[string]any{
		{"type": "sessions.snapshot", "session": MachineReplySession, "request": "req-1", "orchestrator": false},
		{"type": "sessions.snapshot", "session": MachineReplySession, "request": "req-1", "extra": 1},
		{"type": "sessions.snapshot", "session": "%19", "request": "req-1"},
	} {
		answer := bridge.Handle(context.Background(), request(t, ClassCtl, body))
		if answer.Code != "malformed_read" {
			t.Fatalf("%v answered %q, wanted malformed_read", body, answer.Code)
		}
	}
	if len(firsts) != 4 {
		t.Fatalf("a malformed request asked the publisher (%d)", len(firsts))
	}

	// The publisher's refusal crosses as itself.
	busy := Bridge{MachineID: "mac-01", Sessions: func(context.Context) (SessionsStated, *Refusal) {
		return SessionsStated{}, &Refusal{Status: 429, Code: "cloud_read_busy", Message: "busy",
			Detail: map[string]any{"limit": 64, "retry_after": 5}}
	}}
	answer := busy.Handle(context.Background(), request(t, ClassCtl,
		map[string]any{"type": "sessions.snapshot", "session": MachineReplySession, "request": "req-2"}))
	if answer.Code != "cloud_read_busy" || answer.Status != 429 || answer.Name != "read:req-2" {
		t.Fatalf("answered %+v", answer)
	}

	if !AsksForSessions([]byte(`{"type":"sessions.snapshot","session":"__clawdline_machine__","request":"r"}`)) {
		t.Fatal("AsksForSessions missed the word")
	}
	if !AsksForSessions([]byte(`{"type":"sessions.snapshot.initial","session":"__clawdline_machine__","request":"r"}`)) {
		t.Fatal("AsksForSessions missed the initial word")
	}
	for _, other := range []string{`{"type":"send","text":"sessions.snapshot"}`, `not json sessions.snapshot`, `{"type":"info"}`} {
		if AsksForSessions([]byte(other)) {
			t.Fatalf("AsksForSessions took %s for the word", other)
		}
	}
}

// IsRead is what lets a transport answer a read beside its command lane, so a
// word that has an effect must never pass it, and neither may a body the
// bridge would refuse.
func TestIsReadIsTrueOnlyForAKnownEffectFreeWord(t *testing.T) {
	for _, c := range []struct {
		body string
		want bool
	}{
		{`{"type":"places","session":"__clawdline_machine__","request":"r1"}`, true},
		{`{"type":"transcript","session":"%19","limit":50}`, true},
		{`{"type":"send","session":"%19","text":"hi","images":[]}`, false},
		{`{"type":"push-subscribe"}`, false},
		{`{"type":"no-such-word"}`, false},
		{`{"session":"%19"}`, false},
		{`not json`, false},
		{``, false},
	} {
		if got := IsRead([]byte(c.body)); got != c.want {
			t.Errorf("IsRead(%s) = %v, want %v", c.body, got, c.want)
		}
	}
}

// TestTheTokenBillCrossesOnlyWithAnIdTheLocalRouteWouldTake. The three usage
// reads are the local routes' own question, so they take exactly the ids those
// routes take (`usageID` in internal/transport/http/usage.go) and refuse the
// rest here, before this machine is asked anything. A refusal the route itself
// gives — a Board item it has never seen — crosses with the route's own code,
// because the bill says "no such item" by that code and not by its sentence.
func TestTheTokenBillCrossesOnlyWithAnIdTheLocalRouteWouldTake(t *testing.T) {
	longest := strings.Repeat("a", 200)
	for _, word := range []string{"usage.session", "usage.task", "usage.item"} {
		for _, id := range []string{"w1", taskID, "rollout-2026.09.26_x", longest} {
			r := &router{}
			answer := Bridge{MachineID: "mac-01", Router: r}.Handle(context.Background(), request(t, ClassCtl,
				map[string]any{"type": word, "session": MachineReplySession, "request": "req", "id": id}))
			if answer.Status != 200 || len(r.seen) != 1 {
				t.Fatalf("%s refused %q with the write switch off: %+v", word, id, answer)
			}
			if got := r.last().Path; !strings.HasSuffix(got, "/"+id) || !strings.HasPrefix(got, "/v1/usage/") {
				t.Fatalf("%s asked %s for %q", word, got, id)
			}
		}
		for _, id := range []any{"", longest + "a", ".hidden", "-flag", "a..b", "a/b", "%19", "a b", "a\nb", 7, nil} {
			r := &router{}
			answer := open(r).Handle(context.Background(), request(t, ClassCtl,
				map[string]any{"type": word, "session": MachineReplySession, "request": "req", "id": id}))
			if answer.Code != "malformed_read" || len(r.seen) != 0 {
				t.Fatalf("%s took the id %#v: %+v, asked %v", word, id, answer, r.seen)
			}
			if answer.Name != "read:req" {
				t.Fatalf("%s refused %#v on %q, wanted the machine's read:req", word, id, answer.Name)
			}
		}
		for _, body := range []map[string]any{
			{"type": word, "session": pane, "request": "req", "id": "w1"},
			{"type": word, "session": MachineReplySession, "id": "w1"},
			{"type": word, "session": MachineReplySession, "request": "req", "id": "w1", "limit": 1},
		} {
			r := &router{}
			if answer := open(r).Handle(context.Background(), request(t, ClassCtl, body)); answer.Code != "malformed_read" || len(r.seen) != 0 {
				t.Fatalf("%s took %v: %+v", word, body, answer)
			}
		}
	}

	unknown := &router{status: 404, body: `{"error":"unknown_item","detail":"No Board item has this id."}`}
	answer := open(unknown).Handle(context.Background(), request(t, ClassCtl, map[string]any{
		"type": "usage.item", "session": MachineReplySession, "request": "req", "id": "w404"}))
	if answer.Status != 404 || answer.Code != "unknown_item" {
		t.Fatalf("answered %d/%q, wanted 404/unknown_item", answer.Status, answer.Code)
	}
	if failure := errorOf(t, answer); failure["message"] != "No Board item has this id." {
		t.Fatalf("the route's own sentence did not cross: %v", failure)
	}

	// `not_yet_read` and `transcript_missing` are not refusals: they are a
	// bill's own `reason`, inside a 200, and cross as the body they are.
	unread := &router{body: `{"conversation":"c1","reason":"not_yet_read","bill":{"categories":[]}}`}
	answer = open(unread).Handle(context.Background(), request(t, ClassCtl, map[string]any{
		"type": "usage.session", "session": MachineReplySession, "request": "req", "id": "c1"}))
	body, _ := answerOf(t, answer)["body"].(map[string]any)
	if answer.Status != 200 || body["reason"] != "not_yet_read" {
		t.Fatalf("the reason did not cross: %d %s", answer.Status, answer.Payload)
	}
}

// TestTheWorkSamplesCrossWithOnlyTheFieldsTheRouteReads: no field asks for
// the route's default range, a since and an until it could read cross as its
// query, and anything else is refused before this machine is asked.
func TestTheWorkSamplesCrossWithOnlyTheFieldsTheRouteReads(t *testing.T) {
	word := "usage.work-samples"
	r := &router{}
	answer := Bridge{MachineID: "mac-01", Router: r}.Handle(context.Background(), request(t, ClassCtl,
		map[string]any{"type": word, "session": MachineReplySession, "request": "req"}))
	if answer.Status != 200 || len(r.seen) != 1 || r.last().Path != "/v1/usage/work-samples" || len(r.last().Query) != 0 {
		t.Fatalf("no field: %+v, asked %+v", answer, r.seen)
	}
	r = &router{}
	answer = Bridge{MachineID: "mac-01", Router: r}.Handle(context.Background(), request(t, ClassCtl,
		map[string]any{"type": word, "session": MachineReplySession, "request": "req", "until": "1790000000"}))
	if answer.Status != 200 || len(r.seen) != 1 || r.last().Query["until"] != "1790000000" || r.last().Query["since"] != "" {
		t.Fatalf("until alone: %+v, asked %+v", answer, r.seen)
	}
	for _, body := range []map[string]any{
		{"type": word, "session": MachineReplySession, "request": "req", "until": "14d"},
		{"type": word, "session": MachineReplySession, "request": "req", "until": ""},
		{"type": word, "session": MachineReplySession, "request": "req", "until": 1790000000},
		{"type": word, "session": MachineReplySession, "request": "req", "since": "14w"},
		{"type": word, "session": MachineReplySession, "request": "req", "limit": 1},
		{"type": word, "session": pane, "request": "req"},
	} {
		r := &router{}
		if answer := open(r).Handle(context.Background(), request(t, ClassCtl, body)); answer.Code != "malformed_read" || len(r.seen) != 0 {
			t.Fatalf("took %v: %+v, asked %v", body, answer, r.seen)
		}
	}
}

// TestTheCompactionComparisonCrossesWithOnlyTheSinceTheRouteReads. The word
// is the local route's question: no `since` asks for its default range, a
// `since` the route could read crosses as its query, and anything else —
// another field, a since that is not a number and a unit, a session channel —
// is refused here before this machine is asked anything.
func TestTheCompactionComparisonCrossesWithOnlyTheSinceTheRouteReads(t *testing.T) {
	word := "usage.compare-compaction"
	r := &router{}
	answer := Bridge{MachineID: "mac-01", Router: r}.Handle(context.Background(), request(t, ClassCtl,
		map[string]any{"type": word, "session": MachineReplySession, "request": "req"}))
	if answer.Status != 200 || len(r.seen) != 1 || r.last().Path != "/v1/usage/compare-compaction" || len(r.last().Query) != 0 {
		t.Fatalf("no since: %+v, asked %+v", answer, r.seen)
	}
	for _, since := range []string{"14d", "36h", "1790000000"} {
		r := &router{}
		answer := Bridge{MachineID: "mac-01", Router: r}.Handle(context.Background(), request(t, ClassCtl,
			map[string]any{"type": word, "session": MachineReplySession, "request": "req", "since": since}))
		if answer.Status != 200 || len(r.seen) != 1 || r.last().Query["since"] != since {
			t.Fatalf("since %q: %+v, asked %+v", since, answer, r.seen)
		}
	}
	for _, body := range []map[string]any{
		{"type": word, "session": MachineReplySession, "request": "req", "since": "14w"},
		{"type": word, "session": MachineReplySession, "request": "req", "since": "14d&x=1"},
		{"type": word, "session": MachineReplySession, "request": "req", "since": ""},
		{"type": word, "session": MachineReplySession, "request": "req", "since": 14},
		{"type": word, "session": MachineReplySession, "request": "req", "limit": 1},
		{"type": word, "session": pane, "request": "req"},
		{"type": word, "session": MachineReplySession},
	} {
		r := &router{}
		if answer := open(r).Handle(context.Background(), request(t, ClassCtl, body)); answer.Code != "malformed_read" || len(r.seen) != 0 {
			t.Fatalf("took %v: %+v, asked %v", body, answer, r.seen)
		}
	}
	// The route's own refusal of a range it does not take crosses as itself.
	refused := &router{status: 400, body: `{"error":"bad_request","detail":"since: out of range."}`}
	answer = open(refused).Handle(context.Background(), request(t, ClassCtl, map[string]any{
		"type": word, "session": MachineReplySession, "request": "req", "since": "0d"}))
	if answer.Status != 400 || answer.Code != "bad_request" {
		t.Fatalf("answered %d/%q, wanted 400/bad_request", answer.Status, answer.Code)
	}
}

// TestAVerificationCrossesOnlyAsTheRouteWouldTakeIt. The verification words
// name one record by an id the route takes (letters, digits, dashes, at most
// 64) and refuse the rest here; a delete names its force every time, so a
// phone that forgot to say is refused rather than read as either; a criterion
// is an index the route could have. The five commands are commands: the write
// switch refuses them before their bodies are read.
func TestAVerificationCrossesOnlyAsTheRouteWouldTakeIt(t *testing.T) {
	for _, id := range []any{"", strings.Repeat("a", 65), "a/b", "a..b", "a b", "%2F", 7, nil} {
		r := &router{}
		answer := open(r).Handle(context.Background(), request(t, ClassCtl,
			map[string]any{"type": "verification.get", "session": MachineReplySession, "request": "req", "id": id}))
		if answer.Code != "malformed_read" || len(r.seen) != 0 {
			t.Fatalf("verification.get took the id %#v: %+v", id, answer)
		}
	}
	for _, body := range []map[string]any{
		{"type": "verification.delete", "session": MachineReplySession, "request": "req", "id": verificationFixture},
		{"type": "verification.delete", "session": MachineReplySession, "request": "req", "id": verificationFixture, "force": "yes"},
		{"type": "verification.delete", "session": MachineReplySession, "request": "req", "id": "a/b", "force": false},
		{"type": "verification.delete", "session": MachineReplySession, "id": verificationFixture, "force": false},
		{"type": "verification.criterion", "session": MachineReplySession, "request": "req", "id": verificationFixture,
			"index": 12, "verification": map[string]any{"state": "passed"}},
		{"type": "verification.criterion", "session": MachineReplySession, "request": "req", "id": verificationFixture,
			"index": -1, "verification": map[string]any{"state": "passed"}},
		{"type": "verification.note", "session": MachineReplySession, "request": "req", "id": verificationFixture,
			"verification": "looked"},
		{"type": "verification.note", "session": pane, "request": "req", "id": verificationFixture,
			"verification": map[string]any{"text": "looked"}},
		{"type": "verification.create", "session": MachineReplySession, "request": "req",
			"verification": map[string]any{"text": strings.Repeat("x", 70<<10)}},
	} {
		r := &router{}
		answer := open(r).Handle(context.Background(), request(t, ClassCtl, body))
		if answer.Status == 200 || len(r.seen) != 0 {
			t.Fatalf("took %v: %+v", body, answer)
		}
	}
	// Without force, the delete asks with no query at all.
	r := &router{}
	open(r).Handle(context.Background(), request(t, ClassCtl, map[string]any{"type": "verification.delete",
		"session": MachineReplySession, "request": "req", "id": verificationFixture, "force": false}))
	if len(r.seen) != 1 || len(r.last().Query) != 0 || r.last().Method != "DELETE" {
		t.Fatalf("asked %+v", r.seen)
	}
	if got := r.last().Header[actorHeader]; got != actorDevice {
		t.Fatalf("the delete is not a device's press: %q", got)
	}
	// The reads cross with the write switch off; the commands do not.
	closed := Bridge{MachineID: "mac-01", Router: &router{}}
	if answer := closed.Handle(context.Background(), request(t, ClassCtl, map[string]any{"type": "verification.list",
		"session": MachineReplySession, "request": "req"})); answer.Status != 200 {
		t.Fatalf("the list is a read: %+v", answer)
	}
	for _, word := range []string{"verification.create", "verification.note", "verification.criterion",
		"verification.close", "verification.delete"} {
		answer := closed.Handle(context.Background(), request(t, ClassCtl, map[string]any{"type": word,
			"session": MachineReplySession, "request": "req"}))
		if answer.Code != "cloud_commands_disabled" {
			t.Fatalf("%s crossed with the write switch off: %+v", word, answer)
		}
	}
}

// TestTheCapacityReadCrossesWithNothingButItsName. The capacity block on the
// Settings page is what every capacity push points at, and the phone that got
// the push reads it through here. The word is the local route's question and
// nothing more: no field beyond the envelope, and no session channel, reaches
// this machine; the route's own answer crosses as itself.
func TestTheCapacityReadCrossesWithNothingButItsName(t *testing.T) {
	r := &router{}
	answer := Bridge{MachineID: "mac-01", Router: r}.Handle(context.Background(), request(t, ClassCtl,
		map[string]any{"type": "capacity", "session": MachineReplySession, "request": "req"}))
	if answer.Status != 200 || len(r.seen) != 1 || r.last().Method != "GET" || r.last().Path != "/v1/capacity" || len(r.last().Query) != 0 {
		t.Fatalf("answered %+v, asked %+v", answer, r.seen)
	}
	for _, body := range []map[string]any{
		{"type": "capacity", "session": MachineReplySession, "request": "req", "all": true},
		{"type": "capacity", "session": MachineReplySession, "request": "req", "name": "artifacts.drops"},
		{"type": "capacity", "session": pane, "request": "req"},
		{"type": "capacity", "session": MachineReplySession},
	} {
		r := &router{}
		if answer := open(r).Handle(context.Background(), request(t, ClassCtl, body)); answer.Code != "malformed_read" || len(r.seen) != 0 {
			t.Fatalf("took %v: %+v, asked %v", body, answer, r.seen)
		}
	}
	refused := &router{status: 405, body: `{"error":{"code":"bad_request","message":"The capacity panel is read with GET."}}`}
	answer = open(refused).Handle(context.Background(), request(t, ClassCtl, map[string]any{
		"type": "capacity", "session": MachineReplySession, "request": "req"}))
	if answer.Status != 405 || answer.Code != "bad_request" {
		t.Fatalf("answered %d/%q, wanted 405/bad_request", answer.Status, answer.Code)
	}
}

// TestRestoringCarriesTheRequestAsItsKeyAndRefusesABadList: a restore is one
// resume per conversation, so a retried envelope must reach the route under
// the same key, and a list that is not ids never reaches it at all.
func TestRestoringCarriesTheRequestAsItsKeyAndRefusesABadList(t *testing.T) {
	r := &router{}
	open(r).Handle(context.Background(), request(t, ClassCtl, map[string]any{
		"type": "restore-sessions", "session": MachineReplySession, "request": "req-restore",
		"conversations": []any{"018f2f7a"}}))
	if key := r.last().Header["Idempotency-Key"]; key != "req-restore" {
		t.Fatalf("the key is %q, wanted the request id", key)
	}
	open(r).Handle(context.Background(), request(t, ClassCtl, map[string]any{
		"type": "dismiss-restorable", "session": MachineReplySession, "request": "req-dismiss",
		"conversations": []any{"018f2f7a"}}))
	if got := r.last(); got.Header["Idempotency-Key"] != "req-dismiss" || string(got.Body) != `{"conversations":["018f2f7a"]}` {
		t.Fatalf("the dismissal reached the route as %+v", got)
	}
	before := len(r.seen)
	for _, bad := range []any{[]any{}, []any{7}, "018f2f7a", []any{"a\nb"}} {
		answer := open(r).Handle(context.Background(), request(t, ClassCtl, map[string]any{
			"type": "restore-sessions", "session": MachineReplySession, "request": "req-bad", "conversations": bad}))
		if answer.Status == 200 {
			t.Fatalf("conversations %v was answered 200", bad)
		}
	}
	if len(r.seen) != before {
		t.Fatalf("a malformed list reached the route: %+v", r.seen[before:])
	}
}

// TestArchivingCarriesTheRequestAsItsKey: an archive is a close and a
// restore is one resume per conversation, so a retried envelope reaches the
// route under the same key; an archive without `force` is not a forced one,
// and a malformed field never reaches the route.
func TestArchivingCarriesTheRequestAsItsKey(t *testing.T) {
	r := &router{}
	open(r).Handle(context.Background(), request(t, ClassCtl, map[string]any{
		"type": "archive-session", "session": pane, "request": "req-archive"}))
	if got := r.last(); got.Header["Idempotency-Key"] != "req-archive" || string(got.Body) != `{"force":false}` {
		t.Fatalf("the archive reached the route as %+v", got)
	}
	open(r).Handle(context.Background(), request(t, ClassCtl, map[string]any{
		"type": "archive-session", "session": pane, "request": "req-versioned", "force": false,
		"expected_closeability_version": "cl1-current"}))
	if got := r.last(); got.Header["Idempotency-Key"] != "req-versioned" ||
		string(got.Body) != `{"expected_closeability_version":"cl1-current","force":false}` {
		t.Fatalf("the versioned archive reached the route as %+v", got)
	}
	open(r).Handle(context.Background(), request(t, ClassCtl, map[string]any{
		"type": "restore-archived", "session": MachineReplySession, "request": "req-unarchive",
		"conversations": []any{"018f2f7a"}}))
	if key := r.last().Header["Idempotency-Key"]; key != "req-unarchive" {
		t.Fatalf("the key is %q, wanted the request id", key)
	}
	before := len(r.seen)
	for _, bad := range []map[string]any{
		{"type": "archive-session", "session": pane, "request": "req-bad", "force": "yes"},
		{"type": "archive-session", "session": pane, "request": "req-bad", "conversations": []any{"x"}},
		{"type": "restore-archived", "session": MachineReplySession, "request": "req-bad", "conversations": []any{}},
		{"type": "restore-archived", "session": pane, "request": "req-bad", "conversations": []any{"x"}},
	} {
		if answer := open(r).Handle(context.Background(), request(t, ClassCtl, bad)); answer.Status == 200 {
			t.Fatalf("%v was answered 200", bad)
		}
	}
	if len(r.seen) != before {
		t.Fatalf("a malformed body reached the route: %+v", r.seen[before:])
	}
}

func TestTheMachineUsageReadCrossesWithNothingButItsName(t *testing.T) {
	r := &router{}
	answer := Bridge{MachineID: "mac-01", Router: r}.Handle(context.Background(), request(t, ClassCtl,
		map[string]any{"type": "machine-usage", "session": MachineReplySession, "request": "req"}))
	if answer.Status != 200 || len(r.seen) != 1 || r.last().Method != "GET" || r.last().Path != "/v1/machine/usage" || len(r.last().Query) != 0 {
		t.Fatalf("answered %+v, asked %+v", answer, r.seen)
	}
	for _, body := range []map[string]any{
		{"type": "machine-usage", "session": MachineReplySession, "request": "req", "pid": 1},
		{"type": "machine-usage", "session": pane, "request": "req"},
		{"type": "machine-usage", "session": MachineReplySession},
	} {
		r := &router{}
		if answer := open(r).Handle(context.Background(), request(t, ClassCtl, body)); answer.Code != "malformed_read" || len(r.seen) != 0 {
			t.Fatalf("took %v: %+v, asked %v", body, answer, r.seen)
		}
	}
	refused := &router{status: 501, body: `{"error":"machine_usage_unsupported","detail":"machine usage is read on Linux only"}`}
	answer = open(refused).Handle(context.Background(), request(t, ClassCtl, map[string]any{
		"type": "machine-usage", "session": MachineReplySession, "request": "req"}))
	if answer.Status != 501 || answer.Code != "machine_usage_unsupported" {
		t.Fatalf("answered %d/%q, wanted 501/machine_usage_unsupported", answer.Status, answer.Code)
	}
}

func TestTheUpdateReadCrossesWithNothingButItsName(t *testing.T) {
	r := &router{}
	answer := Bridge{MachineID: "mac-01", Router: r}.Handle(context.Background(), request(t, ClassCtl,
		map[string]any{"type": "update", "session": MachineReplySession, "request": "req"}))
	if answer.Status != 200 || len(r.seen) != 1 || r.last().Method != "GET" || r.last().Path != "/v1/update" || len(r.last().Query) != 0 {
		t.Fatalf("answered %+v, asked %+v", answer, r.seen)
	}
	for _, body := range []map[string]any{
		{"type": "update", "session": MachineReplySession, "request": "req", "apply": true},
		{"type": "update", "session": pane, "request": "req"},
		{"type": "update", "session": MachineReplySession},
	} {
		r := &router{}
		if answer := open(r).Handle(context.Background(), request(t, ClassCtl, body)); answer.Code != "malformed_read" || len(r.seen) != 0 {
			t.Fatalf("took %v: %+v, asked %v", body, answer, r.seen)
		}
	}
	// A daemon predating the route answers 404; the phone hears that, not a 200.
	missing := &router{status: 404, body: `{"error":"not_found","detail":"no route"}`}
	answer = open(missing).Handle(context.Background(), request(t, ClassCtl, map[string]any{
		"type": "update", "session": MachineReplySession, "request": "req"}))
	if answer.Status != 404 {
		t.Fatalf("answered %d/%q, wanted 404", answer.Status, answer.Code)
	}
}

// The Settings page's 「立即更新」 over Cloud is a command: it needs the
// machine's command switch, carries nothing but the request, and reaches the
// apply route as a paired device rather than as this machine.
func TestUpdateApplyCrossesAsACommandWithNothingButItsName(t *testing.T) {
	press := map[string]any{"type": "update-apply", "session": MachineReplySession, "request": "req-apply"}
	r := &router{}
	refused := Bridge{MachineID: "mac-01", Router: r}.Handle(context.Background(), request(t, ClassCtl, press))
	if refused.Code != "cloud_commands_disabled" || len(r.seen) != 0 {
		t.Fatalf("with commands off answered %+v, asked %v", refused, r.seen)
	}

	r = &router{status: 202, body: `{"state":"update_available","running":{"stamp":""},"latest":{"stamp":""},"source_url":"x","apply":{"state":"downloading"}}`}
	answer := open(r).Handle(context.Background(), request(t, ClassCtl, press))
	if answer.Status != 202 || len(r.seen) != 1 {
		t.Fatalf("answered %+v, asked %+v", answer, r.seen)
	}
	got := r.last()
	if got.Method != "POST" || got.Path != "/v1/update/apply" || string(got.Body) != "{}" ||
		got.Header[actorHeader] != actorDevice || len(got.Query) != 0 {
		t.Fatalf("the press crossed as %+v body=%s", got, got.Body)
	}

	for _, body := range []map[string]any{
		{"type": "update-apply", "session": MachineReplySession, "request": "req", "version": "v9.9.9"},
		{"type": "update-apply", "session": MachineReplySession, "request": "req", "force": true},
		{"type": "update-apply", "session": pane, "request": "req"},
		{"type": "update-apply", "session": MachineReplySession},
	} {
		r := &router{}
		if bad := open(r).Handle(context.Background(), request(t, ClassCtl, body)); bad.Code != "malformed_command" || len(r.seen) != 0 {
			t.Fatalf("took %v: %+v, asked %v", body, bad, r.seen)
		}
	}
}

func TestDefaultModelsCrossOnlyTheirNarrowSettingsRoute(t *testing.T) {
	r := &router{}
	answer := Bridge{MachineID: "mac-01", Router: r}.Handle(context.Background(), request(t, ClassCtl,
		map[string]any{"type": "default-models", "session": MachineReplySession, "request": "req-read"}))
	if answer.Status != 200 || len(r.seen) != 1 || r.last().Method != "GET" || r.last().Path != "/v1/settings/default-models" {
		t.Fatalf("read answered %+v, asked %+v", answer, r.seen)
	}

	r = &router{}
	answer = open(r).Handle(context.Background(), request(t, ClassCtl, map[string]any{
		"type": "default-models-update", "session": MachineReplySession, "request": "req-write",
		"changes": map[string]any{"claude_default_model": "claude-sonnet-4-5"},
	}))
	if answer.Status != 200 || len(r.seen) != 1 {
		t.Fatalf("write answered %+v, asked %+v", answer, r.seen)
	}
	got := r.last()
	if got.Method != "POST" || got.Path != "/v1/settings/default-models" ||
		string(got.Body) != `{"claude_default_model":"claude-sonnet-4-5"}` ||
		got.Header[actorHeader] != actorDevice || got.Header["Idempotency-Key"] != "req-write" {
		t.Fatalf("write crossed too broadly: %+v body=%s", got, got.Body)
	}

	r = &router{}
	answer = open(r).Handle(context.Background(), request(t, ClassCtl, map[string]any{
		"type": "default-models-update", "session": MachineReplySession, "request": "req-effort",
		"changes": map[string]any{"codex_default_effort": "xhigh"},
	}))
	if answer.Status != 200 || len(r.seen) != 1 || string(r.last().Body) != `{"codex_default_effort":"xhigh"}` {
		t.Fatalf("effort did not cross the narrow route: %+v, asked %+v", answer, r.seen)
	}

	for _, changes := range []any{
		map[string]any{"remote": true},
		map[string]any{"codex_default_model": 6},
		map[string]any{"codex_default_model": "gpt-6", "claude_default_model": "opus", "remote": nil},
		[]any{"gpt-6"},
	} {
		r := &router{}
		bad := open(r).Handle(context.Background(), request(t, ClassCtl, map[string]any{
			"type": "default-models-update", "session": MachineReplySession, "request": "req-bad", "changes": changes,
		}))
		if bad.Code != "malformed_command" || len(r.seen) != 0 {
			t.Fatalf("took changes %#v: %+v, asked %+v", changes, bad, r.seen)
		}
	}
}

func TestWorkGateSettingsCrossOnlyTheirNarrowSettingsRoute(t *testing.T) {
	r := &router{}
	answer := Bridge{MachineID: "mac-01", Router: r}.Handle(context.Background(), request(t, ClassCtl,
		map[string]any{"type": "work-gate-settings", "session": MachineReplySession, "request": "req-read"}))
	if answer.Status != 200 || len(r.seen) != 1 || r.last().Method != "GET" || r.last().Path != "/v1/settings/work-gates" {
		t.Fatalf("read answered %+v, asked %+v", answer, r.seen)
	}

	r = &router{}
	answer = open(r).Handle(context.Background(), request(t, ClassCtl, map[string]any{
		"type": "work-gate-settings-update", "session": MachineReplySession, "request": "req-write",
		"changes": map[string]any{"planning_gate": false, "verify_gate": true},
	}))
	if answer.Status != 200 || len(r.seen) != 1 {
		t.Fatalf("write answered %+v, asked %+v", answer, r.seen)
	}
	got := r.last()
	if got.Method != "POST" || got.Path != "/v1/settings/work-gates" ||
		string(got.Body) != `{"planning_gate":false,"verify_gate":true}` ||
		got.Header[actorHeader] != actorDevice || got.Header["Idempotency-Key"] != "req-write" {
		t.Fatalf("write crossed too broadly: %+v body=%s", got, got.Body)
	}

	for _, changes := range []any{
		map[string]any{"remote": true},
		map[string]any{"planning_gate": "on"},
		map[string]any{"planning_gate": true, "verify_gate": false, "remote": nil},
		[]any{true},
	} {
		r := &router{}
		bad := open(r).Handle(context.Background(), request(t, ClassCtl, map[string]any{
			"type": "work-gate-settings-update", "session": MachineReplySession, "request": "req-bad", "changes": changes,
		}))
		if bad.Code != "malformed_command" || len(r.seen) != 0 {
			t.Fatalf("took changes %#v: %+v, asked %+v", changes, bad, r.seen)
		}
	}
}

func TestBoardSettingsCommandCrossesAsThePairedDevice(t *testing.T) {
	command := map[string]any{
		"operation": "set_ai_consent", "provider": "codex", "enabled": true,
		"policy": "board-reading-v1", "expectedRevision": 4, "requestId": "board-setting-1",
	}
	r := &router{}
	answer := open(r).Handle(context.Background(), request(t, ClassCtl, map[string]any{
		"type": "board-command", "session": MachineReplySession, "request": "board-setting-1",
		"command": command,
	}))
	if answer.Status != 200 || len(r.seen) != 1 {
		t.Fatalf("write answered %+v, asked %+v", answer, r.seen)
	}
	got := r.last()
	if got.Method != "POST" || got.Path != "/v1/board" ||
		string(got.Body) != `{"enabled":true,"expectedRevision":4,"operation":"set_ai_consent","policy":"board-reading-v1","provider":"codex","requestId":"board-setting-1"}` ||
		got.Header[actorHeader] != actorDevice || got.Header["Idempotency-Key"] != "board-setting-1" {
		t.Fatalf("write crossed incorrectly: %+v body=%s", got, got.Body)
	}

	for _, receipt := range []string{"", "another-request"} {
		r := &router{}
		answer := open(r).Handle(context.Background(), request(t, ClassCtl, map[string]any{
			"type": "board-command", "session": MachineReplySession, "request": receipt,
			"command": command,
		}))
		if answer.Code != "malformed_command" || len(r.seen) != 0 {
			t.Fatalf("request %q answered %+v, asked %+v", receipt, answer, r.seen)
		}
	}
}

func TestThePersonasReadCrossesWithNothingButItsName(t *testing.T) {
	r := &router{}
	answer := Bridge{MachineID: "mac-01", Router: r}.Handle(context.Background(), request(t, ClassCtl,
		map[string]any{"type": "personas", "session": MachineReplySession, "request": "req"}))
	if answer.Status != 200 || len(r.seen) != 1 || r.last().Method != "GET" || r.last().Path != "/v1/personas" || len(r.last().Query) != 0 {
		t.Fatalf("answered %+v, asked %+v", answer, r.seen)
	}
	for _, body := range []map[string]any{
		{"type": "personas", "session": MachineReplySession, "request": "req", "kind": "epic"},
		{"type": "personas", "session": pane, "request": "req"},
		{"type": "personas", "session": MachineReplySession},
	} {
		r := &router{}
		if answer := open(r).Handle(context.Background(), request(t, ClassCtl, body)); answer.Code != "malformed_read" || len(r.seen) != 0 {
			t.Fatalf("took %v: %+v, asked %v", body, answer, r.seen)
		}
	}
}

// TestAStartOrAResumeMayNameAPersona: `persona` is an optional seventh key on
// both words. With it the route ends `/as/<persona>` after a spelled-out
// assistant, because the local route reads `as` only there; without it the
// route is the one a page from before personas always reached.
func TestAStartOrAResumeMayNameAPersona(t *testing.T) {
	const place = "/Users/sean/code/clawdline-go"
	const escaped = "%2FUsers%2Fsean%2Fcode%2Fclawdline-go"
	for _, c := range []struct {
		name string
		body map[string]any
		path string
	}{
		{"start with none", map[string]any{"type": "start", "session": MachineReplySession, "request": "req",
			"place": place, "assistant": "", "model": ""}, "/v1/places/" + escaped + "/start"},
		{"start in the machine workspace", map[string]any{"type": "start", "session": MachineReplySession, "request": "req",
			"place": "@machine", "assistant": "codex", "model": ""}, "/v1/places/%40machine/start/codex"},
		{"start with a model", map[string]any{"type": "start", "session": MachineReplySession, "request": "req",
			"place": place, "assistant": "claude", "model": "opus", "persona": "architect"},
			"/v1/places/" + escaped + "/start/claude/opus/as/architect"},
		{"start with the default assistant", map[string]any{"type": "start", "session": MachineReplySession, "request": "req",
			"place": place, "assistant": "", "model": "", "persona": "code-reviewer"},
			"/v1/places/" + escaped + "/start/claude/as/code-reviewer"},
		{"start in codex", map[string]any{"type": "start", "session": MachineReplySession, "request": "req",
			"place": place, "assistant": "codex", "model": "", "persona": "security"},
			"/v1/places/" + escaped + "/start/codex/as/security"},
		{"resume with none", map[string]any{"type": "resume", "session": MachineReplySession, "request": "req",
			"place": place, "past": "018f2f7a", "assistant": ""}, "/v1/places/" + escaped + "/resume/018f2f7a"},
		{"resume with an assistant", map[string]any{"type": "resume", "session": MachineReplySession, "request": "req",
			"place": place, "past": "018f2f7a", "assistant": "codex", "persona": "backend"},
			"/v1/places/" + escaped + "/resume/codex/018f2f7a/as/backend"},
		{"resume with the default assistant", map[string]any{"type": "resume", "session": MachineReplySession, "request": "req",
			"place": place, "past": "018f2f7a", "assistant": "", "persona": "frontend"},
			"/v1/places/" + escaped + "/resume/claude/018f2f7a/as/frontend"},
		// Whether the catalog has the name is the route's answer, not this
		// bridge's: it travels escaped, and the route says unknown_persona.
		{"a name the catalog may not have", map[string]any{"type": "start", "session": MachineReplySession, "request": "req",
			"place": place, "assistant": "claude", "model": "", "persona": "a/b"},
			"/v1/places/" + escaped + "/start/claude/as/a%2Fb"},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := &router{}
			answer := open(r).Handle(context.Background(), request(t, ClassCtl, c.body))
			if answer.Status != 200 || len(r.seen) != 1 {
				t.Fatalf("answered %+v, asked %+v", answer, r.seen)
			}
			got := r.last()
			if got.Method != "POST" || got.Path != c.path || got.Header["Idempotency-Key"] != "req" {
				t.Fatalf("routed %s %s key %q, wanted POST %s key req", got.Method, got.Path, got.Header["Idempotency-Key"], c.path)
			}
		})
	}
	for _, persona := range []any{"", 7, nil, "a\nb", strings.Repeat("x", 257)} {
		for _, word := range []string{"start", "resume"} {
			body := map[string]any{"type": word, "session": MachineReplySession, "request": "req",
				"place": place, "assistant": "claude", "persona": persona}
			if word == "start" {
				body["model"] = ""
			} else {
				body["past"] = "018f2f7a"
			}
			r := &router{}
			if answer := open(r).Handle(context.Background(), request(t, ClassCtl, body)); answer.Status == 200 || len(r.seen) != 0 {
				t.Fatalf("%s with persona %q was taken: %+v, asked %v", word, persona, answer, r.seen)
			}
		}
	}
	refused := &router{status: 400, body: `{"error":{"code":"unknown_persona","message":"No persona named that."}}`}
	answer := open(refused).Handle(context.Background(), request(t, ClassCtl, map[string]any{
		"type": "start", "session": MachineReplySession, "request": "req",
		"place": place, "assistant": "claude", "model": "", "persona": "nobody"}))
	if answer.Status != 400 || answer.Code != "unknown_persona" {
		t.Fatalf("answered %d/%q, wanted 400/unknown_persona", answer.Status, answer.Code)
	}
}

func TestLocalRefusalDetailKeyCrossesCloudOnlyWhenTheWriterProvidedIt(t *testing.T) {
	const detail = "That task already finished."
	const key = "http.ee71993f567fd4fe"
	for _, tc := range []struct {
		name   string
		body   string
		want   string
		origin string
	}{
		{"flat writer", `{"error":"task_finished","detail":"That task already finished.","detail_key":"http.ee71993f567fd4fe"}`, key, key},
		{"nested writer", `{"error":{"code":"task_finished","message":"That task already finished.","detail_key":"http.ee71993f567fd4fe"}}`, key, key},
		{"raw same sentence", `{"error":"task_finished","detail":"That task already finished."}`, "", ""},
		{"external copied key and sentence", `{"error":"task_finished","detail":"That task already finished.","detail_key":"http.ee71993f567fd4fe"}`, "", ""},
		{"wrong explicit key", `{"error":"task_finished","detail":"That task already finished.","detail_key":"http.0000000000000000"}`, "", key},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &router{status: 409, body: tc.body, fixedDetailKey: tc.origin}
			answer := open(r).Handle(context.Background(), request(t, ClassCtl, map[string]any{
				"type": "screen", "session": pane}))
			failure := errorOf(t, answer)
			if answer.Status != 409 || failure["code"] != "task_finished" || failure["message"] != detail ||
				failure["layer"] != layerRoute || failure["seq"] != float64(411) {
				t.Fatalf("Cloud refusal changed: %#v", failure)
			}
			if got, _ := failure["detail_key"].(string); got != tc.want {
				t.Fatalf("detail_key = %q, want %q: %#v", got, tc.want, failure)
			}
		})
	}
}

func TestBridgeFixedClockRefusalCarriesKeyButDynamicCopyDoesNot(t *testing.T) {
	r := &router{}
	b := open(r)
	b.Authority = func(context.Context, string, bool) Authority {
		return Authority{RosterReadable: true, RosterAllowsSender: true, WriteGateAllows: true}
	}
	answer := b.Handle(context.Background(), request(t, ClassCtl, map[string]any{
		"type": "focus", "session": pane, "request": "req-clock",
	}))
	failure := errorOf(t, answer)
	if failure["code"] != "command_clock_uncertain" || failure["message"] !=
		"This machine is still confirming the time; try again shortly." ||
		failure["detail_key"] != "http.72e768b5b868dcc2" {
		t.Fatalf("Bridge fixed copy lost its verified key: %#v", failure)
	}
	if len(r.seen) != 0 {
		t.Fatalf("preflight refusal still reached a route: %#v", r.seen)
	}
	raw := bridgeRefusalBase(Refusal{Code: "dynamic", Message: "This machine is still confirming the time; try again shortly."})
	if _, ok := raw["detail_key"]; ok {
		t.Fatalf("unmarked matching prose gained a key: %#v", raw)
	}
}
