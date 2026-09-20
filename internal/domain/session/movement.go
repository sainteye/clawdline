package session

import "time"

// When a session last moved, as an answer this daemon can give and every
// device can agree on.
//
// **One fact, chosen out of three that are not the same.** "Something
// happened" in a session can mean the assistant produced a turn, the person
// sent one, or the state changed, and a list ordered by the wrong one is worse
// than a list ordered by nothing:
//
//   - **State change** is not it. A session that has been working since
//     midnight changes state twice a night; ordered by that, an assistant
//     halfway through an eight-hour run sits below one that blinked idle and
//     back an hour ago. The thing that moves is exactly the thing state does
//     not record.
//   - **The person's send** alone is not it either: it makes every session the
//     person is not typing into look abandoned, which is most of them, and it
//     is the assistant's output that the list is watching for.
//   - **The assistant's output** alone drops the send, and a session that has
//     just been given a fresh instruction and has not answered yet is the one
//     a reader most wants near the top.
//
// So it is **the moment the session's own conversation record last grew**.
// Both an assistant turn and a person's message are appended to that file by
// the assistant itself, so one reading follows both; nothing else writes it;
// and it is one `stat` of a file that already exists, not a transcript read.
// Which of the two kinds of turn it was is deliberately not distinguished —
// that would need the file opened, and it would not change the order.
//
// It answers the question the list is really asked. An assistant that has run
// all night is appending to its record now, so it reads as moving now; a
// session somebody replied to ten minutes ago and which has been silent since
// reads as ten minutes old. Between those two — both idle, say, at four in the
// morning — the one replied to ten minutes ago comes first, which is the
// answer somebody scanning the list wants. While both are working they tie at
// "now" and the title separates them, exactly as before.
//
// **Not readable is not old.** A time that could not be read is carried as
// unknown with the reason, never as a very old time: an unread row that sorted
// as though it had been quiet for a week would be a row hidden at the bottom of
// the list by a failure nobody was told about. Consumers are required to order
// an unknown ahead of every known time inside its state, for the same reason
// the screen reader prefers "waiting" to "working" — of the two ways to be
// wrong, only one hides the row somebody has to act on.

// ActivityReason is which kind of nothing took the place of an activity time.
// Empty means there is a time.
type ActivityReason string

const (
	// ActivityRead means the time is there.
	ActivityRead ActivityReason = ""
	// ActivityNoRecord is a session that has written no conversation record
	// yet. It is a session opened a moment ago, not a session that has been
	// quiet: Claude Code writes its transcript at the first turn and Codex
	// its rollout at the first message.
	ActivityNoRecord ActivityReason = "no_record"
	// ActivityUnreadable is this machine failing to read a record that should
	// be there — which is its own fault to fix, and never the session's
	// silence.
	ActivityUnreadable ActivityReason = "unreadable"
	// ActivityUnread is a row this reading did not get to: the reading's own
	// ceiling on how many records it may stat was reached
	// (capacity `sessions.activity_reads`).
	ActivityUnread ActivityReason = "unread"
	// ActivityUnsupported is a row nothing here can answer for at all — no
	// identity source was asked, or the assistant keeps no record this can
	// read.
	ActivityUnsupported ActivityReason = "unsupported"
)

// Activity is that answer for one session.
type Activity struct {
	// At is when the record last grew. Meaningful only when Known.
	At time.Time
	// Reason is empty when there is a time, and otherwise which kind of
	// nothing there is instead.
	Reason ActivityReason
	// Evidence is how the time was obtained.
	Evidence Evidence
	// Detail is this machine's own sentence about the answer, for a reader
	// deciding whether to wait, fix this machine or stop asking.
	Detail string
}

// Known reports whether there is a time to order by.
func (a Activity) Known() bool { return a.Reason == ActivityRead && !a.At.IsZero() }

// Unknown is the reason a reader is told, normalised: a value nobody filled in
// is `unsupported` rather than an empty word that would read as "there is a
// time" beside an absent one.
func (a Activity) Unknown() ActivityReason {
	if a.Known() {
		return ActivityRead
	}
	if a.Reason == ActivityRead {
		return ActivityUnsupported
	}
	return a.Reason
}
