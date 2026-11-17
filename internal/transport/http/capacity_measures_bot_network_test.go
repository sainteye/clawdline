package http

import (
	"context"
	"testing"

	networkstore "github.com/sainteye/clawdline/internal/adapters/botnetwork"
)

func TestBotNetworkCapacityReadsRetainedMachineNamespace(t *testing.T) {
	f := newPAServerFixture(t, true)
	if r := f.s.botNetworkGrantMeasure(); !r.Known || r.Used != 0 || r.Err != "" {
		t.Fatalf("uninstalled grants: %+v", r)
	}
	if r := f.s.botNetworkReceiptMeasure(); !r.Known || r.Used != 0 || r.Err != "" {
		t.Fatalf("uninstalled: %+v", r)
	}
	db := f.s.store.ProjectAssistantDatabase()
	if _, err := networkstore.NewStore(context.Background(), db, "fixture-machine"); err != nil {
		t.Fatal(err)
	}
	for _, entry := range []struct{ machine, id string }{{"fixture-machine", "one"}, {"fixture-machine", "two"}, {"other-machine", "one"}} {
		if _, err := db.Exec(`INSERT INTO project_assistant_network_grants(machine,id,data) VALUES(?,?,?)`, entry.machine, entry.id, `{}`); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO project_assistant_network_receipts(machine,actor,id,digest,data) VALUES(?,?,?,?,?)`, entry.machine, "fixture-actor", entry.id, "fixture-digest", `{}`); err != nil {
			t.Fatal(err)
		}
	}
	if r := f.s.botNetworkGrantMeasure(); !r.Known || r.Used != 2 {
		t.Fatalf("retained grant machine namespace: %+v", r)
	}
	if r := f.s.botNetworkReceiptMeasure(); !r.Known || r.Used != 2 {
		t.Fatalf("retained per-machine occupancy: %+v", r)
	}
	if _, err := db.Exec(`DELETE FROM project_assistant_network_receipts WHERE machine='fixture-machine'`); err != nil {
		t.Fatal(err)
	}
	if r := f.s.botNetworkReceiptMeasure(); !r.Known || r.Used != 1 {
		t.Fatalf("remaining namespace: %+v", r)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if r := f.s.botNetworkReceiptMeasure(); r.Known || r.Err == "" {
		t.Fatalf("unreadable storage: %+v", r)
	}
	if r := f.s.botNetworkGrantMeasure(); r.Known || r.Err == "" {
		t.Fatalf("unreadable grant storage: %+v", r)
	}
}

func TestBotAssistantCapacityReadsAccountMachineNamespaces(t *testing.T) {
	f := newPAServerFixture(t, true)
	tables := []string{"project_assistant_gateway_grants", "project_assistant_gateway_receipts"}
	for _, table := range tables {
		if r := f.s.botAssistantRowsMeasure(table)(); !r.Known || r.Used != 0 || r.Err != "" {
			t.Fatalf("uninstalled %s: %+v", table, r)
		}
	}
	db := f.s.store.ProjectAssistantDatabase()
	if _, err := networkstore.NewAssistantStore(context.Background(), db, "fixture-account", "fixture-machine"); err != nil {
		t.Fatal(err)
	}
	for _, entry := range []struct{ account, machine, id string }{{"fixture-account", "fixture-machine", "one"}, {"fixture-account", "fixture-machine", "two"}, {"other-account", "fixture-machine", "one"}, {"fixture-account", "other-machine", "one"}} {
		if _, err := db.Exec(`INSERT INTO project_assistant_gateway_grants(account,machine,id,epoch,data) VALUES(?,?,?,?,?)`, entry.account, entry.machine, entry.id, 1, `{}`); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO project_assistant_gateway_receipts(account,machine,actor,id,digest,data) VALUES(?,?,?,?,?,?)`, entry.account, entry.machine, "fixture-actor", entry.id, "fixture-digest", `{}`); err != nil {
			t.Fatal(err)
		}
	}
	for _, table := range tables {
		if r := f.s.botAssistantRowsMeasure(table)(); !r.Known || r.Used != 2 {
			t.Fatalf("distinct account/machine namespaces %s: %+v", table, r)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	for _, table := range tables {
		if r := f.s.botAssistantRowsMeasure(table)(); r.Known || r.Err == "" {
			t.Fatalf("unreadable %s: %+v", table, r)
		}
	}
}
