package http

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/analytics"
	"github.com/sainteye/clawdline-go/internal/app/orchestrator"
	"github.com/sainteye/clawdline-go/internal/contract"
	"github.com/sainteye/clawdline-go/internal/domain/capacity"
)

// The verification ledger: GET /v1/orchestrator/usage/verification-ledger.
//
// One question asked per Feature — what did reviewing this find, what did
// proving it cost, and how did its tokens divide between building the work and
// reading it. The Swift app answered it out of four receipt tables; this
// daemon answers it out of records it already keeps, and nothing here writes
// anything down:
//
//   - the Feature is a task graph (`graph` in task.json, orchestrator/graphs.go),
//     and its label is the graph's destination;
//   - the findings are the review receipts a child writes into `result.review`,
//     whose shape taskdir/finish.go already refuses to accept unless it is the
//     closed `verdict` + three axes + `id/severity/summary/evidence` schema;
//   - the verification is the child's own `result.verification`;
//   - the tokens are the usage analytics rows (internal/adapters/analytics),
//     joined to the tasks by task id.
//
// **The screen's subject is not a number, it is which of three answers a number
// is.** Every figure leaves here as `present` with a figure, `absent` — no
// record — or `unknown` — not measurable. None of the branches below can put a
// `0` on screen for the last two, and that is the whole of why the route
// exists: a page drawing "nobody recorded it" as "it cost nothing" is the
// defect, printed larger.
//
// **A fourth word for a fourth gap.** `undeclared` is a row whose task named no
// side of the work. It is a token bucket of its own and is never added to the
// implementation figure beside it, because calling it implementation is a claim
// about a side that nothing on the row supports.

// What one ledger read may reach. Both are registered rows
// (capacity.LedgerScan, capacity.LedgerFeatures): the tasks this daemon holds
// grow with every dispatch and the store bounds only their bytes, so the read
// over them is bounded here and the answer says where it stopped
// (docs/limits.md §3.3, design-decisions D27).
const (
	// ledgerTaskLimit is the most task records one read counts, newest first.
	ledgerTaskLimit = 4_000
	// ledgerFeatureLimit is the most Features one answer lists. The count of
	// what was found is sent beside it, so a Mac past the cap can tell.
	ledgerFeatureLimit = 200
)

// ledgerScanSince is how far back the token rows are read. The tasks decide
// what is on the page; this only decides which rows can be joined to them, and
// a window is what keeps the transcript scan from walking a year of records to
// answer a page about this month's Features.
const ledgerScanSince = 120 * 24 * time.Hour

// ledgerRoute is the page's one read. `graph` asks for a single Feature.
func (s *Server) ledgerRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET only")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	records, _, err := s.broker.Records(ctx)
	if err != nil {
		writeRefusal(w, http.StatusServiceUnavailable, "ledger_unreadable",
			"This daemon's own task records could not be read.")
		return
	}
	rows, _, ok := s.analyticsRows(w, r, time.Now().Add(-ledgerScanSince))
	if !ok {
		return
	}

	ledger := buildLedger(records, rows, capacity.Default(capacity.LedgerScan),
		capacity.Default(capacity.LedgerFeatures))
	if want := strings.TrimSpace(r.URL.Query().Get("graph")); want != "" {
		one := findLedgerFeature(ledger, want)
		if one == nil {
			writeRefusal(w, http.StatusNotFound, "graph_not_found",
				"No stored receipt or interval names that Feature.")
			return
		}
		ledger.Feature = one
		ledger.Features = []contract.LedgerFeature{}
		ledger.Unattributed = nil
	}
	writeJSON(w, contract.VerificationLedgerEnvelope{VerificationLedger: ledger})
}

// findLedgerFeature is the one-Feature read: the same card the list draws, with
// the findings themselves under it.
func findLedgerFeature(l contract.VerificationLedger, want string) *contract.LedgerFeature {
	for i := range l.Features {
		if l.Features[i].GraphID != nil && *l.Features[i].GraphID == want {
			one := l.Features[i]
			return &one
		}
	}
	return nil
}

// feature is one Feature while it is being gathered.
type ledgerBuild struct {
	graphID   string
	label     string
	tasks     map[string]bool
	lastSeen  time.Time
	severity  map[string]int64
	verdicts  map[string]int64
	axes      map[string]*contract.LedgerAxis
	items     []contract.LedgerFinding
	reviews   int64
	findings  int64
	runs      int64
	seconds   int64
	endedRed  int64
	receipts  int64
	tokens    map[string]*tokenBucket
	rowsSeen  int64
	axisOrder []string
}

// tokenBucket accumulates one role's rows. `measured` is what the rows that
// measured anything add up to; `incomplete` is how many measured only part of
// themselves, which is what turns a total into a floor.
type tokenBucket struct {
	rows       int64
	measured   int64
	measuring  int64
	incomplete int64
}

// severityOrder is worst first, which is the order the page draws and the
// order a reader needs.
var severityOrder = map[string]int{"blocking": 0, "important": 1, "minor": 2}

// reviewReceipt is `result.review` as taskdir/finish.go admits it.
type reviewReceipt struct {
	Verdict string `json:"verdict"`
	Axes    []struct {
		Axis     string `json:"axis"`
		Status   string `json:"status"`
		Findings []struct {
			ID       string   `json:"id"`
			Severity string   `json:"severity"`
			Summary  string   `json:"summary"`
			Evidence []string `json:"evidence"`
		} `json:"findings"`
	} `json:"axes"`
}

// buildLedger is the whole answer, and is a pure function of the records so
// that a test drives it without a server.
func buildLedger(records []orchestrator.Record, rows []analytics.Row, taskLimit, featureLimit int64) contract.VerificationLedger {
	sort.SliceStable(records, func(i, j int) bool { return records[i].CreatedAt.After(records[j].CreatedAt) })
	truncated := int64(len(records)) > taskLimit
	if truncated {
		records = records[:taskLimit]
	}

	// Which Feature each task is on, and which side of the work it was. A task
	// with no graph names no Feature and declares no side, which are two
	// different silences and are kept apart: the first puts its rows in the
	// unattributed block, the second in the `undeclared` bucket.
	feature := map[string]string{}
	role := map[string]string{}
	builds := map[string]*ledgerBuild{}
	loose := newLedgerBuild("")

	for _, rec := range records {
		build := loose
		if rec.Graph != nil && rec.Graph.ID != "" {
			feature[rec.ID] = rec.Graph.ID
			build = builds[rec.Graph.ID]
			if build == nil {
				build = newLedgerBuild(rec.Graph.ID)
				builds[rec.Graph.ID] = build
			}
			if build.label == "" {
				build.label = rec.Graph.Destination
			}
			role[rec.ID] = graphNodeRole(rec.Graph)
		}
		build.tasks[rec.ID] = true
		if rec.FinishedAt.After(build.lastSeen) {
			build.lastSeen = rec.FinishedAt
		} else if rec.CreatedAt.After(build.lastSeen) {
			build.lastSeen = rec.CreatedAt
		}
		if rec.Result == nil {
			continue
		}
		if len(rec.Result.Review) > 0 {
			addReview(build, rec.Result.Review)
		}
		if v := rec.Result.Verify; v != nil {
			build.receipts++
			build.runs += int64(v.Runs)
			build.seconds += int64(v.Seconds)
			// `last` is the child's own word for how its last run ended. Only
			// `pass` is green; anything else — including a word this build has
			// never seen — is not, because a run nobody can classify is not a
			// run that passed.
			if strings.TrimSpace(v.Last) != "pass" {
				build.endedRed++
			}
		}
	}

	for _, row := range rows {
		build := loose
		if id := feature[row.TaskID]; id != "" && builds[id] != nil {
			build = builds[id]
		}
		build.rowsSeen++
		bucket := build.tokens[roleOf(role, row.TaskID)]
		bucket.rows++
		total, complete := rowTokens(row)
		if total == nil {
			continue
		}
		bucket.measuring++
		bucket.measured += *total
		if !complete {
			bucket.incomplete++
		}
	}

	found := int64(len(builds))
	ordered := make([]*ledgerBuild, 0, len(builds))
	for _, b := range builds {
		ordered = append(ordered, b)
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if !ordered[i].lastSeen.Equal(ordered[j].lastSeen) {
			return ordered[i].lastSeen.After(ordered[j].lastSeen)
		}
		return ordered[i].graphID < ordered[j].graphID
	})
	if int64(len(ordered)) > featureLimit {
		ordered = ordered[:featureLimit]
	}

	out := contract.VerificationLedger{
		Features: make([]contract.LedgerFeature, 0, len(ordered)),
		Read: contract.LedgerRead{
			RowsScanned:    int64(len(rows)),
			FeaturesFound:  found,
			FeaturesListed: int64(len(ordered)),
			Truncated:      truncated,
			// One scan reads the tasks and their receipts together, so a cut
			// list of tasks is a cut list of receipts and there is no second
			// ceiling to report. Sending it as its own field keeps the page
			// able to say so the day the two part company.
			ReceiptsTruncated: truncated,
		},
	}
	for _, b := range ordered {
		out.Features = append(out.Features, b.card())
	}
	if loose.rowsSeen > 0 || loose.reviews > 0 || loose.receipts > 0 {
		card := loose.card()
		out.Unattributed = &card
	}
	return out
}

func newLedgerBuild(id string) *ledgerBuild {
	b := &ledgerBuild{
		graphID:  id,
		tasks:    map[string]bool{},
		severity: map[string]int64{},
		verdicts: map[string]int64{},
		axes:     map[string]*contract.LedgerAxis{},
		tokens:   map[string]*tokenBucket{},
	}
	for _, role := range []string{"implementation", "review", "undeclared"} {
		b.tokens[role] = &tokenBucket{}
	}
	return b
}

// graphNodeRole is which side of the work a task was on, from the kind of the
// graph node it is. A graph that names no current node declares no side.
func graphNodeRole(g *orchestrator.Graph) string {
	for _, n := range g.Nodes {
		if n.ID != g.CurrentNode {
			continue
		}
		if n.Kind == "review" {
			return "review"
		}
		return "implementation"
	}
	return ""
}

// roleOf is the bucket a row belongs in. A row whose task is not among the
// records read — swept, or older than the scan reached — declared no side
// either, and is counted as undeclared rather than assumed.
func roleOf(role map[string]string, taskID string) string {
	if r := role[taskID]; r != "" {
		return r
	}
	return "undeclared"
}

// rowTokens is one interval row's spend, and whether it measured all four of
// its parts. A row that measured none is not a zero: it is a row with nothing
// to add, and the caller counts it without adding it.
func rowTokens(row analytics.Row) (*int64, bool) {
	var sum int64
	measured, complete := false, true
	for _, part := range row.Tokens {
		if part == nil {
			complete = false
			continue
		}
		measured = true
		sum += *part
	}
	if !measured {
		return nil, false
	}
	return &sum, complete
}

// addReview folds one review receipt into a Feature. A receipt this build
// cannot decode is counted as a receipt and contributes no findings: it is
// evidence that a review happened, and nothing more may be read from it.
func addReview(b *ledgerBuild, raw json.RawMessage) {
	var receipt reviewReceipt
	if json.Unmarshal(raw, &receipt) != nil {
		b.reviews++
		return
	}
	b.reviews++
	if receipt.Verdict != "" {
		b.verdicts[receipt.Verdict]++
	}
	for _, axis := range receipt.Axes {
		seen := b.axes[axis.Axis]
		if seen == nil {
			seen = &contract.LedgerAxis{Axis: axis.Axis, Status: axis.Status}
			b.axes[axis.Axis] = seen
			b.axisOrder = append(b.axisOrder, axis.Axis)
		}
		// One axis read twice is `findings` if either reading found any: a
		// pass that a later review contradicted is not a pass.
		if axis.Status == "findings" {
			seen.Status = "findings"
		}
		for _, f := range axis.Findings {
			seen.FindingCount++
			b.findings++
			b.severity[f.Severity]++
			b.items = append(b.items, contract.LedgerFinding{
				FindingID: f.ID, Severity: f.Severity, Summary: f.Summary,
				Evidence: append([]string{}, f.Evidence...),
			})
		}
	}
}

// card is one Feature as the page draws it.
func (b *ledgerBuild) card() contract.LedgerFeature {
	card := contract.LedgerFeature{
		Tasks:        int64(len(b.tasks)),
		Rows:         b.rowsSeen,
		Findings:     b.findingsReading(),
		Verification: b.verificationReading(),
		Tokens: contract.LedgerTokens{
			Implementation: b.tokens["implementation"].reading(),
			Review:         b.tokens["review"].reading(),
			Undeclared:     b.tokens["undeclared"].reading(),
		},
		Verdicts: []contract.LedgerVerdict{},
		Axes:     []contract.LedgerAxis{},
		Items:    b.items,
	}
	if b.graphID != "" {
		id := b.graphID
		card.GraphID = &id
	}
	if b.label != "" {
		label := b.label
		card.Label = &label
	}
	if !b.lastSeen.IsZero() {
		at := b.lastSeen.UTC().Format(time.RFC3339)
		card.LastSeenAt = &at
	}
	if card.Items == nil {
		card.Items = []contract.LedgerFinding{}
	}
	for verdict, count := range b.verdicts {
		card.Verdicts = append(card.Verdicts, contract.LedgerVerdict{Verdict: verdict, Count: count})
	}
	sort.SliceStable(card.Verdicts, func(i, j int) bool { return card.Verdicts[i].Verdict < card.Verdicts[j].Verdict })
	for _, name := range b.axisOrder {
		card.Axes = append(card.Axes, *b.axes[name])
	}
	return card
}

// findingsReading is the one place a finding count becomes a state. No review
// receipt is `absent` — this Mac holds none, which is not the same as nobody
// having reviewed it, and the page says so in those words.
func (b *ledgerBuild) findingsReading() contract.LedgerFindings {
	out := contract.LedgerFindings{
		State:          contract.LedgerStateAbsent,
		ReviewReceipts: b.reviews,
		Severities:     []contract.LedgerSeverity{},
	}
	if b.reviews == 0 {
		return out
	}
	out.State = contract.LedgerStatePresent
	total := b.findings
	out.Total = &total
	for severity, count := range b.severity {
		out.Severities = append(out.Severities, contract.LedgerSeverity{Severity: severity, Count: count})
	}
	sort.SliceStable(out.Severities, func(i, j int) bool {
		a, ok := severityOrder[out.Severities[i].Severity]
		c, ok2 := severityOrder[out.Severities[j].Severity]
		if ok != ok2 {
			return ok
		}
		if a != c {
			return a < c
		}
		return out.Severities[i].Severity < out.Severities[j].Severity
	})
	return out
}

func (b *ledgerBuild) verificationReading() contract.LedgerVerification {
	if b.receipts == 0 {
		return contract.LedgerVerification{State: contract.LedgerStateAbsent}
	}
	return contract.LedgerVerification{
		State: contract.LedgerStatePresent, Receipts: b.receipts,
		Runs: b.runs, Seconds: b.seconds, EndedRed: b.endedRed,
	}
}

// reading turns one bucket into the three answers.
//
// No rows is `absent`. Rows and nothing measured is `unknown` — spending
// nobody can count, never zero. Rows where some measured only part of
// themselves is `present` with a null total, which the page draws as a floor.
func (t *tokenBucket) reading() contract.LedgerTokenReading {
	out := contract.LedgerTokenReading{
		State: contract.LedgerStateAbsent, Rows: t.rows,
		Measured: t.measured, IncompleteRows: t.incomplete,
	}
	switch {
	case t.rows == 0:
		return out
	case t.measuring == 0:
		out.State = contract.LedgerStateUnknown
		return out
	}
	out.State = contract.LedgerStatePresent
	if t.incomplete == 0 {
		total := t.measured
		out.Total = &total
	}
	return out
}

// What the two projection rows are holding now (diagnostics.go).
//
// Neither is a store, so "used" is what the next read would walk: the task
// records for `ledger.scan`, the Features among them for `ledger.features`,
// and the fullest single Project for `timeline.entries`. Measuring them is one
// read of records this daemon already holds, on the capacity beat's hour.
func (s *Server) historyReadings() (tasks, features, entries int64, err error) {
	if s.broker == nil {
		// No broker is no task records, which is a read of nought and not a
		// read that failed: this daemon dispatches nothing in that shape.
		return 0, 0, 0, nil
	}
	records, _, err := s.broker.Records(context.Background())
	if err != nil {
		return 0, 0, 0, err
	}
	graphs := map[string]bool{}
	perProject := map[string]int64{}
	for _, rec := range records {
		if rec.Graph != nil && rec.Graph.ID != "" {
			graphs[rec.Graph.ID] = true
		}
		if _, ok := timelineEntry(rec); ok {
			perProject[valueOr(rec.Repository, rec.ProjectDir)]++
		}
	}
	for _, n := range perProject {
		if n > entries {
			entries = n
		}
	}
	return int64(len(records)), int64(len(graphs)), entries, nil
}
