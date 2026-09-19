package board

import (
	"encoding/json"
	"sort"
	"strings"
	"time"
)

// Envelope is `GET /v1/board`'s `board` object: the 19 keys the Swift app
// publishes, plus `truncation` when one project is selected.
type Envelope struct {
	SchemaVersion        int            `json:"schemaVersion"`
	Revision             int64          `json:"revision"`
	Enabled              bool           `json:"enabled"`
	Mode                 string         `json:"mode"`
	UpdatedAt            float64        `json:"updatedAt"`
	Available            bool           `json:"available"`
	Entitlement          Entitlement    `json:"entitlement"`
	NarrativeConsent     *string        `json:"narrativeConsent"`
	Projects             []ProjectRow   `json:"projects"`
	Items                []Card         `json:"items"`
	Item                 *Card          `json:"item"`
	Truncated            bool           `json:"truncated"`
	Truncation           *Truncation    `json:"truncation,omitempty"`
	SnapshotBudgetBytes  int            `json:"snapshotBudgetBytes"`
	AutomaticMutation    map[string]any `json:"automaticMutation"`
	SourceIngestion      map[string]any `json:"sourceIngestion"`
	Source               map[string]any `json:"source"`
	ReadState            ReadState      `json:"readState"`
	ResponsibilitySource map[string]any `json:"responsibilitySource"`
	Viewer               Viewer         `json:"viewer"`
}

// Truncation says what this list left out, and why. It describes this list —
// never a different one the byte budget happened to cut.
type Truncation struct {
	Reason            *string `json:"reason"`
	ItemsOmittedCount int     `json:"itemsOmittedCount"`
}

// ReadState is how fresh this answer is.
type ReadState struct {
	Status      string         `json:"status"`
	Revision    int64          `json:"revision"`
	ObservedAt  *float64       `json:"observedAt"`
	AttemptedAt *float64       `json:"attemptedAt"`
	Refreshing  bool           `json:"refreshing"`
	Error       map[string]any `json:"error"`
}

// ProjectRow is one catalog entry.
type ProjectRow struct {
	ID                    string         `json:"id"`
	Name                  string         `json:"name"`
	Label                 string         `json:"label"`
	DisplayPath           string         `json:"displayPath,omitempty"`
	Icon                  any            `json:"icon,omitempty"`
	IsStartPoint          bool           `json:"isStartPoint"`
	ItemCount             int            `json:"itemCount"`
	ActiveItemCount       int            `json:"activeItemCount"`
	HumanItemCount        int            `json:"humanItemCount"`
	CurrentHumanItemCount int            `json:"currentHumanItemCount"`
	HumanLandedItemCount  int            `json:"humanLandedItemCount"`
	AgentRecordCount      int            `json:"agentRecordCount"`
	ArchivedRecordCount   int            `json:"archivedRecordCount"`
	Revision              int64          `json:"revision"`
	UpdatedAt             float64        `json:"updatedAt"`
	ObservedAt            float64        `json:"observedAt"`
	Summary               map[string]any `json:"summary"`
	SummaryCoverage       map[string]any `json:"summaryCoverage"`
}

// Card is one list row, built through `BoardCardContract`'s allow-list: the
// identity fields as they are, `progress` narrowed, and nothing else. A field
// that is not here does not reach the console, which is the point — the Swift
// app learned that by measuring 62,801 bytes a read nobody was reading.
type Card struct {
	ID             string         `json:"id"`
	Key            string         `json:"key"`
	KeyStatus      string         `json:"keyStatus"`
	ProjectID      string         `json:"projectId"`
	Title          string         `json:"title"`
	Type           string         `json:"type"`
	State          string         `json:"state"`
	Summary        string         `json:"summary"`
	Owner          string         `json:"owner"`
	ParentID       *string        `json:"parentId"`
	CreatedAt      float64        `json:"createdAt"`
	UpdatedAt      float64        `json:"updatedAt"`
	ScopeRevision  int64          `json:"scopeRevision"`
	Presentation   map[string]any `json:"presentation"`
	ListSummary    ListSummary    `json:"listSummary"`
	CardSummary    CardSummary    `json:"cardSummary"`
	Progress       Progress       `json:"progress"`
	Responsibility map[string]any `json:"responsibility"`
}

// Presence is the live session inventory the `responsibility` projection joins
// on. It is request-time data and never enters a store.
type Presence struct {
	Complete   bool
	Fresh      bool
	ObservedAt time.Time
	Generation int64
	// ByConversation maps a lowercased conversation UUID to the sessions
	// running it right now.
	ByConversation map[string][]PresenceRow
}

// PresenceRow is one live session.
type PresenceRow struct {
	TerminalID string
	Title      string
	Provider   string
	State      string
}

// Query is what a read asked for.
type Query struct {
	Project  string
	Item     string
	Audience string
	Cursor   int
	Limit    int
}

// Inputs is everything one read is built from.
type Inputs struct {
	Legacy      *StoredState
	LegacyAt    time.Time
	LegacyErr   error
	Settings    SettingsState
	Viewer      Viewer
	Presence    Presence
	ProjectIcon func(path string) any
	ProjectPath func(id string) string
}

// Build answers one read.
func Build(in Inputs, q Query) (Envelope, int) {
	now := float64(time.Now().UnixNano()) / 1e9
	env := Envelope{
		SchemaVersion:       WireSchemaVersion,
		Revision:            in.Settings.Revision,
		Enabled:             in.Settings.Enabled,
		Mode:                Mode(in.Settings.Enabled),
		UpdatedAt:           in.Settings.UpdatedAt,
		Available:           true,
		Entitlement:         FreePreview(),
		NarrativeConsent:    in.Settings.NarrativeConsent,
		Projects:            []ProjectRow{},
		Items:               []Card{},
		SnapshotBudgetBytes: MaximumSnapshotBytes,
		AutomaticMutation:   map[string]any{"status": "available", "persisted": true, "reason": nil},
		Viewer:              in.Viewer,
	}
	env.ResponsibilitySource = map[string]any{
		"provenance": "session_watch", "complete": in.Presence.Complete,
		"fresh": in.Presence.Fresh, "observedAt": unixOrNil(in.Presence.ObservedAt),
		"generation": in.Presence.Generation,
	}

	legacy := in.Legacy
	status := "ready"
	var readError map[string]any
	switch {
	case legacy == nil && (in.LegacyErr == ErrLegacyAbsent || in.LegacyErr == ErrLegacyDisabled):
		// No Swift board on this machine, or one this daemon was told not to
		// read (cutover B1): an empty catalog is the truth of what is read.
		// Which of the two it is, /v1/board/tracks says in `board.status`.
	case legacy == nil:
		// Unknown is not empty. Say so rather than draw an empty board.
		status = "error"
		readError = map[string]any{"code": "board_source_unreadable", "message": errText(in.LegacyErr)}
	case in.LegacyErr != nil && IsStale(in.LegacyErr):
		status = "stale"
	}

	var observed *float64
	if !in.LegacyAt.IsZero() {
		value := float64(in.LegacyAt.UnixNano()) / 1e9
		observed = &value
	}
	env.ReadState = ReadState{Status: status, Revision: in.Settings.Revision,
		ObservedAt: observed, AttemptedAt: &now, Error: readError}

	coverage := []StoredCoverage{}
	if legacy != nil {
		coverage = legacy.IngestionCoverage
	}
	env.SourceIngestion = ingestionObject(coverage)
	env.Source = map[string]any{
		"observedAt": observedOrNil(observed), "attemptedAt": now, "truncated": false,
		"ingestion": map[string]any{
			"status": env.SourceIngestion["status"], "issueCount": len(coverage),
			"reasons": env.SourceIngestion["reasons"], "historicalDroppedCount": env.SourceIngestion["droppedCount"],
		},
	}

	if legacy == nil {
		if q.Project != "" {
			env.ReadState.Status = "error"
			env.ReadState.Error = map[string]any{"code": "project_not_found",
				"message": "This board has no such project."}
		}
		return env, 200
	}

	items := append([]StoredItem(nil), legacy.Items...)
	sortItems(items)
	keyCounts := map[string]map[string]int{}
	for _, item := range items {
		if keyCounts[item.ProjectID] == nil {
			keyCounts[item.ProjectID] = map[string]int{}
		}
		keyCounts[item.ProjectID][item.Key]++
	}
	cards := make([]Card, 0, len(items))
	byProject := map[string][]Card{}
	for _, item := range items {
		card := cardOf(item, items, keyCounts, in.Presence)
		cards = append(cards, card)
		byProject[item.ProjectID] = append(byProject[item.ProjectID], card)
	}

	env.Projects = catalog(legacy, byProject, in, now)

	if q.Item != "" && !strings.Contains(q.Item, ":") {
		for index := range cards {
			if cards[index].ID == q.Item && (q.Project == "" || cards[index].ProjectID == q.Project) {
				env.Item = &cards[index]
				break
			}
		}
		return env, 200
	}
	if q.Project == "" {
		return env, 200
	}

	known := false
	for _, p := range env.Projects {
		if p.ID == q.Project {
			known = true
			break
		}
	}
	if !known {
		env.ReadState.Status = "error"
		env.ReadState.Error = map[string]any{"code": "project_not_found",
			"message": "This board has no such project."}
		return env, 200
	}

	list := byProject[q.Project]
	if q.Audience != "" {
		env.Items, env.Truncated, env.Truncation = page(list, q)
		return env, 200
	}
	env.Items, env.Truncated, env.Truncation = budgeted(env, list)
	return env, 200
}

// budgeted cuts the list to the snapshot budget, measured on the envelope with
// both lists empty so that no card is counted twice. This is the Swift app's
// rule and its flaw is documented (docs/board-design.md A8); it is kept here
// because the console the rule serves is the same console.
func budgeted(env Envelope, list []Card) ([]Card, bool, *Truncation) {
	shell := env
	shell.Items, shell.Projects = []Card{}, []ProjectRow{}
	baseBytes, _ := json.Marshal(map[string]any{"board": shell})
	used := len(baseBytes)
	kept := make([]Card, 0, len(list))
	for _, card := range list {
		if len(kept) == MaximumSnapshotItems {
			break
		}
		body, _ := json.Marshal(card)
		separator := 0
		if len(kept) > 0 {
			separator = 1
		}
		if used+len(body)+separator > MaximumSnapshotBytes {
			break
		}
		kept = append(kept, card)
		used += len(body) + separator
	}
	omitted := len(list) - len(kept)
	truncation := &Truncation{ItemsOmittedCount: omitted}
	if omitted > 0 {
		reason := "snapshot_byte_budget"
		if len(kept) == MaximumSnapshotItems {
			reason = "item_count_limit"
		}
		truncation.Reason = &reason
	}
	return kept, omitted > 0, truncation
}

// page is the audience slice. Its truncation is recomputed for the list it
// actually sent: saying `truncated` about somebody else's omission, or nothing
// about this one, would both be lies.
func page(list []Card, q Query) ([]Card, bool, *Truncation) {
	filtered := make([]Card, 0, len(list))
	for _, card := range list {
		audience := card.ListSummary.View.Audience
		if q.Audience == "all" || audience == q.Audience {
			filtered = append(filtered, card)
		}
	}
	start := q.Cursor
	if start > len(filtered) {
		start = len(filtered)
	}
	end := start + q.Limit
	if end > len(filtered) {
		end = len(filtered)
	}
	out := append([]Card(nil), filtered[start:end]...)
	omitted := len(filtered) - len(out)
	truncation := &Truncation{ItemsOmittedCount: omitted}
	if end < len(filtered) {
		reason := "page_limit"
		truncation.Reason = &reason
	}
	return out, end < len(filtered), truncation
}

func cardOf(item StoredItem, all []StoredItem, keyCounts map[string]map[string]int,
	presence Presence) Card {
	progress := ProgressOf(item, all)
	keyStatus := "unique"
	if keyCounts[item.ProjectID][item.Key] > 1 {
		keyStatus = "ambiguous"
	}
	return Card{
		ID: item.ID, Key: item.Key, KeyStatus: keyStatus, ProjectID: item.ProjectID,
		Title: item.Title, Type: item.Type, State: item.State, Summary: item.Summary,
		Owner: item.Owner, ParentID: item.ParentID, CreatedAt: item.CreatedAt,
		UpdatedAt: item.UpdatedAt, ScopeRevision: item.scopeRevision(),
		Presentation:   presentationOf(item),
		ListSummary:    ListSummaryOf(item, progress),
		CardSummary:    CardSummaryOf(item),
		Progress:       progress,
		Responsibility: responsibilityOf(item.Owner, presence),
	}
}

// presentationOf carries only the AI prose marked current. It is narrative and
// never evidence, which is what `authority` says.
func presentationOf(item StoredItem) map[string]any {
	variants := []map[string]any{}
	for _, p := range item.Presentations {
		if p.Status != "current" {
			continue
		}
		row := map[string]any{"status": p.Status, "locale": p.Locale, "title": p.Title,
			"summary": p.Summary}
		if p.Outcome != nil {
			row["outcome"] = *p.Outcome
		}
		variants = append(variants, row)
	}
	return map[string]any{"authority": "narrative_only", "variants": variants}
}

// responsibilityOf is `ProjectBoardIntegration.responsibilityObject`. The join
// is on the conversation UUID only; the title is presentation. An incomplete or
// old inventory is never evidence that the owner is gone.
func responsibilityOf(owner string, presence Presence) map[string]any {
	answer := map[string]any{
		"observedAt": unixOrNil(presence.ObservedAt), "inventoryGeneration": presence.Generation,
		"inventoryComplete": presence.Complete, "nextAction": nil,
	}
	if owner == "" {
		answer["ownerId"], answer["ownerKind"], answer["status"], answer["live"] =
			nil, "unassigned", "unassigned", nil
		return answer
	}
	conversation := strings.ToLower(owner)
	if !isUUID(conversation) {
		answer["ownerId"], answer["ownerKind"], answer["status"], answer["live"] =
			owner, "recorded", "recorded_owner", nil
		return answer
	}
	matches := presence.ByConversation[conversation]
	answer["ownerId"], answer["ownerKind"], answer["matchCount"] = conversation, "session", len(matches)
	switch {
	case !presence.Complete || !presence.Fresh:
		answer["status"], answer["live"] = "inventory_unknown", nil
	case len(matches) == 1:
		m := matches[0]
		answer["status"], answer["live"] = "live_session", true
		answer["title"], answer["terminalId"], answer["sessionState"] = m.Title, m.TerminalID, m.State
		if m.Provider != "" {
			answer["provider"] = m.Provider
		} else {
			answer["provider"] = nil
		}
	case len(matches) == 0:
		answer["status"], answer["live"] = "session_not_live", false
	default:
		answer["status"], answer["live"] = "session_ambiguous", nil
	}
	return answer
}

func isUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index, r := range value {
		switch index {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			if !strings.ContainsRune("0123456789abcdef", r) {
				return false
			}
		}
	}
	return true
}

var listGroupNames = []string{"active", "planning", "waiting", "history", "completed", "canceled", "coordination"}

func emptyGroups() map[string]int {
	out := map[string]int{}
	for _, name := range listGroupNames {
		out[name] = 0
	}
	return out
}

func catalog(legacy *StoredState, byProject map[string][]Card, in Inputs, now float64) []ProjectRow {
	rows := make([]ProjectRow, 0, len(legacy.Projects))
	for _, p := range legacy.Projects {
		cards := byProject[p.ID]
		groups, human, agent := emptyGroups(), emptyGroups(), emptyGroups()
		var humanCount, humanLanded, agentCount, archived, currentHuman, active int
		var open, needsClarity, landed, coordination, waiting, history, settled int
		updated := 0.0
		for _, c := range cards {
			group := c.ListSummary.Group
			groups[group]++
			switch c.ListSummary.View.Audience {
			case "agent":
				agent[group]++
				agentCount++
			case "archive":
				archived++
			default:
				human[group]++
				humanCount++
				if group == "completed" {
					humanLanded++
				}
				if group == "active" || group == "waiting" || group == "planning" || group == "coordination" {
					currentHuman++
				}
			}
			if c.Progress.Active {
				active++
			}
			switch c.Progress.State {
			case "landed":
				landed++
			case "settled":
				settled++
			}
			if c.Progress.Group == "waiting" {
				waiting++
			}
			if c.Progress.Group == "history" {
				history++
			}
			if c.Type == "coordination" {
				coordination++
			}
			if c.Progress.State == "unknown" || c.Progress.State == "planning" {
				needsClarity++
			}
			if group != "completed" && group != "canceled" {
				open++
			}
			if c.UpdatedAt > updated {
				updated = c.UpdatedAt
			}
		}
		// Archived rows are retained evidence, counted apart from both readers.
		archived += 0
		path := ""
		if in.ProjectPath != nil {
			path = in.ProjectPath(p.ID)
		}
		row := ProjectRow{
			ID: p.ID, Name: p.Name, Label: p.Name, DisplayPath: path,
			IsStartPoint: path != "", ItemCount: len(cards), ActiveItemCount: groups["active"],
			HumanItemCount: humanCount, CurrentHumanItemCount: currentHuman,
			HumanLandedItemCount: humanLanded, AgentRecordCount: agentCount,
			ArchivedRecordCount: len(cards) - groups["active"] - groups["waiting"] - groups["planning"],
			Revision:            in.Settings.Revision, UpdatedAt: updated, ObservedAt: now,
			Summary: map[string]any{
				"active": active, "waiting": waiting, "landed": landed, "history": history,
				"settled": settled, "coordination": coordination, "needsClarity": needsClarity,
				"open": open, "listGroups": groups, "humanListGroups": human, "agentListGroups": agent,
			},
			SummaryCoverage: map[string]any{
				"status": "complete", "retainedCount": len(cards), "omittedCount": 0,
				"humanRetainedCount": humanCount, "agentRetainedCount": agentCount,
				"reasons": []string{},
			},
		}
		if in.ProjectIcon != nil && path != "" {
			row.Icon = in.ProjectIcon(path)
		}
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(a, b int) bool {
		return strings.ToLower(rows[a].Name) < strings.ToLower(rows[b].Name)
	})
	return rows
}

func ingestionObject(coverage []StoredCoverage) map[string]any {
	reasons := map[string]bool{}
	dropped := 0
	issues := make([]map[string]any, 0, len(coverage))
	for _, c := range coverage {
		reasons[c.Reason] = true
		dropped += len(c.SourceDigests)
		issues = append(issues, map[string]any{"kind": c.Kind, "reason": c.Reason,
			"droppedCount": len(c.SourceDigests), "saturated": c.Saturated,
			"firstObservedAt": c.FirstObservedAt})
	}
	sorted := make([]string, 0, len(reasons))
	for r := range reasons {
		sorted = append(sorted, r)
	}
	sort.Strings(sorted)
	status := "complete"
	if len(coverage) > 0 {
		status = "partial"
	}
	return map[string]any{"status": status, "droppedCount": dropped, "reasons": sorted,
		"issues": issues}
}

func unixOrNil(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.Unix()
}

func observedOrNil(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
