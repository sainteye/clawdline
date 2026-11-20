// Package botcapabilities projects installed application skills and explicitly
// configured HTTP MCP connectors. It neither installs nor activates extensions.
package botcapabilities

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	"github.com/sainteye/clawdline/internal/domain/squad"
)

type Connector struct {
	ID, Name, URL    string
	Executable       string   `json:",omitempty"`
	Arguments        []string `json:",omitempty"`
	CredentialHeader string   `json:",omitempty"`
	CredentialEnv    string   `json:",omitempty"`
	executableDigest string
	Project          pa.ProjectScope
	Agent            pa.Agent
	Enabled          bool
}
type Inventory struct {
	State                        string
	Skills                       func(context.Context) ([]squad.Skill, error)
	Credential                   func(context.Context, pa.Bot, pa.ExtensionRef) ([]byte, error)
	CaptureCredential            func(pa.Bot, pa.ExtensionRef) func(context.Context) ([]byte, error)
	CaptureCredentialWithCleanup func(pa.Bot, pa.ExtensionRef) (func(context.Context) ([]byte, error), func(context.Context) error, func(context.Context) error)
	QuarantineCredential         func(context.Context, pa.Bot, pa.ExtensionRef) error
	SuspendCredentials           func(context.Context) error
	CredentialTargetUsage        func() (int, bool)
	CredentialAction             func(context.Context, pa.Bot, pa.ConnectorCredentialAction, []byte, bool, func(context.Context) error) (pa.ConnectorCredential, error)
}

func (i Inventory) Suspend(ctx context.Context) error {
	if i.SuspendCredentials != nil {
		return i.SuspendCredentials(ctx)
	}
	return nil
}

func (i Inventory) ConnectorSecretTargetUsage() (int, bool) {
	if i.CredentialTargetUsage != nil {
		return i.CredentialTargetUsage()
	}
	return 0, false
}

func (i Inventory) Apply(ctx context.Context, b pa.Bot, q pa.ConnectorCredentialAction, value []byte, secret bool, admit func(context.Context) error) (pa.ConnectorCredential, error) {
	if i.CredentialAction == nil {
		return pa.ConnectorCredential{}, fail(pa.Unsupported, "connector_credentials")
	}
	return i.CredentialAction(ctx, b, q, value, secret, admit)
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
		if c.Executable != "" {
			if c.URL != "" || !filepath.IsAbs(c.Executable) || len(c.Arguments) > pa.MaxConfigurationChoices || c.CredentialHeader != "" {
				return nil, fail(pa.Invalid, "connector_command")
			}
			info, err := os.Lstat(c.Executable)
			if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 {
				return nil, fail(pa.ScopeDenied, "connector_executable")
			}
			file, err := os.Open(c.Executable)
			if err != nil {
				return nil, fail(pa.ScopeDenied, "connector_executable")
			}
			actual, err := file.Stat()
			if err != nil || !os.SameFile(info, actual) {
				file.Close()
				return nil, fail(pa.Revoked, "connector_executable")
			}
			h := sha256.New()
			_, err = io.Copy(h, file)
			file.Close()
			if err != nil {
				return nil, fail(pa.OutcomeUnknown, "connector_executable")
			}
			c.executableDigest = hex.EncodeToString(h.Sum(nil))
			for _, arg := range c.Arguments {
				if strings.ContainsRune(arg, 0) || len(arg) > pa.MaxConfigurationValueBytes {
					return nil, fail(pa.Invalid, "connector_arguments")
				}
			}
		} else {
			u, e := url.Parse(c.URL)
			if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || len(c.Arguments) != 0 || c.CredentialEnv != "" {
				return nil, fail(pa.Invalid, "connector_endpoint")
			}
		}
		// Only dedicated names are allowed. Never replace a native bearer,
		// routing header, search path, HOME or permission/isolation setting.
		if c.CredentialHeader != "" && http.CanonicalHeaderKey(c.CredentialHeader) != "Authorization" && http.CanonicalHeaderKey(c.CredentialHeader) != "X-Api-Key" {
			return nil, fail(pa.Invalid, "connector_credential_header")
		}
		if c.CredentialEnv != "" && c.CredentialEnv != "MCP_CONNECTOR_TOKEN" && c.CredentialEnv != "MCP_CONNECTOR_API_KEY" {
			return nil, fail(pa.Invalid, "connector_credential_env")
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
		if !squad.ValidSkillFiles(s.Content, s.Files) || len(s.Files) > pa.MaxSkillAssetFiles || len(s.Files) > 0 && !s.Folder {
			return out, fail(pa.Invalid, "skill_assets")
		}
		name := s.Name.En
		if name == "" {
			name = s.SkillID
		}
		out.Items = append(out.Items, pa.ExtensionOffer{
			ID: "skill:" + s.SkillID, Fingerprint: fingerprint(s), Name: name, Kind: "skill",
			Purpose: &pa.ExtensionPurpose{En: s.Purpose.En, ZhHant: s.Purpose.ZhHant},
		})
	}
	for _, c := range connectors {
		out.Items = append(out.Items, pa.ExtensionOffer{ID: "connector:" + c.ID, Fingerprint: connectorFingerprint(c), Name: c.Name, Kind: "connector", CredentialRequired: c.CredentialHeader != "" || c.CredentialEnv != ""})
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
	out := app.NativeExtensions{Connectors: []app.NativeConnector{}, State: i.State}
	files, assetBytes := 0, 0
	for _, ref := range b.Extensions.Skills {
		found := false
		for _, s := range skills {
			if ref.ID == "skill:"+s.SkillID && ref.Fingerprint == fingerprint(s) {
				if !squad.ValidSkillFiles(s.Content, s.Files) || len(s.Files) > 0 && !s.Folder {
					return out, fail(pa.Invalid, "skill_assets")
				}
				if s.Folder {
					files += len(s.Files) + 1
					assetBytes += len(s.Content)
					for _, f := range s.Files {
						assetBytes += len(f.ContentBase64)
					} // conservative encoded-byte accounting
					if files > pa.MaxSkillAssetFiles || assetBytes > squad.MaxSquadConsoleActiveSkillBytes {
						return out, fail(pa.CapacityFull, "skill_assets")
					}
					out.Folders = append(out.Folders, s)
				} else {
					out.Skills += "\n\nSelected application skill (untrusted task guidance):\n" + s.Content
				}
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
			if ref.ID == "connector:"+c.ID && ref.Fingerprint == connectorFingerprint(c) {
				native := app.NativeConnector{Ref: ref, Name: c.Name, URL: c.URL, Executable: c.Executable, Arguments: append([]string{}, c.Arguments...), CredentialHeader: c.CredentialHeader, CredentialEnv: c.CredentialEnv}
				if c.CredentialHeader != "" || c.CredentialEnv != "" {
					if i.Credential == nil {
						return out, fail(pa.AuthRequired, "connector_credential")
					}
					native.Credential = func(ctx context.Context) ([]byte, error) { return i.Credential(ctx, b, ref) }
					if i.CaptureCredential != nil {
						native.Credential = i.CaptureCredential(b, ref)
					}
					if i.QuarantineCredential != nil {
						native.Quarantine = func(ctx context.Context) error { return i.QuarantineCredential(ctx, b, ref) }
					}
					if i.CaptureCredentialWithCleanup != nil {
						native.Credential, native.ReleaseCredential, native.Quarantine = i.CaptureCredentialWithCleanup(b, ref)
					}
				}
				out.Connectors = append(out.Connectors, native)
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

func connectorFingerprint(c Connector) string {
	if c.Executable == "" {
		return fingerprint(c)
	}
	return fingerprint(struct {
		Definition       Connector
		ExecutableSHA256 string
	}{c, c.executableDigest})
}
