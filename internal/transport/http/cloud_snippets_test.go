package http

import (
	"net/http"
	"testing"

	"github.com/sainteye/clawdline-go/internal/app/cloudops"
)

// 常用句 over Clawdline Cloud, end to end and in one process: the plaintext a
// hosted page seals, decoded by the bridge, carried by the in-process router
// with this daemon's own credentials on it — and therefore through the gate,
// the capability checks, the receipt table and the same store a paired browser
// on this machine's own network writes to.
//
// Before this the whole sheet was one refusal. This Mac's catalog listed the
// word `snippets` with a `decode` and no `route`, so `Implemented()` left it
// out, the bridge answered `unknown_command`, and the console's carry table
// said so in its own words: "Snippets are not carried over Clawdline Cloud
// yet: open them on the Mac." Every sentence in that chain was true and the
// thing the person wanted was simply not there.
//
// **Nothing anybody wrote is in this file.** Every snippet below is invented
// for the test, which is the whole of what a snippet fixture in a public
// repository may be: what is asserted is the structure and the count.

// aSnippet is one write's fields, in the shape `view/snippets-data.js` builds
// and `internal/domain/snippet` reads.
func aSnippet(title, body string) map[string]any {
	return map[string]any{"title": title, "body": body, "scope": "global"}
}

// TestACloudViewerReadsAndWritesTheSnippetsOnThisMac walks all five paths in
// the order a person walks them: open the sheet on nothing, make one, see it,
// change it, move it, take it away.
func TestACloudViewerReadsAndWritesTheSnippetsOnThisMac(t *testing.T) {
	s := cloudStandIn(t)

	// An empty machine answers, and its answer is an empty list rather than a
	// refusal. The sheet draws two different things for those, so the
	// difference has to survive the trip.
	answer, payload := s.ask(t, 1, map[string]any{"type": "snippets",
		"session": cloudops.MachineReplySession, "request": "req-list-0"})
	if !answer.OK() {
		t.Fatalf("the list was refused: %d/%q %s", answer.Status, answer.Code, answer.Payload)
	}
	if rows, ok := bodyOf(t, payload)["snippets"].([]any); !ok || len(rows) != 0 {
		t.Fatalf("a machine with no snippets answered %v", payload)
	}

	made, payload := s.ask(t, 2, map[string]any{"type": "snippet-create",
		"session": cloudops.MachineReplySession, "request": "req-new",
		"snippet": aSnippet("stand the branch up", "git switch -c review")})
	if !made.OK() {
		t.Fatalf("the viewer could not make a snippet: %d/%q %s", made.Status, made.Code, made.Payload)
	}
	body := bodyOf(t, payload)
	id, _ := body["id"].(string)
	if id == "" || body["scope"] != "global" {
		t.Fatalf("the answer is not one snippet: %v", body)
	}

	// **The same key again is the same answer, not a second snippet.** The
	// viewer's own request id is what becomes the Mac's `Idempotency-Key`
	// (`cloudops.route`), and the outcome is filed under it in the store's
	// receipt table — so a phone that resent after a dropped connection is
	// answered rather than obeyed twice.
	again, payloadAgain := s.ask(t, 3, map[string]any{"type": "snippet-create",
		"session": cloudops.MachineReplySession, "request": "req-new",
		"snippet": aSnippet("stand the branch up", "git switch -c review")})
	if !again.OK() {
		t.Fatalf("the retry was refused: %d/%q", again.Status, again.Code)
	}
	if bodyOf(t, payloadAgain)["id"] != id {
		t.Fatalf("the retry made a second snippet: %v then %v", id, bodyOf(t, payloadAgain)["id"])
	}
	if got := s.snippetCount(t, 4); got != 1 {
		t.Fatalf("this machine holds %d snippets after one make and one retry, wanted 1", got)
	}

	// Saving it. The Mac is what decides what a snippet may be, so the change
	// comes back as the stored record and not as what was sent.
	saved, payload := s.ask(t, 5, map[string]any{"type": "snippet-update",
		"session": cloudops.MachineReplySession, "request": "req-save", "id": id,
		"snippet": map[string]any{"title": "stand the review branch up"}})
	if !saved.OK() {
		t.Fatalf("the viewer could not save it: %d/%q %s", saved.Status, saved.Code, saved.Payload)
	}
	if got := bodyOf(t, payload)["title"]; got != "stand the review branch up" {
		t.Fatalf("the saved snippet reads %v", got)
	}

	// A second one, so that an order has something to be an order of.
	second, payload := s.ask(t, 6, map[string]any{"type": "snippet-create",
		"session": cloudops.MachineReplySession, "request": "req-new-2",
		"snippet": aSnippet("read the run", "tail -n 200 the log")})
	if !second.OK() {
		t.Fatalf("the second snippet was refused: %d/%q %s", second.Status, second.Code, second.Payload)
	}
	other, _ := bodyOf(t, payload)["id"].(string)

	ordered, payload := s.ask(t, 7, map[string]any{"type": "snippet-order",
		"session": cloudops.MachineReplySession, "request": "req-order",
		"ordering": map[string]any{"scope": "global", "order": []any{other, id}}})
	if !ordered.OK() {
		t.Fatalf("the viewer could not reorder the group: %d/%q %s",
			ordered.Status, ordered.Code, ordered.Payload)
	}
	rows, ok := bodyOf(t, payload)["snippets"].([]any)
	if !ok || len(rows) != 2 {
		t.Fatalf("the order came back as %v", payload)
	}
	first, _ := rows[0].(map[string]any)
	if first["id"] != other {
		t.Fatalf("the group was not reordered: %v", rows)
	}

	// And taking one away.
	gone, payload := s.ask(t, 8, map[string]any{"type": "snippet-delete",
		"session": cloudops.MachineReplySession, "request": "req-gone", "id": id})
	if !gone.OK() {
		t.Fatalf("the viewer could not remove it: %d/%q %s", gone.Status, gone.Code, gone.Payload)
	}
	if bodyOf(t, payload)["deleted"] != id {
		t.Fatalf("something else was removed: %v", payload)
	}
	if got := s.snippetCount(t, 9); got != 1 {
		t.Fatalf("this machine holds %d snippets after one removal, wanted 1", got)
	}
}

// TestACloudSnippetWriteIsRefusedByTheRoutesOwnWords is the other half of a
// write being carried: a refusal has to reach the phone as the sentence the
// sheet already branches on, rather than as a shrug.
func TestACloudSnippetWriteIsRefusedByTheRoutesOwnWords(t *testing.T) {
	s := cloudStandIn(t)

	// A global snippet may not carry a project path at all — not even an
	// empty one — and the route says which rule was broken.
	answer, payload := s.ask(t, 1, map[string]any{"type": "snippet-create",
		"session": cloudops.MachineReplySession, "request": "req-bad",
		"snippet": map[string]any{"title": "a title", "body": "a body",
			"scope": "global", "project": ""}})
	if answer.Status != http.StatusBadRequest || answer.Code != "snippet_scope_mismatch" {
		t.Fatalf("answered %d/%q, wanted 400/snippet_scope_mismatch", answer.Status, answer.Code)
	}
	if failure, _ := payload["error"].(map[string]any); failure["layer"] != "mac_route" {
		t.Fatalf("the refusal was not the route's: %v", failure)
	}

	// And a snippet this machine does not have is the route's own not_found,
	// which is not the same answer as a word this machine does not know.
	answer, _ = s.ask(t, 2, map[string]any{"type": "snippet-delete",
		"session": cloudops.MachineReplySession, "request": "req-nothing",
		"id": "3b000000-0000-4000-8000-00000000dead"})
	if answer.Status != http.StatusNotFound || answer.Code != "snippet_not_found" {
		t.Fatalf("answered %d/%q, wanted 404/snippet_not_found", answer.Status, answer.Code)
	}
}

// snippetCount is how many snippets this machine holds, read back over the
// same wire rather than out of the store — the count is only worth asserting
// where a phone would see it.
func (s *standIn) snippetCount(t *testing.T, seq uint64) int {
	t.Helper()
	answer, payload := s.ask(t, seq, map[string]any{"type": "snippets",
		"session": cloudops.MachineReplySession, "request": "req-count"})
	if !answer.OK() {
		t.Fatalf("the list was refused: %d/%q %s", answer.Status, answer.Code, answer.Payload)
	}
	rows, ok := bodyOf(t, payload)["snippets"].([]any)
	if !ok {
		t.Fatalf("the list carries no rows: %v", payload)
	}
	return len(rows)
}
