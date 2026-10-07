package capacity

import (
	"fmt"
	"strconv"
	"time"

	"github.com/sainteye/clawdline/internal/productcopy"
)

// A notice, said to a person (docs/limits.md §4.5, design-decisions C4).
//
// The tracker decides *when* a row has something to say: on entering critical
// or full, and on coming back to ok, at most once a day. This file decides
// *what* it says and *whether* it is said out loud — a push — rather than only
// written down as a `capacity.notify` event. The words are fixed when the
// notice is recorded, so the push that is owed carries the sentence that was
// true then and not a re-reading taken later.
//
// The push has a budget of its own. It is the tracker's one notice per row per
// NoticeEvery, and nothing here counts against the thirty an hour the agents'
// own notifications may send, nor they against it: a child that spends its
// allowance cannot silence a full store, and a register having a bad day
// cannot use up what a child needs to reach its person.

// Pushes reports whether a notice about this row is pushed: the rows whose
// register line says a person is told by notice. A row whose limit is answered
// to whoever sent the thing it turned away — a lease queue, an open wait — is
// told to that sender and written down; buzzing a phone about it would tell
// the one person who cannot do anything with it.
func Pushes(e Entry) bool {
	for _, c := range e.Told {
		if c == Notice {
			return true
		}
	}
	return false
}

// PushTag is the tag, and so the push service's topic, of every notice about
// one row: a newer one replaces an older one still waiting at the push service
// or still on the lock screen, so a phone shows where the row stands and not
// its history.
func PushTag(name string) string { return "capacity-" + name }

// NoticeText is the one sentence a notice says: what, how full, when it will
// be full, and what to do (limits §4.5). It fits the bounds every push from
// this daemon keeps — eighty characters of title, five hundred of body — for
// every registered row, which a test holds.
//
// state is the state the row entered; used and limit are the reading that
// entered it. full is when the row is projected to reach its limit, zero when
// it is not projected. loc is where the date is written for; nil is UTC.
func NoticeText(e Entry, state State, used, limit int64, full time.Time, loc *time.Location) (title, body string) {
	return NoticeTextLanguage("zh-Hant", e, state, used, limit, full, loc)
}

// NoticeTextLanguage renders fixed copy in the selected product language.
func NoticeTextLanguage(language string, e Entry, state State, used, limit int64, full time.Time, loc *time.Location) (title, body string) {
	amount := productcopy.Format(language, "capacity.amount", map[string]string{
		"used": amountOfLanguage(language, e.Unit, used), "limit": amountOfLanguage(language, e.Unit, limit), "percent": percent(used, limit),
	})
	if state == OK {
		return productcopy.Format(language, "capacity.recovered.title", map[string]string{"name": e.Name}),
			productcopy.Format(language, "capacity.recovered.body", map[string]string{"name": e.Name, "amount": amount})
	}
	if e.Name == ArtifactsDropsYoung {
		return youngDrops(language, used)
	}
	title = productcopy.Format(language, "capacity.critical.title", map[string]string{"name": e.Name, "percent": percent(used, limit)})
	if state == Full {
		title = productcopy.Format(language, "capacity.full.title", map[string]string{"name": e.Name})
	}
	body = productcopy.Format(language, "capacity.used", map[string]string{"name": e.Name, "amount": amount, "consequence": consequence(language, e)})
	if state != Full && !full.IsZero() {
		if loc == nil {
			loc = time.UTC
		}
		body += productcopy.Format(language, "capacity.projected", map[string]string{"at": full.In(loc).Format("01-02 15:04")})
	}
	if e.EvictedBy == Person {
		body += productcopy.Format(language, "capacity.person", nil)
	} else {
		body += productcopy.Format(language, "capacity.daemon", nil)
	}
	return title, body
}

// youngDrops is the notice of artifacts.drops_young, the one row whose
// reading is something that already happened rather than how full a thing is:
// pictures the drop cache's byte cap removed before they were a day old. What
// a person can do is send the picture again when a session cannot read it.
func youngDrops(language string, used int64) (title, body string) {
	return productcopy.Format(language, "capacity.young.title", map[string]string{"name": ArtifactsDropsYoung}),
		productcopy.Format(language, "capacity.young.body", map[string]string{"name": ArtifactsDropsYoung, "count": strconv.FormatInt(used, 10), "drops": ArtifactsDrops})
}

// consequence is what the row does at its limit, in the words a person needs:
// what stops working, or what is let go.
func consequence(language string, e Entry) string {
	key := "capacity.none"
	switch e.AtLimit {
	case Refuse:
		if e.EvictedBy == Person {
			if e.Class == Evidence {
				key = "capacity.refuse.evidence"
			} else {
				key = "capacity.refuse.person"
			}
		} else {
			key = "capacity.refuse.other"
		}
	case EvictOldest:
		key = "capacity.evict"
	case Expire:
		key = "capacity.expire"
	case Rotate:
		if e.Class == SecurityAudit {
			key = "capacity.rotate.audit"
		} else {
			key = "capacity.rotate"
		}
	case Summarize:
		key = "capacity.summarize"
	case Coalesce:
		key = "capacity.coalesce"
	case Disconnect:
		key = "capacity.disconnect"
	default:
		if e.Class == Evidence {
			key = "capacity.none.evidence"
		}
	}
	return productcopy.Format(language, key, nil)
}

// amountOf is a reading in the row's unit: bytes in binary units, the others
// as the count their register row names.
func amountOf(u Unit, n int64) string {
	return amountOfLanguage("en", u, n)
}

func amountOfLanguage(language string, u Unit, n int64) string {
	switch u {
	case Characters:
		return strconv.FormatInt(n, 10) + productcopy.Format(language, "capacity.unit.characters", nil)
	case Seconds:
		return strconv.FormatInt(n, 10) + productcopy.Format(language, "capacity.unit.seconds", nil)
	case Rows:
		return strconv.FormatInt(n, 10) + productcopy.Format(language, "capacity.unit.rows", nil)
	}
	const k = 1 << 10
	switch {
	case n >= k*k*k:
		return fmt.Sprintf("%.1f GiB", float64(n)/(k*k*k))
	case n >= k*k:
		return fmt.Sprintf("%.1f MiB", float64(n)/(k*k))
	case n >= k:
		return fmt.Sprintf("%.1f KiB", float64(n)/k)
	}
	return strconv.FormatInt(n, 10) + " B"
}

// percent is used over limit, rounded down, so a row that is not full never
// reads as 100%.
func percent(used, limit int64) string {
	if limit <= 0 {
		return "?%"
	}
	return strconv.FormatInt(used*100/limit, 10) + "%"
}

// Seed tells the tracker what an earlier process last said about a row: when,
// and what state the row had entered. A daemon that restarts keeps its
// promise of one notice per row per NoticeEvery — without it every restart
// would buzz the phone again about a row that has been critical all along —
// and a row that was last announced critical or full still owes its "back to
// ok" when it gets there, even if it recovered while nothing was running.
//
// A row this tracker has already read is left alone: what it saw itself is
// newer than anything a store can tell it.
func (t *Tracker) Seed(name string, at time.Time, state State) {
	t.mu.Lock()
	defer t.mu.Unlock()
	tr := t.rows[name]
	if tr == nil {
		tr = &track{}
		t.rows[name] = tr
	}
	if tr.seen || at.IsZero() {
		return
	}
	if at.After(tr.noticeAt) {
		tr.noticeAt = at
	}
	tr.alerted = state == Critical || state == Full
}
