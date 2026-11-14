package projectassistant

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

type fixtureCase struct {
	ID       string          `json:"id"`
	Rule     string          `json:"rule"`
	Input    json.RawMessage `json:"input"`
	Expected struct {
		Code     ErrorCode `json:"code"`
		Decision string    `json:"decision"`
		Text     string    `json:"text"`
		Promote  bool      `json:"promote"`
		Seq      int64     `json:"seq"`
	} `json:"expected"`
}

func decode(t *testing.T, raw []byte, out any) {
	t.Helper()
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatal(err)
	}
}
func TestSharedPolicyFixtures(t *testing.T) {
	raw, err := os.ReadFile("testdata/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		ContractVersion string        `json:"contract_version"`
		FixtureVersion  int           `json:"fixture_version"`
		Cases           []fixtureCase `json:"policy_cases"`
	}
	decode(t, raw, &fixture)
	if fixture.ContractVersion != ContractVersion || fixture.FixtureVersion != FixtureVersion || len(fixture.Cases) == 0 {
		t.Fatal("unsupported or empty fixture")
	}
	for _, c := range fixture.Cases {
		t.Run(c.ID, func(t *testing.T) {
			var err error
			decision := ""
			switch c.Rule {
			case "fence":
				var in struct {
					Received Fence
					Current  Fence
				}
				decode(t, c.Input, &in)
				err = CheckFence(in.Received, in.Current)
			case "replay":
				var in struct {
					Command Command
					Receipt *Receipt
				}
				decode(t, c.Input, &in)
				var d ReplayDecision
				d, err = DecideReplay(in.Command, in.Receipt)
				decision = string(d)
			case "reply":
				var in struct {
					ExplicitIDs []string `json:"explicit_ids"`
					FinalText   string   `json:"final_text"`
					Stopped     bool
				}
				decode(t, c.Input, &in)
				text, promote := VisibleReply(in.ExplicitIDs, in.FinalText, in.Stopped)
				if text != c.Expected.Text || promote != c.Expected.Promote {
					t.Fatalf("reply=(%q,%v), want (%q,%v)", text, promote, c.Expected.Text, c.Expected.Promote)
				}
			case "resume":
				var in struct {
					Binding      ExecutionBinding
					Current      MemoryRevision
					Context      ContextSnapshot
					Capabilities Capabilities
				}
				decode(t, c.Input, &in)
				var d ResumeDecision
				d, err = DecideResume(in.Binding, in.Current, in.Context, in.Capabilities)
				decision = string(d)
			case "read":
				var in struct {
					Current  ReadCursor
					Incoming ReadCursor
					LastSeq  int64 `json:"last_seq"`
				}
				decode(t, c.Input, &in)
				var cursor ReadCursor
				cursor, err = AdvanceRead(in.Current, in.Incoming, in.LastSeq)
				if cursor.MessageSeq != c.Expected.Seq {
					t.Fatalf("read seq=%d, want %d", cursor.MessageSeq, c.Expected.Seq)
				}
			case "create":
				var in struct {
					Settings   CreateBot
					Resolution ProjectResolution
				}
				decode(t, c.Input, &in)
				err = ValidateCreate(in.Settings, in.Resolution)
			default:
				t.Fatalf("unsupported fixture rule %q", c.Rule)
			}
			if Code(err) != c.Expected.Code {
				t.Fatalf("code=%q (%v), want %q", Code(err), err, c.Expected.Code)
			}
			if decision != c.Expected.Decision {
				t.Fatalf("decision=%q, want %q", decision, c.Expected.Decision)
			}
		})
	}
}

func testFence() Fence {
	return Fence{Key: BotKey{"machine-a", "bot-a"}, Project: ProjectScope{"machine-a", "project-a", "repo-a", RepositoryProject}, ConversationID: "chat-a", BindingID: "binding-a", Generation: 1}
}
func testCapabilities() Capabilities {
	f := testFence()
	return Capabilities{Key: f.Key, Project: f.Project, Agent: Codex, CLIVersion: "fixture/1", AdapterVersion: "fixture/1", Evidence: "synthetic-only", ReadOnlyEnforced: true, NativeMemory: NativeMemoryDisabled, NativeMemoryVerified: true, Resume: true}
}
func TestEditsFreezeRunningInstructionsUntilNextIdleTurn(t *testing.T) {
	bot := Bot{Name: "Before", Appearance: Appearance{Blob, EntryColor}, Agent: Codex, Instructions: InstructionSnapshot{Revision: 1, Text: "Before", RoleSnapshotRef: "role@1"}, Version: 1}
	captured, err := CaptureInstructions(bot, "")
	if err != nil {
		t.Fatal(err)
	}
	changed, err := ApplyEdit(bot, EditBot{ExpectedVersion: 1, Name: "After", Appearance: Appearance{Hex, "#abcdef"}, Instructions: "After"})
	if err != nil {
		t.Fatal(err)
	}
	if captured.Text != "Before" || captured.Revision != 1 || changed.Instructions.Revision != 2 || changed.Agent != Codex {
		t.Fatal("edit rewrote running snapshot or immutable agent")
	}
	if _, err := CaptureInstructions(changed, "turn-a"); Code(err) != StaleVersion {
		t.Fatal("captured during running turn", err)
	}
	next, err := CaptureInstructions(changed, "")
	if err != nil || next.Text != "After" || next.RoleSnapshotRef != "role@1" {
		t.Fatal("next turn did not capture edited snapshot", next, err)
	}
	if _, err := ApplyEdit(changed, EditBot{ExpectedVersion: 1}); Code(err) != StaleVersion {
		t.Fatal("stale edit accepted", err)
	}
}
func TestCapacityBoundaries(t *testing.T) {
	for _, text := range []string{strings.Repeat("a", MaxMessageBytes), strings.Repeat("界", MaxMessageBytes/3)} {
		if err := ValidateMessage(text); err != nil {
			t.Fatal(err)
		}
	}
	if Code(ValidateMessage(strings.Repeat("a", MaxMessageBytes+1))) != CapacityFull {
		t.Fatal("message cap not enforced")
	}
	if Code(ValidateMessage(" \n")) != Invalid {
		t.Fatal("blank message accepted")
	}
	if err := CheckAdmission(MaxPendingTurns - 1); err != nil {
		t.Fatal(err)
	}
	if Code(CheckAdmission(MaxPendingTurns)) != CapacityFull {
		t.Fatal("queue full accepted")
	}
	for _, events := range []bool{false, true} {
		n := MaxPageRows
		if events {
			n = MaxEventPageRows
		}
		if CheckPage(n, events) != nil || Code(CheckPage(n+1, events)) != Invalid || Code(CheckPage(0, events)) != Invalid {
			t.Fatal("page boundary")
		}
	}
	settings := CreateBot{Target: ProjectSelection{"machine-a", "project-a"}, Name: strings.Repeat("界", MaxNameRunes), Appearance: Appearance{Blob, EntryColor}, Agent: Codex, PermissionPolicy: Ask, Instructions: strings.Repeat("a", MaxInstructionBytes)}
	resolved := ProjectResolution{Scope: testFence().Project, Directory: "/workspace/repo", Current: true, Available: true}
	if err := ValidateCreate(settings, resolved); err != nil {
		t.Fatal(err)
	}
	settings.Name += "界"
	if Code(ValidateCreate(settings, resolved)) != Invalid {
		t.Fatal("name rune cap not enforced")
	}
	settings.Name = "Good"
	settings.Instructions += "a"
	if Code(ValidateCreate(settings, resolved)) != CapacityFull {
		t.Fatal("instructions cap not enforced")
	}
}
func TestPermissionCannotWidenScopeOrAnswerAnotherGeneration(t *testing.T) {
	f := testFence()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	offer := PermissionOffer{ID: "offer-a", Fence: f, TurnID: "turn-a", State: PermissionPending, NativeRequestID: "rpc-a", ExpiresAt: now.Add(time.Minute), Options: []PermissionOption{{ID: "read", ReadOnly: true, Allow: true, Kind: AllowOnce}, {ID: "write", Allow: true, Kind: AllowOnce}, {ID: "deny", Kind: RejectOnce}}}
	answer := PermissionAnswer{Fence: f, OfferID: offer.ID, OptionID: "read", TurnID: offer.TurnID}
	source := SourceStatus{MachineID: f.Key.MachineID, State: SourceOnline, Readable: true, Writable: true, Fresh: true, Access: Interactive}
	check := func() error {
		return CheckPermission(offer, answer, PermissionContext{Fence: f, ActiveTurnID: "turn-a", Now: now, Source: source, Feature: FeatureState{Enabled: true}})
	}
	if err := check(); err != nil {
		t.Fatal(err)
	}
	// Following CodyNC, the person may allow one action that changes something;
	// a standing allow or reject is still refused.
	answer.OptionID = "write"
	if err := check(); err != nil {
		t.Fatalf("allowing one native action was refused: %v", err)
	}
	// As in Codync (decided 2026-11-14), the person may also allow or refuse
	// requests like this for good; an option whose allow flag contradicts its
	// kind is still refused.
	offer.Options = append(offer.Options, PermissionOption{ID: "always", Allow: true, Kind: AllowAlways}, PermissionOption{ID: "never", Kind: RejectAlways}, PermissionOption{ID: "bad-never", Allow: true, Kind: RejectAlways})
	for _, standing := range []string{"always", "never"} {
		answer.OptionID = standing
		if err := check(); err != nil {
			t.Fatalf("standing %s rule refused: %v", standing, err)
		}
	}
	answer.OptionID = "bad-never"
	if Code(check()) != Invalid {
		t.Fatal("a never option that allows was accepted")
	}
	answer.OptionID = "deny"
	if err := check(); err != nil {
		t.Fatal(err)
	}
	answer.Fence.Generation--
	if Code(check()) != StaleGeneration {
		t.Fatal("late generation answer accepted")
	}
	answer.Fence = f
	answer.TurnID = "turn-other"
	if Code(check()) != StaleVersion {
		t.Fatal("answer from another turn accepted")
	}
	answer.TurnID = offer.TurnID
	offer.State = PermissionResolved
	if Code(check()) != StaleVersion {
		t.Fatal("answer replay accepted")
	}
	offer.State = PermissionPending
	offer.ExpiresAt = now
	if Code(check()) != Expired {
		t.Fatal("expiry boundary accepted")
	}
	offer.ExpiresAt = now.Add(time.Minute)
	source.State = SourceRevoked
	if Code(check()) != Revoked {
		t.Fatal("revoked answer accepted")
	}
}

func TestForgottenEntryCannotReturnThroughAnOldSource(t *testing.T) {
	f := testFence()
	memory := MemoryRevision{Key: f.Key, Project: f.Project, Revision: 2, ForgottenThrough: 2, ForgottenSourceIDs: []string{"source-forgotten", "entry-forgotten"}}
	entry := MemoryEntry{ID: "entry-new", Key: f.Key, Project: f.Project, Text: "Retained fact", Revision: 2, Sources: []MemorySource{{ID: "source-keep", Key: f.Key, Project: f.Project, Kind: "user_message", Revision: 1}}}
	if err := ValidateMemoryWrite(entry, memory, 2); err != nil {
		t.Fatal(err)
	}
	entry.Sources[0].ID = "source-forgotten"
	if Code(ValidateMemoryWrite(entry, memory, 2)) != ContextStale {
		t.Fatal("forgotten source revived")
	}
	entry.Sources[0].ID = "source-keep"
	entry.ID = "entry-forgotten"
	if Code(ValidateMemoryWrite(entry, memory, 2)) != ContextStale {
		t.Fatal("forgotten entry revived")
	}
	entry.ID = "entry-new"
	if Code(ValidateMemoryWrite(entry, memory, 1)) != StaleVersion {
		t.Fatal("stale memory write accepted")
	}
}
func TestFeatureModesAndReadableBadge(t *testing.T) {
	if (FeatureState{}).Access() != Hidden || (FeatureState{HasData: true}).Access() != HistoryOnly || (FeatureState{Enabled: true}).Access() != Interactive {
		t.Fatal("feature default")
	}
	if Code((FeatureState{HasData: true}).CheckWrite()) != Disabled {
		t.Fatal("disabled feature can write")
	}
	readable := SourceStatus{MachineID: "machine-a", State: SourceOffline, Readable: true, Fresh: false, Access: HistoryOnly}
	revoked := readable
	revoked.State = SourceRevoked
	u, p := EntryBadge([]BotSummary{{Source: readable, UnreadCount: 2, PendingPermissions: 1}, {Source: revoked, UnreadCount: 99, PendingPermissions: 99}})
	if u != 2 || p != 1 {
		t.Fatal("badge leaked revoked source", u, p)
	}
	if Code(CheckSource(readable, true)) != Offline {
		t.Fatal("offline source queued work")
	}
}
func TestForgetRejectsOpaqueHistoryAndKeepsTombstones(t *testing.T) {
	f := testFence()
	before := MemoryRevision{Key: f.Key, Project: f.Project, Revision: 1, ForgottenSourceIDs: []string{"old"}}
	after, err := Forget(before, 1, []string{"secret", "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if before.Revision != 1 || len(before.ForgottenSourceIDs) != 1 || after.Revision != 2 || len(after.ForgottenSourceIDs) != 2 {
		t.Fatal("forget mutated old state or duplicate tombstone")
	}
	if _, err := Forget(after, 1, []string{"other"}); Code(err) != StaleVersion {
		t.Fatal("stale forget accepted")
	}
	binding := ExecutionBinding{Fence: f, Agent: Codex, NativeSessionID: "native-a", MemoryRevision: 1}
	if Code(CheckOpaqueResume(binding, after)) != ResumeForbiddenAfterForget {
		t.Fatal("opaque forgotten history accepted")
	}
	filtered := ContextSnapshot{Memory: after, Text: "Only retained data", ProvenanceComplete: true, Sources: []MemorySource{{ID: "retained", Key: f.Key, Project: f.Project, Revision: 1, Kind: "user_message"}}}
	d, err := DecideResume(binding, after, filtered, testCapabilities())
	if err != nil || d != StartFiltered {
		t.Fatal(d, err)
	}
	filtered.ProvenanceComplete = false
	if _, err := DecideResume(binding, after, filtered, testCapabilities()); Code(err) != ContextStale {
		t.Fatal("untraceable summary accepted")
	}
}
func TestCapabilitiesKeepUnknownAndSourceMismatchUnavailable(t *testing.T) {
	c := testCapabilities()
	if err := CheckExecutionCapabilities(c, testFence()); err != nil {
		t.Fatal(err)
	}
	c.Project.CanonicalID = "other"
	if Code(CheckExecutionCapabilities(c, testFence())) != ScopeDenied {
		t.Fatal("borrowed another project proof")
	}
	c = testCapabilities()
	c.NativeMemoryVerified = false
	if Code(CheckCapabilities(c)) != MemoryIsolationUnverified {
		t.Fatal("unverified native memory admitted")
	}
	// Native tools ask the person before acting, as in CodyNC; read-only is
	// no longer an admission requirement, but pinned evidence still is.
	c = testCapabilities()
	c.ReadOnlyEnforced = false
	if err := CheckCapabilities(c); err != nil {
		t.Fatalf("per-action approval was refused: %v", err)
	}
	c.Evidence = ""
	if Code(CheckCapabilities(c)) != AdapterNotFound {
		t.Fatal("capabilities without pinned evidence admitted")
	}
	for _, code := range []ErrorCode{AdapterNotFound, AdapterInventoryUnknown, AuthUnavailableInIsolatedRuntime, SandboxUnavailable} {
		c = testCapabilities()
		c.Unavailable = &Failure{Code: code, Field: "probe"}
		if Code(CheckCapabilities(c)) != code {
			t.Fatal("lost typed native refusal", code)
		}
	}
}
func TestFirstSendFenceAndTerminalUnknown(t *testing.T) {
	current := testFence()
	current.BindingID = ""
	current.Generation = 0
	if err := CheckSendFence(current, current); err != nil {
		t.Fatal(err)
	}
	bound := testFence()
	if Code(CheckSendFence(current, bound)) != StaleGeneration {
		t.Fatal("old unbound send accepted after binding")
	}
	for _, to := range []TurnState{TurnQueued, TurnRunning, TurnSucceeded} {
		if CanTransition(TurnUnknown, to) {
			t.Fatal("unknown automatically replayed")
		}
	}
	if !CanTransition(TurnRunning, TurnUnknown) || !CanTransition(TurnWaitingPermission, TurnCancelled) {
		t.Fatal("missing failure/stop paths")
	}
}
