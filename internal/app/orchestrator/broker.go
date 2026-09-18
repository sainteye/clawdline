package orchestrator

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/git"
	"github.com/sainteye/clawdline-go/internal/adapters/projects"
	"github.com/sainteye/clawdline-go/internal/adapters/store"
	"github.com/sainteye/clawdline-go/internal/adapters/taskdir"
	"github.com/sainteye/clawdline-go/internal/app/ports"
	"github.com/sainteye/clawdline-go/internal/domain/session"
)

// Broker is the loop: dispatch, spawn, brief, watch, collect, notify,
// acknowledge, land.
//
// Its dependencies are all ports or adapters that this package does not own,
// because the one thing a broker must not become is the place every other
// concern is reachable from. The Swift app's Orchestrator is 10,745 lines for
// exactly that reason.
type Broker struct {
	Store *store.Store
	Tasks taskdir.Root
	Git   *git.Git

	// Live is a current reading of the machine's sessions. The broker asks for
	// it rather than holding one, because every question it uses it for —
	// which terminal is this conversation, is the root still there — is about
	// now and is wrong if it is a second old.
	Live func(ctx context.Context) []session.Session
	// Reading is the same reading with its completeness attached, for the
	// beat: one per pass, shared by every task the pass looks at, because a
	// verdict about a child's executor may be drawn only from a reading that
	// says it saw everything (observe.go). Nil falls back to Live, read as
	// complete only when it is not empty.
	Reading func(ctx context.Context) session.Inventory
	// Fault is a failure-injection seam, called at the top of every pass with
	// the pass's number. Nil in production; set only from
	// CLAWDLINE_NEXT_BEAT_FAULT, so the alarms this broker raises about its own
	// beat can be shown to fire on the real daemon and not only in a test.
	Fault func(pass int64)
	// Type types one line into a terminal and submits it.
	Type func(ctx context.Context, terminalID, text string) error
	// Choosing reports whether a terminal is showing a menu. Typing into one
	// answers the menu instead of delivering the message, so both the message
	// route and the notice pump ask first.
	Choosing func(ctx context.Context, terminalID string) bool
	// Screen is what a terminal currently shows. The briefing path needs it
	// because a new child's first screen may be a dialog, and a caret is not a
	// composer — see composer.go for what that cost once.
	Screen func(ctx context.Context, terminalID string) (string, bool)
	// Launcher opens the child's tab.
	Launcher ports.Launcher
	// Terminal is the machine's `terminal` setting.
	Terminal func() projects.TerminalChoice
	// Policy is this Mac's dispatch policy, pasted into every child briefing.
	Policy func() string
	// Port is this daemon's own port, for the curl recipes in a briefing.
	Port int
	// Dir is the daemon's state root; isolated checkouts live under it.
	Dir string
	// Language picks the one line the child says out loud.
	Language string
	// MaxChildren is the per-root ceiling; the machine's is four times it.
	MaxChildren int
	// Notify sends one push, and reports how many devices took it. A daemon
	// with no push implementation leaves this nil, and /notify then answers
	// `not_subscribed` — which is the truth, not a stub.
	Notify func(ctx context.Context, title, body, tag string) (sent, failed int, err error)
	// Clock is injectable so a test does not wait out a real deadline.
	Clock func() time.Time
	// NewID makes a task id when a caller does not.
	NewID func() string

	// dispatches is the rate-limit window: a machine-wide count of accepted
	// dispatches, in memory only. It is deliberately not durable — the limit
	// exists to stop a runaway loop in this process, and a restart is already
	// the strongest evidence that the loop stopped.
	mu         sync.Mutex
	dispatches []time.Time
	// writeMu serialises every change to a stored record. See mutate.
	writeMu sync.Mutex
	// spawning holds the plaintext secret between admitting a task and typing
	// it into the child. It is in memory only and is dropped the moment the
	// briefing is typed: the hash on disk is what authenticates the child
	// afterwards, and a plaintext secret at rest is a credential nobody meant
	// to keep.
	secrets map[string]string

	// beat is the loop's account of itself (observe.go). In memory only: it
	// describes this process, and a restart is a new beat.
	beat beatState
	// observed is what the beat last saw of each child's executor, and
	// deferred is when a notice whose root was showing a menu may be tried
	// again. Both are observations — recomputed by the next reading, useless
	// after a restart — and so neither is ever written (observe.go).
	observed observations
	// progress carries each accepted note to whoever is streaming.
	progress progressBus
}

const (
	// rateWindow and rateLimit are the Swift app's: twenty accepted dispatches
	// in ten minutes, machine-wide.
	rateWindow = 10 * time.Minute
	// readyLimit is how long a child has to reach a prompt. Past it, with no
	// authenticated progress note, the tab is recorded as never having started.
	readyLimit = 240 * time.Second
	// progressKept is how many notes a task carries.
	progressKept = 5
	// notifyTaskLimit and notifyHourLimit are the two caps on agent pushes.
	notifyTaskLimit  = 5
	notifyHourLimit  = 30
	notifyGrace      = 60 * time.Second
	summaryLimit     = 2000
	progressLimit    = 300
	notifyTitleLimit = 80
	notifyBodyLimit  = 500
	// sessionSummaryLimit is the root delivery receipt's sentence.
	sessionSummaryLimit = 500
)

func (b *Broker) now() time.Time {
	if b.Clock != nil {
		return b.Clock()
	}
	return time.Now()
}

func (b *Broker) maxChildren() int {
	if b.MaxChildren > 0 {
		return b.MaxChildren
	}
	return 5
}

func (b *Broker) machineChildren() int { return b.maxChildren() * 4 }

// NewSecret mints a task secret. The broker mints one only when a caller did
// not bring its own; the protocol lets the caller choose, so that the plaintext
// need never travel back over HTTP.
func NewSecret() string {
	buf := make([]byte, 32)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

// HashSecret is how a secret is stored and compared: lowercase hex SHA-256.
func HashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// SecretMatches is a constant-time comparison of the two hex digests. Comparing
// the secrets themselves would leak their length through the compare, and
// comparing digests with `==` would leak the digest through timing.
func SecretMatches(storedHash, presented string) bool {
	if storedHash == "" || presented == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(storedHash), []byte(HashSecret(presented))) == 1
}

// Records reads every task this broker holds, newest first. It is exported for
// the console's task list, which shows this daemon's own children beside the
// Swift app's.
func (b *Broker) Records(ctx context.Context) ([]Record, error) { return b.records(ctx) }

// records reads every task this broker holds, each joined to its completion
// envelope.
func (b *Broker) records(ctx context.Context) ([]Record, error) {
	rows, err := b.Store.BrokerTasks(ctx, "")
	if err != nil {
		return nil, storeUnavailable(err)
	}
	notices, err := b.Store.BrokerNotices(ctx)
	if err != nil {
		return nil, storeUnavailable(err)
	}
	out := make([]Record, 0, len(rows))
	for _, row := range rows {
		r, err := Decode(row.Record)
		if err != nil {
			// A record this daemon wrote and cannot read is a fault worth
			// naming, but it must not make the whole list unreadable: the rows
			// beside it are somebody's running work.
			continue
		}
		if n, ok := notices[r.ID]; ok {
			r.Notice = noticeOf(n)
		}
		out = append(out, r)
	}
	return out, nil
}

// Record reads one task, or says this store has never heard of it.
func (b *Broker) Record(ctx context.Context, id string) (Record, string, error) {
	row, err := b.Store.BrokerTask(ctx, id)
	if errors.Is(err, store.ErrNoTask) {
		return Record{}, "", notFoundTask()
	}
	if err != nil {
		return Record{}, "", storeUnavailable(err)
	}
	r, err := Decode(row.Record)
	if err != nil {
		return Record{}, "", err
	}
	n, err := b.Store.BrokerNotice(ctx, id)
	switch {
	case err == nil:
		r.Notice = noticeOf(n)
	case !errors.Is(err, store.ErrNoNotice):
		return Record{}, "", storeUnavailable(err)
	}
	return r, row.SecretHash, nil
}

// storeUnavailable is a store that did not answer, said as the Swift app says
// it. It is never an empty answer: "I could not read the tasks" and "there
// are no tasks" are different sentences, and a phone told the second when the
// first was true tells its person their work is gone.
func storeUnavailable(err error) Refusal {
	return refuseWith(http.StatusServiceUnavailable, "orchestrator_store_unavailable",
		"The broker's store could not be read; nothing was changed.",
		map[string]any{"cause": err.Error()})
}

// save writes a record back, with the event that explains the change.
func (b *Broker) save(ctx context.Context, r Record, secretHash string, kind string) error {
	return b.saveWith(ctx, r, secretHash, kind, nil)
}

// saveWith is save that also opens the task's completion envelope, in the
// same transaction, when notice is not nil.
func (b *Broker) saveWith(ctx context.Context, r Record, secretHash string, kind string, notice *store.BrokerNotice) error {
	payload, _ := json.Marshal(map[string]any{"state": r.State, "task": r.ID})
	return b.Store.SaveBrokerTaskWithNotice(ctx, store.BrokerRow{
		ID:         r.ID,
		Project:    r.ProjectDir,
		Repository: r.Repository,
		Assistant:  r.Assistant,
		State:      string(r.State),
		CreatedAt:  r.CreatedAt,
		UpdatedAt:  b.now(),
		SecretHash: secretHash,
		Record:     r.Encode(),
	}, notice, []store.Event{{Kind: kind, Subject: r.ID, Payload: payload}})
}

// errNoticeOutsideLedger is a change function that edited an existing notice.
// Notices move only through the ledger's compare-and-set (notice.go); a record
// rewrite that carried one along would be the lost update the ledger exists
// to make impossible, so it is refused rather than quietly dropped.
var errNoticeOutsideLedger = errors.New("a completion notice changes only through its ledger")

// errUnchanged tells mutate that the change it was asked for is already true,
// or no longer applies, and nothing should be written.
var errUnchanged = errors.New("unchanged")

// mutate is the one way a stored record changes: read it, change it, write it,
// with nothing else able to write in between.
//
// It exists because the first version of this broker did not have it, and had
// four ways to lose an update. The beat read every record, then spent seconds
// typing a notice into a terminal, then saved its copy — over an ACK that had
// arrived in the meantime, putting an acknowledged notice back on the resend
// ladder. A `/complete` and the beat's own collection of result.json could both
// settle one task and mint two notice ids. A dispatch still typing its briefing
// could save `spawning` over the `briefed` that a progress note had just
// proved. Every one of those is a read of an earlier moment written back as
// though it were now.
//
// So a change is a function of the record **as it is when the change is
// made**, never of a copy taken before something slow. Anything slow — typing,
// asking git — happens outside, and the function here decides whether what it
// learned still applies to the record it finds.
func (b *Broker) mutate(ctx context.Context, id, kind string, change func(r *Record) error) (Record, error) {
	b.writeMu.Lock()
	defer b.writeMu.Unlock()
	r, hash, err := b.Record(ctx, id)
	if err != nil {
		return Record{}, err
	}
	var before *Notice
	if r.Notice != nil {
		copied := *r.Notice
		before = &copied
	}
	if err := change(&r); err != nil {
		if errors.Is(err, errUnchanged) {
			return r, nil
		}
		return r, err
	}
	// The one notice a record rewrite may carry is a new one: the envelope a
	// settlement opens, stored in the same transaction as the state it
	// announces. Anything else about a notice is the ledger's to change.
	var opened *store.BrokerNotice
	switch {
	case before == nil && r.Notice != nil:
		row := noticeRow(r.ID, *r.Notice)
		opened = &row
	case before != nil && (r.Notice == nil || !sameNotice(*before, *r.Notice)):
		return r, errNoticeOutsideLedger
	}
	if err := b.saveWith(ctx, r, hash, kind, opened); err != nil {
		return r, err
	}
	return r, nil
}

// Authenticate resolves a task and proves the presented secret is its own.
func (b *Broker) Authenticate(ctx context.Context, id, secret string) (Record, string, error) {
	r, hash, err := b.Record(ctx, id)
	if err != nil {
		return Record{}, "", err
	}
	if !SecretMatches(hash, secret) {
		return Record{}, "", badSecret()
	}
	return r, hash, nil
}

// rememberSecret holds a plaintext secret until the child has been briefed.
func (b *Broker) rememberSecret(id, secret string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.secrets == nil {
		b.secrets = map[string]string{}
	}
	b.secrets[id] = secret
}

func (b *Broker) forgetSecret(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.secrets, id)
}

// admitDispatch takes one ticket from the rate window, or refuses.
func (b *Broker) admitDispatch() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	kept := b.dispatches[:0]
	for _, at := range b.dispatches {
		if now.Sub(at) < rateWindow {
			kept = append(kept, at)
		}
	}
	b.dispatches = kept
	limit := b.machineChildren()
	if limit < 10 {
		limit = 10
	}
	if len(b.dispatches) >= limit {
		return refuse(http.StatusTooManyRequests, "rate_limited",
			"Too many dispatches; wait a few minutes.")
	}
	b.dispatches = append(b.dispatches, now)
	return nil
}

// refundDispatch gives the ticket back when the dispatch did not happen. A
// refusal that consumed a ticket punishes a caller for asking correctly.
func (b *Broker) refundDispatch() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if n := len(b.dispatches); n > 0 {
		b.dispatches = b.dispatches[:n-1]
	}
}

// liveTasks is every task that has not reached a terminal state.
func (b *Broker) liveTasks(ctx context.Context) ([]Record, error) {
	all, err := b.records(ctx)
	if err != nil {
		return nil, err
	}
	out := []Record{}
	for _, r := range all {
		if !r.State.Terminal() {
			out = append(out, r)
		}
	}
	return out, nil
}

// terminalFor resolves a conversation id to the one live terminal that proves
// it, and refuses when the answer is not exactly one.
//
// Not-one has two different codes because they are two different problems: no
// match means the session this task belongs to is not on this machine, and more
// than one means the machine cannot tell which of two processes is meant, and
// choosing either would group the work under a session that is not its owner.
func (b *Broker) terminalFor(ctx context.Context, conversationID, assistant string) (session.Session, error) {
	if b.Live == nil {
		return session.Session{}, refuse(http.StatusConflict, "registry_stale",
			"The live session registry is incomplete; retry after the next scan.")
	}
	matches := []session.Session{}
	for _, s := range b.Live(ctx) {
		if !s.IsAssistant() || s.ConversationID != conversationID {
			continue
		}
		if assistant != "" && string(s.Assistant) != assistant {
			continue
		}
		matches = append(matches, s)
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return session.Session{}, refuse(http.StatusNotFound, "conversation_not_found",
			"No live session is bound to that exact conversation id.")
	default:
		return session.Session{}, refuse(http.StatusConflict, "conversation_ambiguous",
			"More than one live session claims that conversation id; none was chosen.")
	}
}

// sessionByTerminal resolves a terminal id against a current reading.
func (b *Broker) sessionByTerminal(ctx context.Context, id string) (session.Session, bool) {
	if b.Live == nil {
		return session.Session{}, false
	}
	for _, s := range b.Live(ctx) {
		if s.ID == id {
			return s, true
		}
	}
	return session.Session{}, false
}

// WorktreeRoot is where isolated checkouts go.
//
// Under this daemon's own state directory, not the Swift app's
// `~/Library/Application Support/Clawdline/worktrees`: two brokers sharing one
// worktrees root would each prune the other's checkouts, and `git worktree
// prune` does not ask who made something.
func (b *Broker) WorktreeRoot() string { return filepath.Join(b.Dir, "worktrees") }

// worktreePath is `<root>/<slug>/<task id>`, with the Swift app's slug so a
// person reading two machines sees the same name: the repository's basename,
// lowercased and punctuation-flattened, then eight hex of its canonical path.
func (b *Broker) worktreePath(repo, taskID string) string {
	return filepath.Join(b.WorktreeRoot(), RepoSlug(repo), taskID)
}

// RepoSlug is the directory name a repository's checkouts live under.
func RepoSlug(repo string) string {
	canonical := repo
	if resolved, err := filepath.EvalSymlinks(repo); err == nil {
		canonical = resolved
	}
	base := strings.ToLower(filepath.Base(canonical))
	flat := make([]rune, 0, len(base))
	for _, r := range base {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			flat = append(flat, r)
		} else {
			flat = append(flat, '-')
		}
	}
	name := string(flat)
	if len(name) > 32 {
		name = name[:32]
	}
	if name == "" {
		name = "repo"
	}
	sum := sha256.Sum256([]byte(canonical))
	return name + "-" + hex.EncodeToString(sum[:])[:8]
}

// BranchName is the delivery branch an isolated task is given.
func BranchName(taskID string) string { return "clawdline/task/" + taskID }

func dirExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// sortedUnique is used wherever a list goes into a digest or a message.
func sortedUnique(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, v := range in {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}
