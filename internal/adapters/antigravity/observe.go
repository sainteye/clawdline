// Package antigravity implements an opt-in, local-only observation bridge.
package antigravity

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/session"
)

const (
	MaxHookPayloadBytes     = 1 << 20
	MaxObservationBytes     = 4 << 20
	MaxLiveBindings         = 512
	MaxAncestry             = 32
	MaxObservationAge       = 15 * time.Second
	MaxObservationRetention = 24 * time.Hour
	MaxProcessReadTime      = 3 * time.Second
)

// BridgeError codes are safe to publish; raw payloads and paths are not errors.
type BridgeError struct{ Code string }

func (e *BridgeError) Error() string { return e.Code }
func (e *BridgeError) AntigravityNotStandalone() bool {
	return e.Code == "antigravity_process_not_standalone"
}
func refuse(code string) error { return &BridgeError{Code: code} }

// PathResolver is the single transcript-package resolver wired by the app.
// Its contract is canonical confinement, UUID/directory agreement and no symlinks.
type PathResolver func(home, conversationID string) (string, bool)

// Witness is local helper metadata acquired independently of stdin. Root must
// use its own PID/birth and a durable, increasing sequence per provider process.
// It must never expose Observe as a remote event-injection endpoint.
type Witness struct {
	HelperPID                      int
	HelperBirth                    string
	HelperBootID, HelperGeneration string
	At                             time.Time
	Sequence                       uint64
	LaunchID, Capability           string
}

// LaunchCommitment is registered by trusted launch composition, never stdin.
// CapabilityPath names a private, current file containing the launch capability.
// RequestedConversationID is provisional and cannot manufacture a binding.
type LaunchCommitment struct {
	ID, CapabilityPath, CapabilityDigest, RequestedConversationID string
	Process                                                       ports.AntigravityProcess
}

type Observation struct {
	ConversationID    string  `json:"conversationId"`
	TranscriptPath    string  `json:"transcriptPath"`
	Model             string  `json:"modelName,omitempty"`
	FullyIdle         *bool   `json:"fullyIdle,omitempty"`
	TerminationReason string  `json:"terminationReason,omitempty"`
	InvocationNum     *uint64 `json:"invocationNum,omitempty"`
	ExecutionNum      *uint64 `json:"executionNum,omitempty"`
	InitialNumSteps   *uint64 `json:"initialNumSteps,omitempty"`
	StepIdx           *uint64 `json:"stepIdx,omitempty"`
	// Error text is deliberately not retained; only its presence matters.
	Failed bool `json:"-"`
}

type retained struct {
	Binding      ports.AntigravityBinding `json:"binding"`
	Digest       string                   `json:"digest"`
	LaunchID     string                   `json:"launchId,omitempty"`
	EventDigests map[string]string        `json:"eventDigests,omitempty"`
}
type diskState struct {
	Version int                 `json:"version"`
	Entries map[string]retained `json:"entries"`
}

// Bridge serializes validate/persist/publish with a mutex; it starts no
// goroutines, queues or retries. Persistence failure never publishes authority.
type Bridge struct {
	mu                          sync.Mutex
	home, statePath, executable string
	executableInfo              os.FileInfo
	executableDigest            string
	reader                      ports.AntigravityProcessReader
	resolve                     PathResolver
	now                         func() time.Time
	entries                     map[string]retained
	launches                    map[string]LaunchCommitment
	bytes                       int64
}

// NewBridge pins a standalone executable already fingerprinted as supported by
// launch admission. It never runs it or infers support from an agy basename.
func NewBridge(home, statePath, executable string, reader ports.AntigravityProcessReader, resolve PathResolver) (*Bridge, error) {
	if reader == nil || resolve == nil || !filepath.IsAbs(executable) || !filepath.IsAbs(statePath) {
		return nil, refuse("antigravity_configuration_invalid")
	}
	info, err := os.Lstat(executable)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&0111 == 0 {
		return nil, refuse("antigravity_executable_invalid")
	}
	digest, err := fileDigest(executable)
	if err != nil {
		return nil, refuse("antigravity_executable_unreadable")
	}
	// Hash once, then pin the post-read kernel change stamp. Inventory must
	// not reread the entire executable for every session on every poll.
	after, err := os.Lstat(executable)
	if err != nil || !sameExecutable(info, after) {
		return nil, refuse("antigravity_executable_changed")
	}
	info = after
	b := &Bridge{home: home, statePath: statePath, executable: executable, executableInfo: info, executableDigest: digest, reader: reader, resolve: resolve, now: time.Now, entries: map[string]retained{}, launches: map[string]LaunchCommitment{}}
	f, err := os.Open(statePath)
	if errors.Is(err, os.ErrNotExist) {
		return b, nil
	}
	if err != nil {
		return nil, refuse("antigravity_store_unreadable")
	}
	defer f.Close()
	info, err = os.Lstat(statePath)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, refuse("antigravity_store_invalid")
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxObservationBytes+1))
	if err != nil || len(data) > MaxObservationBytes {
		return nil, refuse("antigravity_store_unreadable")
	}
	var state diskState
	if json.Unmarshal(data, &state) != nil || state.Version != 1 || len(state.Entries) > MaxLiveBindings {
		return nil, refuse("antigravity_store_invalid")
	}
	b.bytes = int64(len(data))
	for key, entry := range state.Entries {
		if len(entry.EventDigests) > 6 {
			return nil, refuse("antigravity_store_invalid")
		}
		for event := range entry.EventDigests {
			if !eventAllowed(event) {
				return nil, refuse("antigravity_store_invalid")
			}
		}
		entry.Binding.Verified = false
		entry.Binding.StateFresh = false
		entry.Binding.State = session.StateUnknown
		b.entries[key] = entry
	}
	return b, nil
}

func fileDigest(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func validID(id string) bool { u, err := uuid.Parse(id); return err == nil && u.String() == id }
func processKey(p ports.AntigravityProcess) string {
	return fmt.Sprintf("%d/%s/%s/%s", p.PID, p.Birth, p.BootID, p.KernelGeneration)
}
func sameProcess(a, b ports.AntigravityProcess) bool {
	return a.PID == b.PID && a.Birth != "" && a.Birth == b.Birth && a.BootID == b.BootID && a.KernelGeneration != "" && a.KernelGeneration == b.KernelGeneration && a.Executable == b.Executable && a.TTY != "" && a.TTY == b.TTY && a.TerminalID != "" && a.TerminalID == b.TerminalID
}
func (b *Bridge) supported(p ports.AntigravityProcess) bool {
	if p.Executable != b.executable || p.PID <= 0 || p.Birth == "" || p.KernelGeneration == "" || p.TTY == "" || p.TerminalID == "" {
		return false
	}
	info, err := os.Lstat(p.Executable)
	if err != nil || !info.Mode().IsRegular() || !sameExecutable(info, b.executableInfo) {
		return false
	}
	return true
}

// Kernel ctime catches same-inode writes even if size and mtime are restored.
// The native Windows process backend is unavailable; without a supported
// kernel change stamp this bridge refuses live executable authority.
func sameExecutable(a, b os.FileInfo) bool {
	if !os.SameFile(a, b) || a.Mode() != b.Mode() || a.Size() != b.Size() || !a.ModTime().Equal(b.ModTime()) {
		return false
	}
	av, bv := reflect.Indirect(reflect.ValueOf(a.Sys())), reflect.Indirect(reflect.ValueOf(b.Sys()))
	if !av.IsValid() || !bv.IsValid() || av.Kind() != reflect.Struct || bv.Kind() != reflect.Struct {
		return false
	}
	for _, field := range []string{"Ctim", "Ctimespec"} {
		x, y := av.FieldByName(field), bv.FieldByName(field)
		if x.IsValid() && y.IsValid() && x.CanInterface() && y.CanInterface() {
			return reflect.DeepEqual(x.Interface(), y.Interface())
		}
	}
	return false
}

func (b *Bridge) CommitLaunch(c LaunchCommitment) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if c.ID == "" || !b.supported(c.Process) || !filepath.IsAbs(c.CapabilityPath) || len(c.CapabilityDigest) != sha256.Size*2 || (c.RequestedConversationID != "" && !validID(c.RequestedConversationID)) {
		return refuse("antigravity_launch_invalid")
	}
	if old, ok := b.launches[c.ID]; ok {
		if old == c {
			return nil
		}
		return refuse("antigravity_launch_conflict")
	}
	if len(b.launches) >= MaxLiveBindings {
		return refuse("antigravity_bindings_full")
	}
	b.launches[c.ID] = c
	return nil
}

func eventAllowed(event string) bool {
	switch event {
	case "SessionStart", "PreInvocation", "PostInvocation", "PreToolUse", "PostToolUse", "Stop":
		return true
	}
	return false
}

// DecodeObservation reads bounded stdin, ignoring all non-allowlisted content.
// Credentials, workspace lists, tool arguments, email and payload PID never
// enter persisted state. JSON must contain exactly one object.
func DecodeObservation(r io.Reader) (Observation, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxHookPayloadBytes+1))
	if err != nil {
		return Observation{}, refuse("antigravity_payload_unreadable")
	}
	if len(data) > MaxHookPayloadBytes {
		return Observation{}, refuse("antigravity_payload_too_large")
	}
	fields, err := rawObject(data)
	if err != nil {
		return Observation{}, refuse("antigravity_payload_invalid")
	}
	// encoding/json otherwise accepts case-insensitive aliases. Keep the
	// provider's measured spellings as the entire input authority surface.
	allowed := map[string]json.RawMessage{}
	for _, key := range []string{"conversationId", "transcriptPath", "modelName", "fullyIdle", "terminationReason", "invocationNum", "executionNum", "initialNumSteps", "stepIdx", "error"} {
		if value, ok := fields[key]; ok {
			allowed[key] = value
		}
	}
	data, _ = json.Marshal(allowed)
	var raw struct {
		Observation
		Error json.RawMessage `json:"error"`
	}
	if json.Unmarshal(data, &raw) != nil || !validID(raw.ConversationID) || raw.TranscriptPath == "" {
		return Observation{}, refuse("antigravity_payload_invalid")
	}
	errText := strings.TrimSpace(string(raw.Error))
	raw.Failed = errText != "" && errText != "null" && errText != `""`
	if raw.Model != "" && !modelName.MatchString(raw.Model) {
		return Observation{}, refuse("antigravity_model_invalid")
	}
	return raw.Observation, nil
}

var modelName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._/-]*$`)

// ObserverResponse follows the measured native hook protocol. An observer
// never grants tool permission. Squad injection is composed by the app and
// must not be confused with observation persistence or identity authority.
func ObserverResponse(event string) ([]byte, error) {
	if !eventAllowed(event) {
		return nil, refuse("antigravity_event_unsupported")
	}
	if event == "PreToolUse" {
		return []byte("{\"decision\":\"ask\"}\n"), nil
	}
	return []byte("{}\n"), nil
}

func (b *Bridge) ancestor(ctx context.Context, w Witness) (ports.AntigravityProcess, error) {
	ctx, cancel := context.WithTimeout(ctx, MaxProcessReadTime)
	defer cancel()
	first, err := b.reader.ReadAntigravityProcess(ctx, w.HelperPID)
	if err != nil || first.PID != w.HelperPID || first.Birth == "" || first.Birth != w.HelperBirth || first.KernelGeneration == "" || first.KernelGeneration != w.HelperGeneration || first.BootID != w.HelperBootID {
		return ports.AntigravityProcess{}, refuse("antigravity_helper_unverified")
	}
	cur := first
	var candidate ports.AntigravityProcess
	var chain []ports.AntigravityProcess
	seen := map[int]bool{}
	complete := false
	for depth := 0; depth < MaxAncestry; depth++ {
		if ctx.Err() != nil {
			return ports.AntigravityProcess{}, refuse("antigravity_process_unreadable")
		}
		if seen[cur.PID] || cur.PID <= 1 {
			return ports.AntigravityProcess{}, refuse("antigravity_ancestry_unverified")
		}
		seen[cur.PID] = true
		chain = append(chain, cur)
		if cur.PID != first.PID && b.supported(cur) {
			if candidate.PID != 0 {
				return ports.AntigravityProcess{}, refuse("antigravity_ancestry_ambiguous")
			}
			if cur.TTY != first.TTY || cur.TerminalID != first.TerminalID {
				return ports.AntigravityProcess{}, refuse("antigravity_terminal_mismatch")
			}
			candidate = cur
		}
		if cur.ParentPID <= 1 {
			complete = true
			break
		}
		parent, e := b.reader.ReadAntigravityProcess(ctx, cur.ParentPID)
		if e != nil || parent.PID != cur.ParentPID {
			return ports.AntigravityProcess{}, refuse("antigravity_process_unreadable")
		}
		cur = parent
	}
	if candidate.PID != 0 && complete {
		for _, before := range chain {
			after, e := b.reader.ReadAntigravityProcess(ctx, before.PID)
			if e != nil || before.PID != after.PID || before.Birth == "" || before.Birth != after.Birth || before.BootID != after.BootID || before.KernelGeneration != after.KernelGeneration || before.Executable != after.Executable || before.TTY != after.TTY || before.TerminalID != after.TerminalID || before.ParentPID != after.ParentPID {
				return ports.AntigravityProcess{}, refuse("antigravity_helper_changed")
			}
		}
		return candidate, nil
	}
	return ports.AntigravityProcess{}, refuse("antigravity_ancestry_unverified")
}

func (b *Bridge) VerifyAntigravityProcess(ctx context.Context, pid int) (ports.AntigravityProcess, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, MaxProcessReadTime)
	defer cancel()
	p, err := b.reader.ReadAntigravityProcess(ctx, pid)
	if err != nil {
		return ports.AntigravityProcess{}, refuse("antigravity_process_unreadable")
	}
	if p.Executable != "" && p.Executable != b.executable {
		return p, refuse("antigravity_process_not_standalone")
	}
	if p.PID != pid || !b.supported(p) {
		return ports.AntigravityProcess{}, refuse("antigravity_process_unverified")
	}
	return p, nil
}

func (b *Bridge) checkLaunch(w Witness, p ports.AntigravityProcess) error {
	if w.LaunchID == "" {
		for _, c := range b.launches {
			if sameProcess(c.Process, p) {
				return refuse("antigravity_launch_unverified")
			}
		}
		if w.Capability != "" {
			return refuse("antigravity_launch_invalid")
		}
		return nil
	}
	c, ok := b.launches[w.LaunchID]
	if !ok || !sameProcess(c.Process, p) {
		return refuse("antigravity_launch_unverified")
	}
	info, err := os.Lstat(c.CapabilityPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return refuse("antigravity_capability_invalid")
	}
	f, err := os.Open(c.CapabilityPath)
	if err != nil {
		return refuse("antigravity_capability_invalid")
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxHookPayloadBytes+1))
	if err != nil || len(data) > MaxHookPayloadBytes {
		return refuse("antigravity_capability_invalid")
	}
	d := sha256.Sum256(data)
	value := hex.EncodeToString(d[:])
	if value != c.CapabilityDigest || subtle.ConstantTimeCompare(data, []byte(w.Capability)) != 1 {
		return refuse("antigravity_capability_invalid")
	}
	return nil
}

func (b *Bridge) Observe(ctx context.Context, w Witness, event string, r io.Reader) (ports.AntigravityBinding, error) {
	if !eventAllowed(event) {
		return ports.AntigravityBinding{}, refuse("antigravity_event_unsupported")
	}
	o, err := DecodeObservation(r)
	if err != nil {
		return ports.AntigravityBinding{}, err
	}
	if !b.mu.TryLock() {
		return ports.AntigravityBinding{}, refuse("antigravity_bridge_busy")
	}
	defer b.mu.Unlock()
	now := b.now()
	if w.Sequence == 0 || w.At.After(now) || now.Sub(w.At) > MaxObservationAge {
		return ports.AntigravityBinding{}, refuse("antigravity_observation_stale")
	}
	p, err := b.ancestor(ctx, w)
	if err != nil {
		return ports.AntigravityBinding{}, err
	}
	if err = b.checkLaunch(w, p); err != nil {
		return ports.AntigravityBinding{}, err
	}
	path, ok := b.resolve(b.home, o.ConversationID)
	if !ok || path != o.TranscriptPath || !b.confined(path, o.ConversationID) {
		return ports.AntigravityBinding{}, refuse("antigravity_transcript_unverified")
	}
	// Recheck the native path, even when an injected resolver returned it.
	canonical, e := filepath.EvalSymlinks(path)
	if e != nil || canonical != path {
		return ports.AntigravityBinding{}, refuse("antigravity_transcript_unverified")
	}
	info, e := os.Lstat(path)
	if e != nil || !info.Mode().IsRegular() {
		return ports.AntigravityBinding{}, refuse("antigravity_transcript_unverified")
	}
	encoded, _ := json.Marshal(struct {
		Event       string
		Observation Observation
		Failed      bool
	}{event, o, o.Failed})
	digest := sha256.Sum256(encoded)
	hash := hex.EncodeToString(digest[:])
	key := processKey(p)
	old, exists := b.entries[key]
	if exists && (w.Sequence <= old.Binding.Sequence || w.At.Before(old.Binding.ObservedAt) || old.EventDigests[event] == hash) {
		return ports.AntigravityBinding{}, refuse("antigravity_observation_replayed")
	}
	state := session.StateUnknown
	switch event {
	case "SessionStart", "PreInvocation", "PreToolUse", "PostToolUse":
		state = session.StateWorking
	case "Stop":
		if o.FullyIdle != nil && *o.FullyIdle && !o.Failed {
			state = session.StateIdle
		}
	}
	// PostInvocation never creates idle; a finished model call can schedule tools.
	binding := ports.AntigravityBinding{Process: p, ConversationID: o.ConversationID, TranscriptPath: path, Model: o.Model, Event: event, ObservedAt: w.At, Sequence: w.Sequence, Verified: true, StateFresh: true, State: state}
	next := make(map[string]retained, len(b.entries)+1)
	for k, v := range b.entries {
		if now.Sub(v.Binding.ObservedAt) <= MaxObservationRetention {
			next[k] = v
		}
	}
	if _, ok := next[key]; !ok && len(next) >= MaxLiveBindings {
		return ports.AntigravityBinding{}, refuse("antigravity_bindings_full")
	}
	digests := map[string]string{}
	for k, v := range old.EventDigests {
		digests[k] = v
	}
	digests[event] = hash
	next[key] = retained{Binding: binding, Digest: hash, LaunchID: w.LaunchID, EventDigests: digests}
	data, e := json.Marshal(diskState{Version: 1, Entries: next})
	if e != nil || len(data) > MaxObservationBytes {
		return ports.AntigravityBinding{}, refuse("antigravity_store_full")
	}
	if e = atomicReplace(b.statePath, data, 0600); e != nil {
		return ports.AntigravityBinding{}, refuse("antigravity_store_write_failed")
	}
	b.entries = next
	b.bytes = int64(len(data))
	return binding, nil
}

// ObservationStats exposes the actual retained sizes for capacity measurement.
// Launches are a separate map, with the same per-map cardinality ceiling.
type ObservationStats struct {
	Bindings, Launches, Recovered int
	Bytes                         int64
	OldestAt                      time.Time
}

func (b *Bridge) ObservationStats() ObservationStats {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := ObservationStats{Bindings: len(b.entries), Launches: len(b.launches), Bytes: b.bytes}
	for _, entry := range b.entries {
		if s.OldestAt.IsZero() || entry.Binding.ObservedAt.Before(s.OldestAt) {
			s.OldestAt = entry.Binding.ObservedAt
		}
		if !entry.Binding.Verified {
			s.Recovered++
		}
	}
	return s
}

func (b *Bridge) AntigravityBinding(ctx context.Context, s session.Session) (ports.AntigravityBinding, error) {
	if s.Assistant != session.AssistantAntigravity {
		return ports.AntigravityBinding{}, refuse("antigravity_binding_unverified")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, MaxProcessReadTime)
	defer cancel()
	p, err := b.reader.ReadAntigravityProcess(ctx, s.PID)
	if err != nil {
		return ports.AntigravityBinding{}, refuse("antigravity_process_unreadable")
	}
	r, ok := b.entries[processKey(p)]
	if !ok || !r.Binding.Verified || !b.supported(p) || !sameProcess(p, r.Binding.Process) || s.TTY != p.TTY || (s.Backend == session.BackendTmux && s.ID != p.TerminalID) || (!s.ExpectedStart.IsZero() && s.ExpectedStart.UTC().Format(time.RFC3339Nano) != p.Birth) || (s.ConversationID != "" && s.ConversationID != r.Binding.ConversationID) {
		return ports.AntigravityBinding{}, refuse("antigravity_binding_unverified")
	}
	r.Binding.StateFresh = b.now().Sub(r.Binding.ObservedAt) <= MaxObservationAge && !r.Binding.ObservedAt.After(b.now())
	if r.LaunchID != "" {
		c, found := b.launches[r.LaunchID]
		info, e := os.Lstat(c.CapabilityPath)
		if !found || !sameProcess(c.Process, p) || e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return ports.AntigravityBinding{}, refuse("antigravity_launch_unverified")
		}
		digest, e := fileDigest(c.CapabilityPath)
		if e != nil || digest != c.CapabilityDigest {
			return ports.AntigravityBinding{}, refuse("antigravity_capability_invalid")
		}
	}
	if !r.Binding.StateFresh {
		r.Binding.State = session.StateUnknown
	}
	path, ok := b.resolve(b.home, r.Binding.ConversationID)
	canonical, e := filepath.EvalSymlinks(path)
	if !ok || e != nil || canonical != path || path != r.Binding.TranscriptPath || !b.confined(path, r.Binding.ConversationID) {
		return ports.AntigravityBinding{}, refuse("antigravity_transcript_unverified")
	}
	info, e := os.Lstat(path)
	if e != nil || !info.Mode().IsRegular() {
		return ports.AntigravityBinding{}, refuse("antigravity_transcript_unverified")
	}
	return r.Binding, nil
}

// This checks the resolver's result without independently discovering a path.
func (b *Bridge) confined(path, id string) bool {
	root := filepath.Join(b.home, ".gemini", "antigravity-cli", "brain")
	rel, err := filepath.Rel(root, path)
	return err == nil && rel == filepath.Join(id, ".system_generated", "logs", "transcript_full.jsonl")
}

func atomicReplace(path string, data []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".clawdline-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(name, path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
