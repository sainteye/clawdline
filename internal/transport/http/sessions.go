package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/limits"
	"github.com/sainteye/clawdline-go/internal/adapters/store"
	"github.com/sainteye/clawdline-go/internal/adapters/swiftstore"
	"github.com/sainteye/clawdline-go/internal/contract"
	"github.com/sainteye/clawdline-go/internal/domain/coordinator"
	"github.com/sainteye/clawdline-go/internal/domain/icon"
	"github.com/sainteye/clawdline-go/internal/domain/session"
	"github.com/sainteye/clawdline-go/internal/domain/task"
)

// ownsSessions reports whether this daemon answers /v1/sessions itself.
//
// With nobody behind it — the ordinary case since the Swift app was stopped on
// 2026-09-19 — the answer is yes and there is nothing to decide: handing the
// route to an upstream that does not exist is how `/v1/sessions` came to answer
// `502 upstream_unreachable` on a machine where the session list is the whole
// product.
//
// With an upstream configured, the switch means what it always meant. Taking a
// route over is the one change that can break the console for a person who is
// working, so while there is something behind this daemon it stays a switch
// that can be turned back within a second rather than a property of the build.
func (s *Server) ownsSessions() bool {
	if !s.proxying() {
		return true
	}
	return os.Getenv("CLAWDLINE_NEXT_OWN_SESSIONS") == "1"
}

// epoch identifies this process. A client that reconnects to a restarted daemon
// must be able to tell that the generation counter started over rather than
// went backwards.
var epoch = time.Now().Unix()

var generation atomic.Int64

// sessions answers the route the console actually reads.
func (s *Server) sessions(w http.ResponseWriter, r *http.Request) {
	if !s.ownsSessions() {
		s.forwardUpstream(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.sessionsPayload(ctx))
}

// sessionRowWire is a row as it is sent. It exists for one key the contract
// generator cannot express: `work_person_needed` is optional, and `false` is a
// real answer the console tests for (`=== false`), so it must be a pointer
// rather than the generated bool, whose omitempty would drop the false. The
// outer field shadows the embedded one for encoding/json.
type sessionRowWire struct {
	contract.SessionRow
	WorkPersonNeeded *bool `json:"work_person_needed,omitempty"`
	// Menu shadows the generated field for the same reason: a multi-select's
	// unticked row is `checked: false`, which the generated bool would drop.
	Menu *menuWire `json:"menu,omitempty"`
}

// sessionsSnapshotWire is contract.SessionsSnapshot with those rows.
type sessionsSnapshotWire struct {
	At       int64            `json:"at"`
	Scan     contract.Scan    `json:"scan"`
	Sessions []sessionRowWire `json:"sessions"`
}

// sessionsPayload builds the one snapshot both the route and the event stream
// publish. They are the same payload, so they are the same code: two builders
// would drift, and the client would have no way to tell which one it got.
func (s *Server) sessionsPayload(ctx context.Context) sessionsSnapshotWire {
	return s.sessionsPayloadFrom(ctx, s.reading(ctx))
}

// sessionsPayloadFrom is that snapshot built from a reading the caller already
// took, for the broker's lists that need the reading itself beside the rows
// (orchestrator.go): one reading, so a row and the source completeness read
// with it are of the same moment.
func (s *Server) sessionsPayloadFrom(ctx context.Context, inv session.Inventory) sessionsSnapshotWire {
	// This daemon's own records get a clock of their own, and not what the
	// terminal scan left of the request's (recordsContext).
	records, releaseRecords := recordsContext(ctx)
	defer releaseRecords()

	// One reading of what is owed, for the whole list, against this same
	// reading of the sessions. Asking per row would ask the same question
	// eight times and let two rows disagree about the same moment. An
	// unreadable broker makes the projection incomplete, not wrong in one
	// place: it is carried as missing evidence rather than as zero.
	owed, owedErr := s.owed(records, inv.Sessions)
	// One reading of the Swift app's store, for the same reason. It is history
	// now (cutover B1): this daemon's own records are laid over it below, and
	// with the legacy switch off it is read as holding nothing.
	swift := s.swift.Read()
	// And of this daemon's own machine role (W5): when it holds one, the
	// crown is drawn from it and from nothing else — one role, one source.
	// An unreadable role draws no crown of its own and falls back to the
	// Swift store's, as before this daemon had a role at all.
	role, roleStatus, _ := s.store.Coordinator(records)
	ownRole := roleStatus == store.CoordinatorReady

	// Only assistant sessions are rows. A terminal running an ordinary shell is
	// not a session in this contract — the Swift app carries shells as an
	// attribute of the session that left them running, and publishing them as
	// rows of their own turned eight cards into eighteen, ten of which the
	// console could only describe as unreadable.
	items := make([]session.Session, 0, len(inv.Sessions))
	for _, item := range inv.Sessions {
		if item.IsAssistant() {
			items = append(items, item)
		}
	}
	lives := make([]swiftstore.Live, len(items))
	for i, item := range items {
		lives[i] = liveOf(item)
	}
	matches := identityMatchCounts(items)
	s.lastScreen.Store(&screenReading{at: time.Now(), rows: onScreen(items)})

	// This daemon's own tasks, waits, deliveries, handoffs and Feature Roots,
	// in the Swift store's shape, so the one set of rules reads both
	// (ownrecords.go). A source of ours that could not be read is evidence
	// missing for every row, as an unreadable Swift store is.
	own, ownErr := s.ownOverlay(records, lives)
	swift = swift.With(own)

	// Names first, because a coordination wait names the sessions on either
	// side of it by the label this list gives them.
	labels := make(map[string]string, len(items))
	for i, item := range items {
		labels[item.ID] = rowLabel(item, swift.TitleOf(lives[i], item.CustomTitle, lives))
	}
	labelOf := func(id string) string { return labels[id] }

	// One generation for the whole snapshot, taken before the rows are built,
	// so every row's closeability names the same reading it arrived with.
	gen := generation.Add(1)
	now := time.Now()
	rows := make([]sessionRowWire, 0, len(items))
	for i, item := range items {
		rows = append(rows, s.sessionRow(rowInput{
			item:       item,
			live:       lives[i],
			label:      labels[item.ID],
			labelOf:    labelOf,
			matches:    matches[item.ID],
			swift:      swift,
			owed:       owed,
			owedErr:    owedErr,
			ownErr:     ownErr,
			inv:        inv,
			now:        now,
			generation: gen,
			role:       role,
			ownRole:    ownRole,
		}))
	}

	return sessionsSnapshotWire{
		At:       now.Unix(),
		Sessions: rows,
		Scan: contract.Scan{
			Epoch:      epoch,
			Generation: gen,
			Complete:   inv.Complete,
			Provenance: inv.Provenance,
			// An empty list is only authoritative when the reading was
			// complete. Saying so here is what stops a client from treating a
			// failed scan as "every session went away".
			EmptyAuthoritative: inv.Complete && len(rows) == 0,
			Completed: contract.ScanCompleted{
				Sequence: gen,
				Complete: inv.Complete,
			},
		},
	}
}

// recordsBudget is what this daemon's own records are given to answer in.
//
// Two seconds, and they are SQLite reads of files on this machine: the whole
// overlay measured in single-digit milliseconds, so this is a bound on a
// pathology, not a schedule.
const recordsBudget = 2 * time.Second

// recordsContext is that budget, and it deliberately does not inherit the
// request's deadline.
//
// **A read that was never given any time did not find nothing; it was not
// asked.** The session list's request carried one eight-second deadline for
// everything, the terminal scan spent all of it, and the record reads that
// follow then ran on a context that was already dead. Every one of them failed
// at once, the response carried `obligation_list_unreadable` and
// `own_records_unreadable`, and the rows lost the broker's task titles and
// fell back to `Clawdline task <id>` — while the titles sat in the database,
// readable, a millisecond away.
//
// So the records are taken off that clock. The cancellation is dropped with
// it, which is the right trade for reads of this daemon's own store: they are
// local, they are bounded here, and a client that hung up costs at most this
// long. What it buys is that "unreadable" in the answer means the store could
// not be read — never that the scan in front of it was slow.
func recordsContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), recordsBudget)
}

// liveOf is a session reduced to the identity the Swift store's records are
// compared with. The start time is asked of the kernel here, once per row per
// reading: it is one sysctl, and a cached one would be the one fact that could
// be wrong about a process that replaced another in the same second.
func liveOf(item session.Session) swiftstore.Live {
	return swiftstore.Live{
		TerminalID:     item.ID,
		TTY:            item.TTY,
		Assistant:      string(item.Assistant),
		PID:            int64(item.PID),
		ProcessStart:   swiftstore.ProcessStart(item.PID),
		ConversationID: item.ConversationID,
	}
}

// identityMatchCounts is SessionClosePolicy.identityMatchCounts: how many rows
// share each row's conversation. A row whose conversation could not be read
// counts as one; any unreadable conversation makes every other row of that
// assistant ambiguous, because it could be any of them.
func identityMatchCounts(items []session.Session) map[string]int {
	unreadable := map[session.Assistant]bool{}
	byConversation := map[string]int{}
	for _, item := range items {
		if item.ConversationID == "" {
			unreadable[item.Assistant] = true
			continue
		}
		byConversation[string(item.Assistant)+"\x01"+item.ConversationID]++
	}
	out := make(map[string]int, len(items))
	for _, item := range items {
		switch {
		case item.ConversationID == "":
			out[item.ID] = 1
		case unreadable[item.Assistant]:
			out[item.ID] = 0
		default:
			out[item.ID] = byConversation[string(item.Assistant)+"\x01"+item.ConversationID]
		}
	}
	return out
}

// rowLabel chooses a name by the Swift app's rungs, with the two it keeps in
// its store filled from there. A Claude conversation's automatic name is its
// thread rung.
func rowLabel(item session.Session, titles swiftstore.Titles) string {
	rungs := item.Rungs
	rungs.Manual = titles.Manual
	rungs.Orchestrator = titles.Orchestrator
	if rungs.Thread == "" {
		rungs.Thread = titles.Automatic
	}
	if label := session.PreferredLabel(rungs); label != "" {
		return label
	}
	return item.Label
}

type rowInput struct {
	item       session.Session
	live       swiftstore.Live
	label      string
	labelOf    func(string) string
	matches    int
	swift      swiftstore.Snapshot
	owed       []task.Obligation
	owedErr    error
	ownErr     error // this daemon's own records not all read (ownrecords.go)
	inv        session.Inventory
	now        time.Time
	generation int64
	// role is this daemon's own machine role, and ownRole whether it holds
	// one; see sessionsPayload.
	role    *coordinator.Record
	ownRole bool
}

// sessionRow renders one session in the shape the console reads.
//
// Fields that cannot be supported are left at their zero value and the
// contract marks them optional, so they are absent rather than invented.
func (s *Server) sessionRow(in rowInput) sessionRowWire {
	item := in.item
	state := string(item.State)
	row := contract.SessionRow{
		Icon:     wireIcon(s.icons.For(item.CWD)),
		ID:       item.ID,
		Backend:  contract.Backend(item.Backend),
		State:    contract.SessionState(item.State),
		IsClaude: item.Assistant == session.AssistantClaude,
		// Not in the Swift contract: how this row's state was learned. A
		// registry reading and a screen guess are different kinds of fact.
		Evidence:  contract.Evidence(item.Evidence),
		TTY:       item.TTY,
		Assistant: contract.Assistant(item.Assistant),
		Label:     in.label,
		Line:      item.Line,
		CWD:       item.CWD,
		SessionID: item.ConversationID,
		// How that id was obtained, or which kind of nothing took its place.
		// A row with no `sessionId` is three different situations, and only
		// one of them — a session that has not written anything yet — is
		// worth waiting out rather than acting on.
		Identity: contract.IdentityBinding(item.Binding),
		Shells:   wireShells(item.Shells),
	}
	out := sessionRowWire{Menu: wireMenu(item)}

	// The records are usable when the Swift store was read or is known to
	// hold nothing (absent, or switched off) and this daemon's own were read:
	// then the Swift app's rules run over both. Otherwise the daemon's own
	// projection answers with the evidence marked missing.
	if in.swift.Usable() && in.ownErr == nil {
		work := in.swift.Work(in.live, state)
		// This daemon's own peer waits are an input the Swift store does not
		// hold; they rank where the Swift app's coordination waits do.
		if work.State != "waiting_you" && ownWaitingOnPeer(item, in.owed) {
			work = swiftstore.Work{State: "waiting_session", Provenance: "broker", Owed: work.Owed}
		}
		row.WorkState = contract.WorkState(work.State)
		row.WorkProvenance = contract.WorkProvenance(work.Provenance)
		row.WorkNote = work.Note
		row.WorkSince = work.Since
		row.WorkMovedBy = work.MovedBy
		out.WorkPersonNeeded = work.PersonNeeded
		row.Owed = work.Owed
		row.Disposition = work.Disposition
	} else {
		// Without the store there is no reading of the task, the delivery or
		// the declaration behind a state, and the honest projection is the
		// daemon's own with that evidence marked missing.
		missing := errSwiftUnknown
		if in.ownErr != nil {
			missing = in.ownErr
		}
		row.WorkState = contract.WorkState(workState(item, in.owed, missing))
		row.WorkProvenance = contract.WorkProvenanceBroker
	}
	// A Feature Root or a wait that was read is a fact whatever else could
	// not be: they are drawn from the records there are, the daemon's own
	// among them.
	row.RootAssignment = in.swift.RootAssignment(in.live)
	row.Coordination = in.swift.Coordination(item.ID, in.labelOf)

	// One role, one source: this daemon's own when it holds one; otherwise
	// the Swift app's, which is nobody's when that store is absent or off.
	if in.ownRole {
		row.Coordinator = ownCrown(in.role, in.live)
	} else {
		row.Coordinator = in.swift.CoordinatorFor(in.live)
	}

	extra := ownCloseReasons(item, in.owed, in.owedErr)
	if in.ownErr != nil {
		extra = append(extra, contract.CloseReason{
			Code: "own_records_unreadable", Kind: "evidence",
			Mover: contract.CloseMover{Kind: "broker"},
		})
	}

	row.Closeability = in.swift.Closeability(swiftstore.CloseInput{
		Live:          in.live,
		TerminalState: state,
		Bound: in.live.Assistant != "" && in.live.PID != 0 &&
			!in.live.ProcessStart.IsZero() && in.live.ConversationID != "",
		Matches:             in.matches,
		InventoryComplete:   in.inv.Complete,
		InventoryObservedAt: in.inv.ObservedAt,
		Now:                 in.now,
		Generation:          in.generation,
		Extra:               extra,
	})
	out.SessionRow = row
	return out
}

var errSwiftUnknown = errors.New("the Swift store could not be read")

// ownWaitingOnPeer is this daemon's own record of a session parked on another.
func ownWaitingOnPeer(item session.Session, owed []task.Obligation) bool {
	for _, o := range owed {
		if o.Mover.Kind == task.MoverOtherSession && o.Subject == item.ID {
			return true
		}
	}
	return false
}

// ownCloseReasons carries this daemon's own obligations — the broker's pending
// landings (D01) — into the closeability projection beside the Swift store's,
// as Swift's `additionalObligations`. An unreadable list is evidence, so the
// row reads unknown rather than short.
func ownCloseReasons(item session.Session, owed []task.Obligation, err error) []contract.CloseReason {
	if err != nil {
		return []contract.CloseReason{{
			Code: "obligation_list_unreadable", Kind: "evidence",
			Mover: contract.CloseMover{Kind: "broker"},
		}}
	}
	c := task.Closeability(item.ID, owed, nil)
	out := make([]contract.CloseReason, 0, len(c.Reasons))
	for _, r := range c.Reasons {
		out = append(out, contract.CloseReason{
			Kind:        "obligation",
			Code:        string(r.Kind),
			SubjectID:   r.Mover.ID,
			SubjectKind: string(r.Mover.Kind),
			Mover:       wireCloseMover(r.Mover, item.ID),
		})
	}
	return out
}

// wireShells carries a session's background commands across. None is nil, so
// the key is absent as the Swift app leaves it.
func wireShells(shells []session.Shell) []contract.SessionShell {
	if len(shells) == 0 {
		return nil
	}
	out := make([]contract.SessionShell, 0, len(shells))
	for _, sh := range shells {
		out = append(out, contract.SessionShell{
			ID:      sh.ID,
			At:      sh.At.Unix(),
			Command: sh.Command,
			What:    sh.What,
			Doing:   sh.Doing,
		})
	}
	return out
}

// wireIcon carries a mark across to the wire in the shape the console draws.
func wireIcon(g icon.Grid) *contract.Icon {
	if len(g.Cells) == 0 {
		return nil
	}
	return &contract.Icon{Accent: g.Accent, Cells: g.Cells}
}

// wireMover is the one place a mover crosses from the domain to the wire. It is
// a function rather than a literal in three handlers because a mover that means
// something different in the obligation list than in a close reason is exactly
// the drift this contract exists to prevent.
func wireMover(m task.Mover) contract.Mover {
	return contract.Mover{Kind: contract.MoverKind(m.Kind), ID: m.ID}
}

// workState is this daemon's own projection, used only when the Swift store
// cannot be read: it gathers this session's axes and hands them to the
// projection.
func workState(item session.Session, owed []task.Obligation, owedErr error) task.WorkState {
	in := task.WorkInputs{
		AskedOnScreen:          item.State == session.StateWaiting,
		EvidenceMissing:        owedErr != nil || item.State == session.StateUnknown,
		Active:                 item.State == session.StateWorking,
		PromptWithoutAssistant: !item.IsAssistant() && item.State == session.StateIdle,
	}
	for _, o := range owed {
		switch {
		case o.Mover.Kind == task.MoverOtherSession && o.Subject == item.ID:
			in.WaitingOnPeer = true
		case o.Mover.Kind == task.MoverTask && o.Subject != "":
			// A task obligation belongs to whoever dispatched it; without that
			// link recorded yet this cannot claim the session, so it does not.
		}
	}
	return task.ProjectWorkState(in)
}

// wireCloseMover says who clears one reason, as the Swift app's
// CloseabilityMover.wire spells it.
//
// `self` is the distinction that matters to a reader: a thing this session has
// to do is a thing they can do now, and a thing another session has to do is
// only something to wait for — so the other session is named.
func wireCloseMover(m task.Mover, subject string) contract.CloseMover {
	switch m.Kind {
	case task.MoverPerson:
		return contract.CloseMover{Kind: "person", PersonNeeded: true}
	case task.MoverBroker:
		return contract.CloseMover{Kind: "broker"}
	case task.MoverTask:
		return contract.CloseMover{Kind: "task", TaskID: m.ID}
	}
	if m.ID == subject {
		return contract.CloseMover{Kind: "session", Self: true}
	}
	return contract.CloseMover{Kind: "session", SessionID: m.ID}
}

// The plan-window reader lives beside the Server rather than in it, for the
// same reason the usage collector does: this adds no field to server.go.
var (
	limitsOnce   sync.Once
	limitsReader *limits.Reader
)

// quotaReader is the one plan-window reader: a session's `/info` and the
// assistants route read the same five-second reading of each account.
func quotaReader() *limits.Reader {
	limitsOnce.Do(func() {
		home, _ := os.UserHomeDir()
		config := swiftstore.OpenQuotaConfig(swiftstore.Dir())
		limitsReader = limits.NewReader(home, func() limits.Settings {
			q := config.Read()
			return limits.Settings{StatusDir: q.StatusDir, CodexHome: q.CodexHome, LowThreshold: q.LowThreshold}
		})
	})
	return limitsReader
}

// wireWindows is a reading's windows as the wire spells them, in both places
// a window is sent.
func wireWindows(windows []limits.Window) []contract.SessionLimitWindow {
	out := make([]contract.SessionLimitWindow, 0, len(windows))
	for _, w := range windows {
		row := contract.SessionLimitWindow{Name: w.Name, Hit: w.Hit}
		if w.UsedPercent != nil {
			row.UsedPercent = *w.UsedPercent
		}
		if w.ResetsAt != nil {
			row.ResetsAt = *w.ResetsAt
		}
		out = append(out, row)
	}
	return out
}

// sessionLimits is what the Swift app's `/info` puts under `limits`: the
// account-level plan windows of the session's assistant
// (`AssistantQuota.machineLimits`), stamped with the moment they were read. A
// session with no assistant has none and no stamp.
//
// The windows come with the age of the record they were read from, the line
// that age is judged against, and — when there are none — which of the four
// kinds of nothing that was. A status line that draws a percentage without
// them draws a number from three hours ago as a number from now.
func (s *Server) sessionLimits(assistant session.Assistant, now time.Time) *contract.SessionLimits {
	out := &contract.SessionLimits{Windows: []contract.SessionLimitWindow{}}
	if assistant != session.AssistantClaude && assistant != session.AssistantCodex {
		return out
	}
	read := quotaReader().Machine(string(assistant), now)
	out.Windows = append(out.Windows, wireWindows(read.Windows)...)
	if read.At != nil {
		out.At = *read.At
		out.AgeSeconds = ageSeconds(*read.At, now)
	}
	if read.FreshFor > 0 {
		fresh := read.FreshFor
		out.FreshForSeconds = &fresh
	}
	out.Stale = read.Stale
	out.Detail = read.Detail
	out.UnknownReason = contract.AssistantUnknownReason(read.Reason)
	out.ReadAtMs = now.UnixMilli()
	return out
}

// ageSeconds is how old a provider record is now, never negative: a record
// stamped in the future is nought seconds old, not a negative age.
func ageSeconds(observedAt int64, now time.Time) *int64 {
	age := now.Unix() - observedAt
	if age < 0 {
		age = 0
	}
	return &age
}
