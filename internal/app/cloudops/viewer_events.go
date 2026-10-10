package cloudops

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// viewerEventsWord is the command a hosted page sends its own failures with:
// a batch of small, content-free rows its copied client keeps in the browser
// (`legacy/js/net/cloud-viewer-events.js`) and delivers, with nobody pressing
// anything, to the one paired machine that advertises this word.
//
// It is the only way a failure that happened on the phone reaches this
// machine. On 2026-10-10 the hosted Session page said "could not read the
// conversation" several times an hour while this daemon's log held no refused
// or slow read at all: whatever went wrong went wrong before the request left
// the page or after the answer left here, and nothing recorded which.
const viewerEventsWord = "diagnostics.events"

// ViewerEventsBatchBytesLimit is the largest batch this machine reads. The
// page cuts its own at 240 KiB (`VIEWER_EVENT_LIMITS.batchBytes`), so a larger
// one is refused, never trimmed: the page drops it and counts it.
const ViewerEventsBatchBytesLimit = 256 << 10

// ViewerEventsLoggedRowsLimit is how many rows of one batch become a log line
// each. The page holds at most 100 (`VIEWER_EVENT_LIMITS.rows`); past this the
// rest are one count on the batch's summary line, so a page that misbehaves
// cannot write a thousand lines into this machine's log in one envelope.
const ViewerEventsLoggedRowsLimit = 128

// ViewerEventsLineBytesLimit is the longest log line one row becomes. A row is
// at most 2 KiB on the page; a longer line is cut and says so.
const ViewerEventsLineBytesLimit = 1024

// ViewerEventsSeenBatchesLimit is how many delivered batch ids are remembered,
// per process, so a batch the page sends again — its receipt was lost — is
// acknowledged without being written twice. The oldest is forgotten first.
const ViewerEventsSeenBatchesLimit = 256

// viewerFieldsNeverLogged are names whose values are not written even if a
// page sends them. The page already keeps them out (`FORBIDDEN_FIELDS`); this
// is the second fence, on the machine that writes the file.
var viewerFieldsNeverLogged = map[string]bool{
	"nonce": true, "ct": true, "sig": true, "plaintext": true, "clear": true, "token": true,
	"device_token": true, "cookie": true, "cookies": true, "secret": true, "master_secret": true,
	"key_bytes": true, "private_key": true, "title": true, "text": true, "transcript": true,
	"message": true, "line": true,
}

// ViewerEventLog writes what a page reported to this daemon's log, one line
// per row, and remembers which batches it has written.
type ViewerEventLog struct {
	// Log is the daemon's log. Nil discards, which only a test wants.
	Log func(format string, args ...any)
	// Now is the clock a row's age is measured by. Nil is time.Now.
	Now func() time.Time

	mu    sync.Mutex
	seen  map[string]int
	order []string
}

// viewerBatch is the part of a batch this machine reads. The rest of the
// page's shape is the page's: an unknown field is not a refusal here.
type viewerBatch struct {
	V            int              `json:"v"`
	BatchID      string           `json:"batch_id"`
	WebBuild     string           `json:"web_build"`
	Rows         []viewerEventRow `json:"rows"`
	Completeness struct {
		DroppedRateLimited int `json:"dropped_rate_limited"`
		DroppedOverflow    int `json:"dropped_overflow"`
		DroppedRefused     int `json:"dropped_refused"`
		DroppedUnflushed   int `json:"dropped_unflushed"`
	} `json:"completeness"`
}

type viewerEventRow struct {
	N     int64                      `json:"n"`
	AtMS  int64                      `json:"at_ms"`
	Event string                     `json:"event"`
	Data  map[string]json.RawMessage `json:"data"`
}

// viewerEvents answers one `diagnostics.events` batch.
func (b Bridge) viewerEvents(cmd Command, p plan) Answer {
	if b.ViewerEvents == nil {
		return b.publish(cmd, p, Refusal{Status: 400, Code: "unknown_command",
			Message: "This machine does not know that Cloud command.", fixedCopy: true}, nil)
	}
	receipt, refusal := b.ViewerEvents.Write(cmd.Sender, p.document)
	if refusal != nil {
		return b.publish(cmd, p, *refusal, nil)
	}
	body, err := json.Marshal(receipt)
	if err != nil {
		return b.publish(cmd, p, Refusal{Status: 500, Code: "viewer_events_write_failed",
			Message: "This machine could not record the page's diagnostic events.", Layer: layerRoute}, nil)
	}
	return b.publish(cmd, p, Refusal{}, body)
}

// ViewerEventsReceipt is the answer the page removes its batch on: it checks
// `batch_id` and `rows` against what it sealed.
type ViewerEventsReceipt struct {
	OK        bool   `json:"ok"`
	BatchID   string `json:"batch_id"`
	Rows      int    `json:"rows"`
	Logged    int    `json:"logged"`
	Duplicate bool   `json:"duplicate"`
}

// Write logs one batch from `sender`, or refuses it. A refusal of the batch's
// bytes is `viewer_events_malformed` in the route layer, which the page reads
// as "drop this batch and count it" rather than "stop sending".
func (l *ViewerEventLog) Write(sender string, raw []byte) (ViewerEventsReceipt, *Refusal) {
	malformed := func(why string) (ViewerEventsReceipt, *Refusal) {
		return ViewerEventsReceipt{}, &Refusal{Status: 400, Code: "viewer_events_malformed",
			Message: "The page's diagnostic events are malformed at " + why + ".", Layer: layerRoute}
	}
	if len(raw) > ViewerEventsBatchBytesLimit {
		return ViewerEventsReceipt{}, &Refusal{Status: 413, Code: "viewer_events_too_large",
			Message: "The page's diagnostic events are larger than this machine reads.", Layer: layerRoute}
	}
	var batch viewerBatch
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(&batch); err != nil {
		return malformed("batch")
	}
	if batch.V != 1 {
		return malformed("v")
	}
	if batch.BatchID == "" || len(batch.BatchID) > 128 || !printable(batch.BatchID) {
		return malformed("batch_id")
	}
	for i, row := range batch.Rows {
		if row.Event == "" || len(row.Event) > 96 || !printable(row.Event) {
			return malformed("rows[" + strconv.Itoa(i) + "].event")
		}
	}
	receipt := ViewerEventsReceipt{OK: true, BatchID: batch.BatchID, Rows: len(batch.Rows)}
	key := sender + "\t" + batch.BatchID
	l.mu.Lock()
	defer l.mu.Unlock()
	if logged, done := l.seen[key]; done {
		receipt.Logged, receipt.Duplicate = logged, true
		return receipt, nil
	}
	now := l.now()
	for i, row := range batch.Rows {
		if i == ViewerEventsLoggedRowsLimit {
			break
		}
		l.logf("%s", viewerEventLine(sender, batch.BatchID, row, now))
		receipt.Logged++
	}
	c := batch.Completeness
	l.logf("cloud: viewer events delivered: sender=%s batch=%s rows=%d logged=%d dropped_rate_limited=%d dropped_overflow=%d dropped_refused=%d dropped_unflushed=%d web_build=%s",
		logWord(sender), logWord(shortBatch(batch.BatchID)), len(batch.Rows), receipt.Logged,
		c.DroppedRateLimited, c.DroppedOverflow, c.DroppedRefused, c.DroppedUnflushed, logWord(batch.WebBuild))
	l.remember(key, receipt.Logged)
	return receipt, nil
}

func (l *ViewerEventLog) remember(key string, logged int) {
	if l.seen == nil {
		l.seen = map[string]int{}
	}
	l.seen[key] = logged
	l.order = append(l.order, key)
	if len(l.order) > ViewerEventsSeenBatchesLimit {
		delete(l.seen, l.order[0])
		l.order = l.order[1:]
	}
}

func (l *ViewerEventLog) now() time.Time {
	if l.Now != nil {
		return l.Now()
	}
	return time.Now()
}

func (l *ViewerEventLog) logf(format string, args ...any) {
	if l.Log != nil {
		l.Log(format, args...)
	}
}

// viewerEventLine is one row as one line: the event, how long ago the page
// recorded it, and its fields in name order. A read failure the page records
// (`cloud.read.failed`) therefore reads
// `cloud: viewer reported: event=cloud.read.failed age_s=12 cond=marker_stale code=… stage=viewer_refused word=transcript sender=…`.
func viewerEventLine(sender, batchID string, row viewerEventRow, now time.Time) string {
	var line strings.Builder
	fmt.Fprintf(&line, "cloud: viewer reported: event=%s", logWord(row.Event))
	if row.AtMS > 0 {
		age := now.Sub(time.UnixMilli(row.AtMS)).Seconds()
		fmt.Fprintf(&line, " age_s=%d", int64(age))
	}
	names := make([]string, 0, len(row.Data))
	for name := range row.Data {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if viewerFieldsNeverLogged[name] || !printable(name) || len(name) > 64 {
			continue
		}
		value, ok := viewerValue(row.Data[name])
		if !ok {
			continue
		}
		fmt.Fprintf(&line, " %s=%s", logWord(name), value)
	}
	fmt.Fprintf(&line, " sender=%s batch=%s n=%d", logWord(sender), logWord(shortBatch(batchID)), row.N)
	out := line.String()
	if len(out) > ViewerEventsLineBytesLimit {
		cut := ViewerEventsLineBytesLimit - len(" …cut")
		for cut > 0 && !utf8Start(out[cut]) {
			cut--
		}
		out = out[:cut] + " …cut"
	}
	return out
}

// viewerValue is a scalar or a short list of scalars, as one word. Anything
// else — an object, a list of objects — is not written.
func viewerValue(raw json.RawMessage) (string, bool) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return "", false
	}
	switch v := value.(type) {
	case nil:
		return "null", true
	case bool:
		return strconv.FormatBool(v), true
	case json.Number:
		return logWord(v.String()), true
	case string:
		return logWord(v), true
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			switch s := item.(type) {
			case string:
				parts = append(parts, logWord(s))
			case json.Number:
				parts = append(parts, s.String())
			case bool:
				parts = append(parts, strconv.FormatBool(s))
			default:
				return "", false
			}
		}
		return "[" + strings.Join(parts, ",") + "]", true
	}
	return "", false
}

// logWord keeps a value on its own line and one word long: whitespace and
// control characters become `_`, and an empty value is `-`.
func logWord(value string) string {
	if value == "" {
		return "-"
	}
	if len(value) > 160 {
		cut := 160
		for cut > 0 && !utf8Start(value[cut]) {
			cut--
		}
		value = value[:cut]
	}
	return strings.Map(func(r rune) rune {
		if r <= ' ' || r == 0x7f || r == '=' {
			return '_'
		}
		return r
	}, value)
}

func shortBatch(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func utf8Start(b byte) bool { return b&0xC0 != 0x80 }
