package projectassistantstore

import (
	"context"
	"testing"
	"time"

	host "github.com/sainteye/clawdline/internal/adapters/store"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// The group list and a room's history are person-facing reads polled about
// every four seconds. They must not queue behind a write that owns Store's
// single writer connection; the query-only pool sees the committed state.
func TestGroupReadsDoNotWaitForTheWriter(t *testing.T) {
	ctx := context.Background()
	st, err := host.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	db := st.ProjectAssistantDatabase()
	if err = (Installer{}).Install(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err = (GroupInstaller{}).Install(ctx, db); err != nil {
		t.Fatal(err)
	}
	groups := NewGroupStore(db).WithReader(st.ProjectAssistantReader())

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE project_assistant_group_schema SET version=version WHERE id=1`); err != nil {
		t.Fatal(err)
	}

	reads := map[string]func(context.Context) error{
		"previews": func(ctx context.Context) error {
			_, err := groups.GroupPreviews(ctx, "source", 1<<20)
			return err
		},
		"list": func(ctx context.Context) error {
			_, err := groups.Groups(ctx, "source")
			return err
		},
		"history": func(ctx context.Context) error {
			_, err := groups.History(ctx, pa.BotKey{MachineID: "source", BotID: "absent"}, 0, 10)
			if pa.Code(err) == pa.NotFound {
				return nil
			}
			return err
		},
	}
	for name, read := range reads {
		wait, cancel := context.WithTimeout(ctx, time.Second)
		started := time.Now()
		err := read(wait)
		cancel()
		if err != nil {
			t.Errorf("%s waited for the writer and failed after %v: %v", name, time.Since(started), err)
		}
	}
}
