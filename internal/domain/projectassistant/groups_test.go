package projectassistant

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// Room limits use the production scheduler values.
var testGroupLimits = GroupLimits{MaxRounds: 3, MaxReplies: 10, MaxLateTurns: 3}

var groupAt = time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)

func TestRoomResponseResponsibilityFollowsActualAnswers(t *testing.T) {
	person := RoomEntry{Kind: RoomPersonEntry, Text: "嗨"}
	answer := RoomEntry{Kind: RoomMemberEntry, Text: "嗨！"}
	for _, tc := range []struct {
		name  string
		lines []RoomEntry
		want  bool
	}{
		{"empty", nil, false},
		{"greeting", []RoomEntry{person}, true},
		{"answered", []RoomEntry{person, answer}, false},
		{"new message needs acknowledgement", []RoomEntry{person, answer, person}, true},
		{"old answer alone", []RoomEntry{answer}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := RoomNeedsResponse(tc.lines); got != tc.want {
				t.Fatalf("needs response = %t, want %t", got, tc.want)
			}
		})
	}
}

func groupScope() ProjectScope {
	return ProjectScope{MachineID: "m", ProjectID: "p", CanonicalID: "repo", Kind: RepositoryProject}
}

func groupBot(id, name string, scope ProjectScope) Bot {
	return Bot{Key: BotKey{MachineID: scope.MachineID, BotID: id}, Project: scope, ConversationID: "chat/" + id, Name: name, Agent: ClaudeCode}
}

func groupCatalog(bots ...Bot) GroupCatalog {
	c := GroupCatalog{Bots: map[BotKey]Bot{}, Groups: map[BotKey]bool{}}
	for _, b := range bots {
		c.Bots[b.Key] = b
	}
	return c
}

func key(id string) BotKey { return BotKey{MachineID: "m", BotID: id} }

func TestGroupMembershipFollowsStoredBots(t *testing.T) {
	scope := groupScope()
	otherProject := scope
	otherProject.ProjectID = "p2" // same canonical repository, another registered Project
	hidden := groupBot("h", "Hidden", scope)
	hidden.Hidden = true
	cat := groupCatalog(groupBot("a", "Alice", scope), groupBot("b", "Bob", scope), hidden, groupBot("x", "Xeno", otherProject))
	cat.Groups[key("g2")] = true
	foreign := BotKey{MachineID: "other", BotID: "a"}
	for _, c := range []struct {
		name    string
		members []BotKey
		code    ErrorCode
		field   string
	}{
		{"valid with hidden member", []BotKey{key("a"), key("h")}, "", ""},
		{"empty", nil, Invalid, "group_members"},
		{"duplicate silently deduplicated", []BotKey{key("a"), key("a")}, "", ""},
		{"nested", []BotKey{key("a"), key("g2")}, Invalid, "group_member_nested"},
		{"self", []BotKey{key("g")}, Invalid, "group_member_self"},
		{"deleted", []BotKey{key("a"), key("gone")}, NotFound, "group_member"},
		{"foreign machine", []BotKey{key("a"), foreign}, ScopeDenied, "group_member_machine"},
		{"same canonical other Project", []BotKey{key("a"), key("x")}, ScopeDenied, "group_member_project"},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateGroupMembers(key("g"), scope, c.members, cat, testGroupLimits)
			if Code(err) != c.code {
				t.Fatalf("code %q, want %q (%v)", Code(err), c.code, err)
			}
			if c.code != "" && err.(*Failure).Field != c.field {
				t.Fatalf("field %q, want %q", err.(*Failure).Field, c.field)
			}
		})
	}
	if err := ValidateGroupMembers(key("g"), scope, []BotKey{key("a")}, cat, GroupLimits{}); Code(err) != Invalid {
		t.Fatalf("missing limits accepted: %v", err)
	}
	if _, err := NewGroup(GroupDraft{Key: key("a"), Project: scope, ConversationID: "room", Name: "Room", Members: []BotKey{key("b")}}, cat, testGroupLimits, groupAt); Code(err) != Invalid {
		t.Fatalf("a group used a Bot's key: %v", err)
	}
	// Codync silent deduplication: [a, b, a] becomes [a, b]
	dedupGroup, err := NewGroup(GroupDraft{Key: key("g_dedup"), Project: scope, ConversationID: "room", Name: "Room", Members: []BotKey{key("a"), key("b"), key("a")}}, cat, testGroupLimits, groupAt)
	if err != nil {
		t.Fatalf("NewGroup with duplicates failed: %v", err)
	}
	if len(dedupGroup.Members) != 2 || dedupGroup.Members[0].Key != key("a") || dedupGroup.Members[1].Key != key("b") {
		t.Fatalf("unexpected dedupGroup members: %+v", dedupGroup.Members)
	}

}

func TestGroupEditMovesMembershipOnlyForChangedMembers(t *testing.T) {
	scope := groupScope()
	cat := groupCatalog(groupBot("a", "Alice", scope), groupBot("b", "Bob", scope), groupBot("c", "Cy", scope))
	g, err := NewGroup(GroupDraft{Key: key("g"), Project: scope, ConversationID: "room", Name: "Room", Members: []BotKey{key("a"), key("b")}}, cat, testGroupLimits, groupAt)
	if err != nil {
		t.Fatal(err)
	}
	same, err := EditGroup(g, GroupEdit{ExpectedVersion: 1, Name: "Renamed", Members: []BotKey{key("b"), key("a")}}, cat, testGroupLimits, groupAt)
	if err != nil || same.MembershipRevision != 1 || same.Version != 2 {
		t.Fatalf("reorder moved membership: %+v %v", same, err)
	}
	if _, err = EditGroup(same, GroupEdit{ExpectedVersion: 1, Name: "x", Members: []BotKey{key("a")}}, cat, testGroupLimits, groupAt); Code(err) != StaleVersion {
		t.Fatalf("stale edit: %v", err)
	}
	removed, _ := EditGroup(same, GroupEdit{ExpectedVersion: 2, Name: "x", Members: []BotKey{key("a")}}, cat, testGroupLimits, groupAt)
	back, _ := EditGroup(removed, GroupEdit{ExpectedVersion: 3, Name: "x", Members: []BotKey{key("a"), key("b")}}, cat, testGroupLimits, groupAt)
	a, _ := back.Member(key("a"))
	b, _ := back.Member(key("b"))
	if a.JoinedRevision != 1 || b.JoinedRevision != 3 {
		t.Fatalf("joined revisions %d %d", a.JoinedRevision, b.JoinedRevision)
	}
	if found, ok := GroupWithMembers([]Group{back}, scope, []BotKey{key("b"), key("a")}); !ok || found.Key != g.Key {
		t.Fatal("same members did not find the group")
	}
	other := scope
	other.ProjectID = "p2"
	if _, ok := GroupWithMembers([]Group{back}, other, []BotKey{key("a"), key("b")}); ok {
		t.Fatal("same members in another Project matched")
	}
	left, ok := LeaveGroup(back, key("a"), groupAt)
	if !ok || len(left.Members) != 1 || left.MembershipRevision != 4 {
		t.Fatalf("leave: %+v", left)
	}
	gone, err := DeleteGroup(left, left.Version, groupAt)
	if err != nil || !gone.Deleted {
		t.Fatal(err)
	}
	if _, ok := GroupWithMembers([]Group{gone}, scope, gone.MemberKeys()); ok {
		t.Fatal("a deleted group still matched")
	}
}

func TestRoomEpochsLateTurnsAndStop(t *testing.T) {
	scope := groupScope()
	cat := groupCatalog(groupBot("a", "Alice", scope), groupBot("b", "Bob", scope))
	g, _ := NewGroup(GroupDraft{Key: key("g"), Project: scope, ConversationID: "room", Name: "Room", Members: []BotKey{key("a"), key("b")}}, cat, testGroupLimits, groupAt)
	l := testGroupLimits
	var s RoomState
	if _, stopped, _ := StopRoom(s, l); stopped {
		t.Fatal("stop before any room turn")
	}
	s, first, err := StartRoomTurn(g, s)
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := TicketFor(g, s, key("a"), false)
	if err != nil {
		t.Fatal(err)
	}
	s, late, _ := LateAnswer(s, 7, l)
	if late != nil {
		t.Fatal("a late answer started while a room turn runs")
	}
	s, _, _ = LateAnswer(s, 5, l)
	if !RoomTurnCurrent(s, first.Epoch) {
		t.Fatal("late answer ended the running turn")
	}
	s, follow, _ := RoomTurnEnded(s, first.Epoch, l)
	if follow == nil || follow.Late != 5 {
		t.Fatalf("follow-up %+v", follow)
	}
	if _, again, _ := RoomTurnEnded(s, first.Epoch, l); again != nil {
		t.Fatal("old turn end changed something")
	}
	// The person's first ticket is now stale: a newer room turn runs.
	if err = CheckRoomTicket(g, s, ticket); Code(err) != StaleGeneration {
		t.Fatalf("stale epoch published: %v", err)
	}
	if _, _, err = PublishRoomAnswer(g, s, ticket, 9, "late words", nil); Code(err) != StaleGeneration {
		t.Fatalf("stale answer published: %v", err)
	}
	s, _, _ = RoomTurnEnded(s, follow.Epoch, l)
	for i := 1; i < l.MaxLateTurns; i++ {
		var lt *RoomTurn
		s, lt, _ = LateAnswer(s, 9, l)
		if lt == nil {
			t.Fatalf("late turn %d not started", i)
		}
		s, _, _ = RoomTurnEnded(s, lt.Epoch, l)
	}
	if _, lt, _ := LateAnswer(s, 9, l); lt != nil {
		t.Fatal("late turns exceeded the supplied limit")
	}
	s, next, _ := StartRoomTurn(g, s)
	busy, _ := TicketFor(g, s, key("b"), true)
	s, _, _ = RoomTurnEnded(s, next.Epoch, l)
	s, _, _ = StartRoomTurn(g, s)
	if err = CheckRoomTicket(g, s, busy); err != nil {
		t.Fatalf("a late ticket was dropped by a newer person turn: %v", err)
	}
	// Own-chat Stop keeps late room turns; group Stop drops them.
	queue := []RoomTicket{busy, ticket}
	if kept := AfterOwnChatStop(queue); len(kept) != 1 || !kept[0].Late {
		t.Fatalf("own-chat stop kept %+v", kept)
	}
	if kept := AfterGroupStop(queue, g.Key); len(kept) != 0 {
		t.Fatalf("group stop kept %+v", kept)
	}
	if again := AskAgain(queue, RoomTicket{Group: g.Key, Member: key("b"), Epoch: s.Epoch, JoinedRevision: 1}); len(again) != 2 || again[1].Late {
		t.Fatalf("asking again did not replace: %+v", again)
	}
	s, stopped, _ := StopRoom(s, l)
	if !stopped {
		t.Fatal("stop")
	}
	if err = CheckRoomTicket(g, s, busy); Code(err) != StaleGeneration {
		t.Fatalf("late ticket survived group stop: %v", err)
	}
	if _, lt, _ := LateAnswer(s, 20, l); lt != nil {
		t.Fatal("late turn after stop")
	}
	// Revoked membership: leaving and rejoining invalidates the old ticket.
	s, _, _ = StartRoomTurn(g, s)
	tb, _ := TicketFor(g, s, key("b"), true)
	removed, _ := EditGroup(g, GroupEdit{ExpectedVersion: 1, Name: "Room", Members: []BotKey{key("a")}}, cat, l, groupAt)
	if err = CheckRoomTicket(removed, s, tb); Code(err) != Revoked {
		t.Fatalf("removed member published: %v", err)
	}
	rejoined, _ := EditGroup(removed, GroupEdit{ExpectedVersion: 2, Name: "Room", Members: []BotKey{key("a"), key("b")}}, cat, l, groupAt)
	if err = CheckRoomTicket(rejoined, s, tb); Code(err) != Revoked {
		t.Fatalf("rejoined member's old ticket published: %v", err)
	}
	deleted, _ := DeleteGroup(rejoined, rejoined.Version, groupAt)
	if err = CheckRoomTicket(deleted, s, tb); Code(err) != NotFound {
		t.Fatalf("deleted group published: %v", err)
	}
	if _, _, err = StartRoomTurn(deleted, s); Code(err) != NotFound {
		t.Fatal("deleted group started a turn")
	}
}

func TestRoomSpeakersMentionsAndPassTermination(t *testing.T) {
	members := []RoomSpeaker{{key("a"), "Alice Smith"}, {key("b"), "Bob"}, {key("c"), "小明"}}
	person := func(seq int64, text string) RoomEntry { return RoomEntry{Seq: seq, Kind: RoomPersonEntry, Text: text} }
	said := func(seq int64, who, text string) RoomEntry {
		return RoomEntry{Seq: seq, Kind: RoomMemberEntry, Author: key(who), Text: text}
	}
	ids := func(s []RoomSpeaker) (out []string) {
		for _, m := range s {
			out = append(out, m.Key.BotID)
		}
		return
	}
	eq := func(got []RoomSpeaker, want ...string) {
		t.Helper()
		g := ids(got)
		if len(g) != len(want) {
			t.Fatalf("speakers %v, want %v", g, want)
		}
		for i := range g {
			if g[i] != want[i] {
				t.Fatalf("speakers %v, want %v", g, want)
			}
		}
	}
	eq(RoomSpeakersFor(members, []RoomEntry{person(1, "hello")}, false, 0, nil), "a", "b", "c")
	eq(RoomSpeakersFor(members, []RoomEntry{person(1, "@bob what now")}, false, 0, nil), "b")
	eq(RoomSpeakersFor(members, []RoomEntry{person(1, "@alice and @AliceSmith")}, false, 0, nil), "a")
	eq(RoomSpeakersFor(members, []RoomEntry{person(1, "@al hi")}, false, 0, nil), "a", "b", "c")
	eq(RoomSpeakersFor(members, []RoomEntry{person(1, "@小明請看")}, false, 0, nil), "c")
	eq(RoomSpeakersFor(members, []RoomEntry{person(1, "@bob @all")}, false, 0, nil), "a", "b", "c")
	eq(RoomSpeakersFor(members, []RoomEntry{{Seq: 1, Kind: RoomPersonEntry, Text: "yes", ReplyToAuthor: key("c")}}, false, 0, nil), "c")
	// Later rounds: only members another member named and not asked since.
	since := []RoomEntry{person(1, "hi"), said(2, "a", "@bob check this")}
	eq(RoomSpeakersFor(members, since, false, 1, map[BotKey]int64{key("a"): 1, key("b"): 1, key("c"): 1}), "b")
	eq(RoomSpeakersFor(members, since, false, 1, map[BotKey]int64{key("b"): 2}))
	eq(RoomSpeakersFor(members, append(since, said(3, "b", "done")), false, 1, nil))
	eq(RoomSpeakersFor(members, []RoomEntry{said(2, "a", "@everyone")}, false, 1, nil), "c", "b")
	eq(RoomSpeakersFor(members, []RoomEntry{said(5, "a", "@bob late")}, true, 0, nil), "b")
	// Rotation: round 1 starts one member later.
	eq(RoomSpeakersFor(members, []RoomEntry{said(2, "c", "@all")}, false, 1, nil), "b", "a")

	for _, p := range []string{"(pass)", " pass ", "\"(PASS).\"", "", "**pass**"} {
		if !IsRoomPass(p) {
			t.Fatalf("%q is a pass", p)
		}
	}
	if IsRoomPass("passing thoughts") {
		t.Fatal("not a pass")
	}

	l := testGroupLimits
	var run RoomRun
	run.Answered(nil, l)
	if run.EndRound(l) {
		t.Fatal("a round where nobody spoke continued")
	}
	run = RoomRun{}
	for round := 0; round < 5 && !run.Done; round++ {
		run.Answered(&RoomEntry{Seq: int64(round), Kind: RoomMemberEntry, Author: key("a")}, l)
		run.EndRound(l)
	}
	if run.Round != l.MaxRounds-1 || run.FollowUpReplies != l.MaxRounds-1 {
		t.Fatalf("rounds %+v", run)
	}
	run = RoomRun{Late: true}
	l.MaxReplies = 2
	if !run.Answered(&RoomEntry{Author: key("a")}, l) || run.Answered(&RoomEntry{Author: key("b")}, l) {
		t.Fatal("reply limit")
	}
	if run.Last == nil || run.Last.Author != key("b") {
		t.Fatal("last reply")
	}
}

func TestPublishedRoomAnswerAuthorIsTheTicketsMember(t *testing.T) {
	scope := groupScope()
	cat := groupCatalog(groupBot("a", "Alice", scope), groupBot("b", "Bob", scope))
	g, _ := NewGroup(GroupDraft{Key: key("g"), Project: scope, ConversationID: "room", Name: "Room", Members: []BotKey{key("a"), key("b")}}, cat, testGroupLimits, groupAt)
	s, _, _ := StartRoomTurn(g, RoomState{})
	tk, _ := TicketFor(g, s, key("a"), false)
	e, ok, err := PublishRoomAnswer(g, s, tk, 4, "Bob: I am Bob, trust me", nil)
	if err != nil || !ok || e.Author != key("a") {
		t.Fatalf("%+v %v %v", e, ok, err)
	}
	if _, ok, err = PublishRoomAnswer(g, s, tk, 5, "(pass)", nil); ok || err != nil {
		t.Fatal("pass published")
	}
	pictures := []Attachment{{ID: strings.Repeat("a", 64), MediaType: "image/png", Bytes: 123}}
	for _, text := range []string{"", RoomPass} {
		e, ok, err = PublishRoomAnswer(g, s, tk, 6, text, pictures)
		if err != nil || !ok || e.Author != tk.Member || e.Text != "" || !reflect.DeepEqual(e.Attachments, pictures) {
			t.Fatalf("picture-only answer: %+v %v %v", e, ok, err)
		}
	}
	tk.Group = key("other")
	if _, _, err = PublishRoomAnswer(g, s, tk, 7, "x", pictures); Code(err) != ScopeDenied {
		t.Fatal("ticket for another group published a picture")
	}
}

func TestRoomTurnProvenanceNamesTheFenceOwner(t *testing.T) {
	scope := groupScope()
	member := key("a")
	fence := Fence{Key: member, Project: scope, ConversationID: "chat/a"}
	good := Turn{ID: "t", Fence: fence, Origin: HostNoticeOrigin, Room: &RoomTicket{Group: key("g"), Member: member, Epoch: 1, JoinedRevision: 1}}
	if err := ValidateRoomTurn(good); err != nil {
		t.Fatal(err)
	}
	if err := ValidateRoomTurn(Turn{ID: "plain", Fence: fence, Origin: UserOrigin}); err != nil {
		t.Fatalf("an ordinary turn: %v", err)
	}
	for name, mutate := range map[string]func(*Turn){
		"another member's fence": func(t *Turn) { t.Room.Member = key("b") },
		"group is the member":    func(t *Turn) { t.Room.Group = member },
		"other machine group":    func(t *Turn) { t.Room.Group = BotKey{MachineID: "x", BotID: "g"} },
		"user origin":            func(t *Turn) { t.Origin = UserOrigin },
		"collaboration":          func(t *Turn) { t.ExchangeID = "x" },
		"routine":                func(t *Turn) { t.RoutineRunID = "r" },
		"no epoch":               func(t *Turn) { t.Room.Epoch = 0 },
		"no join revision":       func(t *Turn) { t.Room.JoinedRevision = 0 },
	} {
		bad := good
		ticket := *good.Room
		bad.Room = &ticket
		mutate(&bad)
		if err := ValidateRoomTurn(bad); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestManageTeamIsThePersonsSwitchAndCreatedByIsHostOnly(t *testing.T) {
	b := groupBot("a", "Alice", groupScope())
	b.Version, b.Instructions.Revision, b.Appearance = 1, 1, Appearance{Shape: Blob, Color: "#e39567"}
	edit := EditBot{ExpectedVersion: 1, Name: "Alice", Appearance: b.Appearance}
	kept, err := ApplyEdit(b, edit)
	if err != nil || kept.ManageTeam {
		t.Fatalf("an edit without the field turned it on: %+v %v", kept, err)
	}
	on := true
	edit.ManageTeam = &on
	turned, err := ApplyEdit(b, edit)
	if err != nil || !turned.ManageTeam || turned.Version != 2 {
		t.Fatalf("person's switch: %+v %v", turned, err)
	}
	off := false
	edit.ExpectedVersion, edit.ManageTeam = 2, nil
	if again, _ := ApplyEdit(turned, edit); !again.ManageTeam {
		t.Fatal("an older console's edit reset the switch")
	}
	edit.ManageTeam = &off
	if again, _ := ApplyEdit(turned, edit); again.ManageTeam {
		t.Fatal("the person could not turn it off")
	}
	// Neither request type can carry CreatedBy: only the host's manager
	// operation sets it on the stored Bot.
	for _, typ := range []any{CreateBot{}, EditBot{}} {
		if hasJSONField(typ, "created_by") {
			t.Fatalf("%T carries created_by", typ)
		}
	}
	manager := key("m1")
	b.CreatedBy = &manager
	if err := ValidateCreatedBy(b); err != nil {
		t.Fatal(err)
	}
	self := b.Key
	b.CreatedBy = &self
	if err := ValidateCreatedBy(b); err == nil {
		t.Fatal("a Bot created itself")
	}
	other := BotKey{MachineID: "x", BotID: "m1"}
	b.CreatedBy = &other
	if err := ValidateCreatedBy(b); err == nil {
		t.Fatal("a manager on another machine")
	}
}

func hasJSONField(v any, name string) bool {
	typ := reflect.TypeOf(v)
	for i := range typ.NumField() {
		if strings.Split(typ.Field(i).Tag.Get("json"), ",")[0] == name {
			return true
		}
	}
	return false
}

func TestARoomLineCarriesAtMostFourPictures(t *testing.T) {
	picture := Attachment{ID: strings.Repeat("a", 64), MediaType: "image/png", Bytes: 10}
	if err := ValidateRoomMessage("", []Attachment{picture}); err != nil {
		t.Fatalf("a picture without text was refused: %v", err)
	}
	if err := ValidateRoomMessage("", nil); Code(err) != Invalid {
		t.Fatalf("an empty line was admitted: %v", err)
	}
	five := []Attachment{picture, picture, picture, picture, picture}
	if err := ValidateRoomMessage("look", five); err == nil || err.Error() != "pa_capacity_full: images" {
		t.Fatalf("five pictures: %v", err)
	}
	big := picture
	big.Bytes = MaxAttachmentBytes + 1
	if err := ValidateRoomMessage("look", []Attachment{big}); Code(err) != Invalid {
		t.Fatalf("an oversized picture was admitted: %v", err)
	}
}
