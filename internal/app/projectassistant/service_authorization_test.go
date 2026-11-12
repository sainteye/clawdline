package projectassistant

import (
	"context"
	"testing"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type effectMemory struct {
	*serviceMemory
	forgets int
}

func (m *effectMemory) Forget(_ context.Context, q ForgetRequest) (pa.MemoryRevision, error) {
	m.forgets++
	return pa.MemoryRevision{Revision: q.ExpectedRevision + 1}, nil
}

type effectAction struct {
	name string
	run  func(*serviceFixture) error
}

func serviceEffectActions() []effectAction {
	return []effectAction{
		{"create", func(f *serviceFixture) error {
			_, _, err := f.s.Create(context.Background(), f.command(pa.CreateOperation), f.settings())
			return err
		}},
		{"edit", func(f *serviceFixture) error {
			_, _, err := f.s.Edit(context.Background(), f.command(pa.EditOperation), pa.EditBot{ExpectedVersion: f.repo.bot.Version, Name: "Updated", Appearance: f.repo.bot.Appearance, Instructions: f.repo.bot.Instructions.Text})
			return err
		}},
		{"send", func(f *serviceFixture) error {
			_, err := f.s.Send(context.Background(), f.command(pa.SendOperation), pa.SendMessage{Fence: pa.Fence{Key: f.key}, Text: "A new message"})
			return err
		}},
		{"stop", func(f *serviceFixture) error {
			_, err := f.s.Stop(context.Background(), StopRequest{Command: f.command(pa.StopOperation), Fence: pa.Fence{Key: f.key}, TurnID: "active-turn"})
			return err
		}},
		{"answer", func(f *serviceFixture) error {
			_, err := f.s.Answer(context.Background(), f.command(pa.AnswerOperation), pa.PermissionAnswer{Fence: pa.Fence{Key: f.key}})
			return err
		}},
		{"read", func(f *serviceFixture) error {
			_, err := f.s.MarkRead(context.Background(), f.command(pa.ReadOperation), pa.ReadCursor{Key: f.key, ConversationID: f.repo.conversation.ID, MessageSeq: 1})
			return err
		}},
		{"observed", func(f *serviceFixture) error {
			_, err := f.s.Observe(context.Background(), f.command(pa.ObserveOperation), "receipt-one", 1)
			return err
		}},
		{"forget", func(f *serviceFixture) error {
			_, err := f.s.Forget(context.Background(), ForgetRequest{Command: f.command(pa.ForgetOperation), Project: f.repo.bot.Project, ExpectedRevision: 1, SourceIDs: []string{"manual:entry"}})
			return err
		}},
		{"memory", func(f *serviceFixture) error {
			_, err := f.s.SaveMemory(context.Background(), MemoryWrite{Command: f.command(pa.MemorySaveOperation), ExpectedRevision: 1, Entry: pa.MemoryEntry{ID: "entry", Key: f.key, Project: f.repo.bot.Project, Text: "Remember this"}})
			return err
		}},
	}
}

func effectFixture(t *testing.T, action effectAction) (*serviceFixture, func() int) {
	t.Helper()
	f := newServiceFixture(t)
	if action.name != "create" {
		f.create(t)
	}
	f.repo.commits = nil
	f.runtime.calls = 0
	f.repo.observations = 0
	f.repo.read = pa.ReadCursor{}
	memory := &effectMemory{serviceMemory: f.memory}
	f.s.opts.Memory = memory
	return f, func() int {
		n := len(f.repo.commits) + f.runtime.calls + f.memory.calls + memory.forgets + f.repo.observations
		if f.repo.read.MessageSeq != 0 {
			n++
		}
		return n
	}
}

func TestServiceRevalidatesAuthorityBeforeEffects(t *testing.T) {
	changes := []struct {
		name   string
		change func(*Authority)
	}{
		{"revoked", func(a *Authority) { a.Source.State = pa.SourceRevoked }},
		{"unreadable", func(a *Authority) { a.Source.Readable = false }},
		{"different_machine", func(a *Authority) { a.Source.MachineID = "other-source" }},
		{"different_principal", func(a *Authority) { a.PrincipalID = "other-principal" }},
		{"different_reader", func(a *Authority) { a.ReaderID = "other-reader" }},
		{"different_grant", func(a *Authority) { a.AuthorizationRevision = "replacement-grant" }},
	}
	for _, action := range serviceEffectActions() {
		t.Run(action.name+"/unchanged", func(t *testing.T) {
			f, effects := effectFixture(t, action)
			if err := action.run(f); err != nil || effects() != 1 {
				t.Fatalf("unchanged authority: err=%v effects=%d", err, effects())
			}
		})
		for _, change := range changes {
			t.Run(action.name+"/"+change.name, func(t *testing.T) {
				f, effects := effectFixture(t, action)
				entered, release := make(chan struct{}), make(chan struct{})
				original := f.s.opts.Authorize
				first := true
				f.s.opts.Authorize = func(ctx context.Context, key pa.BotKey, write bool) (Authority, error) {
					a, err := original(ctx, key, write)
					if first {
						first = false
						close(entered)
						<-release
					}
					return a, err
				}
				done := make(chan error, 1)
				go func() { done <- action.run(f) }()
				<-entered
				change.change(&f.authority)
				close(release)
				err := <-done
				if pa.Code(err) == "" || effects() != 0 {
					t.Fatalf("changed authority reached effect: err=%v effects=%d", err, effects())
				}
			})
		}
	}
}

type effectProjectBarrier struct {
	ProjectResolver
	entered chan struct{}
	release chan struct{}
}

func (p effectProjectBarrier) Resolve(ctx context.Context, selection pa.ProjectSelection) (pa.ProjectResolution, error) {
	resolution, err := p.ProjectResolver.Resolve(ctx, selection)
	close(p.entered)
	<-p.release
	return resolution, err
}

func TestServiceRevalidatesAfterProjectResolution(t *testing.T) {
	f := newServiceFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	f.s.opts.Projects = effectProjectBarrier{f.projects, entered, release}
	done := make(chan error, 1)
	go func() {
		_, _, err := f.s.Create(context.Background(), f.command(pa.CreateOperation), f.settings())
		done <- err
	}()
	<-entered
	f.authority.Source.State = pa.SourceRevoked
	close(release)
	if err := <-done; pa.Code(err) != pa.Revoked || len(f.repo.commits) != 0 || f.runtime.calls != 0 {
		t.Fatalf("revoked while resolving Project: err=%v commits=%d runtime=%d", err, len(f.repo.commits), f.runtime.calls)
	}
}

func TestServiceRevalidatesObservationFreshnessBeforeEffects(t *testing.T) {
	for _, action := range serviceEffectActions() {
		if action.name != "read" && action.name != "observed" {
			continue
		}
		for _, stale := range []bool{false, true} {
			t.Run(action.name+map[bool]string{true: "/stale", false: "/offline"}[stale], func(t *testing.T) {
				f, effects := effectFixture(t, action)
				original := f.s.opts.Authorize
				first := true
				f.s.opts.Authorize = func(ctx context.Context, key pa.BotKey, write bool) (Authority, error) {
					a, err := original(ctx, key, write)
					if first {
						first = false
						if stale {
							f.authority.Source.Fresh = false
						} else {
							f.authority.Source.State = pa.SourceOffline
						}
					}
					return a, err
				}
				if err := action.run(f); pa.Code(err) != pa.Offline || effects() != 0 {
					t.Fatalf("observation lost freshness: err=%v effects=%d", err, effects())
				}
			})
		}
	}
}

func TestServiceRevalidatesWritePolicyBeforeEffects(t *testing.T) {
	changes := []struct {
		name   string
		change func(*Authority)
	}{
		{"read_only", func(a *Authority) { a.Source.Writable = false }},
		{"stale", func(a *Authority) { a.Source.Fresh = false }},
		{"disabled", func(a *Authority) { a.Feature = pa.FeatureState{HasData: true} }},
	}
	for _, action := range serviceEffectActions() {
		if action.name == "read" || action.name == "observed" {
			continue
		}
		for _, change := range changes {
			t.Run(action.name+"/"+change.name, func(t *testing.T) {
				f, effects := effectFixture(t, action)
				original := f.s.opts.Authorize
				first := true
				f.s.opts.Authorize = func(ctx context.Context, key pa.BotKey, write bool) (Authority, error) {
					a, err := original(ctx, key, write)
					if first {
						first = false
						change.change(&f.authority)
					}
					return a, err
				}
				if err := action.run(f); pa.Code(err) == "" || effects() != 0 {
					t.Fatalf("write lost authority: err=%v effects=%d", err, effects())
				}
			})
		}
	}
}

func TestServiceRevalidatesCancelledPreparation(t *testing.T) {
	f := newServiceFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	f.s.opts.Projects = effectProjectBarrier{f.projects, entered, release}
	done := make(chan error, 1)
	go func() {
		_, _, err := f.s.Create(ctx, f.command(pa.CreateOperation), f.settings())
		done <- err
	}()
	<-entered
	cancel()
	close(release)
	if err := <-done; err != context.Canceled || len(f.repo.commits) != 0 || f.runtime.calls != 0 {
		t.Fatalf("cancelled while resolving Project: err=%v commits=%d runtime=%d", err, len(f.repo.commits), f.runtime.calls)
	}
}
