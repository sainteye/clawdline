package projectassistanthost

import (
	"context"
	"testing"
	"time"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// A write transaction held elsewhere in the daemon (a Bot turn, the broker's
// landing, workflow reconciliation) must not stall the feature inspection that
// every Bot read performs. Before 2026-11-23 the inspection queued for the
// single writer connection, so the roster read failed on the phone.
func TestStatusAnswersWhileTheWriterIsHeld(t *testing.T) {
	f := newHostFixture(t)
	f.o.Reader = f.st.ProjectAssistantReader()
	f.start(t)
	f.enable(t)

	tx, err := f.o.Database.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.Exec(`UPDATE project_assistant_schema SET version=version WHERE id=1`); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	started := time.Now()
	state, err := f.h.Status(ctx)
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("Status with the writer held: %v after %v", err, elapsed)
	}
	if !state.Enabled {
		t.Fatalf("Status with the writer held reported disabled: %+v", state)
	}
	if elapsed > time.Second {
		t.Fatalf("Status waited %v for the writer connection", elapsed)
	}
}

// The Bots roster's group previews read through the same path and must also
// answer while another writer holds the connection.
func TestGroupPreviewsAnswerWhileTheWriterIsHeld(t *testing.T) {
	f := newHostFixture(t)
	f.o.Reader = f.st.ProjectAssistantReader()
	f.start(t)
	f.enable(t)
	service, err := f.h.Service()
	if err != nil {
		t.Fatal(err)
	}

	tx, err := f.o.Database.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.Exec(`UPDATE project_assistant_schema SET version=version WHERE id=1`); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	started := time.Now()
	_, err = service.GroupPreviews(ctx, "source", 1<<16)
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("GroupPreviews with the writer held: %v after %v", err, elapsed)
	}
	if elapsed > time.Second {
		t.Fatalf("GroupPreviews waited %v for the writer connection", elapsed)
	}
}

// A snapshot roster still uses List on sources whose roster stream is
// unavailable. It needs to answer while another transaction owns the writer.
func TestBotListAndSendReceiptAnswerWhileTheWriterIsHeld(t *testing.T) {
	f := newHostFixture(t)
	f.o.Reader = f.st.ProjectAssistantReader()
	f.start(t)
	f.enable(t)
	service, err := f.h.Service()
	if err != nil {
		t.Fatal(err)
	}

	tx, err := f.o.Database.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err = service.List(ctx, pa.ListBots{MachineID: f.o.MachineID, PageSize: 10}); err != nil {
		t.Fatalf("Bot list waited for the writer: %v", err)
	}
	key := pa.BotKey{MachineID: f.o.MachineID, BotID: "missing"}
	if _, err = f.h.store.SendReceipt(ctx, key, "conversation", "send"); pa.Code(err) != pa.NotFound {
		t.Fatalf("an absent receipt should return not found while the writer is held: %v", err)
	}
}
