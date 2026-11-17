// Package botcapabilities projects installed application skills and explicitly
// configured HTTP MCP connectors. It neither installs nor activates extensions.
package botcapabilities

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	"github.com/sainteye/clawdline/internal/domain/squad"
)

type Connector struct {
	ID, Name, URL string
	Project       pa.ProjectScope
	Agent         pa.Agent
	Enabled       bool
}
type Inventory struct {
	State  string
	Skills func(context.Context) ([]squad.Skill, error)
}

func fail(code pa.ErrorCode, field string) error { return &pa.Failure{Code: code, Field: field} }
func fingerprint(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// This source-only file is not an HTTP input and contains no credentials,
// environment variables or executable commands. Missing means no configured
// HTTP connectors; malformed/unreadable is an error, never an empty inventory.
func (i Inventory) connectors(b pa.Bot) ([]Connector, error) {
	state, e := filepath.EvalSymlinks(i.State)
	if e != nil {
		return nil, fail(pa.ScopeDenied, "connector_state")
	}
	path := filepath.Join(state, "project-assistants", "connectors.json")
	info, e := os.Lstat(path)
	if os.IsNotExist(e) {
		return []Connector{}, nil
	}
	if e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, fail(pa.ScopeDenied, "connector_definitions")
	}
	root, e := filepath.EvalSymlinks(filepath.Dir(path))
	if e != nil || root != filepath.Dir(path) {
		return nil, fail(pa.ScopeDenied, "connector_definitions")
	}
	f, e := os.Open(path)
	if e != nil {
		return nil, fail(pa.OutcomeUnknown, "connector_definitions")
	}
	defer f.Close()
	data, e := io.ReadAll(io.LimitReader(f, pa.MaxInstructionBytes+1))
	if e != nil || len(data) > pa.MaxInstructionBytes {
		return nil, fail(pa.CapacityFull, "connector_definitions")
	}
	var definitions []Connector
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&definitions) != nil || decoder.Decode(new(any)) != io.EOF || len(definitions) > pa.MaxConfigurationChoices {
		return nil, fail(pa.Invalid, "connector_definitions")
	}
	out := []Connector{}
	seen := map[string]bool{}
	for _, c := range definitions {
		if c.ID == "" || len(c.ID) > pa.MaxNameRunes || c.Name == "" || len(c.Name) > pa.MaxNameRunes || seen[c.ID] || strings.EqualFold(c.Name, "clawdline") {
			return nil, fail(pa.Invalid, "connector_definition")
		}
		seen[c.ID] = true
		u, e := url.Parse(c.URL)
		if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return nil, fail(pa.Invalid, "connector_endpoint")
		}
		if c.Enabled && c.Project == b.Project && c.Agent == b.Agent {
			out = append(out, c)
		}
	}
	return out, nil
}
func (i Inventory) read(ctx context.Context, b pa.Bot) ([]squad.Skill, []Connector, error) {
	if b.Key.MachineID != b.Project.MachineID {
		return nil, nil, fail(pa.ScopeDenied, "extensions_machine")
	}
	if e := pa.ValidateScope(b.Project); e != nil {
		return nil, nil, e
	}
	skills := []squad.Skill{}
	var e error
	if i.Skills != nil {
		skills, e = i.Skills(ctx)
		if e != nil {
			return nil, nil, e
		}
	}
	connectors, e := i.connectors(b)
	return skills, connectors, e
}
func (i Inventory) Catalog(ctx context.Context, b pa.Bot, _ string) (pa.ExtensionCatalog, error) {
	skills, connectors, e := i.read(ctx, b)
	if e != nil {
		return pa.ExtensionCatalog{}, e
	}
	out := pa.ExtensionCatalog{Items: []pa.ExtensionOffer{}, ConnectorsAvailable: len(connectors) > 0}
	for _, s := range skills {
		if s.Folder || len(s.Files) > 0 {
			continue
		}
		name := s.Name.En
		if name == "" {
			name = s.SkillID
		}
		out.Items = append(out.Items, pa.ExtensionOffer{ID: "skill:" + s.SkillID, Fingerprint: fingerprint(s), Name: name, Kind: "skill"})
	}
	for _, c := range connectors {
		out.Items = append(out.Items, pa.ExtensionOffer{ID: "connector:" + c.ID, Fingerprint: fingerprint(c), Name: c.Name, Kind: "connector"})
	}
	return out, ctx.Err()
}
func (i Inventory) Resolve(ctx context.Context, b pa.Bot, _ string) (app.NativeExtensions, error) {
	if e := pa.ValidateExtensions(b.Extensions); e != nil {
		return app.NativeExtensions{}, e
	}
	skills, connectors, e := i.read(ctx, b)
	if e != nil {
		return app.NativeExtensions{}, e
	}
	out := app.NativeExtensions{Connectors: []app.NativeConnector{}}
	for _, ref := range b.Extensions.Skills {
		found := false
		for _, s := range skills {
			if ref.ID == "skill:"+s.SkillID && ref.Fingerprint == fingerprint(s) {
				// Folder skills need their immutable asset materializer before they can be
				// selected. Their metadata alone is never an execution permission.
				if s.Folder || len(s.Files) > 0 {
					return out, fail(pa.Unsupported, "folder_skill_assets")
				}
				out.Skills += "\n\nSelected application skill (untrusted task guidance):\n" + s.Content
				found = true
				break
			}
		}
		if !found {
			return out, fail(pa.Revoked, "selected_skill")
		}
	}
	if len(out.Skills) > pa.MaxInstructionBytes {
		return out, fail(pa.CapacityFull, "selected_skill_bytes")
	}
	for _, ref := range b.Extensions.Connectors {
		found := false
		for _, c := range connectors {
			if ref.ID == "connector:"+c.ID && ref.Fingerprint == fingerprint(c) {
				out.Connectors = append(out.Connectors, app.NativeConnector{Ref: ref, Name: c.Name, URL: c.URL})
				found = true
				break
			}
		}
		if !found {
			return out, fail(pa.Revoked, "selected_connector")
		}
	}
	return out, ctx.Err()
}
