package projects

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/swiftstore"
)

// BoardStorePath is ProjectBoardStore.defaultURL, including its override.
func BoardStorePath() string {
	if override := os.Getenv("CLAWDLINE_BOARD_STORE"); override != "" {
		return override
	}
	return filepath.Join(home(), "Library", "Application Support", "Clawdline", "project-board.json")
}

// boardFile declares only what the catalog reads. Everything else in the
// Swift app's Board store — items' titles, evidence, receipts — is skipped by
// the decoder and never held.
type boardFile struct {
	Enabled   *bool   `json:"enabled"`
	Revision  int64   `json:"revision"`
	UpdatedAt float64 `json:"updatedAt"`
	Projects  []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"projects"`
	Items []struct {
		ProjectID string `json:"projectId"`
	} `json:"items"`
}

// BoardProject is one row of the Board store's project list, with the one
// count this port can state exactly.
type BoardProject struct {
	ID        string
	Name      string
	ItemCount int
}

// BoardCatalog is one reading of the Swift app's Board store.
//
// **Only the catalog, and only what it can say for certain.** The Swift app's
// catalog also counts each Project's active items, and that count is its
// progress projection (ProjectBoardStore.progressObject) — attempt chronology,
// verification evidence, spans, session deliveries — which this port does not
// restate. So a Project's active count is not in this reading at all, and the
// page draws it as the original draws an unknown count, not as zero.
type BoardCatalog struct {
	// Known is false when the store could not be read and there is no earlier
	// reading to carry.
	Known bool
	// Stale is true when the newest read failed and an earlier one is carried.
	Stale     bool
	Err       error
	Enabled   bool
	Revision  int64
	UpdatedAt float64
	Projects  []BoardProject
	ReadAt    time.Time
}

// Catalog reads the Board store on demand, decoding only when the file's
// size or time changed. A half-written file keeps the last good reading,
// because the Swift app rewrites this file while it runs.
type Catalog struct {
	Path string

	mu    sync.Mutex
	stamp string
	last  *BoardCatalog
}

// NewCatalog reads the Swift app's Board store; with the legacy switch off
// (CLAWDLINE_NEXT_LEGACY_STORE=off, cutover B1) it reads nothing.
func NewCatalog() *Catalog {
	if swiftstore.Disabled() {
		return &Catalog{}
	}
	return &Catalog{Path: BoardStorePath()}
}

// ErrCatalogDisabled is the Board store not read because the legacy switch is
// off: known, and not a failure to read it.
var ErrCatalogDisabled = errors.New("reading the Swift app's board is switched off")

func (c *Catalog) Read() BoardCatalog {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.Path == "" {
		return BoardCatalog{Err: ErrCatalogDisabled}
	}
	fail := func(err error) BoardCatalog {
		if c.last != nil {
			out := *c.last
			out.Stale, out.Err = true, err
			return out
		}
		return BoardCatalog{Err: err}
	}
	st, err := os.Stat(c.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// No store is not a stale store: there is nothing earlier to carry.
			c.last, c.stamp = nil, ""
			return BoardCatalog{Err: err}
		}
		return fail(err)
	}
	s := fmt.Sprintf("%d:%d", st.Size(), st.ModTime().UnixNano())
	if s == c.stamp && c.last != nil {
		return *c.last
	}
	data, err := os.ReadFile(c.Path)
	if err != nil {
		return fail(err)
	}
	var file boardFile
	if err := json.Unmarshal(data, &file); err != nil {
		return fail(fmt.Errorf("project-board.json did not decode: %w", err))
	}
	if file.Enabled == nil {
		return fail(errors.New("project-board.json carries no enabled flag"))
	}
	counts := map[string]int{}
	for _, item := range file.Items {
		counts[item.ProjectID]++
	}
	out := BoardCatalog{Known: true, Enabled: *file.Enabled, Revision: file.Revision,
		UpdatedAt: file.UpdatedAt, ReadAt: time.Now()}
	for _, p := range file.Projects {
		out.Projects = append(out.Projects, BoardProject{ID: p.ID, Name: p.Name, ItemCount: counts[p.ID]})
	}
	c.last, c.stamp = &out, s
	return out
}
