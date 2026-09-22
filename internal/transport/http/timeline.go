package http

import (
	"context"
	"net/http"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// The Project Timeline: GET /v1/timeline.
//
// **Its whole reason for existing is that a commit is not a release.** A build
// that compiled, a task that answered `success` and an assistant's summary of
// its own work are none of them evidence that anything reached anybody
// (timeline-design A1, design-decisions D41).
//
// So this is a projection and it stores nothing (D01, D04): the entries are the
// broker's own task records, and an entry is `landed_to_git` only where the
// landing record proves a commit reached its target, `upcoming` where a
// delivery has not landed. **There is no producer of deployment evidence on
// this machine**, so no entry is ever drawn as available and the status
// vocabulary here is three words rather than the Swift app's twelve: a word no
// producer can write is a promise the screen cannot keep (X25).
//
// The Swift app kept a 2.4 MB document that reached its entry ceiling and then
// stopped recording for over a day with nobody told (timeline-design §0). This
// cannot: there is no document, the bound is on the answer, and what the bound
// drops is re-derived on the next read from records this daemon still holds.

// What one timeline read may reach. `timeline.entries` is a registered row:
// the task records under it grow with every dispatch and the store bounds only
// their bytes, so the read over them is bounded here, the bound is on the wire
// (timeline-design A4) and the oldest is what is left out.
const (
	// timelineEntryLimit is the most entries one Project's timeline holds.
	timelineEntryLimit = 500
	// timelinePageLimit is how many of them one page carries.
	timelinePageLimit = 40
	// timelineSummaryLimit is how much of a delivery's own account of itself
	// a card carries. The rest is on the task, which is where a reader who
	// wants all of it should be reading it.
	timelineSummaryLimit = 500
)

// timelineEnvironments and timelineCategories are the closed parameter sets
// (timeline-design A3): a value outside them is refused by name rather than
// quietly ignored, because a filter that silently did nothing is how the Swift
// app's default view came to hide 100% of its own data.
var (
	timelineEnvironments = []string{"production", "staging", "preview", "development", "all"}
	timelineCategories   = []string{"feature", "operation"}
)

// githubRemote is a repository this daemon may build a commit link for. Only
// the shape the Swift app's `githubCommitURL` accepts, because a link built
// from a guess is a link to somebody else's repository.
var githubRemote = regexp.MustCompile(`^github:[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// timelineRoute is the page's one read. It writes nothing: the Swift app's
// `command` route had no caller here and the projection has no state to set.
func (s *Server) timelineRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET only")
		return
	}
	query := r.URL.Query()
	environment := valueOr(query.Get("environment"), "production")
	if !oneOfList(environment, timelineEnvironments) {
		writeRefusal(w, http.StatusBadRequest, "bad_environment",
			"environment must be one of "+strings.Join(timelineEnvironments, ", ")+".")
		return
	}
	category := strings.TrimSpace(query.Get("category"))
	if category != "" && !oneOfList(category, timelineCategories) {
		writeRefusal(w, http.StatusBadRequest, "bad_category",
			"category must be one of "+strings.Join(timelineCategories, ", ")+".")
		return
	}
	project := strings.TrimSpace(query.Get("project"))
	if project == "" {
		writeRefusal(w, http.StatusBadRequest, "project_required",
			"A timeline is one Project's; name it with ?project=.")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	records, unreadable, err := s.broker.Records(ctx)
	if err != nil {
		writeRefusal(w, http.StatusServiceUnavailable, "timeline_unreadable",
			"This daemon's own task records could not be read.")
		return
	}

	snapshot := buildTimeline(records, timelineQuery{
		project:     project,
		entry:       strings.TrimSpace(query.Get("entry")),
		cursor:      strings.TrimSpace(query.Get("cursor")),
		environment: environment,
		category:    category,
		// `upcoming` was the Swift app's default-off filter, and with no
		// deployment producer it hid every row there was (timeline-design
		// A6). Here it defaults on: a reader may still turn it off to ask the
		// narrower question, and a page that shows nothing answers nothing.
		upcoming: query.Get("upcoming") != "false",
		limit:    capacity.Default(capacity.TimelineEntries),
		page:     timelinePageLimit,
		// A record this daemon holds and cannot decode is a delivery whose
		// place on the timeline nobody can see. The answer says `partial`
		// rather than drawing a shorter list as a complete one (DG-7).
		partial: len(unreadable) > 0,
	})
	writeJSON(w, contract.TimelineEnvelope{Timeline: snapshot})
}

// timelineReading reports the fullest single Project now. The Timeline is a
// projection rather than a store, so this is what its registered capacity row
// would make the next read walk.
func (s *Server) timelineReading() (entries int64, err error) {
	if s.broker == nil {
		return 0, nil
	}
	records, _, err := s.broker.Records(context.Background())
	if err != nil {
		return 0, err
	}
	perProject := map[string]int64{}
	for _, rec := range records {
		if _, ok := timelineEntry(rec); ok {
			perProject[valueOr(rec.Repository, rec.ProjectDir)]++
		}
	}
	for _, n := range perProject {
		if n > entries {
			entries = n
		}
	}
	return entries, nil
}

type timelineQuery struct {
	project     string
	entry       string
	cursor      string
	environment string
	category    string
	upcoming    bool
	limit       int64
	page        int
	partial     bool
}

// buildTimeline is the whole snapshot, and a pure function of the records.
func buildTimeline(records []orchestrator.Record, q timelineQuery) contract.TimelineSnapshot {
	entries := make([]contract.TimelineEntry, 0, len(records))
	var newest time.Time
	// The Project's own name, taken from the first record that names it. The
	// page may have arrived holding `project-<digest>`, and a heading that
	// reads as a digest tells a person nothing about which Project they are
	// looking at.
	label := projectLabel(q.project)
	named := false
	for _, rec := range records {
		if !timelineNames(rec, q.project) {
			continue
		}
		if !named {
			if from := valueOr(rec.Repository, rec.ProjectDir); from != "" {
				if canonical, ok := projects.CanonicalProjectKey(from); ok {
					label, named = filepath.Base(canonical), true
				}
			}
		}
		entry, ok := timelineEntry(rec)
		if !ok {
			continue
		}
		if rec.FinishedAt.After(newest) {
			newest = rec.FinishedAt
		}
		entries = append(entries, entry)
	}
	// Newest first, and ties broken by id so that two deliveries recorded in
	// the same second cannot swap places between two reads and make a keyset
	// cursor skip one of them.
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entryAt(entries[i]), entryAt(entries[j])
		if a != b {
			return a > b
		}
		return entries[i].ID > entries[j].ID
	})

	held := int64(len(entries))
	history := contract.TimelineHistoryStatusUnknown
	if held > q.limit {
		// The bound is reached. Nothing is deleted — there is nothing to
		// delete — and the answer says so rather than going quiet.
		entries = entries[:q.limit]
		held = q.limit
		history = contract.TimelineHistoryStatusCapacity
	}

	shown := make([]contract.TimelineEntry, 0, len(entries))
	for _, e := range entries {
		if q.category != "" && string(e.PrimaryCategory) != q.category {
			continue
		}
		// `all` asks for every entry whatever its evidence; every other
		// environment asks about a deployment, and nothing here is deployed.
		if !q.upcoming && e.Projection.Status != contract.TimelineStatusLandedToGit {
			continue
		}
		if q.environment != "all" && q.environment != "production" && e.Projection.Status != contract.TimelineStatusLandedToGit {
			continue
		}
		shown = append(shown, e)
	}
	shown = afterCursor(shown, q.cursor)

	// A Project nothing has delivered in has no newest record, and the zero
	// time's Unix seconds is a number from the year 1 — a reading that looks
	// like a measurement. Nought is the honest counter for "nothing yet".
	var revision int64
	if !newest.IsZero() {
		revision = newest.Unix()
	}
	snapshot := contract.TimelineSnapshot{
		Revision: revision,
		Enabled:  true,
		Viewer:   contract.TimelineViewer{CanManage: false},
		Project:  contract.TimelineProject{ID: q.project, Label: label, Name: label},
		Capacity: contract.TimelineCapacity{EntryCount: held, EntryLimit: q.limit},
		Checkpoints: []contract.TimelineCheckpoint{
			{ProjectID: q.project, HistoryStatus: history},
		},
		Status:  "current",
		Entries: []contract.TimelineEntry{},
	}
	if q.partial {
		snapshot.Status = "partial"
	}
	if len(shown) > q.page {
		last := shown[q.page-1]
		snapshot.Entries = shown[:q.page]
		snapshot.NextCursor = strconv.FormatInt(entryAt(last), 10) + ":" + last.ID
	} else {
		snapshot.Entries = shown
	}
	if q.entry != "" {
		for i := range entries {
			if entries[i].ID == q.entry {
				one := entries[i]
				snapshot.Selected = &one
				break
			}
		}
	}
	return snapshot
}

// timelineNames is whether a record belongs to the Project asked for.
//
// **A Project has three spellings on this machine and the page may arrive with
// any of them.** The Projects page and the Board hand over `project-<digest>`
// (projects.ProjectID); a link or a script is likelier to carry the path; a
// person typing one carries the directory's name. A route that knew only the
// path answered the Board's own tab with an empty timeline, which reads as
// "nothing was delivered here" — the one sentence this page must not say by
// accident. So the record's repository and its dispatch directory are each
// compared in all three spellings.
func timelineNames(rec orchestrator.Record, project string) bool {
	for _, candidate := range []string{rec.Repository, rec.ProjectDir} {
		if candidate == "" {
			continue
		}
		if candidate == project || filepath.Base(candidate) == project {
			return true
		}
		canonical, ok := projects.CanonicalProjectKey(candidate)
		if !ok {
			continue
		}
		if canonical == project || filepath.Base(canonical) == project ||
			projects.ProjectID(canonical) == project {
			return true
		}
	}
	return false
}

// timelineEntry is one delivery, or false for a task that has not delivered:
// a task that is still running is not a thing that happened.
func timelineEntry(rec orchestrator.Record) (contract.TimelineEntry, bool) {
	delivered := rec.Result != nil && rec.Result.Status == "success"
	landed := rec.Landing != nil && (rec.Landing.State == orchestrator.LandingLanded ||
		rec.Landing.State == orchestrator.LandingIncorporated)
	if !delivered && !landed {
		return contract.TimelineEntry{}, false
	}

	entry := contract.TimelineEntry{
		ID:              rec.ID,
		ProjectID:       valueOr(rec.Repository, rec.ProjectDir),
		OriginalTitle:   rec.Title,
		PrimaryCategory: timelineCategory(rec),
		BoardItemIds:    []string{},
		SourceRevisions: []contract.TimelineRevision{},
		Events:          []contract.TimelineEvent{},
	}
	if rec.WorkID != "" {
		entry.BoardItemIds = append(entry.BoardItemIds, rec.WorkID)
	}
	if rec.Result != nil {
		entry.Summary = cut(rec.Result.Summary, timelineSummaryLimit)
	}

	observed := rec.FinishedAt
	if observed.IsZero() {
		observed = rec.CreatedAt
	}
	entry.Projection = contract.TimelineProjection{
		Status:     contract.TimelineStatusUpcoming,
		ObservedAt: observed.Unix(),
		// One target — this machine's checkout — and nothing proves anything
		// available on it, because nothing on this machine produces that
		// evidence. Zero of one is the honest reading; it is not a failure.
		RequiredTargets:  1,
		AvailableTargets: 0,
	}
	if !observed.IsZero() {
		at := observed.Unix()
		entry.Projection.EffectiveAt = at
	}
	if delivered {
		entry.Events = append(entry.Events, contract.TimelineEvent{
			Kind: "delivery_recorded", Authority: "broker", Result: rec.Result.Status,
			ObservedAt: observed.Unix(), EffectiveAt: observed.Unix(),
		})
	}
	if landed {
		at := rec.Landing.At
		if at.IsZero() {
			at = observed
		}
		entry.Projection.Status = contract.TimelineStatusLandedToGit
		entry.Projection.EffectiveAt = at.Unix()
		entry.Projection.ObservedAt = at.Unix()
		entry.Events = append(entry.Events, contract.TimelineEvent{
			Kind: "landed_to_git", Authority: "broker", Result: string(rec.Landing.State),
			ObservedAt: at.Unix(), EffectiveAt: at.Unix(),
		})
		if rev, ok := timelineRevision(rec); ok {
			entry.SourceRevisions = append(entry.SourceRevisions, rev)
		}
	}
	return entry, true
}

// timelineRevision is the commit a landing proved, as a revision the card can
// link. The repository id is the landing's own `repo`, which is a path here
// rather than a forge name, so a GitHub link is built only for a record that
// spells one.
func timelineRevision(rec orchestrator.Record) (contract.TimelineRevision, bool) {
	commit := strings.TrimSpace(rec.Landing.Commit)
	if commit == "" {
		return contract.TimelineRevision{}, false
	}
	short := commit
	if len(short) > 8 {
		short = short[:8]
	}
	repo := valueOr(rec.Landing.Repo, rec.Repository)
	rev := contract.TimelineRevision{RepositoryID: repo, Commit: commit, ShortCommit: short}
	if githubRemote.MatchString(repo) {
		rev.GithubUrl = "https://github.com/" + strings.TrimPrefix(repo, "github:") +
			"/commit/" + strings.ToLower(commit)
	}
	return rev, true
}

// timelineCategory is what kind of work a delivery was, from what it declared
// it would write. A task whose whole write set is tooling or documentation is
// an operation; everything else is a Feature, which is the assumption that
// overstates nothing.
func timelineCategory(rec orchestrator.Record) contract.TimelineCategory {
	if len(rec.Claims) == 0 {
		return contract.TimelineCategoryFeature
	}
	for _, claim := range rec.Claims {
		head := strings.SplitN(strings.TrimPrefix(claim, "./"), "/", 2)[0]
		if head != "docs" && head != "tools" && head != "skills" {
			return contract.TimelineCategoryFeature
		}
	}
	return contract.TimelineCategoryOperation
}

// afterCursor is the keyset page: everything strictly older than the cursor.
// An unreadable cursor starts at the beginning rather than refusing, because a
// cursor is the page's own bookkeeping and a reader cannot correct one.
func afterCursor(entries []contract.TimelineEntry, cursor string) []contract.TimelineEntry {
	if cursor == "" {
		return entries
	}
	at, id, ok := strings.Cut(cursor, ":")
	seconds, err := strconv.ParseInt(at, 10, 64)
	if !ok || err != nil {
		return entries
	}
	for i, e := range entries {
		if entryAt(e) < seconds || (entryAt(e) == seconds && e.ID < id) {
			return entries[i:]
		}
	}
	return []contract.TimelineEntry{}
}

func entryAt(e contract.TimelineEntry) int64 {
	if e.Projection.EffectiveAt != 0 {
		return e.Projection.EffectiveAt
	}
	return e.Projection.ObservedAt
}

func projectLabel(project string) string {
	if strings.HasPrefix(project, "/") {
		return filepath.Base(project)
	}
	return project
}

func cut(text string, max int) string {
	text = strings.TrimSpace(text)
	if len(text) <= max {
		return text
	}
	return strings.TrimSpace(text[:max]) + "…"
}

func valueOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func oneOfList(value string, allowed []string) bool {
	for _, a := range allowed {
		if a == value {
			return true
		}
	}
	return false
}
