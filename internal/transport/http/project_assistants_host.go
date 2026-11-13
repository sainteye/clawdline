package http

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/adapters/cloudkeys"
	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline/internal/adapters/projectassistantacp"
	"github.com/sainteye/clawdline/internal/adapters/projectassistanthost"
	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/app/cloudops"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/auth"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	cloudtransport "github.com/sainteye/clawdline/internal/transport/cloud"
)

// One composition owner per Server. transition serializes lifecycle IO, while
// mu protects only process-local state and is never held during Host/SQL IO.
type projectAssistantServer struct {
	transition       sync.Mutex
	mu               sync.RWMutex
	host             *projectassistanthost.Host
	account          string
	machine          string
	allowed          bool
	stopped          bool
	inspectionFailed bool
	// Private fixture overrides; production uses the installed native factory.
	provider          func() (app.Provider, error)
	limits            *app.RuntimeLimits
	beforeViewPublish func()
}

func (s *Server) projectAssistantIdentity() (adaptercloud.Identity, error) {
	i, found, err := adaptercloud.NewIdentityStore(filepath.Join(s.cfg.Dir, cloudkeys.DirName)).Load()
	if err != nil {
		return i, errors.Join(paHTTPFailure(pa.Unpaired, "source_identity"), err)
	}
	if !found {
		// The existing execution identity seam is test-only. It never enrolls
		// a machine, reads a hostname, or fabricates an account identity.
		if s.executionMachineID != "" {
			return adaptercloud.Identity{MachineID: s.executionMachineID}, nil
		}
		return i, paHTTPFailure(pa.Unpaired, "source_identity")
	}
	return i, nil
}

func projectAssistantID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Never substitute a clock/counter for an unavailable random identity.
		panic("project assistant identity randomness unavailable")
	}
	return hex.EncodeToString(b[:])
}

// Caller holds transition. A failed durable opt-out retains the same Host and
// its cleanupPending state, so a later Enable cannot silently replay queues.
func (s *Server) ensureProjectAssistantHost(ctx context.Context) (*projectassistanthost.Host, error) {
	p := &s.projectAssistants
	p.mu.RLock()
	h, stopped := p.host, p.stopped
	p.mu.RUnlock()
	if stopped {
		return h, paHTTPFailure(pa.Disabled, "server_stopped")
	}
	i, err := s.projectAssistantIdentity()
	if err != nil {
		return h, err
	}
	if h != nil {
		p.mu.RLock()
		same := p.machine == i.MachineID && p.account == i.AccountID
		p.mu.RUnlock()
		if !same {
			return h, paHTTPFailure(pa.ScopeDenied, "source_identity_changed")
		}
		return h, nil
	}
	if s.store == nil || s.broker == nil {
		return nil, paHTTPFailure(pa.Disabled, "server_store")
	}
	limits := app.RuntimeLimits{
		MaxBots: capacity.MaximumBotActors, MaxProcesses: capacity.MaximumBotProcesses,
		PromptTimeout:  time.Duration(capacity.MaximumBotPromptMilliseconds) * time.Millisecond,
		CleanupTimeout: time.Duration(capacity.MaximumBotCleanupMilliseconds) * time.Millisecond,
	}
	if p.limits != nil {
		limits = *p.limits
	}
	provider := p.provider
	if provider == nil {
		provider = func() (app.Provider, error) { return projectassistantacp.InstalledClaudeProvider(s.cfg.Dir), nil }
	}
	h, err = projectassistanthost.New(ctx, projectassistanthost.Options{
		Database: s.store.ProjectAssistantDatabase(), Reader: s.store.ProjectAssistantReader(), MachineID: i.MachineID,
		ReadViewByteLimit: cloudops.ReadPlaintextBytesLimit,
		Projects:          projectAssistantProjects{s}, Broker: s.broker, Clock: time.Now,
		ID: projectAssistantID, Limits: limits, Authority: s.projectAssistantAuthority,
		Source: s.projectAssistantSource, Provider: provider,
		// The provider receives only execution-bound NativeTools from Host.
		// Dispatch still requires separate source-owned exact claims.
	})
	if err != nil {
		p.mu.Lock()
		p.inspectionFailed = true
		p.mu.Unlock()
		return nil, err
	}
	p.mu.Lock()
	p.host, p.account, p.machine = h, i.AccountID, i.MachineID
	p.inspectionFailed = false
	p.mu.Unlock()
	return h, h.Disable(ctx)
}

// StartProjectAssistants performs the persisted opt-in/opt-out once at boot.
// It constructs neither a provider nor an Engine, and an unpaired default-off
// installation has no Host or feature schema at all.
func (s *Server) StartProjectAssistants(ctx context.Context) error {
	p := &s.projectAssistants
	p.transition.Lock()
	defer p.transition.Unlock()
	v, err := s.settingsFile().Read()
	if err != nil {
		return err
	}
	h, err := s.ensureProjectAssistantHost(ctx)
	if err != nil {
		if pa.Code(err) == pa.Unpaired && !v.ProjectAssistantsEnabled() && h == nil {
			return nil
		}
		return err
	}
	if !v.ProjectAssistantsEnabled() {
		p.mu.Lock()
		p.allowed = false
		p.mu.Unlock()
		return h.Disable(ctx)
	}
	if err = h.Enable(ctx); err != nil {
		return err
	}
	p.mu.Lock()
	p.allowed = true
	p.mu.Unlock()
	return nil
}

// StopProjectAssistants retains unresolved cleanup ownership and never closes
// the shared database or the ordinary work Broker.
func (s *Server) StopProjectAssistants(ctx context.Context) error {
	p := &s.projectAssistants
	p.transition.Lock()
	defer p.transition.Unlock()
	p.mu.Lock()
	p.allowed, p.stopped = false, true
	h := p.host
	p.mu.Unlock()
	if h == nil {
		return nil
	}
	return h.Close(ctx)
}

func (s *Server) projectAssistantFeature(ctx context.Context) (pa.FeatureState, error) {
	p := &s.projectAssistants
	p.mu.RLock()
	h, allowed := p.host, p.allowed && !p.stopped
	p.mu.RUnlock()
	if h == nil {
		return pa.FeatureState{}, nil
	}
	f, err := h.Status(ctx)
	// Capacity consumes the last actual status inspection, without issuing
	// one SQL query per measured row. A retained Host is not proof that its
	// installed schema remains readable. Only a fresh success clears this.
	p.mu.Lock()
	p.inspectionFailed = err != nil
	p.mu.Unlock()
	f.Enabled = f.Enabled && allowed
	return f, err
}

// Source is independent current machine policy. Human contexts are rechecked
// during runtime admission; actual actor-owned backgrounds carry no bearer.
func (s *Server) projectAssistantSource(ctx context.Context, key pa.BotKey, write bool) (pa.SourceStatus, error) {
	i, err := s.projectAssistantIdentity()
	if err != nil {
		return pa.SourceStatus{}, err
	}
	p := &s.projectAssistants
	p.mu.RLock()
	same := p.machine == i.MachineID && p.account == i.AccountID
	allowed := p.allowed && !p.stopped
	p.mu.RUnlock()
	if !same || key.MachineID != i.MachineID {
		return pa.SourceStatus{}, paHTTPFailure(pa.ScopeDenied, "source_identity_changed")
	}
	v, err := s.settingsFile().Read()
	if err != nil {
		return pa.SourceStatus{}, err
	}
	f, err := s.projectAssistantFeature(ctx)
	if err != nil {
		return pa.SourceStatus{}, err
	}
	f.Enabled = f.Enabled && allowed && v.ProjectAssistantsEnabled()
	source := pa.SourceStatus{MachineID: i.MachineID, State: pa.SourceOnline,
		Readable: true, Writable: f.Enabled, Fresh: true, ObservedAt: time.Now().UTC(), Access: f.Access()}
	if a, ok := ctx.Value(accessKey{}).(access); ok {
		_, _, _, writable, err := s.projectAssistantHuman(ctx, a, i)
		if err != nil {
			return source, err
		}
		source.Writable = source.Writable && writable
	}
	if write {
		err = pa.CheckSource(source, true)
	}
	return source, err
}

func (s *Server) projectAssistantHuman(ctx context.Context, a access, i adaptercloud.Identity) (principal, reader, revision string, writable bool, err error) {
	if a.gate == nil || a.gate != s.gate() || a.machine {
		err = paHTTPFailure(pa.ScopeDenied, "gate_proof")
		return
	}
	v := a.gate.auth.Revalidate(a.verdict)
	if !v.Allowed || !v.Caps.Has(auth.Read) {
		err = paHTTPFailure(pa.Revoked, "gate_proof")
		return
	}
	if sender := cloudtransport.CloudViewerOf(ctx); sender != "" {
		grant, e := adaptercloud.NewPinnedStore(filepath.Join(s.cfg.Dir, cloudkeys.DirName)).ProjectAssistantGrant(i.AccountID, sender)
		if e != nil {
			err = errors.Join(paHTTPFailure(pa.Revoked, "viewer_grant"), e)
			return
		}
		return grant.PrincipalID, grant.ReaderID, grant.AuthorizationRevision + ":" + v.AuthorizationRevision(), grant.Writable && v.Caps.Has(auth.Send), nil
	}
	// Cloud transport without a verified envelope sender must never inherit
	// the local token's account-owner exemption or a LAN device namespace.
	if cloudtransport.ViaCloud(ctx) {
		err = paHTTPFailure(pa.ScopeDenied, "viewer_sender")
		return
	}
	if v.Local && i.AccountID != "" {
		principal, reader = adaptercloud.ProjectAssistantOwnerIdentity(i.AccountID)
	} else {
		identity := auth.Hash("project-assistant-device:" + i.AccountID + ":" + i.MachineID + ":" + v.Device)
		principal, reader = "principal:"+identity, "reader:"+identity
	}
	return principal, reader, v.AuthorizationRevision(), v.Caps.Has(auth.Send), nil
}

func (s *Server) projectAssistantAuthority(ctx context.Context, key pa.BotKey, write bool) (app.Authority, error) {
	i, err := s.projectAssistantIdentity()
	if err != nil {
		return app.Authority{}, err
	}
	a, ok := ctx.Value(accessKey{}).(access)
	if !ok {
		return app.Authority{}, paHTTPFailure(pa.ScopeDenied, "gate_proof")
	}
	principal, reader, revision, writable, err := s.projectAssistantHuman(ctx, a, i)
	if err != nil {
		return app.Authority{}, err
	}
	source, err := s.projectAssistantSource(ctx, key, false)
	if err != nil {
		return app.Authority{}, err
	}
	source.Writable = source.Writable && writable
	f, err := s.projectAssistantFeature(ctx)
	f.Enabled = f.Enabled && source.Access == pa.Interactive
	if err == nil && write {
		err = pa.CheckSource(source, true)
	}
	return app.Authority{PrincipalID: principal, ReaderID: reader, AuthorizationRevision: revision, Feature: f, Source: source}, err
}

type projectAssistantProjects struct{ server *Server }

func (p projectAssistantProjects) Resolve(ctx context.Context, target pa.ProjectSelection) (pa.ProjectResolution, error) {
	if err := ctx.Err(); err != nil {
		return pa.ProjectResolution{}, err
	}
	i, err := p.server.projectAssistantIdentity()
	if err != nil {
		return pa.ProjectResolution{}, err
	}
	if target.MachineID != i.MachineID || target.ProjectID == "" {
		return pa.ProjectResolution{}, paHTTPFailure(pa.ScopeDenied, "project_machine")
	}
	// Read saved choices only, then inspect the single selected directory.
	// /v1/projects remains the intentionally retired empty catalog.
	rows, err := projects.OpenPlaceRegistry(p.server.cfg.Dir, swiftDirs()...).List()
	if err != nil {
		return pa.ProjectResolution{}, errors.Join(paHTTPFailure(pa.StaleVersion, "project_registry"), err)
	}
	var selected *projects.RegisteredPlace
	for _, row := range rows {
		if projects.PlaceID(row.Path) != target.ProjectID {
			continue
		}
		if selected != nil {
			return pa.ProjectResolution{}, paHTTPFailure(pa.ScopeDenied, "ambiguous_project")
		}
		copy := row
		selected = &copy
	}
	if selected == nil {
		return pa.ProjectResolution{}, paHTTPFailure(pa.NotFound, "project")
	}
	st, err := os.Stat(selected.Path)
	if err != nil || !st.IsDir() || projects.IsMachineWorkspace(p.server.cfg.Dir, selected.Path) {
		return pa.ProjectResolution{}, paHTTPFailure(pa.NotFound, "project_directory")
	}
	scope, ok := projects.ResolveScope(selected.Path)
	if !ok || selected.ProjectKey == "" || scope.Key != selected.ProjectKey {
		return pa.ProjectResolution{}, paHTTPFailure(pa.StaleVersion, "canonical_project")
	}
	directory, err := filepath.EvalSymlinks(selected.Path)
	if err != nil {
		return pa.ProjectResolution{}, paHTTPFailure(pa.NotFound, "project_directory")
	}
	kind := pa.PlaceProject
	if scope.Kind == projects.ScopeRepository {
		kind = pa.RepositoryProject
	}
	resolved := pa.ProjectResolution{Scope: pa.ProjectScope{MachineID: i.MachineID, ProjectID: target.ProjectID, CanonicalID: scope.Key, Kind: kind}, Directory: directory, Current: true, Available: true}
	return resolved, pa.ValidateTarget(target, resolved)
}

func (s *Server) projectAssistantsRoute(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	p := &s.projectAssistants
	p.mu.RLock()
	h := p.host
	p.mu.RUnlock()
	if h == nil {
		if err := s.StartProjectAssistants(r.Context()); err != nil {
			paWriteHTTPFailure(w, err)
			return
		}
		p.mu.RLock()
		h = p.host
		p.mu.RUnlock()
	}
	i, err := s.projectAssistantIdentity()
	if err != nil || h == nil {
		if err == nil {
			err = paHTTPFailure(pa.Unpaired, "source_identity")
		}
		paWriteHTTPFailure(w, err)
		return
	}
	service, err := h.Service()
	if err != nil && pa.Code(err) != pa.Disabled {
		paWriteHTTPFailure(w, err)
		return
	}
	newProjectAssistantHTTP(projectAssistantHTTPOptions{Service: service, MachineID: i.MachineID, BeforeViewPublish: p.beforeViewPublish,
		Status: func(ctx context.Context) (pa.SourceStatus, error) {
			a, err := s.projectAssistantAuthority(ctx, pa.BotKey{MachineID: i.MachineID}, false)
			return a.Source, err
		}}).ServeHTTP(w, r)
}

// This decoder is deliberately strict for both settings entry points: duplicate
// fields, null, unknown keys and trailing JSON cannot trigger any lifecycle IO.
func decodeProjectAssistantSetting(w http.ResponseWriter, r *http.Request) (bool, error) {
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		return false, paHTTPFailure(pa.Invalid, "content_type")
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, settingsRequestBodyLimit))
	token, err := dec.Token()
	if err != nil || token != json.Delim('{') {
		return false, paHTTPFailure(pa.Invalid, "enabled")
	}
	seen, enabled := false, false
	for dec.More() {
		key, e := dec.Token()
		if e != nil || key != "enabled" || seen {
			return false, paHTTPFailure(pa.Invalid, "enabled")
		}
		var raw json.RawMessage
		if e = dec.Decode(&raw); e != nil || (string(raw) != "true" && string(raw) != "false") {
			return false, paHTTPFailure(pa.Invalid, "enabled")
		}
		seen, enabled = true, string(raw) == "true"
	}
	if _, err = dec.Token(); err != nil || !seen {
		return false, paHTTPFailure(pa.Invalid, "enabled")
	}
	if _, err = dec.Token(); err != io.EOF {
		return false, paHTTPFailure(pa.Invalid, "trailing_json")
	}
	return enabled, nil
}

func (s *Server) projectAssistantSettingsRoute(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	switch r.Method {
	case http.MethodGet:
		f, err := s.projectAssistantFeature(r.Context())
		if err != nil {
			paWriteHTTPFailure(w, err)
			return
		}
		writeJSON(w, contract.ProjectAssistantSettings{Enabled: f.Enabled})
	case http.MethodPost:
		enabled, err := decodeProjectAssistantSetting(w, r)
		if err == nil {
			_, err = s.setProjectAssistantSettings(r.Context(), s.settingsFile(), map[string]any{"project_assistants_enabled": enabled})
		}
		if err != nil {
			paWriteHTTPFailure(w, err)
			return
		}
		f, err := s.projectAssistantFeature(r.Context())
		if err != nil {
			paWriteHTTPFailure(w, err)
			return
		}
		writeJSON(w, contract.ProjectAssistantSettings{Enabled: f.Enabled})
	default:
		paWriteHTTPFailure(w, paHTTPFailure(pa.Invalid, "method"))
	}
}

func (s *Server) setProjectAssistantSettings(ctx context.Context, f *nextconfig.File, changes map[string]any) (nextconfig.Values, error) {
	enabled, changed := changes["project_assistants_enabled"].(bool)
	if !changed {
		return s.setBrowserAwareSettings(f, changes)
	}
	a, ok := ctx.Value(accessKey{}).(access)
	if !ok || a.gate == nil || a.gate != s.gate() || a.machine || cloudtransport.ViaCloud(ctx) || cloudtransport.CloudViewerOf(ctx) != "" {
		return nextconfig.Values{}, paHTTPFailure(pa.ScopeDenied, "local_owner")
	}
	if v := a.gate.auth.Revalidate(a.verdict); !v.Allowed || !v.Local {
		return nextconfig.Values{}, paHTTPFailure(pa.ScopeDenied, "local_owner")
	}
	p := &s.projectAssistants
	p.transition.Lock()
	defer p.transition.Unlock()
	// Seal admission before either the file write or Host cleanup. Neither
	// failure can leave new work accepted merely because config still says true.
	p.mu.Lock()
	p.allowed = false
	h := p.host
	p.mu.Unlock()
	if !enabled {
		v, saveErr := s.setBrowserAwareSettings(f, changes)
		var stopErr error
		if h == nil && saveErr == nil {
			h, stopErr = s.ensureProjectAssistantHost(ctx)
		}
		if h != nil && stopErr == nil {
			stopErr = h.Disable(ctx)
		}
		if saveErr != nil {
			return v, errors.Join(paHTTPFailure(pa.OutcomeUnknown, "settings_persist"), saveErr, stopErr)
		}
		return v, stopErr
	}
	h, err := s.ensureProjectAssistantHost(ctx)
	if err == nil {
		err = h.Enable(ctx)
	}
	if err != nil {
		// Even an unpaired/failed enable must leave durable opt-in false.
		v, saveErr := f.Set(map[string]any{"project_assistants_enabled": false})
		return v, errors.Join(err, saveErr)
	}
	v, saveErr := s.setBrowserAwareSettings(f, changes)
	if saveErr != nil {
		stopErr := h.Disable(context.WithoutCancel(ctx))
		_, compensateErr := f.Set(map[string]any{"project_assistants_enabled": false})
		return v, errors.Join(paHTTPFailure(pa.OutcomeUnknown, "settings_persist"), saveErr, stopErr, compensateErr)
	}
	p.mu.Lock()
	p.allowed = true
	p.mu.Unlock()
	return v, nil
}
