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
	// Excerpt is the start of what the person said, kept so that what a
	// Session did on their word can show that word (RunExcerptOf). Runs
	// issued before it existed have none.
	Excerpt string `json:"excerpt,omitempty"`
}

// Actor is who a move made on this run's word says it was.
func (r Run) Actor() string { return ActorViaSession + r.ID }

// Evidence is what a move made on this run's word records of it.
func (r Run) Evidence() map[string]any {
	return map[string]any{"id": r.ID, "session": r.Session, "terminal": r.Terminal, "principal": r.Principal,
		"said_at": r.At.Unix()}
}

// AcceptanceRevisionInstruction requires the retained words of the person's
// message to request an acceptance change. An item reference is required when
// the owning Root has more than one open item. A run proves a message existed,
// not that an unrelated message authorized an edit.
func AcceptanceRevisionInstruction(excerpt, itemID, title string, soleOwnedItem bool) bool {
	words := strings.ToLower(strings.Join(strings.Fields(excerpt), " "))
	// An answer Note appends its own ID and context after the person's reply.
	// Its ID identifies the Note, not another Board item. Keep the reply intact.
	const noteContext = " (clawdline 便條 "
	if at := strings.LastIndex(words, noteContext); at >= 0 {
		suffix := words[at+len(noteContext):]
		if len(suffix) >= 36 && RunShaped(suffix[:36]) && strings.HasPrefix(suffix[36:], "：「") {
			words = words[:at]
		}
	}
	identified := strings.Contains(words, strings.ToLower(itemID))
	if title = strings.ToLower(strings.TrimSpace(title)); title != "" {
		identified = identified || strings.Contains(words, title)
	}
	// A conversation may refer to "this Epic" without repeating its title.
	// That context is usable only when this Root owns exactly one open item.
	for n := 0; n+36 <= len(words); n++ {
		candidate := words[n : n+36]
		if RunShaped(candidate) && candidate != strings.ToLower(itemID) {
			return false
		}
	}
	if !identified && !soleOwnedItem {
		return false
	}
	// A message can discuss several actions. A denial about one action must
	// not cancel a request about acceptance in a later sentence, and a polite
	// request about another action must not authorize acceptance editing.
	for _, clause := range strings.FieldsFunc(words, func(r rune) bool {
		return strings.ContainsRune("。！？?;；", r)
	}) {
		if !strings.Contains(clause, "驗收") && !strings.Contains(clause, "acceptance") {
			continue
		}
		change := false
		for _, verb := range []string{"修訂", "修改", "更新", "改寫", "改成", "改為", "調整", "刪除", "revise", "change", "update", "edit", "remove"} {
			change = change || strings.Contains(clause, verb)
		}
		if !change {
			continue
		}
		// A replacement criterion can say that a former requirement is no
		// longer needed. "不要求" contains "不要" as bytes, but it does not
		// negate the earlier request to revise this item's acceptance.
		denialScope := strings.NewReplacer("不要求", "", "do not require", "", "don't require", "").Replace(clause)
		denied := false
		for _, denial := range []string{"不要", "別改", "禁止", "不准", "不得", "不應", "不能", "不可以", "do not", "don't", "never", "must not", "should not"} {
			denied = denied || strings.Contains(denialScope, denial)
		}
		if denied {
			continue
		}
		request := false
		for _, marker := range []string{"請", "幫我", "麻煩", "我要求", "我要", "我想要", "改成", "改為", "應改", "please "} {
			request = request || strings.Contains(clause, marker)
		}
		for _, imperative := range []string{"revise ", "change ", "update ", "edit ", "remove "} {
			request = request || strings.HasPrefix(clause, imperative)
		}
		if request && (!strings.Contains(clause, "為何") || strings.Contains(clause, "請")) &&
			(!strings.Contains(clause, "why ") || strings.Contains(clause, "please ")) {
			return true
		}
	}
	return false
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
