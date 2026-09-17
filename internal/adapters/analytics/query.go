package analytics

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Limits, as `UsageQueryService` has them.
const (
	MaxPageSize    = 200
	MaxScannedRows = 100_000
	staleAfter     = 600 // seconds: twice the ledger's checkpoint interval
)

var groupNames = []string{"model", "assistant", "origin", "project", "day", "coverage", "task"}

// Query is one parsed request.
type Query struct {
	From, To   string // local days, "" for open
	Zone       string
	Loc        *time.Location
	Group      string
	Bucket     string
	View       string
	Limit      int
	Cursor     *Cursor
	Assistant  string
	Model      string
	Origin     string
	Project    string
	start, end time.Time // zero for open
}

// Cursor is where a page continues: after the row started at At with key Key.
type Cursor struct {
	Key string  `json:"key"`
	At  float64 `json:"at"`
}

var localDay = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// Parse is `UsageQueryService.parse`. The error text is the Swift app's.
func Parse(values url.Values) (Query, error) {
	allowed := map[string]bool{"from": true, "to": true, "timezone": true, "group": true, "bucket": true,
		"assistant": true, "model": true, "origin": true, "project": true, "view": true, "limit": true, "cursor": true}
	unknown := []string{}
	for k, v := range values {
		if !allowed[k] {
			unknown = append(unknown, k)
			continue
		}
		if len(v) > 1 {
			return Query{}, fmt.Errorf("Usage query fields may appear only once.")
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return Query{}, fmt.Errorf("Unknown usage query field: %s.", strings.Join(unknown, ", "))
	}
	q := Query{Group: "model", Bucket: "day", View: "overview", Limit: 50}
	zone, loc, err := zoneFor(values.Get("timezone"))
	if err != nil {
		return Query{}, err
	}
	q.Zone, q.Loc = zone, loc
	q.From, q.To = values.Get("from"), values.Get("to")
	for _, d := range []string{q.From, q.To} {
		if d == "" {
			continue
		}
		if !validDay(d) {
			return Query{}, fmt.Errorf("from and to are local dates, YYYY-MM-DD.")
		}
	}
	if q.From != "" && q.To != "" && q.From > q.To {
		return Query{}, fmt.Errorf("from must not be after to.")
	}
	if g := values.Get("group"); g != "" {
		if !contains(groupNames, g) {
			return Query{}, fmt.Errorf("group must be one of model, assistant, origin, project, day, coverage, task.")
		}
		q.Group = g
	}
	if b := values.Get("bucket"); b != "" {
		if b != "day" && b != "week" && b != "month" {
			return Query{}, fmt.Errorf("bucket must be day, week or month.")
		}
		q.Bucket = b
	}
	if v := values.Get("view"); v != "" {
		if v != "overview" && v != "agent_work" {
			return Query{}, fmt.Errorf("view must be overview or agent_work.")
		}
		q.View = v
	}
	if raw, ok := values["limit"]; ok {
		n, err := strconv.Atoi(raw[0])
		if err != nil {
			return Query{}, fmt.Errorf("limit must be an integer between 1 and 200.")
		}
		if n < 1 || n > MaxPageSize {
			return Query{}, fmt.Errorf("limit must be between 1 and 200.")
		}
		q.Limit = n
	}
	if c := values.Get("cursor"); c != "" {
		cur, ok := decodeCursor(c)
		if !ok {
			return Query{}, fmt.Errorf("cursor is not a usage continuation issued by this service.")
		}
		q.Cursor = &cur
	}
	q.Assistant = strings.TrimSpace(values.Get("assistant"))
	q.Model = strings.TrimSpace(values.Get("model"))
	q.Origin = strings.TrimSpace(values.Get("origin"))
	q.Project = strings.TrimSpace(values.Get("project"))
	if q.From != "" {
		q.start = dayStart(q.From, q.Loc)
	}
	if q.To != "" {
		q.end = dayStart(q.To, q.Loc).AddDate(0, 0, 1)
	}
	return q, nil
}

// ScanFrom is the earliest write that can matter to this query, the equal
// previous range included. Zero means everything.
func (q Query) ScanFrom() time.Time {
	if q.start.IsZero() {
		return time.Time{}
	}
	if prev, ok := q.previous(); ok {
		return prev.start
	}
	return q.start
}

func zoneFor(name string) (string, *time.Location, error) {
	if name == "" {
		name = SystemZone()
	}
	loc, err := time.LoadLocation(name)
	if err != nil || name == "Local" {
		return "", nil, fmt.Errorf("timezone must be an IANA timezone identifier.")
	}
	return name, loc, nil
}

// SystemZone is this machine's IANA zone name, as the Swift app's
// `TimeZone.current.identifier` would give it.
func SystemZone() string {
	if tz := os.Getenv("TZ"); tz != "" {
		return strings.TrimPrefix(tz, ":")
	}
	if link, err := os.Readlink("/etc/localtime"); err == nil {
		if _, rest, ok := strings.Cut(link, "zoneinfo/"); ok {
			return rest
		}
	}
	return "UTC"
}

func validDay(s string) bool {
	if !localDay.MatchString(s) {
		return false
	}
	t, err := time.Parse("2006-01-02", s)
	return err == nil && t.Format("2006-01-02") == s
}

func dayStart(day string, loc *time.Location) time.Time {
	t, _ := time.ParseInLocation("2006-01-02", day, loc)
	return t
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func encodeCursor(c Cursor) string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCursor(s string) (Cursor, bool) {
	for _, r := range s {
		if !(r == '-' || r == '_' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')) {
			return Cursor{}, false
		}
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(s, "="))
	if err != nil {
		return Cursor{}, false
	}
	var c Cursor
	if json.Unmarshal(b, &c) != nil || c.Key == "" || math.IsNaN(c.At) || math.IsInf(c.At, 0) {
		return Cursor{}, false
	}
	return c, true
}

type window struct {
	start, end time.Time
	from, to   string
}

// previous is the equal range before this one, when this one is closed.
func (q Query) previous() (window, bool) {
	if q.start.IsZero() || q.end.IsZero() {
		return window{}, false
	}
	days := 0
	for d := q.start; d.Before(q.end); d = d.AddDate(0, 0, 1) {
		days++
	}
	start := q.start.AddDate(0, 0, -days)
	return window{
		start: start,
		end:   q.start,
		from:  start.Format("2006-01-02"),
		to:    q.start.AddDate(0, 0, -1).Format("2006-01-02"),
	}, true
}

func (q Query) matches(r Row, start, end time.Time) bool {
	if !start.IsZero() && r.StartedAt.Before(start) {
		return false
	}
	if !end.IsZero() && !r.StartedAt.Before(end) {
		return false
	}
	if q.Assistant != "" && r.Assistant != q.Assistant {
		return false
	}
	if q.Model != "" && r.Model != q.Model {
		return false
	}
	if q.Origin != "" && r.Origin != q.Origin {
		return false
	}
	if q.Project != "" {
		name := projectName(r)
		if name == nil || *name != q.Project {
			return false
		}
	}
	return true
}

func (q Query) filter(all []Row, start, end time.Time) ([]Row, bool) {
	out := []Row{}
	for _, r := range all {
		if q.matches(r, start, end) {
			out = append(out, r)
		}
	}
	sortNewestFirst(out)
	truncated := len(out) > MaxScannedRows
	if truncated {
		out = out[:MaxScannedRows]
	}
	return out, truncated
}

// Result is one answered query.
type Result struct {
	Query       Query
	All         []Row // everything collected, for freshness
	Rows        []Row // the matched rows, newest first
	Previous    []Row
	Truncated   bool
	PrevTrunc   bool
	GeneratedAt time.Time
}

// Run answers a query over the collected rows.
func Run(q Query, all []Row, now time.Time) Result {
	res := Result{Query: q, All: all, GeneratedAt: now}
	res.Rows, res.Truncated = q.filter(all, q.start, q.end)
	if prev, ok := q.previous(); ok {
		res.Previous, res.PrevTrunc = q.filter(all, prev.start, prev.end)
	}
	return res
}

type obj = map[string]any

func iso(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05Z") }

func isoOrNil(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return iso(t)
}

func strOrNil(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func intOrNil(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

// ---------- measurement ----------

type measure struct {
	measured     int64
	unknownParts []string
	total        *int64
	unknown      bool
	incomplete   bool
	reasons      []string
}

func measureRow(r Row) measure {
	m := measure{unknownParts: []string{}}
	known := 0
	for i, p := range r.Tokens {
		if p == nil {
			m.unknownParts = append(m.unknownParts, partNames[i])
			continue
		}
		known++
		m.measured += *p
	}
	if known == 4 {
		t := m.measured
		m.total = &t
	}
	m.unknown = known == 0
	m.incomplete = known < 4
	m.reasons = append([]string{}, r.CoverageReasons...)
	if strings.HasPrefix(r.SessionID, "unresolved-session:") && !contains(m.reasons, "session_unresolved") {
		m.reasons = append(m.reasons, "session_unresolved")
	}
	return m
}

func output(r Row) *int64 { return r.Tokens[partOutput] }

func tokensObj(sums [4]*int64) obj {
	o := obj{}
	for i, n := range partNames {
		o[n] = intOrNil(sums[i])
	}
	return o
}

func sumTokens(rows []Row) [4]*int64 {
	var out [4]*int64
	for _, r := range rows {
		for i, p := range r.Tokens {
			if p == nil {
				continue
			}
			if out[i] == nil {
				v := int64(0)
				out[i] = &v
			}
			*out[i] += *p
		}
	}
	return out
}

func partsUnknown(rows []Row) obj {
	o := obj{}
	for _, r := range rows {
		for i, p := range r.Tokens {
			if p == nil {
				n, _ := o[partNames[i]].(int)
				o[partNames[i]] = n + 1
			}
		}
	}
	return o
}

type costSeries struct {
	unit, basis string
	value       float64
	rows        int
	snapshots   map[string]bool
}

func costsOf(rows []Row) []*costSeries {
	by := map[string]*costSeries{}
	for _, r := range rows {
		if r.CostValue == nil || r.CostUnit == "" {
			continue
		}
		k := r.CostUnit + "\x00" + r.CostBasis
		s, ok := by[k]
		if !ok {
			s = &costSeries{unit: r.CostUnit, basis: r.CostBasis, snapshots: map[string]bool{}}
			by[k] = s
		}
		s.value += *r.CostValue
		s.rows++
		if r.PriceSnapshotID != "" {
			s.snapshots[r.PriceSnapshotID] = true
		}
	}
	out := make([]*costSeries, 0, len(by))
	for _, s := range by {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].unit != out[j].unit {
			return out[i].unit < out[j].unit
		}
		return out[i].basis < out[j].basis
	})
	return out
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func coverageObj(rows []Row) obj {
	states, reasons := obj{}, obj{}
	unknownRows := 0
	for _, r := range rows {
		m := measureRow(r)
		n, _ := states[r.Coverage].(int)
		states[r.Coverage] = n + 1
		for _, why := range m.reasons {
			n, _ := reasons[why].(int)
			reasons[why] = n + 1
		}
		if m.incomplete {
			unknownRows++
		}
	}
	return obj{"states": states, "reasons": reasons, "tokenRowsUnknown": unknownRows,
		"tokenPartsUnknown": partsUnknown(rows)}
}

// summary is `UsageQueryService.summary`.
func summary(rows []Row) obj {
	var floor, strict int64
	anyFloor := false
	strictOK := len(rows) > 0
	unknownRows := 0
	origins := obj{}
	unavailable := obj{}
	unavailableRows := 0
	scheduled := map[string]bool{}
	for _, r := range rows {
		m := measureRow(r)
		if !m.unknown {
			floor += m.measured
			anyFloor = true
		}
		if m.incomplete {
			unknownRows++
			strictOK = false
		} else {
			strict += *m.total
		}
		n, _ := origins[r.Origin].(int)
		origins[r.Origin] = n + 1
		if r.CostValue == nil || r.CostUnit == "" {
			unavailableRows++
			why := r.MissingCost
			if why == "" {
				why = "no_cost_recorded"
			}
			n, _ := unavailable[why].(int)
			unavailable[why] = n + 1
		}
		if r.Origin == "schedule" {
			id := r.TaskID
			if id == "" {
				id = r.IntervalKey
			}
			scheduled[id] = true
		}
	}
	costs := []obj{}
	for _, s := range costsOf(rows) {
		costs = append(costs, obj{"unit": s.unit, "basis": s.basis, "value": s.value, "rows": s.rows,
			"priceSnapshotIds": sortedKeys(s.snapshots)})
	}
	out := obj{
		"rows":              len(rows),
		"tokens":            tokensObj(sumTokens(rows)),
		"tokenPartsUnknown": partsUnknown(rows),
		"tokenRowsUnknown":  unknownRows,
		"measuredFloor":     nil,
		"strictTotal":       nil,
		"costs":             costs,
		"unavailableCost":   obj{"rows": unavailableRows, "reasons": unavailable},
		"origins":           origins,
		"scheduledRuns":     len(scheduled),
		"coverage":          coverageObj(rows),
	}
	if anyFloor {
		out["measuredFloor"] = floor
	}
	if strictOK {
		out["strictTotal"] = strict
	}
	return out
}

// ---------- rows on the wire ----------

// projectName is the label a row's Project goes by: the last component of its
// key, and nothing for a key that is missing or a retired managed worktree.
func projectName(r Row) *string {
	key := strings.TrimSpace(r.ProjectKey)
	if key == "" || LegacyWorktreeKey(key) {
		return nil
	}
	name := filepath.Base(r.ProjectKey)
	if name == "" || name == "." || name == "/" {
		return nil
	}
	return &name
}

func publicRow(r Row) obj {
	m := measureRow(r)
	var cost any
	if r.CostValue != nil && r.CostUnit != "" {
		cost = obj{"value": *r.CostValue, "unit": r.CostUnit, "basis": r.CostBasis,
			"priceSnapshotId": strOrNil(r.PriceSnapshotID)}
	}
	var floor any
	if !m.unknown {
		floor = m.measured
	}
	var project any
	if p := projectName(r); p != nil {
		project = *p
	}
	var verified any
	if r.LandingVerified != nil {
		verified = *r.LandingVerified
	}
	return obj{
		"id":                r.IntervalKey,
		"taskId":            strOrNil(r.TaskID),
		"scheduleId":        strOrNil(r.ScheduleID),
		"startedAt":         iso(r.StartedAt),
		"endedAt":           isoOrNil(r.EndedAt),
		"assistant":         r.Assistant,
		"model":             strOrNil(r.Model),
		"origin":            r.Origin,
		"project":           project,
		"tokens":            tokensObj(r.Tokens),
		"strictTotal":       intOrNil(m.total),
		"measuredFloor":     floor,
		"unknownTokenParts": m.unknownParts,
		"sourceTotal":       intOrNil(r.SourceTotal),
		"cost":              cost,
		"missingCostReason": strOrNil(r.MissingCost),
		"coverage":          r.Coverage,
		"coverageReasons":   m.reasons,
		"reconciliation":    nil,
		"inputBasis":        strOrNil(r.InputBasis),
		"lineage": obj{
			"graphId":         nil,
			"parentTaskId":    strOrNil(r.ParentTaskID),
			"retryOf":         nil,
			"attempt":         nil,
			"landingState":    strOrNil(r.LandingState),
			"landingVerified": verified,
			"disposition":     nil,
		},
	}
}

// ---------- grouping ----------

func groupKey(r Row, group string, loc *time.Location) *string {
	var s string
	switch group {
	case "model":
		s = r.Model
	case "assistant":
		s = r.Assistant
	case "origin":
		s = r.Origin
	case "project":
		return projectName(r)
	case "day":
		s = r.StartedAt.In(loc).Format("2006-01-02")
	case "coverage":
		s = r.Coverage
	case "task":
		s = r.TaskID
	}
	if s == "" {
		return nil
	}
	return &s
}

func outputRank(p any) int64 {
	if v, ok := p.(int64); ok {
		return v
	}
	return -1
}

func breakdown(rows []Row, group string, loc *time.Location) []obj {
	order := []string{}
	keys := map[string]*string{}
	by := map[string][]Row{}
	for _, r := range rows {
		k := groupKey(r, group, loc)
		id := "\x00nil"
		if k != nil {
			id = *k
		}
		if _, ok := by[id]; !ok {
			order = append(order, id)
			keys[id] = k
		}
		by[id] = append(by[id], r)
	}
	out := make([]obj, 0, len(order))
	for _, id := range order {
		s := summary(by[id])
		if k := keys[id]; k != nil {
			s["key"] = *k
		} else {
			s["key"] = nil
		}
		out = append(out, s)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a := outputRank(out[i]["tokens"].(obj)["output"])
		b := outputRank(out[j]["tokens"].(obj)["output"])
		if a != b {
			return a > b
		}
		ka, _ := out[i]["key"].(string)
		kb, _ := out[j]["key"].(string)
		return ka < kb
	})
	return out
}

func bucketOf(t time.Time, bucket string, loc *time.Location) (time.Time, string) {
	l := t.In(loc)
	day := time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, loc)
	switch bucket {
	case "week":
		start := day.AddDate(0, 0, -int(day.Weekday()))
		return start, start.Format("2006-01-02")
	case "month":
		start := time.Date(l.Year(), l.Month(), 1, 0, 0, 0, 0, loc)
		return start, start.Format("2006-01")
	}
	return day, day.Format("2006-01-02")
}

func trend(rows []Row, bucket string, loc *time.Location) []obj {
	type b struct {
		start time.Time
		label string
		rows  []Row
	}
	by := map[string]*b{}
	for _, r := range rows {
		start, label := bucketOf(r.StartedAt, bucket, loc)
		x, ok := by[label]
		if !ok {
			x = &b{start: start, label: label}
			by[label] = x
		}
		x.rows = append(x.rows, r)
	}
	list := make([]*b, 0, len(by))
	for _, x := range by {
		list = append(list, x)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].start.Before(list[j].start) })
	out := make([]obj, 0, len(list))
	for _, x := range list {
		s := summary(x.rows)
		out = append(out, obj{"bucket": x.label, "tokens": s["tokens"], "measuredFloor": s["measuredFloor"],
			"strictTotal": s["strictTotal"], "coverage": s["coverage"]})
	}
	return out
}

// ---------- portfolio ----------

func runID(r Row) string {
	if t := strings.TrimSpace(r.TaskID); t != "" {
		return "task:" + t
	}
	if r.BoundaryKind != "" && r.BoundaryID != "" {
		return r.BoundaryKind + ":" + r.BoundaryID
	}
	if r.SessionID != "" {
		return "session:" + r.SessionID
	}
	return "interval:" + r.IntervalKey
}

func distinctRuns(rows []Row, keep func(Row) bool) int {
	seen := map[string]bool{}
	for _, r := range rows {
		if keep == nil || keep(r) {
			seen[runID(r)] = true
		}
	}
	return len(seen)
}

func unknownOutputRuns(rows []Row) int {
	return distinctRuns(rows, func(r Row) bool { return output(r) == nil })
}

// identity is `UsageFeatureClassifier`'s Project identity for a row, or ""
// with the reason there is none.
func identity(r Row) (string, string) {
	key := strings.TrimSpace(r.ProjectKey)
	if key == "" {
		return "", "project_key_missing"
	}
	clean := filepath.Clean(key)
	if LegacyWorktreeKey(clean) {
		return "", "legacy_managed_worktree_project_key"
	}
	return clean, ""
}

// ProjectID is the Portfolio's id for an identity.
func ProjectID(identity string) string {
	if identity == "" {
		return "unknown-project"
	}
	sum := sha256.Sum256([]byte(identity))
	return "project-" + hex.EncodeToString(sum[:8])
}

func projectLabel(identity string) string {
	if identity == "" {
		return "Unknown Project"
	}
	if name := filepath.Base(identity); name != "" && name != "." && name != "/" {
		return name
	}
	return "Unnamed Project"
}

func coveragePayload(rows []Row) obj {
	complete, partial := 0, 0
	for _, r := range rows {
		if measureRow(r).incomplete {
			partial++
		} else {
			complete++
		}
	}
	status := "partial"
	if len(rows) == 0 {
		status = "unavailable"
	} else if partial == 0 {
		status = "complete"
	}
	c := coverageObj(rows)
	return obj{"status": status, "rows": len(rows), "completeRows": complete, "partialRows": partial,
		"unknownOutputRuns": unknownOutputRuns(rows), "states": c["states"], "reasons": c["reasons"]}
}

func comparableCost(rows []Row) obj {
	claude := []Row{}
	for _, r := range rows {
		if r.Assistant == "claude" {
			claude = append(claude, r)
		}
	}
	if len(claude) == 0 {
		return obj{"status": "unavailable", "reason": "no_claude_code_usage", "assistant": "claude"}
	}
	missing := 0
	for _, r := range claude {
		if r.CostValue == nil || r.CostUnit == "" {
			missing++
		}
	}
	if missing > 0 {
		return obj{"status": "unavailable", "reason": "partial_cost_coverage", "unavailableRows": missing,
			"assistant": "claude"}
	}
	series := costsOf(claude)
	if len(series) != 1 {
		reason := "mixed_cost_series"
		if len(series) == 0 {
			reason = "no_cost_series"
		}
		return obj{"status": "unavailable", "assistant": "claude", "reason": reason}
	}
	s := series[0]
	return obj{"status": "available", "value": s.value, "unit": s.unit, "basis": s.basis, "rows": s.rows,
		"assistant": "claude"}
}

func lineage(rows []Row) obj {
	roles := map[string]string{}
	order := []string{}
	for _, r := range rows {
		id := runID(r)
		role := "unknown"
		switch {
		case r.Origin == "schedule":
			role = "scheduled"
		case r.BoundaryKind == "session":
			role = "root"
		case r.BoundaryKind == "task" && r.Depth == 1:
			role = "child"
		}
		prev, seen := roles[id]
		if !seen {
			order = append(order, id)
		}
		if !seen || prev == "unknown" {
			roles[id] = role
		}
	}
	counts := map[string]int{}
	for _, id := range order {
		counts[roles[id]]++
	}
	status := "partial"
	if counts["unknown"] == 0 {
		status = "available"
	} else if counts["root"]+counts["child"] == 0 {
		status = "unavailable"
	}
	var reason any
	if counts["unknown"] > 0 {
		reason = "lineage_evidence_missing"
	}
	return obj{"status": status, "rootRuns": counts["root"], "childRuns": counts["child"],
		"scheduledRuns": counts["scheduled"], "unknownRuns": counts["unknown"], "reason": reason}
}

func totalOutput(rows []Row) *int64 { return sumTokens(rows)[partOutput] }

func outputComparison(rows, previous []Row, reason string) obj {
	if reason != "" {
		return obj{"status": "unavailable", "reason": reason}
	}
	if len(previous) == 0 {
		return obj{"status": "unavailable", "reason": "no_previous_data"}
	}
	for _, side := range [][]Row{rows, previous} {
		for _, r := range side {
			if output(r) == nil {
				return obj{"status": "unavailable", "reason": "incomplete_output"}
			}
		}
	}
	cur, prev := totalOutput(rows), totalOutput(previous)
	if cur == nil || prev == nil {
		return obj{"status": "unavailable", "reason": "incomplete_output"}
	}
	delta := *cur - *prev
	out := obj{"status": "comparable", "current": *cur, "previous": *prev, "absolute": delta}
	if *prev == 0 {
		out["percent"] = nil
		out["percentReason"] = "previous_zero"
	} else {
		out["percent"] = float64(delta) / float64(*prev) * 100
		out["percentReason"] = nil
	}
	return out
}

func mix(rows []Row, label func(Row) string) []obj {
	order := []string{}
	by := map[string][]Row{}
	for _, r := range rows {
		l := label(r)
		if l == "" {
			l = "Unknown"
		}
		if _, ok := by[l]; !ok {
			order = append(order, l)
		}
		by[l] = append(by[l], r)
	}
	out := []obj{}
	for _, l := range order {
		out = append(out, obj{"label": l, "runs": distinctRuns(by[l], nil), "output": intOrNil(totalOutput(by[l])),
			"unknownOutputRuns": unknownOutputRuns(by[l])})
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := outputRank(out[i]["output"]), outputRank(out[j]["output"])
		if a != b {
			return a > b
		}
		return out[i]["label"].(string) < out[j]["label"].(string)
	})
	return out
}

type projectGroup struct {
	identity string
	reasons  map[string]bool
	rows     []Row
}

func groupProjects(rows []Row) (map[string]*projectGroup, []string) {
	by := map[string]*projectGroup{}
	order := []string{}
	for _, r := range rows {
		id, why := identity(r)
		key := id
		if id == "" {
			key = "\x00unknown-project"
		}
		g, ok := by[key]
		if !ok {
			g = &projectGroup{identity: id, reasons: map[string]bool{}}
			by[key] = g
			order = append(order, key)
		}
		if why != "" {
			g.reasons[why] = true
		}
		g.rows = append(g.rows, r)
	}
	return by, order
}

func (res Result) comparisonReason() string {
	if _, ok := res.Query.previous(); !ok {
		return "closed_range_required"
	}
	if res.Truncated || res.PrevTrunc {
		return "range_truncated"
	}
	return ""
}

func (res Result) portfolio() obj {
	q := res.Query
	reason := res.comparisonReason()
	groups, order := groupProjects(res.Rows)
	prevGroups, _ := groupProjects(res.Previous)

	projects := []obj{}
	for _, key := range order {
		g := groups[key]
		var prevRows []Row
		if p, ok := prevGroups[key]; ok {
			prevRows = p.rows
		}
		s := summary(g.rows)
		label := projectLabel(g.identity)
		scheduledRows := []Row{}
		for _, r := range g.rows {
			if r.Origin == "schedule" {
				scheduledRows = append(scheduledRows, r)
			}
		}
		idStatus := "available"
		if g.identity == "" {
			idStatus = "unavailable"
		}
		recent := []obj{}
		for i, r := range g.rows {
			if i == 6 {
				break
			}
			row := publicRow(r)
			row["project"] = label
			recent = append(recent, row)
		}
		projects = append(projects, obj{
			"id":                ProjectID(g.identity),
			"label":             label,
			"identity":          obj{"status": idStatus, "reasons": sortedKeys(g.reasons)},
			"output":            s["tokens"].(obj)["output"],
			"unknownOutputRuns": unknownOutputRuns(g.rows),
			"tokens":            s["tokens"],
			"tokenPartsUnknown": s["tokenPartsUnknown"],
			"runs":              distinctRuns(g.rows, nil),
			"scheduledRuns":     distinctRuns(scheduledRows, nil),
			"scheduledOutput":   intOrNil(totalOutput(scheduledRows)),
			"cost":              comparableCost(g.rows),
			"coverage":          coveragePayload(g.rows),
			"lineage":           lineage(g.rows),
			"comparison":        outputComparison(g.rows, prevRows, reason),
			"trend":             trend(g.rows, "day", q.Loc),
			"assistantMix":      mix(g.rows, func(r Row) string { return r.Assistant }),
			"workMix": mix(g.rows, func(r Row) string {
				if r.Origin == "schedule" {
					return "Scheduled"
				}
				return "Interactive"
			}),
			"recentWork": recent,
		})
	}
	sort.SliceStable(projects, func(i, j int) bool {
		a, b := outputRank(projects[i]["output"]), outputRank(projects[j]["output"])
		if a != b {
			return a > b
		}
		return projects[i]["id"].(string) < projects[j]["id"].(string)
	})
	for i := range projects {
		projects[i]["rank"] = i + 1
	}

	comparison := outputComparison(res.Rows, res.Previous, reason)
	comparison["currentRange"] = obj{"from": strOrNil(q.From), "to": strOrNil(q.To)}
	if prev, ok := q.previous(); ok {
		comparison["previousRange"] = obj{"from": prev.from, "to": prev.to}
	} else {
		comparison["previousRange"] = nil
	}

	return obj{
		"schemaVersion": 1,
		"primarySignal": "generated_output",
		"scoreWarning":  "Generated output is an operational signal, not a productivity score.",
		"runs":          distinctRuns(res.Rows, nil),
		"comparison":    comparison,
		"projects":      projects,
		"scheduledWork": res.scheduledWork(),
		"features":      features(res.Rows),
		"insights":      res.insights(projects),
	}
}

func (res Result) scheduledWork() obj {
	loc := res.Query.Loc
	scheduled := []Row{}
	by := map[string][]Row{}
	order := []string{}
	noID := []Row{}
	for _, r := range res.Rows {
		if r.Origin != "schedule" {
			continue
		}
		scheduled = append(scheduled, r)
		id := strings.TrimSpace(r.ScheduleID)
		if id == "" {
			noID = append(noID, r)
			continue
		}
		if _, ok := by[id]; !ok {
			order = append(order, id)
		}
		by[id] = append(by[id], r)
	}
	schedules := []obj{}
	for _, id := range order {
		rows := by[id]
		days := map[string]bool{}
		var last time.Time
		label := ""
		for _, r := range rows {
			days[r.StartedAt.In(loc).Format("2006-01-02")] = true
			if r.StartedAt.After(last) {
				last = r.StartedAt
			}
			// Rows are newest first, so the first label is the newest name.
			if label == "" && r.RootLabel != "" {
				label = r.RootLabel
			}
		}
		if label == "" {
			label = id
		}
		schedules = append(schedules, obj{"id": id, "label": label, "runs": distinctRuns(rows, nil),
			"output": intOrNil(totalOutput(rows)), "unknownOutputRuns": unknownOutputRuns(rows),
			"activeDays": len(days), "lastRunAt": isoOrNil(last), "coverage": coveragePayload(rows)})
	}
	sort.SliceStable(schedules, func(i, j int) bool {
		a, b := outputRank(schedules[i]["output"]), outputRank(schedules[j]["output"])
		if a != b {
			return a > b
		}
		return schedules[i]["id"].(string) < schedules[j]["id"].(string)
	})
	status, reason := "available", any(nil)
	if len(schedules) == 0 {
		status, reason = "unavailable", "no_schedule_identity_in_range"
	}
	return obj{
		"status":            status,
		"reason":            reason,
		"runs":              distinctRuns(scheduled, nil),
		"output":            intOrNil(totalOutput(scheduled)),
		"unknownOutputRuns": unknownOutputRuns(scheduled),
		"schedules":         schedules,
		"unknownSchedule":   obj{"runs": distinctRuns(noID, nil), "reason": "schedule_identity_missing"},
	}
}

// features has no accepted attribution to read: this daemon records none and
// runs no classifier, so every run is the Unknown Feature.
func features(rows []Row) obj {
	status := "available"
	if len(rows) > 0 {
		status = "no_accepted_attribution"
	}
	return obj{
		"status":               status,
		"automaticAttribution": false,
		"policy":               "one_unambiguous_accepted_head",
		"classifier":           obj{"configured": false},
		"roleEvidence": obj{"reviewReceipts": obj{"status": "complete", "read": 0, "limit": 5000,
			"truncated": false}},
		"groups": []obj{},
		"unknown": obj{"label": "Unknown Feature", "runs": distinctRuns(rows, nil),
			"output": intOrNil(totalOutput(rows)), "unknownOutputRuns": unknownOutputRuns(rows),
			"reason": "no_unambiguous_accepted_head"},
	}
}

func (res Result) insights(projects []obj) []obj {
	out := []obj{}
	// top_mover
	var mover obj
	var moverAbs int64 = -1
	for _, p := range projects {
		c := p["comparison"].(obj)
		if c["status"] != "comparable" {
			continue
		}
		d := c["absolute"].(int64)
		if d == 0 {
			continue
		}
		if a := abs(d); a > moverAbs {
			mover, moverAbs = p, a
		}
	}
	if mover != nil {
		d := mover["comparison"].(obj)["absolute"].(int64)
		out = append(out, obj{"kind": "top_mover", "projectId": mover["id"], "title": "Largest output change",
			"detail": fmt.Sprintf("%s changed by %d generated tokens versus the equal previous range. Inspect the work mix before drawing a conclusion.", mover["label"], d)})
	}
	// context_to_output
	var ctxProject obj
	best := -1.0
	for _, p := range projects {
		generated := outputRank(p["output"])
		t := p["tokens"].(obj)
		in, okIn := t["inputNew"].(int64)
		cache, okCache := t["cacheRead"].(int64)
		unknown := p["tokenPartsUnknown"].(obj)
		if generated <= 0 || !okIn || !okCache || unknown["output"] != nil || unknown["inputNew"] != nil || unknown["cacheRead"] != nil {
			continue
		}
		ratio := float64(in+cache) / float64(generated)
		if ratio > best {
			ctxProject, best = p, ratio
		}
	}
	if ctxProject != nil && best >= 10 {
		out = append(out, obj{"kind": "context_to_output", "projectId": ctxProject["id"],
			"title":  "High context-to-output ratio",
			"detail": fmt.Sprintf("%s read %.1fx as much new-plus-cached context as it generated. This can be normal for review or retrieval-heavy work; inspect recent runs.", ctxProject["label"], best)})
	}
	// coverage_degradation
	if len(res.Rows) > 0 && len(res.Previous) > 0 {
		cur, prev := completeFraction(res.Rows), completeFraction(res.Previous)
		if prev-cur >= 0.1 {
			out = append(out, obj{"kind": "coverage_degradation", "title": "Coverage declined",
				"detail": fmt.Sprintf("Complete rows fell from %.0f%% to %.0f%% versus the equal previous range. Check the named coverage reasons before using totals.", prev*100, cur*100)})
		}
	}
	// cost_concentration
	if all := comparableCost(res.Rows); all["status"] == "available" {
		total := all["value"].(float64)
		var top obj
		topValue := -1.0
		for _, p := range projects {
			c := p["cost"].(obj)
			if c["status"] != "available" {
				continue
			}
			if v := c["value"].(float64); v > topValue {
				top, topValue = p, v
			}
		}
		if total > 0 && top != nil && topValue/total >= 0.5 {
			out = append(out, obj{"kind": "cost_concentration", "projectId": top["id"],
				"title":  "Comparable cost is concentrated",
				"detail": fmt.Sprintf("%s accounts for %.0f%% of Claude Code's comparable %s / %s estimated spending in this range.", top["label"], topValue/total*100, all["unit"], all["basis"])})
		}
	}
	if len(out) > 4 {
		out = out[:4]
	}
	return out
}

func completeFraction(rows []Row) float64 {
	complete := 0
	for _, r := range rows {
		if !measureRow(r).incomplete {
			complete++
		}
	}
	return float64(complete) / float64(len(rows))
}

func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// ---------- the payload ----------

func latest(rows []Row) time.Time {
	var t time.Time
	for _, r := range rows {
		if r.UpdatedAt.After(t) {
			t = r.UpdatedAt
		}
	}
	return t
}

func (res Result) base() obj {
	q := res.Query
	now := res.GeneratedAt
	fresh := obj{"generatedAt": iso(now), "latestObservedAt": nil, "ageSeconds": nil, "status": "empty",
		"scanTruncated": res.Truncated}
	if t := latest(res.All); !t.IsZero() {
		age := int64(now.Sub(t) / time.Second)
		fresh["latestObservedAt"], fresh["ageSeconds"] = iso(t), age
		fresh["status"] = "current"
		if age > staleAfter {
			fresh["status"] = "stale"
		}
	}
	rangeFresh := obj{"dataThrough": nil, "ageSeconds": nil, "status": "empty"}
	if t := latest(res.Rows); !t.IsZero() {
		age := int64(now.Sub(t) / time.Second)
		rangeFresh["dataThrough"], rangeFresh["ageSeconds"] = iso(t), age
		rangeFresh["status"] = "current"
		if age > staleAfter {
			rangeFresh["status"] = "historical"
		}
	}
	observed := map[string]bool{}
	for _, r := range res.Rows {
		if r.PriceSnapshotID != "" {
			observed[r.PriceSnapshotID] = true
		}
	}
	totals := summary(res.Rows)
	availability := obj{"status": "complete"}
	if res.Truncated {
		availability = obj{"status": "partial", "reason": "scan_limit_reached"}
	}
	return obj{
		"schemaVersion":  1,
		"view":           q.View,
		"range":          obj{"from": strOrNil(q.From), "to": strOrNil(q.To), "timezone": q.Zone},
		"freshness":      fresh,
		"rangeFreshness": rangeFresh,
		"capabilities": obj{
			"views":       []string{"overview", "agent_work"},
			"groupBy":     groupNames,
			"buckets":     []string{"day", "week", "month"},
			"filters":     []string{"from", "to", "timezone", "assistant", "model", "origin", "project"},
			"exports":     []string{"csv", "json"},
			"maxPageSize": MaxPageSize, "maxScannedRows": MaxScannedRows,
			"attribution": obj{
				"dimensions":                  []string{"project", "feature"},
				"sources":                     []string{"explicit", "inherited", "manual", "llm", "policy", "heuristic"},
				"decisions":                   []string{"proposed", "accepted", "rejected"},
				"featureAggregation":          "one_unambiguous_accepted_head",
				"automaticFeatureAttribution": false,
				"featureProducer":             "manual_or_external_only",
				"llmEvidence":                 "classifier_version_confidence_and_sha256_only",
			},
		},
		"priceSnapshot": obj{"activeId": PriceSnapshotID, "observedIds": sortedKeys(observed),
			"meaning": "Observed ids priced rows in this range; activeId is the current list-price table, not an actual bill."},
		"totals":      totals,
		"coverage":    totals["coverage"],
		"corrections": 0,
		"breakdown":   breakdown(res.Rows, q.Group, q.Loc),
		"groupBy":     q.Group,
		"trend":       trend(res.Rows, q.Bucket, q.Loc),
		"bucket":      q.Bucket,
		"unavailableDimensions": obj{
			"dimensions":          []string{"disposition"},
			"reason":              "An accepted outcome, or Feature, is unavailable unless explicit lineage or an accepted attribution event exists. Clawdline never infers them from a root Session or task success.",
			"graphView":           false,
			"retryView":           false,
			"landingView":         false,
			"featureView":         true,
			"featureAvailability": "one_unambiguous_accepted_head_or_unknown",
		},
		"portfolio":    res.portfolio(),
		"availability": availability,
	}
}

// Payload is the body under `usage`: one page of rows.
func (res Result) Payload() obj {
	q := res.Query
	out := res.base()
	page := []Row{}
	rest := res.Rows
	if c := q.Cursor; c != nil {
		for i, r := range rest {
			at := epoch(r.StartedAt)
			if at < c.At || (at == c.At && r.IntervalKey < c.Key) {
				rest = rest[i:]
				break
			}
			if i == len(rest)-1 {
				rest = nil
			}
		}
	}
	if len(rest) > q.Limit {
		page = rest[:q.Limit]
	} else {
		page = rest
	}
	hasMore := len(rest) > len(page)
	var next any
	if hasMore && len(page) > 0 {
		last := page[len(page)-1]
		next = encodeCursor(Cursor{Key: last.IntervalKey, At: epoch(last.StartedAt)})
	}
	rows := make([]obj, 0, len(page))
	for _, r := range page {
		rows = append(rows, publicRow(r))
	}
	out["rows"] = rows
	out["rowCount"] = len(res.Rows)
	out["pagination"] = obj{"limit": q.Limit, "nextCursor": next, "hasMore": hasMore}
	return out
}

// ExportJSON is the lossless export: every matched row, no pagination.
func (res Result) ExportJSON() obj {
	out := res.base()
	rows := make([]obj, 0, len(res.Rows))
	for _, r := range res.Rows {
		rows = append(rows, publicRow(r))
	}
	out["rows"] = rows
	out["rowCount"] = len(rows)
	out["truncated"] = res.Truncated
	return out
}

// ExportCSV is `UsageQueryService.exportCSV`.
func (res Result) ExportCSV() string {
	var b strings.Builder
	b.WriteString("interval_id,task_id,started_at,ended_at,assistant,model,origin,project,input_new,output,cache_read,cache_write,strict_total,measured_floor,unknown_token_parts,source_total,reconciliation,input_basis,cost_value,cost_unit,cost_basis,price_snapshot_id,missing_cost_reason,coverage,coverage_reasons\n")
	num := func(p *int64) string {
		if p == nil {
			return ""
		}
		return strconv.FormatInt(*p, 10)
	}
	for _, r := range res.Rows {
		m := measureRow(r)
		floor := ""
		if !m.unknown {
			floor = strconv.FormatInt(m.measured, 10)
		}
		ended := ""
		if !r.EndedAt.IsZero() {
			ended = iso(r.EndedAt)
		}
		project := ""
		if p := projectName(r); p != nil {
			project = *p
		}
		cost := ""
		if r.CostValue != nil {
			cost = swiftDouble(*r.CostValue)
		}
		cells := []string{r.IntervalKey, r.TaskID, iso(r.StartedAt), ended, r.Assistant, r.Model, r.Origin, project,
			num(r.Tokens[partInputNew]), num(r.Tokens[partOutput]), num(r.Tokens[partCacheRead]),
			num(r.Tokens[partCacheWrite]), num(m.total), floor, strings.Join(m.unknownParts, " "),
			num(r.SourceTotal), "", r.InputBasis, cost, r.CostUnit, r.CostBasis, r.PriceSnapshotID,
			r.MissingCost, r.Coverage, strings.Join(m.reasons, " ")}
		for i, c := range cells {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(csvCell(c))
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// swiftDouble is Swift's `String(Double)`: the shortest form that reads back,
// always with a fractional part.
func swiftDouble(v float64) string {
	s := strconv.FormatFloat(v, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eEn") {
		s += ".0"
	}
	return s
}

func csvCell(c string) string {
	trimmed := strings.TrimLeft(c, " ")
	if trimmed != "" && strings.ContainsRune("=+-@\t\r", rune(trimmed[0])) {
		c = "'" + c
	}
	if strings.ContainsAny(c, ",\"\n\r") {
		c = `"` + strings.ReplaceAll(c, `"`, `""`) + `"`
	}
	return c
}
