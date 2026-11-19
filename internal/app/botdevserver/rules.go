package botdevserver

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type Decision string

const (
	Allow Decision = "allow"
	Deny  Decision = "deny"
)

// Rule belongs to one immutable local definition, never to a native turn.
// Revocation retains Version, so an old card cannot restore a revoked grant.
type Rule struct {
	ServerID string   `json:"server_id"`
	Spec     Spec     `json:"spec"`
	Version  int64    `json:"version"`
	Decision Decision `json:"decision"`
}

const rulePrefix = "clawdline.dev-server.v1:"

func IsSourceRule(r pa.PermissionRule) bool { return strings.HasPrefix(r.Rule, rulePrefix) }

func (r Rule) PermissionRule() (pa.PermissionRule, error) {
	if err := r.validate(); err != nil {
		return pa.PermissionRule{}, err
	}
	if r.Decision == "" {
		return pa.PermissionRule{}, failure(pa.Invalid, "server_rule")
	}
	b, err := json.Marshal(r)
	if err != nil {
		return pa.PermissionRule{}, err
	}
	out := pa.PermissionRule{Behavior: string(r.Decision), Rule: rulePrefix + string(b)}
	return out, pa.ValidateRule(out)
}

func ParseRule(p pa.PermissionRule) (Rule, error) {
	var r Rule
	if pa.ValidateRule(p) != nil || !IsSourceRule(p) {
		return r, failure(pa.Invalid, "server_rule")
	}
	d := json.NewDecoder(bytes.NewBufferString(strings.TrimPrefix(p.Rule, rulePrefix)))
	d.DisallowUnknownFields()
	if err := d.Decode(&r); err != nil {
		return Rule{}, failure(pa.Invalid, "server_rule")
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return Rule{}, failure(pa.Invalid, "server_rule")
	}
	if r.validate() != nil || r.Decision == "" || p.Behavior != string(r.Decision) {
		return Rule{}, failure(pa.Invalid, "server_rule")
	}
	return r, nil
}

func (r Rule) validate() error {
	if r.ServerID == "" || r.Version < 0 || (r.Decision != "" && r.Decision != Allow && r.Decision != Deny) || (r.Version == 0 && r.Decision != "") {
		return failure(pa.Invalid, "server_rule")
	}
	return r.Spec.Validate()
}
