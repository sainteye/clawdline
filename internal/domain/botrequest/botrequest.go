// Package botrequest holds the rules of the Bot request card (Bot 交辦卡,
// docs/bot-request-cards.md): one request of the person, carried across Bots,
// with a holder, a self-reported checklist, optional verdicts and a completion
// that notifies once per cycle. It does no I/O; the store applies these rules
// inside one transaction per card.
package botrequest

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// The bounds of §10. Each is registered in internal/domain/capacity. Nothing
// else about a card is bounded here: the request body limit every route has is
// its only size guard (§10, "Limits deliberately not set").
const (
	// MaxPageRows is one page of a card listing; more rows come by cursor.
	MaxPageRows = 100
	// MaxReceipts is the idempotency window. When it is full the oldest
	// receipt expires early; a write is never refused for it.
	MaxReceipts = 4_096
	// MaxMergeHops bounds following merged_into from a named card to the
	// card that absorbed it; past it a dispatch naming the card is refused.
	MaxMergeHops = 8
)

// Card states.
const (
	StateOpen      = "open"
	StateDone      = "done"
	StateCancelled = "cancelled"
	StateMerged    = "merged"
)

// Baton roles.
const (
	RoleMaker      = "maker"
	RoleReviewer   = "reviewer"
	RoleIntegrator = "integrator"
)

// Checklist entry states.
const (
	EntryOpen      = "open"
	EntryDone      = "done"
	EntryNotNeeded = "not_needed"
)

// Verdicts.
const (
	VerdictPass            = "pass"
	VerdictConditionalPass = "conditional_pass"
	VerdictReject          = "reject"
)

// Event kinds written to bot_request_events (§4).
const (
	EventCreated        = "card.created"
	EventHanded         = "baton.handed"
	EventVerdict        = "verdict.recorded"
	EventChecklistAdded = "checklist.added"
	EventChecklistMark  = "checklist.marked"
	EventWaitingSet     = "waiting.set"
	EventWaitingCleared = "waiting.cleared"
	EventDone           = "card.done"
	EventNotified       = "notified"
	EventCancelled      = "card.cancelled"
	EventReopened       = "card.reopened"
	EventChainJoined    = "chain.joined"
)

// Progress-state labels shown on a row (§9.1).
const (
	ProgressInProgress = "in_progress"
	ProgressWaiting    = "waiting_for_you"
	ProgressDone       = "done"
	ProgressCancelled  = "cancelled"
	ProgressMerged     = "merged"
	// ProgressStalled is an open card nobody is working on after its last
	// attempt went wrong. The card alone cannot tell; the daemon's read
	// decides it from its holder's turns and work (§9.1).
	ProgressStalled = "stalled"
)

// Refusal codes. The first five are reserved by §4.1; the rest are the
// ordinary shapes every write can be refused with.
const (
	CodeChecklistOpen   = "bot_request_checklist_open"
	CodeChecklistEmpty  = "bot_request_checklist_empty"
	CodeAlreadyMerged   = "bot_request_already_merged"
	CodeVersionConflict = "bot_request_version_conflict"
	CodeHandoffScope    = "bot_request_handoff_scope"
	CodeForbidden       = "bot_request_forbidden"
	CodeInvalid         = "bot_request_invalid"
	CodeNotFound        = "bot_request_not_found"
	CodeAmbiguous       = "bot_request_ambiguous"
	CodeClosed          = "bot_request_closed"
	CodeNoAuthority     = "bot_request_no_person_authority"
)

// Refusal is a typed refusal a caller branches on by Code. Field names the
// input at fault when there is one; Version is the card's current version on a
// version conflict.
type Refusal struct {
	Code       string   `json:"code"`
	Field      string   `json:"field,omitempty"`
	Version    int64    `json:"version,omitempty"`
	Reason     string   `json:"reason,omitempty"`
	Candidates []string `json:"candidates,omitempty"`
}

func (r *Refusal) Error() string {
	message := r.Code
	if r.Field != "" {
		message += ": " + r.Field
	}
	if r.Reason != "" {
		message += ": " + r.Reason
	}
	if len(r.Candidates) > 0 {
		message += ": " + strings.Join(r.Candidates, ", ")
	}
	return message
}

func refuse(code, field string) error { return &Refusal{Code: code, Field: field} }

// CodeOf answers the refusal code of err, or "" when err is not a Refusal.
func CodeOf(err error) string {
	var r *Refusal
	if errors.As(err, &r) {
		return r.Code
	}
	return ""
}

// Authority is the person's message a card roots in.
type Authority struct {
	Bot            string `json:"bot"`
	ConversationID string `json:"conversation_id"`
	TurnID         string `json:"turn_id"`
	MessageID      string `json:"message_id"`
}

// Baton is the one holder of a card.
type Baton struct {
	Role string `json:"role"`
	Bot  string `json:"bot"`
}

// Entry is one checklist entry: an unordered to-do the holder reports.
type Entry struct {
	Key        string    `json:"key"`
	Label      string    `json:"label"`
	State      string    `json:"state"`
	Note       string    `json:"note,omitempty"`
	ReportedBy string    `json:"reported_by,omitempty"`
	ReportedAt time.Time `json:"reported_at,omitzero"`
}

// Verdict is one reviewer Bot's recorded verdict. Never edited.
type Verdict struct {
	ID          string    `json:"id"`
	Commit      string    `json:"commit,omitempty"`
	Verdict     string    `json:"verdict"`
	Text        string    `json:"text,omitempty"`
	ReviewerBot string    `json:"reviewer_bot"`
	At          time.Time `json:"at"`
}

// Card is one Bot request card with its checklist and verdicts.
type Card struct {
	ID          string      `json:"id"`
	ProjectID   string      `json:"project_id"`
	ProjectPath string      `json:"project_path"`
	Kind        string      `json:"kind,omitempty"`
	Title       string      `json:"title"`
	Description string      `json:"description,omitempty"`
	Authorities []Authority `json:"authority"`
	CreatedBy   string      `json:"created_by"`
	Baton       Baton       `json:"baton"`
	Chain       []string    `json:"chain"`
	BoardItemID string      `json:"board_item_id,omitempty"`
	State       string      `json:"state"`
	Waiting     string      `json:"waiting,omitempty"`
	MergedInto  string      `json:"merged_into,omitempty"`
	SplitFrom   string      `json:"split_from,omitempty"`
	// Cycle counts completions: it is 1 for the first and grows by one on
	// each reopen, so each completion has its own notification key.
	Cycle     int64     `json:"cycle"`
	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	// UpdatedAt is the time of the card's last event, shown as "last
	// updated" so a stalled handoff is visible (§4).
	UpdatedAt time.Time `json:"updated_at"`
	ClosedAt  time.Time `json:"closed_at,omitzero"`
	Checklist []Entry   `json:"checklist"`
	Verdicts  []Verdict `json:"verdicts,omitempty"`
}

// Actor is who writes: the person, or one Bot. A Session is never an actor.
type Actor struct {
	Person bool   `json:"person,omitempty"`
	Bot    string `json:"bot,omitempty"`
}

func (a Actor) Name() string {
	if a.Person {
		return "person"
	}
	return "bot:" + a.Bot
}

func (a Actor) valid() bool { return a.Person != (a.Bot != "") }

// Event is one accepted write.
type Event struct {
	Kind   string    `json:"kind"`
	Actor  string    `json:"actor"`
	Detail string    `json:"detail,omitempty"`
	At     time.Time `json:"at"`
}

// Action names a row of the authority table (§7.3).
type Action string

const (
	ActEdit           Action = "edit"
	ActHandoffChain   Action = "handoff_chain"
	ActHandoffOutside Action = "handoff_outside"
	ActVerdict        Action = "verdict"
	ActMark           Action = "mark"
	ActWait           Action = "wait"
	ActDone           Action = "done"
	ActCancelReopen   Action = "cancel_reopen"
)

// Authorize applies one row of §7.3. Read is open to every caller with
// machine read authority and is not checked here.
func Authorize(c Card, a Actor, act Action) error {
	if !a.valid() {
		return refuse(CodeForbidden, "actor")
	}
	if a.Person {
		switch act {
		case ActVerdict, ActWait:
			return refuse(CodeForbidden, string(act))
		}
		return nil
	}
	holder := c.Baton.Bot == a.Bot
	creator := c.isAuthorityBot(a.Bot)
	ok := false
	switch act {
	case ActEdit, ActHandoffChain, ActHandoffOutside:
		ok = holder || creator
	case ActVerdict:
		ok = holder && c.Baton.Role == RoleReviewer
	case ActMark, ActWait, ActDone:
		ok = holder
	case ActCancelReopen:
		ok = false
	}
	if !ok {
		if act == ActHandoffOutside {
			return &Refusal{Code: CodeForbidden, Field: string(act),
				Reason: "Only the holder, creating Bot or an authority Bot can hand the card to a new Bot. Ask one of them to hand it off. 只有持棒者、開卡 Bot 或交辦來源的 Bot 能交棒給新成員。請其中一位交棒。"}
		}
		return refuse(CodeForbidden, string(act))
	}
	return nil
}

func (c Card) isAuthorityBot(bot string) bool {
	if c.CreatedBy == bot {
		return true
	}
	for _, x := range c.Authorities {
		if x.Bot == bot {
			return true
		}
	}
	return false
}

// InChain reports whether bot has been reached by this card.
func (c Card) InChain(bot string) bool {
	for _, b := range c.Chain {
		if b == bot {
			return true
		}
	}
	return false
}

// CardID derives a card's id from the person's message it roots in (§7.1),
// so two creates of one request through two Bots meet on one card. It is a
// lowercase UUID-shaped string from SHA-256 (version 8 layout).
func CardID(machine, rootBot, rootConversation, rootMessage string) string {
	h := sha256.Sum256([]byte("bot-request-card\x00" + machine + "\x00" + rootBot + "\x00" + rootConversation + "\x00" + rootMessage))
	b := h[:16]
	b[6] = (b[6] & 0x0f) | 0x80
	b[8] = (b[8] & 0x3f) | 0x80
	x := hex.EncodeToString(b)
	return x[0:8] + "-" + x[8:12] + "-" + x[12:16] + "-" + x[16:20] + "-" + x[20:32]
}

// DefaultChecklist answers the default labels for a known kind (§3.3) and nil
// for any other.
func DefaultChecklist(kind string) []string {
	switch kind {
	case "development":
		return []string{"main", "GitHub"}
	case "publishing":
		return []string{"draft", "reviewed by the person", "published"}
	}
	return nil
}

// EntryKey derives a checklist key from its label. It means nothing to the
// daemon beyond matching entries by key.
func EntryKey(label string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(strings.TrimSpace(label)) {
		if r == ' ' || r == '-' || r == '_' || r == '/' {
			if !dash && b.Len() > 0 {
				b.WriteByte('-')
				dash = true
			}
			continue
		}
		b.WriteRune(r)
		dash = false
	}
	return strings.TrimSuffix(b.String(), "-")
}

func checkText(field, s string, required bool) error {
	if !utf8.ValidString(s) {
		return refuse(CodeInvalid, field)
	}
	if required && strings.TrimSpace(s) == "" {
		return refuse(CodeInvalid, field)
	}
	return nil
}

// CreateInput is bot_request_create's arguments.
type CreateInput struct {
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Kind        string   `json:"kind,omitempty"`
	Checklist   []string `json:"checklist,omitempty"`
	BoardItemID string   `json:"board_item_id,omitempty"`
}

// NewCard builds a new card for bot in a turn rooted in authority. The caller
// has already proved that the turn roots in a person (§7.1).
func NewCard(id, projectID, projectPath, bot string, authority Authority, in CreateInput, now time.Time) (Card, error) {
	if bot == "" || authority.MessageID == "" {
		return Card{}, refuse(CodeNoAuthority, "authority")
	}
	if err := checkText("title", in.Title, true); err != nil {
		return Card{}, err
	}
	if err := checkText("kind", in.Kind, false); err != nil {
		return Card{}, err
	}
	if err := checkText("description", in.Description, false); err != nil {
		return Card{}, err
	}
	labels := in.Checklist
	if len(labels) == 0 {
		labels = DefaultChecklist(in.Kind)
	}
	if len(labels) == 0 {
		return Card{}, refuse(CodeChecklistEmpty, "checklist")
	}
	c := Card{
		ID: id, ProjectID: projectID, ProjectPath: projectPath, Kind: in.Kind,
		Title: in.Title, Description: in.Description,
		Authorities: []Authority{authority}, CreatedBy: bot,
		Baton: Baton{Role: RoleMaker, Bot: bot}, Chain: []string{bot},
		BoardItemID: in.BoardItemID, State: StateOpen, Cycle: 1, Version: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	for _, l := range labels {
		if err := c.addEntry(l); err != nil {
			return Card{}, err
		}
	}
	return c, nil
}

func (c *Card) addEntry(label string) error {
	if err := checkText("checklist", label, true); err != nil {
		return err
	}
	key := EntryKey(label)
	if key == "" {
		return refuse(CodeInvalid, "checklist")
	}
	for _, e := range c.Checklist {
		if e.Key == key {
			return nil
		}
	}
	c.Checklist = append(c.Checklist, Entry{Key: key, Label: strings.TrimSpace(label), State: EntryOpen})
	return nil
}

// JoinChain adds bot to the card's chain. It answers whether the chain changed.
func (c *Card) JoinChain(bot string) bool {
	if bot == "" || c.InChain(bot) {
		return false
	}
	c.Chain = append(c.Chain, bot)
	return true
}

func (c Card) requireOpen() error {
	switch c.State {
	case StateOpen:
		return nil
	case StateMerged:
		return refuse(CodeAlreadyMerged, "state")
	}
	return refuse(CodeClosed, "state")
}

// Handoff moves the baton and adds the recipient to the chain (§7.2).
// The Bot tool host checks the recipient's eligibility in the card's Project.
func (c *Card) Handoff(a Actor, toBot, role, note string) error {
	if err := c.requireOpen(); err != nil {
		return err
	}
	if toBot == "" {
		return refuse(CodeInvalid, "to_bot")
	}
	switch role {
	case RoleMaker, RoleReviewer, RoleIntegrator:
	default:
		return refuse(CodeInvalid, "role")
	}
	if err := checkText("note", note, false); err != nil {
		return err
	}
	act := ActHandoffChain
	if !c.InChain(toBot) {
		act = ActHandoffOutside
	}
	if err := Authorize(*c, a, act); err != nil {
		return err
	}
	c.JoinChain(toBot)
	c.Baton = Baton{Role: role, Bot: toBot}
	return nil
}

// Mark is one entry of a bot_request_check call: add the entry when it is new,
// then set its state when one is given.
type Mark struct {
	Label string `json:"label"`
	State string `json:"state,omitempty"`
	Note  string `json:"note,omitempty"`
}

// Check applies marks all-or-nothing and answers whether anything changed;
// marks that repeat an entry's current state and note change nothing, so a
// retried batch is a no-op.
func (c *Card) Check(a Actor, marks []Mark, now time.Time) (bool, error) {
	if err := c.requireOpen(); err != nil {
		return false, err
	}
	if len(marks) == 0 {
		return false, refuse(CodeInvalid, "entries")
	}
	next := *c
	next.Checklist = append([]Entry(nil), c.Checklist...)
	changed := false
	for _, m := range marks {
		key := EntryKey(m.Label)
		idx := next.entry(key)
		if idx < 0 {
			if err := Authorize(next, a, ActEdit); err != nil {
				return false, err
			}
			if err := next.addEntry(m.Label); err != nil {
				return false, err
			}
			idx = len(next.Checklist) - 1
			changed = true
		}
		if m.State == "" {
			continue
		}
		if err := Authorize(next, a, ActMark); err != nil {
			return false, err
		}
		switch m.State {
		case EntryOpen, EntryDone:
		case EntryNotNeeded:
			if strings.TrimSpace(m.Note) == "" {
				return false, refuse(CodeInvalid, "note")
			}
		default:
			return false, refuse(CodeInvalid, "state")
		}
		if err := checkText("note", m.Note, false); err != nil {
			return false, err
		}
		e := &next.Checklist[idx]
		if e.State == m.State && e.Note == m.Note {
			continue
		}
		e.State, e.Note = m.State, m.Note
		if m.State == EntryOpen {
			e.ReportedBy, e.ReportedAt = "", time.Time{}
		} else {
			e.ReportedBy, e.ReportedAt = a.Name(), now
		}
		changed = true
	}
	if changed {
		*c = next
	}
	return changed, nil
}

// CheckEvents applies marks as Check does and answers the card's events for
// the write (§4): checklist.added with its label for each entry the marks
// added, then one checklist.marked when an entry's state or note changed.
// No change answers no events.
func (c *Card) CheckEvents(a Actor, marks []Mark, now time.Time) ([]Event, error) {
	before := make(map[string]Entry, len(c.Checklist))
	for _, e := range c.Checklist {
		before[e.Key] = e
	}
	changed, err := c.Check(a, marks, now)
	if err != nil || !changed {
		return nil, err
	}
	var out []Event
	marked := false
	for _, e := range c.Checklist {
		prev, had := before[e.Key]
		if !had {
			out = append(out, Event{Kind: EventChecklistAdded, Detail: e.Label})
			marked = marked || e.State != EntryOpen || e.Note != ""
			continue
		}
		marked = marked || prev.State != e.State || prev.Note != e.Note
	}
	if marked {
		out = append(out, Event{Kind: EventChecklistMark})
	}
	return out, nil
}

func (c Card) entry(key string) int {
	for i, e := range c.Checklist {
		if e.Key == key {
			return i
		}
	}
	return -1
}

// RecordVerdict appends a verdict; it changes nothing else (§5). commit is
// already resolved to a full object id by the caller, or empty.
func (c *Card) RecordVerdict(a Actor, id, commit, verdict, text string, now time.Time) error {
	if err := c.requireOpen(); err != nil {
		return err
	}
	if err := Authorize(*c, a, ActVerdict); err != nil {
		return err
	}
	switch verdict {
	case VerdictPass:
	case VerdictConditionalPass, VerdictReject:
		if strings.TrimSpace(text) == "" {
			return refuse(CodeInvalid, "text")
		}
	default:
		return refuse(CodeInvalid, "verdict")
	}
	if err := checkText("text", text, false); err != nil {
		return err
	}
	c.Verdicts = append(c.Verdicts, Verdict{ID: id, Commit: commit, Verdict: verdict, Text: text, ReviewerBot: a.Bot, At: now})
	return nil
}

// SetWaiting sets or clears the ask_person question the holder waits on.
func (c *Card) SetWaiting(a Actor, questionID string) (bool, error) {
	if err := c.requireOpen(); err != nil {
		return false, err
	}
	// The person only clears it, by answering; setting it is the holder's.
	if !a.Person || questionID != "" {
		if err := Authorize(*c, a, ActWait); err != nil {
			return false, err
		}
	}
	if err := checkText("question_id", questionID, false); err != nil {
		return false, err
	}
	if c.Waiting == questionID {
		return false, nil
	}
	c.Waiting = questionID
	return true, nil
}

// Done completes the card. A Bot needs a complete checklist; the person may
// complete at any time. It answers false when the card is already done, so a
// retry writes nothing and notifies nobody again.
func (c *Card) Done(a Actor, now time.Time) (bool, error) {
	if c.State == StateDone {
		return false, nil
	}
	if err := c.requireOpen(); err != nil {
		return false, err
	}
	if err := Authorize(*c, a, ActDone); err != nil {
		return false, err
	}
	if !a.Person {
		for _, e := range c.Checklist {
			if e.State == EntryOpen {
				return false, refuse(CodeChecklistOpen, e.Key)
			}
		}
	}
	c.State, c.Waiting, c.ClosedAt = StateDone, "", now
	return true, nil
}

// Cancel closes an open card for the person.
func (c *Card) Cancel(a Actor, now time.Time) (bool, error) {
	if c.State == StateCancelled {
		return false, nil
	}
	if err := c.requireOpen(); err != nil {
		return false, err
	}
	if err := Authorize(*c, a, ActCancelReopen); err != nil {
		return false, err
	}
	c.State, c.Waiting, c.ClosedAt = StateCancelled, "", now
	return true, nil
}

// Reopen opens a done or cancelled card for the person and starts a new
// completion cycle, so its next completion notifies again.
func (c *Card) Reopen(a Actor) (bool, error) {
	if err := Authorize(*c, a, ActCancelReopen); err != nil {
		return false, err
	}
	switch c.State {
	case StateOpen:
		return false, nil
	case StateMerged:
		return false, refuse(CodeAlreadyMerged, "state")
	}
	if c.State == StateDone {
		c.Cycle++
	}
	c.State, c.ClosedAt = StateOpen, time.Time{}
	return true, nil
}

// Progress answers the derived "n/m ✓": entries done or not needed, of all.
func (c Card) Progress() (done, total int) {
	for _, e := range c.Checklist {
		if e.State != EntryOpen {
			done++
		}
	}
	return done, len(c.Checklist)
}

// ProgressState answers the row's state label (§9.1).
func (c Card) ProgressState() string {
	switch c.State {
	case StateDone:
		return ProgressDone
	case StateCancelled:
		return ProgressCancelled
	case StateMerged:
		return ProgressMerged
	}
	if c.Waiting != "" {
		return ProgressWaiting
	}
	return ProgressInProgress
}

// NotifyKey is the outbox effect key of one completion (§4.1).
func NotifyKey(cardID string, cycle int64) string {
	return "bot-request-done-" + cardID + "-" + itoa(cycle)
}

// NotifyTargets answers the Bots told of a completion: every authority Bot
// and the creating Bot, without the Bot that marked it done (§5).
func (c Card) NotifyTargets(doneBy Actor) []string {
	var out []string
	seen := map[string]bool{}
	add := func(b string) {
		if b == "" || seen[b] || (!doneBy.Person && b == doneBy.Bot) {
			return
		}
		seen[b] = true
		out = append(out, b)
	}
	for _, x := range c.Authorities {
		add(x.Bot)
	}
	add(c.CreatedBy)
	return out
}

// HandoffDispatch decides whether a turn woken by a handoff notice for
// noticeCard may dispatch work or message a Bot naming namedCard (§7.2). The
// turn's authority is the card's: it grants nothing outside that card.
func HandoffDispatch(c Card, noticeCard, bot, namedCard string) error {
	if namedCard == "" || namedCard != noticeCard || c.ID != noticeCard {
		return refuse(CodeHandoffScope, "bot_request_id")
	}
	if c.State != StateOpen {
		return refuse(CodeHandoffScope, "state")
	}
	if len(c.Authorities) == 0 || c.Authorities[0].MessageID == "" {
		return refuse(CodeNoAuthority, "authority")
	}
	if c.Baton.Bot != bot {
		return refuse(CodeHandoffScope, "holder")
	}
	return nil
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
