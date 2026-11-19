package botdevserverstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"

	servers "github.com/sainteye/clawdline/internal/app/botdevserver"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestExactRulePersistsRevokesAndRejectsStaleOrForeignCards(t *testing.T) {
	db, path := testDatabase(t)
	c := readyCatalog(t, db)
	ctx := context.Background()
	saved := savedDefinition("rules")
	saved.Spec.ForgottenThrough = 3
	if err := c.Create(ctx, saved); err != nil {
		t.Fatal(err)
	}
	empty, err := c.Rule(ctx, saved)
	if err != nil || empty.Version != 0 || empty.Decision != "" {
		t.Fatal(empty, err)
	}
	allowed, err := c.SaveRule(ctx, empty, servers.Allow)
	if err != nil || allowed.Version != 1 {
		t.Fatal(allowed, err)
	}
	wire, err := allowed.PermissionRule()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := servers.ParseRule(wire)
	if err != nil || parsed != allowed {
		t.Fatal(parsed, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	c, err = NewCatalog(ctx, reopened)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := c.Rule(ctx, saved); err != nil || got != allowed {
		t.Fatal(got, err)
	}
	for _, change := range []func(*servers.Rule){
		func(r *servers.Rule) { r.Spec.Key.BotID = "foreign" },
		func(r *servers.Rule) { r.Spec.Project.CanonicalID = "other" },
		func(r *servers.Rule) { r.Spec.Command += " --extra" },
		func(r *servers.Rule) { r.Spec.ForgottenThrough++ },
	} {
		bad := allowed
		change(&bad)
		if _, err := c.SaveRule(ctx, bad, servers.Deny); pa.Code(err) != pa.ScopeDenied {
			t.Fatal(err)
		}
	}
	revoked, err := c.SaveRule(ctx, parsed, "")
	if err != nil || revoked.Version != 2 || revoked.Decision != "" {
		t.Fatal(revoked, err)
	}
	if _, err := c.SaveRule(ctx, allowed, servers.Allow); pa.Code(err) != pa.StaleVersion {
		t.Fatal(err)
	}
	if _, err := c.SaveRule(ctx, empty, servers.Deny); pa.Code(err) != pa.StaleVersion {
		t.Fatal(err)
	}
	denied, err := c.SaveRule(ctx, revoked, servers.Deny)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Remove(ctx, saved); err != nil {
		t.Fatal(err)
	}
	if _, err := c.SaveRule(ctx, denied, servers.Allow); pa.Code(err) != pa.NotFound {
		t.Fatal(err)
	}
	var decision string
	if err := reopened.QueryRow(`SELECT rule_decision FROM bot_devserver_definitions WHERE id=?`, saved.ID).Scan(&decision); err != nil || decision != "" {
		t.Fatal(decision, err)
	}
}

func TestLegacyCatalogMigrationIsExplicitAtomicAndDoesNotGrantCommands(t *testing.T) {
	for _, broken := range []bool{false, true} {
		t.Run(map[bool]string{false: "valid", true: "malformed"}[broken], func(t *testing.T) {
			db, _ := testDatabase(t)
			ctx := context.Background()
			if err := Install(ctx, db); err != nil {
				t.Fatal(err)
			}
			for _, ddl := range []string{
				`CREATE TABLE bot_devserver_catalog_schema (id INTEGER PRIMARY KEY CHECK(id=1), version INTEGER NOT NULL)`,
				`CREATE TABLE bot_devserver_definitions (id TEXT NOT NULL PRIMARY KEY, fingerprint TEXT NOT NULL, spec BLOB NOT NULL, current_attempt TEXT NOT NULL, removed INTEGER NOT NULL CHECK(removed IN (0,1)))`,
				`INSERT INTO bot_devserver_catalog_schema VALUES(1,1)`,
			} {
				if _, err := db.Exec(ddl); err != nil {
					t.Fatal(err)
				}
			}
			saved := savedDefinition("old")
			// Historical version-1 bytes/digest; do not recompute with the new encoder.
			raw := []byte(`{"key":{"machine_id":"synthetic-machine","bot_id":"synthetic-bot"},"project":{"machine_id":"synthetic-machine","project_id":"synthetic-project","canonical_id":"synthetic-repository","kind":"repository"},"name":"Synthetic server","command":"synthetic-server --foreground old","directory":"/synthetic/project"}`)
			fingerprint := "f422ac62f2630608565cee3a05548346265bf343fbd2247efbdc960a36034ee1"
			if broken {
				raw = []byte(`{"unknown":"field"}`)
			}
			if _, err := db.Exec(`INSERT INTO bot_devserver_definitions VALUES(?,?,?,'',0)`, saved.ID, fingerprint, raw); err != nil {
				t.Fatal(err)
			}
			before := catalogDatabaseState(t, db)
			if _, err := NewCatalog(ctx, db); pa.Code(err) != pa.Unsupported {
				t.Fatal(err)
			}
			if after := catalogDatabaseState(t, db); after != before {
				t.Fatal("reader migrated the database")
			}
			err := InstallCatalog(ctx, db)
			if broken {
				if err == nil || catalogDatabaseState(t, db) != before {
					t.Fatal("malformed legacy definition was changed", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			c, err := NewCatalog(ctx, db)
			if err != nil {
				t.Fatal(err)
			}
			got, err := c.Rule(ctx, saved)
			if err != nil || got.Decision != "" || got.Version != 0 {
				t.Fatal(got, err)
			}
			if err := InstallCatalog(ctx, db); err != nil {
				t.Fatal(err)
			}
		})
	}
}

type uncertainRuleStore struct {
	*Catalog
	lose bool
}

func (s *uncertainRuleStore) SaveRule(ctx context.Context, r servers.Rule, d servers.Decision) (servers.Rule, error) {
	got, err := s.Catalog.SaveRule(ctx, r, d)
	if err == nil && s.lose {
		return servers.Rule{}, errors.New("synthetic lost commit acknowledgement")
	}
	return got, err
}

func TestApprovalHonorsAllFourChoicesAndLostRuleAcknowledgement(t *testing.T) {
	for _, kind := range []pa.PermissionOptionKind{pa.AllowOnce, pa.AllowAlways, pa.RejectOnce, pa.RejectAlways} {
		t.Run(string(kind), func(t *testing.T) {
			db, _ := testDatabase(t)
			c := readyCatalog(t, db)
			saved := savedDefinition("choice")
			ctx := context.Background()
			if err := c.Create(ctx, saved); err != nil {
				t.Fatal(err)
			}
			var reviews atomic.Int32
			a := servers.Approval{Rules: c, Check: func(ctx context.Context, _ servers.Spec) error { return ctx.Err() }, Guard: func(ctx context.Context, _ servers.Spec, fn func() error) error {
				if err := ctx.Err(); err != nil {
					return err
				}
				return fn()
			},
				Review: func(_ context.Context, action pa.PermissionAction, options []pa.PermissionOption) (pa.PermissionOption, error) {
					reviews.Add(1)
					var shown servers.Spec
					if action.Shortened || json.Unmarshal([]byte(action.Detail), &shown) != nil || shown != saved.Spec || len(options) != 4 {
						t.Fatal("review did not show the complete exact command", action)
					}
					for _, o := range options {
						if o.Kind == kind {
							return o, nil
						}
					}
					t.Fatal("choice missing")
					return pa.PermissionOption{}, nil
				}}
			check, err := a.Authorize(ctx, saved)
			allow := kind == pa.AllowOnce || kind == pa.AllowAlways
			if allow {
				if err != nil || check == nil || check(ctx) != nil {
					t.Fatal(err)
				}
			} else if pa.Code(err) != pa.Revoked || check != nil {
				t.Fatal(err)
			}
			rule, e := c.Rule(ctx, saved)
			if e != nil {
				t.Fatal(e)
			}
			want := servers.Decision("")
			if kind == pa.AllowAlways {
				want = servers.Allow
			}
			if kind == pa.RejectAlways {
				want = servers.Deny
			}
			if rule.Decision != want {
				t.Fatal(rule)
			}
			if want != "" {
				_, _ = a.Authorize(ctx, saved)
				if reviews.Load() != 1 {
					t.Fatal("standing rule reopened review")
				}
				if _, err := c.SaveRule(ctx, rule, ""); err != nil {
					t.Fatal(err)
				}
				if check != nil && pa.Code(check(ctx)) != pa.StaleVersion {
					t.Fatal("old approval survived revocation")
				}
			}
		})
	}
	t.Run("lost saved grant acknowledgement", func(t *testing.T) {
		db, _ := testDatabase(t)
		c := readyCatalog(t, db)
		saved := savedDefinition("uncertain")
		ctx := context.Background()
		if err := c.Create(ctx, saved); err != nil {
			t.Fatal(err)
		}
		a := servers.Approval{Rules: &uncertainRuleStore{Catalog: c, lose: true}, Check: func(context.Context, servers.Spec) error { return nil }, Guard: func(_ context.Context, _ servers.Spec, fn func() error) error { return fn() }, Review: func(_ context.Context, _ pa.PermissionAction, o []pa.PermissionOption) (pa.PermissionOption, error) {
			return o[1], nil
		}}
		if check, err := a.Authorize(ctx, saved); err == nil || check != nil {
			t.Fatal("uncertain persistence released launch authority")
		}
		if rule, err := c.Rule(ctx, saved); err != nil || rule.Decision != servers.Allow {
			t.Fatal("fault did not commit first", rule, err)
		}
	})
}
