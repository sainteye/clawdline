package projects

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/transcript"
)

const (
	AntigravityHistoryEntryLimit = 512
	AntigravityHistoryByteLimit  = 64 << 20
	AntigravityHistoryTimeLimit  = 250 * time.Millisecond
	AntigravityHistoryTitleLimit = 80
)

// AntigravityHistoryError distinguishes an unavailable evidence source or
// exhausted scan from an honestly empty history. It never includes a path.
type AntigravityHistoryError struct{ Code string }

func (e *AntigravityHistoryError) Error() string { return "Antigravity history: " + e.Code }

// AntigravityHistoryBounds lets the capacity owner lower each budget. Zero
// selects the default; values above the defaults cannot widen this reader.
type AntigravityHistoryBounds struct {
	Entries  int
	Bytes    int64
	Duration time.Duration
}

// AntigravityPast reads only brain/<UUID>'s known native log chain. Bindings
// must come from validated hook observations retained by the identity owner,
// not cwd inference, tool paths or model prose. Native USER metadata sampled
// in 1.4.5 carries time but no workspace; absence of this evidence is unknown.
// The caller's open set uses resolved native record paths, as ClaudePast does.
func AntigravityPast(ctx context.Context, home string, place Place, open map[string]bool, titles PastTitles, bindings map[string][]string, limit int, bounds ...AntigravityHistoryBounds) ([]Past, error) {
	if bindings == nil {
		return nil, &AntigravityHistoryError{Code: "workspace_evidence_unavailable"}
	}
	if !filepath.IsAbs(place.Path) {
		return nil, &AntigravityHistoryError{Code: "workspace_invalid"}
	}
	budget := AntigravityHistoryBounds{Entries: AntigravityHistoryEntryLimit, Bytes: AntigravityHistoryByteLimit, Duration: AntigravityHistoryTimeLimit}
	if len(bounds) > 0 {
		if bounds[0].Entries > 0 {
			budget.Entries = min(budget.Entries, bounds[0].Entries)
		}
		if bounds[0].Bytes > 0 {
			budget.Bytes = min(budget.Bytes, bounds[0].Bytes)
		}
		if bounds[0].Duration > 0 {
			budget.Duration = min(budget.Duration, bounds[0].Duration)
		}
	}
	deadline := time.Now().Add(budget.Duration)
	if limit <= 0 {
		return []Past{}, nil
	}
	dir, err := transcript.OpenAntigravityBrain(home)
	if errors.Is(err, transcript.ErrNoRecord) {
		return []Past{}, nil
	}
	if err != nil {
		return nil, &AntigravityHistoryError{Code: "directory_unreadable"}
	}
	defer dir.Close()
	entries, err := dir.ReadDir(budget.Entries + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, &AntigravityHistoryError{Code: "directory_unreadable"}
	}
	if len(entries) > budget.Entries {
		return nil, &AntigravityHistoryError{Code: "entry_budget_exceeded"}
	}
	var readBytes int64
	out := []Past{}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if time.Now().After(deadline) {
			return nil, &AntigravityHistoryError{Code: "time_budget_exceeded"}
		}
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		id := entry.Name()
		path, ok := transcript.AntigravityPath(home, id)
		if !ok {
			continue
		}
		member := false
		for _, workspace := range bindings[id] {
			if filepath.IsAbs(workspace) && filepath.Clean(workspace) == filepath.Clean(place.Path) {
				member = true
				break
			}
		}
		if !member {
			continue
		}
		st, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || !st.Mode().IsRegular() {
			return nil, &AntigravityHistoryError{Code: "record_unreadable"}
		}
		if st.Size() > budget.Bytes-readBytes {
			return nil, &AntigravityHistoryError{Code: "byte_budget_exceeded"}
		}
		readBytes += st.Size()
		info, err := transcript.ReadAntigravityInfo(path)
		if err != nil {
			return nil, err
		}
		if info.FirstUser == "" || info.LastActivity.IsZero() || strings.HasPrefix(info.FirstUser, BriefingOpening) {
			continue
		}
		fr := front{opening: antigravityHistoryOpening(info.FirstUser), said: info.FirstUser}
		title := displayedTitle(path, id, fr, titles)
		if title == "" {
			continue
		}
		out = append(out, Past{ID: id, Title: title, At: info.LastActivity, Live: open[path]})
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if time.Now().After(deadline) {
		return nil, &AntigravityHistoryError{Code: "time_budget_exceeded"}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].At.Equal(out[j].At) {
			return out[i].ID < out[j].ID
		}
		return out[i].At.After(out[j].At)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func antigravityHistoryOpening(text string) string {
	if end := strings.IndexAny(text, "\n\r"); end >= 0 {
		text = text[:end]
	}
	text = strings.TrimSpace(text)
	count := 0
	for index := range text {
		if count == AntigravityHistoryTitleLimit {
			return text[:index] + "…"
		}
		count++
	}
	return text
}
