package http

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/domain/squad"
)

var ErrSquadLaunchProject = errors.New("unknown_project")
var ErrSquadLaunchDefinition = errors.New("unknown_definition")
var ErrSquadLaunchSkill = errors.New("unknown_skill_version")

// ResolveSquadLaunch reads the complete, versioned value for one session start.
// Its caller owns publication and persistence of the returned snapshot. An
// empty projectPath selects global settings; a nonempty path must resolve to
// an absolute Project path.
func (s *Server) ResolveSquadLaunch(ctx context.Context, personaID, projectPath string) (squad.LaunchSnapshot, error) {
	scope := projects.Scope{Kind: "global", ID: "global"}
	if projectPath != "" {
		resolved, ok := projects.ResolveScope(projectPath)
		if !ok {
			return squad.LaunchSnapshot{}, ErrSquadLaunchProject
		}
		scope = resolved
	}
	catalog, err := s.squadCatalog(ctx)
	if err != nil {
		return squad.LaunchSnapshot{}, err
	}
	id, _ := squad.ResolveID(personaID)
	var definition *squad.Definition
	for i := range catalog.Definitions {
		if catalog.Definitions[i].DefinitionID == id {
			definition = &catalog.Definitions[i]
			break
		}
	}
	if definition == nil {
		return squad.LaunchSnapshot{}, ErrSquadLaunchDefinition
	}
	effective, err := s.squadEffective(ctx, scope, "")
	if err != nil {
		return squad.LaunchSnapshot{}, err
	}
	var choice *squad.EffectivePersona
	for i := range effective.Personas {
		if effective.Personas[i].DefinitionID == id {
			choice = &effective.Personas[i]
			break
		}
	}
	if choice == nil {
		return squad.LaunchSnapshot{}, ErrSquadLaunchDefinition
	}
	out := squad.LaunchSnapshot{
		DefinitionID: id, ScopeID: scope.ID, ScopeKind: scope.Kind,
		Definition: *definition,
		Handbook: squad.LaunchHandbook{
			Text: choice.Handbook.Value, Source: choice.Handbook.Source,
			Present: choice.Handbook.Present, Version: choice.Handbook.Version,
		},
		AutoAssignEnabled: choice.AutoAssign.Value,
		Skills:            []squad.LaunchSkill{},
	}
	for _, ref := range choice.Skills.Value {
		var selected *squad.Skill
		for i := range catalog.Skills {
			if catalog.Skills[i].SkillID == ref.ID && catalog.Skills[i].Version == ref.Version {
				selected = &catalog.Skills[i]
				break
			}
		}
		if selected == nil {
			row, ok, err := s.store.SquadEntityVersion(ctx, "skill", ref.ID, ref.Version)
			if err != nil {
				return squad.LaunchSnapshot{}, err
			}
			if !ok {
				return squad.LaunchSnapshot{}, ErrSquadLaunchSkill
			}
			var historical squad.Skill
			if err := json.Unmarshal(row.Payload, &historical); err != nil ||
				historical.SkillID != ref.ID || historical.Version != ref.Version ||
				historical.Digest != row.Digest {
				return squad.LaunchSnapshot{}, ErrSquadLaunchSkill
			}
			selected = &historical
		}
		out.Skills = append(out.Skills, squad.LaunchSkill{
			ID: ref.ID, Version: ref.Version, Enabled: ref.Enabled, Skill: *selected,
		})
	}
	return out, nil
}
