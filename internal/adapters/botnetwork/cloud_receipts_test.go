package botnetwork

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	wire "github.com/sainteye/clawdline/internal/domain/agenthandoff"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	_ "modernc.org/sqlite"
)

func TestBotCloudDurableTransportCorrelationPrecedesACKAndNeverEvicts(t *testing.T) {
	ctx := context.Background()
	db, e := sql.Open("sqlite", filepath.Join(t.TempDir(), "receipts.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	s, e := NewStore(ctx, db, "machine")
	if e != nil {
		t.Fatal(e)
	}
	if e = s.InstallCloudReceipts(ctx); e != nil {
		t.Fatal(e)
	}
	r := CloudReceipt{Header: wire.BotHeader{RequestID: "original", Kind: "reply"}, HeaderDigest: "header", FrameDigest: "frame", DeliveryID: "delivery", Connection: "socket/1", Status: "target_received"}
	if fresh, e := s.RetainCloud(ctx, "receive", r); e != nil || !fresh {
		t.Fatal(fresh, e)
	}
	if fresh, e := s.RetainCloud(ctx, "receive", r); e != nil || fresh {
		t.Fatal("exact identity replay", fresh, e)
	}
	for _, mutate := range []func(*CloudReceipt){func(q *CloudReceipt) { q.Connection = "socket/2" }, func(q *CloudReceipt) { q.FrameDigest = "changed" }, func(q *CloudReceipt) { q.DeliveryID = "changed" }, func(q *CloudReceipt) { q.Header.Kind = "message" }} {
		q := r
		mutate(&q)
		if _, e = s.RetainCloud(ctx, "receive", q); pa.Code(e) != pa.IdempotencyConflict {
			t.Fatal("correlation moved", e)
		}
	}
	for i := 1; i < pa.MaxNetworkCloudReceipts; i++ {
		q := r
		q.Header.RequestID = fmt.Sprint("operation-", i)
		if _, e = s.RetainCloud(ctx, "receive", q); e != nil {
			t.Fatal(e)
		}
	}
	q := r
	q.Header.RequestID = "at-cap"
	if _, e = s.RetainCloud(ctx, "receive", q); pa.Code(e) != pa.CapacityFull {
		t.Fatal("cap", e)
	}
	if fresh, e := s.RetainCloud(ctx, "receive", r); e != nil || fresh {
		t.Fatal("full store lost retained fact", fresh, e)
	}
}
func TestBotCloudTransportStatusCannotRegressOrForgeCorrelation(t *testing.T) {
	ctx := context.Background()
	db, _ := sql.Open("sqlite", filepath.Join(t.TempDir(), "receipts.db"))
	defer db.Close()
	s, _ := NewStore(ctx, db, "machine")
	_ = s.InstallCloudReceipts(ctx)
	r := CloudReceipt{Header: wire.BotHeader{RequestID: "id"}, HeaderDigest: "header", FrameDigest: "frame", Status: "unknown"}
	if _, e := s.RetainCloud(ctx, "send", r); e != nil {
		t.Fatal(e)
	}
	if e := s.RecordCloudStatus(ctx, "id", "header", "forged", "", "target_delivered"); pa.Code(e) != pa.IdempotencyConflict {
		t.Fatal(e)
	}
	for _, state := range []string{"target_delivered", "relay_accepted"} {
		if e := s.RecordCloudStatus(ctx, "id", "header", "frame", "", state); e != nil {
			t.Fatal(e)
		}
	}
	var raw []byte
	if e := db.QueryRow(`SELECT data FROM project_assistant_network_cloud_receipts WHERE id='id'`).Scan(&raw); e != nil {
		t.Fatal(e)
	}
	_ = json.Unmarshal(raw, &r)
	if r.Status != "target_delivered" {
		t.Fatal("delivery regressed", r)
	}
	if revision, pending, e := s.LeaseRevision(ctx); e != nil || revision != 0 || pending {
		t.Fatal(revision, pending, e)
	}
	if e := s.SaveLeaseRevision(ctx, 9, true); e != nil {
		t.Fatal(e)
	}
	if revision, pending, e := s.LeaseRevision(ctx); e != nil || revision != 9 || !pending {
		t.Fatal(revision, pending, e)
	}
}
