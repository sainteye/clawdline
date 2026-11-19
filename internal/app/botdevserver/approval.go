package botdevserver

import (
	"context"
	"encoding/json"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type RuleStore interface {
	Rule(context.Context, SavedServer) (Rule, error)
	SaveRule(context.Context, Rule, Decision) (Rule, error)
}

// Approval is composed for one native execution, while its returned check is
// Host-owned. Review must be the private durable source-card callback, never a
// model tool or a vendor permission answer. Guard serializes publication with
// the current Forget boundary without holding that lock during human review.
type Approval struct {
	Rules  RuleStore
	Check  func(context.Context, Spec) error
	Guard  func(context.Context, Spec, func() error) error
	Review func(context.Context, pa.PermissionAction, []pa.PermissionOption) (pa.PermissionOption, error)
}

func (a Approval) Authorize(ctx context.Context, saved SavedServer) (func(context.Context) error, error) {
	if a.Rules == nil || a.Check == nil || a.Guard == nil || a.Review == nil || saved.ID == "" {
		return nil, failure(pa.Invalid, "server_approval")
	}
	if err := saved.Spec.Validate(); err != nil {
		return nil, err
	}
	if err := a.Check(ctx, saved.Spec); err != nil {
		return nil, err
	}
	rule, err := a.Rules.Rule(ctx, saved)
	if err != nil {
		return nil, err
	}
	if rule.validate() != nil || rule.ServerID != saved.ID || rule.Spec != saved.Spec {
		return nil, failure(pa.ScopeDenied, "server_rule")
	}
	if rule.Decision == Deny {
		return nil, failure(pa.Revoked, "server_rule")
	}
	if rule.Decision == "" {
		// Full structured content is displayed without shortening. Identity,
		// canonical directory and command all belong to the exact saved rule.
		detail, err := json.MarshalIndent(saved.Spec, "", "  ")
		if err != nil {
			return nil, err
		}
		options := []pa.PermissionOption{
			{ID: string(pa.AllowOnce), Kind: pa.AllowOnce, Allow: true},
			{ID: string(pa.AllowAlways), Kind: pa.AllowAlways, Allow: true},
			{ID: string(pa.RejectOnce), Kind: pa.RejectOnce},
			{ID: string(pa.RejectAlways), Kind: pa.RejectAlways},
		}
		choice, err := a.Review(ctx, pa.PermissionAction{Title: saved.Spec.Name, ToolKind: "source_command", Detail: string(detail), Diffs: []pa.PermissionDiff{}}, options)
		if err != nil {
			return nil, err
		}
		valid := false
		for _, option := range options {
			if choice == option {
				valid = true
			}
		}
		if !valid {
			return nil, failure(pa.Invalid, "server_choice")
		}
		if err := a.Check(ctx, saved.Spec); err != nil {
			return nil, err
		}
		if choice.Kind == pa.RejectOnce {
			return nil, failure(pa.Revoked, "server_approval")
		}
		if choice.Kind == pa.AllowAlways || choice.Kind == pa.RejectAlways {
			decision := Allow
			if choice.Kind == pa.RejectAlways {
				decision = Deny
			}
			err = a.Guard(ctx, saved.Spec, func() error {
				var err error
				rule, err = a.Rules.SaveRule(ctx, rule, decision)
				return err
			})
			if err != nil {
				return nil, err
			}
			if decision == Deny {
				return nil, failure(pa.Revoked, "server_rule")
			}
		}
	}
	// Recheck exact version even for a one-time choice: a concurrent Never or
	// revocation must not be overwritten or ignored by an old review.
	// Keep only Host-owned functions in the retained launch check. The review
	// callback belongs to the native execution and must not be retained by it.
	checkSource, guard, rules := a.Check, a.Guard, a.Rules
	check := func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := checkSource(ctx, saved.Spec); err != nil {
			return err
		}
		return guard(ctx, saved.Spec, func() error {
			current, err := rules.Rule(ctx, saved)
			if err != nil {
				return err
			}
			if current != rule {
				return failure(pa.StaleVersion, "server_rule")
			}
			if current.Decision == Deny {
				return failure(pa.Revoked, "server_rule")
			}
			return nil
		})
	}
	if err := check(ctx); err != nil {
		return nil, err
	}
	return check, nil
}
