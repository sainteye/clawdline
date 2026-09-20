package cloudops

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sainteye/clawdline-go/internal/domain/capacity"
)

// A question's fingerprint, as session.MenuFingerprint writes one.
const fingerprint = "8eca80fffc9359d5f0fca31f3e36b741bd50218b658fc086f05931747bd4c5ce"

// F1: a menu answer names the question it was chosen for, and that name
// reaches the route that types the digit — which types nothing at any other
// question (app.Actions.Key).
func TestACloudAnswerCarriesTheQuestionItWasChosenFor(t *testing.T) {
	for _, word := range []string{"answer", "key"} {
		r := &router{}
		answer := open(r).Handle(context.Background(), request(t, ClassCtl, map[string]any{
			"type": word, "session": pane, "request": "req-1", word: "1", "expect": fingerprint}))
		if !answer.OK() || len(r.seen) != 1 {
			t.Fatalf("%s: answered %d/%q, asked %d", word, answer.Status, answer.Code, len(r.seen))
		}
		var body map[string]any
		if err := json.Unmarshal(r.last().Body, &body); err != nil {
			t.Fatal(err)
		}
		if r.last().Path != "/v1/sessions/%2519/key" || body["key"] != "1" || body["expect"] != fingerprint || len(body) != 2 {
			t.Fatalf("%s: routed %s %s", word, r.last().Path, r.last().Body)
		}
	}
}

// An answer that names no question cannot be checked against the screen, and
// across Clawdline Cloud — where the reply is slow, can be lost, and a second
// device can answer first — an unchecked digit is how a permission prompt gets
// approved for a command nobody read. It is refused, with nothing asked of
// this machine, in a code of its own on the channel the request named.
func TestACloudAnswerThatNamesNoQuestionIsRefused(t *testing.T) {
	r := &router{}
	answer := open(r).Handle(context.Background(), request(t, ClassCtl, map[string]any{
		"type": "answer", "session": pane, "request": "req-1", "answer": "1"}))
	if answer.Code != "menu_unverified" || answer.Status != 428 {
		t.Fatalf("answered %d/%q, wanted 428/menu_unverified", answer.Status, answer.Code)
	}
	if !answer.Published() || answer.Name != "action:req-1" {
		t.Fatalf("the refusal reaches nobody: %+v", answer)
	}
	if failure := errorOf(t, answer); failure["layer"] != layerPreflight {
		t.Fatalf("layer %v", failure["layer"])
	}
	// The oldest page's answer names neither a question nor a channel.
	older := open(r).Handle(context.Background(), request(t, ClassCtl, map[string]any{
		"type": "key", "session": pane, "key": "2"}))
	if older.Code != "menu_unverified" {
		t.Fatalf("an older page's answer: %d/%q", older.Status, older.Code)
	}
	if len(r.seen) != 0 {
		t.Fatalf("an unchecked answer still asked this machine: %+v", r.seen)
	}
	// That shape has no channel, so the refusal reaches nobody — but it names
	// the session it was about, which is the only thing the log can say about
	// a press somebody is still waiting on.
	if older.Published() {
		t.Fatalf("an answer with no request found a channel: %+v", older)
	}
	if older.Subject != pane {
		t.Fatalf("a silent refusal does not say what it was about: %q", older.Subject)
	}
}

func TestAnExpectationThatIsNotAFingerprintIsMalformed(t *testing.T) {
	r := &router{}
	for _, expect := range []any{"zz", 7, "", fingerprint + "0"} {
		answer := open(r).Handle(context.Background(), request(t, ClassCtl, map[string]any{
			"type": "answer", "session": pane, "request": "req-1", "answer": "1", "expect": expect}))
		if answer.Code != "malformed_command" {
			t.Fatalf("expect %v: %d/%q", expect, answer.Status, answer.Code)
		}
	}
	if len(r.seen) != 0 {
		t.Fatalf("asked %+v", r.seen)
	}
}

// F6, the Mac's half: a route's refusal crosses with its code, its sentence
// and the fields its code may show — §11.6 — and nothing else. A blocked
// close's reasons cross as what blocks it, without the mover's record, whose
// title is somebody's task name; a field the route put beside the code for a
// local reader does not ride along to every paired phone.
func TestARouteRefusalCarriesOnlyWhatItsCodeMayShow(t *testing.T) {
	blocked := &router{status: 409, body: `{"error":"close_blocked","detail":"still owed: landing",` +
		`"route":"/v1/sessions/%2519/close","cwd":"/Users/someone/secret",` +
		`"reasons":[{"kind":"obligation","code":"landing","subject_id":"t1","subject_kind":"task",` +
		`"mover":{"id":"t1","kind":"task","title":"Fix the thing for ACME Corp"}},"not an object"]}`}
	answer := open(blocked).Handle(context.Background(), request(t, ClassCtl, map[string]any{
		"type": "end", "session": pane, "request": "req-end", "accept_loss": false,
		"expected_closeability_version": ""}))
	failure := errorOf(t, answer)
	for _, leaked := range []string{"route", "cwd"} {
		if _, ok := failure[leaked]; ok {
			t.Fatalf("%s crossed: %v", leaked, failure)
		}
	}
	reasons, ok := failure["reasons"].([]any)
	if !ok || len(reasons) != 1 {
		t.Fatalf("reasons %v", failure["reasons"])
	}
	reason := reasons[0].(map[string]any)
	want := map[string]any{"kind": "obligation", "code": "landing", "subject_id": "t1", "subject_kind": "task"}
	if len(reason) != len(want) {
		t.Fatalf("a reason carried %v", reason)
	}
	for k, v := range want {
		if reason[k] != v {
			t.Fatalf("a reason carried %v", reason)
		}
	}
	// A field a code may show still crosses, at the top level where the
	// hosted console reads it too.
	closed := &router{status: 409, body: `{"error":"terminal_closed","detail":"gone","app":"iTerm2","pid":4242}`}
	answer = open(closed).Handle(context.Background(), request(t, ClassCtl, map[string]any{
		"type": "focus", "session": pane, "request": "req-focus"}))
	failure = errorOf(t, answer)
	if failure["app"] != "iTerm2" {
		t.Fatalf("app did not cross: %v", failure)
	}
	if _, ok := failure["pid"]; ok {
		t.Fatalf("pid crossed: %v", failure)
	}
}

// The picture door is the smaller of the two ceilings, and the smaller one is
// this machine's own.
//
// Before 2026-09-21 it was the relay's 16 MiB alone, so every picture between
// what a channel holds and what an envelope allows was admitted here and
// refused at the spool — where, at the time, a refusal was a log line. A door
// that admits what the next room refuses is not a door.
func TestThePictureDoorIsTheSmallerOfTheTwoCeilings(t *testing.T) {
	limit := imageMaxEncodedBytes()
	channel := int(capacity.Default(capacity.CloudSpoolChannelBytes))
	// What goes on the wire is base64 of the PNG plus the JSON around it, and
	// that is what the spool charges the channel for.
	payload := (limit+2)/3*4 + imageAnswerOverhead
	if payload > channel {
		t.Fatalf("a picture at the door is %d bytes on the wire; one channel holds %d", payload, channel)
	}
	// And it is not needlessly smaller than that: the largest picture allowed
	// uses the channel it is going to travel on.
	if payload < channel-8*1024 {
		t.Fatalf("a picture at the door is %d bytes on the wire and the channel holds %d; the door is smaller than it needs to be",
			payload, channel)
	}
}
