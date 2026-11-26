package projectassistanthost

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
	"sync"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// A non-lead Bot dispatches into another Project only after the person
// answers a card. "Always" saves one exact Bot-to-Project rule, shown and
// revoked on the existing saved-permission-rules settings surface.

const dispatchRuleSchema = `CREATE TABLE IF NOT EXISTS project_assistant_dispatch_rules (
	machine TEXT NOT NULL, bot TEXT NOT NULL, project TEXT NOT NULL, folder TEXT NOT NULL,
	PRIMARY KEY(machine,bot,project)
)`

// dispatchRulePrefix starts the visible rule text: the Project ID follows,
// then the folder name for the person. Only the ID is ever parsed back.
const dispatchRulePrefix = "clawdline-dispatch-project:"

const (
	dispatchAllowOnce   = "allow-dispatch-once"
	dispatchAllowAlways = "allow-dispatch-always"
	dispatchDecline     = "decline-dispatch"
)

func isDispatchRule(r pa.PermissionRule) bool { return strings.HasPrefix(r.Rule, dispatchRulePrefix) }

func dispatchRuleText(projectID, folder string) string {
	return dispatchRulePrefix + projectID + " " + folder
}

func dispatchRuleProject(r pa.PermissionRule) (string, error) {
	rest := strings.TrimPrefix(r.Rule, dispatchRulePrefix)
	id, _, _ := strings.Cut(rest, " ")
	if !isDispatchRule(r) || r.Behavior != "allow" || id == "" {
		return "", failure(pa.Invalid, "dispatch_rule")
	}
	return id, nil
}

// dispatchCardID depends only on the turn and the model's idempotency key,
// so a resent call finds the same card and its stored answer.
func dispatchCardID(turnID, key string) string {
	digest := sha256.Sum256([]byte(turnID + "\x00" + key))
	return app.DispatchPermissionPrefix + hex.EncodeToString(digest[:16])
}

// dispatchGrants remembers which destination each card asked about, and
// which cards the person allowed once, for the life of the asking turn.
type dispatchGrants struct {
	mu    sync.Mutex
	cards map[pa.BotKey]map[string]dispatchCard
}

type dispatchCard struct {
	turnID, projectID string
	allowed           bool
}

func (g *dispatchGrants) remember(key pa.BotKey, id string, card dispatchCard) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.cards == nil {
		g.cards = map[pa.BotKey]map[string]dispatchCard{}
	}
	cards := g.cards[key]
	for prior, c := range cards {
		if c.turnID != card.turnID {
			delete(cards, prior)
		}
	}
	if old, ok := cards[id]; ok {
		if old.projectID != card.projectID {
			return failure(pa.IdempotencyConflict, "dispatch_project")
		}
		card.allowed = card.allowed || old.allowed
	} else if len(cards) >= capacity.MaximumBotServerCallerReceipts {
		return failure(pa.CapacityFull, "dispatch_permissions")
	}
	if cards == nil {
		cards = map[string]dispatchCard{}
		g.cards[key] = cards
	}
	cards[id] = card
	return nil
}

func (g *dispatchGrants) lookup(key pa.BotKey, id string) (dispatchCard, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	c, ok := g.cards[key][id]
	return c, ok
}

func (g *dispatchGrants) approved(key pa.BotKey, turnID string) []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	var out []string
	for _, c := range g.cards[key] {
		if c.turnID == turnID && c.allowed {
			out = append(out, c.projectID)
		}
	}
	return out
}

func (h *Host) dispatchRuleTable(ctx context.Context) error {
	_, err := h.o.Database.ExecContext(ctx, dispatchRuleSchema)
	return err
}

// dispatchRules returns this Bot's saved destination Project IDs and folders.
func (h *Host) dispatchRules(ctx context.Context, key pa.BotKey) (map[string]string, error) {
	if err := h.dispatchRuleTable(ctx); err != nil {
		return nil, err
	}
	rows, err := h.o.Database.QueryContext(ctx, `SELECT project, folder FROM project_assistant_dispatch_rules WHERE machine=? AND bot=? ORDER BY project LIMIT ?`, key.MachineID, key.BotID, capacity.MaximumBotDispatchRules)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var project, folder string
		if err := rows.Scan(&project, &folder); err != nil {
			return nil, err
		}
		out[project] = folder
	}
	return out, rows.Err()
}

// saveDispatchRule commits the exact rule before the dispatch it allows.
func (h *Host) saveDispatchRule(ctx context.Context, key pa.BotKey, target pa.ProjectResolution) error {
	if err := h.dispatchRuleTable(ctx); err != nil {
		return err
	}
	rules, err := h.dispatchRules(ctx, key)
	if err != nil {
		return err
	}
	if _, ok := rules[target.Scope.ProjectID]; !ok && len(rules) >= capacity.MaximumBotDispatchRules {
		return failure(pa.CapacityFull, "dispatch_rules")
	}
	_, err = h.o.Database.ExecContext(ctx, `INSERT INTO project_assistant_dispatch_rules(machine,bot,project,folder) VALUES(?,?,?,?) ON CONFLICT(machine,bot,project) DO UPDATE SET folder=excluded.folder`,
		key.MachineID, key.BotID, target.Scope.ProjectID, filepath.Base(target.Directory))
	return err
}

func (h *Host) removeDispatchRule(ctx context.Context, key pa.BotKey, projectID string) error {
	if err := h.dispatchRuleTable(ctx); err != nil {
		return err
	}
	_, err := h.o.Database.ExecContext(ctx, `DELETE FROM project_assistant_dispatch_rules WHERE machine=? AND bot=? AND project=?`, key.MachineID, key.BotID, projectID)
	return err
}

type dispatchAsk struct {
	Fence          pa.Fence
	TurnID         string
	MemoryRevision int64
	BotName        string
	// Key is the model's idempotency key for this dispatch_work call.
	Key, Title string
	Claims     []string
	Target     pa.ProjectResolution
}

// askDispatch returns nil only when the person allowed this exact Bot and
// destination: by a saved rule, or by answering this call's card. A resent
// call with the same key reads that card's stored answer instead of asking
// again. An unanswered card keeps the turn waiting; expiry never dispatches.
func (h *Host) askDispatch(ctx context.Context, r *app.Engine, q dispatchAsk) error {
	key := q.Fence.Key
	rules, err := h.dispatchRules(ctx, key)
	if err != nil {
		return err
	}
	if _, ok := rules[q.Target.Scope.ProjectID]; ok {
		return nil
	}
	id := dispatchCardID(q.TurnID, q.Key)
	if err := h.grants.remember(key, id, dispatchCard{turnID: q.TurnID, projectID: q.Target.Scope.ProjectID}); err != nil {
		return err
	}
	h.mu.Lock()
	st := h.store
	h.mu.Unlock()
	if st == nil {
		return failure(pa.Disabled, "feature")
	}
	stored, err := st.Permission(ctx, key, id)
	if err == nil {
		switch stored.State {
		case pa.PermissionPending:
			return failure(pa.IdempotencyConflict, "dispatch_permission_pending")
		case pa.PermissionResolved:
			for _, option := range stored.Options {
				if option.ID == stored.SelectedOptionID {
					return h.dispatchChoice(ctx, key, id, option, q.Target)
				}
			}
			return failure(pa.OutcomeUnknown, "dispatch_permission")
		default:
			return failure(pa.Expired, "dispatch_permission")
		}
	} else if pa.Code(err) != pa.NotFound {
		return err
	}
	folder := filepath.Base(q.Target.Directory)
	detail := folder + "\n\n" + q.Title
	if len(q.Claims) > 0 {
		detail += "\n\n- " + strings.Join(q.Claims, "\n- ")
	}
	option, err := r.RequestHumanPermission(ctx, app.SourcePermissionRequest{
		Fence: q.Fence, TurnID: q.TurnID, MemoryRevision: q.MemoryRevision,
		Action: pa.PermissionAction{Title: q.BotName + " → " + folder, ToolKind: "dispatch_project", Detail: detail, Diffs: []pa.PermissionDiff{}},
		Options: []pa.PermissionOption{
			{ID: dispatchAllowOnce, Kind: pa.AllowOnce, Allow: true},
			{ID: dispatchAllowAlways, Kind: pa.AllowAlways, Allow: true},
			{ID: dispatchDecline, Kind: pa.RejectOnce},
		},
	}, id)
	if err != nil {
		return err
	}
	return h.dispatchChoice(ctx, key, id, option, q.Target)
}

func (h *Host) dispatchChoice(ctx context.Context, key pa.BotKey, id string, option pa.PermissionOption, target pa.ProjectResolution) error {
	switch option.ID {
	case dispatchAllowAlways:
		// The visible, revocable rule is durable before the dispatch it allows.
		if err := h.saveDispatchRule(ctx, key, target); err != nil {
			return err
		}
		fallthrough
	case dispatchAllowOnce:
		card, ok := h.grants.lookup(key, id)
		if !ok || card.projectID != target.Scope.ProjectID {
			return failure(pa.StaleGeneration, "dispatch_permission")
		}
		card.allowed = true
		return h.grants.remember(key, id, card)
	default:
		return failure(pa.Declined, "dispatch_project")
	}
}
