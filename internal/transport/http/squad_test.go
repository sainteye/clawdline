package http

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/adapters/squadfiles"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/auth"
	"github.com/sainteye/clawdline/internal/domain/squad"
)

type squadFixture struct {
	h                     http.Handler
	s                     *Server
	local, reader, sender string
	paths                 map[string]string
}

func newSquadFixture(t *testing.T) *squadFixture {
	t.Helper()
	t.Setenv("CLAWDLINE_SWIFT_DIR", filepath.Join(t.TempDir(), "retired"))
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	root := t.TempDir()
	paths := map[string]string{}
	for _, name := range []string{"a", "b"} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Join(path, ".git"), 0700); err != nil {
			t.Fatal(err)
		}
		paths[name] = path
	}
	s := &Server{cfg: config.Config{Dir: dir, Port: 7757}, store: st}
	s.squadPlace = func(_ context.Context, id string) (projects.Scope, bool) {
		path, ok := paths[id]
		if !ok {
			return projects.Scope{}, false
		}
		return projects.ResolveScope(path)
	}
	s.squadPlaces = func(context.Context) []projects.Place {
		return []projects.Place{
			{ID: "a", Path: paths["a"], Label: "Project A"},
			{ID: "b", Path: paths["b"], Label: "Project B"},
		}
	}
	g := s.gate()
	if g.err != nil {
		t.Fatal(g.err)
	}
	local, err := g.auth.LocalToken()
	if err != nil {
		t.Fatal(err)
	}
	_, reader, err := g.auth.AddDevice("reader", auth.NewCaps(auth.Read), false)
	if err != nil {
		t.Fatal(err)
	}
	_, sender, err := g.auth.AddDevice("sender", auth.NewCaps(auth.Read, auth.Send), false)
	if err != nil {
		t.Fatal(err)
	}
	return &squadFixture{h: s.Handler(), s: s, local: local, reader: reader,
		sender: sender, paths: paths}
}

func (f *squadFixture) ask(method, path, token, body, key string) (int, string) {
	headers := map[string]string{}
	if token != "" {
		headers["Authorization"] = "Bearer " + token
	}
	if body != "" {
		headers["Content-Type"] = "application/json"
	}
	if key != "" {
		headers["Idempotency-Key"] = key
	}
	rec := call{method: method, path: path, body: body, headers: headers}.do(f.h)
	return rec.Code, rec.Body.String()
}

func TestSquadPrivateCatalogAndAuthorization(t *testing.T) {
	f := newSquadFixture(t)
	if status, _ := f.ask("GET", "/v1/squad/catalog", "", "", ""); status != 401 {
		t.Fatalf("anonymous catalog = %d", status)
	}
	status, raw := f.ask("GET", "/v1/squad/catalog", f.reader, "", "")
	var catalog contract.SquadCatalog
	if status != 200 || json.Unmarshal([]byte(raw), &catalog) != nil ||
		len(catalog.Definitions) != 42 || len(catalog.Skills) != 12 {
		t.Fatalf("private catalog = %d, %s", status, raw)
	}
	launch, err := f.s.ResolveSquadLaunch(context.Background(), "backend", "")
	if err != nil || len(launch.Skills) != 1 || !launch.Skills[0].Enabled ||
		launch.Skills[0].Content == "" || launch.Skills[0].ID != "clawdline.skill.golang-concurrency" {
		t.Fatalf("default built-in skill launch = %+v, %v", launch.Skills, err)
	}
	for _, d := range catalog.Definitions {
		if !d.Builtin || d.Body == "" || d.DefinitionID == "" || d.Digest == "" || d.Skills == nil {
			t.Fatalf("incomplete definition %+v", d)
		}
	}
	var previous squad.Definition
	for _, d := range squad.Builtins().Definitions {
		if d.ShortID == "backend" {
			previous = squad.PreSkillBuiltinVersion(d)
			break
		}
	}
	if status, raw := f.ask("GET", "/v1/squad/definitions/backend?version="+previous.Version, f.reader, "", ""); status != 200 || !strings.Contains(raw, `"skills":[]`) {
		t.Fatalf("prior built-in definition = %d, %s", status, raw)
	}
	if status, raw := f.ask("GET", "/v1/personas", f.reader, "", ""); status != 200 || strings.Contains(raw, "Clawdline persona") {
		t.Fatalf("summary leaked full content: %d, %s", status, raw)
	}
	write := `{"definition_id":"backend","expected_version":0,"overrides":{"handbook":{"present":true,"value":"secret"}}}`
	if status, _ := f.ask("PUT", "/v1/squad/settings", f.reader, write, "reader-1"); status != 403 {
		t.Fatalf("read-only writer = %d", status)
	}
	if status, _ := f.ask("PUT", "/v1/squad/settings", "", write, "anon-1"); status != 401 {
		t.Fatalf("anonymous writer = %d", status)
	}
	if status, raw := f.ask("POST", "/v1/squad/catalog", f.sender,
		`{"kind":"definition","expected_version":0,"entity":{"definition":{"definition_id":"clawdline.persona.backend"}}}`,
		"builtin-1"); status != 400 || !strings.Contains(raw, "builtin_read_only") {
		t.Fatalf("builtin mutation = %d, %s", status, raw)
	}
	if status, raw := f.ask("GET", "/v1/squad/auto-candidates?conversation_id=claimed", f.local, "", ""); status != 403 || !strings.Contains(raw, "actor_unverified") {
		t.Fatalf("self-claimed actor = %d, %s", status, raw)
	}
}

func TestEarlierBuiltinSkillVersionStillLaunchesAfterPortableUpdate(t *testing.T) {
	f := newSquadFixture(t)
	var previous squad.Definition
	for _, d := range squad.Builtins().Definitions {
		if d.ShortID == "backend" {
			var ok bool
			previous, ok = squad.HistoricalBuiltinDefinition(d)
			if !ok {
				t.Fatal("previous backend definition is not addressable")
			}
			break
		}
	}
	if status, raw := f.ask("GET", "/v1/squad/definitions/backend?version="+previous.Version, f.reader, "", ""); status != 200 || !strings.Contains(raw, previous.Skills[0].Version) {
		t.Fatalf("previous definition = %d %s", status, raw)
	}
	body := fmt.Sprintf(`{"definition_id":"backend","expected_version":0,"overrides":{"skills":{"present":true,"value":[{"id":%q,"version":%q,"enabled":true}]}}}`,
		previous.Skills[0].ID, previous.Skills[0].Version)
	if status, raw := f.ask("PUT", "/v1/squad/settings", f.local, body, "keep-previous-builtin"); status != 200 {
		t.Fatalf("pin previous skill = %d %s", status, raw)
	}
	launch, err := f.s.ResolveSquadLaunch(context.Background(), "backend", "")
	if err != nil || len(launch.Skills) != 1 || launch.Skills[0].Version != previous.Skills[0].Version ||
		!strings.Contains(launch.Skills[0].Content, "Clawdline") {
		t.Fatalf("previous skill launch = %+v, %v", launch.Skills, err)
	}
}

func TestCodeReviewerLaunchReceivesPortableReviewSkill(t *testing.T) {
	f := newSquadFixture(t)
	launch, err := f.s.ResolveSquadLaunch(context.Background(), "code-reviewer", "")
	if err != nil || len(launch.Skills) != 1 ||
		launch.Skills[0].ID != "clawdline.skill.two-axis-code-review" ||
		!strings.Contains(launch.Skills[0].Content, "project-rules axis") ||
		!strings.Contains(launch.Skills[0].Content, "request axis") ||
		strings.Contains(launch.Skills[0].Content, "Clawdline") {
		t.Fatalf("code-reviewer launch = %+v, %v", launch.Skills, err)
	}
}

func TestSquadBuiltInSkillReachesNewSessionFiles(t *testing.T) {
	f := newSquadFixture(t)
	snapshot, err := f.s.ResolveSquadLaunch(context.Background(), "backend", "")
	if err != nil || len(snapshot.Skills) != 1 || !snapshot.Skills[0].Enabled {
		t.Fatalf("default backend snapshot: %+v, %v", snapshot.Skills, err)
	}
	document, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(document)
	files, err := squadfiles.Publish(t.TempDir(), "ABCDEFGHIJKLMNOPQRSTUV", hex.EncodeToString(hash[:]), "test-capability", document)
	if err != nil {
		t.Fatal(err)
	}
	prompt, err := os.ReadFile(files.PromptPath)
	if err != nil || !strings.Contains(string(prompt), snapshot.Skills[0].ID) {
		t.Fatalf("launch prompt omitted the built-in skill: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(filepath.Dir(files.SnapshotPath), "skills"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("launch skill files: %d, %v", len(entries), err)
	}
	content, err := os.ReadFile(filepath.Join(filepath.Dir(files.SnapshotPath), "skills", entries[0].Name()))
	if err != nil || string(content) != snapshot.Skills[0].Content {
		t.Fatalf("launch skill content differs from snapshot: %v", err)
	}
}

func TestSquadEffectiveSettingsAcrossProjectsAndPatch(t *testing.T) {
	f := newSquadFixture(t)
	get := func(path string) contract.SquadEffectiveSettings {
		t.Helper()
		status, raw := f.ask("GET", path, f.reader, "", "")
		var out contract.SquadEffectiveSettings
		if status != 200 || json.Unmarshal([]byte(raw), &out) != nil {
			t.Fatalf("%s = %d %s", path, status, raw)
		}
		return out
	}
	persona := func(out contract.SquadEffectiveSettings) contract.SquadEffectivePersona {
		t.Helper()
		for _, p := range out.Personas {
			if p.DefinitionID == "clawdline.persona.backend" {
				return p
			}
		}
		t.Fatal("backend missing")
		return contract.SquadEffectivePersona{}
	}
	if global := get("/v1/squad/settings"); !global.Motion.Value || global.MotionSettingsVersion != 0 {
		t.Fatalf("motion default = %+v", global.Motion)
	}
	globalWrite := `{"definition_id":"backend","expected_version":0,"overrides":{"handbook":{"present":true,"value":"global"}}}`
	if status, raw := f.ask("PUT", "/v1/squad/settings", f.local, globalWrite, "global-1"); status != 200 || !strings.Contains(raw, `"version":1`) {
		t.Fatalf("global save = %d, %s", status, raw)
	}
	projectWrite := `{"place_id":"a","definition_id":"backend","expected_version":0,"overrides":{"handbook":{"present":true,"value":""}}}`
	if status, raw := f.ask("PUT", "/v1/squad/settings", f.sender, projectWrite, "a-1"); status != 200 || !strings.Contains(raw, `"version":1`) {
		t.Fatalf("A blank save = %d, %s", status, raw)
	}
	a := get("/v1/squad/settings?place_id=a")
	b := get("/v1/squad/settings?place_id=b")
	scopeA, _ := projects.ResolveScope(f.paths["a"])
	scopeB, _ := projects.ResolveScope(f.paths["b"])
	if a.ScopeID != scopeA.ID || b.ScopeID != scopeB.ID ||
		persona(a).Handbook.Value != "" || persona(a).Handbook.Source != "project" ||
		persona(a).GlobalHandbook.Value != "global" || persona(b).Handbook.Value != "global" ||
		persona(b).Handbook.Source != "global" {
		t.Fatalf("effective A = %+v, B = %+v", persona(a), persona(b))
	}
	patch := `{"place_id":"a","definition_id":"backend","expected_version":1,"overrides":{"auto_assign":{"present":true,"value":false}}}`
	if status, raw := f.ask("PUT", "/v1/squad/settings", f.sender, patch, "a-2"); status != 200 {
		t.Fatalf("patch = %d %s", status, raw)
	}
	a = get("/v1/squad/settings?place_id=a")
	if persona(a).Handbook.Source != "project" || persona(a).Handbook.Value != "" ||
		persona(a).AutoAssign.Value || persona(a).SettingsVersion != 2 {
		t.Fatalf("patch erased another field: %+v", persona(a))
	}
	if status, raw := f.ask("PUT", "/v1/squad/settings", f.sender, patch, "a-2"); status != 200 || !strings.Contains(raw, `"version":2`) {
		t.Fatalf("replay = %d %s", status, raw)
	}
	if status, raw := f.ask("PUT", "/v1/squad/settings", f.sender, patch, "a-3"); status != 409 || !strings.Contains(raw, `"current_version":2`) {
		t.Fatalf("stale write = %d %s", status, raw)
	}
	restore := `{"place_id":"a","definition_id":"backend","expected_version":2,"overrides":{"handbook":{"present":false,"value":""}}}`
	if status, raw := f.ask("PUT", "/v1/squad/settings", f.sender, restore, "a-4"); status != 200 {
		t.Fatalf("restore = %d %s", status, raw)
	}
	if p := persona(get("/v1/squad/settings?place_id=a")); p.Handbook.Value != "global" || p.Handbook.Source != "global" {
		t.Fatalf("restored handbook = %+v", p.Handbook)
	}
	motion := `{"place_id":"a","expected_version":0,"motion":{"present":true,"value":false}}`
	if status, raw := f.ask("PUT", "/v1/squad/motion", f.sender, motion, "motion-a"); status != 200 {
		t.Fatalf("motion write = %d %s", status, raw)
	}
	a = get("/v1/squad/settings?place_id=a")
	b = get("/v1/squad/settings?place_id=b")
	if a.Motion.Value || a.Motion.Source != "project" || a.MotionSettingsVersion != 1 ||
		!b.Motion.Value || b.MotionSettingsVersion != 0 {
		t.Fatalf("motion A = %+v/%d, B = %+v/%d", a.Motion, a.MotionSettingsVersion, b.Motion, b.MotionSettingsVersion)
	}
	if status, raw := f.ask("GET", "/v1/squad/settings?place_id=unknown", f.reader, "", ""); status != 404 || !strings.Contains(raw, "unknown_project") {
		t.Fatalf("unknown project = %d %s", status, raw)
	}
}

func TestSquadScopesIncludeProjectsBeforeTheyHaveSettings(t *testing.T) {
	f := newSquadFixture(t)
	status, raw := f.ask("GET", "/v1/squad/scopes", f.reader, "", "")
	var out contract.SquadScopes
	if status != 200 || json.Unmarshal([]byte(raw), &out) != nil || len(out.Scopes) != 2 || len(out.ScopeIds) != 0 {
		t.Fatalf("scopes = %d %s", status, raw)
	}
	for _, place := range out.Scopes {
		if place.PlaceID == "" || place.ScopeID == "" || place.Kind != projects.ScopeRepository ||
			place.Label == "" || place.Path == "" {
			t.Fatalf("incomplete scope %+v", place)
		}
	}
}

func TestSquadLaunchSnapshotCarriesOrderedVersionedSkills(t *testing.T) {
	f := newSquadFixture(t)
	icon := squad.Icon{Accent: "#123456", Cells: make([][]*string, 7)}
	for row := range icon.Cells {
		icon.Cells[row] = make([]*string, 8)
	}
	rows := make([]store.SquadEntityRow, 0, 2)
	for _, name := range []string{"one", "two"} {
		skill := squad.Skill{SkillID: "example.skill." + name, Version: "1",
			Source: "private", License: "MIT", Name: squad.Names{En: name},
			Purpose: squad.Names{En: "Help " + name}, Icon: icon, Content: "body " + name}
		skill.Digest = squad.Digest(skill)
		payload, err := json.Marshal(skill)
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, store.SquadEntityRow{Kind: "skill", ID: skill.SkillID,
			Version: skill.Version, Digest: skill.Digest, Payload: payload})
	}
	if v, err := f.s.store.SaveSquadEntities(context.Background(), rows, 0, "skills", store.DefaultSquadLimits()); err != nil || v != 1 {
		t.Fatalf("skills = %d, %v", v, err)
	}
	write := `{"place_id":"a","definition_id":"backend","expected_version":0,"overrides":{"handbook":{"present":true,"value":"team rules"},"skills":{"present":true,"value":[{"id":"example.skill.two","version":"1","enabled":false},{"id":"example.skill.one","version":"1","enabled":true}]}}}`
	if status, raw := f.ask("PUT", "/v1/squad/settings", f.local, write, "launch-settings"); status != 200 {
		t.Fatalf("settings = %d %s", status, raw)
	}
	snapshot, err := f.s.ResolveSquadLaunch(context.Background(), "backend", f.paths["a"])
	scopeA, _ := projects.ResolveScope(f.paths["a"])
	if err != nil || snapshot.DefinitionID != "clawdline.persona.backend" ||
		snapshot.ScopeID != scopeA.ID ||
		snapshot.Definition.Body == "" || snapshot.Handbook.Text != "team rules" ||
		!snapshot.AutoAssignEnabled ||
		len(snapshot.Skills) != 2 || snapshot.Skills[0].ID != "example.skill.two" ||
		snapshot.Skills[0].Enabled || snapshot.Skills[0].Content != "body two" ||
		snapshot.Skills[1].ID != "example.skill.one" || !snapshot.Skills[1].Enabled {
		t.Fatalf("snapshot = %+v, %v", snapshot, err)
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var shape struct {
		DefinitionID string `json:"definition_id"`
		ScopeID      string `json:"scope_id"`
		Handbook     struct {
			Text string `json:"text"`
		} `json:"handbook"`
		Skills []struct {
			ID      string `json:"id"`
			Version string `json:"version"`
			Enabled bool   `json:"enabled"`
			Content string `json:"content"`
			Digest  string `json:"digest"`
		} `json:"skills"`
	}
	if err := json.Unmarshal(raw, &shape); err != nil || len(shape.Skills) != 2 ||
		shape.Skills[0].Version != "1" || shape.Skills[0].Digest == "" {
		t.Fatalf("snapshot JSON = %s, %v", raw, err)
	}
	var updated squad.Skill
	if err := json.Unmarshal(rows[0].Payload, &updated); err != nil {
		t.Fatal(err)
	}
	updated.Version, updated.Content, updated.Digest = "2", "new body", ""
	updated.Digest = squad.Digest(updated)
	updatedPayload, _ := json.Marshal(updated)
	if _, err := f.s.store.SaveSquadEntity(context.Background(), store.SquadEntityRow{
		Kind: "skill", ID: updated.SkillID, Version: updated.Version,
		Digest: updated.Digest, Payload: updatedPayload}, 1, "skill-update", store.DefaultSquadLimits()); err != nil {
		t.Fatal(err)
	}
	snapshot, err = f.s.ResolveSquadLaunch(context.Background(), "backend", f.paths["a"])
	if err != nil || snapshot.Skills[1].Version != "1" || snapshot.Skills[1].Content != "body one" {
		t.Fatalf("historical skill = %+v, %v", snapshot.Skills, err)
	}
	reselect := `{"place_id":"a","definition_id":"backend","expected_version":1,"overrides":{"skills":{"present":true,"value":[{"id":"example.skill.one","version":"1","enabled":true}]}}}`
	if status, raw := f.ask("PUT", "/v1/squad/settings", f.local, reselect, "historical-choice"); status != 200 {
		t.Fatalf("historical choice = %d %s", status, raw)
	}
	disable := `{"place_id":"a","definition_id":"backend","expected_version":2,"overrides":{"auto_assign":{"present":true,"value":false}}}`
	if status, raw := f.ask("PUT", "/v1/squad/settings", f.local, disable, "disable-auto"); status != 200 {
		t.Fatalf("disable = %d %s", status, raw)
	}
	snapshot, err = f.s.ResolveSquadLaunch(context.Background(), "backend", f.paths["a"])
	if err != nil || snapshot.AutoAssignEnabled {
		t.Fatalf("disabled snapshot = %+v, %v", snapshot, err)
	}
	if _, err := f.s.ResolveSquadLaunch(context.Background(), "unknown", f.paths["a"]); !errors.Is(err, ErrSquadLaunchDefinition) {
		t.Fatalf("unknown definition = %v", err)
	}
	if _, err := f.s.ResolveSquadLaunch(context.Background(), "backend", "relative"); !errors.Is(err, ErrSquadLaunchProject) {
		t.Fatalf("unknown project = %v", err)
	}
}
