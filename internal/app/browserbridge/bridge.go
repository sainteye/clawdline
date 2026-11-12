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
)

// Refusal is a typed reason a command was not carried out.
type Refusal string

const (
	HostMissing       Refusal = "browser_extension_missing"
	Stopped           Refusal = "browser_stopped"
	PersonControlling Refusal = "browser_person_controlling"
	OtherSessionsTab  Refusal = "browser_tab_other_session"
	QueueFull         Refusal = "browser_queue_full"
	TimedOut          Refusal = "browser_timeout"
	BadRequest        Refusal = "browser_bad_request"
	NoTab             Refusal = "browser_no_tab"
)

func (r Refusal) Error() string { return string(r) }

// Ops an agent may send. Each is executed by the extension.
var agentOps = map[string]bool{
	"open": true, "tabs": true, "read": true, "click": true, "type": true,
	"key": true, "screenshot": true, "scroll": true, "release": true,
}

// Command is one request handed to the host.
type Command struct {
	ID           string          `json:"id"`
	Op           string          `json:"op"`
	Tab          int             `json:"tab,omitempty"`
	Args         json.RawMessage `json:"args,omitempty"`
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
}

// HostInfo is what the host says about itself when it polls.
type HostInfo struct {
	Version  string `json:"version"`
	Browser  string `json:"browser,omitempty"`
	Profile  string `json:"profile,omitempty"`
	LastPoll int64  `json:"last_poll"`
}

// AuditRow is one recorded command.
type AuditRow struct {
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
	mu       sync.Mutex
	queue    []Command
	waiting  map[string]chan Result
	wake     chan struct{}
	host     HostInfo
	stopped  bool
	person   map[int]bool   // tabs the person has taken over
	owner    map[int]string // tab → conversation that opened it
	current  map[string]int // conversation → the tab it last used
	sites    map[string]map[string]bool
	muted    map[string]bool // conversations whose new-site notification is off
	audit    []AuditRow
	pruned   int
	journal  *journal
	now      func() time.Time
	onSite   func(NewSite)
	timeout  time.Duration
	pollWait time.Duration
}

// New makes an empty bridge. onSite may be nil.
func New(onSite func(NewSite)) *Bridge {
	return &Bridge{
		waiting:  map[string]chan Result{},
		wake:     make(chan struct{}),
		person:   map[int]bool{},
		owner:    map[int]string{},
		current:  map[string]int{},
		sites:    map[string]map[string]bool{},
		muted:    map[string]bool{},
		now:      time.Now,
		onSite:   onSite,
		timeout:  CommandTimeoutSeconds * time.Second,
		pollWait: HostPollSeconds * time.Second,
	}
}

func (b *Bridge) hostConnected() bool {
	return b.host.LastPoll != 0 && b.now().Sub(time.Unix(b.host.LastPoll, 0)) <= HostStaleSeconds*time.Second
}

// Status is what the console and `clawdline browser status` show.
type Status struct {
	Connected  bool      `json:"connected"`
	Host       HostInfo  `json:"host"`
	Stopped    bool      `json:"stopped"`
	PersonTabs []int     `json:"person_tabs"`
	Pending    int       `json:"pending"`
	Tabs       []TabInfo `json:"tabs"`
	Muted      []string  `json:"muted"`
}

// TabInfo is one tab a Session has used: the console finds a Session's tab here.
type TabInfo struct {
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
	return Status{Connected: b.hostConnected(), Host: b.host, Stopped: b.stopped, PersonTabs: people, Pending: len(b.queue), Tabs: tabs, Muted: muted}
}

// SetMuted turns one Session's new-site notification off or back on. It is
// the person's choice for this daemon's lifetime; the machine setting
// browser_notify_new_site still decides for every Session.
func (b *Bridge) SetMuted(conversation string, on bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if on {
		b.muted[conversation] = true
	} else {
		delete(b.muted, conversation)
	}
}

// SetStopped is the person's emergency stop. Stopping fails every queued command.
func (b *Bridge) SetStopped(on bool) {
	b.mu.Lock()
	b.stopped = on
	var dropped []Command
	if on {
		dropped, b.queue = b.queue, nil
	}
	b.mu.Unlock()
	for _, c := range dropped {
		b.deliver(Result{ID: c.ID, Error: string(Stopped)})
	}
}

// SetPersonControl marks a tab taken over by the person, or handed back,
// and records the change in the audit under the tab's Session.
func (b *Bridge) SetPersonControl(tab int, on bool) {
	b.mu.Lock()
	was := b.person[tab]
	if on {
		b.person[tab] = true
	} else {
		delete(b.person, tab)
	}
	owner := b.owner[tab]
	b.mu.Unlock()
	if was == on {
		return
	}
	op := "handback"
	if on {
		op = "takeover"
	}
	b.record(owner, op, tab, "", Result{OK: true, Tab: tab})
}

// personOps are what a person's own device may send to a tab: looking at it
// at any time, and acting on it only while they have taken it over.
var personOps = map[string]bool{"screenshot": true, "click": true, "type": true, "key": true, "scroll": true}

// NotTakenOver is a person's input to a tab they have not taken over.
const NotTakenOver Refusal = "browser_take_over_first"

// PersonDo carries one of the person's own commands to a tab. It is not an
// agent's command: ownership and the agent stop do not apply, and what the
// person types is never recorded.
func (b *Bridge) PersonDo(ctx context.Context, op string, tab int, args json.RawMessage) (json.RawMessage, error) {
	if !personOps[op] || tab <= 0 || len(args) > MaxArgBytes {
		return nil, BadRequest
	}
	b.mu.Lock()
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
	c := Command{ID: newID(), Op: op, Tab: tab, Args: args, conversation: ""}
	ch := make(chan Result, 1)
	b.waiting[c.ID] = ch
	b.queue = append(b.queue, c)
	b.signal()
	b.mu.Unlock()
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
	b.dropQueued(c.ID)
	b.mu.Unlock()
	if res.OK {
		return res.Value, nil
	}
	if Refusal(res.Error) == TimedOut {
		return nil, TimedOut
	}
	return nil, &ExtensionError{Message: res.Error}
}

// Records returns up to limit audit rows, newest first, and how many were pruned.
func (b *Bridge) Records(conversation string, limit int) ([]AuditRow, int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := []AuditRow{}
	for i := len(b.audit) - 1; i >= 0 && len(out) < limit; i-- {
		if conversation == "" || b.audit[i].Conversation == conversation {
			out = append(out, b.audit[i])
		}
	}
	return out, b.pruned
}

// Do runs one agent command and waits for the extension's answer.
func (b *Bridge) Do(ctx context.Context, conversation, op string, args json.RawMessage) (json.RawMessage, error) {
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
	if op == "open" && !webURL(a.URL) {
		return nil, BadRequest
	}
	b.mu.Lock()
	if b.stopped {
		b.mu.Unlock()
		b.record(conversation, op, 0, a.Text, Result{Error: string(Stopped)})
		return nil, Stopped
	}
	if !b.hostConnected() {
		b.mu.Unlock()
		return nil, HostMissing
	}
	tab := a.Tab
	if tab == 0 && op != "open" && op != "tabs" {
		tab = b.current[conversation]
		if tab == 0 {
			b.mu.Unlock()
			return nil, NoTab
		}
	}
	if tab != 0 {
		if owner := b.owner[tab]; owner != "" && owner != conversation {
			b.mu.Unlock()
			return nil, OtherSessionsTab
		}
		if b.person[tab] {
			b.mu.Unlock()
			b.record(conversation, op, tab, a.Text, Result{Error: string(PersonControlling)})
			return nil, PersonControlling
		}
	}
	if len(b.queue) >= MaxPendingCommands {
		b.mu.Unlock()
		return nil, QueueFull
	}
	c := Command{ID: newID(), Op: op, Tab: tab, Args: args, conversation: conversation}
	ch := make(chan Result, 1)
	b.waiting[c.ID] = ch
	b.queue = append(b.queue, c)
	b.signal()
	b.mu.Unlock()

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
	b.dropQueued(c.ID)
	if res.OK && res.Tab != 0 {
		if op == "open" {
			b.owner[res.Tab] = conversation
		}
		if op == "release" {
			delete(b.owner, res.Tab)
			if b.current[conversation] == res.Tab {
				delete(b.current, conversation)
			}
		} else if op != "tabs" {
			b.current[conversation] = res.Tab
		}
	}
	b.mu.Unlock()
	if a.Secret {
		// A Secret Handoff is never recorded, whatever field it went into.
		res.Password = true
	}
	b.record(conversation, op, tab, a.Text, res)
	if res.OK {
		return res.Value, nil
	}
	switch Refusal(res.Error) {
	case Stopped, PersonControlling, TimedOut:
		return nil, Refusal(res.Error)
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
	info.LastPoll = b.now().Unix()
	b.host = info
	if len(b.queue) == 0 && !b.stopped {
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
		b.host.LastPoll = b.now().Unix()
	}
	out := b.queue
	b.queue = nil
	b.mu.Unlock()
	if out == nil {
		out = []Command{}
	}
	return out
}

// Deliver records the host's results.
func (b *Bridge) Deliver(results []Result) {
	for _, r := range results {
		b.deliver(r)
	}
}

func (b *Bridge) deliver(r Result) {
	b.mu.Lock()
	ch := b.waiting[r.ID]
	b.mu.Unlock()
	if ch != nil {
		select {
		case ch <- r:
		default:
		}
	}
}

func (b *Bridge) record(conversation, op string, tab int, text string, r Result) {
	row := AuditRow{At: b.now().Unix(), Conversation: conversation, Op: op, Tab: tab, URL: r.URL,
		Site: Site(r.URL), Target: r.Target, OK: r.OK, Error: r.Error, Password: r.Password}
	if r.Tab != 0 {
		row.Tab = r.Tab
	}
	if op == "type" {
		row.TextLen = len([]rune(text))
		if !r.Password {
			row.Text, row.TextCut = cutText(text)
		}
	}
	var announce *NewSite
	b.mu.Lock()
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
