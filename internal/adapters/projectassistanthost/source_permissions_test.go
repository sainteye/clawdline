package projectassistanthost

import (
	"context"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/app/cloudops"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type hostSourceChoice struct {
	option pa.PermissionOption
	err    error
}

func hostSourceOffer(t *testing.T, f *hostFixture, key pa.BotKey) pa.PermissionOffer {
	t.Helper()
	var offer pa.PermissionOffer
	fixtureEventually(t, func() bool {
		x, err := f.h.store.Export(context.Background(), key)
		if err != nil {
			return false
		}
		for _, p := range x.Permissions {
			if p.Action != nil && p.Action.Title == "Synthetic source command" {
				offer = p
				return true
			}
		}
		return false
	})
	return offer
}

func askHostSource(t *testing.T, session *fixtureSession) (context.CancelFunc, <-chan hostSourceChoice) {
	t.Helper()
	tools, ok := session.q.Tools.(NativeWork)
	if !ok || tools.approveSource == nil {
		t.Fatal("actual Host did not bind its private source approval callback")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	done := make(chan hostSourceChoice, 1)
	go func() {
		option, err := tools.approveSource(ctx, pa.PermissionAction{Title: "Synthetic source command", ToolKind: "execute", Detail: "synthetic-command\n/synthetic/project"}, []pa.PermissionOption{{ID: "once", Kind: pa.AllowOnce, Allow: true}, {ID: "reject", Kind: pa.RejectOnce}})
		done <- hostSourceChoice{option, err}
	}()
	t.Cleanup(cancel)
	return cancel, done
}

func TestHostSourceCardUsesRealStoreReadViewAndNoVendorAnswer(t *testing.T) {
	f := newHostFixture(t)
	f.p.answers = make(chan [2]string, 1)
	f.o.Reader, f.o.ReadViewByteLimit = f.st.ProjectAssistantReader(), cloudops.ReadPlaintextBytesLimit
	f.start(t)
	f.enable(t)
	b := f.create(t, "source-card")
	if _, err := f.send(t, b, "source"); err != nil {
		t.Fatal(err)
	}
	_ = fixtureReceive(t, f.p.sent)
	session := fixtureReceive(t, f.p.sessions)
	_, done := askHostSource(t, session)
	offer := hostSourceOffer(t, f, b.Key)
	service, err := f.h.Service()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	view, err := service.View(ctx, pa.ReadViewRequest{Key: b.Key, ConversationID: b.ConversationID, Mode: pa.ReadViewSnapshot, PageSize: 100})
	if err != nil || view.View.Head.PendingPermissionCount != 1 {
		t.Fatalf("real view did not include the source-owned card: %+v %v", view.View.Head, err)
	}
	fixtureEventually(t, func() bool {
		usage := f.h.Usage()
		return len(usage.Sessions) == 1 && usage.Sessions[0].PendingPermissions == 1
	})
	// Saving memory is not a Forget: it must not invalidate this captured card.
	if _, err := service.SaveMemory(ctx, app.MemoryWrite{Command: pa.Command{Key: b.Key, Operation: pa.MemorySaveOperation, IdempotencyKey: "save-during-card"}, ExpectedRevision: 1, Entry: pa.MemoryEntry{ID: "topic", Key: b.Key, Project: b.Project, Kind: pa.TopicMemory, Title: "Synthetic", Summary: "Synthetic summary", Text: "Synthetic body"}}); err != nil {
		t.Fatal(err)
	}
	receipt, err := service.Answer(ctx, pa.Command{Key: b.Key, Operation: pa.AnswerOperation, IdempotencyKey: "source-answer"}, pa.PermissionAnswer{Fence: offer.Fence, TurnID: offer.TurnID, OfferID: offer.ID, OptionID: "once"})
	if err != nil || receipt.State != pa.EffectDelivered {
		t.Fatalf("source answer=%+v %v", receipt, err)
	}
	choice := fixtureReceive(t, done)
	stored, err := f.h.store.Permission(ctx, b.Key, offer.ID)
	if err != nil || choice.err != nil || choice.option.ID != "once" || stored.State != pa.PermissionResolved || stored.SelectedOptionID != "once" {
		t.Fatalf("choice was not backed by SQLite: %+v %+v %v", choice, stored, err)
	}
	select {
	case answer := <-f.p.answers:
		t.Fatal("source card was answered through the vendor", answer)
	default:
	}
	session.reply <- app.PromptResult{FinalText: "Source choice observed"}
	fixtureEventually(t, func() bool { return f.h.Usage().ActiveActors == 0 })
	tools := session.q.Tools.(NativeWork)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := tools.approveSource(ctx, pa.PermissionAction{Title: "Late", ToolKind: "execute"}, []pa.PermissionOption{{ID: "once", Kind: pa.AllowOnce, Allow: true}}); err == nil {
		t.Fatal("an ended native execution retained source approval authority")
	}
}

func TestHostForgetRefusesTheSourceChoiceBeforeReturningAuthority(t *testing.T) {
	f := newHostFixture(t)
	f.start(t)
	f.enable(t)
	b := f.create(t, "source-forget")
	ctx := context.Background()
	service, err := f.h.Service()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.SaveMemory(ctx, app.MemoryWrite{Command: pa.Command{Key: b.Key, Operation: pa.MemorySaveOperation, IdempotencyKey: "topic"}, ExpectedRevision: 1, Entry: pa.MemoryEntry{ID: "topic", Key: b.Key, Project: b.Project, Kind: pa.TopicMemory, Title: "Synthetic", Summary: "Synthetic", Text: "Synthetic body"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.send(t, b, "source"); err != nil {
		t.Fatal(err)
	}
	_ = fixtureReceive(t, f.p.sent)
	session := fixtureReceive(t, f.p.sessions)
	cancel, done := askHostSource(t, session)
	offer := hostSourceOffer(t, f, b.Key)
	if _, err := service.Forget(ctx, app.ForgetRequest{Command: pa.Command{Key: b.Key, Operation: pa.ForgetOperation, IdempotencyKey: "forget-during-source-card"}, Project: b.Project, ExpectedRevision: 2, SourceIDs: []string{"topic"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Answer(ctx, pa.Command{Key: b.Key, Operation: pa.AnswerOperation, IdempotencyKey: "late-source-answer"}, pa.PermissionAnswer{Fence: offer.Fence, TurnID: offer.TurnID, OfferID: offer.ID, OptionID: "once"}); pa.Code(err) != pa.ContextStale {
		t.Fatalf("forgotten command context was approved: %v", err)
	}
	cancel()
	if choice := fixtureReceive(t, done); choice.err == nil || choice.option.ID != "" {
		t.Fatal("Forget released source command authority", choice)
	}
}
