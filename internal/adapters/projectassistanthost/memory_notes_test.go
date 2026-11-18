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
