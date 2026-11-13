package projectassistanttest

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

type installer struct {
	version int
	fail    bool
}

func (i installer) Version() int { return i.version }
func (i installer) Install(ctx context.Context, db *sql.DB) error {
	if i.fail {
		return errors.New("injected install failure")
	}
	_, err := db.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS pa_test (id TEXT PRIMARY KEY)")
	return err
}
func TestBootstrapIsIsolatedAndInstallerOwned(t *testing.T) {
	ctx := context.Background()
	a, err := Bootstrap(ctx, installer{version: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := Bootstrap(ctx, installer{version: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if _, err = a.Exec("INSERT INTO pa_test VALUES ('only-a')"); err != nil {
		t.Fatal(err)
	}
	var n int
	if err = b.QueryRow("SELECT count(*) FROM pa_test").Scan(&n); err != nil || n != 0 {
		t.Fatalf("cross-test leak: %d, %v", n, err)
	}
	if err = (installer{version: 1}).Install(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err = a.QueryRow("SELECT count(*) FROM pa_test").Scan(&n); err != nil || n != 1 {
		t.Fatalf("reinstall lost data: %d, %v", n, err)
	}
	if err = a.QueryRow("PRAGMA user_version").Scan(&n); err != nil || n != 0 {
		t.Fatalf("changed host version: %d, %v", n, err)
	}
}
func TestBootstrapRejectsUnknownVersionAndFailure(t *testing.T) {
	for _, i := range []installer{{version: 2}, {version: 1, fail: true}} {
		if db, err := Bootstrap(context.Background(), i); err == nil || db != nil {
			t.Fatalf("accepted bad installer: %v %v", db, err)
		}
	}
}
