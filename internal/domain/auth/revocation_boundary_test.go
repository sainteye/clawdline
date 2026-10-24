package auth

import "testing"

// A wake-up is useful only if rechecking at that instant sees committed
// revocation. Reload also proves the old key cannot return after a restart.
func TestRevocationBoundaryWakeObservesCommittedDenial(t *testing.T) {
	for _, all := range []bool{false, true} {
		name := "single"
		if all {
			name = "all"
		}
		t.Run(name, func(t *testing.T) {
			store := &memStore{}
			a, _ := newAuthority(t, store)
			local, err := a.LocalToken()
			if err != nil {
				t.Fatal(err)
			}
			id, token, err := a.AddDevice("synthetic browser", NewCaps(Read, Send), false)
			if err != nil {
				t.Fatal(err)
			}
			otherID, other, err := a.AddDevice("synthetic other browser", NewCaps(Read, Send), false)
			if err != nil {
				t.Fatal(err)
			}
			changed, cancel := a.Changes()
			defer cancel()
			// The second watcher represents a busy stream with an unread
			// notification; it must not block the revoke or the first watcher.
			slow, cancelSlow := a.Changes()
			defer cancelSlow()
			a.NotifyChanged()
			<-changed
			if all {
				if count, err := a.RevokeAll(); err != nil || count != 2 {
					t.Fatalf("revoke-all: count=%d err=%v", count, err)
				}
			} else if err := a.Revoke(id); err != nil {
				t.Fatal(err)
			}
			select {
			case <-changed:
			default:
				t.Fatal("revocation did not wake an existing subscriber")
			}
			select {
			case <-slow:
			default:
				t.Fatal("busy subscriber lost its pending wake-up")
			}
			check := func(current *Authority) {
				t.Helper()
				if current.Verify(token).Allowed {
					t.Error("revoked token still verifies")
				}
				if _, held := current.Holds(id); held {
					t.Error("revoked stream holder still exists")
				}
				if current.Verify(other).Allowed == all {
					t.Error("unrelated token has the wrong authority")
				}
				if _, held := current.Holds(otherID); held == all {
					t.Error("unrelated stream holder has the wrong authority")
				}
				if !current.Verify(local).Allowed || !current.Verify(local).Local {
					t.Error("local authority was lost")
				}
			}
			check(a)
			reloaded, _ := newAuthority(t, store)
			check(reloaded)
		})
	}
}

func TestRevocationBoundaryFailedSaveDoesNotAnnounceRevocation(t *testing.T) {
	for _, all := range []bool{false, true} {
		name := "single"
		if all {
			name = "all"
		}
		t.Run(name, func(t *testing.T) {
			store := &memStore{}
			a, _ := newAuthority(t, store)
			id, token, err := a.AddDevice("synthetic browser", NewCaps(Read, Send), false)
			if err != nil {
				t.Fatal(err)
			}
			changed, cancel := a.Changes()
			defer cancel()
			saves, audits := store.saves, len(store.audit)
			store.fail = true
			if all {
				if count, err := a.RevokeAll(); err == nil || count != 0 {
					t.Fatal("failed revoke-all reported success")
				}
			} else if err := a.Revoke(id); err == nil {
				t.Fatal("failed revoke reported success")
			}
			if !a.Verify(token).Allowed {
				t.Fatal("failed save changed live authority")
			}
			if _, held := a.Holds(id); !held {
				t.Fatal("failed save removed stream holder")
			}
			select {
			case <-changed:
				t.Fatal("failed save announced a device change")
			default:
			}
			if store.saves != saves || len(store.audit) != audits {
				t.Fatal("failed revocation persisted or audited success")
			}
			store.fail = false
			if err := a.Revoke(id); err != nil {
				t.Fatal(err)
			}
			if a.Verify(token).Allowed {
				t.Fatal("successful retry retained authority")
			}
			select {
			case <-changed:
			default:
				t.Fatal("successful retry did not wake subscriber")
			}
		})
	}
}
