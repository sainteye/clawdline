package botdevserverstore

import (
	"context"
	"testing"

	servers "github.com/sainteye/clawdline/internal/app/botdevserver"
)

// A ledger written before the processes table existed is upgraded in place,
// and a running attempt's PID and start time survive a reopen.
func TestARunningAttemptKeepsItsProcessMarkAcrossReopenAndUpgrade(t *testing.T) {
	db, _ := testDatabase(t)
	ctx := context.Background()
	if err := Install(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP TABLE bot_devserver_processes`); err != nil { // A version-1 ledger.
		t.Fatal(err)
	}
	s := ready(t, db)
	i := identity()
	if err := s.Save(ctx, 0, servers.Record{Identity: i, Version: 1, State: servers.Accepted}); err != nil {
		t.Fatal(err)
	}
	if err := Install(ctx, db); err != nil {
		t.Fatalf("an older ledger was not upgraded: %v", err)
	}
	mark := servers.Mark{PID: 4242, Started: 1_800_000_000}
	if err := s.Save(ctx, 1, servers.Record{Identity: i, Version: 2, State: servers.Running, Mark: mark}); err != nil {
		t.Fatal(err)
	}
	reopened, err := New(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	r, err := reopened.Load(ctx, i)
	if err != nil || r.State != servers.Running || r.Mark != mark {
		t.Fatalf("mark lost across reopen: %+v %v", r, err)
	}
	if err := reopened.Save(ctx, 2, servers.Record{Identity: i, Version: 3, State: servers.Stopped, Mark: mark}); err != nil {
		t.Fatal(err)
	}
}
