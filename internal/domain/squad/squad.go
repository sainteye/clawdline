// Package squad defines versioned persona, team, skill, and setting contracts.
package squad

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"github.com/sainteye/clawdline/internal/domain/persona"
)

const (
	MaxSquadEntities     = 1024
	MaxSquadSettingsRows = 10000
	MaxSquadReceipts     = 10000
	MaxSquadBodyBytes    = 64 << 10
	MaxSquadRequestBytes = 256 << 10
	MaxSquadPlaceLookup  = 1024
	// Mirrored by the Console before it lets a person enable another skill.
	MaxSquadConsoleActiveSkillBytes = 512 << 10
)

const BuiltinPrefix = "clawdline.persona."

type Names struct {
	En     string `json:"en"`
	ZhHant string `json:"zh-Hant"`
}

type Icon struct {
	Accent string      `json:"accent"`
	Cells  [][]*string `json:"cells"`
}

type Reference struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

type SkillReference struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Enabled bool   `json:"enabled"`
}

type Definition struct {
	DefinitionID string           `json:"definition_id"`
	ShortID      string           `json:"short_id,omitempty"`
	Version      string           `json:"version"`
	Source       string           `json:"source"`
	License      string           `json:"license"`
	Digest       string           `json:"digest"`
	Name         Names            `json:"name"`
	Summary      Names            `json:"summary"`
	Body         string           `json:"body"`
	Icon         Icon             `json:"icon"`
	Teams        []Reference      `json:"teams"`
	Skills       []SkillReference `json:"skills"`
	Builtin      bool             `json:"builtin"`
}

type Team struct {
	TeamID   string      `json:"team_id"`
	Version  string      `json:"version"`
	Source   string      `json:"source"`
	License  string      `json:"license"`
	Digest   string      `json:"digest"`
	Name     Names       `json:"name"`
	Icon     Icon        `json:"icon"`
	Personas []Reference `json:"personas"`
	Builtin  bool        `json:"builtin"`
}

type Skill struct {
	SkillID string `json:"skill_id"`
	Version string `json:"version"`
	Source  string `json:"source"`
	License string `json:"license"`
	Digest  string `json:"digest"`
	Name    Names  `json:"name"`
	Purpose Names  `json:"purpose"`
	Icon    Icon   `json:"icon"`
	Content string `json:"content"`
	Builtin bool   `json:"builtin"`
}

type Catalog struct {
	CatalogVersion int64        `json:"catalog_version"`
	Definitions    []Definition `json:"definitions"`
	Teams          []Team       `json:"teams"`
	Skills         []Skill      `json:"skills"`
}

func BuiltinID(short string) string { return BuiltinPrefix + short }

// ResolveID accepts a short ID only as a compatibility alias for a built-in.
func ResolveID(id string) (string, bool) {
	if _, ok := persona.Known(id); ok {
		return BuiltinID(id), true
	}
	if strings.HasPrefix(id, BuiltinPrefix) {
		_, ok := persona.Known(strings.TrimPrefix(id, BuiltinPrefix))
		return id, ok
	}
	return id, false
}

func Digest(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func builtinDefinitionDigest(d Definition) string {
	if len(d.Skills) == 0 {
		return Digest(struct {
			Body   string
			Source string
			Teams  []Reference
		}{d.Body, d.Source, d.Teams})
	}
	return Digest(struct {
		Body   string
		Source string
		Teams  []Reference
		Skills []SkillReference
	}{d.Body, d.Source, d.Teams, d.Skills})
}

// PreSkillBuiltinVersion preserves the definition that existing clients may
// have cached before reviewed skills were linked to their built-in role.
func PreSkillBuiltinVersion(d Definition) Definition {
	d.Skills = []SkillReference{}
	d.Digest = builtinDefinitionDigest(d)
	d.Version = "sha256:" + d.Digest
	return d
}

// Builtins converts the closed launch catalog into immutable full definitions.
// Reviewed role skills are bundled locally and never fetched at launch time.
func Builtins() Catalog {
	out := Catalog{Definitions: []Definition{}, Teams: []Team{}, Skills: []Skill{}}
	skillByPersona := builtinSkills()
	for _, spec := range builtinSkillSpecs {
		out.Skills = append(out.Skills, skillByPersona[spec.Persona])
	}
	for _, name := range persona.Teams {
		cells := make([][]*string, 7)
		for row := range cells {
			cells[row] = make([]*string, 8)
		}
		out.Teams = append(out.Teams, Team{
			TeamID: "clawdline.team." + name, Version: "1", Source: "clawdline",
			License: persona.UpstreamLicense, Name: Names{En: name, ZhHant: name},
			Icon:     Icon{Accent: "#777777", Cells: cells},
			Personas: []Reference{}, Builtin: true,
		})
	}
	for _, p := range persona.All() {
		teams := make([]Reference, 0, len(p.Teams))
		for _, team := range p.Teams {
			teams = append(teams, Reference{ID: "clawdline.team." + team, Version: "1"})
		}
		refs := []SkillReference{}
		if skill, ok := skillByPersona[p.ID]; ok {
			refs = append(refs, SkillReference{ID: skill.SkillID, Version: skill.Version, Enabled: true})
		}
		d := Definition{
			DefinitionID: BuiltinID(p.ID), ShortID: p.ID,
			Source: p.Source, License: persona.UpstreamLicense,
			Name:    Names{En: p.Name.En, ZhHant: p.Name.ZhHant},
			Summary: Names{En: p.Summary.En, ZhHant: p.Summary.ZhHant},
			Body:    p.Text(), Icon: Icon{Accent: p.Icon.Accent, Cells: p.Icon.Cells},
			Teams: teams, Skills: refs, Builtin: true,
		}
		d.Digest = builtinDefinitionDigest(d)
		d.Version = "sha256:" + d.Digest
		out.Definitions = append(out.Definitions, d)
		for _, team := range p.Teams {
			for i := range out.Teams {
				if out.Teams[i].TeamID == "clawdline.team."+team {
					out.Teams[i].Personas = append(out.Teams[i].Personas,
						Reference{ID: d.DefinitionID, Version: d.Version})
					break
				}
			}
		}
	}
	for i := range out.Teams {
		out.Teams[i].Digest = Digest(out.Teams[i].Personas)
	}
	return out
}

var namespacedID = regexp.MustCompile(`^[a-z][a-z0-9-]*(\.[a-z][a-z0-9-]*)+$`)

func ValidCustomID(id string) bool {
	return namespacedID.MatchString(id) && !strings.HasPrefix(id, "clawdline.")
}

type Choice struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Enabled bool   `json:"enabled"`
}

type Override struct {
	Handbook   *string   `json:"handbook"`
	AutoAssign *bool     `json:"auto_assign"`
	Skills     *[]Choice `json:"skills"`
}

// Patch names exactly the fields a settings write touches. A nil value with
// Set* true removes that field's presence marker; Set* false leaves it alone.
type Patch struct {
	SetHandbook   bool
	Handbook      *string
	SetAutoAssign bool
	AutoAssign    *bool
	SetSkills     bool
	Skills        *[]Choice
}

type MotionOverride struct {
	Motion *bool `json:"motion"`
}

type Field[T any] struct {
	Value   T      `json:"value"`
	Source  string `json:"source"`
	Present bool   `json:"present"`
	Version int64  `json:"version"`
}

type EffectivePersona struct {
	DefinitionID   string          `json:"definition_id"`
	Version        int64           `json:"settings_version"`
	Handbook       Field[string]   `json:"handbook"`
	GlobalHandbook Field[string]   `json:"global_handbook"`
	AutoAssign     Field[bool]     `json:"auto_assign"`
	Skills         Field[[]Choice] `json:"skills"`
}

type Effective struct {
	PlaceID               string             `json:"place_id,omitempty"`
	ScopeID               string             `json:"scope_id"`
	ScopeKind             string             `json:"scope_kind"`
	Motion                Field[bool]        `json:"motion"`
	MotionSettingsVersion int64              `json:"motion_settings_version"`
	Personas              []EffectivePersona `json:"personas"`
}

type ScopeInfo struct {
	PlaceID string `json:"place_id,omitempty"`
	ScopeID string `json:"scope_id"`
	Kind    string `json:"kind"`
	Label   string `json:"label"`
	Path    string `json:"path,omitempty"`
}

type ScopeList struct {
	Scopes   []ScopeInfo `json:"scopes"`
	ScopeIDs []string    `json:"scope_ids"`
}

// LaunchSnapshot is the immutable value read at session start. The launch
// owner persists these exact bytes; reading it never changes squad settings.
type LaunchSnapshot struct {
	DefinitionID      string         `json:"definition_id"`
	ScopeID           string         `json:"scope_id"`
	ScopeKind         string         `json:"scope_kind"`
	Definition        Definition     `json:"definition"`
	Handbook          LaunchHandbook `json:"handbook"`
	AutoAssignEnabled bool           `json:"auto_assign_enabled"`
	Skills            []LaunchSkill  `json:"skills"`
}

type LaunchHandbook struct {
	Text    string `json:"text"`
	Source  string `json:"source"`
	Present bool   `json:"present"`
	Version int64  `json:"version"`
}

type LaunchSkill struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Enabled bool   `json:"enabled"`
	Skill
}

func Resolve[T any](def T, global *T, globalVersion int64, project *T, projectVersion int64) Field[T] {
	f := Field[T]{Value: def, Source: "default", Present: false}
	if global != nil {
		f = Field[T]{Value: *global, Source: "global", Present: true, Version: globalVersion}
	}
	if project != nil {
		f = Field[T]{Value: *project, Source: "project", Present: true, Version: projectVersion}
	}
	return f
}

type Actor struct {
	Verified     bool
	DefinitionID string
	ScopeID      string
}

var ErrActorUnverified = errors.New("actor_unverified")
var ErrActorNotManager = errors.New("actor_not_manager")

// Candidates accepts only an already authenticated session actor. HTTP request
// fields such as conversation_id cannot construct one.
func Candidates(actor Actor, effective Effective) ([]string, error) {
	if !actor.Verified {
		return nil, ErrActorUnverified
	}
	if actor.DefinitionID != BuiltinID("product-manager") && actor.DefinitionID != BuiltinID("architect") {
		return nil, ErrActorNotManager
	}
	if actor.ScopeID != effective.ScopeID {
		return nil, ErrActorUnverified
	}
	out := []string{}
	for _, p := range effective.Personas {
		if p.AutoAssign.Value {
			out = append(out, p.DefinitionID)
		}
	}
	return out, nil
}
