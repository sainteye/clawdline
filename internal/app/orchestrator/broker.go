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
	"github.com/sainteye/clawdline-go/internal/app/lane"
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
	// Lanes is the machine's terminal lanes (lane, D22) — the same set the
	// daemon's Type goes through. A dispatch takes its admission here before
	// it writes anything, so a machine that is full answers 429 rather than
	// recording a task it cannot open a tab for. Nil admits everything, which
	// only a test wants.
	Lanes *lane.Lanes
	// Terminal is the machine's `terminal` setting.
	Terminal func() projects.TerminalChoice
	// Policy is this Mac's dispatch policy — the base file and the person's
	// local one, read at every dispatch — pasted into every child briefing
	// through ComposePolicy (policy.go).
	Policy func() (base, local string)
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
	// decoded is every record this broker has decoded, by id and version, so
	// a list that is asked for every two seconds decodes only the rows that
	// changed since it last looked (G33). It is replaced wholesale by each
	// whole-table read, so it never holds a row the table no longer has.
	decoded decodedRecords
	// EffectFault is a failure-injection seam, called with the name of each
	// point an effect passes — "committed" is after the intent is durable and
	// before the effect is attempted. Nil in production; set only from
	// CLAWDLINE_NEXT_OUTBOX_FAULT, so "the process died after the commit and
	// before the effect" can be made to happen on the real daemon.
	EffectFault func(point string, e store.Effect)
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

// Records reads every task this broker holds, newest first, and every stored
// row it could not decode. It is exported for the console's task list, which
// shows this daemon's own children beside the Swift app's — and which must
// show a row it cannot read as exactly that, not leave it out.
func (b *Broker) Records(ctx context.Context) ([]Record, []Unreadable, error) {
	return b.ledger(ctx)
}

// Unreadable is a stored row this daemon wrote and cannot decode.
//
// It is not a state a task moves through. It is what the row is called while
// nobody can say what it holds, and it is listed rather than skipped because
// "I could not read that task" and "there is no such task" are different
// sentences (docs/design-decisions.md D05 ②): the first time this broker met a
// row it could not read, it `continue`d past it, and the next dispatch with the
// same id upserted a new task over it. The columns lifted beside the record
// are still readable, so they are carried — they say where and whose the row
// was, never what it claims or how far it got.
type Unreadable struct {
	ID         string
	Project    string
	Repository string
	Assistant  string
	// StoredState is the state column as last written. It is a hint, not the
	// task's state: the record it summarised is the part that cannot be read.
	StoredState string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Cause       string
}

// StateUnreadable is how an Unreadable row is spelled wherever a task state
// is expected (the task list, the inventory).
const StateUnreadable State = "unreadable"

// ledger reads every task this broker holds, each joined to its completion
// envelope, and names every row it could not decode.
//
// It reads every row's head — no record, nothing decoded — and fetches and
// decodes only the rows whose version it has not decoded before (G33). The
// long prose is never read here: a list shows titles, not instructions.
func (b *Broker) ledger(ctx context.Context) ([]Record, []Unreadable, error) {
	heads, err := b.Store.BrokerTaskHeads(ctx)
	if err != nil {
		return nil, nil, storeError(err)
	}
	notices, err := b.Store.BrokerNotices(ctx)
	if err != nil {
		return nil, nil, storeError(err)
	}
	decoded, stale := b.decoded.lookup(heads)
	if len(stale) > 0 {
		fetched, err := b.Store.BrokerTaskRecords(ctx, stale)
		if err != nil {
			return nil, nil, storeError(err)
		}
		for id, row := range fetched {
			r, err := Decode(row.Record)
			decoded[id] = decodedRecord{version: row.Version, record: r, err: err, row: row}
		}
	}
	b.decoded.replace(decoded)
	out := make([]Record, 0, len(heads))
	bad := []Unreadable{}
	for _, h := range heads {
		d, ok := decoded[h.ID]
		if !ok {
			// Gone between the two reads: it is not in this answer, and the
			// next read will say so either way.
			continue
		}
		if d.err != nil {
			bad = append(bad, unreadableRow(d.row, d.err))
			continue
		}
		r := d.record
		if n, ok := notices[r.ID]; ok {
			r.Notice = noticeOf(n)
		}
		out = append(out, r)
	}
	return out, bad, nil
}

func unreadableRow(row store.BrokerRow, err error) Unreadable {
	return Unreadable{
		ID: row.ID, Project: row.Project, Repository: row.Repository, Assistant: row.Assistant,
		StoredState: row.State, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, Cause: err.Error(),
	}
}

// records is ledger for the callers that act on a record's contents — the
// beat, respawn, the message route — and that have nothing they could do
// with a row whose contents are unknown. The rows it leaves out are still
// listed, counted and refused by id elsewhere; see Unreadable.
func (b *Broker) records(ctx context.Context) ([]Record, error) {
	out, _, err := b.ledger(ctx)
	return out, err
}

// Record reads one task, or says this store has never heard of it, or says
// the row is there and cannot be read — three answers, and the third is a 409
// rather than a 404 or a 500: the id is taken, and nothing may be done to it
// until somebody can say what it holds.
func (b *Broker) Record(ctx context.Context, id string) (Record, string, error) {
	row, err := b.Store.BrokerTask(ctx, id)
	if errors.Is(err, store.ErrNoTask) {
		return Record{}, "", notFoundTask()
	}
	if err != nil {
		return Record{}, "", storeError(err)
	}
	r, err := Decode(row.Record)
	if err != nil {
		return Record{}, "", taskUnreadable(unreadableRow(row, err))
	}
	r.applyTexts(row.Texts)
	n, err := b.Store.BrokerNotice(ctx, id)
	switch {
	case err == nil:
		r.Notice = noticeOf(n)
	case !errors.Is(err, store.ErrNoNotice):
		return Record{}, "", storeError(err)
	}
	return r, row.SecretHash, nil
}

// taskUnreadable is the refusal every route gives for an Unreadable row.
func taskUnreadable(u Unreadable) Refusal {
	return refuseWith(http.StatusConflict, "task_unreadable",
		"A task with this id is stored and cannot be read. It is not absent, so nothing was changed "+
			"and nothing may reuse the id; the row stays as it is for a person to inspect.",
		map[string]any{"task": u.ID, "stored_state": u.StoredState, "cause": u.Cause})
}

// IsUnreadable reports whether err is the refusal for an Unreadable row.
func IsUnreadable(err error) bool {
	ref, ok := err.(Refusal)
	return ok && ref.Code == "task_unreadable"
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

// storeError is a store failure said as the refusal that fits it. A store
// that was busy — another writer held it past busy_timeout — is contended,
// not broken, and a caller told 503 `orchestrator_store_busy` with a
// Retry-After knows to try again; a write that lost a compare-and-set is a
// 409 `stale_write`, because what it decided was about a row that has since
// changed (D08, G13, G16).
func storeError(err error) error {
	var ref Refusal
	switch {
	case errors.As(err, &ref):
		return ref
	case errors.Is(err, store.ErrNoTask):
		return notFoundTask()
	case errors.Is(err, store.ErrBusy):
		return refuseWith(http.StatusServiceUnavailable, "orchestrator_store_busy",
			"Another writer held the broker's store longer than this one waits; nothing was changed. Retry.",
			map[string]any{"retry_after": 1, "cause": err.Error()})
	case errors.Is(err, store.ErrConflict):
		return refuseWith(http.StatusConflict, "stale_write",
			"The task changed while this write was being decided; nothing was written. Read it again and retry.",
			map[string]any{"cause": err.Error()})
	}
	return storeUnavailable(err)
}

// save writes a record back, with the event that explains the change.
func (b *Broker) save(ctx context.Context, r Record, secretHash string, kind string) error {
	return b.saveWith(ctx, r, secretHash, kind, nil)
}

// create writes a task's first record. Unlike save it never replaces a row:
// an id that is already stored — readable or not — is refused, because the
// row it would have replaced is somebody's work (D05 ②).
//
// The effects are what the task's creation owes the world — its checkout —
// recorded as intent in the same transaction and run after it (outbox.go in
// the store, effects.go here). Their ids come back, owned by this broker.
func (b *Broker) create(ctx context.Context, r Record, secretHash string, effects ...store.Effect) ([]int64, error) {
	payload, _ := json.Marshal(map[string]any{"state": r.State, "task": r.ID})
	record, texts := r.stored()
	ids, err := b.Store.CreateBrokerTask(ctx, store.BrokerRow{
		ID:         r.ID,
		Project:    r.ProjectDir,
		Repository: r.Repository,
		Assistant:  r.Assistant,
		State:      string(r.State),
		CreatedAt:  r.CreatedAt,
		UpdatedAt:  b.now(),
		SecretHash: secretHash,
		Record:     record,
		Texts:      texts,
	}, []store.Event{{Kind: "task.queued", Subject: r.ID, Payload: payload}}, effects...)
	if errors.Is(err, store.ErrTaskExists) {
		return nil, refuseWith(http.StatusConflict, "task_exists",
			"A task with this id was stored while this dispatch was being admitted; nothing was written over it.",
			map[string]any{"task": r.ID})
	}
	if err != nil {
		return nil, storeError(err)
	}
	return ids, nil
}

// saveWith is save that also opens the task's completion envelope, in the
// same transaction, when notice is not nil.
//
// It is a compare-and-set against version: a row that has moved since the
// caller read it is ErrConflict, and nothing is written.
func (b *Broker) saveWith(ctx context.Context, r Record, secretHash string, kind string, notice *store.BrokerNotice) error {
	return b.saveAt(ctx, r, secretHash, kind, notice, 0)
}

func (b *Broker) saveAt(ctx context.Context, r Record, secretHash string, kind string, notice *store.BrokerNotice, version int64) error {
	payload, _ := json.Marshal(map[string]any{"state": r.State, "task": r.ID})
	record, texts := r.stored()
	return b.Store.SaveBrokerTaskWithNotice(ctx, store.BrokerRow{
		ID:         r.ID,
		Project:    r.ProjectDir,
		Repository: r.Repository,
		Assistant:  r.Assistant,
		State:      string(r.State),
		CreatedAt:  r.CreatedAt,
		UpdatedAt:  b.now(),
		SecretHash: secretHash,
		Record:     record,
		Texts:      texts,
		Version:    version,
	}, notice, []store.Event{{Kind: kind, Subject: r.ID, Payload: payload}})
}

// row is a record as the store holds it, for a write.
func (b *Broker) storedRow(r Record) store.BrokerRow {
	record, texts := r.stored()
	return store.BrokerRow{
		ID:         r.ID,
		Project:    r.ProjectDir,
		Repository: r.Repository,
		Assistant:  r.Assistant,
		State:      string(r.State),
		CreatedAt:  r.CreatedAt,
		UpdatedAt:  b.now(),
		Record:     record,
		Texts:      texts,
	}
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
//
// "Nothing else able to write in between" used to be a mutex in this process.
// It is now the store's write transaction (D08): the read, this function and
// the write are one BEGIN IMMEDIATE, so a CLI or a second daemon on the same
// file is held off too, and one that got past would meet a compare-and-set
// and be told so rather than be written over. The function runs holding the
// write right, and the effect guard (effects.go) refuses a terminal, a push or
// a checkout asked for from inside it.
func (b *Broker) mutate(ctx context.Context, id, kind string, change func(r *Record) error) (Record, error) {
	r, _, err := b.mutateTx(ctx, id, kind, func(_ *store.Tx, r *Record) ([]store.Effect, error) {
		return nil, change(r)
	})
	return r, err
}

// mutateTx is mutate for a change that also writes through the transaction —
// a note that proves a briefing — or owes the world an effect, recorded here
// as intent and run after the commit. The effect ids come back, this
// broker's to run.
func (b *Broker) mutateTx(ctx context.Context, id, kind string, change func(tx *store.Tx, r *Record) ([]store.Effect, error)) (Record, []int64, error) {
	return b.mutateEvent(ctx, id, kind, nil, change)
}

// mutateEvent is mutateTx whose event says more than the state it left the
// task in: extra is merged into the event's payload, for a change whose
// meaning is in the values it replaced — a landing corrected from one commit
// to another is that pair, and the pair is what the event keeps (D18).
func (b *Broker) mutateEvent(ctx context.Context, id, kind string, extra map[string]any, change func(tx *store.Tx, r *Record) ([]store.Effect, error)) (Record, []int64, error) {
	var out Record
	// decided is the error this broker's own code answered from inside the
	// transaction — a refusal, errAlreadyTerminal — which is returned as it
	// is. Anything else is the store's, and is said as a store refusal.
	var decided error
	_, ids, err := b.Store.UpdateBrokerTask(ctx, id, func(tx *store.Tx, row store.BrokerRow) (*store.BrokerWrite, error) {
		r, err := Decode(row.Record)
		if err != nil {
			decided = taskUnreadable(unreadableRow(row, err))
			return nil, decided
		}
		r.applyTexts(row.Texts)
		n, err := tx.Notice(id)
		switch {
		case err == nil:
			r.Notice = noticeOf(n)
		case !errors.Is(err, store.ErrNoNotice):
			return nil, err
		}
		var before *Notice
		if r.Notice != nil {
			copied := *r.Notice
			before = &copied
		}
		out = r
		effects, err := change(tx, &r)
		if err != nil {
			if errors.Is(err, errUnchanged) {
				return nil, nil
			}
			decided = err
			return nil, err
		}
		// The one notice a record rewrite may carry is a new one: the envelope a
		// settlement opens, stored in the same transaction as the state it
		// announces. Anything else about a notice is the ledger's to change.
		var opened *store.BrokerNotice
		switch {
		case before == nil && r.Notice != nil:
			nr := noticeRow(r.ID, *r.Notice)
			opened = &nr
		case before != nil && (r.Notice == nil || !sameNotice(*before, *r.Notice)):
			decided = errNoticeOutsideLedger
			return nil, decided
		}
		body := map[string]any{"state": r.State, "task": r.ID}
		for k, v := range extra {
			body[k] = v
		}
		payload, _ := json.Marshal(body)
		out = r
		return &store.BrokerWrite{
			Row:     b.storedRow(r),
			Notice:  opened,
			Events:  []store.Event{{Kind: kind, Subject: r.ID, Payload: payload}},
			Effects: effects,
		}, nil
	})
	if err != nil {
		if decided != nil && errors.Is(err, decided) {
			return out, nil, err
		}
		return out, nil, storeError(err)
	}
	return out, ids, nil
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
//
// It asks the store for the rows whose state column is not terminal, and
// nothing else: its cost is the number of live tasks, never the length of
// the history (G33). A live row that cannot be decoded is left out here, as
// it always was, and counted by the caller that asks for it (liveLedger).
func (b *Broker) liveTasks(ctx context.Context) ([]Record, error) {
	out, _, err := b.liveLedger(ctx)
	return out, err
}

// liveStates is every state a task can still move out of.
var liveStates = []string{string(StateQueued), string(StateSpawning), string(StateBriefed)}

// liveLedger is liveTasks with the live rows it could not decode.
func (b *Broker) liveLedger(ctx context.Context) ([]Record, []Unreadable, error) {
	rows, err := b.Store.BrokerTasksInState(ctx, liveStates)
	if err != nil {
		return nil, nil, storeError(err)
	}
	out := []Record{}
	bad := []Unreadable{}
	for _, row := range rows {
		r, err := Decode(row.Record)
		if err != nil {
			bad = append(bad, unreadableRow(row, err))
			continue
		}
		if !r.State.Terminal() {
			out = append(out, r)
		}
	}
	return out, bad, nil
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
