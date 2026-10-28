package http

import (
	"encoding/json"
	"strconv"
	"sync"

	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/capacity"
)

const transcriptDeltaPagesLimit = 128
const transcriptDeltaBytesLimit = 8 << 20
const transcriptDeltaRowsLimit = 1000

type transcriptBaseline struct {
	key string
	raw []byte
}

// Immutable JSON baselines bound both retained versions and their charged bytes.
// Eviction never removes history: a miss sends the current full page instead.
type transcriptBaselines struct {
	mu      sync.Mutex
	rows    []transcriptBaseline
	bytes   int
	evicted int64
}

func (c *transcriptBaselines) get(key string) []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, row := range c.rows {
		if row.key == key {
			copy(c.rows[i:], c.rows[i+1:])
			c.rows[len(c.rows)-1] = row
			return row.raw
		}
	}
	return nil
}
func (c *transcriptBaselines) put(key string, raw []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(raw) > transcriptDeltaBytesLimit {
		return
	}
	for _, row := range c.rows {
		if row.key == key {
			return
		}
	}
	for len(c.rows) >= transcriptDeltaPagesLimit || c.bytes+len(raw) > transcriptDeltaBytesLimit {
		c.bytes -= len(c.rows[0].raw)
		copy(c.rows, c.rows[1:])
		c.rows[len(c.rows)-1] = transcriptBaseline{}
		c.rows = c.rows[:len(c.rows)-1]
		c.evicted++
	}
	c.rows = append(c.rows, transcriptBaseline{key, raw})
	c.bytes += len(raw)
}
func (c *transcriptBaselines) reading(rows bool) capacity.Reading {
	c.mu.Lock()
	defer c.mu.Unlock()
	used := c.bytes
	if rows {
		used = len(c.rows)
	}
	return capacity.Reading{Known: true, Used: int64(used), Counters: capacity.Counters{Evicted: c.evicted}, Note: "evicted baselines fall back to a full transcript page"}
}

func transcriptDelta(base, page contract.TranscriptPage) contract.TranscriptDelta {
	left, right := base.Entries, page.Entries
	encode := func(rows []contract.TranscriptEntry) []string {
		out := make([]string, len(rows))
		for i, row := range rows {
			raw, _ := json.Marshal(row)
			out[i] = string(raw)
		}
		return out
	}
	old, now := encode(left), encode(right)
	// The registered comparison ceiling bounds this matrix. LCS preserves order and
	// duplicate counts, including a rolling window dropping and appending rows.
	width := len(now) + 1
	lengths := make([]uint16, (len(old)+1)*width)
	for i := len(old) - 1; i >= 0; i-- {
		for j := len(now) - 1; j >= 0; j-- {
			if old[i] == now[j] {
				lengths[i*width+j] = 1 + lengths[(i+1)*width+j+1]
			} else {
				lengths[i*width+j] = max(lengths[(i+1)*width+j], lengths[i*width+j+1])
			}
		}
	}
	edits := []contract.TranscriptSplice{}
	i, j := 0, 0
	for i < len(old) || j < len(now) {
		if i < len(old) && j < len(now) && old[i] == now[j] {
			i++
			j++
			continue
		}
		edit := contract.TranscriptSplice{Start: int64(i), Entries: []contract.TranscriptEntry{}}
		for i < len(old) || j < len(now) {
			if i < len(old) && j < len(now) && old[i] == now[j] {
				break
			}
			if j < len(now) && (i == len(old) || lengths[i*width+j+1] >= lengths[(i+1)*width+j]) {
				edit.Entries = append(edit.Entries, right[j])
				j++
			} else {
				edit.DeleteCount++
				i++
			}
		}
		edits = append(edits, edit)
	}
	metadata := page
	metadata.Entries = []contract.TranscriptEntry{}
	return contract.TranscriptDelta{BaseVersion: base.Version, EntryCount: int64(len(right)), Page: metadata, Delta: edits}
}

// The same projected page is recomputed before diffing, so rewrites, completed
// tools, attachments and native history replacement follow full-read semantics.
func (s *Server) transcriptDeltaAnswer(page contract.TranscriptPage, known, scope string, limit int) any {
	if len(page.Entries) > transcriptDeltaRowsLimit {
		return page
	}
	raw, _ := json.Marshal(page)
	key := func(version string) string { return scope + "\x00" + strconv.Itoa(limit) + "\x00" + version }
	previous := s.transcriptBaselines.get(key(known))
	s.transcriptBaselines.put(key(page.Version), raw)
	if known == "" || len(previous) == 0 {
		return page
	}
	var base contract.TranscriptPage
	if json.Unmarshal(previous, &base) != nil || len(base.Entries) > transcriptDeltaRowsLimit {
		return page
	}
	delta := transcriptDelta(base, page)
	encoded, _ := json.Marshal(delta)
	if len(encoded) >= len(raw) {
		return page
	}
	return delta
}
