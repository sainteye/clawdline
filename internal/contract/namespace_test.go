package contract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// One name for a session (docs/broker-design.md #36, docs/design-guidelines.md
// DG-5).
//
// The Swift app had two: its broker routes took `root.session_id` meaning a
// conversation id, and its coordinator, wait and landing-slot routes took
// `session_id` meaning a terminal id — the same key, opposite values, which is
// how a landing slot came to be named by one and looked up by the other and
// never delivered once (O2). On this daemon a session is its conversation id;
// a terminal is a place.
//
// Nothing about a JSON key says which one it holds, so this guard makes every
// author say: each property in api/v1 whose name mentions a session is in the
// table below, as a conversation id or as one of the known exceptions — each
// with where it comes from and what retires it. A new property is red until it
// is classified; a property that went away is red until its line goes too. The
// exceptions are the residue the W5 report lists; they are the ones this
// guard lets stand, by name, rather than the ones nobody looked at.

var sessionNamed = regexp.MustCompile(`(?i)session`)

// Not identities: collections, counts, and states.
var notAnIdentity = regexp.MustCompile(`(?i)^sessions$|count$|_state$|_generation$`)

const (
	conversation = "conversation"
	object       = "object" // an object whose own fields are classified here
)

var sessionFields = map[string]string{
	// The coordination plane (W5): conversation ids only, and a terminal-shaped
	// value is refused as 409 session_id_is_terminal.
	"coordination#CoordinatorSession.session_id":         conversation,
	"coordination#CoordinatorMetadata.session":           object,
	"coordination#CoordinatorRegisterRequest.session_id": conversation,
	"coordination#CoordinatorRebindRequest.session_id":   conversation,
	"coordination#LeaseRequest.session_id":               conversation,
	"coordination#LeaseHolder.session_id":                conversation,
	"coordination#LeaseWaiter.session_id":                conversation,
	"coordination#WaitRequest.owner_session_id":          conversation,
	"coordination#WaitRequest.waiter_session_id":         conversation,
	"coordination#WaitReleaseRequest.owner_session_id":   conversation,
	"coordination#WaitCancelRequest.waiter_session_id":   conversation,
	"coordination#WaitWaiter.sessionId":                  conversation,
	"coordination#Wait.ownerSessionId":                   conversation,
	"coordination#CompletionRow.root_session_id":         conversation,

	"orchestrator#BrokerRoot.session_id":    conversation,
	"projects#PlaceResumed.session":         conversation,
	"schedules#ScheduleRequest.session_id":  conversation,
	"sessions#SessionRow.sessionId":         conversation,
	"sessions#SessionInfoSession.sessionId": conversation,
	"sessions#SessionInfo.session":          object,
	"tasks#TaskRoot.sessionId":              conversation,
	"tasks#TaskChild.sessionId":             conversation,
	// W6: a handoff's sender, resolved like a dispatch's root.
	"handover#BrokerHandoff.from_session":        conversation,
	"handover#BrokerHandoffRequest.from_session": conversation,

	// The known exceptions. Each one holds a terminal id, or either, under a
	// session's name.
	"orchestrator#BrokerMessageRequest.from_session": "either: the sender names itself by terminal or conversation (W1 messages route); retire when the messages route takes conversation ids",
	"orchestrator#BrokerMessageRequest.to_session":   "terminal: a message is typed into a place (W1 messages route); retire with the same change",
	"push#PushTestRequest.session_id":                "terminal: the console's open row id, compared with a watched terminal (push.go pushSessionURL); retire when the console names rows by conversation",
	"sessions#CloseMover.session_id":                 "either: a pending landing's mover is the root's terminal while it is live and its conversation id once it is not (orchestrator owed.go landingMovers, pinned by W4's TestAPendingLandingNamesWhoMustMove); retire by matching closeability on conversation ids",
	"sessions#CoordinationWaitRow.ownerSessionId":    "terminal: the Swift app's coordination waits, read from its store (swiftstore); retires with cutover B1",
	"sessions#CoordinationWaitRow.waiterSessionId":   "terminal: as above",
	"transcript#TranscriptNotice.waiter_session_id":  "either: echoes a file-wait notice as it was written — a terminal id from the Swift app, a conversation id from this daemon (W5)",
	"tasks#TaskRow.attachSession":                    "swift: the Swift app's attach_session, read-only from its store; retires with cutover B1",
	"tasks#TaskRow.session_root":                     "swift: the Swift app's task field, read-only from its store; retires with cutover B1",
	"projects#WorktreeOwner.sessionId":               "swift: the Swift worktree lifecycle's owner, read-only; namespace not verified; retires with cutover B1",
	"projects#WorktreeOrigin.sessionId":              "swift: as above",
	"projects#WorktreeContext.originSession":         object,
	"schedules#ScheduleRun.session_id":               "swift: a field of the Swift schedule file format this daemon does not fill",

	// W8: a root's own notification names where it lands the way the push
	// test does.
	"orchestrator#BrokerMachineNotifyRequest.session_id": "terminal: the Swift route's terminal-neutral id, compared with a watched terminal as PushTestRequest.session_id is (push.go pushSessionURL); retire with it",
}

func TestEverySessionFieldSaysWhichSessionItNames(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "api", "v1", "*.schema.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no schemas: %v", err)
	}
	seen := map[string]bool{}
	var unclassified []string
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			Defs map[string]struct {
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"$defs"`
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		file := strings.TrimSuffix(filepath.Base(f), ".schema.json")
		for def, schema := range doc.Defs {
			for prop := range schema.Properties {
				if !sessionNamed.MatchString(prop) || notAnIdentity.MatchString(prop) {
					continue
				}
				key := file + "#" + def + "." + prop
				seen[key] = true
				if _, ok := sessionFields[key]; !ok {
					unclassified = append(unclassified, key)
				}
			}
		}
	}
	sort.Strings(unclassified)
	for _, key := range unclassified {
		t.Errorf("%s names a session and does not say which: add it to sessionFields as a conversation id, "+
			"or as an exception with what retires it", key)
	}
	for key := range sessionFields {
		if !seen[key] {
			t.Errorf("%s is classified and no longer exists; remove its line", key)
		}
	}
}
