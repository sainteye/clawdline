package projectassistant

import (
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// A Group is a room several Bots of one Project share with the person, as in
// Codync's group chat. It is not a Bot: it has no Agent, backend, folder or
// native session. Each member answers in its own conversation under its own
// execution fence; the room only decides who speaks and whether an answer is
// still wanted. Key is source-qualified like a BotKey and never equals a Bot's.
type Group struct {
	Key            BotKey        `json:"key"`
	Project        ProjectScope  `json:"project"`
	ConversationID string        `json:"conversation_id"`
	Name           string        `json:"name"`
	Members        []GroupMember `json:"members"`
	// MembershipRevision moves whenever the member set changes. A member
	// added at revision n carries JoinedRevision n, so leaving and joining
	// again invalidates what was asked of it before it left.
	MembershipRevision int64     `json:"membership_revision"`
	Version            int64     `json:"version"`
	Deleted            bool      `json:"deleted,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type GroupMember struct {
	Key            BotKey `json:"key"`
	JoinedRevision int64  `json:"joined_revision"`
}

const DefaultMaxGroupMembers = 64

// GroupLimits are supplied by the caller that registers the product bounds.
// Upstream has no member maximum; MaxMembers can be dynamically defaulted to
// DefaultMaxGroupMembers when omitted or non-positive.
type GroupLimits struct {
	MaxMembers   int `json:"max_members"`
	MaxRounds    int `json:"max_rounds"`
	MaxReplies   int `json:"max_replies"`
	MaxLateTurns int `json:"max_late_turns"`
}

func (l GroupLimits) Validate() error {
	if l.MaxRounds < 1 || l.MaxReplies < 1 || l.MaxLateTurns < 1 {
		return fail(Invalid, "group_limits")
	}
	return nil
}

func (l GroupLimits) EffectiveMaxMembers() int {
	if l.MaxMembers > 0 {
		return l.MaxMembers
	}
	return DefaultMaxGroupMembers
}

// GroupCatalog is what the source machine actually stores, read in the same
// transaction as the group write: live Bots by key, and every key a group
// (live or deleted) uses. A Bot that was deleted is simply absent.
type GroupCatalog struct {
	Bots   map[BotKey]Bot
	Groups map[BotKey]bool
}

func (g Group) MemberKeys() []BotKey {
	out := make([]BotKey, len(g.Members))
	for i, m := range g.Members {
		out[i] = m.Key
	}
	return out
}

func (g Group) Member(key BotKey) (GroupMember, bool) {
	for _, m := range g.Members {
		if m.Key == key {
			return m, true
		}
	}
	return GroupMember{}, false
}

// DeduplicateGroupMembers returns requested members with duplicate keys
// silently dropped, preserving first appearance order (aligning with Codync host/src/chat/group.rs).
func DeduplicateGroupMembers(members []BotKey) []BotKey {
	if len(members) == 0 {
		return nil
	}
	seen := make(map[BotKey]bool, len(members))
	out := make([]BotKey, 0, len(members))
	for _, k := range members {
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}

// ValidateGroupMembers checks a requested member list against the stored
// Bots. In accordance with Codync (host/src/chat/group.rs), duplicate member
// keys are deduplicated silently while preserving first appearance order.
func ValidateGroupMembers(group BotKey, scope ProjectScope, requested []BotKey, catalog GroupCatalog, limits GroupLimits) error {
	if err := limits.Validate(); err != nil {
		return err
	}
	if err := ValidateScope(scope); err != nil {
		return err
	}
	if group.MachineID != scope.MachineID {
		return fail(ScopeDenied, "group_machine")
	}
	deduped := DeduplicateGroupMembers(requested)
	if len(deduped) == 0 {
		return fail(Invalid, "group_members")
	}
	if len(deduped) > limits.EffectiveMaxMembers() {
		return fail(CapacityFull, "group_members")
	}
	for _, key := range deduped {
		switch {
		case key.MachineID == "" || key.BotID == "":
			return fail(Invalid, "group_member")
		case key.MachineID != scope.MachineID:
			return fail(ScopeDenied, "group_member_machine")
		case key == group:
			return fail(Invalid, "group_member_self")
		case catalog.Groups[key]:
			return fail(Invalid, "group_member_nested")
		}
		bot, ok := catalog.Bots[key]
		if !ok {
			return fail(NotFound, "group_member")
		}
		if bot.Key != key {
			return fail(Invalid, "group_member")
		}
		// The full scope: the same canonical repository under another
		// registered Project is a different Project.
		if bot.Project != scope {
			return fail(ScopeDenied, "group_member_project")
		}
		if !bot.Agent.Valid() {
			return fail(Invalid, "group_member_agent")
		}
	}
	return nil
}

func validGroupName(name string) error {
	if !utf8.ValidString(name) || strings.TrimSpace(name) == "" || utf8.RuneCountInString(name) > MaxNameRunes {
		return fail(Invalid, "group_name")
	}
	return nil
}

// GroupDraft is a new group as the host builds it: the key and conversation
// are host-generated, the Project is the resolved full scope.
type GroupDraft struct {
	Key            BotKey       `json:"key"`
	Project        ProjectScope `json:"project"`
	ConversationID string       `json:"conversation_id"`
	Name           string       `json:"name"`
	Members        []BotKey     `json:"members"`
}

func NewGroup(d GroupDraft, catalog GroupCatalog, limits GroupLimits, at time.Time) (Group, error) {
	if d.Key.BotID == "" || d.ConversationID == "" {
		return Group{}, fail(Invalid, "group")
	}
	if _, bot := catalog.Bots[d.Key]; bot || catalog.Groups[d.Key] {
		return Group{}, fail(Invalid, "group_key")
	}
	if err := validGroupName(d.Name); err != nil {
		return Group{}, err
	}
	if err := ValidateGroupMembers(d.Key, d.Project, d.Members, catalog, limits); err != nil {
		return Group{}, err
	}
	deduped := DeduplicateGroupMembers(d.Members)
	g := Group{Key: d.Key, Project: d.Project, ConversationID: d.ConversationID, Name: d.Name, MembershipRevision: 1, Version: 1, CreatedAt: at, UpdatedAt: at}
	for _, k := range deduped {
		g.Members = append(g.Members, GroupMember{Key: k, JoinedRevision: 1})
	}
	return g, nil
}

// GroupEdit replaces the name and the whole member list.
type GroupEdit struct {
	ExpectedVersion int64    `json:"expected_version"`
	Name            string   `json:"name"`
	Members         []BotKey `json:"members"`
}

func EditGroup(g Group, e GroupEdit, catalog GroupCatalog, limits GroupLimits, at time.Time) (Group, error) {
	if g.Deleted {
		return g, fail(NotFound, "group")
	}
	if e.ExpectedVersion != g.Version {
		return g, fail(StaleVersion, "group")
	}
	if err := validGroupName(e.Name); err != nil {
		return g, err
	}
	if err := ValidateGroupMembers(g.Key, g.Project, e.Members, catalog, limits); err != nil {
		return g, err
	}
	deduped := DeduplicateGroupMembers(e.Members)
	next := g
	next.Name, next.Version, next.UpdatedAt = e.Name, g.Version+1, at
	next.Members = nil
	if !SameMembers(g.MemberKeys(), deduped) {
		next.MembershipRevision = g.MembershipRevision + 1
	}
	for _, k := range deduped {
		m, kept := g.Member(k)
		if !kept {
			m = GroupMember{Key: k, JoinedRevision: next.MembershipRevision}
		}
		next.Members = append(next.Members, m)
	}
	return next, nil
}

// LeaveGroup takes a deleted Bot out of a group; what it said stays in the
// room. As upstream, the group may be left with no members: it then has
// nobody to ask and StartRoomTurn refuses.
func LeaveGroup(g Group, member BotKey, at time.Time) (Group, bool) {
	if g.Deleted {
		return g, false
	}
	if _, ok := g.Member(member); !ok {
		return g, false
	}
	next := g
	next.Members = nil
	for _, m := range g.Members {
		if m.Key != member {
			next.Members = append(next.Members, m)
		}
	}
	next.MembershipRevision++
	next.Version++
	next.UpdatedAt = at
	return next, true
}

// DeleteGroup tombstones the room. Member Bots, their conversations and what
// they said elsewhere are untouched.
func DeleteGroup(g Group, expectedVersion int64, at time.Time) (Group, error) {
	if g.Deleted {
		return g, fail(NotFound, "group")
	}
	if expectedVersion != g.Version {
		return g, fail(StaleVersion, "group")
	}
	g.Deleted, g.Version, g.UpdatedAt = true, g.Version+1, at
	g.MembershipRevision++
	return g, nil
}

// SameMembers compares member sets regardless of order: creating a group of
// the same Bots again opens the one they already share (Codync's rule).
func SameMembers(a, b []BotKey) bool {
	if len(a) != len(b) {
		return false
	}
	x, y := slices.Clone(a), slices.Clone(b)
	less := func(p, q BotKey) int {
		if c := strings.Compare(p.MachineID, q.MachineID); c != 0 {
			return c
		}
		return strings.Compare(p.BotID, q.BotID)
	}
	slices.SortFunc(x, less)
	slices.SortFunc(y, less)
	return slices.Equal(x, y)
}

// GroupWithMembers is the live group of the same full Project whose members
// are exactly members, if any.
func GroupWithMembers(groups []Group, scope ProjectScope, members []BotKey) (Group, bool) {
	for _, g := range groups {
		if !g.Deleted && g.Project == scope && SameMembers(g.MemberKeys(), members) {
			return g, true
		}
	}
	return Group{}, false
}

// RoomState is one group's room-turn coordination, persisted so a stale
// answer is refused after a restart too. Revision is the CAS token; Epoch
// moves with every room turn and with Stop.
type RoomState struct {
	Group     BotKey `json:"group"`
	Revision  int64  `json:"revision"`
	Epoch     int64  `json:"epoch"`
	Running   int64  `json:"running,omitempty"`
	Pending   int64  `json:"pending,omitempty"`
	LateTurns int    `json:"late_turns"`
	// StoppedAt is the epoch the last group Stop moved to: a late answer
	// asked before it is dropped.
	StoppedAt int64 `json:"stopped_at,omitempty"`
	// Overdue are asks the room stopped waiting for (Codync's
	// MEMBER_TIMEOUT): their answers still land, as late ones. At most one
	// per member: asking it again replaces its entry.
	Overdue []RoomTicket `json:"overdue,omitempty"`
	// Publications are the current round's successful room publication
	// receipts, one per member. A zero sequence means completion was quiet.
	Publications []RoomMemberPublication `json:"publications,omitempty"`
}

type RoomMemberPublication struct {
	Member         BotKey `json:"member"`
	JoinedRevision int64  `json:"joined_revision"`
	TurnID         string `json:"turn_id"`
	ReplySeq       int64  `json:"reply_seq,omitempty"`
}

// RecordRoomPublication is called only after the terminal transaction checks
// the ticket and determines whether it will write a room line.
func RecordRoomPublication(g Group, s RoomState, t RoomTicket, turnID string, replySeq int64) RoomState {
	if t.Epoch != s.Epoch {
		return s
	}
	out := slices.DeleteFunc(slices.Clone(s.Publications), func(p RoomMemberPublication) bool {
		member, ok := g.Member(p.Member)
		return !ok || member.JoinedRevision != p.JoinedRevision || t.Member == p.Member
	})
	s.Publications = append(out, RoomMemberPublication{Member: t.Member, JoinedRevision: t.JoinedRevision, TurnID: turnID, ReplySeq: replySeq})
	s.Revision++
	return s
}

// MarkOverdue: the room stops waiting for t; its answer may still land.
func MarkOverdue(s RoomState, t RoomTicket) RoomState {
	t.Late = true
	s.Overdue = AskAgain(s.Overdue, t)
	s.Revision++
	return s
}

func (s RoomState) overdue(t RoomTicket) (int, bool) {
	for i, o := range s.Overdue {
		if o.Group == t.Group && o.Member == t.Member && o.Epoch == t.Epoch && o.JoinedRevision == t.JoinedRevision {
			return i, true
		}
	}
	return -1, false
}

// Landed removes a published answer's overdue entry, if any.
func Landed(s RoomState, t RoomTicket) (RoomState, bool) {
	i, ok := s.overdue(t)
	if !ok {
		return s, false
	}
	s.Overdue = slices.Delete(slices.Clone(s.Overdue), i, i+1)
	s.Revision++
	return s, true
}

// RoomTurn is a room turn to run. Late is the seq of the oldest late answer
// it follows up, or zero when the person's message started it.
type RoomTurn struct {
	Epoch int64 `json:"epoch"`
	Late  int64 `json:"late,omitempty"`
}

func (s RoomState) begin(late int64) (RoomState, RoomTurn) {
	s.Epoch++
	s.Running = s.Epoch
	s.Publications = nil
	s.Revision++
	return s, RoomTurn{Epoch: s.Epoch, Late: late}
}

func (s RoomState) followUp(l GroupLimits) (RoomState, *RoomTurn) {
	late := s.Pending
	if late == 0 {
		return s, nil
	}
	s.Pending = 0
	s.Revision++
	if s.LateTurns >= l.MaxLateTurns {
		return s, nil
	}
	s.LateTurns++
	next, t := s.begin(late)
	return next, &t
}

// StartRoomTurn: the person wrote in the room. The new turn replaces the one
// running, clears late answers and restores the late-turn allowance.
func StartRoomTurn(g Group, s RoomState) (RoomState, RoomTurn, error) {
	if g.Deleted {
		return s, RoomTurn{}, fail(NotFound, "group")
	}
	if len(g.Members) == 0 {
		return s, RoomTurn{}, fail(Invalid, "group_members")
	}
	s.Group = g.Key
	s.Pending, s.LateTurns = 0, 0
	next, t := s.begin(0)
	return next, t, nil
}

// LateAnswer: a member's answer the room stopped waiting for landed at seq and
// names someone. It runs now, waits for the running turn, or is not allowed.
func LateAnswer(s RoomState, seq int64, l GroupLimits) (RoomState, *RoomTurn, error) {
	if err := l.Validate(); err != nil {
		return s, nil, err
	}
	if seq < 1 {
		return s, nil, fail(Invalid, "room_seq")
	}
	if s.Pending == 0 || seq < s.Pending {
		s.Pending = seq
	}
	s.Revision++
	if s.Running != 0 {
		return s, nil, nil
	}
	next, t := s.followUp(l)
	return next, t, nil
}

// RoomTurnEnded: epoch's turn ended; an older turn ending changes nothing.
func RoomTurnEnded(s RoomState, epoch int64, l GroupLimits) (RoomState, *RoomTurn, error) {
	if err := l.Validate(); err != nil {
		return s, nil, err
	}
	if s.Running == 0 || s.Running != epoch {
		return s, nil, nil
	}
	s.Running = 0
	s.Revision++
	next, t := s.followUp(l)
	return next, t, nil
}

// StopRoom is the group's Stop: it ends the room turn and late answers start
// nothing until the person writes again. False when no room turn ever ran.
func StopRoom(s RoomState, l GroupLimits) (RoomState, bool, error) {
	if err := l.Validate(); err != nil {
		return s, false, err
	}
	if s.Epoch == 0 {
		return s, false, nil
	}
	s.Epoch++
	s.StoppedAt = s.Epoch
	s.Pending = 0
	s.Overdue = nil
	s.Publications = nil
	s.LateTurns = l.MaxLateTurns
	s.Revision++
	return s, true, nil
}

func RoomTurnCurrent(s RoomState, epoch int64) bool { return epoch != 0 && s.Epoch == epoch }

// RoomTicket is what a member's queued room turn carries: the host-derived
// member, the room turn that asked, and its membership at the time.
type RoomTicket struct {
	Group          BotKey `json:"group"`
	Member         BotKey `json:"member"`
	Epoch          int64  `json:"epoch"`
	JoinedRevision int64  `json:"joined_revision"`
	// Late: the member was busy elsewhere or overdue, so it answers once
	// free whatever the room did since. Only the group's Stop, the room
	// asking it again, or it leaving the group drops it.
	Late bool `json:"late,omitempty"`
}

// CheckRoomTicket is checked before a member's room turn runs and again
// before its answer is published. The Bot's own fence is checked separately
// by its ordinary turn path; a room ticket never replaces it.
func CheckRoomTicket(g Group, s RoomState, t RoomTicket) error {
	if t.Group != g.Key || s.Group != g.Key {
		return fail(ScopeDenied, "room_ticket")
	}
	if g.Deleted {
		return fail(NotFound, "group")
	}
	m, ok := g.Member(t.Member)
	if !ok || m.JoinedRevision != t.JoinedRevision {
		return fail(Revoked, "group_member")
	}
	if t.Epoch < 1 || t.Epoch > s.Epoch {
		return fail(Invalid, "room_epoch")
	}
	if _, ok := s.overdue(t); ok {
		t.Late = true
	}
	if t.Late {
		if s.StoppedAt != 0 && t.Epoch < s.StoppedAt {
			return fail(StaleGeneration, "room_stopped")
		}
		return nil
	}
	if !RoomTurnCurrent(s, t.Epoch) {
		return fail(StaleGeneration, "room_epoch")
	}
	return nil
}

// TicketFor is the ticket the room hands a member it asks now.
func TicketFor(g Group, s RoomState, member BotKey, late bool) (RoomTicket, error) {
	m, ok := g.Member(member)
	if !ok || g.Deleted {
		return RoomTicket{}, fail(Revoked, "group_member")
	}
	t := RoomTicket{Group: g.Key, Member: member, Epoch: s.Epoch, JoinedRevision: m.JoinedRevision, Late: late}
	return t, CheckRoomTicket(g, s, t)
}

// QueuedRoomTickets are a member's waiting room turns after something
// happened. Asking again replaces its turn in that room; the member's own-chat
// Stop keeps only late ones; the group's Stop drops every one for that group.
func AskAgain(queue []RoomTicket, next RoomTicket) []RoomTicket {
	out := slices.DeleteFunc(slices.Clone(queue), func(t RoomTicket) bool { return t.Group == next.Group && t.Member == next.Member })
	return append(out, next)
}
func AfterOwnChatStop(queue []RoomTicket) []RoomTicket {
	return slices.DeleteFunc(slices.Clone(queue), func(t RoomTicket) bool { return !t.Late })
}
func AfterGroupStop(queue []RoomTicket, group BotKey) []RoomTicket {
	return slices.DeleteFunc(slices.Clone(queue), func(t RoomTicket) bool { return t.Group == group })
}

// RoomEntryKind distinguishes the person's lines from members' answers.
type RoomEntryKind string

const (
	RoomPersonEntry RoomEntryKind = "person"
	RoomMemberEntry RoomEntryKind = "member"
)

// RoomEntry is one line in the room. Author is set by the host from the
// ticket that produced it, never from text a model wrote.
type RoomEntry struct {
	Seq  int64         `json:"seq"`
	Kind RoomEntryKind `json:"kind"`
	At   time.Time     `json:"at,omitzero"`
	// Author is the member for a member entry; zero for the person.
	Author BotKey `json:"author,omitzero"`
	Text   string `json:"text"`
	// ReplyToAuthor is the stored author of the message the person quoted.
	ReplyToAuthor BotKey `json:"reply_to_author,omitzero"`
	// Quote is captured from this room's stored line by the Host. It is not
	// supplied by the client and is kept apart from the person's own text.
	Quote *RoomQuote `json:"quote,omitempty"`
}

type RoomQuote struct {
	Seq       int64         `json:"seq"`
	Kind      RoomEntryKind `json:"kind"`
	Author    BotKey        `json:"author,omitzero"`
	Text      string        `json:"text"`
	Truncated bool          `json:"truncated,omitempty"`
}

// GroupPreview is a read projection, never part of the group's write intent.
type GroupPreview struct {
	Group     Group
	Room      *RoomState
	LastEntry *RoomEntry
}

type RoomActivity struct {
	Member BotKey    `json:"member"`
	TurnID string    `json:"turn_id"`
	State  TurnState `json:"state"`
}

type RoomMemberOutcome struct {
	Member       BotKey        `json:"member"`
	Name         string        `json:"name"`
	TurnID       string        `json:"turn_id"`
	State        TurnState     `json:"state"`
	Disposition  string        `json:"disposition,omitempty"`
	ReplySeq     int64         `json:"reply_seq,omitempty"`
	Late         bool          `json:"late,omitempty"`
	FailureCode  ErrorCode     `json:"failure_code,omitempty"`
	FailureField string        `json:"failure_field,omitempty"`
	Attempts     []RoomAttempt `json:"attempts,omitempty"`
}

type RoomAttempt struct {
	Ordinal           int          `json:"ordinal"`
	Agent             Agent        `json:"agent"`
	Stage             AttemptStage `json:"stage"`
	Reason            string       `json:"reason,omitempty"`
	Fallback          bool         `json:"fallback,omitempty"`
	PrimaryRestReason string       `json:"primary_rest_reason,omitempty"`
}

// RoomTicketIsLate describes when the room stopped waiting, independently
// of whether the member can still publish a reply.
func (s RoomState) RoomTicketIsLate(t RoomTicket) bool {
	_, overdue := s.overdue(t)
	return t.Late || overdue
}

func (s RoomState) PublicationFor(t RoomTicket, turnID string) (RoomMemberPublication, bool) {
	if s.Epoch == t.Epoch {
		for _, p := range s.Publications {
			if p.Member == t.Member && p.JoinedRevision == t.JoinedRevision && p.TurnID == turnID {
				return p, true
			}
		}
	}
	return RoomMemberPublication{}, false
}

// RoomSpeaker is a member as the room names it: its key and current name,
// read from its stored Bot.
type RoomSpeaker struct {
	Key  BotKey
	Name string
}

// RoomSpeakers returns live member names from the catalog, in member order;
// a member whose Bot is gone is skipped, as upstream's members() does.
func RoomSpeakers(g Group, catalog GroupCatalog) []RoomSpeaker {
	var out []RoomSpeaker
	for _, m := range g.Members {
		if b, ok := catalog.Bots[m.Key]; ok {
			out = append(out, RoomSpeaker{Key: m.Key, Name: b.Name})
		}
	}
	return out
}

// PublishRoomAnswer turns a member's final text into the room line it
// publishes, after the ticket is checked against current state. A pass
// publishes nothing (ok false).
func PublishRoomAnswer(g Group, s RoomState, t RoomTicket, seq int64, text string) (RoomEntry, bool, error) {
	if err := CheckRoomTicket(g, s, t); err != nil {
		return RoomEntry{}, false, err
	}
	if IsRoomPass(text) {
		return RoomEntry{}, false, nil
	}
	return RoomEntry{Seq: seq, Kind: RoomMemberEntry, Author: t.Member, Text: text}, true, nil
}

const RoomPass = "(pass)"

// RoomNeedsResponse assigns conversational responsibility to the next eligible
// participant until someone has actually answered the person's latest message.
// A missing publication is not proof of a pass: it may also be a failed,
// unauthorized, busy or overdue turn. Those terminal facts remain on the member.
func RoomNeedsResponse(lines []RoomEntry) bool {
	for i := len(lines) - 1; i >= 0; i-- {
		switch lines[i].Kind {
		case RoomMemberEntry:
			return false
		case RoomPersonEntry:
			return true
		}
	}
	return false
}

// IsRoomPass: a member's final text that the room does not see.
func IsRoomPass(text string) bool {
	t := strings.ToLower(strings.Trim(strings.TrimSpace(text), `".*`))
	return t == "" || t == RoomPass || t == "pass"
}

// RoomSpeakersFor is who speaks in round, rotated one member later each
// round. Round 0 of the person's turn asks who their message names (or
// everyone); later rounds and late turns ask only members another member
// @-mentioned since and who have not spoken or been asked since. since holds
// the room's lines from the person's last message on (or, for a late turn,
// from the oldest late answer on); asked is the newest seq each member was
// shown when last asked.
func RoomSpeakersFor(members []RoomSpeaker, since []RoomEntry, late bool, round int, asked map[BotKey]int64) []RoomSpeaker {
	var out []RoomSpeaker
	if !late && round == 0 {
		out = roomResponders(members, since)
	} else {
		out = roomCited(members, since, asked)
	}
	if len(out) > 0 {
		k := round % len(out)
		out = append(out[k:], out[:k]...)
	}
	return out
}

func roomResponders(members []RoomSpeaker, since []RoomEntry) []RoomSpeaker {
	everyone := false
	for _, e := range since {
		everyone = everyone || namesEveryone(e.Text)
	}
	var named []RoomSpeaker
	seen := map[BotKey]bool{}
	for _, m := range members {
		for _, e := range since {
			if e.ReplyToAuthor == m.Key || namesSpeaker(e.Text, m.Name) {
				if !seen[m.Key] {
					seen[m.Key] = true
					named = append(named, m)
				}
				break
			}
		}
	}
	if everyone || len(named) == 0 {
		var unique []RoomSpeaker
		seenAll := map[BotKey]bool{}
		for _, m := range members {
			if !seenAll[m.Key] {
				seenAll[m.Key] = true
				unique = append(unique, m)
			}
		}
		return unique
	}
	return named
}

func roomCited(members []RoomSpeaker, since []RoomEntry, asked map[BotKey]int64) []RoomSpeaker {
	cited := map[BotKey]int64{}
	seen := map[BotKey]int64{}
	for k, v := range asked {
		seen[k] = v
	}
	for _, e := range since {
		if e.Kind != RoomMemberEntry {
			continue
		}
		seen[e.Author] = max(seen[e.Author], e.Seq)
		for _, m := range RoomNamedBy(members, e.Author, e.Text) {
			cited[m.Key] = e.Seq
		}
	}
	var out []RoomSpeaker
	for _, m := range members {
		c, ok := cited[m.Key]
		if !ok {
			continue
		}
		if s, ok := seen[m.Key]; !ok || s < c {
			out = append(out, m)
		}
	}
	return out
}

// RoomNamedBy is the members other than author that text @-mentions.
func RoomNamedBy(members []RoomSpeaker, author BotKey, text string) []RoomSpeaker {
	everyone := namesEveryone(text)
	var out []RoomSpeaker
	for _, m := range members {
		if m.Key != author && (everyone || namesSpeaker(text, m.Name)) {
			out = append(out, m)
		}
	}
	return out
}

func namesEveryone(text string) bool { return mentions(text, "all") || mentions(text, "everyone") }

func namesSpeaker(text, name string) bool {
	for _, h := range handles(name) {
		if mentions(text, h) {
			return true
		}
	}
	return false
}

// handles: the full name, without spaces, or its first word (lowercased).
func handles(name string) []string {
	lower := strings.ToLower(strings.TrimSpace(name))
	fields := strings.Fields(lower)
	out := []string{lower, strings.Join(fields, "")}
	if len(fields) > 0 {
		out = append(out, fields[0])
	}
	out = slices.DeleteFunc(out, func(h string) bool { return h == "" })
	return slices.Compact(out)
}

// mentions: @handle not followed by more of a Latin word, so @al is not
// @alice; CJK names are often followed straight by the message.
func mentions(text, handle string) bool {
	lower := strings.ToLower(text)
	needle := "@" + handle
	for i := 0; ; {
		j := strings.Index(lower[i:], needle)
		if j < 0 {
			return false
		}
		end := i + j + len(needle)
		if end >= len(lower) {
			return true
		}
		c := lower[end]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_') {
			return true
		}
		i = i + j + 1
	}
}

// RoomRun counts one room turn's rounds and replies: at most MaxRounds
// rounds and MaxReplies replies, and a round in which nobody spoke ends it.
type RoomRun struct {
	Round   int
	Replies int
	Spoke   int
	Done    bool
	Last    *RoomEntry
}

// Answered records one asked member's outcome; it reports whether the turn
// may ask the next speaker.
func (r *RoomRun) Answered(entry *RoomEntry, l GroupLimits) bool {
	if entry != nil {
		r.Spoke++
		r.Replies++
		e := *entry
		r.Last = &e
	}
	if r.Replies >= l.MaxReplies {
		r.Done = true
	}
	return !r.Done
}

// EndRound reports whether another round runs.
func (r *RoomRun) EndRound(l GroupLimits) bool {
	if r.Done || r.Spoke == 0 || r.Round+1 >= l.MaxRounds {
		r.Done = true
		return false
	}
	r.Round++
	r.Spoke = 0
	return true
}

// ValidateRoomTurn checks a turn's room provenance as stored: a room turn is
// a host notice of the member's own conversation, never a collaboration or
// routine turn, and its ticket names the member that owns the fence.
func ValidateRoomTurn(t Turn) error {
	if t.Room == nil {
		return nil
	}
	r := *t.Room
	switch {
	case t.Origin != HostNoticeOrigin || t.ExchangeID != "" || t.RoutineRunID != "":
		return fail(Invalid, "room_turn")
	case r.Member != t.Fence.Key || r.Group.MachineID != r.Member.MachineID || r.Group == r.Member || r.Group.BotID == "":
		return fail(ScopeDenied, "room_turn")
	case r.Epoch < 1 || r.JoinedRevision < 1:
		return fail(Invalid, "room_turn")
	}
	return nil
}

// ValidateCreatedBy: a manager is another Bot of the same machine.
func ValidateCreatedBy(b Bot) error {
	if b.CreatedBy == nil {
		return nil
	}
	if *b.CreatedBy == b.Key || b.CreatedBy.MachineID != b.Key.MachineID || b.CreatedBy.BotID == "" {
		return fail(ScopeDenied, "created_by")
	}
	return nil
}
