package projectassistant

import (
	"context"
	"testing"
	"time"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type compactionMemoryMock struct {
	Memory
	current pa.MemoryRevision
	saved   []MemoryWrite
	forgots []ForgetRequest
}

func (m *compactionMemoryMock) Current(_ context.Context, _ pa.BotKey) (pa.MemoryRevision, error) {
	return m.current, nil
}

func (m *compactionMemoryMock) Save(_ context.Context, w MemoryWrite) (pa.MemoryRevision, error) {
	m.saved = append(m.saved, w)
	m.current.Revision++
	return m.current, nil
}

func (m *compactionMemoryMock) Forget(_ context.Context, f ForgetRequest) (pa.MemoryRevision, error) {
	m.forgots = append(m.forgots, f)
	m.current.Revision++
	return m.current, nil
}

type compactionMockRepo struct {
	Repository
	bot    pa.Bot
	export Export
}

func (r *compactionMockRepo) Bot(context.Context, pa.BotKey) (pa.Bot, error) {
	return r.bot, nil
}

func (r *compactionMockRepo) Export(context.Context, pa.BotKey) (Export, error) {
	return r.export, nil
}

func TestCompactBotMemoryIdleRefusal(t *testing.T) {
	key := pa.BotKey{MachineID: "m1", BotID: "b1"}
	engine := &Engine{}

	busyState := pa.IdleBotState{
		LastActiveAt: time.Now(),
		ActiveTurns:  1,
	}

	_, _, err := engine.CompactBotMemory(context.Background(), key, busyState, 10*time.Minute, 2)
	if pa.Code(err) != pa.Invalid {
		t.Fatalf("expected pa.Invalid on active bot, got %v", err)
	}
}

func TestCompactBotMemoryExecution(t *testing.T) {
	key := pa.BotKey{MachineID: "m1", BotID: "b1"}
	proj := pa.ProjectScope{MachineID: "m1", ProjectID: "p1"}
	bot := pa.Bot{Key: key, Project: proj, Name: "test-bot"}

	memStore := &compactionMemoryMock{
		current: pa.MemoryRevision{Key: key, Project: proj, Revision: 1},
	}

	s1 := pa.MemorySource{ID: "src-1", Key: key, Project: proj, Kind: "user", Revision: 1}
	entries := []pa.MemoryEntry{
		{ID: "e1", Key: key, Project: proj, Text: "pref: user prefers dark theme", Sources: []pa.MemorySource{s1}, Revision: 1},
		{ID: "e2", Key: key, Project: proj, Text: "pref: user prefers concise code", Sources: []pa.MemorySource{s1}, Revision: 1},
		{ID: "e3", Key: key, Project: proj, Text: "other: something else", Sources: []pa.MemorySource{s1}, Revision: 1},
	}

	repo := &compactionMockRepo{
		bot: bot,
		export: Export{
			Bot:           bot,
			MemoryEntries: entries,
		},
	}

	engine := &Engine{
		o: RuntimeOptions{
			Repository: repo,
			Memory:     memStore,
		},
	}

	idleState := pa.IdleBotState{
		LastActiveAt: time.Now().Add(-15 * time.Minute),
		ActiveTurns:  0,
		PendingWork:  0,
	}

	plan, rev, err := engine.CompactBotMemory(context.Background(), key, idleState, 10*time.Minute, 2)
	if err != nil {
		t.Fatalf("unexpected compaction error: %v", err)
	}

	if plan == nil {
		t.Fatal("expected compaction plan, got nil")
	}

	if len(plan.AggregatedEntries) != 1 {
		t.Fatalf("expected 1 aggregated entry, got %d", len(plan.AggregatedEntries))
	}

	if len(plan.TombstoneIDs) != 2 {
		t.Fatalf("expected 2 tombstone IDs, got %d", len(plan.TombstoneIDs))
	}

	// Verify revision incremented (1 initial + 1 saved + 1 forgot = 3)
	if rev.Revision != 3 {
		t.Fatalf("expected revision 3, got %d", rev.Revision)
	}

	if len(memStore.saved) != 1 {
		t.Fatalf("expected 1 saved entry, got %d", len(memStore.saved))
	}

	if len(memStore.forgots) != 1 {
		t.Fatalf("expected 1 forget call, got %d", len(memStore.forgots))
	}
}
