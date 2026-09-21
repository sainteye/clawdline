package analytics

import (
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// WorktreeQuery is `UsageProjectWorktreeService.parse`.
type WorktreeQuery struct {
	Project string
	Usage   Query
}

// Refusal is a typed answer that is not a reading.
type Refusal struct {
	Status  int
	Code    string
	Message string
}

func (r *Refusal) Error() string { return r.Message }

// ParseWorktrees reads the project-worktrees query. Messages are the Swift app's.
func ParseWorktrees(values url.Values) (WorktreeQuery, error) {
	allowed := map[string]bool{"project": true, "from": true, "to": true, "timezone": true}
	unknown := []string{}
	for k, v := range values {
		if !allowed[k] {
			unknown = append(unknown, k)
			continue
		}
		if len(v) > 1 {
			return WorktreeQuery{}, fmt.Errorf("Project-worktrees query fields may appear only once.")
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return WorktreeQuery{}, fmt.Errorf("Unknown project-worktrees query field: %s.", strings.Join(unknown, ", "))
	}
	project := strings.TrimSpace(values.Get("project"))
	if project == "" {
		return WorktreeQuery{}, fmt.Errorf("project is required: the Portfolio's project id, the Project's final name, or its absolute canonical path.")
	}
	rest := url.Values{}
	for _, k := range []string{"from", "to", "timezone"} {
		if v, ok := values[k]; ok {
			rest[k] = v
		}
	}
	q, err := Parse(rest)
	if err != nil {
		return WorktreeQuery{}, err
	}
	return WorktreeQuery{Project: project, Usage: q}, nil
}

var projectIDShape = regexp.MustCompile(`^project-[0-9a-f]{16}$`)

// Worktrees answers which worktrees under one Project finished a Feature.
// This daemon has no Feature-attribution producer, so no worktree qualifies.
// The read still resolves the Project and says that attribution was not
// measured; an empty list must never claim the measured answer was zero.
func Worktrees(q WorktreeQuery, all []Row, now time.Time) (map[string]any, error) {
	rows, truncated := q.Usage.filter(all, q.Usage.start, q.Usage.end)
	groups, order := groupProjects(rows)
	matches := []string{}
	for _, key := range order {
		g := groups[key]
		if g.identity == "" {
			continue
		}
		id, label := ProjectID(g.identity), projectLabel(g.identity)
		switch {
		case projectIDShape.MatchString(q.Project):
			if id == q.Project {
				matches = append(matches, key)
			}
		case label == q.Project,
			filepath.IsAbs(q.Project) && filepath.Clean(q.Project) == g.identity:
			matches = append(matches, key)
		}
	}
	if len(matches) > 1 {
		ids := []string{}
		for _, k := range matches {
			ids = append(ids, ProjectID(groups[k].identity))
		}
		return nil, &Refusal{409, "ambiguous_project", fmt.Sprintf("%d Projects in this range are named %s. Ask by id: %s.",
			len(matches), q.Project, strings.Join(ids, ", "))}
	}
	if len(matches) == 0 {
		return nil, &Refusal{404, "project_not_found", fmt.Sprintf("No usage row in this range resolves to a Project named %s. %d row(s) were read.",
			q.Project, len(rows))}
	}
	g := groups[matches[0]]
	worktrees := map[string]bool{}
	worktreeRows := 0
	for _, r := range g.rows {
		if id := LegacyWorktreeID(r.WorkingDir); id != "" {
			worktrees[id] = true
			worktreeRows++
		}
	}
	unattributed := map[string]bool{}
	reasons := map[string]any{}
	for _, r := range rows {
		id, why := identity(r)
		if id != "" {
			continue
		}
		if wt := LegacyWorktreeID(r.WorkingDir); wt != "" {
			unattributed[wt] = true
			n, _ := reasons[why].(int)
			reasons[why] = n + 1
		}
	}
	return map[string]any{
		"schemaVersion": 1,
		"status":        "not_measured",
		"policy":        "one_unambiguous_accepted_head",
		"outcomeRule":   "landed_by_record_then_landed_by_nonempty_merged_branch_then_settled_then_branch_gone_then_delivered_then_live_then_abandoned",
		"generatedAt":   iso(now),
		"range":         map[string]any{"timezone": q.Usage.Zone, "from": strOrNil(q.Usage.From), "to": strOrNil(q.Usage.To)},
		"project":       map[string]any{"id": ProjectID(g.identity), "label": projectLabel(g.identity)},
		"read": map[string]any{"rows": len(rows), "projectRows": len(g.rows), "worktreeRows": worktreeRows,
			"featureRowsStatus": "not_measured", "truncated": truncated, "maxScannedRows": MaxScannedRows},
		"worktrees":    []any{},
		"excluded":     map[string]any{"worktreesWithoutFeature": len(worktrees), "reason": "feature_attribution_not_measured"},
		"unattributed": map[string]any{"worktrees": len(unattributed), "reasons": reasons},
	}, nil
}
