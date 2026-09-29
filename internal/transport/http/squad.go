package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	"github.com/sainteye/clawdline/internal/domain/squad"
)

// /v1/squad is the private, versioned counterpart to the compatible summary
// at /v1/personas. The gate admits paired readers; only Local or Send may write.
func (s *Server) squadRoute(w http.ResponseWriter, r *http.Request) {
	switch routePath(r) {
	case "/v1/squad/catalog":
		if r.Method == http.MethodGet {
			s.squadCatalogRead(w, r)
		} else if r.Method == http.MethodPost {
			s.squadEntityWrite(w, r)
		} else {
			writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "Use GET or POST.")
		}
	case "/v1/squad/settings":
		if r.Method == http.MethodGet {
			s.squadSettingsRead(w, r)
		} else if r.Method == http.MethodPut {
			s.squadSettingsWrite(w, r)
		} else {
			writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "Use GET or PUT.")
		}
	case "/v1/squad/motion":
		if r.Method == http.MethodPut {
			s.squadMotionWrite(w, r)
		} else {
			writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "Use PUT.")
		}
	case "/v1/squad/scopes":
		if r.Method != http.MethodGet {
			writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "Use GET.")
			return
		}
		s.squadScopesRead(w, r)
	case "/v1/squad/auto-candidates":
		s.squadCandidateRead(w, r)
	default:
		if strings.HasPrefix(routePath(r), "/v1/squad/definitions/") && r.Method == http.MethodGet {
			s.squadDefinitionRead(w, r)
			return
		}
		writeRefusal(w, http.StatusNotFound, "not_found", "No such squad route.")
	}
}

func (s *Server) squadCandidateRead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "Use GET.")
		return
	}
	if s.squadActor == nil {
		writeRefusal(w, http.StatusForbidden, "actor_unverified",
			"A bound Session capability is required.")
		return
	}
	actor, err := s.squadActor(r)
	if err != nil || !actor.Verified || actor.ScopeID == "" {
		writeRefusal(w, http.StatusForbidden, "actor_unverified",
			"A bound Session capability is required.")
		return
	}
	kind := projects.ScopeRepository
	if strings.HasPrefix(actor.ScopeID, "place:") {
		kind = projects.ScopePlace
	}
	effective, err := s.squadEffective(r.Context(),
		projects.Scope{Kind: kind, ID: actor.ScopeID}, "")
	if err != nil {
		squadFailure(w, err, true)
		return
	}
	candidates, err := squad.Candidates(actor, effective)
	if errors.Is(err, squad.ErrActorNotManager) {
		writeRefusal(w, http.StatusForbidden, "actor_not_manager",
			"Only a verified management Session may request automatic candidates.")
		return
	}
	if err != nil {
		writeRefusal(w, http.StatusForbidden, "actor_unverified",
			"A bound Session capability is required.")
		return
	}
	writeJSON(w, contract.SquadCandidates{ScopeID: actor.ScopeID, DefinitionIds: candidates})
}

func squadWriteAllowed(r *http.Request) bool {
	a := accessOf(r)
	return a.verdict.Allowed && (a.verdict.Local || maySend(r))
}

func squadWriteDoor(w http.ResponseWriter, r *http.Request) (string, bool) {
	if !squadWriteAllowed(r) {
		writeRefusal(w, http.StatusForbidden, "forbidden", "Squad changes need a write-capable person.")
		return "", false
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" || len(key) > 200 {
		writeRefusal(w, http.StatusBadRequest, "idempotency_key_required",
			"Every squad write needs an Idempotency-Key of at most 200 characters.")
		return "", false
	}
	// The credential-derived principal separates two writers' retry keys.
	principal := personPrincipal(r)
	if accessOf(r).verdict.Local {
		principal = "local"
	}
	return principal + ":" + key, true
}

func squadBody(w http.ResponseWriter, r *http.Request, out any) bool {
	raw, err := io.ReadAll(io.LimitReader(r.Body, CapacityLimit(capacity.SquadRequestBytes)+1))
	if err != nil {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "The squad request could not be read.")
		return false
	}
	if int64(len(raw)) > CapacityLimit(capacity.SquadRequestBytes) {
		writeRefusal(w, http.StatusRequestEntityTooLarge, "body_too_large", "The squad request is too large.")
		return false
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil || dec.Decode(new(any)) != io.EOF {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "The squad request is not one JSON object of the expected shape.")
		return false
	}
	return true
}

func squadLimits() store.SquadLimits {
	return store.SquadLimits{
		Entities:     CapacityLimit(capacity.SquadEntities),
		SettingsRows: CapacityLimit(capacity.SquadSettingsRows),
		Receipts:     CapacityLimit(capacity.SquadReceipts),
	}
}

func squadFailure(w http.ResponseWriter, err error, read bool) {
	var conflict store.SquadVersionConflict
	switch {
	case errors.As(err, &conflict):
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(contract.SquadRefusal{
			Error: "version_conflict", Detail: "The settings changed; reload the current version.",
			CurrentVersion: conflict.Current})
	case errors.Is(err, store.ErrSquadReceiptConflict):
		writeRefusal(w, http.StatusConflict, "idempotency_conflict", "That retry key already names a different write.")
	case errors.Is(err, store.ErrSquadVersionContent):
		writeRefusal(w, http.StatusConflict, "definition_version_conflict", "That immutable version already has different content.")
	case errors.Is(err, store.ErrSquadCapacityFull):
		writeRefusal(w, http.StatusInsufficientStorage, "capacity_full", "The squad store is full.")
	case errors.Is(err, store.ErrBusy):
		writeRefusal(w, http.StatusTooManyRequests, "busy", "Another writer held the store; retry.")
	case read:
		writeRefusal(w, http.StatusInternalServerError, "store_unreadable", "The squad store could not be read.")
	default:
		writeRefusal(w, http.StatusInternalServerError, "write_failed", "The squad change could not be saved.")
	}
}

func (s *Server) squadKnownPlace(ctx context.Context, placeID string) (projects.Scope, bool) {
	if s.squadPlace != nil {
		return s.squadPlace(ctx, placeID)
	}
	for _, p := range s.squadListedPlaces(ctx) {
		if p.ID == placeID {
			return projects.ResolveScope(p.Path)
		}
	}
	return projects.Scope{}, false
}

func (s *Server) squadListedPlaces(ctx context.Context) []projects.Place {
	if s.squadPlaces != nil {
		return s.squadPlaces(ctx)
	}
	return s.projectReaders().places.List(s.liveDirectories(ctx),
		int(CapacityLimit(capacity.SquadPlaceLookup)))
}

func (s *Server) squadScopesRead(w http.ResponseWriter, r *http.Request) {
	saved, err := s.store.SquadScopes(r.Context())
	if err != nil {
		squadFailure(w, err, true)
		return
	}
	out := squad.ScopeList{Scopes: []squad.ScopeInfo{}, ScopeIDs: saved}
	seen := map[string]bool{}
	for _, p := range s.squadListedPlaces(r.Context()) {
		scope, ok := projects.ResolveScope(p.Path)
		if !ok {
			continue
		}
		out.Scopes = append(out.Scopes, squad.ScopeInfo{
			PlaceID: p.ID, ScopeID: scope.ID, Kind: scope.Kind,
			Label: p.Label, Path: p.Path})
		seen[scope.ID] = true
	}
	for _, id := range saved {
		if seen[id] {
			continue
		}
		kind := projects.ScopeRepository
		if strings.HasPrefix(id, "place:") {
			kind = projects.ScopePlace
		}
		out.Scopes = append(out.Scopes, squad.ScopeInfo{
			ScopeID: id, Kind: kind, Label: id})
	}
	writeJSON(w, out)
}

func (s *Server) squadScope(ctx context.Context, placeID, scopeID string) (projects.Scope, error) {
	if placeID != "" && scopeID != "" {
		return projects.Scope{}, errors.New("scope_mismatch")
	}
	if placeID == "" && scopeID == "" {
		return projects.Scope{Kind: "global", ID: "global"}, nil
	}
	if placeID != "" {
		scope, ok := s.squadKnownPlace(ctx, placeID)
		if !ok {
			return projects.Scope{}, errors.New("unknown_project")
		}
		return scope, nil
	}
	ids, err := s.store.SquadScopes(ctx)
	if err != nil {
		return projects.Scope{}, err
	}
	for _, id := range ids {
		if id == scopeID {
			kind := projects.ScopeRepository
			if strings.HasPrefix(id, "place:") {
				kind = projects.ScopePlace
			}
			return projects.Scope{Kind: kind, ID: id}, nil
		}
	}
	return projects.Scope{}, errors.New("unknown_project")
}

func squadScopeRefusal(w http.ResponseWriter, err error) {
	if err == nil {
		return
	}
	switch err.Error() {
	case "scope_mismatch":
		writeRefusal(w, http.StatusBadRequest, "scope_mismatch", "Name either place_id or scope_id.")
	case "unknown_project":
		writeRefusal(w, http.StatusNotFound, "unknown_project", "That Project is not known to this machine.")
	default:
		squadFailure(w, err, true)
	}
}

func (s *Server) squadCatalog(ctx context.Context) (squad.Catalog, error) {
	version, rows, err := s.store.SquadCatalogRows(ctx, false)
	if err != nil {
		return squad.Catalog{}, err
	}
	out := squad.Builtins()
	out.CatalogVersion = version
	for _, row := range rows {
		switch row.Kind {
		case "definition":
			var d squad.Definition
			if err := json.Unmarshal(row.Payload, &d); err != nil || d.DefinitionID != row.ID ||
				d.Version != row.Version || d.Digest != row.Digest {
				return squad.Catalog{}, errors.New("invalid catalog definition")
			}
			out.Definitions = append(out.Definitions, d)
		case "team":
			var d squad.Team
			if err := json.Unmarshal(row.Payload, &d); err != nil || d.TeamID != row.ID ||
				d.Version != row.Version || d.Digest != row.Digest {
				return squad.Catalog{}, errors.New("invalid catalog team")
			}
			out.Teams = append(out.Teams, d)
		case "skill":
			var d squad.Skill
			if err := json.Unmarshal(row.Payload, &d); err != nil || d.SkillID != row.ID ||
				d.Version != row.Version || d.Digest != row.Digest {
				return squad.Catalog{}, errors.New("invalid catalog skill")
			}
			out.Skills = append(out.Skills, d)
		default:
			return squad.Catalog{}, errors.New("invalid catalog kind")
		}
	}
	return out, nil
}

func (s *Server) squadCatalogRead(w http.ResponseWriter, r *http.Request) {
	c, err := s.squadCatalog(r.Context())
	if err != nil {
		squadFailure(w, err, true)
		return
	}
	writeJSON(w, c)
}

func (s *Server) squadDefinitionRead(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(routePath(r), "/v1/squad/definitions/")
	if id == "" || strings.Contains(id, "/") {
		writeRefusal(w, http.StatusNotFound, "unknown_definition", "No definition has that ID.")
		return
	}
	c, err := s.squadCatalog(r.Context())
	if err != nil {
		squadFailure(w, err, true)
		return
	}
	resolved, _ := squad.ResolveID(id)
	version := r.URL.Query().Get("version")
	for _, d := range c.Definitions {
		if d.DefinitionID == resolved && (version == "" || version == d.Version) {
			writeJSON(w, d)
			return
		}
		if d.DefinitionID == resolved && version != "" && d.Builtin && len(d.Skills) > 0 {
			if historical, ok := squad.HistoricalBuiltinDefinition(d); ok && version == historical.Version {
				writeJSON(w, historical)
				return
			}
			previous := squad.PreSkillBuiltinVersion(d)
			if version == previous.Version {
				writeJSON(w, previous)
				return
			}
		}
	}
	if version != "" {
		row, ok, err := s.store.SquadEntityVersion(r.Context(), "definition", resolved, version)
		if err != nil {
			squadFailure(w, err, true)
			return
		}
		if ok {
			var d squad.Definition
			if err := json.Unmarshal(row.Payload, &d); err != nil || d.Digest != row.Digest {
				squadFailure(w, errors.New("invalid historical definition"), true)
				return
			}
			writeJSON(w, d)
			return
		}
	}
	writeRefusal(w, http.StatusNotFound, "unknown_definition", "No definition has that ID and version.")
}

func squadRows(rows []store.SquadSettingRow) (map[string]store.SquadSettingRow, error) {
	out := map[string]store.SquadSettingRow{}
	for _, row := range rows {
		if !json.Valid(row.Payload) {
			return nil, errors.New("invalid setting payload")
		}
		out[row.DefinitionID] = row
	}
	return out, nil
}

func squadOverrideRow(row store.SquadSettingRow) (squad.Override, error) {
	if row.Version == 0 {
		return squad.Override{}, nil
	}
	var v squad.Override
	err := json.Unmarshal(row.Payload, &v)
	return v, err
}

func squadMotionRow(row store.SquadSettingRow) (squad.MotionOverride, error) {
	if row.Version == 0 {
		return squad.MotionOverride{}, nil
	}
	var v squad.MotionOverride
	err := json.Unmarshal(row.Payload, &v)
	return v, err
}

func squadDefaultChoices(d squad.Definition) []squad.Choice {
	out := make([]squad.Choice, 0, len(d.Skills))
	for _, r := range d.Skills {
		out = append(out, squad.Choice{ID: r.ID, Version: r.Version, Enabled: r.Enabled})
	}
	return out
}

func (s *Server) squadEffective(ctx context.Context, scope projects.Scope, placeID string) (squad.Effective, error) {
	catalog, err := s.squadCatalog(ctx)
	if err != nil {
		return squad.Effective{}, err
	}
	globalRows, err := s.store.SquadSettings(ctx, "global")
	if err != nil {
		return squad.Effective{}, err
	}
	global, err := squadRows(globalRows)
	if err != nil {
		return squad.Effective{}, err
	}
	project := map[string]store.SquadSettingRow{}
	if scope.ID != "global" {
		rows, err := s.store.SquadSettings(ctx, scope.ID)
		if err != nil {
			return squad.Effective{}, err
		}
		project, err = squadRows(rows)
		if err != nil {
			return squad.Effective{}, err
		}
	}
	gm, err := squadMotionRow(global[""])
	if err != nil {
		return squad.Effective{}, err
	}
	pm, err := squadMotionRow(project[""])
	if err != nil {
		return squad.Effective{}, err
	}
	out := squad.Effective{
		PlaceID: placeID, ScopeID: scope.ID, ScopeKind: scope.Kind,
		Motion:   squad.Resolve(true, gm.Motion, global[""].Version, pm.Motion, project[""].Version),
		Personas: make([]squad.EffectivePersona, 0, len(catalog.Definitions)),
	}
	out.MotionSettingsVersion = project[""].Version
	if scope.ID == "global" {
		out.MotionSettingsVersion = global[""].Version
	}
	for _, d := range catalog.Definitions {
		gRow, pRow := global[d.DefinitionID], project[d.DefinitionID]
		g, err := squadOverrideRow(gRow)
		if err != nil {
			return squad.Effective{}, err
		}
		p, err := squadOverrideRow(pRow)
		if err != nil {
			return squad.Effective{}, err
		}
		defaultEligible := d.Builtin
		version := pRow.Version
		if scope.ID == "global" {
			version = gRow.Version
		}
		e := squad.EffectivePersona{
			DefinitionID: d.DefinitionID, Version: version,
			Handbook:       squad.Resolve("", g.Handbook, gRow.Version, p.Handbook, pRow.Version),
			GlobalHandbook: squad.Resolve("", g.Handbook, gRow.Version, (*string)(nil), 0),
			AutoAssign:     squad.Resolve(defaultEligible, g.AutoAssign, gRow.Version, p.AutoAssign, pRow.Version),
			Skills:         squad.Resolve(squadDefaultChoices(d), g.Skills, gRow.Version, p.Skills, pRow.Version),
		}
		out.Personas = append(out.Personas, e)
	}
	return out, nil
}

func (s *Server) squadSettingsRead(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if len(q) > 2 || len(q["place_id"]) > 1 || len(q["scope_id"]) > 1 {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "Name one Project scope.")
		return
	}
	for name := range q {
		if name != "place_id" && name != "scope_id" {
			writeRefusal(w, http.StatusBadRequest, "bad_request", "Unknown squad setting query.")
			return
		}
	}
	placeID, scopeID := q.Get("place_id"), q.Get("scope_id")
	scope, err := s.squadScope(r.Context(), placeID, scopeID)
	if err != nil {
		squadScopeRefusal(w, err)
		return
	}
	out, err := s.squadEffective(r.Context(), scope, placeID)
	if err != nil {
		squadFailure(w, err, true)
		return
	}
	writeJSON(w, out)
}

func (s *Server) squadSettingsWrite(w http.ResponseWriter, r *http.Request) {
	key, ok := squadWriteDoor(w, r)
	if !ok {
		return
	}
	var req contract.SquadSaveRequest
	if !squadBody(w, r, &req) {
		return
	}
	if req.ExpectedVersion < 0 || req.DefinitionID == "" {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "Name a definition and nonnegative expected_version.")
		return
	}
	scope, err := s.squadScope(r.Context(), req.PlaceID, req.ScopeID)
	if err != nil {
		squadScopeRefusal(w, err)
		return
	}
	catalog, err := s.squadCatalog(r.Context())
	if err != nil {
		squadFailure(w, err, true)
		return
	}
	id, _ := squad.ResolveID(req.DefinitionID)
	found := false
	for _, d := range catalog.Definitions {
		if d.DefinitionID == id {
			found = true
			break
		}
	}
	if !found {
		writeRefusal(w, http.StatusNotFound, "unknown_definition", "No definition has that ID.")
		return
	}
	var patch squad.Patch
	if req.Overrides.Handbook != nil {
		patch.SetHandbook = true
		v := req.Overrides.Handbook.Value
		if len(v) > int(CapacityLimit(capacity.SquadBodyBytes)) {
			writeRefusal(w, http.StatusRequestEntityTooLarge, "body_too_large", "The handbook is too large.")
			return
		}
		if req.Overrides.Handbook.Present {
			patch.Handbook = &v
		}
	}
	if req.Overrides.AutoAssign != nil {
		patch.SetAutoAssign = true
		v := req.Overrides.AutoAssign.Value
		if req.Overrides.AutoAssign.Present {
			patch.AutoAssign = &v
		}
	}
	if req.Overrides.Skills != nil {
		patch.SetSkills = true
		choices := make([]squad.Choice, 0, len(req.Overrides.Skills.Value))
		seen := map[string]bool{}
		for _, ref := range req.Overrides.Skills.Value {
			valid := false
			for _, skill := range catalog.Skills {
				if skill.SkillID == ref.ID && skill.Version == ref.Version {
					valid = true
					break
				}
			}
			if !valid {
				_, valid = squad.HistoricalBuiltinSkill(ref.ID, ref.Version)
			}
			if !valid {
				_, exists, err := s.store.SquadEntityVersion(r.Context(), "skill", ref.ID, ref.Version)
				if err != nil {
					squadFailure(w, err, true)
					return
				}
				valid = exists
			}
			if !valid || seen[ref.ID] {
				writeRefusal(w, http.StatusBadRequest, "unknown_reference", "A skill reference is missing or repeated.")
				return
			}
			seen[ref.ID] = true
			choices = append(choices, squad.Choice{ID: ref.ID, Version: ref.Version, Enabled: ref.Enabled})
		}
		if req.Overrides.Skills.Present {
			patch.Skills = &choices
		}
	}
	if !patch.SetHandbook && !patch.SetAutoAssign && !patch.SetSkills {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "Name at least one setting to change.")
		return
	}
	version, err := s.store.PatchSquadSetting(context.WithoutCancel(r.Context()), scope.ID, id,
		req.ExpectedVersion, patch, key, squadLimits())
	if err != nil {
		squadFailure(w, err, false)
		return
	}
	writeJSON(w, contract.SquadWriteReply{ScopeID: scope.ID, Version: version})
}

func (s *Server) squadMotionWrite(w http.ResponseWriter, r *http.Request) {
	key, ok := squadWriteDoor(w, r)
	if !ok {
		return
	}
	var req contract.SquadMotionRequest
	if !squadBody(w, r, &req) {
		return
	}
	if req.ExpectedVersion < 0 {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "expected_version must be nonnegative.")
		return
	}
	scope, err := s.squadScope(r.Context(), req.PlaceID, req.ScopeID)
	if err != nil {
		squadScopeRefusal(w, err)
		return
	}
	var override squad.MotionOverride
	if req.Motion.Present {
		v := req.Motion.Value
		override.Motion = &v
	}
	payload, _ := json.Marshal(override)
	version, err := s.store.SaveSquadSetting(context.WithoutCancel(r.Context()),
		scope.ID, "", req.ExpectedVersion, payload, key, squadLimits())
	if err != nil {
		squadFailure(w, err, false)
		return
	}
	writeJSON(w, contract.SquadWriteReply{ScopeID: scope.ID, Version: version})
}

func squadReferencesValid(kind string, d squad.Definition, t squad.Team, c squad.Catalog) bool {
	switch kind {
	case "definition":
		seen := map[string]bool{}
		for _, ref := range d.Teams {
			found := false
			for _, team := range c.Teams {
				if team.TeamID == ref.ID && team.Version == ref.Version {
					found = true
					break
				}
			}
			if !found || seen[ref.ID] {
				return false
			}
			seen[ref.ID] = true
		}
		seen = map[string]bool{}
		for _, ref := range d.Skills {
			found := false
			for _, skill := range c.Skills {
				if skill.SkillID == ref.ID && skill.Version == ref.Version {
					found = true
					break
				}
			}
			if !found || seen[ref.ID] {
				return false
			}
			seen[ref.ID] = true
		}
	case "team":
		seen := map[string]bool{}
		for _, ref := range t.Personas {
			found := false
			for _, definition := range c.Definitions {
				if definition.DefinitionID == ref.ID && definition.Version == ref.Version {
					found = true
					break
				}
			}
			if !found || seen[ref.ID] {
				return false
			}
			seen[ref.ID] = true
		}
	}
	return true
}

func squadIconValid(icon squad.Icon) bool {
	if icon.Accent == "" || len(icon.Cells) != 7 {
		return false
	}
	for _, row := range icon.Cells {
		if len(row) != 8 {
			return false
		}
	}
	return true
}

func (s *Server) squadEntityWrite(w http.ResponseWriter, r *http.Request) {
	key, ok := squadWriteDoor(w, r)
	if !ok {
		return
	}
	var req contract.SquadEntityRequest
	if !squadBody(w, r, &req) {
		return
	}
	if req.ExpectedVersion < 0 {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "expected_version must be nonnegative.")
		return
	}
	catalog, err := s.squadCatalog(r.Context())
	if err != nil {
		squadFailure(w, err, true)
		return
	}
	var row store.SquadEntityRow
	switch req.Kind {
	case "definition":
		if req.Entity.Definition == nil || req.Entity.Skill != nil || req.Entity.Team != nil {
			writeRefusal(w, http.StatusBadRequest, "bad_request", "A definition write names exactly one definition.")
			return
		}
		raw, _ := json.Marshal(req.Entity.Definition)
		var d squad.Definition
		_ = json.Unmarshal(raw, &d)
		if !squad.ValidCustomID(d.DefinitionID) || d.Builtin || d.ShortID != "" {
			writeRefusal(w, http.StatusBadRequest, "builtin_read_only", "Built-in IDs and aliases cannot be changed.")
			return
		}
		if len(d.Body) > int(CapacityLimit(capacity.SquadBodyBytes)) || d.Version == "" ||
			d.Source == "" || d.License == "" || d.Name.En == "" || !squadIconValid(d.Icon) {
			writeRefusal(w, http.StatusBadRequest, "bad_request", "The definition lacks required metadata or exceeds its text limit.")
			return
		}
		if d.Teams == nil {
			d.Teams = []squad.Reference{}
		}
		if d.Skills == nil {
			d.Skills = []squad.SkillReference{}
		}
		if !squadReferencesValid("definition", d, squad.Team{}, catalog) {
			writeRefusal(w, http.StatusBadRequest, "unknown_reference", "A team or skill reference is missing or repeated.")
			return
		}
		given := d.Digest
		d.Digest = ""
		digest := squad.Digest(d)
		if given != "" && given != digest {
			writeRefusal(w, http.StatusBadRequest, "digest_mismatch", "The definition digest does not match its content.")
			return
		}
		d.Digest = digest
		row.Kind, row.ID, row.Version, row.Digest = "definition", d.DefinitionID, d.Version, digest
		row.Payload, _ = json.Marshal(d)
	case "team":
		if req.Entity.Team == nil || req.Entity.Definition != nil || req.Entity.Skill != nil {
			writeRefusal(w, http.StatusBadRequest, "bad_request", "A team write names exactly one team.")
			return
		}
		raw, _ := json.Marshal(req.Entity.Team)
		var d squad.Team
		_ = json.Unmarshal(raw, &d)
		if !squad.ValidCustomID(d.TeamID) || d.Builtin {
			writeRefusal(w, http.StatusBadRequest, "builtin_read_only", "Built-in teams cannot be changed.")
			return
		}
		if d.Version == "" || d.Source == "" || d.License == "" || d.Name.En == "" || !squadIconValid(d.Icon) {
			writeRefusal(w, http.StatusBadRequest, "bad_request", "The team lacks required metadata.")
			return
		}
		if d.Personas == nil {
			d.Personas = []squad.Reference{}
		}
		if !squadReferencesValid("team", squad.Definition{}, d, catalog) {
			writeRefusal(w, http.StatusBadRequest, "unknown_reference", "A persona reference is missing or repeated.")
			return
		}
		given := d.Digest
		d.Digest = ""
		digest := squad.Digest(d)
		if given != "" && given != digest {
			writeRefusal(w, http.StatusBadRequest, "digest_mismatch", "The team digest does not match its content.")
			return
		}
		d.Digest = digest
		row.Kind, row.ID, row.Version, row.Digest = "team", d.TeamID, d.Version, digest
		row.Payload, _ = json.Marshal(d)
	case "skill":
		if req.Entity.Skill == nil || req.Entity.Definition != nil || req.Entity.Team != nil {
			writeRefusal(w, http.StatusBadRequest, "bad_request", "A skill write names exactly one skill.")
			return
		}
		raw, _ := json.Marshal(req.Entity.Skill)
		var d squad.Skill
		_ = json.Unmarshal(raw, &d)
		if !squad.ValidCustomID(d.SkillID) || d.Builtin {
			writeRefusal(w, http.StatusBadRequest, "builtin_read_only", "Built-in skills cannot be changed.")
			return
		}
		if !squad.ValidSkillFiles(d.Content, d.Files) || (len(d.Files) > 0 && !d.Folder) ||
			(d.Folder && strings.TrimSpace(d.Content) == "") || d.Version == "" ||
			d.Source == "" || d.License == "" || d.Name.En == "" ||
			d.Purpose.En == "" || !squadIconValid(d.Icon) {
			writeRefusal(w, http.StatusBadRequest, "bad_request", "The skill lacks required metadata or its folder files are invalid or too large.")
			return
		}
		given := d.Digest
		d.Digest = ""
		digest := squad.Digest(d)
		if given != "" && given != digest {
			writeRefusal(w, http.StatusBadRequest, "digest_mismatch", "The skill digest does not match its content.")
			return
		}
		d.Digest = digest
		row.Kind, row.ID, row.Version, row.Digest = "skill", d.SkillID, d.Version, digest
		row.Payload, _ = json.Marshal(d)
	default:
		writeRefusal(w, http.StatusBadRequest, "bad_request", "kind must be definition, team or skill.")
		return
	}
	version, err := s.store.SaveSquadEntity(context.WithoutCancel(r.Context()), row,
		req.ExpectedVersion, key, squadLimits())
	if err != nil {
		squadFailure(w, err, false)
		return
	}
	writeJSON(w, contract.SquadEntityReply{
		CatalogVersion: version, ID: row.ID, Version: row.Version, Digest: row.Digest})
}
