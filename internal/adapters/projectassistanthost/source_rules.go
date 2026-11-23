package projectassistanthost

import (
	"context"
	"slices"
	"strings"

	serverstore "github.com/sainteye/clawdline/internal/adapters/botdevserverstore"
	servers "github.com/sainteye/clawdline/internal/app/botdevserver"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type hostSourceRules struct{ h *Host }

func (hostSourceRules) Handles(r pa.PermissionRule) bool {
	return servers.IsSourceRule(r) || isDispatchRule(r)
}

// serverMemory publishes local rule/process effects against the current
// memory lock. Ordinary saves may advance Revision; only Forget changes the
// command's identity. Never hold this lock while waiting for a human answer.
func (h *Host) serverMemory(ctx context.Context, spec servers.Spec, publish func() error) error {
	if publish == nil {
		return failure(pa.Invalid, "server_publication")
	}
	h.mu.Lock()
	st := h.store
	h.mu.Unlock()
	if st == nil {
		return failure(pa.Disabled, "feature")
	}
	m, err := st.Current(ctx, spec.Key)
	if err != nil {
		return err
	}
	return st.WithCurrent(ctx, spec.Key, m.Revision, func(snapshot pa.ContextSnapshot) error {
		if err := spec.CheckMemory(snapshot.Memory); err != nil {
			return err
		}
		return publish()
	})
}

// Human history remains readable after Forget; this is not model input.
func (s hostSourceRules) List(ctx context.Context, key pa.BotKey, project pa.ProjectScope) ([]pa.PermissionRule, error) {
	c, err := serverstore.NewCatalog(ctx, s.h.o.Database)
	if err != nil {
		return nil, err
	}
	rows, err := c.List(ctx, capacity.MaximumBotServerDefinitions)
	if err != nil {
		return nil, err
	}
	out := []pa.PermissionRule{}
	// Bot-to-Project dispatch rules belong to the Bot, shown on its own Project.
	if project == s.h.botProject(ctx, key) {
		dispatch, err := s.h.dispatchRules(ctx, key)
		if err != nil {
			return nil, err
		}
		for id, folder := range dispatch {
			out = append(out, pa.PermissionRule{Behavior: "allow", Rule: dispatchRuleText(id, folder)})
		}
		slices.SortFunc(out, func(a, b pa.PermissionRule) int { return strings.Compare(a.Rule, b.Rule) })
	}
	for _, saved := range rows {
		if saved.Spec.Key != key || saved.Spec.Project != project {
			continue
		}
		rule, err := c.Rule(ctx, saved)
		if err != nil {
			return nil, err
		}
		if rule.Decision == "" {
			continue
		}
		wire, err := rule.PermissionRule()
		if err != nil {
			return nil, err
		}
		out = append(out, wire)
	}
	return out, nil
}

func (s hostSourceRules) Remove(ctx context.Context, key pa.BotKey, project pa.ProjectScope, wire pa.PermissionRule) error {
	if isDispatchRule(wire) {
		id, err := dispatchRuleProject(wire)
		if err != nil {
			return err
		}
		if project != s.h.botProject(ctx, key) {
			return failure(pa.ScopeDenied, "dispatch_rule")
		}
		return s.h.removeDispatchRule(ctx, key, id)
	}
	rule, err := servers.ParseRule(wire)
	if err != nil {
		return err
	}
	if rule.Spec.Key != key || rule.Spec.Project != project {
		return failure(pa.ScopeDenied, "server_rule")
	}
	c, err := serverstore.NewCatalog(ctx, s.h.o.Database)
	if err != nil {
		return err
	}
	_, err = c.SaveRule(ctx, rule, "")
	return err
}

// botProject is the Bot's own Project, or zero when it cannot be read.
func (h *Host) botProject(ctx context.Context, key pa.BotKey) pa.ProjectScope {
	h.mu.Lock()
	st := h.store
	h.mu.Unlock()
	if st == nil {
		return pa.ProjectScope{}
	}
	b, err := st.Bot(ctx, key)
	if err != nil {
		return pa.ProjectScope{}
	}
	return b.Project
}
