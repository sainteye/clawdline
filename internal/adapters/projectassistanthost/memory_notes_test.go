package projectassistanthost

import (
	"context"
	"strings"
	"testing"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestHostTopicMemoryBodyRequiresAnOwnedDetailRead(t *testing.T) {
	f := newHostFixture(t)
	f.start(t)
	f.enable(t)
	b := f.create(t, "topic-reader")
	other := f.create(t, "other-topic-reader")
	ctx := context.Background()
	service, err := f.h.Service()
	if err != nil {
		t.Fatal(err)
	}
	for _, bot := range []pa.Bot{b, other} {
		id := "topic"
		if bot.Key == other.Key {
			id = "foreign-topic"
		}
		_, err = service.SaveMemory(ctx, app.MemoryWrite{Command: pa.Command{Key: bot.Key, Operation: pa.MemorySaveOperation, IdempotencyKey: "save-topic"}, ExpectedRevision: 1, Entry: pa.MemoryEntry{ID: id, Key: bot.Key, Project: bot.Project, Kind: pa.TopicMemory, Title: "Release checklist", Summary: "Deployment readiness", Text: "BODY-ONLY-TOPIC-DETAIL"}})
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err = f.send(t, b, "read"); err != nil {
		t.Fatal(err)
	}
	prompt := fixtureReceive(t, f.p.sent)
	session := fixtureReceive(t, f.p.sessions)
	if strings.Contains(prompt.Context.Text, "BODY-ONLY") || !strings.Contains(prompt.Context.Text, "Release checklist") {
		t.Fatal("prompt received a full topic body", prompt.Context.Text)
	}
	entries, err := session.q.Tools.Memory(ctx)
	if err != nil || len(entries) != 1 || entries[0].Text != "Deployment readiness" {
		t.Fatal("native index exposed a body", entries, err)
	}
	detailTools, ok := session.q.Tools.(app.MemoryDetailTools)
	if !ok {
		t.Fatal("owned memory detail tool absent")
	}
	detail, err := detailTools.MemoryDetail(ctx, "topic")
	if err != nil || detail.Text != "BODY-ONLY-TOPIC-DETAIL" || detail.Title != "Release checklist" {
		t.Fatal("owned detail read", detail, err)
	}
	if _, err = detailTools.MemoryDetail(ctx, "foreign-topic"); pa.Code(err) != pa.NotFound {
		t.Fatal("detail crossed Bot ownership", err)
	}
	if err = session.q.Tools.EditMemory(ctx, "short-edit", app.MemoryEdit{Old: "Deployment readiness", New: "Lost body"}); pa.Code(err) != pa.NotFound {
		t.Fatal("short-fact edit rewrote a note index", err)
	}
	session.reply <- app.PromptResult{FinalText: "Viewed"}
	fixtureEventually(t, func() bool { return f.h.Usage().ActiveActors == 0 })
	if _, err = service.Forget(ctx, app.ForgetRequest{Command: pa.Command{Key: b.Key, Operation: pa.ForgetOperation, IdempotencyKey: "forget-topic"}, Project: b.Project, ExpectedRevision: 2, SourceIDs: []string{"topic"}}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.send(t, b, "after-forget"); err != nil {
		t.Fatal(err)
	}
	after := fixtureReceive(t, f.p.sent)
	next := fixtureReceive(t, f.p.sessions)
	if next.q.Resume || strings.Contains(after.Context.Text, "Release checklist") || strings.Contains(after.Context.Text, "BODY-ONLY") {
		t.Fatal("forget retained note context or opaque resume", next.q.Resume, after.Context.Text)
	}
	remaining, err := next.q.Tools.Memory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if matches, _ := app.SearchMemory(remaining, "Release checklist"); len(matches) != 0 {
		t.Fatal("forgotten note remained searchable", matches)
	}
	if _, err = next.q.Tools.(app.MemoryDetailTools).MemoryDetail(ctx, "topic"); pa.Code(err) != pa.NotFound {
		t.Fatal("forgotten body remained available", err)
	}
	next.reply <- app.PromptResult{FinalText: "Forgotten"}
	fixtureEventually(t, func() bool { return f.h.Usage().ActiveActors == 0 })
}

// This fixture loses the first successful tool acknowledgement after SQLite
// commits, forcing the caller to reuse the exact same request and key.
type topicLostReplyTools struct {
	app.NativeTools
	dropped bool
}

func (w *topicLostReplyTools) EditMemory(ctx context.Context, id string, edit app.MemoryEdit) error {
	if err := w.NativeTools.EditMemory(ctx, id, edit); err != nil {
		return err
	}
	if !w.dropped {
		w.dropped = true
		return failure(pa.ExecutionEffectUnknown, "synthetic_reply_loss")
	}
	return nil
}

func TestHostTopicNoteEditsKeepRevisionAndReplayIdentity(t *testing.T) {
	f := newHostFixture(t)
	f.start(t)
	f.enable(t)
	bot := f.create(t, "topic-writer")
	ctx := context.Background()
	if _, err := f.send(t, bot, "write-topic"); err != nil {
		t.Fatal(err)
	}
	prompt := fixtureReceive(t, f.p.sent)
	session := fixtureReceive(t, f.p.sessions)
	indexTools := session.q.Tools.(app.MemoryIndexTools)
	index, err := indexTools.MemoryIndex(ctx)
	if err != nil || index.Revision != 1 || len(index.Entries) != 0 {
		t.Fatal("initial index", index, err)
	}
	create := app.MemoryEdit{Note: &app.MemoryNoteEdit{Title: "Release checklist", Summary: "Readiness", Body: "BODY-ONLY-NATIVE-NOTE", ExpectedRevision: 1}}
	lost := &topicLostReplyTools{NativeTools: session.q.Tools}
	if err = lost.EditMemory(ctx, "create-note", create); pa.Code(err) != pa.ExecutionEffectUnknown {
		t.Fatal("lost acknowledgement did not retain unknown outcome", err)
	}
	index, err = indexTools.MemoryIndex(ctx)
	if err != nil || index.Revision != 2 || len(index.Entries) != 1 || index.Entries[0].Text != "Readiness" {
		t.Fatal("created index", index, err)
	}
	id := index.Entries[0].ID
	if err = lost.EditMemory(ctx, "create-note", create); err != nil {
		t.Fatal("exact retry", err)
	}
	changed := create
	value := *create.Note
	value.Body = "Different payload"
	changed.Note = &value
	if err = session.q.Tools.EditMemory(ctx, "create-note", changed); pa.Code(err) != pa.IdempotencyConflict {
		t.Fatal("changed retry", err)
	}
	stale := app.MemoryEdit{Note: &app.MemoryNoteEdit{EntryID: id, Title: "Revised checklist", Summary: "Revised index", Body: "Revised body", ExpectedRevision: 1}}
	if err = session.q.Tools.EditMemory(ctx, "stale-note", stale); pa.Code(err) != pa.StaleVersion {
		t.Fatal("stale edit", err)
	}
	stale.Note.ExpectedRevision = 2
	if err = session.q.Tools.EditMemory(ctx, "update-note", stale); err != nil {
		t.Fatal(err)
	}
	if err = session.q.Tools.EditMemory(ctx, "create-note", create); err != nil {
		t.Fatal("old exact retry after later save", err)
	}
	index, err = indexTools.MemoryIndex(ctx)
	if err != nil || index.Revision != 3 || len(index.Entries) != 1 || index.Entries[0].Title != "Revised checklist" {
		t.Fatal("retry changed current note", index, err)
	}
	details, err := session.q.Tools.(app.MemoryDetailTools).MemoryDetail(ctx, id)
	if err != nil || details.Text != "Revised body" {
		t.Fatal("replacement body", details, err)
	}
	if err = session.q.Tools.EditMemory(ctx, "foreign-note", app.MemoryEdit{Note: &app.MemoryNoteEdit{EntryID: "another-bot-note", Title: "Foreign", Summary: "Foreign", Body: "Foreign", ExpectedRevision: 3}}); pa.Code(err) != pa.NotFound {
		t.Fatal("unknown note update", err)
	}
	if _, err = f.h.store.Save(ctx, app.MemoryWrite{Command: pa.Command{Key: bot.Key, Operation: pa.MemorySaveOperation, IdempotencyKey: "derived-copy", ContentHash: strings.Repeat("a", 64)}, ExpectedRevision: 3, Entry: pa.MemoryEntry{ID: "dependent-fact", Key: bot.Key, Project: bot.Project, Text: "Dependent synthetic fact", Sources: index.Entries[0].Sources, Revision: 3}}); err != nil {
		t.Fatal(err)
	}
	if err = session.q.Tools.EditMemory(ctx, "forget-note", app.MemoryEdit{Note: &app.MemoryNoteEdit{EntryID: id, ExpectedRevision: 4, Forget: true}}); err != nil {
		t.Fatal("forget note", err)
	}
	// Engine replay returns the original immutable result, even after forgetting.
	// Host turn checks remain independent and may already prohibit further tools.
	result, err := f.h.engine.EditMemory(ctx, bot.Key, prompt.Turn.ID, "create-note", create)
	if err != nil || result.Revision != 2 {
		t.Fatal("original receipt was not preserved", result, err)
	}
	current, err := f.h.store.MemoryEntries(ctx, bot.Key)
	if err != nil || len(current) != 0 {
		t.Fatal("replay resurrected a forgotten note", current, err)
	}
	if _, err = f.h.engine.MemoryDetail(ctx, bot.Key, id); pa.Code(err) != pa.NotFound {
		t.Fatal("forgotten body readable", err)
	}
	session.reply <- app.PromptResult{FinalText: "Done"}
	fixtureEventually(t, func() bool { return f.h.Usage().ActiveActors == 0 })
}
