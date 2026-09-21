package orchestrator

import (
	"sync"

	"github.com/sainteye/clawdline/internal/adapters/store"
)

// decodedRecords is every record the whole-table readers have decoded, by id,
// with the version it was decoded at.
//
// It is not a second copy of a fact. A row is used from here only while the
// store still says it is at this version, and the version moves with every
// write — so a reader holding one of these holds exactly what it would have
// decoded, minus the decoding. It is bounded by the table: each whole-table
// read replaces it with the rows that read found.
type decodedRecords struct {
	mu   sync.Mutex
	rows map[string]decodedRecord
}

type decodedRecord struct {
	version int64
	record  Record
	err     error
	// row is the stored row the record came from, kept for an unreadable
	// one's lifted columns.
	row store.BrokerRow
}

// lookup answers the rows it holds at the version each head says, and the
// ids it must fetch.
func (d *decodedRecords) lookup(heads []store.BrokerHead) (map[string]decodedRecord, []string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make(map[string]decodedRecord, len(heads))
	stale := []string{}
	for _, h := range heads {
		if got, ok := d.rows[h.ID]; ok && got.version == h.Version {
			out[h.ID] = got
			continue
		}
		stale = append(stale, h.ID)
	}
	return out, stale
}

func (d *decodedRecords) replace(rows map[string]decodedRecord) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.rows = rows
}
