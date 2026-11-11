package cloud

import (
	"testing"
	"time"
)

func withPinnedClock(t *testing.T, at time.Time) *time.Time {
	t.Helper()
	now := at
	saved := pinnedNow
	pinnedNow = func() time.Time { return now }
	t.Cleanup(func() { pinnedNow = saved })
	return &now
}

func TestAnInvitedPinIsRefusedFromTheSecondItExpires(t *testing.T) {
	now := withPinnedClock(t, time.Unix(1_800_000_000, 0))
	store := NewPinnedStore(t.TempDir())
	device := freshPin(t, "dev_invited")
	device.Invited, device.ExpiresAt = true, now.Unix()+8*3600
	if err := store.Pin("usr_one", device); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := store.PublicKeyFor("dev_invited"); !ok || err != nil {
		t.Fatalf("a live invited pin did not answer: %v %v", ok, err)
	}
	if refused, _ := store.Refused("dev_invited"); refused {
		t.Fatal("a live invited pin read as refused")
	}
	*now = now.Add(8*time.Hour - time.Second)
	if refused, _ := store.Refused("dev_invited"); refused {
		t.Fatal("an invited pin was refused a second before it expired")
	}
	*now = now.Add(time.Second)
	// Refused, not merely absent: absence would fall back to the account
	// roster, which still lists the browser.
	refused, err := store.Refused("dev_invited")
	if err != nil || !refused {
		t.Fatalf("an expired pin was not refused: %v %v", refused, err)
	}
	if _, ok, _ := store.PublicKeyFor("dev_invited"); ok {
		t.Fatal("an expired pin still answered a key")
	}
	if _, err := store.ViewerScope("dev_invited"); err == nil {
		t.Fatal("an expired pin still answered a scope")
	}
	if _, err := store.ActiveRekeyPin("usr_one", "dev_invited"); err == nil {
		t.Fatal("an expired pin could still take a rekey")
	}
	// Revoking an expired row records the revocation rather than doing
	// nothing, so the list says the person threw it out.
	if changed, err := store.Revoke("dev_invited", *now); err != nil || !changed {
		t.Fatalf("revoking an expired pin: %v %v", changed, err)
	}
	rows, _ := store.Devices()
	if len(rows) != 1 || rows[0].RevokedAt == 0 || !rows[0].Invited {
		t.Fatalf("rows after revoke: %#v", rows)
	}
}

func TestANeverExpiringInvitedPinStaysAdmitted(t *testing.T) {
	now := withPinnedClock(t, time.Unix(1_800_000_000, 0))
	store := NewPinnedStore(t.TempDir())
	device := freshPin(t, "dev_forever")
	device.Invited = true
	if err := store.Pin("usr_one", device); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(10 * 365 * 24 * time.Hour)
	if refused, _ := store.Refused("dev_forever"); refused {
		t.Fatal("an invited pin with no expiry was refused")
	}
}
