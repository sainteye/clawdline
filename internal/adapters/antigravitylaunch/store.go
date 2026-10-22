// Package antigravitylaunch retains private immutable launch context. Caller
// supplied native proofs, not hook payloads, are its trust boundary.
package antigravitylaunch

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/sainteye/clawdline/internal/adapters/antigravity"
	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/adapters/transcript"
	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/squad"
)

// These independently owned resources reuse the observation bridge's budgets.
// Full means refusal, never eviction of an original resumable context.
const (
	MaxLaunches     = antigravity.MaxLiveBindings
	MaxFirstInputs  = MaxLaunches
	MaxHistory      = antigravity.MaxLiveBindings
	MaxReceipts     = antigravity.MaxLiveBindings
	MaxIndexBytes   = antigravity.MaxObservationBytes
	MaxContextBytes = squad.MaxSquadRequestBytes
)

type Config struct {
	StateDir        string // canonical configRoot/antigravity; parent already exists
	SquadLaunchRoot string // exact trusted configRoot/squad/launches
}

// Record is private daemon metadata. Do not return it to a browser. It carries
// no capability bytes. Prepared is durability, not context-loaded authority.
type Record struct {
	Policy           *NativePolicy            `json:"policy,omitempty"`
	Revoked          bool                     `json:"revoked,omitempty"`
	ID               string                   `json:"id"`
	Workspace        string                   `json:"workspace"`
	ContextPath      string                   `json:"context_path"`
	ContextHash      string                   `json:"context_hash"`
	CapabilityPath   string                   `json:"capability_path"`
	CapabilityDigest string                   `json:"capability_digest"`
	RequestDigest    string                   `json:"request_digest"`
	RequestedResume  string                   `json:"requested_resume,omitempty"`
	Backend          string                   `json:"backend,omitempty"`
	TerminalID       string                   `json:"terminal_id,omitempty"`
	Process          ports.AntigravityProcess `json:"process"`
	Prepared         bool                     `json:"prepared"`
	// NativeID changes only after a positive verified binding and an explicit
	// switch policy. Historical mappings retain their original record.
	NativeID        string `json:"native_id,omitempty"`
	BindingSequence uint64 `json:"binding_sequence,omitempty"`
	BindingDigest   string `json:"binding_digest,omitempty"`
}

type Prepared struct {
	Record     Record
	Request    projects.LaunchRequest
	PrivateEnv []string
}

type history struct {
	LaunchID    string `json:"launch_id"`
	ContextHash string `json:"context_hash"`
	Workspace   string `json:"workspace"`
	// This was a positive native observation, never a requested resume ID.
	Evidence ports.AntigravityBinding `json:"evidence"`
}
type index struct {
	Version     int                          `json:"version"`
	Records     map[string]Record            `json:"records"`
	History     map[string]history           `json:"history"`
	Receipts    map[string]ResponseReceipt   `json:"receipts"`
	FirstInputs map[string]FirstInputReceipt `json:"first_inputs,omitempty"`
}

// Store serializes transactions with one mutex and a lifetime nonblocking OS
// lock. It owns three directory handles and a lock; Close releases them. There
// are no goroutines, queues, polling, background cleanup or external calls.
type Store struct {
	mu                    sync.Mutex
	root, launches, squad *privateDir
	lock                  *os.File
	state                 index
	active                map[string]ports.AntigravityProcess
	bytes                 int64
	indexDigest           string
	closed, uncertain     bool
	// Deterministic failure injection is private to filesystem tests.
	beforeWrite  func(string) error
	afterPublish func()
}

func validID(s string) bool { u, e := uuid.Parse(s); return e == nil && u.String() == s }
func validHash(s string) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == 32 && hex.EncodeToString(b) == s
}
func emptyIndex() index {
	return index{Version: 4, Records: map[string]Record{}, History: map[string]history{}, Receipts: map[string]ResponseReceipt{}, FirstInputs: map[string]FirstInputReceipt{}}
}

func Open(c Config) (*Store, error) {
	if !absolute(c.StateDir) || !absolute(c.SquadLaunchRoot) || c.StateDir == c.SquadLaunchRoot {
		return nil, fail("configuration_invalid")
	}
	root, err := openPrivate(c.StateDir, true)
	if err != nil {
		return nil, err
	}
	s := &Store{root: root, state: emptyIndex(), active: map[string]ports.AntigravityProcess{}}
	ok := false
	defer func() {
		if !ok {
			s.Close()
		}
	}()
	s.lock, err = root.lock()
	if err != nil {
		return nil, err
	}
	s.launches, err = root.child("launches", true)
	if err != nil {
		return nil, err
	}
	// A roleless launch does not need a Squad tree. Open it lazily on the first
	// prompt read; no mutable catalog is ever consulted.
	s.squad = &privateDir{path: c.SquadLaunchRoot}
	b, err := root.read("context-index.json", MaxIndexBytes)
	if err == nil {
		decoder := json.NewDecoder(bytes.NewReader(b))
		decoder.DisallowUnknownFields()
		var loaded index
		if decoder.Decode(&loaded) != nil || decoder.Decode(new(any)) != io.EOF {
			return nil, fail("index_invalid")
		}
		s.state = loaded
		// Migrate only known legacy states in memory. The next atomic
		// write publishes v4; old readers refuse rather than drop policy or
		// consumed input authority. Open itself never rewrites an index.
		legacyProofFree := true
		for _, receipt := range s.state.Receipts {
			if receipt.Fenced || receipt.Loaded || receipt.Fence != (transcript.AntigravityContextFence{}) || receipt.Proof != (transcript.AntigravityContextProof{}) {
				legacyProofFree = false
			}
		}
		legacyPolicyFree := true
		for _, record := range s.state.Records {
			if record.Policy != nil || record.Revoked {
				legacyPolicyFree = false
			}
		}
		if legacyPolicyFree && (s.state.Version == 3 || (legacyProofFree && (s.state.Version == 2 || (s.state.Version == 1 && len(s.state.FirstInputs) == 0)))) {
			s.state.Version = 4
		}
		if s.state.FirstInputs == nil {
			s.state.FirstInputs = map[string]FirstInputReceipt{}
		}
		if s.validateIndex() != nil {
			return nil, fail("index_invalid")
		}
		s.bytes = int64(len(b))
		s.indexDigest = hash(b)
	} else if !missing(err) {
		return nil, err
	}
	ok = true
	return s, nil
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.lock != nil {
		s.lock.Close()
	}
	if s.launches != nil {
		s.launches.close()
	}
	if s.root != nil {
		s.root.close()
	}
	return nil
}
func (s *Store) ready(ctx context.Context) error {
	if err := cancelled(ctx); err != nil {
		return err
	}
	if s.closed {
		return fail("closed")
	}
	if s.uncertain {
		return fail("reopen_required")
	}
	if err := s.root.current(); err != nil {
		return err
	}
	if err := s.root.checkLock(s.lock); err != nil {
		return err
	}
	current, err := s.root.read("context-index.json", MaxIndexBytes)
	if s.indexDigest == "" {
		if !missing(err) {
			return fail("index_changed")
		}
	} else if err != nil || hash(current) != s.indexDigest {
		return fail("index_changed")
	}
	return s.launches.current()
}

func (s *Store) validateIndex() error {
	i := s.state
	if i.Version != 4 || i.Records == nil || i.History == nil || i.Receipts == nil || i.FirstInputs == nil || len(i.Records) > MaxLaunches || len(i.History) > MaxHistory || len(i.Receipts) > MaxReceipts || len(i.FirstInputs) > MaxFirstInputs {
		return fail("index_invalid")
	}
	for id, r := range i.Records {
		if id != r.ID || !validID(id) || !absolute(r.Workspace) || !validHash(r.ContextHash) || !validHash(r.CapabilityDigest) || r.ContextPath != filepath.Join(s.launches.path, id, "context.json") || r.CapabilityPath != filepath.Join(s.launches.path, id, "capability") || (r.RequestedResume != "" && !validID(r.RequestedResume)) || (r.NativeID != "" && !validID(r.NativeID)) {
			return fail("index_invalid")
		}
		if !validHash(r.RequestDigest) || (r.Policy != nil && !r.Policy.wellFormed()) {
			return fail("index_invalid")
		}
		if r.Process.PID != 0 && (!completeProcess(r.Process) || r.Process.TerminalID != r.TerminalID || !backend(r.Backend)) {
			return fail("index_invalid")
		}
	}
	for id, h := range i.History {
		r, ok := i.Records[h.LaunchID]
		if !validID(id) || !ok || !r.Prepared || !completeProcess(h.Evidence.Process) || h.ContextHash != r.ContextHash || h.Workspace != r.Workspace || !h.Evidence.Verified || h.Evidence.ConversationID != id || h.Evidence.Sequence == 0 || h.Evidence.Process != r.Process {
			return fail("index_invalid")
		}
	}
	for id, receipt := range i.FirstInputs {
		r, ok := i.Records[id]
		if !ok || !r.Prepared || r.RequestedResume != "" || receipt.LaunchID != id ||
			!validID(receipt.RequestID) || !validHash(receipt.TextDigest) || receipt.ContextHash != r.ContextHash ||
			!completeProcess(receipt.Process) || receipt.Process != r.Process || receipt.Backend != r.Backend ||
			!firstInputPhase(receipt.Phase) {
			return fail("index_invalid")
		}
	}
	for id, r := range i.Receipts {
		launch, ok := i.Records[id]
		if !ok || r.LaunchID != id || r.Status != "PREPARED" || r.Sequence == 0 || !validID(r.ConversationID) || r.Process != launch.Process || !validHash(r.InjectionDigest) || !validHash(r.ResponseDigest) || !validContextReceipt(r) {
			return fail("index_invalid")
		}
	}
	return nil
}
func clone(i index) index {
	n := emptyIndex()
	for k, v := range i.Records {
		n.Records[k] = cloneRecord(v)
	}
	for k, v := range i.History {
		n.History[k] = v
	}
	for k, v := range i.Receipts {
		n.Receipts[k] = v
	}
	for k, v := range i.FirstInputs {
		n.FirstInputs[k] = v
	}
	return n
}
func (s *Store) write(ctx context.Context, n index) error {
	if err := s.ready(ctx); err != nil {
		return err
	}
	b, err := json.Marshal(n)
	if err != nil || len(b) > MaxIndexBytes {
		return fail("index_full")
	}
	if s.beforeWrite != nil {
		if err := s.beforeWrite("index"); err != nil {
			return fail("write_failed")
		}
	}
	if err := cancelled(ctx); err != nil {
		return err
	}
	if err := s.root.replace("context-index.json", b); err != nil {
		s.uncertain = true
		return err
	}
	if s.afterPublish != nil {
		s.afterPublish()
	}
	// Rename may already be durable if cancellation arrives here. Do not allow
	// this instance to infer a rollback; reopen and retry the same operation.
	if err := cancelled(ctx); err != nil {
		s.uncertain = true
		return err
	}
	s.state = n
	s.bytes = int64(len(b))
	s.indexDigest = hash(b)
	return nil
}

func (s *Store) prompt(path string) ([]byte, error) {
	d, err := openPrivate(s.squad.path, false)
	if err != nil {
		return nil, err
	}
	defer d.close()
	return readPrompt(d, path, MaxContextBytes)
}
func (s *Store) load(r Record) (projects.AntigravityContext, error) {
	if !r.Prepared {
		return projects.AntigravityContext{}, fail("not_prepared")
	}
	d, err := s.launches.child(r.ID, false)
	if err != nil {
		return projects.AntigravityContext{}, err
	}
	defer d.close()
	b, err := d.read("context.json", MaxContextBytes)
	if err != nil {
		return projects.AntigravityContext{}, err
	}
	c, err := projects.RestoreAntigravityContext(b, r.ContextHash)
	if err != nil {
		return projects.AntigravityContext{}, fail("hash_mismatch")
	}
	if c.PromptPath != "" {
		p, err := s.prompt(c.PromptPath)
		if err != nil || string(p) != c.Prompt {
			return projects.AntigravityContext{}, fail("prompt_changed")
		}
	}
	return c, nil
}
func (s *Store) restore(workspace, id string) (projects.AntigravityContext, error) {
	h, ok := s.state.History[id]
	if !ok {
		return projects.AntigravityContext{}, fail("unavailable")
	}
	if h.Workspace != workspace {
		return projects.AntigravityContext{}, fail("workspace_conflict")
	}
	r, ok := s.state.Records[h.LaunchID]
	if !ok || r.ContextHash != h.ContextHash {
		return projects.AntigravityContext{}, fail("index_invalid")
	}
	return s.load(r)
}

// Prepare allocates a distinct launch. Keep its ID for subsequent operations.
// For retries across crashes, Root should allocate an ID once and use
// PrepareLaunch: identical retries reuse files and capability, never terminals.
func (s *Store) Prepare(ctx context.Context, req projects.LaunchRequest) (Prepared, error) {
	id, err := uuid.NewRandom()
	if err != nil {
		return Prepared{}, fail("random_unavailable")
	}
	return s.PrepareLaunch(ctx, id.String(), req)
}
func (s *Store) PrepareLaunch(ctx context.Context, id string, req projects.LaunchRequest) (Prepared, error) {
	return s.prepareLaunch(ctx, id, req, nil)
}

func (s *Store) prepareLaunch(ctx context.Context, id string, req projects.LaunchRequest, policy *NativePolicy) (Prepared, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return Prepared{}, err
	}
	if !validID(id) || !absolute(req.ProjectRoot) || req.Assistant != projects.AssistantAntigravity || (req.Resume != "" && !validID(req.Resume)) {
		return Prepared{}, fail("request_invalid")
	}
	canonical, err := filepath.EvalSymlinks(req.ProjectRoot)
	info, e := os.Stat(req.ProjectRoot)
	if err != nil || canonical != req.ProjectRoot || e != nil || !info.IsDir() {
		return Prepared{}, fail("workspace_invalid")
	}
	var c projects.AntigravityContext
	if req.Resume != "" {
		c, err = s.restore(req.ProjectRoot, req.Resume)
		if err != nil {
			return Prepared{}, err
		}
		if req.SquadPromptPath != "" {
			b, e := s.prompt(req.SquadPromptPath)
			if e != nil || string(b) != c.Prompt {
				return Prepared{}, fail("resume_prompt_conflict")
			}
		}
		if req.Persona != "" && c.Prompt == "" {
			return Prepared{}, fail("resume_prompt_conflict")
		}
	} else {
		var prompt []byte
		if req.SquadPromptPath != "" {
			prompt, err = s.prompt(req.SquadPromptPath)
			if err != nil {
				return Prepared{}, err
			}
		}
		if req.Persona != "" && len(prompt) == 0 {
			return Prepared{}, fail("prompt_unavailable")
		}
		c, err = projects.PrepareAntigravityContext(req.SquadPromptPath, string(prompt), req.Memory, req.Language)
		if err != nil {
			return Prepared{}, fail("request_invalid")
		}
	}
	document, digest, err := c.Document()
	if err != nil {
		return Prepared{}, fail("context_too_large")
	}
	req.AntigravityContext = &c
	req.Memory, req.Language, req.SquadPromptPath = c.Memory, c.Language, c.PromptPath
	requestDigest, err := launchRequestDigest(req, policy)
	if err != nil {
		return Prepared{}, err
	}
	r, exists := s.state.Records[id]
	if exists {
		if err := recordAuthority(r); err != nil {
			return Prepared{}, err
		}
	}
	if exists && (r.Workspace != req.ProjectRoot || r.ContextHash != digest || r.RequestedResume != req.Resume || r.RequestDigest != requestDigest || !samePolicy(r.Policy, policy)) {
		return Prepared{}, fail("launch_conflict")
	}
	if !exists {
		if len(s.state.Records) >= MaxLaunches {
			return Prepared{}, fail("launches_full")
		}
		r = Record{ID: id, Workspace: req.ProjectRoot, ContextPath: filepath.Join(s.launches.path, id, "context.json"), ContextHash: digest, CapabilityPath: filepath.Join(s.launches.path, id, "capability"), RequestedResume: req.Resume}
		r.RequestDigest = requestDigest
		r.Policy = copyPolicy(policy)
		// Reserve a bounded slot before creating per-launch files. A crash can
		// leave only this slot's two bounded files, never an unindexed orphan.
		secret := make([]byte, 32)
		if _, err := rand.Read(secret); err != nil {
			return Prepared{}, fail("random_unavailable")
		}
		r.CapabilityDigest = hash([]byte(hex.EncodeToString(secret)))
		n := clone(s.state)
		n.Records[id] = r
		if err := s.write(ctx, n); err != nil {
			return Prepared{}, err
		}
	}
	d, err := s.launches.child(id, true)
	if err != nil {
		return Prepared{}, err
	}
	defer d.close()
	if r.Prepared {
		if _, err := s.load(r); err != nil {
			return Prepared{}, err
		}
		if _, err := s.capability(r); err != nil {
			return Prepared{}, err
		}
	} else {
		// A pending slot has never authorized a process. Recover its known files
		// only; no directory walk or deletion of unrelated bytes is performed.
		capBytes, readErr := d.read("capability", 64)
		if readErr != nil && !missing(readErr) {
			return Prepared{}, readErr
		}
		if !validHash(string(capBytes)) || hash(capBytes) != r.CapabilityDigest {
			if err := d.removePending("capability"); err != nil {
				return Prepared{}, err
			}
			secret := make([]byte, 32)
			if _, err := rand.Read(secret); err != nil {
				return Prepared{}, fail("random_unavailable")
			}
			capBytes = []byte(hex.EncodeToString(secret))
			r.CapabilityDigest = hash(capBytes)
			n := clone(s.state)
			n.Records[id] = r
			if err := s.write(ctx, n); err != nil {
				return Prepared{}, err
			}
			if err := cancelled(ctx); err != nil {
				return Prepared{}, err
			}
			if err := d.exclusive("capability", capBytes); err != nil {
				return Prepared{}, err
			}
		}
		if s.beforeWrite != nil {
			if err := s.beforeWrite("context"); err != nil {
				return Prepared{}, fail("write_failed")
			}
		}
		if err := cancelled(ctx); err != nil {
			return Prepared{}, err
		}
		if err := d.replace("context.json", document); err != nil {
			return Prepared{}, err
		}
		r.Prepared = true
		n := clone(s.state)
		n.Records[id] = r
		if err := s.write(ctx, n); err != nil {
			return Prepared{}, err
		}
	}
	req.AntigravityContext = &c
	req.Memory = c.Memory
	req.Language = c.Language
	req.SquadPromptPath = c.PromptPath
	if err := cancelled(ctx); err != nil {
		return Prepared{}, err
	}
	return Prepared{Record: cloneRecord(r), Request: req, PrivateEnv: []string{"CLAWDLINE_ANTIGRAVITY_CAPABILITY_FILE=" + projects.ShellQuoted(r.CapabilityPath)}}, nil
}

func backend(s string) bool { return s == "tmux" || s == "iterm" || s == "iterm-tmux" }
func completeProcess(p ports.AntigravityProcess) bool {
	b, err := time.Parse(time.RFC3339Nano, p.Birth)
	return p.PID > 0 && p.ParentPID > 0 && err == nil && !b.IsZero() && b.UTC().Format(time.RFC3339Nano) == p.Birth && absolute(p.Executable) && safeText(p.TTY) && safeText(p.TerminalID) && safeText(p.KernelGeneration) && (runtime.GOOS != "linux" || safeText(p.BootID))
}
func (s *Store) record(id string) (Record, error) {
	r, ok := s.state.Records[id]
	if !ok {
		return Record{}, fail("launch_unknown")
	}
	if !r.Prepared {
		return Record{}, fail("not_prepared")
	}
	if err := recordAuthority(r); err != nil {
		return Record{}, err
	}
	return cloneRecord(r), nil
}
func (s *Store) AttachTerminal(ctx context.Context, id, kind, terminal string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return err
	}
	r, err := s.record(id)
	if err != nil {
		return err
	}
	if !backend(kind) || !safeText(terminal) {
		return fail("terminal_invalid")
	}
	if r.TerminalID != "" {
		if r.TerminalID == terminal && r.Backend == kind {
			return nil
		}
		return fail("terminal_conflict")
	}
	for other, p := range s.active {
		if other != id && p.TerminalID == terminal {
			return fail("terminal_conflict")
		}
	}
	r.Backend = kind
	r.TerminalID = terminal
	n := clone(s.state)
	n.Records[id] = r
	return s.write(ctx, n)
}

// PinProcess requires independent fresh native proof on every call, including
// after Open. Persisted process bytes alone never reactivate LookupProcess.
func (s *Store) PinProcess(ctx context.Context, id string, p ports.AntigravityProcess) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return err
	}
	r, err := s.record(id)
	if err != nil {
		return err
	}
	if !completeProcess(p) || r.TerminalID != p.TerminalID || !backend(r.Backend) {
		return fail("process_invalid")
	}
	if r.Process.PID != 0 && r.Process != p {
		return fail("process_conflict")
	}
	for other, q := range s.active {
		if other != id && (q.PID == p.PID || q.TerminalID == p.TerminalID) {
			return fail("process_conflict")
		}
	}
	if _, err := s.load(r); err != nil {
		return err
	}
	if _, err := s.capability(r); err != nil {
		return err
	}
	if r.Process != p {
		r.Process = p
		n := clone(s.state)
		n.Records[id] = r
		if err := s.write(ctx, n); err != nil {
			return err
		}
	}
	s.active[id] = p
	if err := cancelled(ctx); err != nil {
		delete(s.active, id)
		return err
	}
	return nil
}
func (s *Store) lookup(p ports.AntigravityProcess) (Record, error) {
	if !completeProcess(p) {
		return Record{}, fail("process_invalid")
	}
	for id, q := range s.active {
		if p == q {
			r, err := s.record(id)
			if err != nil {
				return Record{}, err
			}
			if r.Process != p {
				return Record{}, fail("process_conflict")
			}
			return r, nil
		}
	}
	return Record{}, fail("process_unverified")
}
func (s *Store) LookupProcess(ctx context.Context, p ports.AntigravityProcess) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return Record{}, err
	}
	r, err := s.lookup(p)
	if err != nil {
		return Record{}, err
	}
	if _, err := s.load(r); err != nil {
		return Record{}, err
	}
	if _, err := s.capability(r); err != nil {
		return Record{}, err
	}
	if err := cancelled(ctx); err != nil {
		return Record{}, err
	}
	return r, nil
}
func (s *Store) Deactivate(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return err
	}
	delete(s.active, id)
	return nil
}
func (s *Store) capability(r Record) ([]byte, error) {
	d, err := s.launches.child(r.ID, false)
	if err != nil {
		return nil, err
	}
	defer d.close()
	b, err := d.read("capability", 64)
	if err != nil || !validHash(string(b)) || hash(b) != r.CapabilityDigest {
		return nil, fail("capability_invalid")
	}
	return b, nil
}

// VerifyLaunchCapability authenticates a prepared intent before any process
// pinning. It performs no mutation and exposes no capability bytes. The caller
// must still supply independent kernel and terminal proof before PinProcess.
func (s *Store) VerifyLaunchCapability(ctx context.Context, id, capability string) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return Record{}, err
	}
	if !validID(id) || !validHash(capability) {
		return Record{}, fail("capability_invalid")
	}
	r, err := s.record(id)
	if err != nil {
		return Record{}, err
	}
	expected, err := s.capability(r)
	if err != nil {
		return Record{}, err
	}
	if subtle.ConstantTimeCompare(expected, []byte(capability)) != 1 {
		return Record{}, fail("capability_invalid")
	}
	if _, err := s.load(r); err != nil {
		return Record{}, err
	}
	if err := cancelled(ctx); err != nil {
		return Record{}, err
	}
	return r, nil
}

func (s *Store) Commitment(ctx context.Context, r Record) (antigravity.LaunchCommitment, antigravity.HookLaunch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return antigravity.LaunchCommitment{}, antigravity.HookLaunch{}, err
	}
	current, err := s.lookup(r.Process)
	if err != nil || !sameRecord(current, r) {
		return antigravity.LaunchCommitment{}, antigravity.HookLaunch{}, fail("process_unverified")
	}
	if _, err := s.load(r); err != nil {
		return antigravity.LaunchCommitment{}, antigravity.HookLaunch{}, err
	}
	b, err := s.capability(r)
	if err != nil {
		return antigravity.LaunchCommitment{}, antigravity.HookLaunch{}, err
	}
	if err := cancelled(ctx); err != nil {
		return antigravity.LaunchCommitment{}, antigravity.HookLaunch{}, err
	}
	return antigravity.LaunchCommitment{ID: r.ID, CapabilityPath: r.CapabilityPath, CapabilityDigest: r.CapabilityDigest, RequestedConversationID: r.RequestedResume, Process: r.Process}, antigravity.HookLaunch{ID: r.ID, Capability: string(b)}, nil
}

type BindingPolicy string

const (
	KeepConversation        BindingPolicy = "keep"
	AllowConversationSwitch BindingPolicy = "allow_new_native_conversation"
)

func bindingDigest(b ports.AntigravityBinding) string { v, _ := json.Marshal(b); return hash(v) }
func (s *Store) RecordBinding(ctx context.Context, id string, b ports.AntigravityBinding, policy BindingPolicy) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return err
	}
	r, err := s.lookup(b.Process)
	if err != nil {
		return err
	}
	if r.ID != id || !b.Verified || !validID(b.ConversationID) || b.Sequence == 0 || b.ObservedAt.IsZero() || !absolute(b.TranscriptPath) {
		return fail("binding_invalid")
	}
	if policy != KeepConversation && policy != AllowConversationSwitch {
		return fail("binding_policy_invalid")
	}
	digest := bindingDigest(b)
	if b.Sequence <= r.BindingSequence {
		if b.Sequence == r.BindingSequence && digest == r.BindingDigest {
			return nil
		}
		return fail("binding_conflict")
	}
	if r.NativeID != "" && r.NativeID != b.ConversationID && policy != AllowConversationSwitch {
		return fail("switch_requires_policy")
	}
	for other := range s.active {
		q := s.state.Records[other]
		if other != id && q.NativeID == b.ConversationID {
			return fail("conversation_conflict")
		}
	}
	h, exists := s.state.History[b.ConversationID]
	if exists && (h.Workspace != r.Workspace || h.ContextHash != r.ContextHash) {
		return fail("conversation_conflict")
	}
	if !exists && len(s.state.History) >= MaxHistory {
		return fail("history_full")
	}
	if _, err := s.load(r); err != nil {
		return err
	}
	n := clone(s.state)
	if !exists {
		n.History[b.ConversationID] = history{LaunchID: id, ContextHash: r.ContextHash, Workspace: r.Workspace, Evidence: b}
	}
	r.NativeID = b.ConversationID
	r.BindingSequence = b.Sequence
	r.BindingDigest = digest
	n.Records[id] = r
	return s.write(ctx, n)
}

func (s *Store) Restore(ctx context.Context, canonicalProject, conversationID string) (projects.AntigravityContext, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return projects.AntigravityContext{}, err
	}
	if !absolute(canonicalProject) || !validID(conversationID) {
		return projects.AntigravityContext{}, fail("request_invalid")
	}
	c, err := s.restore(canonicalProject, conversationID)
	if err != nil {
		return projects.AntigravityContext{}, err
	}
	if err := cancelled(ctx); err != nil {
		return projects.AntigravityContext{}, err
	}
	return c, nil
}
func (s *Store) WorkspaceBindings(ctx context.Context) (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return nil, err
	}
	out := make(map[string]string, len(s.state.History))
	for id, h := range s.state.History {
		if err := cancelled(ctx); err != nil {
			return nil, err
		}
		if _, err := s.restore(h.Workspace, id); err != nil {
			return nil, err
		}
		out[id] = h.Workspace
	}
	return out, nil
}

type Stats struct {
	Launches, Pending, Active, History, Receipts, FirstInputs int
	IndexBytes, ContextBytes                                  int64
	LargestContextBytes                                       int64
}

func (s *Store) Stats(ctx context.Context) (Stats, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return Stats{}, err
	}
	out := Stats{Launches: len(s.state.Records), Active: len(s.active), History: len(s.state.History), Receipts: len(s.state.Receipts), FirstInputs: len(s.state.FirstInputs), IndexBytes: s.bytes}
	for _, r := range s.state.Records {
		if err := cancelled(ctx); err != nil {
			return Stats{}, err
		}
		if !r.Prepared {
			out.Pending++
			continue
		}
		c, err := s.load(r)
		if err != nil {
			return Stats{}, err
		}
		b, _, err := c.Document()
		if err != nil {
			return Stats{}, fail("hash_mismatch")
		}
		out.ContextBytes += int64(len(b))
		if int64(len(b)) > out.LargestContextBytes {
			out.LargestContextBytes = int64(len(b))
		}
	}
	return out, nil
}
