package browserbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/sainteye/clawdline/internal/domain/browserpolicy"
)

const (
	// MaxSessionBrowsers bounds remembered Session browser selections.
	MaxSessionBrowsers = 256
	// MaxProfileStateBytes bounds the profile selector's persisted state.
	MaxProfileStateBytes = MaxArgBytes
)

// SessionBrowser is a Session choice, never a change to the machine's policy.
type SessionBrowser struct {
	Mode     string `json:"mode"`
	Explicit bool   `json:"explicit"`
}

type profilesState struct {
	Version  int                       `json:"version"`
	Primary  string                    `json:"primary"`
	Stopped  bool                      `json:"stopped"`
	Selected map[string]SessionBrowser `json:"selected"`
}

type ProfileReadiness struct {
	Ready      bool     `json:"ready"`
	Connected  bool     `json:"connected"`
	Host       HostInfo `json:"host"`
	StateError bool     `json:"state_error"`
}

// Profiles keeps Chrome namespaces independent: equal tab numbers in usual
// and test Chrome are different tabs. The selector fence orders Session mode
// changes with command admission; host waits never hold that fence.
type Profiles struct {
	mu          sync.Mutex
	channels    map[string]*Bridge
	primary     string
	selected    map[string]SessionBrowser
	stopped     bool
	path        string
	err         error
	writeFailed bool
}

// OpenProfiles retains the legacy journal and control-state files in place.
// Only the other profile uses a new directory. No old bytes are copied or
// rewritten as a migration, and no Chrome installation is performed here.
func OpenProfiles(dir string, onSite func(NewSite), legacy *Bridge) (*Profiles, error) {
	h := &Profiles{channels: map[string]*Bridge{}, selected: map[string]SessionBrowser{}}
	if legacy == nil {
		if dir == "" {
			legacy = New(onSite)
		} else {
			var err error
			legacy, err = Open(dir, onSite)
			h.err = err
		}
	}
	h.primary = legacy.Mode()
	h.channels[h.primary] = legacy
	h.stopped = legacy.Status().Stopped
	if dir != "" {
		h.path = filepath.Join(dir, "browser-profiles.json")
		if raw, err := readProfileState(h.path); err == nil {
			var saved profilesState
			decoder := json.NewDecoder(bytes.NewReader(raw))
			decoder.DisallowUnknownFields()
			if len(raw) > MaxProfileStateBytes || decoder.Decode(&saved) != nil || decoder.Decode(new(any)) != io.EOF || saved.Version != 1 || saved.Primary != h.primary || len(saved.Selected) > MaxSessionBrowsers {
				h.err = StateWriteFailed
			} else {
				for conversation, choice := range saved.Selected {
					if conversation == "" || len(conversation) > 128 || !browserpolicy.ValidMode(choice.Mode) {
						h.err = StateWriteFailed
						break
					}
				}
				if h.err == nil {
					h.selected = saved.Selected
					if h.selected == nil {
						h.selected = map[string]SessionBrowser{}
					}
					h.stopped = saved.Stopped
				}
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			h.err = err
		}
	}
	for _, mode := range []string{browserpolicy.Isolated, browserpolicy.UserChrome} {
		if h.channels[mode] != nil {
			continue
		}
		var b *Bridge
		var err error
		if dir == "" {
			b = New(onSite)
		} else {
			profileDir := filepath.Join(dir, "browser-profiles", mode)
			if err = os.MkdirAll(profileDir, 0o700); err == nil {
				b, err = Open(profileDir, onSite)
			}
		}
		if err != nil {
			h.err = err
		}
		if b == nil {
			b = New(onSite)
		}
		if b.Mode() != mode {
			if len(b.Status().Tabs) != 0 {
				h.err = StateWriteFailed
			} else if err = b.SetMode(mode); err != nil {
				h.err = err
			}
		}
		h.channels[mode] = b
	}
	for _, b := range h.channels {
		b.SetEnabled(h.err == nil)
		if h.stopped || h.err != nil {
			b.pauseProfile()
		}
	}
	return h, h.err
}

func (h *Profiles) save() error {
	if h.path == "" {
		return nil
	}
	raw, err := json.Marshal(profilesState{Version: 1, Primary: h.primary, Stopped: h.stopped, Selected: h.selected})
	if err != nil || len(raw) > MaxProfileStateBytes {
		return StateWriteFailed
	}
	f, err := os.CreateTemp(filepath.Dir(h.path), ".browser-profiles-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0o600); err == nil {
		_, err = f.Write(raw)
	}
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(f.Name(), h.path); err != nil {
		return err
	}
	return syncProfilesDir(filepath.Dir(h.path))
}

func (h *Profiles) Channel(mode string) (*Bridge, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if mode == "" {
		mode = browserpolicy.Isolated
	}
	b := h.channels[mode]
	if b == nil {
		return nil, WrongMode
	}
	return b, nil
}

// ForgetClosedSession removes only a choice whose Session was actually closed.
// Tabs, audit and uncertain operations remain. A failed close or an incomplete
// inventory is never evidence for this cleanup.
func (h *Profiles) ForgetClosedSession(conversation string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.err != nil {
		return StateWriteFailed
	}
	previous, exists := h.selected[conversation]
	if !exists {
		return nil
	}
	for _, b := range h.channels {
		if err := b.SessionBusy(conversation); err != nil {
			return err
		}
	}
	delete(h.selected, conversation)
	if err := h.save(); err != nil {
		h.selected[conversation] = previous
		h.err, h.writeFailed = err, true
		return StateWriteFailed
	}
	return nil
}

func firstAutomatic(policy browserpolicy.Policy) string {
	for _, mode := range policy.Order {
		if policy.Automatic[mode] {
			return mode
		}
	}
	return ""
}

func (h *Profiles) selection(conversation string, policy browserpolicy.Policy) SessionBrowser {
	if choice, ok := h.selected[conversation]; ok {
		return choice
	}
	// Skip a disconnected Chrome only before this Session has admitted work.
	// Remembered choices never fall through after a possibly executed action.
	for _, mode := range policy.Order {
		if !policy.Automatic[mode] {
			continue
		}
		if mode == browserpolicy.Native {
			return SessionBrowser{Mode: mode}
		}
		b := h.channels[mode]
		if b != nil && b.SetupConnected() && !b.Status().StateError {
			return SessionBrowser{Mode: mode}
		}
	}
	return SessionBrowser{Mode: firstAutomatic(policy)}
}

func (h *Profiles) DoSession(ctx context.Context, conversation, op string, args json.RawMessage, policy browserpolicy.Policy, requested string, explicit bool) (json.RawMessage, error) {
	h.mu.Lock()
	if h.err != nil {
		h.mu.Unlock()
		return nil, StateWriteFailed
	}
	if h.stopped {
		h.mu.Unlock()
		return nil, Stopped
	}
	if !policy.Valid() || conversation == "" || len(conversation) > 128 || (explicit && requested == "") {
		h.mu.Unlock()
		return nil, BadRequest
	}
	choice := h.selection(conversation, policy)
	if requested != "" {
		choice = SessionBrowser{Mode: requested, Explicit: explicit}
	}
	if !browserpolicy.ValidMode(choice.Mode) {
		h.mu.Unlock()
		return nil, Disabled
	}
	if !choice.Explicit && !policy.Automatic[choice.Mode] {
		h.mu.Unlock()
		return nil, Disabled
	}
	for _, channel := range h.channels {
		if channel.SessionBusy(conversation) == PersonControlling {
			h.mu.Unlock()
			return nil, PersonControlling
		}
	}
	if previous, ok := h.selected[conversation]; !ok || previous.Mode != choice.Mode {
		for _, b := range h.channels {
			if err := b.SessionBusy(conversation); err != nil {
				h.mu.Unlock()
				return nil, err
			}
		}
	}
	var b *Bridge
	if choice.Mode != browserpolicy.Native {
		b = h.channels[choice.Mode]
		if b == nil || !b.SetupConnected() {
			h.mu.Unlock()
			return nil, SetupRequired
		}
		if b.Status().StateError {
			h.mu.Unlock()
			return nil, StateWriteFailed
		}
	}
	if choice.Mode == browserpolicy.Native && !choice.Explicit && requested == "" {
		if _, exists := h.selected[conversation]; !exists {
			h.mu.Unlock()
			return nil, NativeTools
		}
	}
	previous, exists := h.selected[conversation]
	if !exists && len(h.selected) >= MaxSessionBrowsers {
		h.mu.Unlock()
		return nil, SessionChoicesFull
	}
	if !exists || previous != choice {
		h.selected[conversation] = choice
		if err := h.save(); err != nil {
			if exists {
				h.selected[conversation] = previous
			} else {
				delete(h.selected, conversation)
			}
			h.err = err
			h.writeFailed = true
			h.mu.Unlock()
			return nil, StateWriteFailed
		}
	}
	if choice.Mode == browserpolicy.Native {
		h.mu.Unlock()
		return nil, NativeTools
	}
	return b.DoWithAdmission(ctx, conversation, op, args, h.mu.Unlock)
}

// SelectedStatus is the current Session's channel, with mode-tagged tabs.
// With no conversation, it reports the default and all owned tabs for older
// readers. New viewers always name their Session, so two equal tab IDs cannot
// redirect a phone frame to the other profile.
func (h *Profiles) SelectedStatus(conversation string, policy browserpolicy.Policy) (string, Status, map[string]ProfileReadiness) {
	h.mu.Lock()
	defer h.mu.Unlock()
	selected := h.selection(conversation, policy)
	st := Status{Stopped: h.stopped, StateError: h.err != nil, PersonTabs: []int{}, Tabs: []TabInfo{}, Muted: []string{}}
	ready := map[string]ProfileReadiness{}
	for _, mode := range []string{browserpolicy.Isolated, browserpolicy.UserChrome} {
		b := h.channels[mode]
		current := b.Status()
		ready[mode] = ProfileReadiness{Ready: b.SetupConnected() && !current.StateError, Connected: current.Connected, Host: current.Host, StateError: current.StateError}
		st.Pending += current.Pending
		st.StateError = st.StateError || current.StateError
		for _, tab := range current.Tabs {
			if conversation != "" && tab.Conversation != conversation {
				continue
			}
			tab.Mode = mode
			chosen := h.selection(tab.Conversation, policy)
			tab.Current = tab.Current && chosen.Mode == mode
			st.Tabs = append(st.Tabs, tab)
		}
		if mode == selected.Mode {
			st.Connected = current.Connected
			st.Host = current.Host
			st.PersonTabs = current.PersonTabs
			st.Muted = current.Muted
		}
	}
	sort.Slice(st.Tabs, func(i, j int) bool {
		if st.Tabs[i].Mode != st.Tabs[j].Mode {
			return st.Tabs[i].Mode < st.Tabs[j].Mode
		}
		return st.Tabs[i].Tab < st.Tabs[j].Tab
	})
	return selected.Mode, st, ready
}

func (h *Profiles) SetStopped(stopped bool) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	// Retry only this process's failed writes. Never overwrite startup bytes
	// that are unreadable, corrupt or from a future schema.
	if h.err != nil && (!h.writeFailed || !stopped) {
		return StateWriteFailed
	}
	h.err = nil
	if stopped {
		h.stopped = true
		if err := h.save(); err != nil {
			h.err = err
		}
		for _, b := range h.channels {
			if err := b.SetStopped(true); err != nil {
				h.err = err
			}
		}
	} else {
		for _, b := range h.channels {
			if err := b.SetStopped(false); err != nil {
				h.err = err
			}
		}
		if h.err == nil {
			h.stopped = false
			if err := h.save(); err != nil {
				h.err = err
			}
		}
		if h.err != nil {
			h.stopped = true
			for _, b := range h.channels {
				_ = b.SetStopped(true)
			}
		}
	}
	h.writeFailed = h.err != nil
	if h.err != nil {
		return StateWriteFailed
	}
	return nil
}

// TabChannel refuses old frames after a Session changed profiles. Mode is
// mandatory when the tab number exists in both Chrome namespaces.
func (h *Profiles) TabChannel(mode string, tab int) (*Bridge, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.tabChannelLocked(mode, tab)
}

func (h *Profiles) tabChannelLocked(mode string, tab int) (*Bridge, error) {
	if h.err != nil {
		return nil, StateWriteFailed
	}
	var chosen *Bridge
	for name, b := range h.channels {
		if mode != "" && mode != name {
			continue
		}
		owner := b.OwnerOf(tab)
		if owner == "" {
			continue
		}
		if current, ok := h.selected[owner]; ok && current.Mode != name {
			continue
		}
		if chosen != nil {
			return nil, WrongMode
		}
		chosen = b
	}
	if chosen == nil {
		return nil, WrongMode
	}
	return chosen, nil
}

// Deliver preserves old native-host compatibility only when a result ID
// unambiguously belongs to one channel. Unknown legacy close receipts remain
// unacknowledged instead of being applied to an unrelated profile's tab.
func (h *Profiles) Deliver(mode string, results []Result) error {
	if h.StateError() {
		return StateWriteFailed
	}
	for _, result := range results {
		var chosen *Bridge
		if mode != "" {
			var err error
			chosen, err = h.Channel(mode)
			if err != nil {
				return err
			}
		} else {
			h.mu.Lock()
			for _, b := range h.channels {
				if b.knowsResult(result.ID) {
					if chosen != nil {
						h.mu.Unlock()
						return WrongMode
					}
					chosen = b
				}
			}
			h.mu.Unlock()
			if chosen == nil {
				return WrongMode
			}
		}
		if err := chosen.Deliver([]Result{result}); err != nil {
			return err
		}
	}
	return nil
}

func (h *Profiles) Find(filter AuditFilter, limit int) ([]AuditRow, int) {
	if limit < 0 {
		limit = 0
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	rows := []AuditRow{}
	pruned := 0
	for mode, b := range h.channels {
		part, cut := b.Find(filter, limit)
		pruned += cut
		for i := range part {
			if part[i].Mode == "" {
				part[i].Mode = mode
			}
		}
		rows = append(rows, part...)
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].At > rows[j].At })
	if len(rows) > limit {
		rows = rows[:limit]
	}
	return rows, pruned
}

func (h *Profiles) RecentActivity(conversation string, limit int) []Activity {
	if limit < 0 {
		limit = 0
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	rows := []Activity{}
	for mode, b := range h.channels {
		part := b.RecentActivity(conversation, limit)
		for i := range part {
			if part[i].Mode == "" {
				part[i].Mode = mode
			}
		}
		rows = append(rows, part...)
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].At > rows[j].At })
	if len(rows) > limit {
		rows = rows[:limit]
	}
	return rows
}

func (h *Profiles) JournalErr() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, b := range h.channels {
		if err := b.JournalErr(); err != nil {
			return err
		}
	}
	return nil
}

func (h *Profiles) SetMuted(conversation string, muted bool) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, b := range h.channels {
		if err := b.SetMuted(conversation, muted); err != nil {
			return err
		}
	}
	return nil
}

func (h *Profiles) SelectionCount() int { h.mu.Lock(); defer h.mu.Unlock(); return len(h.selected) }

func (h *Profiles) SetPersonControl(mode string, tab int, person bool) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	b, err := h.tabChannelLocked(mode, tab)
	if err != nil {
		return err
	}
	return b.SetPersonControlForMode(tab, person, b.Mode())
}

func (h *Profiles) PersonDo(ctx context.Context, mode, op string, tab int, args json.RawMessage) (json.RawMessage, error) {
	h.mu.Lock()
	b, err := h.tabChannelLocked(mode, tab)
	if err != nil {
		h.mu.Unlock()
		return nil, err
	}
	return b.PersonDoWithAdmission(ctx, op, tab, args, b.Mode(), h.mu.Unlock)
}

func (h *Profiles) Legacy() *Bridge  { return h.channels[h.primary] }
func (h *Profiles) StateError() bool { h.mu.Lock(); defer h.mu.Unlock(); return h.err != nil }
func (h *Profiles) Ready(mode string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	b := h.channels[mode]
	return h.err == nil && b != nil && b.SetupConnected() && !b.Status().StateError
}

// CapacityMaxima reports the largest independent profile occupancy. Each
// existing browser queue and journal bound applies per profile, not to a
// fictitious single combined buffer.
func (h *Profiles) CapacityMaxima() (pending, audit, activity int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, b := range h.channels {
		p, a, t := b.capacityOccupancy()
		pending = max(pending, p)
		audit = max(audit, a)
		activity = max(activity, t)
	}
	return
}

func (h *Profiles) StateBytes() (int64, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.err != nil {
		return 0, h.err
	}
	if h.path == "" {
		raw, err := json.Marshal(profilesState{Version: 1, Primary: h.primary, Stopped: h.stopped, Selected: h.selected})
		return int64(len(raw)), err
	}
	info, err := os.Stat(h.path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

func readProfileState(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, MaxProfileStateBytes+1))
}
