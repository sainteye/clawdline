package projectassistant

import (
	"context"
	"fmt"
	"testing"
	"time"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type serviceRepo struct {
	Repository
	bot          pa.Bot
	conversation pa.Conversation
	receipts     map[string]pa.Receipt
	commits      []Mutation
	reader       string
	query        pa.ListBots
	next         string
	read         pa.ReadCursor
	exports      int
	observations int
}

func (r *serviceRepo) Bot(context.Context, pa.BotKey) (pa.Bot, error) { return r.bot, nil }
func (r *serviceRepo) Conversation(context.Context, pa.BotKey, string) (pa.Conversation, error) {
	return r.conversation, nil
}
func (r *serviceRepo) Receipt(_ context.Context, c pa.Command) (*pa.Receipt, error) {
	prior, ok := r.receipts[string(c.Operation)+":"+c.IdempotencyKey]
	if !ok {
		return nil, nil
	}
	if _, err := pa.DecideReplay(c, &prior); err != nil {
		return nil, err
	}
	return &prior, nil
}
func (r *serviceRepo) Commit(_ context.Context, m Mutation) (pa.Receipt, error) {
	if m.Bot != nil {
		r.bot = *m.Bot
	}
	if m.Conversation != nil {
		r.conversation = *m.Conversation
	}
	r.commits = append(r.commits, m)
	r.receipts[string(m.Command.Operation)+":"+m.Command.IdempotencyKey] = m.Receipt
	return m.Receipt, nil
}
func (r *serviceRepo) ListForReader(_ context.Context, q pa.ListBots, reader string) (pa.BotPage, error) {
	r.reader = reader
	r.query = q
	return pa.BotPage{NextCursor: r.next}, nil
}
func (r *serviceRepo) MarkRead(_ context.Context, _ pa.Command, c pa.ReadCursor) (pa.ReadCursor, error) {
	r.read = c
	return c, nil
}
func (r *serviceRepo) Observe(_ context.Context, c pa.Command, id string, seq int64) (pa.Receipt, error) {
	r.observations++
	return pa.Receipt{Command: c, ID: id, EventSeq: seq, State: pa.EffectObserved}, nil
}
func (r *serviceRepo) Export(context.Context, pa.BotKey) (Export, error) {
	r.exports++
	return Export{Bot: r.bot}, nil
}

type serviceMemory struct {
	Memory
	write MemoryWrite
	calls int
}

func (m *serviceMemory) Save(_ context.Context, w MemoryWrite) (pa.MemoryRevision, error) {
	m.write = w
	m.calls++
	return pa.MemoryRevision{Revision: w.ExpectedRevision + 1}, nil
}

type serviceEvents struct{ EventFeed }
type serviceProjects struct {
	ProjectResolver
	resolution pa.ProjectResolution
	calls      int
}

func (p *serviceProjects) Resolve(context.Context, pa.ProjectSelection) (pa.ProjectResolution, error) {
	p.calls++
	return p.resolution, nil
}

type serviceRuntime struct {
	Runtime
	calls       int
	submissions []Submission
	answers     []AnswerRequest
	stops       []StopRequest
}

func (r *serviceRuntime) Submit(_ context.Context, s Submission) (pa.Receipt, error) {
	r.calls++
	r.submissions = append(r.submissions, s)
	return pa.Receipt{Command: s.Command, State: pa.EffectAccepted}, nil
}
func (r *serviceRuntime) Answer(_ context.Context, a AnswerRequest) (pa.Receipt, error) {
	r.calls++
	r.answers = append(r.answers, a)
	return pa.Receipt{Command: a.Command}, nil
}
func (r *serviceRuntime) SendNow(_ context.Context, a SendNowRequest) (pa.Receipt, error) {
	r.calls++
	return pa.Receipt{Command: a.Command}, nil
}
func (r *serviceRuntime) Stop(_ context.Context, a StopRequest) (pa.Receipt, error) {
	r.calls++
	r.stops = append(r.stops, a)
	return pa.Receipt{Command: a.Command}, nil
}

type serviceFixture struct {
	s         *Service
	repo      *serviceRepo
	runtime   *serviceRuntime
	memory    *serviceMemory
	projects  *serviceProjects
	authority Authority
	authCalls int
	ids       int
	key       pa.BotKey
}

func newServiceFixture(t *testing.T) *serviceFixture {
	t.Helper()
	f := &serviceFixture{repo: &serviceRepo{receipts: map[string]pa.Receipt{}}, runtime: &serviceRuntime{}, memory: &serviceMemory{}, projects: &serviceProjects{}, key: pa.BotKey{MachineID: "source-one", BotID: "bot-one"}}
	f.authority = Authority{PrincipalID: "principal-one", ReaderID: "principal-one", AuthorizationRevision: "grant-one", Feature: pa.FeatureState{Enabled: true}, Source: pa.SourceStatus{MachineID: "source-one", State: pa.SourceOnline, Readable: true, Writable: true, Fresh: true, Access: pa.Interactive}}
	f.projects.resolution = pa.ProjectResolution{Scope: pa.ProjectScope{MachineID: f.key.MachineID, ProjectID: "project-one", CanonicalID: "canonical-one", Kind: pa.RepositoryProject}, Directory: "/fixture/project", Current: true, Available: true}
	s, err := NewService(ServiceOptions{Repository: f.repo, Memory: f.memory, Events: &serviceEvents{}, Runtime: f.runtime, Projects: f.projects, Authorize: func(context.Context, pa.BotKey, bool) (Authority, error) { f.authCalls++; return f.authority, nil }, Clock: func() time.Time { return time.Unix(1700000000, 0) }, ID: func() string { f.ids++; return fmt.Sprintf("id-%d", f.ids) }})
	if err != nil {
		t.Fatal(err)
	}
	f.s = s
	return f
}
func (f *serviceFixture) command(op pa.Operation) pa.Command {
	return pa.Command{Key: f.key, Operation: op, IdempotencyKey: "command-one", ContentHash: "client-untrusted-hash"}
}
func (f *serviceFixture) settings() pa.CreateBot {
	return pa.CreateBot{Target: pa.ProjectSelection{MachineID: f.key.MachineID, ProjectID: "project-one"}, Name: "Assistant", Appearance: pa.Appearance{Shape: pa.Blob, Color: pa.EntryColor}, Agent: pa.Codex, PermissionPolicy: pa.Ask, Instructions: "Help with this project"}
}
func (f *serviceFixture) create(t *testing.T) pa.Bot {
	t.Helper()
	b, _, e := f.s.Create(context.Background(), f.command(pa.CreateOperation), f.settings())
	if e != nil {
		t.Fatal(e)
	}
	return b
}

func TestServiceCreateIsConfigurationOnlyAndRetryKeepsConversation(t *testing.T) {
	f := newServiceFixture(t)
	bot := f.create(t)
	ids := f.ids
	if f.runtime.calls != 0 || len(f.repo.commits) != 1 || f.projects.calls != 1 {
		t.Fatal("create performed runtime work or lost its single mutation")
	}
	if bot.Version != 1 || bot.Instructions.Revision != 1 || f.repo.conversation.Generation != 0 || f.repo.conversation.BindingID != "" {
		t.Fatal("new Bot should be configured but unbound")
	}
	f.projects.resolution.Available = false
	replayed, receipt, err := f.s.Create(context.Background(), f.command(pa.CreateOperation), f.settings())
	if err != nil || replayed.ConversationID != bot.ConversationID || receipt.State != pa.EffectAccepted || f.ids != ids || len(f.repo.commits) != 1 {
		t.Fatalf("retry: bot=%+v receipt=%+v err=%v", replayed, receipt, err)
	}
	settings := f.settings()
	settings.Name = "Different intent"
	if _, _, err = f.s.Create(context.Background(), f.command(pa.CreateOperation), settings); pa.Code(err) != pa.IdempotencyConflict {
		t.Fatalf("same client hash hid a conflict: %v", err)
	}
}

func TestServiceDoesNotTrustCrossMachineTargetOrOperation(t *testing.T) {
	f := newServiceFixture(t)
	settings := f.settings()
	settings.Target.MachineID = "other-machine"
	if _, _, err := f.s.Create(context.Background(), f.command(pa.CreateOperation), settings); pa.Code(err) != pa.ScopeDenied {
		t.Fatal(err)
	}
	if _, _, err := f.s.Create(context.Background(), f.command(pa.SendOperation), f.settings()); pa.Code(err) != pa.Invalid {
		t.Fatal(err)
	}
	if len(f.repo.commits) != 0 || f.runtime.calls != 0 || f.projects.calls != 0 {
		t.Fatal("refused create had effects")
	}
}

func TestServiceHistoryOnlyAllowsExportAndRefusesExecution(t *testing.T) {
	f := newServiceFixture(t)
	f.create(t)
	f.authority.Feature = pa.FeatureState{HasData: true}
	f.authority.Source.Access = pa.HistoryOnly
	if _, err := f.s.Export(context.Background(), f.key); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.Send(context.Background(), f.command(pa.SendOperation), pa.SendMessage{Fence: pa.Fence{Key: f.key}, Text: "hello"}); pa.Code(err) != pa.Disabled {
		t.Fatal(err)
	}
	if f.runtime.calls != 0 || f.repo.exports != 1 {
		t.Fatal("disabled runtime executed")
	}
	f.authority.Source.State = pa.SourceRevoked
	if _, err := f.s.Export(context.Background(), f.key); pa.Code(err) != pa.Revoked {
		t.Fatal(err)
	}
	if f.repo.exports != 1 {
		t.Fatal("revoked export reached storage")
	}
}

func TestServiceListBindsCursorToSourcePrincipalFilterAndGrant(t *testing.T) {
	f := newServiceFixture(t)
	f.repo.next = "store-cursor"
	q := pa.ListBots{MachineID: f.key.MachineID, PageSize: pa.MaxPageRows, Query: "search"}
	page, err := f.s.List(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if f.repo.reader != f.authority.ReaderID || page.Items == nil || len(page.Sources) != 1 {
		t.Fatal("roster has no principal-scoped projection")
	}
	q.Cursor = page.NextCursor
	if _, err = f.s.List(context.Background(), q); err != nil || f.repo.query.Cursor != "store-cursor" {
		t.Fatalf("valid continuation: %v", err)
	}
	f.authority.AuthorizationRevision = "grant-two"
	if _, err = f.s.List(context.Background(), q); pa.Code(err) != pa.CursorExpired {
		t.Fatal(err)
	}
	f.authority.AuthorizationRevision = "grant-one"
	q.Query = "changed"
	if _, err = f.s.List(context.Background(), q); pa.Code(err) != pa.CursorExpired {
		t.Fatal(err)
	}
	q.Query = "search"
	f.authority.PrincipalID = "other-principal"
	f.authority.ReaderID = "other-principal"
	if _, err = f.s.List(context.Background(), q); pa.Code(err) != pa.CursorExpired {
		t.Fatal(err)
	}
	if f.runtime.calls != 0 {
		t.Fatal("list initialized runtime")
	}
}

func TestServiceReadUsesTrustedPrincipalAndSendHashSurvivesGrantRenewal(t *testing.T) {
	f := newServiceFixture(t)
	cursor := pa.ReadCursor{Key: f.key, ConversationID: "conversation", ReaderID: "forged-reader", MessageSeq: 4}
	if _, err := f.s.MarkRead(context.Background(), f.command(pa.ReadOperation), cursor); err != nil {
		t.Fatal(err)
	}
	if f.repo.read.ReaderID != "principal-one" {
		t.Fatal("request controlled the read principal")
	}
	message := pa.SendMessage{Fence: pa.Fence{Key: f.key}, Text: "hello"}
	first, err := f.s.Send(context.Background(), f.command(pa.SendOperation), message)
	if err != nil {
		t.Fatal(err)
	}
	f.authority.AuthorizationRevision = "renewed"
	second, err := f.s.Send(context.Background(), f.command(pa.SendOperation), message)
	if err != nil {
		t.Fatal(err)
	}
	if first.Command.ContentHash != second.Command.ContentHash || len(first.Command.ContentHash) != 64 {
		t.Fatal("grant renewal changed operation identity")
	}
	f.authority.PrincipalID = "another-principal"
	third, err := f.s.Send(context.Background(), f.command(pa.SendOperation), message)
	if err != nil {
		t.Fatal(err)
	}
	if third.Command.ContentHash == first.Command.ContentHash {
		t.Fatal("different principal inherited operation identity")
	}
	message.Fence.Key.MachineID = "other"
	if _, err = f.s.Send(context.Background(), f.command(pa.SendOperation), message); pa.Code(err) != pa.ScopeDenied {
		t.Fatal(err)
	}
	if len(f.runtime.submissions) != 3 {
		t.Fatal("cross-source send reached runtime")
	}
}

func TestServiceReadableHistoryCanTrackItsOwnReadAndObservation(t *testing.T) {
	f := newServiceFixture(t)
	f.create(t)
	f.authority.Feature = pa.FeatureState{HasData: true}
	f.authority.Source.Access = pa.HistoryOnly
	f.authority.Source.Writable = false
	cursor := pa.ReadCursor{Key: f.key, ConversationID: f.repo.conversation.ID, ReaderID: "another-reader", MessageSeq: 1}
	if _, err := f.s.MarkRead(context.Background(), f.command(pa.ReadOperation), cursor); err != nil {
		t.Fatalf("readable history cannot track its own cursor: %v", err)
	}
	if f.repo.read.ReaderID != f.authority.ReaderID {
		t.Fatal("a read-only caller selected another principal's cursor")
	}
	if _, err := f.s.Observe(context.Background(), f.command(pa.ObserveOperation), "receipt", 1); err != nil || f.repo.observations != 1 {
		t.Fatalf("authenticated observation refused: %v", err)
	}
	if _, err := f.s.Send(context.Background(), f.command(pa.SendOperation), pa.SendMessage{Fence: pa.Fence{Key: f.key}, Text: "write"}); err == nil {
		t.Fatal("metadata authority broadened into a content write")
	}
	if f.runtime.calls != 0 || f.memory.calls != 0 {
		t.Fatal("read metadata started execution or changed memory")
	}
	f.authority.Source.State = pa.SourceOffline
	f.authority.Source.Fresh = false
	if _, err := f.s.MarkRead(context.Background(), f.command(pa.ReadOperation), cursor); pa.Code(err) != pa.Offline {
		t.Fatalf("stale/offline source accepted a new read write: %v", err)
	}
	if _, err := f.s.Observe(context.Background(), f.command(pa.ObserveOperation), "receipt", 2); pa.Code(err) != pa.Offline || f.repo.observations != 1 {
		t.Fatalf("stale/offline source accepted an observation: %v", err)
	}
	f.authority.Source.State = pa.SourceRevoked
	if _, err := f.s.MarkRead(context.Background(), f.command(pa.ReadOperation), cursor); pa.Code(err) != pa.Revoked {
		t.Fatalf("revoked history remained writable: %v", err)
	}
}

func TestServiceEditsOnlySettingsAndReplaysBeforeVersionCheck(t *testing.T) {
	f := newServiceFixture(t)
	before := f.create(t)
	settings := pa.EditBot{ExpectedVersion: 1, Name: "New name", Appearance: before.Appearance, Instructions: "Next idle turn"}
	a, r, err := f.s.Edit(context.Background(), f.command(pa.EditOperation), settings)
	if err != nil {
		t.Fatal(err)
	}
	b, replay, err := f.s.Edit(context.Background(), f.command(pa.EditOperation), settings)
	if err != nil || a.Version != 2 || b.Version != 2 || r.ID != replay.ID || len(f.repo.commits) != 2 {
		t.Fatalf("edit retry failed: %v", err)
	}
	if a.Agent != before.Agent || a.PermissionPolicy != before.PermissionPolicy || a.Instructions.Revision != 2 || f.runtime.calls != 0 {
		t.Fatal("settings edit changed execution")
	}
}

func TestServiceHumanMemoryCannotClaimForeignProvenance(t *testing.T) {
	f := newServiceFixture(t)
	bot := f.create(t)
	entry := pa.MemoryEntry{ID: "entry-one", Key: f.key, Project: bot.Project, Text: "Remember this literal fact", Revision: 99, Forgotten: true, Sources: []pa.MemorySource{{ID: "forged", Key: pa.BotKey{MachineID: "foreign", BotID: "private"}, Kind: "summary"}}}
	_, err := f.s.SaveMemory(context.Background(), MemoryWrite{Command: f.command(pa.MemorySaveOperation), ExpectedRevision: 1, Entry: entry})
	if err != nil {
		t.Fatal(err)
	}
	got := f.memory.write.Entry
	if got.Revision != 1 || got.Forgotten || len(got.Sources) != 1 || got.Sources[0].ID != "manual:entry-one" || got.Sources[0].Key != f.key || got.Sources[0].Kind != "human" {
		t.Fatalf("untrusted provenance survived: %+v", got)
	}
	entry.Project.ProjectID = "foreign"
	if _, err = f.s.SaveMemory(context.Background(), MemoryWrite{Command: f.command(pa.MemorySaveOperation), ExpectedRevision: 1, Entry: entry}); pa.Code(err) != pa.ScopeDenied {
		t.Fatal(err)
	}
	if f.memory.calls != 1 || f.runtime.calls != 0 {
		t.Fatal("invalid memory write had effects")
	}
}

func TestServiceChecksLiveAuthorityForPermissionStopAndCancelledRequests(t *testing.T) {
	f := newServiceFixture(t)
	f.authority.Source.State = pa.SourceRevoked
	if _, err := f.s.Answer(context.Background(), f.command(pa.AnswerOperation), pa.PermissionAnswer{Fence: pa.Fence{Key: f.key}}); pa.Code(err) != pa.Revoked {
		t.Fatal(err)
	}
	if _, err := f.s.Stop(context.Background(), StopRequest{Command: f.command(pa.StopOperation), Fence: pa.Fence{Key: f.key}}); pa.Code(err) != pa.Revoked {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := f.authCalls
	if _, err := f.s.Export(ctx, f.key); err != context.Canceled {
		t.Fatal(err)
	}
	if f.runtime.calls != 0 || f.authCalls != calls {
		t.Fatal("cancelled request had effects")
	}
}
