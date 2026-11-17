package botnetwork

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	wire "github.com/sainteye/clawdline/internal/domain/agenthandoff"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestPeerReceiptPublicationKeepsTerminalReplyAndRead(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "receipt.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	store, err := NewStore(ctx, db, "machine-b")
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"reply_stored", "unknown", "failed", "refused"} {
		t.Run(state, func(t *testing.T) {
			header := wire.BotHeader{Version: 1, RequestID: "request-" + state, Kind: "message", AuthorityID: "consent", AuthorityEpoch: 1, Source: wire.BotEndpoint{AccountID: "synthetic-account", MachineID: "machine-a", ProjectID: "project-a", BotID: "bot-a", ConversationID: "conversation-a"}, Target: wire.BotEndpoint{AccountID: "synthetic-account", MachineID: "machine-b", ProjectID: "project-b", BotID: "bot-b", ConversationID: "conversation-b"}}
			receipt, _, err := store.Claim(ctx, header)
			if err != nil {
				t.Fatal(err)
			}
			stale := receipt
			stale.State = "source_executing"
			stale.Read = false
			receipt.State = state
			receipt.Reply = "durable final reply"
			receipt.Read = false
			if state != "reply_stored" {
				receipt.Failure = &pa.Failure{Code: pa.OutcomeUnknown, Field: "retained-terminal"}
			}
			if err = store.Save(ctx, receipt); err != nil {
				t.Fatal(err)
			}
			initial, err := store.Receipt(ctx, header, header.RequestID)
			if err != nil || initial.Read {
				t.Fatalf("unpublished terminal forged a read: %+v %v", initial, err)
			}
			stale.Read = true
			if err = store.Save(ctx, stale); err != nil {
				t.Fatal(err)
			}
			stale.Read = false
			if err = store.Save(ctx, stale); err != nil {
				t.Fatal(err)
			}
			got, err := store.Receipt(ctx, header, header.RequestID)
			if err != nil || got.State != state || got.Reply != receipt.Reply || !got.Read || (receipt.Failure != nil && got.Failure == nil) {
				t.Fatalf("stale publication regressed terminal: %+v %v", got, err)
			}
		})
	}
}

func TestAssistantReceiptPublicationKeepsTerminalReplyAndRead(t *testing.T) {
	ctx := context.Background()
	f := newAssistantFixture(t)
	principal, _, err := f.service.Verify(ctx, f.call, f.body)
	if err != nil {
		t.Fatal(err)
	}
	receipt, _, err := f.store.ClaimAssistant(ctx, f.call, principal)
	if err != nil {
		t.Fatal(err)
	}
	stale := receipt
	stale.State = "source_executing"
	stale.Read = false
	receipt.State = "reply_stored"
	receipt.Reply = "durable assistant reply"
	receipt.Read = true
	if err = f.store.SaveAssistant(ctx, receipt); err != nil {
		t.Fatal(err)
	}
	if err = f.store.SaveAssistant(ctx, stale); err != nil {
		t.Fatal(err)
	}
	got, err := f.store.AssistantReceipt(ctx, f.call, principal, f.call.RequestID)
	if err != nil || got.State != "reply_stored" || got.Reply != receipt.Reply || !got.Read {
		t.Fatalf("stale assistant publication regressed terminal: %+v %v", got, err)
	}
}
