package capacity

import (
	"fmt"
	"strconv"
	"time"
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
	amount := amountOf(e.Unit, used) + "／" + amountOf(e.Unit, limit) + "（" + percent(used, limit) + "）"
	if state == OK {
		return "容量已恢復：" + e.Name,
			e.Name + " 回到 " + amount + "，已經低於告警門檻，不用做什麼。"
	}
	if e.Name == ArtifactsDropsYoung {
		return youngDrops(used)
	}
	title = "容量快滿了：" + e.Name + "（" + percent(used, limit) + "）"
	if state == Full {
		title = "容量已滿：" + e.Name
	}
	body = e.Name + " 用了 " + amount + "。" + consequence(e) + "。"
	if state != Full && !full.IsZero() {
		if loc == nil {
			loc = time.UTC
		}
		body += "照最近的速度，" + full.In(loc).Format("01-02 15:04") + " 會滿。"
	}
	if e.EvictedBy == Person {
		body += "要騰出空間得由你決定；細節在設定頁的「容量」。"
	} else {
		body += "daemon 會照上面的規則自己處理；細節在設定頁的「容量」。"
	}
	return title, body
}

// youngDrops is the notice of artifacts.drops_young, the one row whose
// reading is something that already happened rather than how full a thing is:
// pictures the drop cache's byte cap removed before they were a day old. What
// a person can do is send the picture again when a session cannot read it.
func youngDrops(used int64) (title, body string) {
	return "圖片快取太小：" + ArtifactsDropsYoung,
		ArtifactsDropsYoung + "：過去一天有 " + strconv.FormatInt(used, 10) + " 張送出不到一天的圖，被 " + ArtifactsDrops +
			" 的位元組上限提早刪掉了。這些路徑已經打進對話，assistant 之後回頭讀（例如 compact 或 resume 之後）會讀不到；需要時請重新貼一次。細節在設定頁的「容量」。"
}

// consequence is what the row does at its limit, in the words a person needs:
// what stops working, or what is let go.
func consequence(e Entry) string {
	switch e.AtLimit {
	case Refuse:
		if e.EvictedBy == Person {
			if e.Class == Evidence {
				return "滿了之後新的寫入會被拒絕，/v1/health 會回 capacity_exhausted，daemon 不會自己刪任何東西"
			}
			return "滿了之後新的會被拒絕，daemon 不會自己刪任何東西"
		}
		return "滿了之後新的請求會被拒絕、請對方稍後再試，已經在裡面的不會被丟掉"
	case EvictOldest:
		return "滿了之後會淘汰最舊的"
	case Expire:
		return "滿了之後過了重試視窗的會過期"
	case Rotate:
		if e.Class == SecurityAudit {
			return "滿了之後會輪替成新的分段，舊的分段不刪"
		}
		return "滿了之後會輪替成新的分段，最舊的分段會被刪掉"
	case Summarize:
		return "滿了之後會先摘要再移走原文"
	case Coalesce:
		return "滿了之後只留最新的值"
	case Disconnect:
		return "滿了之後跟不上的讀者會被斷線、重新讀取"
	}
	// Nothing: a row that only reports its limit (it carries a Deviation).
	if e.Class == Evidence {
		return "目前滿了也不會拒絕寫入，只有 /v1/health 會回 capacity_exhausted"
	}
	return "目前滿了也不會拒絕或淘汰任何東西"
}

// amountOf is a reading in the row's unit: bytes in binary units, the others
// as the count their register row names.
func amountOf(u Unit, n int64) string {
	switch u {
	case Characters:
		return strconv.FormatInt(n, 10) + " 字"
	case Seconds:
		return strconv.FormatInt(n, 10) + " 秒"
	case Rows:
		return strconv.FormatInt(n, 10) + " 筆"
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
