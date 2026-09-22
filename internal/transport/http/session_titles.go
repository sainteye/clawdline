package http

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline/internal/adapters/swiftstore"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	"github.com/sainteye/clawdline/internal/domain/session"
)

const sessionTitlesKey = "session_titles"

// normalizedSessionTitle is the retired app's Config.normalizedSessionTitle:
// one visible line, with terminal control bytes and whitespace runs made into
// ordinary spaces. Empty is a deliberate clear.
func normalizedSessionTitle(raw string) string {
	mapped := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, raw)
	return strings.Join(strings.Fields(mapped), " ")
}

func titleWithinLimit(title string) bool {
	return utf8.RuneCountInString(title) <= int(CapacityLimit(capacity.SessionTitleCharacters))
}

// ownSessionTitles reads only this daemon's rows. A malformed row is skipped,
// as the retired Config did; a malformed settings file is refused by the
// settings adapter before a write can replace it.
func ownSessionTitles(v nextconfig.Values, now time.Time) []swiftstore.SessionTitle {
	var rows []swiftstore.SessionTitle
	if raw := v.Raw[sessionTitlesKey]; len(raw) > 0 {
		_ = json.Unmarshal(raw, &rows)
	}
	kept := rows[:0]
	maxAge := time.Duration(CapacityLimit(capacity.SessionTitleAge)) * time.Second
	for _, row := range rows {
		row.Title = normalizedSessionTitle(row.Title)
		if row.Automatic || row.Title == "" || !titleWithinLimit(row.Title) ||
			strings.TrimSpace(row.TerminalID) == "" || row.UpdatedAt == nil {
			continue
		}
		if maxAge > 0 && now.Sub(row.UpdatedAt.Time()) >= maxAge {
			continue
		}
		kept = append(kept, row)
	}
	sort.SliceStable(kept, func(i, j int) bool {
		return kept[i].UpdatedAt.Time().Before(kept[j].UpdatedAt.Time())
	})
	limit := int(CapacityLimit(capacity.SessionTitleRows))
	if limit >= 0 && len(kept) > limit {
		kept = kept[len(kept)-limit:]
	}
	return kept
}

// withOwnSessionTitles lays this daemon's manual names after the retired
// store's rows. The projection takes the last matching manual row, so a name
// chosen in the current product wins without modifying the retired app's file.
func (s *Server) withOwnSessionTitles(base swiftstore.Snapshot, now time.Time) swiftstore.Snapshot {
	v, err := s.settingsFile().Read()
	if err != nil {
		return base
	}
	base.Titles = append(append([]swiftstore.SessionTitle(nil), base.Titles...), ownSessionTitles(v, now)...)
	base.TitlesKnown = true
	return base
}

func sameSessionTitle(row swiftstore.SessionTitle, item session.Session) bool {
	if row.TerminalID == item.ID {
		return true
	}
	return item.ConversationID != "" && row.SessionID != nil && *row.SessionID == item.ConversationID
}

// saveSessionTitle replaces every current address for this conversation in
// one read-modify-write of config.json. The returned name is normalized; empty
// means the row was removed and the next label rung is visible again.
func (s *Server) saveSessionTitle(item session.Session, raw string, now time.Time) (string, error) {
	title := normalizedSessionTitle(raw)
	_, err := s.settingsFile().Change(func(v nextconfig.Values) (map[string]any, error) {
		rows := ownSessionTitles(v, now)
		kept := rows[:0]
		for _, row := range rows {
			if !sameSessionTitle(row, item) {
				kept = append(kept, row)
			}
		}
		rows = kept
		if title != "" {
			row := swiftstore.SessionTitle{Title: title, TerminalID: item.ID}
			if item.ConversationID != "" {
				id := item.ConversationID
				row.SessionID = &id
			}
			if started := swiftstore.ProcessStart(item.PID); !started.IsZero() {
				at := swiftstore.Seconds(float64(started.UnixNano()) / 1e9)
				row.StartedAt = &at
			}
			// A later /rename is a newer human utterance and retires this local
			// name. Capture the current transcript title only when the record
			// exists; before the first turn there is no baseline to compare.
			if path := recordPath(item); path != "" {
				if _, statErr := os.Stat(path); statErr == nil {
					row.SeenTranscriptPath = &path
					if item.CustomTitle != "" {
						seen := item.CustomTitle
						row.SeenCustomTitle = &seen
					}
				}
			}
			updated := swiftstore.Seconds(float64(now.UnixNano()) / 1e9)
			row.UpdatedAt = &updated
			rows = append(rows, row)
			limit := int(CapacityLimit(capacity.SessionTitleRows))
			if limit >= 0 && len(rows) > limit {
				rows = rows[len(rows)-limit:]
			}
		}
		return map[string]any{sessionTitlesKey: rows}, nil
	})
	return title, err
}

// sessionDisplayLabel asks the same title ladder as the fleet list. It is used
// by the info route and by clearing a title, where the reply must name the rung
// that has just become visible instead of flashing the terminal's raw label.
func (s *Server) sessionDisplayLabel(ctx context.Context, item session.Session) string {
	live := liveOf(item)
	lives := []swiftstore.Live{live}
	view := s.swift.Read()
	if s.store != nil {
		records, release := recordsContext(ctx)
		own, err := s.ownOverlay(records, lives)
		release()
		if err == nil {
			view = view.With(own)
		}
	}
	view = s.withOwnSessionTitles(view, time.Now())
	project := ""
	if s.icons != nil {
		project = s.icons.Label(item.CWD)
	}
	if project == "" && item.CWD != "" {
		project = filepath.Base(item.CWD)
	}
	return rowLabel(item, view.TitleOf(live, item.CustomTitle, lives), project)
}
