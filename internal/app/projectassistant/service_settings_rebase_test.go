package projectassistant

import (
	"context"
	"testing"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// racingRepo checks the expected version as the store does and lets a test
// land Bot activity just before a commit, as a turn finishing would.
type racingRepo struct {
	*serviceRepo
	beforeCommit func(*pa.Bot)
}

func (r *racingRepo) Commit(ctx context.Context, m Mutation) (pa.Receipt, error) {
	if hook := r.beforeCommit; hook != nil {
		r.beforeCommit = nil
		hook(&r.bot)
	}
	if prior, _ := r.serviceRepo.Receipt(ctx, m.Command); prior != nil {
		return *prior, nil
	}
	if m.ExpectedVersion != r.bot.Version {
		return pa.Receipt{}, &pa.Failure{Code: pa.StaleVersion, Field: "bot"}
	}
	return r.serviceRepo.Commit(ctx, m)
}

func settingsBase(b pa.Bot) *pa.EditBase {
	return &pa.EditBase{Name: b.Name, Appearance: b.Appearance, Instructions: b.Instructions.Text, Agent: b.Agent, PermissionPolicy: b.PermissionPolicy, Mode: b.Mode}
}

func racingFixture(t *testing.T) (*serviceFixture, *racingRepo, pa.Bot) {
	f := newServiceFixture(t)
	repo := &racingRepo{serviceRepo: f.repo}
	f.s.opts.Repository = repo
	return f, repo, f.create(t)
}

func TestServiceSettingsSaveSucceedsWhileTheBotReplies(t *testing.T) {
	f, repo, original := racingFixture(t)
	// The Bot replied while the person edited, and replies again during the save.
	repo.bot.Version += 3
	repo.beforeCommit = func(b *pa.Bot) { b.Version++ }
	cmd := f.command(pa.EditOperation)
	edit := pa.EditBot{ExpectedVersion: original.Version, Name: original.Name, Appearance: original.Appearance, Instructions: "Fixed instructions", Base: settingsBase(original)}
	got, r, err := f.s.Edit(context.Background(), cmd, edit)
	if err != nil {
		t.Fatalf("save refused while the Bot worked: %v", err)
	}
	if got.Instructions.Text != "Fixed instructions" || got.Version != original.Version+5 {
		t.Fatalf("saved %+v", got)
	}
	again, replay, err := f.s.Edit(context.Background(), cmd, edit)
	if err != nil || replay.ID != r.ID || again.Version != got.Version {
		t.Fatalf("replay = %v %v %v", replay, again.Version, err)
	}
}

func TestServiceSettingsSaveStopsRebasingAtItsBound(t *testing.T) {
	f, repo, original := racingFixture(t)
	var hook func(*pa.Bot)
	hook = func(b *pa.Bot) { b.Version++; repo.beforeCommit = hook }
	repo.beforeCommit = hook
	_, _, err := f.s.Edit(context.Background(), f.command(pa.EditOperation), pa.EditBot{ExpectedVersion: original.Version, Name: original.Name, Appearance: original.Appearance, Instructions: "x", Base: settingsBase(original)})
	if pa.Code(err) != pa.StaleVersion || pa.SettingsConflicts(err) != nil {
		t.Fatalf("endless activity: %v", err)
	}
	if repo.bot.Version != original.Version+pa.MaxSettingsRebaseAttempts {
		t.Fatalf("attempts = %d", repo.bot.Version-original.Version)
	}
}

func TestServiceConcurrentSettingsSavesKeepBothChanges(t *testing.T) {
	f, repo, original := racingFixture(t)
	other := pa.EditBot{ExpectedVersion: original.Version, Name: "Renamed elsewhere", Appearance: original.Appearance, Instructions: original.Instructions.Text, Base: settingsBase(original)}
	repo.beforeCommit = func(*pa.Bot) {
		otherCommand := f.command(pa.EditOperation)
		otherCommand.IdempotencyKey = "command-two"
		if _, _, err := f.s.Edit(context.Background(), otherCommand, other); err != nil {
			t.Errorf("other save: %v", err)
		}
	}
	got, _, err := f.s.Edit(context.Background(), f.command(pa.EditOperation), pa.EditBot{ExpectedVersion: original.Version, Name: original.Name, Appearance: original.Appearance, Instructions: "Mine", Base: settingsBase(original)})
	if err != nil || got.Name != "Renamed elsewhere" || got.Instructions.Text != "Mine" {
		t.Fatalf("one save ate the other: %+v %v", got, err)
	}
	conflicting := f.command(pa.EditOperation)
	conflicting.IdempotencyKey = "command-three"
	if _, _, err = f.s.Edit(context.Background(), conflicting, pa.EditBot{ExpectedVersion: original.Version, Name: original.Name, Appearance: original.Appearance, Instructions: "Stale draft", Base: settingsBase(original)}); len(pa.SettingsConflicts(err)) != 1 {
		t.Fatalf("stale draft of the same field: %v", err)
	}
}
