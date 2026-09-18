package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/board"
	"github.com/sainteye/clawdline-go/internal/domain/work"
)

// `clawdline board tracks`: the old cards on the three tracks of
// docs/board-redesign.md, by the same rules as `GET /v1/board/tracks`.
//
// Without --store it asks the running daemon. With --store it reads a board
// file itself — the Swift app's, or a copy of it — and the card logs beside
// it, and writes nothing anywhere: that is how a saved snapshot is replayed,
// and how the rules are checked against the numbers they were written from.

func boardCommand(args []string) {
	if len(args) == 0 || args[0] != "tracks" {
		fmt.Fprintln(os.Stderr, "usage: clawdline board tracks [--project id] [--track todo|board|backlog] [--rows] [--json]")
		fmt.Fprintln(os.Stderr, "                              [--store board.json [--sessions file [--assume-complete]] [--now time] [--stall-days n]]")
		os.Exit(2)
	}
	fs := flag.NewFlagSet("board tracks", flag.ExitOnError)
	project := fs.String("project", "", "only this project's cards")
	track := fs.String("track", "", "list the rows of one track: todo, board or backlog")
	rows := fs.Bool("rows", false, "list the cards under their buckets")
	asJSON := fs.Bool("json", false, "print the projection as JSON, every row included")
	store := fs.String("store", "", "read this board file instead of asking the daemon (read-only)")
	sessions := fs.String("sessions", "", "with --store: a saved GET /v1/sessions answer, the reading of running sessions")
	assume := fs.Bool("assume-complete", false, "with --sessions: take that reading as complete even where it says it was not")
	now := fs.String("now", "", "with --store: evaluate at this time, RFC 3339 or Unix seconds")
	stallDays := fs.Float64("stall-days", work.DefaultStall.Hours()/24, "with --store: days a started card may go without a fact")
	_ = fs.Parse(args[1:])
	if fs.NArg() > 0 {
		fail(fmt.Errorf("unexpected argument %q", fs.Arg(0)))
	}
	switch work.Track(*track) {
	case "", work.TrackTodo, work.TrackBoard, work.TrackBacklog:
	default:
		fail(fmt.Errorf("a track is todo, board or backlog, not %q", *track))
	}

	var t work.Tracks
	var err error
	if *store == "" {
		if *sessions != "" || *assume || *now != "" || isSet(fs, "stall-days") {
			fail(errors.New("--sessions, --assume-complete, --now and --stall-days replay a file; they need --store"))
		}
		t, err = daemonTracks(*project, work.Track(*track), *rows || *asJSON)
	} else {
		t, err = fileTracks(*store, *project, *sessions, *assume, *now, *stallDays)
		if err == nil && *track != "" {
			kept := []work.Row{}
			for _, r := range t.Rows {
				if r.Track == work.Track(*track) {
					kept = append(kept, r)
				}
			}
			t.Rows = kept
		}
	}
	if err != nil {
		fail(err)
	}
	if *asJSON {
		out, _ := json.MarshalIndent(map[string]any{"tracks": t}, "", "  ")
		fmt.Println(string(out))
		return
	}
	printTracks(os.Stdout, t, *rows)
}

func isSet(fs *flag.FlagSet, name string) bool {
	set := false
	fs.Visit(func(f *flag.Flag) { set = set || f.Name == name })
	return set
}

// daemonTracks reads the route, every page of it when the rows are wanted.
func daemonTracks(project string, track work.Track, all bool) (work.Tracks, error) {
	var first work.Tracks
	cursor := ""
	for page := 0; ; page++ {
		// The board holds at most 2,000 cards (the Swift app's limit), ten
		// pages; a route that keeps handing out cursors past fifty is wrong.
		if page == 50 {
			return first, errors.New("the daemon kept paging past fifty pages")
		}
		q := url.Values{}
		if project != "" {
			q.Set("project", project)
		}
		if track != "" {
			q.Set("track", string(track))
		}
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		path := "/v1/board/tracks"
		if len(q) > 0 {
			path += "?" + q.Encode()
		}
		req, err := daemonRequest(http.MethodGet, path, nil)
		if err != nil {
			return first, err
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return first, err
		}
		if res.StatusCode != http.StatusOK {
			defer res.Body.Close()
			var refusal struct {
				Error  string `json:"error"`
				Detail string `json:"detail"`
			}
			data, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
			if json.Unmarshal(data, &refusal) == nil && refusal.Error != "" {
				return first, fmt.Errorf("%s: %s (%s)", res.Status, refusal.Detail, refusal.Error)
			}
			return first, errors.New(res.Status)
		}
		var body struct {
			Tracks work.Tracks `json:"tracks"`
		}
		err = json.NewDecoder(io.LimitReader(res.Body, 16<<20)).Decode(&body)
		res.Body.Close()
		if err != nil {
			return first, err
		}
		if page == 0 {
			first = body.Tracks
		} else {
			first.Rows = append(first.Rows, body.Tracks.Rows...)
		}
		if !all || body.Tracks.NextCursor == nil {
			first.NextCursor = nil
			return first, nil
		}
		cursor = *body.Tracks.NextCursor
	}
}

// fileTracks projects a board file without a daemon. Nothing is written: the
// board and the logs are opened read-only and the answer goes to stdout.
func fileTracks(path, project, sessionsPath string, assume bool, nowText string, stallDays float64) (work.Tracks, error) {
	at := time.Now()
	if nowText != "" {
		parsed, err := parseInstant(nowText)
		if err != nil {
			return work.Tracks{}, err
		}
		at = parsed
	}
	if !(stallDays > 0 && stallDays <= 365) {
		return work.Tracks{}, fmt.Errorf("--stall-days is more than 0 and at most 365, not %v", stallDays)
	}
	if assume && sessionsPath == "" {
		return work.Tracks{}, errors.New("--assume-complete needs --sessions")
	}

	state, readAt, err := board.OpenLegacy(path).Read()
	if errors.Is(err, board.ErrLegacyAbsent) {
		return work.Tracks{}, fmt.Errorf("no board at %s", path)
	}
	if err != nil {
		return work.Tracks{}, err
	}
	ids := []string{}
	for _, item := range state.Items {
		if project == "" || item.ProjectID == project {
			ids = append(ids, item.ID)
		}
	}
	if project != "" && len(ids) == 0 {
		known := false
		for _, p := range state.Projects {
			known = known || p.ID == project
		}
		if !known {
			return work.Tracks{}, fmt.Errorf("the board has no project %q", project)
		}
	}
	histories, history := board.OpenHistory(board.HistoryDir(path)).Read(ids)
	keep := map[string]bool{}
	for _, id := range ids {
		keep[id] = true
	}
	var cards []work.Card
	for _, c := range board.TrackCards(state, histories) {
		if keep[c.ID] {
			cards = append(cards, c)
		}
	}

	presence := work.Presence{}
	presenceSource := work.PresenceSource{From: "none"}
	if sessionsPath != "" {
		presence, presenceSource, err = savedPresence(sessionsPath, assume)
		if err != nil {
			return work.Tracks{}, err
		}
	}

	stall := time.Duration(stallDays * float64(24*time.Hour))
	t := work.Tracks{Projection: work.Project(cards, presence, at, stall), Sources: work.Sources{
		Board: work.BoardSource{Status: "ok", Revision: state.Revision, Cards: len(state.Items),
			ReadAt: float64(readAt.UnixNano()) / 1e9},
		History: history, Presence: presenceSource,
	}}
	if project != "" {
		t.Project = &project
	}
	return t, nil
}

// savedPresence reads a saved `GET /v1/sessions` answer. The reading is
// complete when its own scan says so and every assistant in it has a
// conversation id — the rule the daemon applies to its live reading — or when
// the person running this says so with --assume-complete, which is recorded.
func savedPresence(path string, assume bool) (work.Presence, work.PresenceSource, error) {
	file, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return work.Presence{}, work.PresenceSource{}, err
	}
	defer file.Close()
	var snapshot struct {
		Scan *struct {
			Complete bool `json:"complete"`
		} `json:"scan"`
		Sessions []struct {
			ID        string `json:"id"`
			Assistant string `json:"assistant"`
			SessionID string `json:"sessionId"`
		} `json:"sessions"`
	}
	if err := json.NewDecoder(io.LimitReader(file, 16<<20)).Decode(&snapshot); err != nil {
		return work.Presence{}, work.PresenceSource{}, fmt.Errorf("%s is not a GET /v1/sessions answer: %w", path, err)
	}
	p := work.Presence{Complete: snapshot.Scan != nil && snapshot.Scan.Complete, Sessions: map[string]bool{}}
	count := 0
	for _, s := range snapshot.Sessions {
		if s.Assistant == "" {
			continue
		}
		count++
		p.Sessions[strings.ToLower(s.ID)] = true
		if s.SessionID == "" {
			p.Complete = false
			continue
		}
		p.Sessions[strings.ToLower(s.SessionID)] = true
	}
	source := work.PresenceSource{From: "file", Complete: p.Complete, Sessions: count}
	if assume && !p.Complete {
		p.Complete, source.Complete, source.Assumed = true, true, true
	}
	return p, source, nil
}

// parseInstant reads RFC 3339 or Unix seconds.
func parseInstant(text string) (time.Time, error) {
	if seconds, err := strconv.ParseFloat(text, 64); err == nil {
		whole := int64(seconds)
		return time.Unix(whole, int64((seconds-float64(whole))*1e9)), nil
	}
	if t, err := time.Parse(time.RFC3339Nano, text); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("--now is RFC 3339 or Unix seconds, not %q", text)
}

// printTracks is the §7.2 table, and the rows under it when asked.
func printTracks(w io.Writer, t work.Tracks, rows bool) {
	at := time.Unix(0, int64(t.EvaluatedAt*1e9)).UTC().Format(time.RFC3339)
	fmt.Fprintf(w, "rules     %s, stall %s, evaluated %s\n", t.Rules, days(t.StallSeconds), at)
	if t.Project != nil {
		fmt.Fprintf(w, "project   %s\n", *t.Project)
	}
	b, h, p := t.Sources.Board, t.Sources.History, t.Sources.Presence
	fmt.Fprintf(w, "board     revision %d, %d cards (%s)\n", b.Revision, b.Cards, b.Status)
	fmt.Fprintf(w, "history   %d logs, %d unreadable (%s)\n", h.Files, h.Unreadable, h.Status)
	switch {
	case p.From == "none":
		fmt.Fprintf(w, "presence  none given: no session can be shown gone\n")
	case p.Assumed:
		fmt.Fprintf(w, "presence  %d sessions from %s, taken as complete (--assume-complete; the reading said it was not)\n", p.Sessions, p.From)
	case p.Complete:
		fmt.Fprintf(w, "presence  %d sessions from %s, complete\n", p.Sessions, p.From)
	default:
		fmt.Fprintf(w, "presence  %d sessions from %s, incomplete: a session missing from it is unknown, not gone\n", p.Sessions, p.From)
	}
	fmt.Fprintln(w)

	total := t.Counts.Cards
	byTrack := map[work.Track]int{work.TrackTodo: t.Counts.Todo, work.TrackBoard: t.Counts.Board,
		work.TrackBacklog: t.Counts.Backlog}
	for _, track := range work.TrackOrder {
		fmt.Fprintf(w, "%-24s %4d  %5s\n", track, byTrack[track], percent(byTrack[track], total))
		for _, bc := range t.Buckets {
			if bc.Track == track {
				fmt.Fprintf(w, "  %-22s %4d  %s\n", bc.Bucket, bc.Count, composition(bc.Progress))
			}
		}
	}
	fmt.Fprintf(w, "%-24s %4d\n\n", "cards", total)

	count := map[work.Bucket]int{}
	for _, bc := range t.Buckets {
		count[bc.Bucket] = bc.Count
	}
	people := t.ForPeople()
	fmt.Fprintf(w, "for people %d: backlog %d (%s), board.closure %d (%s), board.now %d (%s), board.done %d (%s)\n",
		people, t.Counts.Backlog, percent(t.Counts.Backlog, people),
		count[work.BoardClosure], percent(count[work.BoardClosure], people),
		count[work.BoardNow], percent(count[work.BoardNow], people),
		count[work.BoardDone], percent(count[work.BoardDone], people))

	if !rows {
		return
	}
	for _, bucket := range work.BucketOrder {
		listed := false
		for _, r := range t.Rows {
			if r.Bucket != bucket {
				continue
			}
			if !listed {
				fmt.Fprintf(w, "\n%s\n", bucket)
				listed = true
			}
			fmt.Fprintf(w, "  %-9s %-15s %-26s %s\n", r.Key, r.Facts.Progress, r.Rule, clip(r.Title, 70))
		}
	}
}

func percent(n, of int) string {
	if of == 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f%%", float64(n)*100/float64(of))
}

func days(seconds float64) string {
	return strconv.FormatFloat(seconds/86400, 'g', -1, 64) + "d"
}

// composition is a bucket's progress states, largest first.
func composition(states map[string]int) string {
	names := make([]string, 0, len(states))
	for name := range states {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		if states[names[i]] != states[names[j]] {
			return states[names[i]] > states[names[j]]
		}
		return names[i] < names[j]
	})
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, fmt.Sprintf("%s %d", name, states[name]))
	}
	return strings.Join(parts, ", ")
}

func clip(s string, n int) string {
	runes := []rune(strings.ReplaceAll(s, "\n", " "))
	if len(runes) <= n {
		return string(runes)
	}
	return string(runes[:n-1]) + "…"
}
