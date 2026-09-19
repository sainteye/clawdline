package work

import (
	"strings"
	"time"
)

// Runs: how a session relays what a person said to it (design-decisions U4,
// board-redesign §10 #7). The move a relay writes records the actor
// `user_via_session:<run>`, and the run is what makes that a person's word
// rather than the session's.
//
// A run is one message a person sent a session through this daemon — a
// device allowed to type into sessions, or this Mac's own browser, sending on
// `POST /v1/sessions/{id}/send`. It is issued when the bytes are typed, and
// that is the only way one comes to exist. It is not a broker task: a task is
// a root's delegation to a child, begun by a session, and naming one would
// let a session vouch for a person with its own dispatch. It is not a turn
// read out of a transcript either: a transcript shows the text a session was
// given, and the daemon types text into sessions for other reasons (a
// briefing, a relayed message), so a transcript cannot say a person sent it.
// A person typing straight into a terminal is heard by the session and by
// nobody else; there is no run for it, and a relay of it is refused.
//
// A relay rests on four things, each checked, each refused by name:
//
//   - the run exists: this daemon issued it (run_unknown otherwise);
//   - it is recent: issued within RelayWindow of the relay (run_expired);
//   - it was said to the session the answer belongs to, when the answer
//     belongs to one — a proposal or a decision is a question put to one
//     root, and only a message to that root answers it (run_other_session);
//   - and it was said after the question was put: a message from before a
//     proposal or a decision existed cannot be its answer
//     (run_before_question).
//
// What a run cannot prove is which session is relaying it. The orchestrator
// credential is one for the whole machine, so a session that deliberately
// reads another session's run can name it; run_other_session stops the
// mistake, not the intent. Telling sessions apart needs a credential per
// session, which this daemon does not have.

// RelayWindow is how long a person's message may be relayed after it was
// sent: the day it was said in, the same clock as a to-do worth asking about
// (LongLived). Past it, the session asks again.
const RelayWindow = 24 * time.Hour

// ActorViaSession is the prefix of a relayed actor: `user_via_session:<run>`.
const ActorViaSession = "user_via_session:"

// Run is one message a person sent a session through this daemon.
type Run struct {
	ID string `json:"id"`
	// Session is the conversation the message went to, and Terminal the tab it
	// was typed into. Session is empty when the daemon did not yet know the
	// conversation; such a run relays nothing that belongs to a session.
	Session   string `json:"session_id"`
	Terminal  string `json:"terminal_id"`
	Assistant string `json:"assistant,omitempty"`
	// Principal is the credential that sent it: local, or device:<id>.
	Principal string    `json:"principal"`
	At        time.Time `json:"-"`
}

// Actor is who a move made on this run's word says it was.
func (r Run) Actor() string { return ActorViaSession + r.ID }

// Evidence is what a move made on this run's word records of it.
func (r Run) Evidence() map[string]any {
	return map[string]any{"id": r.ID, "session": r.Session, "terminal": r.Terminal, "principal": r.Principal,
		"said_at": r.At.Unix()}
}

// RunShaped is the shape of a run id: a lowercase UUID, as this daemon
// issues them. Anything else was never issued and is refused before a read.
func RunShaped(id string) bool {
	if len(id) != 36 {
		return false
	}
	for i, c := range id {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
				return false
			}
		}
	}
	return true
}

// CheckRun is whether a run found (or not) may carry a relay now.
func CheckRun(r Run, found bool, now time.Time) error {
	switch {
	case !found:
		return refuse(403, "run_unknown",
			"No person's message carried by this daemon has that run id, so this is not a person's word. "+
				"A run is issued when a person sends the session a message through Clawdline; read yours with "+
				"GET /v1/orchestrator/sessions/<conversation id>/run.")
	case now.Sub(r.At) > RelayWindow:
		return refuse(403, "run_expired",
			"That run is a message from more than a day ago; ask the person again rather than relay it now.")
	}
	return nil
}

// RelayTo is whether a run's message answers a question that belongs to
// session and was put at asked.
func RelayTo(r Run, session string, asked time.Time) error {
	switch {
	case strings.TrimSpace(session) == "" || r.Session != session:
		return refuse(403, "run_other_session",
			"That run is a message the person sent another session; only a message to the session this "+
				"belongs to answers it.")
	case r.At.Before(asked):
		return refuse(403, "run_before_question",
			"That run is a message the person sent before this was asked, so it cannot be the answer to it. "+
				"Relay the message that answered it.")
	}
	return nil
}
