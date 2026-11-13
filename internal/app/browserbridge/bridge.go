// Package browserbridge carries an agent's browser commands to the person's
// Chrome and keeps the record of what was done there.
//
// The path is: `clawdline browser` (an agent, machine-authenticated) → the
// daemon → `clawdline browser-host` (Chrome's native messaging host, which
// long-polls the daemon) → the Clawdline extension (chrome.debugger). The
// daemon never accepts a WebSocket (internal/adapters/cloud/websocket.go), so
// the host polls instead of holding a socket open to it.
//
// The bridge decides nothing about pages. It owns four facts: which Session
// opened which tab, whether the person has taken a tab over, whether the
// person has stopped everything, and the bounded audit of every command.
package browserbridge

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// Bounds, registered in internal/domain/capacity (register_browser.go).
const (
	// MaxPendingCommands is how many commands may wait for the host at once.
	MaxPendingCommands = 32
	// MaxAuditRows is how many audit rows the bridge keeps; the oldest goes first.
	MaxAuditRows = 2000
	// CommandTimeoutSeconds is how long an agent waits for the extension.
	CommandTimeoutSeconds = 30
	// HostPollSeconds is how long one host poll is held open with nothing to send.
	HostPollSeconds = 25
	// HostStaleSeconds is how long after its last poll the host still counts as connected.
	HostStaleSeconds = 60
	// MaxResultBytes bounds one result the host posts (a screenshot is the largest).
	MaxResultBytes = 6 << 20
	// MaxArgBytes bounds one agent command's arguments.
	MaxArgBytes = 64 << 10
	// MaxActivityThumbs is how many recent actions keep a small picture of the
	// page after them, in memory only, for the panel's recent-actions list.
	MaxActivityThumbs = 40
	// MaxThumbBytes bounds one such picture (base64).
	MaxThumbBytes = 96 << 10
)

// Refusal is a typed reason a command was not carried out.
type Refusal string

const (
	WrongMode         Refusal = "browser_wrong_mode"
	Closing           Refusal = "browser_tab_closing"
	Disabled          Refusal = "browser_disabled"
	HostMissing       Refusal = "browser_extension_missing"
	Stopped           Refusal = "browser_stopped"
	PersonControlling Refusal = "browser_person_controlling"
	OtherSessionsTab  Refusal = "browser_tab_other_session"
	QueueFull         Refusal = "browser_queue_full"
	TimedOut          Refusal = "browser_timeout"
	BadRequest        Refusal = "browser_bad_request"
	NoTab             Refusal = "browser_no_tab"
	// OtherDebugger is a tab another program (another extension, DevTools)
	// already controls; Chrome lets only one debugger client hold a tab.
	OtherDebugger     Refusal = "browser_other_debugger"
	StateWriteFailed  Refusal = "browser_state_write_failed"
	SecretInputFailed Refusal = "browser_secret_input_failed"
)

func (r Refusal) Error() string { return string(r) }

// Ops an agent may send. Each is executed by the extension.
var agentOps = map[string]bool{
	"open": true, "tabs": true, "read": true, "click": true, "type": true,
	"key": true, "screenshot": true, "scroll": true, "release": true, "viewport": true, "close": true,
}

// Command is one request handed to the host.
type Command struct {
	ID   string          `json:"id"`
	Op   string          `json:"op"`
	Tab  int             `json:"tab,omitempty"`
	Args json.RawMessage `json:"args,omitempty"`
	// Person marks the person's own command: the extension keeps no picture of it.
	Person       bool `json:"person,omitempty"`
	conversation string
}

// Result is the host's answer to one command.
type Result struct {
	ID    string          `json:"id"`
	OK    bool            `json:"ok"`
	Value json.RawMessage `json:"value,omitempty"`
	Error string          `json:"error,omitempty"`
	// Facts the extension reports for the audit, never shown to the agent.
	URL      string `json:"url,omitempty"`
	Target   string `json:"target,omitempty"`
	Password bool   `json:"password,omitempty"`
	Tab      int    `json:"tab,omitempty"`
	// Thumb is a small JPEG (base64) of the page after the action.
	Thumb string `json:"thumb,omitempty"`
}

// Activity is one recent action with the picture of the page after it.
type Activity struct {
	AuditRow
	Thumb string `json:"thumb"`
}

// HostInfo is what the host says about itself when it polls.
type HostInfo struct {
	Mode     string `json:"mode,omitempty"`
	Version  string `json:"version"`
	Browser  string `json:"browser,omitempty"`
	Profile  string `json:"profile,omitempty"`
	LastPoll int64  `json:"last_poll"`
}

// AuditRow is one recorded command.
type AuditRow struct {
	Mode         string `json:"browser_mode,omitempty"`
	ID           string `json:"id,omitempty"`
	At           int64  `json:"at"`
	Conversation string `json:"conversation"`
	Op           string `json:"op"`
	Tab          int    `json:"tab,omitempty"`
	URL          string `json:"url,omitempty"`
	Site         string `json:"site,omitempty"`
	Target       string `json:"target,omitempty"`
	// Text is what was typed, or empty when it went into a password field.
	Text     string `json:"text,omitempty"`
	TextLen  int    `json:"text_length,omitempty"`
	TextCut  bool   `json:"text_cut,omitempty"`
	Password bool   `json:"password,omitempty"`
	OK       bool   `json:"ok"`
	Error    string `json:"error,omitempty"`
}

// NewSite is announced when a Session operates a site it has not operated
// before (the person's chosen notification rule).
type NewSite struct {
	Conversation string
	Site         string
}

// Bridge is safe for concurrent use.
type Bridge struct {
	mu              sync.Mutex
	queue           []Command
	waiting         map[string]chan Result
	inflight        map[string]string
	wake            chan struct{}
	mode            string   // active Chrome profile kind; native mode only disables this channel
	setupHost       HostInfo // transport readiness, never grants Agent access
	host            HostInfo
	stopped         bool
	stopEpoch       uint64
	disabled        bool
	permissionEpoch uint64
	controlEpoch    map[int]uint64
	person          map[int]bool   // tabs the person has taken over
	owner           map[int]string // tab → conversation that opened it
	closing         map[int]string // outstanding destructive operations, bounded by owned tabs
	current         map[string]int // conversation → the tab it last used
	sites           map[string]map[string]bool
	muted           map[string]bool // conversations whose new-site notification is off
	audit           []AuditRow
	pruned          int
	journal         *journal
	tabsPath        string
	stateErr        error
	thumbs          []Activity // newest last, at most MaxActivityThumbs
	seq             int64
	now             func() time.Time
	onSite          func(NewSite)
	timeout         time.Duration
	pollWait        time.Duration
}

// New makes an empty bridge. onSite may be nil.
func New(onSite func(NewSite)) *Bridge {
	return &Bridge{
		waiting:      map[string]chan Result{},
		inflight:     map[string]string{},
		mode:         "isolated",
		closing:      map[int]string{},
		wake:         make(chan struct{}),
		person:       map[int]bool{},
		controlEpoch: map[int]uint64{},
		owner:        map[int]string{},
		current:      map[string]int{},
		sites:        map[string]map[string]bool{},
		muted:        map[string]bool{},
		now:          time.Now,
		onSite:       onSite,
		timeout:      CommandTimeoutSeconds * time.Second,
		pollWait:     HostPollSeconds * time.Second,
	}
}

func (b *Bridge) hostConnected() bool {
	return b.host.LastPoll != 0 && b.now().Sub(time.Unix(b.host.LastPoll, 0)) <= HostStaleSeconds*time.Second
}

// SetupConnected reports a dedicated host's transport readiness independently
// of the final browser permission. Disabled probes cannot consume commands.
func (b *Bridge) SetupConnected() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.disabled {
		return b.hostConnected()
	}
	return b.setupHost.LastPoll != 0 && b.now().Sub(time.Unix(b.setupHost.LastPoll, 0)) <= HostStaleSeconds*time.Second
}

// ProbeSetupHost holds a readiness-only poll while the Clawdline channel is off.
// The HTTP route admits only the dedicated profile; no page operation is sent.
func (b *Bridge) ProbeSetupHost(ctx context.Context, info HostInfo) bool {
	b.mu.Lock()
	if !b.disabled || (info.Mode != "" && info.Mode != "isolated") {
		b.mu.Unlock()
		return false
	}
	info.LastPoll = b.now().Unix()
	b.setupHost = info
	wake := b.wake
	b.mu.Unlock()
	timer := time.NewTimer(b.pollWait)
	defer timer.Stop()
	select {
	case <-wake:
	case <-timer.C:
	case <-ctx.Done():
	}
	return true
}

// Status is what the console and `clawdline browser status` show.
type Status struct {
	Connected  bool      `json:"connected"`
	Host       HostInfo  `json:"host"`
	Stopped    bool      `json:"stopped"`
	StateError bool      `json:"state_error"`
	PersonTabs []int     `json:"person_tabs"`
	Pending    int       `json:"pending"`
	Tabs       []TabInfo `json:"tabs"`
	Muted      []string  `json:"muted"`
}

// TabInfo is one tab a Session has used: the console finds a Session's tab here.
type TabInfo struct {
	Mode         string `json:"browser_mode,omitempty"`
	Tab          int    `json:"tab"`
	Conversation string `json:"conversation"`
	Person       bool   `json:"person"`
	Current      bool   `json:"current"`
}

func (b *Bridge) Status() Status {
	b.mu.Lock()
	defer b.mu.Unlock()
	people := []int{}
	for t, on := range b.person {
		if on {
			people = append(people, t)
		}
	}
	sort.Ints(people)
	tabs := []TabInfo{}
	for t, c := range b.owner {
		tabs = append(tabs, TabInfo{Tab: t, Conversation: c, Person: b.person[t], Current: b.current[c] == t})
	}
	sort.Slice(tabs, func(i, j int) bool { return tabs[i].Tab < tabs[j].Tab })
	muted := []string{}
	for c := range b.muted {
		muted = append(muted, c)
	}
	sort.Strings(muted)
	return Status{Connected: b.hostConnected() && !b.disabled, Host: b.host, Stopped: b.stopped, StateError: b.stateErr != nil, PersonTabs: people, Pending: len(b.queue), Tabs: tabs, Muted: muted}
}

// SetMuted turns one Session's new-site notification off or back on. It is
// the person's choice for this daemon's lifetime; the machine setting
// browser_notify_new_site still decides for every Session.
func (b *Bridge) SetMuted(conversation string, on bool) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if on {
		b.muted[conversation] = true
	} else {
		delete(b.muted, conversation)
	}
	if b.saveTabs() != nil {
		return StateWriteFailed
	}
	return nil
}

// CheckMode is a read-only preflight. Use ApplyMode to coordinate a settings
// write with command admission; checking and then writing separately can race.
func (b *Bridge) CheckMode(mode string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.checkMode(mode)
}
func (b *Bridge) checkMode(mode string) error {
	if mode != "native" && mode != "isolated" && mode != "user_chrome" {
		return BadRequest
	}
	if mode != "native" && mode != b.mode && len(b.closing) > 0 {
		return Closing
	}
	return nil
}

// ApplyMode orders validation, config persistence and profile selection with
// command admission. persist must not call back into the bridge. A rejected
// precondition never writes config; a config write error leaves runtime intact.
// A later control-state write error leaves access paused and is reported separately.
func (b *Bridge) ApplyMode(mode string, persist func() error) error {
	b.mu.Lock()
	if err := b.checkMode(mode); err != nil {
		b.mu.Unlock()
		return err
	}
	if persist != nil {
		if err := persist(); err != nil {
			b.mu.Unlock()
			return err
		}
	}
	dropped, err := b.setModeLocked(mode)
	b.mu.Unlock()
	for _, c := range dropped {
		b.deliver(Result{ID: c.ID, Error: string(Disabled)})
	}
	return err
}

// SetMode selects one native host. Old pages stay open when profiles change.
func (b *Bridge) SetMode(mode string) error { return b.ApplyMode(mode, nil) }

func (b *Bridge) setModeLocked(mode string) ([]Command, error) {
	var dropped []Command
	if mode != "native" && mode != b.mode {
		dropped = b.queue
		b.queue = nil
		b.mode = mode
		b.host = HostInfo{}
		b.permissionEpoch++
		b.owner = map[int]string{}
		b.current = map[string]int{}
		b.person = map[int]bool{}
		b.controlEpoch = map[int]uint64{}
		b.signal()
		if b.saveTabs() != nil {
			dropped = append(dropped, b.setEnabledLocked(false)...)
			return dropped, StateWriteFailed
		}
	}
	dropped = append(dropped, b.setEnabledLocked(mode != "native")...)
	return dropped, nil
}

// AcceptsHost prevents another profile from consuming this mode's commands.
// Older launchers without a mode belong only to the isolated profile.
func (b *Bridge) AcceptsHost(info HostInfo) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.acceptsHost(info)
}
func (b *Bridge) acceptsHost(info HostInfo) bool {
	mode := info.Mode
	if mode == "" {
		mode = "isolated"
	}
	return !b.disabled && mode == b.mode
}

// SetEnabled revokes the Clawdline channel, including queued viewer input. An
// off/on cycle invalidates old replies without resuming an emergency stop.
func (b *Bridge) SetEnabled(on bool) {
	b.mu.Lock()
	dropped := b.setEnabledLocked(on)
	b.mu.Unlock()
	for _, c := range dropped {
		b.deliver(Result{ID: c.ID, Error: string(Disabled)})
	}
}

func (b *Bridge) setEnabledLocked(on bool) []Command {
	if b.disabled == !on {
		return nil
	}
	b.disabled = !on
	b.permissionEpoch++
	b.signal()
	if on {
		return nil
	}
	dropped := b.queue
	b.queue = nil
	return dropped
}

// SetStopped is the person's emergency stop. Stopping fails every queued command.
func (b *Bridge) SetStopped(on bool) error {
	b.mu.Lock()
	if b.stopped != on {
		b.stopEpoch++
		b.stopped = on
	}
	err := b.saveTabs()
	// A failed resume leaves agent access closed. A failed stop stays stopped
	// in memory, but reports that restart protection has not been saved.
	if err != nil {
		b.stopped = true
	}
	var dropped []Command
	if b.stopped {
		kept := b.queue[:0]
		for _, c := range b.queue {
			if c.Person {
				kept = append(kept, c)
			} else {
				dropped = append(dropped, c)
			}
		}
		b.queue = kept
	}
	b.mu.Unlock()
	for _, c := range dropped {
		b.deliver(Result{ID: c.ID, Error: string(Stopped)})
	}
	if err != nil {
		return StateWriteFailed
	}
	return nil
}

// SetPersonControl marks a tab taken over by the person, or handed back,
// and records the change in the audit under the tab's Session.
func (b *Bridge) SetPersonControl(tab int, on bool) error {
	return b.SetPersonControlForMode(tab, on, "isolated")
}

// SetPersonControlForMode binds a viewer's intent to the profile it saw.
func (b *Bridge) SetPersonControlForMode(tab int, on bool, mode string) error {
	b.mu.Lock()
	if mode == "" {
		mode = "isolated"
	}
	if on && b.disabled {
		b.mu.Unlock()
		return Disabled
	}
	if (!b.disabled || on) && mode != b.mode {
		b.mu.Unlock()
		return WrongMode
	}

	// A close already admitted to the host must settle before takeover can be
	// acknowledged. A timeout is uncertain, so its reservation survives restart.
	if b.closing[tab] != "" {
		b.mu.Unlock()
		return Closing
	}

	was := b.person[tab]
	if on {
		b.person[tab] = true
	} else {
		delete(b.person, tab)
	}
	owner := b.owner[tab]
	var dropped []Command
	if was != on {
		b.controlEpoch[tab]++
		kept := b.queue[:0]
		for _, c := range b.queue {
			if c.Tab == tab && ((!c.Person && on) || (c.Person && !on && c.Op != "screenshot")) {
				dropped = append(dropped, c)
			} else {
				kept = append(kept, c)
			}
		}
		b.queue = kept
	}
	err := b.saveTabs()
	if err != nil && !on {
		b.person[tab] = true
	}
	b.mu.Unlock()
	for _, c := range dropped {
		why := PersonControlling
		if c.Person {
			why = NotTakenOver
		}
		b.deliver(Result{ID: c.ID, Error: string(why)})
	}
	if err != nil {
		return StateWriteFailed
	}
	if was == on {
		return nil
	}
	op := "handback"
	if on {
		op = "takeover"
	}
	b.record(owner, op, tab, "", Result{OK: true, Tab: tab})
	return nil
}

// personOps are what a person's own device may send to a tab: looking at it
// at any time, and acting on it only while they have taken it over.
// field reads the focused field for the person's own check of what they
// sent; an agent cannot ask for it.
var personOps = map[string]bool{"screenshot": true, "click": true, "type": true, "key": true, "scroll": true, "viewport": true, "field": true}

// NotTakenOver is a person's input to a tab they have not taken over.
const NotTakenOver Refusal = "browser_take_over_first"

// PersonDo carries one of the person's own commands to a tab. It is not an
// agent's command: ownership and the agent stop do not apply, and what the
// person types is never recorded.
func (b *Bridge) PersonDo(ctx context.Context, op string, tab int, args json.RawMessage) (json.RawMessage, error) {
	return b.PersonDoForMode(ctx, op, tab, args, "isolated")
}

// PersonDoForMode rejects stale viewer actions even when profiles reuse tab IDs.
func (b *Bridge) PersonDoForMode(ctx context.Context, op string, tab int, args json.RawMessage, mode string) (json.RawMessage, error) {
	return b.personDo(ctx, op, tab, args, mode, nil)
}

func (b *Bridge) PersonDoWithAdmission(ctx context.Context, op string, tab int, args json.RawMessage, mode string, release func()) (json.RawMessage, error) {
	var once sync.Once
	unlock := func() { once.Do(release) }
	defer unlock()
	return b.personDo(ctx, op, tab, args, mode, unlock)
}

func (b *Bridge) personDo(ctx context.Context, op string, tab int, args json.RawMessage, mode string, admitted func()) (json.RawMessage, error) {
	if !personOps[op] || tab <= 0 || len(args) > MaxArgBytes {
		return nil, BadRequest
	}
	if op == "viewport" && !validViewport(args) {
		return nil, BadRequest
	}
	b.mu.Lock()
	if mode == "" {
		mode = "isolated"
	}
	if b.disabled {
		b.mu.Unlock()
		return nil, Disabled
	}
	if mode != b.mode {
		b.mu.Unlock()
		return nil, WrongMode
	}

	if !b.hostConnected() {
		b.mu.Unlock()
		return nil, HostMissing
	}
	if op != "screenshot" && !b.person[tab] {
		b.mu.Unlock()
		return nil, NotTakenOver
	}
	if len(b.queue) >= MaxPendingCommands {
		b.mu.Unlock()
		return nil, QueueFull
	}
	controlEpoch, permissionEpoch := b.controlEpoch[tab], b.permissionEpoch
	c := Command{ID: newID(), Op: op, Tab: tab, Args: args, Person: true, conversation: ""}
	ch := make(chan Result, 1)
	b.waiting[c.ID] = ch
	b.inflight[c.ID] = b.owner[tab]
	b.queue = append(b.queue, c)
	b.signal()
	b.mu.Unlock()
	if admitted != nil {
		admitted()
	}
	timer := time.NewTimer(b.timeout)
	defer timer.Stop()
	var res Result
	select {
	case res = <-ch:
	case <-timer.C:
		res = Result{Error: string(TimedOut)}
	case <-ctx.Done():
		res = Result{Error: string(TimedOut)}
	}
	b.mu.Lock()
	delete(b.waiting, c.ID)
	delete(b.inflight, c.ID)
	b.dropQueued(c.ID)
	if mode != b.mode {
		res = Result{Error: string(WrongMode)}
	} else if b.disabled || b.permissionEpoch != permissionEpoch {
		res = Result{Error: string(Disabled)}
	} else if op != "screenshot" && (!b.person[tab] || b.controlEpoch[tab] != controlEpoch) {
		res = Result{Error: string(NotTakenOver)}
	}
	b.mu.Unlock()
	if res.OK {
		return res.Value, nil
	}
	if Refusal(res.Error) == TimedOut || Refusal(res.Error) == NotTakenOver || Refusal(res.Error) == Disabled || Refusal(res.Error) == WrongMode {
		return nil, Refusal(res.Error)
	}
	if strings.HasPrefix(res.Error, string(OtherDebugger)) {
		return nil, OtherDebugger
	}
	return nil, &ExtensionError{Message: res.Error}
}

// Records returns up to limit audit rows, newest first, and how many were pruned.
func (b *Bridge) Records(conversation string, limit int) ([]AuditRow, int) {
	return b.Find(AuditFilter{Conversation: conversation}, limit)
}

// AuditFilter narrows the audit; an empty field matches every row.
type AuditFilter struct {
	Conversation string
	// Site is a row's registrable domain, as Site returns it.
	Site string
	// Since is a Unix time in seconds; rows before it are left out.
	Since int64
}

func (f AuditFilter) matches(row AuditRow) bool {
	return (f.Conversation == "" || row.Conversation == f.Conversation) &&
		(f.Site == "" || row.Site == f.Site) && row.At >= f.Since
}

// Find returns up to limit audit rows that match f, newest first, and how
// many rows were pruned from the whole audit.
func (b *Bridge) Find(f AuditFilter, limit int) ([]AuditRow, int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := []AuditRow{}
	for i := len(b.audit) - 1; i >= 0 && len(out) < limit; i-- {
		if f.matches(b.audit[i]) {
			out = append(out, b.audit[i])
		}
	}
	return out, b.pruned
}

// RecentActivity includes failed actions and secret input, which have no picture.
// Pictures are joined to their audit rows; their independent retention stays bounded.
func (b *Bridge) RecentActivity(conversation string, limit int) []Activity {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := []Activity{}
	if limit > MaxActivityThumbs {
		limit = MaxActivityThumbs
	}
	pictures := make(map[AuditRow][]string, len(b.thumbs))
	for _, picture := range b.thumbs {
		pictures[picture.AuditRow] = append(pictures[picture.AuditRow], picture.Thumb)
	}
	for i := len(b.audit) - 1; i >= 0 && len(out) < limit; i-- {
		row := b.audit[i]
		if row.Conversation == conversation {
			var thumb string
			if values := pictures[row]; len(values) > 0 {
				thumb = values[len(values)-1]
				pictures[row] = values[:len(values)-1]
			}
			out = append(out, Activity{AuditRow: row, Thumb: thumb})
		}
	}
	return out
}

// Do runs one agent command and waits for the extension's answer.
func (b *Bridge) Do(ctx context.Context, conversation, op string, args json.RawMessage) (json.RawMessage, error) {
	return b.do(ctx, conversation, op, args, nil)
}

// DoWithAdmission releases the selector fence after command admission, before
// waiting for a host. This gives profile selection and in-flight bookkeeping
// one order without holding the selector lock across a long poll.
func (b *Bridge) DoWithAdmission(ctx context.Context, conversation, op string, args json.RawMessage, release func()) (json.RawMessage, error) {
	var once sync.Once
	unlock := func() { once.Do(release) }
	defer unlock()
	return b.do(ctx, conversation, op, args, unlock)
}

func (b *Bridge) do(ctx context.Context, conversation, op string, args json.RawMessage, admitted func()) (value json.RawMessage, err error) {
	if !agentOps[op] || conversation == "" || len(args) > MaxArgBytes {
		return nil, BadRequest
	}
	var a struct {
		Tab    int    `json:"tab"`
		URL    string `json:"url"`
		Text   string `json:"text"`
		Secret bool   `json:"secret"`
	}
	if len(args) > 0 && json.Unmarshal(args, &a) != nil {
		return nil, BadRequest
	}
	if op == "viewport" && !validViewport(args) {
		return nil, BadRequest
	}
	tab := a.Tab
	var recorded Result
	defer func() {
		if recorded.URL == "" {
			recorded.URL = a.URL
		}
		if err != nil && recorded.Error == "" {
			recorded.Error = err.Error()
		}
		// A refused command never reached the field; its sensitivity is unknown.
		if a.Secret || !recorded.OK {
			recorded.Password = true
			recorded.Thumb = ""
		}
		b.record(conversation, op, tab, a.Text, recorded)
	}()
	if op == "open" && !webURL(a.URL) {
		return nil, BadRequest
	}
	b.mu.Lock()
	if b.disabled {
		b.mu.Unlock()
		return nil, Disabled
	}
	if b.stateErr != nil {
		b.mu.Unlock()
		return nil, StateWriteFailed
	}
	if b.stopped {
		b.mu.Unlock()
		return nil, Stopped
	}
	if !b.hostConnected() {
		b.mu.Unlock()
		return nil, HostMissing
	}
	if tab == 0 && op != "open" && op != "tabs" {
		tab = b.current[conversation]
		if tab == 0 {
			b.mu.Unlock()
			return nil, NoTab
		}
	}
	if tab != 0 {
		if b.closing[tab] != "" {
			b.mu.Unlock()
			return nil, Closing
		}

		if (op == "close" || b.mode == "user_chrome") && b.owner[tab] != conversation {
			b.mu.Unlock()
			return nil, OtherSessionsTab
		}
		if owner := b.owner[tab]; owner != "" && owner != conversation {
			b.mu.Unlock()
			return nil, OtherSessionsTab
		}
		if b.person[tab] {
			b.mu.Unlock()
			return nil, PersonControlling
		}
	}
	if len(b.queue) >= MaxPendingCommands {
		b.mu.Unlock()
		return nil, QueueFull
	}
	stopEpoch, controlEpoch, permissionEpoch := b.stopEpoch, b.controlEpoch[tab], b.permissionEpoch
	ownerAtStart := b.owner[tab]
	c := Command{ID: newID(), Op: op, Tab: tab, Args: args, conversation: conversation}
	if op == "close" {
		b.closing[tab] = c.ID
		if b.saveTabs() != nil {
			delete(b.closing, tab)
			b.mu.Unlock()
			return nil, StateWriteFailed
		}
	}
	ch := make(chan Result, 1)
	b.waiting[c.ID] = ch
	b.inflight[c.ID] = conversation
	b.queue = append(b.queue, c)
	b.signal()
	b.mu.Unlock()
	if admitted != nil {
		admitted()
	}

	timer := time.NewTimer(b.timeout)
	defer timer.Stop()
	var res Result
	select {
	case res = <-ch:
	case <-timer.C:
		res = Result{ID: c.ID, Error: string(TimedOut)}
	case <-ctx.Done():
		res = Result{ID: c.ID, Error: string(TimedOut)}
	}
	b.mu.Lock()
	delete(b.waiting, c.ID)
	delete(b.inflight, c.ID)
	if op == "close" {
		for _, queued := range b.queue {
			if queued.ID == c.ID {
				delete(b.closing, tab)
				b.saveTabs()
				break
			}
		}
	}
	b.dropQueued(c.ID)
	// A late answer cannot cross a stop or control handoff, even if control
	// was handed back before the extension answered. Discard frames and text.
	if b.disabled || b.permissionEpoch != permissionEpoch {
		res = Result{ID: c.ID, Tab: tab, Error: string(Disabled)}
	} else if b.stopped || b.stopEpoch != stopEpoch {
		res = Result{ID: c.ID, Tab: tab, Error: string(Stopped)}
	} else if tab != 0 && ownerAtStart != "" && b.owner[tab] != ownerAtStart && !(op == "close" && res.OK && res.Tab == tab) {
		res = Result{ID: c.ID, Tab: tab, Error: string(NoTab)}
	} else if tab != 0 && (b.person[tab] || b.controlEpoch[tab] != controlEpoch) && !(op == "close" && res.OK && res.Tab == tab) {
		res = Result{ID: c.ID, Tab: tab, Error: string(PersonControlling)}
	}
	if res.OK && res.Tab != 0 {
		if op == "open" {
			b.owner[res.Tab] = conversation
		}
		if op == "release" || op == "close" {
			delete(b.owner, res.Tab)
			if op == "close" {
				delete(b.person, res.Tab)
				delete(b.controlEpoch, res.Tab)
			}
			if b.current[conversation] == res.Tab {
				delete(b.current, conversation)
				if op == "close" {
					for other, owner := range b.owner {
						if owner == conversation && other > b.current[conversation] {
							b.current[conversation] = other
						}
					}
				}
			}
		} else if op != "tabs" {
			b.current[conversation] = res.Tab
		}
		if b.saveTabs() != nil {
			res = Result{ID: c.ID, Tab: res.Tab, Error: string(StateWriteFailed)}
		}
	}
	b.mu.Unlock()
	if !res.OK && tab != 0 && res.Error == "the tab was closed" {
		b.Forget(tab)
	}
	// The daemon also protects results from an older or stale worker.
	if a.Secret || op == "type" {
		res.Password = true
	}
	if res.Password {
		res.URL = ""
		res.Target = ""
		res.Thumb = ""
		if res.OK {
			res.Error = ""
		} else {
			res.Error = secretSafeError(res.Error)
		}
	}
	recorded = res
	if res.OK {
		return res.Value, nil
	}
	switch Refusal(res.Error) {
	case Closing, Disabled, Stopped, PersonControlling, TimedOut, StateWriteFailed, SecretInputFailed, NoTab, QueueFull:
		return nil, Refusal(res.Error)
	}
	if strings.HasPrefix(res.Error, string(OtherDebugger)) {
		return nil, OtherDebugger
	}
	return nil, &ExtensionError{Message: res.Error}
}

// ExtensionError is a failure the extension reported (no such element, a
// page that refused, an unreachable address).
type ExtensionError struct{ Message string }

func (e *ExtensionError) Error() string { return e.Message }

// IsRefusal reports whether err is one of the bridge's typed refusals.
func IsRefusal(err error) (Refusal, bool) {
	var r Refusal
	if errors.As(err, &r) {
		return r, true
	}
	return "", false
}

// Poll is the host's long poll: it records the host and returns the queued
// commands, or none after the poll wait.
func (b *Bridge) Poll(ctx context.Context, info HostInfo) []Command {
	b.mu.Lock()
	if !b.acceptsHost(info) {
		b.mu.Unlock()
		return []Command{}
	}

	info.LastPoll = b.now().Unix()
	b.host = info
	if len(b.queue) == 0 {
		wake := b.wake
		b.mu.Unlock()
		timer := time.NewTimer(b.pollWait)
		select {
		case <-wake:
		case <-timer.C:
		case <-ctx.Done():
		}
		timer.Stop()
		b.mu.Lock()
	}
	if !b.acceptsHost(info) {
		b.mu.Unlock()
		return []Command{}
	}
	b.host.LastPoll = b.now().Unix()
	out := b.queue
	b.queue = nil
	b.mu.Unlock()
	if out == nil {
		out = []Command{}
	}
	return out
}

// Deliver records the host's results.
func (b *Bridge) Deliver(results []Result) error {
	var err error
	for _, r := range results {
		err = errors.Join(err, b.deliver(r))
	}
	return err
}

func (b *Bridge) deliver(r Result) error {
	var persistErr error
	b.mu.Lock()
	for tab, id := range b.closing {
		if id != r.ID {
			continue
		}
		// Only an actual host answer (or a queued cancellation) releases this
		// reservation. The caller's timeout never passes through deliver.
		owner, current, person, epoch := b.owner[tab], b.current[b.owner[tab]], b.person[tab], b.controlEpoch[tab]
		delete(b.closing, tab)
		if r.OK || (r.Tab == tab && r.Error == "the tab was closed") {
			b.forgetLocked(tab)
			for other, conversation := range b.owner {
				if conversation == owner && other > b.current[owner] {
					b.current[owner] = other
				}
			}
		}
		if b.saveTabs() != nil {
			// No ACK until the closure is durable. Keep the reservation so
			// the same result can safely settle it on a later delivery.
			b.closing[tab] = r.ID
			if owner != "" {
				b.owner[tab], b.current[owner] = owner, current
			}
			b.person[tab], b.controlEpoch[tab] = person, epoch
			persistErr = StateWriteFailed
			r = Result{ID: r.ID, Tab: tab, Error: string(StateWriteFailed)}
		}
		break
	}
	ch := b.waiting[r.ID]
	b.mu.Unlock()
	if ch != nil {
		select {
		case ch <- r:
		default:
		}
	}
	return persistErr
}

func (b *Bridge) record(conversation, op string, tab int, text string, r Result) {
	row := AuditRow{ID: newID(), At: b.now().Unix(), Conversation: conversation, Op: op, Tab: tab, URL: r.URL,
		Site: Site(r.URL), Target: r.Target, OK: r.OK, Error: r.Error, Password: r.Password}
	// Retain the admitted command identity so legacy result retries still
	// resolve to this profile after its waiting caller has returned.
	if r.ID != "" {
		row.ID = r.ID
	}
	if r.Tab != 0 {
		row.Tab = r.Tab
	}
	if op == "type" && r.Password {
		row.URL = ""
		row.Site = ""
		row.Target = ""
		row.Error = ""
		if !r.OK {
			row.Error = secretSafeError(r.Error)
		}
	}
	if op == "type" {
		row.TextLen = len([]rune(text))
		if !r.Password {
			row.Text, row.TextCut = cutText(text)
		}
	}
	var announce *NewSite
	b.mu.Lock()
	row.Mode = b.mode
	if !r.Password && r.Thumb != "" && len(r.Thumb) <= MaxThumbBytes && conversation != "" {
		b.thumbs = append(b.thumbs, Activity{AuditRow: row, Thumb: r.Thumb})
		if len(b.thumbs) > MaxActivityThumbs {
			b.thumbs = append([]Activity(nil), b.thumbs[len(b.thumbs)-MaxActivityThumbs:]...)
		}
	}
	b.audit = append(b.audit, row)
	if len(b.audit) > MaxAuditRows {
		b.pruned += len(b.audit) - MaxAuditRows
		b.audit = append([]AuditRow(nil), b.audit[len(b.audit)-MaxAuditRows:]...)
	}
	if r.OK && row.Site != "" {
		seen := b.sites[conversation]
		if seen == nil {
			seen = map[string]bool{}
			b.sites[conversation] = seen
		}
		if !seen[row.Site] {
			seen[row.Site] = true
			if !b.muted[conversation] {
				announce = &NewSite{Conversation: conversation, Site: row.Site}
			}
		}
	}
	onSite := b.onSite
	var keep []AuditRow
	pruned := b.pruned
	if b.journal != nil {
		keep = append([]AuditRow(nil), b.audit...)
	}
	b.mu.Unlock()
	if b.journal != nil {
		b.journal.append(row, keep, pruned)
	}
	if announce != nil && onSite != nil {
		onSite(*announce)
	}
}

func (b *Bridge) signal() {
	close(b.wake)
	b.wake = make(chan struct{})
}

func (b *Bridge) dropQueued(id string) {
	for i, c := range b.queue {
		if c.ID == id {
			b.queue = append(b.queue[:i], b.queue[i+1:]...)
			return
		}
	}
}

// Site is the registrable part of a page's host, as far as it can be told
// without a public-suffix list: the last two labels, or three when the
// second-to-last is a common second-level label (co.uk, com.tw).
func Site(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if strings.Count(host, ".") == 0 || strings.Trim(host, "0123456789.") == "" || strings.Contains(host, ":") {
		return host
	}
	labels := strings.Split(host, ".")
	n := 2
	if len(labels) >= 3 && secondLevel[labels[len(labels)-2]] {
		n = 3
	}
	if len(labels) < n {
		return host
	}
	return strings.Join(labels[len(labels)-n:], ".")
}

var secondLevel = map[string]bool{"co": true, "com": true, "net": true, "org": true, "gov": true, "edu": true, "ac": true}

func webURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && len(raw) <= 8<<10
}

func newID() string {
	buf := make([]byte, 9)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

// A page can echo a secret into any error string. Only closed refusal codes
// are safe to retain for a sensitive operation.
func secretSafeError(raw string) string {
	switch Refusal(raw) {
	case Disabled, WrongMode, Closing, HostMissing, Stopped, PersonControlling, OtherSessionsTab, QueueFull, TimedOut, BadRequest, NoTab, OtherDebugger, StateWriteFailed, SecretInputFailed:
		return raw
	}
	return string(SecretInputFailed)
}
