package orchestrator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/terminal"
)

// The hand-over routes (broker-design B8; the dispatch role contract in this
// machine's dispatch policy):
//
//   - a **handoff** continues an existing line of work in a new session: the
//     sender writes a package, and the receiver is told to walk its
//     REFERENCES, answer its VERIFICATION and continue from its OPEN THREADS;
//   - a **Root Assignment** opens an ordinary, independently owned root for a
//     new Feature, briefed with only an objective, scope, constraints,
//     references and acceptance — and nothing that makes it anybody's child:
//     no secret, no timeout, no result, no parent, no landing.
//
// Both open a tab and type one line into it, and both type that line at most
// once: the attempt is recorded before the keystroke, and a broker that died
// between the two finds the attempt and does not type again (the Swift app's
// O5: "Root Assignment was typed into the same root twice").

// --- opening a session --------------------------------------------------------

// openedSession is a tab this broker opened for somebody who is not its child.
type openedSession struct {
	TerminalID string `json:"terminal_id"`
	Backend    string `json:"backend"`
	OpenedAt   int64  `json:"opened_at"`
	// AutoCompactWindow is the window a Claude session was opened with, 0
	// for none; null for Codex, which is never given one, and on a record
	// from before it was kept (compact.go).
	AutoCompactWindow *int64 `json:"auto_compact_window"`
}

// openSession opens a new session running assistant in cwd, in a tmux session
// of its own named name, or an iTerm2 tab — whichever the machine's terminal
// setting and what is running choose, exactly as a child's tab is chosen.
// addDirs are the directories outside cwd the session is told it may read.
func (b *Broker) openSession(ctx context.Context, cwd, name, assistant, model string, addDirs []string) (openedSession, error) {
	if err := outside(); err != nil {
		return openedSession{}, err
	}
	if b.Launcher == nil {
		return openedSession{}, errors.New("this daemon cannot open a terminal")
	}
	launch, err := projects.Admit(projects.LaunchRequest{ProjectRoot: cwd, Assistant: assistant, Model: model})
	if err != nil {
		return openedSession{}, err
	}
	args := append([]string{}, launch.Arguments...)
	for _, dir := range addDirs {
		args = append(args, "--add-dir", projects.ShellQuoted(dir))
	}
	// The same answer a child's launch carries: Codex asks whether to trust
	// the directory before it draws anything, and a session nobody can type
	// into is a hand-over nobody receives (trustArgs).
	args = append(args, trustArgs(launch.Assistant, cwd)...)
	// Claude Code has no such flag, so the answer is recorded where Claude
	// Code keeps it — for the project folder a session was asked for, never a
	// child's disposable checkout (projects.TrustClaudeProject). A folder that
	// could not be recorded still opens: its dialog is then what the briefing
	// reports, as it was before.
	if launch.Assistant == projects.AssistantClaude && b.TrustClaudeProject != nil {
		if err := b.TrustClaudeProject(cwd); err != nil {
			log.Printf("broker: %s was not recorded as a folder Claude Code trusts: %v", cwd, err)
		}
	}
	// A session this broker opens is started with the machine's compaction
	// window, as a child is; nobody's task names one here.
	window := b.autoCompactFor(launch.Assistant, nil)
	command := envPrefix(launch.Assistant, autoCompactEnv(window)) +
		strings.Join(append([]string{launch.Assistant}, args...), " ")

	if b.Lanes != nil {
		release, err := b.Lanes.Acquire(ctx, "open:"+name)
		if err != nil {
			return openedSession{}, refuseWith(http.StatusTooManyRequests, "terminal_busy",
				"This machine already has as many terminal writes in hand as it admits; nothing was opened.",
				map[string]any{"retry_after": 5})
		}
		defer release()
	}
	itermOpen, _ := b.Launcher.ITermRunning(ctx)
	reach := projects.TmuxReach(b.Launcher.TmuxReach(ctx))
	choice := projects.TerminalAuto
	if b.Terminal != nil {
		choice = b.Terminal()
	}
	plan := projects.ChoosePlan(choice, itermOpen, reach)
	out := openedSession{AutoCompactWindow: window}
	switch plan {
	case projects.PlanITerm:
		out.TerminalID, err = b.Launcher.NewITermTab(ctx, "cd "+projects.ShellQuoted(cwd)+" && "+command)
		out.Backend = "iterm"
	case projects.PlanTmux, projects.PlanTmuxDetached:
		out.TerminalID, err = b.Launcher.NewTmuxSession(ctx, cwd, name, command)
		out.Backend = "tmux"
	default:
		err = terminal.Failure{Message: (childPlan{kind: plan, choice: choice, reach: reach}).failure(runtime.GOOS)}
	}
	if err != nil {
		return openedSession{}, err
	}
	out.OpenedAt = b.now().Unix()
	return out, nil
}

// waitComposer waits, at most within, for the session in terminalID to be an
// assistant drawing a composer — never a dialog (composer.go). Which composer
// it is waiting for is the assistant the session was opened with: the two CLIs
// draw different ones, and asking a Codex session for Claude Code's is waiting
// for something it never draws.
func (b *Broker) waitComposer(ctx context.Context, terminalID, assistant string, within time.Duration) error {
	deadline := b.now().Add(within)
	var last error
	for b.now().Before(deadline) {
		if s, ok := b.sessionByTerminal(ctx, terminalID); ok && s.IsAssistant() {
			ready, why := b.composerReady(ctx, terminalID, assistant)
			if ready {
				return nil
			}
			if why != nil {
				last = why
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	if last == nil {
		last = errors.New("the session did not reach a prompt")
	}
	return last
}

// composerWait is how long a new session has to draw a prompt before the line
// meant for it is recorded as not typed.
const composerWait = 150 * time.Second

// sessionName is the tmux session a hand-over opens: its kind and eight hex
// of its id, like a child's.
func sessionName(kind, id string) string {
	short := id
	if len(short) > 8 {
		short = short[:8]
	}
	return "clawdline-" + kind + "-" + short
}

var uuidShape = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// --- handoffs -----------------------------------------------------------------

// Handoff states, the Swift app's (Orchestrator.swift:1127-1131).
const (
	HandoffOpening     = "opening"
	HandoffDelivered   = "delivered"
	HandoffSpawnFailed = "spawn_failed"
)

// HandoffRequest is the closed body of POST /v1/orchestrator/handoffs.
type HandoffRequest struct {
	ID          string `json:"handoff_id"`
	ProjectDir  string `json:"project_dir"`
	Assistant   string `json:"assistant"`
	Model       string `json:"model"`
	Title       string `json:"title"`
	FromSession string `json:"from_session"`
	// Plain is the sender's word that this is a plain handoff and not the
	// machine role's succession. Only the JSON value true is that word. The
	// word is still asked for although succession is not implemented: the
	// sender is saying what it means, and a build that gains succession must
	// not inherit a pile of handoffs that never said.
	Plain *bool `json:"coordinator_plain_handoff"`
}

// Handoff is one handoff as this broker keeps it.
type Handoff struct {
	ID          string         `json:"handoff_id"`
	State       string         `json:"state"`
	ProjectDir  string         `json:"project_dir"`
	Dir         string         `json:"dir"`
	Title       string         `json:"title,omitempty"`
	FromSession string         `json:"from_session"`
	FromTerm    string         `json:"from_terminal,omitempty"`
	Plain       bool           `json:"coordinator_plain_handoff"`
	Assistant   string         `json:"assistant"`
	Model       string         `json:"model,omitempty"`
	CreatedAt   int64          `json:"created"`
	Opened      *openedSession `json:"opened,omitempty"`
	// TypeAttemptedAt is recorded before the line is typed, and is the reason
	// it is never typed twice. DeliveredAt is when it was typed into a
	// composer: typed, not read — the receipt that it was read is the
	// receiver's first turn, which the session list shows.
	TypeAttemptedAt int64  `json:"type_attempted_at,omitempty"`
	DeliveredAt     int64  `json:"delivered_at,omitempty"`
	Failure         string `json:"failure,omitempty"`
	// Receipt is what the sender was told, once.
	Receipt string `json:"receipt,omitempty"`
}

// Handoff limits, the Swift app's (OrchestratorHandoffSender.swift:42-98).
const (
	handoffTitleLimit = 200
	handoffFromLimit  = 200
	// handoffListLimit is the most rows one read of the list answers.
	handoffListLimit = 200
)

// HandoffRoot is where handoff packages are written: beside the task
// directories, never inside one.
func (b *Broker) HandoffRoot() string { return filepath.Join(b.Dir, "handoffs") }

// HandoffLine is the one line typed into the receiver (Orchestrator.swift:3252).
func HandoffLine(path string) string {
	return "You are picking up a Clawdline handoff. Read " + path + " before anything else and follow it: " +
		"walk its REFERENCES, answer its VERIFICATION questions from those sources, say plainly what you could " +
		"not reach, then continue from OPEN THREADS."
}

func badHandoff(message string) error {
	return refuse(http.StatusUnprocessableEntity, "bad_task", message)
}

// OpenHandoff is the route. The order of the refusals is the Swift app's, so a
// caller correcting one at a time meets them in the same sequence.
func (b *Broker) OpenHandoff(ctx context.Context, req HandoffRequest) (Handoff, bool, error) {
	if len(req.ID) != 36 || !uuidShape.MatchString(req.ID) {
		return Handoff{}, false, badHandoff("handoff_id must be a lowercase UUID.")
	}
	// A handoff that exists is answered as it is: a resend is not a second
	// handoff, and nothing about the sender is asked again.
	if o, err := b.Store.ReadOpened(ctx, store.TableHandoffs, req.ID); err == nil {
		h, derr := decodeHandoff(o)
		if derr != nil {
			return Handoff{}, false, derr
		}
		return h, true, nil
	} else if !errors.Is(err, store.ErrNoOpened) {
		return Handoff{}, false, storeError(err)
	}

	from := strings.TrimSpace(req.FromSession)
	switch {
	case from == "":
		return Handoff{}, false, refuse(http.StatusBadRequest, "from_session_required",
			"from_session is required: the conversation id of the session handing its work over.")
	case utf8.RuneCountInString(from) > handoffFromLimit:
		return Handoff{}, false, refuse(http.StatusBadRequest, "from_session_invalid",
			"from_session is at most 200 characters.")
	}
	sender, err := b.terminalFor(ctx, from, "")
	if err != nil {
		var ref Refusal
		if errors.As(err, &ref) {
			switch ref.Code {
			case "conversation_not_found":
				return Handoff{}, false, refuse(http.StatusNotFound, "sender_not_found",
					"No live session is bound to that conversation id; a handoff is sent by a session that is here.")
			case "conversation_ambiguous":
				return Handoff{}, false, refuse(http.StatusConflict, "sender_ambiguous",
					"More than one live session claims that conversation id; none was chosen.")
			}
		}
		return Handoff{}, false, err
	}
	// A plain handoff from the role's holder would leave the crown behind
	// with nobody under it. Succession — moving the crown with the work — is
	// what would answer that, and this daemon does not have it: the refusal
	// says so plainly rather than naming a route that answers 501 (remedy.go).
	rec, status, err := b.Store.Coordinator(ctx)
	switch {
	case err != nil:
		return Handoff{}, false, refuseWith(http.StatusConflict, "coordinator_store_unreadable",
			"Whether the sender holds the machine role could not be read; nothing was opened.",
			map[string]any{"cause": err.Error()})
	case status == store.CoordinatorCorrupt || status == store.CoordinatorUnsupported:
		return Handoff{}, false, refuse(http.StatusConflict, "coordinator_store_unreadable",
			"The machine role is stored in a form this daemon cannot read; nothing was opened.")
	case rec != nil && rec.ConversationID == from:
		return Handoff{}, false, refuseWith(http.StatusConflict, "succession_required",
			"The sender holds the machine role, and a plain handoff would leave the role behind with nobody "+
				"under it. Moving the role with the work is not something this daemon can do; the remediation "+
				"in this refusal says what is missing and what can be done instead.",
			withRemedy(map[string]any{"coordinator_id": rec.ID, "expected_generation": rec.Generation,
				"sender_session_id": from}, "succession_required"))
	}
	if err := b.admitDispatch(); err != nil {
		return Handoff{}, false, err
	}
	refund := true
	defer func() {
		if refund {
			b.refundDispatch()
		}
	}()

	if req.Plain == nil || !*req.Plain {
		return Handoff{}, false, badHandoff("coordinator_plain_handoff must be the JSON value true.")
	}
	project := filepath.Clean(strings.TrimSpace(req.ProjectDir))
	if !filepath.IsAbs(project) || !dirExists(project) {
		return Handoff{}, false, badHandoff("project_dir must be an absolute path to an existing directory.")
	}
	assistant := strings.TrimSpace(req.Assistant)
	if assistant == "" {
		assistant = "claude"
	}
	if assistant != "claude" && assistant != "codex" {
		return Handoff{}, false, badHandoff("assistant must be claude or codex.")
	}
	model := strings.TrimSpace(req.Model)
	if model != "" && !modelName(model) {
		return Handoff{}, false, badHandoff("model must be a model name: lower-case letters, digits, . _ -, at most 64 characters.")
	}
	title := strings.TrimSpace(req.Title)
	if utf8.RuneCountInString(title) > handoffTitleLimit {
		return Handoff{}, false, badHandoff("title is at most 200 characters.")
	}
	dir := filepath.Join(b.HandoffRoot(), req.ID)
	pkg := filepath.Join(dir, "handoff.md")
	if info, err := os.Lstat(pkg); err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		return Handoff{}, false, refuseWith(http.StatusUnprocessableEntity, "bad_task",
			"Write the handoff package first: "+pkg+
				" must be a non-empty regular file (REFERENCES, VERIFICATION, OPEN THREADS). Then send the same request again.",
			withRemedy(map[string]any{"package": pkg, "handoff_id": req.ID, "project_dir": project,
				"from_session": from, "coordinator_plain_handoff": true}, "bad_task"))
	}

	now := b.now()
	h := Handoff{
		ID: req.ID, State: HandoffOpening, ProjectDir: project, Dir: dir, Title: title,
		FromSession: from, FromTerm: sender.ID, Plain: true, Assistant: assistant, Model: model, CreatedAt: now.Unix(),
	}
	if err := b.createOpened(ctx, store.TableHandoffs, h.ID, h.State, h, "handoff.opening", now); err != nil {
		if errors.Is(err, store.ErrOpenedExists) {
			if o, rerr := b.Store.ReadOpened(ctx, store.TableHandoffs, req.ID); rerr == nil {
				if replay, derr := decodeHandoff(o); derr == nil {
					return replay, true, nil
				}
			}
		}
		return Handoff{}, false, storeError(err)
	}
	refund = false

	opened, openErr := b.openSession(ctx, project, sessionName("handoff", h.ID), assistant, model, []string{dir})
	if openErr != nil {
		var ref Refusal
		if errors.As(openErr, &ref) {
			_, _ = b.updateHandoff(ctx, h.ID, "handoff.spawn_failed", func(x *Handoff) {
				x.State, x.Failure = HandoffSpawnFailed, ref.Message
			})
			return Handoff{}, false, ref
		}
		out, _ := b.updateHandoff(ctx, h.ID, "handoff.spawn_failed", func(x *Handoff) {
			x.State, x.Failure = HandoffSpawnFailed, openErr.Error()
		})
		b.handoffReceipt(ctx, out)
		return out, false, nil
	}
	h, _ = b.updateHandoff(ctx, h.ID, "handoff.opened", func(x *Handoff) { x.Opened = &opened })

	typed := b.typeOnce(ctx, opened.TerminalID, assistant, HandoffLine(pkg), func() error {
		_, err := b.updateHandoff(ctx, h.ID, "handoff.typing", func(x *Handoff) { x.TypeAttemptedAt = b.now().Unix() })
		return err
	})
	if typed != nil {
		h, _ = b.updateHandoff(ctx, h.ID, "handoff.spawn_failed", func(x *Handoff) {
			x.State, x.Failure = HandoffSpawnFailed, typed.Error()
		})
	} else {
		h, _ = b.updateHandoff(ctx, h.ID, "handoff.delivered", func(x *Handoff) {
			x.State, x.DeliveredAt = HandoffDelivered, b.now().Unix()
		})
	}
	b.handoffReceipt(ctx, h)
	return h, false, nil
}

// typeOnce waits for a composer, records the attempt, then types — the
// attempt is durable before the keystroke, so it is never made twice.
func (b *Broker) typeOnce(ctx context.Context, terminalID, assistant, line string, attempt func() error) error {
	if err := b.waitComposer(ctx, terminalID, assistant, composerWait); err != nil {
		return err
	}
	if err := attempt(); err != nil {
		return err
	}
	return b.typeLine(ctx, terminalID, line)
}

// handoffReceipt tells the sender, once, whether the receiver was given the
// line: the Swift app's `handoff_receipt` notice, in its envelope.
func (b *Broker) handoffReceipt(ctx context.Context, h Handoff) {
	if h.FromTerm == "" || b.Type == nil {
		return
	}
	state, body := "picked_up", "The handoff line was typed into the receiver's composer."
	if h.State != HandoffDelivered {
		state, body = "first_line_failed", "The receiver was not given the handoff line: "+h.Failure
	}
	notice := map[string]any{
		"protocol": NoticeProtocol, "version": NoticeVersion, "kind": "handoff_receipt", "audience": "source",
		"handoff_id": h.ID, "assistant": h.Assistant, "project_dir": h.ProjectDir, "state": state, "body": body,
	}
	if h.Title != "" {
		notice["title"] = h.Title
	}
	encoded, err := json.Marshal(notice)
	if err != nil || strings.ContainsAny(string(encoded), "\n\r") {
		return
	}
	payload, _ := json.Marshal(messageEffect{Target: h.FromTerm, Source: "clawdline", Accepted: b.now().Unix(),
		Wire: "<clawdline-notice>" + string(encoded) + "</clawdline-notice>"})
	accepted, _ := json.Marshal(map[string]any{"handoff": h.ID, "state": state})
	ids, err := b.Store.RecordIntent(ctx,
		[]store.Event{{Kind: "handoff.receipt", Subject: h.ID, Payload: accepted}},
		[]store.Effect{{Kind: EffectHandoffReceipt, Subject: h.FromTerm, Payload: payload}})
	if err != nil {
		return
	}
	outcome := ""
	for _, res := range b.runRecorded(ctx, ids) {
		outcome = res.outcome
	}
	_, _ = b.updateHandoff(ctx, h.ID, "handoff.receipt."+state, func(x *Handoff) { x.Receipt = state + ": " + outcome })
}

// EffectHandoffReceipt types the handoff receipt into its sender. It is the
// message effect's handler under a name of its own, so a count of receipts is
// not a count of messages.
const EffectHandoffReceipt = "handoff.receipt"

func init() {
	effectHandlers[EffectHandoffReceipt] = effectHandler{idempotent: false, run: runMessage}
}

// Handoffs is the newest handoffs first.
func (b *Broker) Handoffs(ctx context.Context) ([]Handoff, error) {
	rows, err := b.Store.ListOpened(ctx, store.TableHandoffs, handoffListLimit)
	if err != nil {
		return nil, storeError(err)
	}
	out := make([]Handoff, 0, len(rows))
	for _, o := range rows {
		if h, err := decodeHandoff(o); err == nil {
			out = append(out, h)
		}
	}
	return out, nil
}

// HandoffByID reads one handoff.
func (b *Broker) HandoffByID(ctx context.Context, id string) (Handoff, error) {
	o, err := b.Store.ReadOpened(ctx, store.TableHandoffs, id)
	if errors.Is(err, store.ErrNoOpened) {
		return Handoff{}, refuse(http.StatusNotFound, "not_found", "No handoff named that.")
	}
	if err != nil {
		return Handoff{}, storeError(err)
	}
	return decodeHandoff(o)
}

func decodeHandoff(o store.Opened) (Handoff, error) {
	var h Handoff
	if err := json.Unmarshal(o.Record, &h); err != nil {
		return Handoff{}, refuseWith(http.StatusConflict, "handoff_unreadable",
			"A handoff with this id is stored and cannot be read; nothing was changed.",
			map[string]any{"handoff_id": o.ID, "cause": err.Error()})
	}
	return h, nil
}

func (b *Broker) updateHandoff(ctx context.Context, id, kind string, change func(*Handoff)) (Handoff, error) {
	var out Handoff
	_, err := b.Store.UpdateOpened(ctx, store.TableHandoffs, id, func(o store.Opened) (*store.Opened, []store.Event, error) {
		h, err := decodeHandoff(o)
		if err != nil {
			return nil, nil, err
		}
		change(&h)
		out = h
		body, _ := json.Marshal(h)
		payload, _ := json.Marshal(map[string]any{"handoff": id, "state": h.State})
		return &store.Opened{ID: id, State: h.State, Record: body, CreatedAt: o.CreatedAt, UpdatedAt: b.now()},
			[]store.Event{{Kind: kind, Subject: id, Payload: payload}}, nil
	})
	return out, err
}

func (b *Broker) createOpened(ctx context.Context, table, id, state string, record any, kind string, now time.Time) error {
	body, err := json.Marshal(record)
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]any{"id": id, "state": state})
	return b.Store.CreateOpened(ctx, table, store.Opened{ID: id, State: state, Record: body, CreatedAt: now, UpdatedAt: now},
		[]store.Event{{Kind: kind, Subject: id, Payload: payload}})
}

// --- Root Assignments -----------------------------------------------------------

// Root Assignment states, the Swift app's (OrchestratorRootAssignmentShape.swift:23-26)
// less the three this daemon does not observe yet (`prompt_ready` is folded
// into the wait, `active` and `inactive` are a session list's reading).
const (
	AssignmentAccepted       = "accepted"
	AssignmentTerminalOpened = "terminal_opened"
	AssignmentBriefed        = "briefed"
	AssignmentFailed         = "failed"
)

// ScopeRootAssignments is the receipt scope of the route (D03).
const ScopeRootAssignments = "orchestrator.root_assignments"

// Assignment is the five things a Feature Root is briefed with, and nothing
// else.
type Assignment struct {
	Objective          string `json:"objective"`
	Scope              string `json:"scope"`
	Constraints        string `json:"constraints"`
	RelevantReferences string `json:"relevant_references"`
	Acceptance         string `json:"acceptance"`
}

// RootAssignmentRequest is the closed body.
type RootAssignmentRequest struct {
	RequestID  string     `json:"request_id"`
	Assistant  string     `json:"assistant"`
	Model      string     `json:"model"`
	ProjectDir string     `json:"project_dir"`
	Label      string     `json:"label"`
	Assignment Assignment `json:"assignment"`
}

// RootAssignment is one Feature Root as this broker keeps it. It carries no
// child lineage: no task, parent, secret, timeout, result or landing — the
// role contract says so, and a reader must be able to tell by its shape.
type RootAssignment struct {
	ID         string         `json:"id"`
	RequestID  string         `json:"request_id"`
	Assistant  string         `json:"assistant"`
	Model      string         `json:"model"`
	ProjectDir string         `json:"project_dir"`
	Label      string         `json:"label"`
	State      string         `json:"state"`
	Assignment Assignment     `json:"assignment"`
	CreatedAt  int64          `json:"created_at"`
	Ownership  string         `json:"ownership"`
	Executor   *openedSession `json:"executor,omitempty"`
	BriefPath  string         `json:"brief_path"`
	// BriefAttemptedAt is durable before the keystroke (O5).
	BriefAttemptedAt int64  `json:"brief_attempted_at,omitempty"`
	BriefedAt        int64  `json:"briefed_at,omitempty"`
	Failure          string `json:"failure,omitempty"`
}

// The assignment's limits, the Swift app's (Orchestrator.swift:3398-3466).
const (
	assignmentFieldLimit = 8192
	assignmentTotalLimit = 32768
	assignmentLabelLimit = 200
	assignmentListLimit  = 200
)

// AssignmentRoot is where Feature Roots' briefs are written.
func (b *Broker) AssignmentRoot() string { return filepath.Join(b.Dir, "root-assignments") }

func badAssignment(message string) error {
	return refuse(http.StatusUnprocessableEntity, "bad_root_assignment", message)
}

// validateAssignment is the closed body's checks, in the Swift app's order.
func validateAssignment(req RootAssignmentRequest) error {
	if !uuidShape.MatchString(req.RequestID) {
		return badAssignment("request_id must be a lowercase UUID.")
	}
	if req.Assistant != "claude" && req.Assistant != "codex" {
		return badAssignment("assistant must be claude or codex.")
	}
	if req.Model != "" && req.Model != "default" && !modelName(req.Model) {
		return badAssignment("model must be \"default\" or a model name.")
	}
	if !filepath.IsAbs(req.ProjectDir) || !dirExists(req.ProjectDir) {
		return badAssignment("project_dir must be an absolute path to an existing directory.")
	}
	label := strings.TrimSpace(req.Label)
	if label == "" || len(req.Label) > assignmentLabelLimit {
		return badAssignment("label must be 1–200 bytes and not blank.")
	}
	total := 0
	for name, v := range map[string]string{
		"objective": req.Assignment.Objective, "scope": req.Assignment.Scope,
		"constraints": req.Assignment.Constraints, "relevant_references": req.Assignment.RelevantReferences,
		"acceptance": req.Assignment.Acceptance,
	} {
		if strings.TrimSpace(v) == "" || len(v) > assignmentFieldLimit || strings.ContainsRune(v, 0) {
			return badAssignment("assignment." + name + " must be 1–8192 bytes, not blank, with no NUL.")
		}
		total += len(v)
	}
	if total > assignmentTotalLimit {
		return badAssignment("The assignment is at most 32768 bytes altogether.")
	}
	return nil
}

// AssignmentBrief is the file a Feature Root is told to read first.
func AssignmentBrief(id string, a Assignment) string {
	var sb strings.Builder
	sb.WriteString("You are an independently owned Clawdline Feature Root for Root Assignment " + id + ".\n")
	sb.WriteString("Own this feature through implementation, verification, integration, and landing.\n\n")
	for _, s := range []struct{ head, body string }{
		{"OBJECTIVE", a.Objective}, {"SCOPE", a.Scope}, {"CONSTRAINTS", a.Constraints},
		{"RELEVANT REFERENCES", a.RelevantReferences}, {"ACCEPTANCE", a.Acceptance},
	} {
		sb.WriteString(s.head + "\n" + strings.TrimSpace(s.body) + "\n\n")
	}
	return sb.String()
}

// AssignmentLine is the one line typed into the Feature Root. The five
// sections are in the file it names rather than typed: a multi-line paste is
// the terminal write most likely to be split by a composer, and a Feature Root
// briefed with half its acceptance is worse than one told where the whole of
// it is.
func AssignmentLine(id, path string) string {
	return "You are an independently owned Clawdline Feature Root for Root Assignment " + id + ". " +
		"Own this feature through implementation, verification, integration, and landing. " +
		"Read " + path + " now, before anything else: it is your OBJECTIVE, SCOPE, CONSTRAINTS, " +
		"RELEVANT REFERENCES and ACCEPTANCE."
}

// assignmentDigest is the request as its receipt keeps it: the canonical
// body, so the same assignment resent is recognised and a different one under
// the same request_id is refused.
func assignmentDigest(req RootAssignmentRequest) string {
	body, _ := json.Marshal(req)
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// OpenRootAssignment is the route. key is the Idempotency-Key header, which
// must be the request_id.
func (b *Broker) OpenRootAssignment(ctx context.Context, key string, req RootAssignmentRequest) (RootAssignment, bool, error) {
	if err := validateAssignment(req); err != nil {
		return RootAssignment{}, false, err
	}
	if strings.TrimSpace(key) != req.RequestID {
		return RootAssignment{}, false, refuse(http.StatusUnprocessableEntity, "idempotency_mismatch",
			"The Idempotency-Key header must equal request_id.")
	}
	receipt := store.ReceiptKey{Scope: ScopeRootAssignments, Actor: "machine", Key: req.RequestID}
	claim, err := b.Store.ClaimReceipt(ctx, receipt, assignmentDigest(req), store.ReceiptPolicy{}, b.now())
	if err != nil {
		return RootAssignment{}, false, storeError(err)
	}
	switch claim.Outcome {
	case store.ReceiptReplay:
		var answer struct {
			ID string `json:"id"`
		}
		if claim.Answer.Status != http.StatusOK || json.Unmarshal(claim.Answer.Body, &answer) != nil {
			var r storedRefusal
			_ = json.Unmarshal(claim.Answer.Body, &r)
			return RootAssignment{}, false, Refusal{Status: claim.Answer.Status, Code: r.Code, Message: r.Message, Extra: r.Extra}
		}
		a, err := b.RootAssignmentByID(ctx, answer.ID)
		return a, true, err
	case store.ReceiptMismatch:
		return RootAssignment{}, false, refuse(http.StatusConflict, "request_conflict",
			"This request_id was already used for a different assignment; nothing was opened.")
	case store.ReceiptNew:
	default:
		_, err, _ := b.claimed(ctx, receipt, claim)
		return RootAssignment{}, false, err
	}
	answered := false
	defer func() {
		if !answered {
			_ = b.Store.ReleaseReceipt(context.WithoutCancel(ctx), receipt)
		}
	}()
	if err := b.admitDispatch(); err != nil {
		return RootAssignment{}, false, err
	}
	now := b.now()
	id := b.newID()
	dir := filepath.Join(b.AssignmentRoot(), id)
	a := RootAssignment{
		ID: id, RequestID: req.RequestID, Assistant: req.Assistant, Model: req.Model,
		ProjectDir: filepath.Clean(req.ProjectDir), Label: strings.TrimSpace(req.Label), State: AssignmentAccepted,
		Assignment: req.Assignment, CreatedAt: now.Unix(), Ownership: "independent_root",
		BriefPath: filepath.Join(dir, "ASSIGNMENT.md"),
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		b.refundDispatch()
		return RootAssignment{}, false, refuse(http.StatusServiceUnavailable, "persistence_failed", err.Error())
	}
	if err := writeFileSync(a.BriefPath, []byte(AssignmentBrief(id, req.Assignment))); err != nil {
		b.refundDispatch()
		return RootAssignment{}, false, refuse(http.StatusServiceUnavailable, "persistence_failed", err.Error())
	}
	if err := b.createOpened(ctx, store.TableRootAssignments, id, a.State, a, "root_assignment.accepted", now); err != nil {
		b.refundDispatch()
		return RootAssignment{}, false, storeError(err)
	}
	answer, _ := json.Marshal(map[string]string{"id": id})
	if err := b.Store.CompleteReceipt(ctx, receipt, store.ReceiptAnswer{Status: http.StatusOK, Body: answer}); err == nil {
		answered = true
	}

	model := req.Model
	if model == "default" {
		model = ""
	}
	opened, err := b.openSession(ctx, a.ProjectDir, sessionName("root", id), req.Assistant, model, []string{dir})
	if err != nil {
		out, _ := b.updateAssignment(ctx, id, "root_assignment.failed", func(x *RootAssignment) {
			x.State, x.Failure = AssignmentFailed, "terminal_open_failed: "+err.Error()
		})
		return out, false, nil
	}
	a, _ = b.updateAssignment(ctx, id, "root_assignment.terminal_opened", func(x *RootAssignment) {
		x.State, x.Executor = AssignmentTerminalOpened, &opened
	})
	typed := b.typeOnce(ctx, opened.TerminalID, req.Assistant, AssignmentLine(id, a.BriefPath), func() error {
		_, err := b.updateAssignment(ctx, id, "root_assignment.briefing", func(x *RootAssignment) {
			x.BriefAttemptedAt = b.now().Unix()
		})
		return err
	})
	if typed != nil {
		a, _ = b.updateAssignment(ctx, id, "root_assignment.failed", func(x *RootAssignment) {
			x.State, x.Failure = AssignmentFailed, "brief_failed: "+typed.Error()
		})
		return a, false, nil
	}
	a, _ = b.updateAssignment(ctx, id, "root_assignment.briefed", func(x *RootAssignment) {
		x.State, x.BriefedAt = AssignmentBriefed, b.now().Unix()
	})
	return a, false, nil
}

// RootAssignments is the newest first.
func (b *Broker) RootAssignments(ctx context.Context) ([]RootAssignment, error) {
	rows, err := b.Store.ListOpened(ctx, store.TableRootAssignments, assignmentListLimit)
	if err != nil {
		return nil, storeError(err)
	}
	out := make([]RootAssignment, 0, len(rows))
	for _, o := range rows {
		var a RootAssignment
		if json.Unmarshal(o.Record, &a) == nil {
			out = append(out, a)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out, nil
}

// RootAssignmentByID reads one.
func (b *Broker) RootAssignmentByID(ctx context.Context, id string) (RootAssignment, error) {
	o, err := b.Store.ReadOpened(ctx, store.TableRootAssignments, id)
	if errors.Is(err, store.ErrNoOpened) {
		return RootAssignment{}, refuse(http.StatusNotFound, "not_found", "No Root Assignment named that.")
	}
	if err != nil {
		return RootAssignment{}, storeError(err)
	}
	var a RootAssignment
	if err := json.Unmarshal(o.Record, &a); err != nil {
		return RootAssignment{}, refuseWith(http.StatusConflict, "root_assignment_unreadable",
			"A Root Assignment with this id is stored and cannot be read.", map[string]any{"id": id})
	}
	return a, nil
}

func (b *Broker) updateAssignment(ctx context.Context, id, kind string, change func(*RootAssignment)) (RootAssignment, error) {
	var out RootAssignment
	_, err := b.Store.UpdateOpened(ctx, store.TableRootAssignments, id, func(o store.Opened) (*store.Opened, []store.Event, error) {
		var a RootAssignment
		if err := json.Unmarshal(o.Record, &a); err != nil {
			return nil, nil, err
		}
		change(&a)
		out = a
		body, _ := json.Marshal(a)
		payload, _ := json.Marshal(map[string]any{"root_assignment": id, "state": a.State})
		return &store.Opened{ID: id, State: a.State, Record: body, CreatedAt: o.CreatedAt, UpdatedAt: b.now()},
			[]store.Event{{Kind: kind, Subject: id, Payload: payload}}, nil
	})
	return out, err
}
