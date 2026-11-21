package projectassistant

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf16"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// This port double tests coordination only. The real SQLite transaction is
// tested by the host integration suite, not inferred from this double.
type teamTestRepository struct {
	Repository
	mu           sync.Mutex
	bots         map[pa.BotKey]pa.Bot
	receipts     map[string]pa.Receipt
	manager      pa.BotKey
	writes       int
	beforeCommit func()
}

func (r *teamTestRepository) Bot(_ context.Context, k pa.BotKey) (pa.Bot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	b, ok := r.bots[k]
	if !ok {
		return b, serviceFailure(pa.NotFound, "bot")
	}
	return b, nil
}
func (r *teamTestRepository) List(context.Context, pa.ListBots) (pa.BotPage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p := pa.BotPage{}
	for _, b := range r.bots {
		p.Items = append(p.Items, pa.BotSummary{Bot: b})
	}
	return p, nil
}
func (r *teamTestRepository) Receipt(_ context.Context, c pa.Command) (*pa.Receipt, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.receipts[c.IdempotencyKey]
	if !ok {
		return nil, nil
	}
	return &v, nil
}
func (r *teamTestRepository) CommitManaged(_ context.Context, e ManagerExecution, m Mutation, _ int) (pa.Receipt, error) {
	if r.beforeCommit != nil {
		r.beforeCommit()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	b := r.bots[e.Fence.Key]
	if err := ConfirmManagerPlan(e.Under, ManagerContext{Caller: b, Grant: GrantFor(b), Project: b.Project}); err != nil {
		return pa.Receipt{}, err
	}
	r.writes++
	r.bots[m.Command.Key] = *m.Bot
	r.receipts[m.Command.IdempotencyKey] = m.Receipt
	return m.Receipt, nil
}

type teamFixture struct {
	team    *TeamCoordinator
	repo    *teamTestRepository
	fence   pa.Fence
	now     time.Time
	clockMu sync.Mutex
}

func newTeamFixture(t *testing.T) *teamFixture {
	t.Helper()
	scope := pa.ProjectScope{MachineID: "m", ProjectID: "p", CanonicalID: "canonical", Kind: pa.RepositoryProject}
	key := pa.BotKey{MachineID: "m", BotID: "manager"}
	b := pa.Bot{Key: key, Project: scope, ConversationID: "chat", Name: "Manager", Appearance: pa.Appearance{Shape: pa.Blob, Color: pa.EntryColor}, Agent: pa.Codex, PermissionPolicy: pa.Ask, ManageTeam: true, Version: 1, Instructions: pa.InstructionSnapshot{Revision: 1}}
	r := &teamTestRepository{bots: map[pa.BotKey]pa.Bot{key: b}, receipts: map[string]pa.Receipt{}, manager: key}
	f := &teamFixture{repo: r, fence: pa.Fence{Key: key, Project: scope, ConversationID: "chat", BindingID: "binding", Generation: 1}, now: time.Unix(1800000000, 0)}
	ids := 0
	coordinator, err := NewTeamCoordinator(TeamCoordinatorOptions{Repository: r, Check: func(ctx context.Context, fence pa.Fence, turn string) (ManagerContext, error) {
		if ctx.Err() != nil {
			return ManagerContext{}, ctx.Err()
		}
		if fence != f.fence || turn != "turn" {
			return ManagerContext{}, serviceFailure(pa.StaleGeneration, "turn")
		}
		b, e := r.Bot(ctx, fence.Key)
		if e != nil {
			return ManagerContext{}, e
		}
		m := ManagerContext{Caller: b, Grant: GrantFor(b), Project: b.Project}
		return m, m.check()
	}, Clock: func() time.Time { f.clockMu.Lock(); defer f.clockMu.Unlock(); return f.now }, ID: func() string { ids++; return fmt.Sprint("team-id-", ids) }})
	if err != nil {
		t.Fatal(err)
	}
	f.team = coordinator
	return f
}
func (f *teamFixture) mutateManager(change func(*pa.Bot)) {
	f.repo.mu.Lock()
	defer f.repo.mu.Unlock()
	b := f.repo.bots[f.repo.manager]
	change(&b)
	f.repo.bots[f.repo.manager] = b
}
func (f *teamFixture) call(t *testing.T, action string, args any) (TeamPlan, <-chan error, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	raw, _ := json.Marshal(args)
	done := make(chan error, 1)
	go func() { _, err := f.team.Call(ctx, f.fence, "turn", action, raw); done <- err }()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		p, err := f.team.Plans(t.Context(), f.fence.Key)
		if err != nil {
			t.Fatal(err)
		}
		if len(p) > 0 {
			return p[0], done, cancel
		}
		select {
		case err := <-done:
			t.Fatalf("no approval card: %v", err)
		default:
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("approval card did not appear")
	return TeamPlan{}, done, cancel
}
func teamTestSource() pa.SourceStatus {
	return pa.SourceStatus{MachineID: "m", State: pa.SourceOnline, Fresh: true, Readable: true, Writable: true, Access: pa.Interactive}
}
func teamNoRefusal(context.Context) error { return nil }

func TestTeamChangesWaitForThePersonAndCreatedByIsHostOwned(t *testing.T) {
	f := newTeamFixture(t)
	args := map[string]any{"idempotency_key": "create-one", "name": "Helper", "instructions": "Review changes"}
	plan, done, _ := f.call(t, "create_bot", args)
	if f.repo.writes != 0 {
		t.Fatal("a proposal changed the team before approval")
	}
	if !strings.Contains(plan.Detail, "Review changes") || !strings.Contains(plan.Detail, `"manageTeam": true`) {
		t.Fatalf("incomplete card: %s", plan.Detail)
	}
	state, err := f.team.Decide(t.Context(), f.fence.Key, plan.ID, true, teamTestSource(), teamNoRefusal)
	if err != nil || state != "applied" {
		t.Fatal(state, err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	b, err := f.repo.Bot(t.Context(), plan.Target)
	if err != nil || b.CreatedBy == nil || *b.CreatedBy != f.fence.Key || !b.ManageTeam || b.Agent != pa.Codex || b.PermissionPolicy != pa.Ask || b.Project != f.fence.Project {
		t.Fatalf("host inheritance: %+v %v", b, err)
	}
	raw, _ := json.Marshal(args)
	if _, err = f.team.Call(t.Context(), f.fence, "turn", "create_bot", raw); err != nil {
		t.Fatal(err)
	}
	if f.repo.writes != 1 {
		t.Fatal("a retried tool call created another Bot")
	}
	raw, _ = json.Marshal(map[string]any{"idempotency_key": "create-one", "name": "Changed request"})
	if _, err = f.team.Call(t.Context(), f.fence, "turn", "create_bot", raw); pa.Code(err) != pa.IdempotencyConflict {
		t.Fatal(err)
	}
}
func TestTeamDeclineRevocationAndExpiredCardsChangeNothing(t *testing.T) {
	for _, reason := range []string{"decline", "off", "grant-replaced", "project-changed", "expired", "human-revoked"} {
		t.Run(reason, func(t *testing.T) {
			f := newTeamFixture(t)
			plan, done, _ := f.call(t, "create_bot", map[string]any{"idempotency_key": reason, "name": "Helper"})
			allow := true
			check := teamNoRefusal
			switch reason {
			case "decline":
				allow = false
			case "off":
				f.mutateManager(func(b *pa.Bot) { b.ManageTeam = false; b.Version++ })
			case "grant-replaced":
				f.mutateManager(func(b *pa.Bot) { b.SettingsVersion = 2 })
			case "project-changed":
				f.mutateManager(func(b *pa.Bot) { b.Project.ProjectID = "other"; b.Version++ })
			case "expired":
				f.clockMu.Lock()
				f.now = plan.ExpiresAt
				f.clockMu.Unlock()
			case "human-revoked":
				check = func(context.Context) error { return serviceFailure(pa.Revoked, "device") }
			}
			state, err := f.team.Decide(t.Context(), f.fence.Key, plan.ID, allow, teamTestSource(), check)
			if reason == "decline" {
				if err != nil || state != "declined" {
					t.Fatal(state, err)
				}
			} else if err == nil {
				t.Fatal("a stale or revoked card applied")
			}
			if err = <-done; err == nil {
				t.Fatal("the managing Bot was told its refused proposal succeeded")
			}
			if f.repo.writes != 0 {
				t.Fatal("a refused card changed a Bot")
			}
		})
	}
}
func TestTeamToolsRefuseOtherProjectsAndAuthorityFieldsBeforeShowingACard(t *testing.T) {
	f := newTeamFixture(t)
	other := f.repo.bots[f.repo.manager]
	other.Key.BotID = "stranger"
	other.Project.ProjectID = "other"
	f.repo.bots[other.Key] = other
	for _, args := range []map[string]any{
		{"idempotency_key": "bad-project", "botId": "stranger", "name": "Changed"},
		{"idempotency_key": "grant", "botId": "manager", "manageTeam": true},
		{"idempotency_key": "created-by", "botId": "manager", "createdBy": "model"},
		{"idempotency_key": "agent", "botId": "manager", "agent": "claude"},
		{"idempotency_key": "delete", "botId": "manager", "delete": true},
	} {
		raw, _ := json.Marshal(args)
		if _, err := f.team.Call(t.Context(), f.fence, "turn", "update_bot", raw); err == nil {
			t.Fatalf("accepted authority-bearing request: %v", args)
		}
	}
	if p, _ := f.team.Plans(t.Context(), f.fence.Key); len(p) != 0 {
		t.Fatal("an invalid proposal got a card")
	}
	f.mutateManager(func(b *pa.Bot) { b.ManageTeam = false; b.Version++ })
	for _, action := range []string{"list_team", "create_bot", "update_bot"} {
		if _, err := f.team.Call(t.Context(), f.fence, "turn", action, json.RawMessage(`{}`)); pa.Code(err) != pa.ScopeDenied {
			t.Fatal(action, err)
		}
	}
}
func TestTeamCardLimitUsesUTF16AndNeverTruncates(t *testing.T) {
	f := newTeamFixture(t)
	plan, done, cancel := f.call(t, "create_bot", map[string]any{"idempotency_key": "short", "name": "Helper", "instructions": ""})
	overhead := len(utf16.Encode([]rune(plan.Detail)))
	cancel()
	<-done
	instructions := strings.Repeat("x", 2000-overhead-2) + "😀"
	plan, done, cancel = f.call(t, "create_bot", map[string]any{"idempotency_key": "exact", "name": "Helper", "instructions": instructions})
	if n := len(utf16.Encode([]rune(plan.Detail))); n != 2000 {
		t.Fatalf("card length %d", n)
	}
	if !strings.Contains(plan.Detail, instructions) {
		t.Fatal("card truncated instructions")
	}
	cancel()
	<-done
	raw, _ := json.Marshal(map[string]any{"idempotency_key": "over", "name": "Helper", "instructions": instructions + "x"})
	if _, err := f.team.Call(t.Context(), f.fence, "turn", "create_bot", raw); pa.Code(err) != pa.CapacityFull {
		t.Fatal(err)
	}
	if f.repo.writes != 0 {
		t.Fatal("a size check changed the team")
	}
}
func TestTeamListIncludesHiddenPeersInTheExactProject(t *testing.T) {
	f := newTeamFixture(t)
	b := f.repo.bots[f.repo.manager]
	b.Key.BotID = "hidden"
	b.Hidden = true
	f.repo.bots[b.Key] = b
	b.Key.BotID = "foreign"
	b.Project.ProjectID = "other"
	f.repo.bots[b.Key] = b
	answer, err := f.team.Call(t.Context(), f.fence, "turn", "list_team", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	bots := answer.(map[string]any)["bots"].([]TeamMember)
	if len(bots) != 2 {
		t.Fatalf("team list: %+v", bots)
	}
	for _, b := range bots {
		if b.ID == "foreign" {
			t.Fatal("another Project was listed")
		}
	}
}

func TestTeamOldCardCannotApproveARetryUnderANewGrant(t *testing.T) {
	f := newTeamFixture(t)
	args := map[string]any{"idempotency_key": "same-operation", "name": "Helper"}
	old, done, _ := f.call(t, "create_bot", args)
	f.mutateManager(func(b *pa.Bot) { b.ManageTeam = false; b.Version++ })
	if _, err := f.team.Decide(t.Context(), f.fence.Key, old.ID, true, teamTestSource(), teamNoRefusal); err == nil {
		t.Fatal("revoked card succeeded")
	}
	<-done
	f.mutateManager(func(b *pa.Bot) { b.ManageTeam = true; b.Version++ })
	fresh, done, _ := f.call(t, "create_bot", args)
	if fresh.ID == old.ID {
		t.Fatal("a retry reused an old approval capability")
	}
	if _, err := f.team.Decide(t.Context(), f.fence.Key, old.ID, true, teamTestSource(), teamNoRefusal); pa.Code(err) != pa.NotFound {
		t.Fatal("old card addressed a replacement plan", err)
	}
	if f.repo.writes != 0 {
		t.Fatal("old approval changed the team")
	}
	if _, err := f.team.Decide(t.Context(), f.fence.Key, fresh.ID, true, teamTestSource(), teamNoRefusal); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if f.repo.writes != 1 {
		t.Fatal(f.repo.writes)
	}
}

func TestTeamCancellationDuringAnApprovedMutationStaysUnknown(t *testing.T) {
	f := newTeamFixture(t)
	started, release := make(chan struct{}), make(chan struct{})
	// Deliberately retain an already accepted write after cancellation. The
	// coordination layer cannot promise it was withdrawn from another owner.
	f.repo.beforeCommit = func() { close(started); <-release }
	plan, done, cancel := f.call(t, "create_bot", map[string]any{"idempotency_key": "one", "name": "Helper"})
	decision := make(chan error, 1)
	go func() {
		_, err := f.team.Decide(context.Background(), f.fence.Key, plan.ID, true, teamTestSource(), teamNoRefusal)
		decision <- err
	}()
	<-started
	cancel()
	if err := <-done; pa.Code(err) != pa.OutcomeUnknown {
		close(release)
		<-decision
		t.Fatal("cancellation claimed no effect after mutation admission", err)
	}
	close(release)
	if err := <-decision; err != nil {
		t.Fatal(err)
	}
	if f.repo.writes != 1 {
		t.Fatal("fixture did not retain its accepted effect")
	}
}
