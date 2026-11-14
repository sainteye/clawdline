package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/planner"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/copilot"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// Session recaps (docs/session-recaps.md): the machine asks each Session's
// own assistant, from a throwaway fork of its conversation, where it got to,
// and keeps the answer so the voice copilot and the Session list can say it at
// once instead of reading transcripts while the person waits.
//
// It exists because on 2026-11-14 the person said they could not keep track of
// their Sessions through the voice copilot. Its call log showed why: asked
// what the working Sessions were doing, it read each one's newest page and
// answered with the step of the minute ("filling in six new limits' strings"),
// covered two of eight before the call ended, and once answered that the
// briefing was not ready.
//
// When a recap is written:
//
//   - a stopped Session (idle, or waiting for the person) once it has stood
//     still for recapAfterStop, and again only after its conversation grew;
//   - a working Session after recapFirstWhileWorking, then at most every
//     recapWhileWorking while its conversation keeps growing, because "what
//     are the working ones doing" is the question the person asks most.
//
// One fork runs at a time, the most urgent first: a Session waiting for the
// person, then an idle one, then a working one. A failed fork is not retried
// for recapRetryAfter, so an assistant that is out of quota is not asked every
// scan.
const (
	recapAfterStop         = 3 * time.Minute
	recapFirstWhileWorking = 2 * time.Minute
	recapWhileWorking      = 10 * time.Minute
	recapRetryAfter        = 10 * time.Minute
	// maxSessionRecapRows bounds the recaps kept, one per conversation; the
	// one written longest ago gives way.
	maxSessionRecapRows = 200
	// maxSessionRecapDuration bounds one fork, process start to answer.
	maxSessionRecapDuration = 2 * time.Minute
	// maxSessionRecapOnDemand bounds the forks the voice copilot waits for at
	// once, so "what are they all doing" forks a few Sessions side by side
	// rather than one after another, and never all of them.
	maxSessionRecapOnDemand = 3
	// maxSessionRecapWait bounds how long the copilot waits for one fork; a
	// slower one goes on and is kept for the next question.
	maxSessionRecapWait = 45 * time.Second
)

// recapFile is where recaps are kept across restarts, beside copilot.json.
const recapFile = "session-recaps.json"

// recapRow is one stored recap.
type recapRow struct {
	Conversation string `json:"conversation"`
	Assistant    string `json:"assistant"`
	Text         string `json:"text"`
	At           int64  `json:"at"`
	State        string `json:"state"`
	// Signature is the conversation record's size and modification time when
	// the fork was asked (transcript.signature's shape): a recap whose record
	// has not changed since is current.
	Signature string `json:"signature"`
}

// recapMark is when a Session was last seen to change state, in memory only:
// a restart starts every clock again, which costs at most one late recap.
type recapMark struct {
	state session.State
	since time.Time
}

// Recapper forks one conversation and answers with its recap.
type Recapper func(context.Context, planner.RecapTarget) (string, error)

// recapBook decides which Session is due, runs one fork at a time and keeps
// the answers.
type recapBook struct {
	mu      sync.Mutex
	path    string
	rows    map[string]recapRow
	marks   map[string]recapMark
	tried   map[string]time.Time
	failed  map[string]time.Time
	running bool
	loaded  bool

	recap Recapper
	// mode is copilot.Settings.SessionRecaps, read when asked.
	mode func() string
	// onDemand admits forks the voice copilot is waiting for.
	onDemand chan struct{}
	record   func(session.Session) string
	now      func() time.Time
	// done is told when a fork ends; tests wait on it.
	done func(conversation string, err error)
}

func newRecapBook(dir string, recap Recapper, mode func() string, record func(session.Session) string) *recapBook {
	return &recapBook{
		path: filepath.Join(dir, recapFile), rows: map[string]recapRow{}, marks: map[string]recapMark{},
		tried: map[string]time.Time{}, failed: map[string]time.Time{},
		recap: recap, mode: mode, record: record, now: time.Now,
		onDemand: make(chan struct{}, maxSessionRecapOnDemand),
	}
}

// recordSignature is the record's size and modification time, or "" when it
// cannot be read.
func recordSignature(path string) string {
	if path == "" {
		return ""
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return ""
	}
	return fmt.Sprintf("%d-%d", info.Size(), info.ModTime().Unix())
}

func (b *recapBook) load() {
	if b.loaded {
		return
	}
	b.loaded = true
	raw, err := os.ReadFile(b.path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Printf("session recaps: could not read %s: %v", recapFile, err)
		}
		return
	}
	var rows []recapRow
	if err := json.Unmarshal(raw, &rows); err != nil {
		log.Printf("session recaps: %s is not readable JSON; starting empty: %v", recapFile, err)
		return
	}
	for _, r := range rows {
		if r.Conversation != "" && r.Text != "" {
			b.rows[r.Conversation] = r
		}
	}
}

// save writes every row, newest first, through a temporary file and a rename.
// It is called with b.mu held.
func (b *recapBook) save() {
	rows := make([]recapRow, 0, len(b.rows))
	for _, r := range b.rows {
		rows = append(rows, r)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].At > rows[j].At })
	raw, err := json.Marshal(rows)
	if err != nil {
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(b.path), ".session-recaps-*.json")
	if err != nil {
		log.Printf("session recaps: could not save: %v", err)
		return
	}
	name := tmp.Name()
	_, werr := tmp.Write(raw)
	cerr := tmp.Close()
	if werr != nil || cerr != nil || os.Chmod(name, 0o600) != nil || os.Rename(name, b.path) != nil {
		_ = os.Remove(name)
		log.Printf("session recaps: could not save %s", recapFile)
	}
}

// keep stores a recap and lets the oldest go past maxSessionRecapRows. It is
// called with b.mu held.
func (b *recapBook) keep(r recapRow) {
	b.rows[r.Conversation] = r
	for len(b.rows) > maxSessionRecapRows {
		oldest := ""
		for k, v := range b.rows {
			if oldest == "" || v.At < b.rows[oldest].At {
				oldest = k
			}
		}
		delete(b.rows, oldest)
	}
	b.save()
}

// recapCandidate is one Session that may be due, with how urgent it is.
type recapCandidate struct {
	s         session.Session
	signature string
	rank      int
	since     time.Time
}

// Observe reads one scan: it moves each Session's clock and starts the most
// urgent fork that is due, if none is running. It never waits for a fork.
func (b *recapBook) Observe(ctx context.Context, inv session.Inventory) {
	if b == nil || b.recap == nil || b.currentMode() != copilot.RecapBackground {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.load()
	now := b.now()
	seen := map[string]bool{}
	var due []recapCandidate
	for _, s := range inv.Sessions {
		conv := s.ConversationID
		if conv == "" || !planner.RecapCan(string(s.Assistant)) || s.DiscoveryExcluded {
			continue
		}
		if s.Observation.Freshness == session.FreshnessUnverified || s.Observation.Freshness == session.FreshnessMissing {
			continue
		}
		rank := 0
		switch s.State {
		case session.StateWaiting:
			rank = 0
		case session.StateIdle:
			rank = 1
		case session.StateWorking:
			rank = 2
		default:
			continue
		}
		seen[conv] = true
		mark, ok := b.marks[conv]
		if !ok || mark.state != s.State {
			mark = recapMark{state: s.State, since: now}
			b.marks[conv] = mark
		}
		if b.running {
			continue
		}
		if t, ok := b.failed[conv]; ok && now.Sub(t) < recapRetryAfter {
			continue
		}
		sig := recordSignature(b.record(s))
		if sig == "" {
			continue
		}
		row, has := b.rows[conv]
		if has && row.Signature == sig {
			continue
		}
		stood := now.Sub(mark.since)
		if s.State == session.StateWorking {
			if !has && stood < recapFirstWhileWorking {
				continue
			}
			if has && now.Sub(time.Unix(row.At, 0)) < recapWhileWorking {
				continue
			}
		} else if stood < recapAfterStop {
			continue
		}
		due = append(due, recapCandidate{s: s, signature: sig, rank: rank, since: mark.since})
	}
	for conv := range b.marks {
		if !seen[conv] && inv.Complete {
			delete(b.marks, conv)
		}
	}
	if b.running || len(due) == 0 {
		return
	}
	sort.SliceStable(due, func(i, j int) bool {
		if due[i].rank != due[j].rank {
			return due[i].rank < due[j].rank
		}
		return due[i].since.Before(due[j].since)
	})
	b.start(due[0])
}

// start runs one fork in its own goroutine. It is called with b.mu held.
func (b *recapBook) start(c recapCandidate) {
	b.running = true
	conv := c.s.ConversationID
	b.tried[conv] = b.now()
	target := planner.RecapTarget{Assistant: string(c.s.Assistant), Conversation: conv, Dir: c.s.CWD}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), maxSessionRecapDuration)
		started := time.Now()
		text, err := b.recap(ctx, target)
		cancel()
		b.mu.Lock()
		b.running = false
		ok := 0
		why := ""
		if err != nil {
			b.failed[conv] = b.now()
			why = recapFailure(err)
		} else {
			delete(b.failed, conv)
			b.keep(recapRow{Conversation: conv, Assistant: target.Assistant, Text: text,
				At: b.now().Unix(), State: string(c.s.State), Signature: c.signature})
			ok = 1
		}
		done := b.done
		b.mu.Unlock()
		log.Printf("audit session.recap assistant=%s state=%s ms=%d ok=%d why=%s",
			target.Assistant, c.s.State, time.Since(started).Milliseconds(), ok, why)
		if done != nil {
			done(conv, err)
		}
	}()
}

// recapFailure names a failure for the audit line without its text, which
// may quote the conversation.
func recapFailure(err error) string {
	switch {
	case errors.Is(err, planner.ErrOutOfQuota):
		return "out_of_quota"
	case errors.Is(err, planner.ErrNoPlanner):
		return "no_assistant"
	case errors.Is(err, planner.ErrNotARecap):
		return "not_a_recap"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	default:
		return "failed"
	}
}

func (b *recapBook) currentMode() string {
	if b == nil || b.mode == nil {
		return copilot.RecapOff
	}
	return b.mode()
}

// On says whether recaps are turned on; a list asks once, not per row.
func (b *recapBook) On() bool { return b.currentMode() != copilot.RecapOff }

// For is the recap a Session row carries, or nil. Current says whether the
// conversation has grown since; a recap of an earlier conversation in the
// same terminal is never returned, because rows are kept by conversation.
func (b *recapBook) For(s session.Session) *contract.SessionRecap {
	if b == nil || s.ConversationID == "" {
		return nil
	}
	b.mu.Lock()
	b.load()
	row, ok := b.rows[s.ConversationID]
	b.mu.Unlock()
	if !ok || row.Assistant != string(s.Assistant) {
		return nil
	}
	sig := recordSignature(b.record(s))
	return &contract.SessionRecap{Text: row.Text, At: row.At, State: contract.SessionState(row.State),
		Current: sig != "" && sig == row.Signature}
}

// ErrRecapBusy is an on-demand recap refused because maxSessionRecapOnDemand
// forks are already running.
var ErrRecapBusy = errors.New("session recaps are busy")

// Now answers for one Session at once: its recap when the conversation has not
// grown since, otherwise a fork run now and kept. It is what the voice copilot
// calls in the on-demand mode, and in the background mode for a Session whose
// recap is stale. The fork is not cut short when the caller stops waiting:
// it goes on and its answer is kept for the next question.
func (b *recapBook) Now(ctx context.Context, s session.Session) (*contract.SessionRecap, bool, error) {
	if b == nil || b.recap == nil || !b.On() {
		return nil, false, ErrNoRecaps
	}
	if s.ConversationID == "" || !planner.RecapCan(string(s.Assistant)) {
		return nil, false, planner.ErrNoPlanner
	}
	if have := b.For(s); have != nil && have.Current {
		return have, false, nil
	}
	sig := recordSignature(b.record(s))
	select {
	case b.onDemand <- struct{}{}:
	default:
		return nil, false, ErrRecapBusy
	}
	type result struct {
		text string
		err  error
	}
	out := make(chan result, 1)
	target := planner.RecapTarget{Assistant: string(s.Assistant), Conversation: s.ConversationID, Dir: s.CWD}
	go func() {
		defer func() { <-b.onDemand }()
		fork, cancel := context.WithTimeout(context.Background(), maxSessionRecapDuration)
		started := time.Now()
		text, err := b.recap(fork, target)
		cancel()
		ok, why := 0, ""
		b.mu.Lock()
		b.load()
		if err == nil {
			b.keep(recapRow{Conversation: s.ConversationID, Assistant: target.Assistant, Text: text,
				At: b.now().Unix(), State: string(s.State), Signature: sig})
			ok = 1
		} else {
			why = recapFailure(err)
		}
		b.mu.Unlock()
		log.Printf("audit session.recap assistant=%s state=%s ms=%d ok=%d why=%s on_demand=1",
			target.Assistant, s.State, time.Since(started).Milliseconds(), ok, why)
		out <- result{text, err}
	}()
	wait, cancel := context.WithTimeout(ctx, maxSessionRecapWait)
	defer cancel()
	select {
	case r := <-out:
		if r.err != nil {
			return nil, false, r.err
		}
		return &contract.SessionRecap{Text: r.text, At: b.now().Unix(), State: contract.SessionState(s.State),
			Current: sig != "" && sig == recordSignature(b.record(s))}, true, nil
	case <-wait.Done():
		return nil, false, context.DeadlineExceeded
	}
}

// ErrNoRecaps is a recap asked for while Session recaps are off.
var ErrNoRecaps = errors.New("session recaps are off")

// Rows is how many recaps are kept, for the capacity reading.
func (b *recapBook) Rows() int {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.load()
	return len(b.rows)
}

// Running is how many on-demand forks are running now.
func (b *recapBook) Running() int {
	if b == nil {
		return 0
	}
	return len(b.onDemand)
}
