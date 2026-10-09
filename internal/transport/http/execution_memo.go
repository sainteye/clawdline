package http

import (
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// ExecutionMemoLimit is how many terminals one remembered scan may name. A
// scan past it is not remembered and goes to the store every time, as every
// scan did before the memo; it is the store's own row limit, which no scan
// that the store would accept can pass.
const ExecutionMemoLimit = store.ExecutionRecordsLimit

// executionMemo is what observeExecutions last committed, and for which scan.
//
// Every session list used to open a write transaction on the store: it read
// every execution row, compared, and committed even when nothing differed,
// once per list build. A list build is every two seconds per open stream, so
// that was the busiest writer in the database while the machine sat still.
//
// The memo holds one scan: the identities it saw, the terminals it listed and
// which sources could prove an absence. A scan equal to that one would commit
// nothing — every identity is already stored with that generation, and every
// row the store could have deleted for a proven absence was deleted by the
// scan remembered — so its answer is the remembered one and the store is not
// opened.
//
// It is held across the store call. observeExecutions is the only writer of
// those rows in this process, and two scans committing in one order and
// remembered in the other would leave a memo that disagrees with the store.
type executionMemo struct {
	mu     sync.Mutex
	key    string
	result map[string]string
	// skipped counts the scans answered without the store, for diagnostics
	// and the test that proves an unchanged scan writes nothing.
	skipped int64
}

// executionScanKey is a scan's identity for the memo, or false when the scan
// is too large to remember.
func executionScanKey(machine string, seen []store.ExecutionSeen, present map[string]bool,
	inv session.Inventory) (string, bool) {
	if len(seen) > ExecutionMemoLimit || len(present) > ExecutionMemoLimit {
		return "", false
	}
	var b strings.Builder
	b.WriteString(machine)
	b.WriteByte(0)
	for _, item := range seen {
		b.WriteString(item.ID)
		b.WriteByte(0x1f)
		b.WriteString(item.Source)
		b.WriteByte(0x1f)
		b.WriteString(item.Fingerprint)
		b.WriteByte(0x1e)
	}
	b.WriteByte(0)
	ids := make([]string, 0, len(present))
	for id := range present {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		b.WriteString(id)
		b.WriteByte(0x1e)
	}
	// Which sources may delete a row this scan does not list. A source no
	// reading names answers with the reading's completeness (ProvesAbsence).
	b.WriteByte(0)
	b.WriteString(strconv.FormatBool(inv.Complete))
	sources := make([]string, 0, len(inv.Sources))
	for source := range inv.Sources {
		sources = append(sources, source)
	}
	sort.Strings(sources)
	for _, source := range sources {
		proves, _ := inv.ProvesAbsence(source)
		b.WriteString(source)
		b.WriteByte('=')
		b.WriteString(strconv.FormatBool(proves))
		b.WriteByte(0x1e)
	}
	return b.String(), true
}

// recall answers the remembered generations for this scan, as a copy.
// The caller holds mu.
func (m *executionMemo) recall(key string) (map[string]string, bool) {
	if m.result == nil || m.key != key {
		return nil, false
	}
	m.skipped++
	out := make(map[string]string, len(m.result))
	for id, generation := range m.result {
		out[id] = generation
	}
	return out, true
}

// remember keeps what this scan committed. The caller holds mu.
func (m *executionMemo) remember(key string, ok bool, result map[string]string) {
	if !ok || result == nil {
		m.key, m.result = "", nil
		return
	}
	kept := make(map[string]string, len(result))
	for id, generation := range result {
		kept[id] = generation
	}
	m.key, m.result = key, kept
}

// reading is how many terminals the memo holds and how many scans it saved.
func (m *executionMemo) reading() (int, int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.result), m.skipped
}
