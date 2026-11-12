package cloud

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	domain "github.com/sainteye/clawdline/internal/domain/cloud"
)

func assistantPin(t *testing.T, sender string) PinnedDevice {
	t.Helper()
	pin := freshPin(t, sender)
	key, _ := pin.Key()
	pin.Fingerprint = domain.Fingerprint(key)
	return pin
}

func writeAssistantPins(t *testing.T, store *PinnedStore, record pinnedRecord) {
	t.Helper()
	body, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomically(store.dir, store.Path(), body); err != nil {
		t.Fatal(err)
	}
}

func requireAssistantGrant(t *testing.T, store *PinnedStore, account, sender string) AssistantViewerGrant {
	t.Helper()
	grant, err := store.ProjectAssistantGrant(account, sender)
	if err != nil {
		t.Fatal(err)
	}
	if grant.PrincipalID == "" || grant.ReaderID == "" || grant.AuthorizationRevision == "" {
		t.Fatalf("incomplete grant: %+v", grant)
	}
	return grant
}

func TestProjectAssistantGrantSharesOwnerAcrossDevicesAndIsolatesInvites(t *testing.T) {
	store := NewPinnedStore(t.TempDir())
	for _, sender := range []string{"desktop", "phone", "guest-one", "guest-two"} {
		pin := assistantPin(t, sender)
		pin.Invited = sender == "guest-one" || sender == "guest-two"
		if err := store.Pin("account-one", pin); err != nil {
			t.Fatal(err)
		}
	}
	desktop := requireAssistantGrant(t, store, "account-one", "desktop")
	phone := requireAssistantGrant(t, store, "account-one", "phone")
	if desktop.PrincipalID != phone.PrincipalID || desktop.ReaderID != phone.ReaderID {
		t.Fatal("one account's devices did not share the reader")
	}
	if desktop.AuthorizationRevision == phone.AuthorizationRevision {
		t.Fatal("distinct device authorizations shared a revision")
	}
	grants := []AssistantViewerGrant{desktop,
		requireAssistantGrant(t, store, "account-one", "guest-one"),
		requireAssistantGrant(t, store, "account-one", "guest-two")}
	other := NewPinnedStore(t.TempDir())
	if err := other.Pin("account-two", assistantPin(t, "desktop")); err != nil {
		t.Fatal(err)
	}
	grants = append(grants, requireAssistantGrant(t, other, "account-two", "desktop"))
	invited := assistantPin(t, "guest-one")
	invited.Invited = true
	if err := other.Pin("account-two", invited); err != nil {
		t.Fatal(err)
	}
	grants = append(grants, requireAssistantGrant(t, other, "account-two", "guest-one"))
	for i, grant := range grants {
		for _, previous := range grants[:i] {
			if grant.PrincipalID == previous.PrincipalID || grant.ReaderID == previous.ReaderID {
				t.Fatal("unrelated account or invited readers shared an identity")
			}
		}
	}
	if got := requireAssistantGrant(t, NewPinnedStore(store.dir), "account-one", "desktop"); got != desktop {
		t.Fatal("reopening the store changed the grant")
	}
}

func TestProjectAssistantGrantKeepsInvitedReaderAcrossAuthorizationChanges(t *testing.T) {
	store := NewPinnedStore(t.TempDir())
	pin := assistantPin(t, "guest")
	pin.Invited = true
	pin.Scope = ScopeReadOnly
	if err := store.Pin("account", pin); err != nil {
		t.Fatal(err)
	}
	before := requireAssistantGrant(t, store, "account", "guest")
	if before.Writable {
		t.Fatal("readonly invitation was writable")
	}
	pin = assistantPin(t, "guest")
	pin.Invited = true
	pin.Scope = ScopeOperate
	if err := store.Pin("account", pin); err != nil {
		t.Fatal(err)
	}
	after := requireAssistantGrant(t, store, "account", "guest")
	if !after.Writable || after.AuthorizationRevision == before.AuthorizationRevision || after.ReaderID != before.ReaderID || after.PrincipalID != before.PrincipalID {
		t.Fatal("invitation authority change did not preserve its own reader")
	}
	pin.Invited = false
	if err := store.Pin("account", pin); err != nil {
		t.Fatal(err)
	}
	owner := requireAssistantGrant(t, store, "account", "guest")
	if owner.ReaderID == after.ReaderID || owner.PrincipalID == after.PrincipalID || owner.AuthorizationRevision == after.AuthorizationRevision {
		t.Fatal("invitation reused owner authority or cursor")
	}
}

func TestProjectAssistantGrantFollowsEffectiveScopeWithoutChangingViewerScope(t *testing.T) {
	store := NewPinnedStore(t.TempDir())
	if scope, err := store.ViewerScope("missing"); err != nil || scope != ScopeOperate {
		t.Fatalf("legacy missing-pin scope changed: %q %v", scope, err)
	}
	if grant, err := store.ProjectAssistantGrant("account", "missing"); !errors.Is(err, ErrViewerNotPinned) || grant != (AssistantViewerGrant{}) {
		t.Fatalf("missing pin produced an assistant grant: %+v %v", grant, err)
	}
	if _, err := os.Stat(store.Path()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing-pin read created a file: %v", err)
	}
	pin := assistantPin(t, "viewer")
	if err := store.Pin("account", pin); err != nil {
		t.Fatal(err)
	}
	legacy := requireAssistantGrant(t, store, "account", "viewer")
	if !legacy.Writable {
		t.Fatal("completed legacy empty-scope pin lost operate authority")
	}
	for _, scope := range []string{ScopeReadOnly, ScopeOperate} {
		if err := store.SetScope("viewer", scope); err != nil {
			t.Fatal(err)
		}
		grant := requireAssistantGrant(t, store, "account", "viewer")
		if grant.Writable != (scope == ScopeOperate) || grant.AuthorizationRevision == legacy.AuthorizationRevision {
			t.Fatalf("scope did not affect authority and revision: %+v", grant)
		}
		if grant.ReaderID != legacy.ReaderID || grant.PrincipalID != legacy.PrincipalID {
			t.Fatal("a device scope change moved the account's reader")
		}
		if got, err := store.ViewerScope("viewer"); err != nil || got != scope {
			t.Fatalf("original ViewerScope changed: %q %v", got, err)
		}
	}
}

func TestProjectAssistantGrantRejectsInvalidPins(t *testing.T) {
	valid := assistantPin(t, "viewer")
	for _, test := range []struct {
		name   string
		mutate func(*pinnedRecord)
		want   error
	}{
		{"no account", func(r *pinnedRecord) { r.AccountID = "" }, ErrViewerNotPinned},
		{"wrong account", func(r *pinnedRecord) { r.AccountID = "other" }, ErrViewerNotPinned},
		{"missing sender", func(r *pinnedRecord) { r.Devices = nil }, ErrViewerNotPinned},
		{"pending", func(r *pinnedRecord) { r.Devices[0].PairingPending = true }, ErrViewerNotPinned},
		{"revoked", func(r *pinnedRecord) { r.Devices[0].RevokedAt = 1 }, ErrViewerNotPinned},
		{"expired", func(r *pinnedRecord) { r.Devices[0].ExpiresAt = 1 }, ErrViewerNotPinned},
		{"unknown scope", func(r *pinnedRecord) { r.Devices[0].Scope = "future" }, ErrViewerScope},
		{"invalid key encoding", func(r *pinnedRecord) { r.Devices[0].PublicKey = "invalid" }, ErrPinnedUnreadable},
		{"short key", func(r *pinnedRecord) { r.Devices[0].PublicKey = "AA==" }, ErrPinnedUnreadable},
		{"missing fingerprint", func(r *pinnedRecord) { r.Devices[0].Fingerprint = "" }, ErrPinnedUnreadable},
		{"wrong fingerprint", func(r *pinnedRecord) { r.Devices[0].Fingerprint = "AAAA-BBBB-CCCC-DDDD" }, ErrPinnedUnreadable},
		{"no completed pairing time", func(r *pinnedRecord) { r.Devices[0].PairedAt = 0 }, ErrViewerNotPinned},
		{"negative pairing time", func(r *pinnedRecord) { r.Devices[0].PairedAt = -1 }, ErrViewerNotPinned},
		{"duplicate active", func(r *pinnedRecord) { r.Devices = append(r.Devices, valid) }, ErrPinnedUnreadable},
		{"duplicate revoked", func(r *pinnedRecord) {
			copy := valid
			copy.RevokedAt = 1
			r.Devices = append(r.Devices, copy)
		}, ErrPinnedUnreadable},
		{"future version", func(r *pinnedRecord) { r.Version = 2 }, ErrPinnedUnreadable},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := NewPinnedStore(t.TempDir())
			record := pinnedRecord{Version: 1, AccountID: "account", Devices: []PinnedDevice{valid}}
			test.mutate(&record)
			writeAssistantPins(t, store, record)
			grant, err := store.ProjectAssistantGrant("account", "viewer")
			if !errors.Is(err, test.want) || grant != (AssistantViewerGrant{}) {
				t.Fatalf("invalid pin granted authority: %+v %v; want %v", grant, err, test.want)
			}
		})
	}
	store := NewPinnedStore(t.TempDir())
	if err := store.Pin("account", valid); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][2]string{{"", "viewer"}, {"account", ""}, {" account", "viewer"}, {"account", " viewer"}} {
		grant, err := store.ProjectAssistantGrant(args[0], args[1])
		if !errors.Is(err, ErrViewerNotPinned) || grant != (AssistantViewerGrant{}) {
			t.Fatalf("unmatched identity granted authority: %+v %v", grant, err)
		}
	}
}

func TestProjectAssistantGrantReloadsAuthorizationAndKeepsReader(t *testing.T) {
	dir := t.TempDir()
	writer, reader := NewPinnedStore(dir), NewPinnedStore(dir)
	pin := assistantPin(t, "viewer")
	if err := writer.Pin("account", pin); err != nil {
		t.Fatal(err)
	}
	previous := requireAssistantGrant(t, reader, "account", "viewer")
	assertChanged := func() {
		t.Helper()
		grant := requireAssistantGrant(t, reader, "account", "viewer")
		if grant.AuthorizationRevision == previous.AuthorizationRevision {
			t.Fatal("changed authorization reused a revision")
		}
		if grant.ReaderID != previous.ReaderID || grant.PrincipalID != previous.PrincipalID {
			t.Fatal("changed authorization moved the reader")
		}
		previous = grant
	}
	if err := writer.SetScope("viewer", ScopeReadOnly); err != nil {
		t.Fatal(err)
	}
	assertChanged()
	if previous.Writable {
		t.Fatal("another store's readonly change was not observed")
	}
	pin = assistantPin(t, "viewer")
	pin.Scope = ScopeReadOnly
	if err := writer.Pin("account", pin); err != nil {
		t.Fatal(err)
	}
	assertChanged()
	pin.PairedAt++
	if err := writer.Pin("account", pin); err != nil {
		t.Fatal(err)
	}
	assertChanged()
	pin.ExpiresAt = time.Now().Add(time.Hour).Unix()
	if err := writer.Pin("account", pin); err != nil {
		t.Fatal(err)
	}
	assertChanged()
	if changed, err := writer.Revoke("viewer", time.Now()); err != nil || !changed {
		t.Fatalf("revoke: %v %v", changed, err)
	}
	if _, err := reader.ProjectAssistantGrant("account", "viewer"); !errors.Is(err, ErrViewerNotPinned) {
		t.Fatalf("cached authorization survived revoke: %v", err)
	}
	if err := writer.Pin("account", pin); err != nil {
		t.Fatal(err)
	}
	_ = requireAssistantGrant(t, reader, "account", "viewer")
	pin.ExpiresAt = 1
	if err := writer.Pin("account", pin); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ProjectAssistantGrant("account", "viewer"); !errors.Is(err, ErrViewerNotPinned) {
		t.Fatalf("cached authorization survived expiry change: %v", err)
	}
	writeAssistantPins(t, writer, pinnedRecord{Version: 1, AccountID: "other-account", Devices: []PinnedDevice{pin}})
	if _, err := reader.ProjectAssistantGrant("account", "viewer"); !errors.Is(err, ErrViewerNotPinned) {
		t.Fatalf("cached account survived replacement: %v", err)
	}
	if err := os.WriteFile(writer.Path(), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ProjectAssistantGrant("account", "viewer"); !errors.Is(err, ErrPinnedUnreadable) {
		t.Fatalf("broken file reused cached authority: %v", err)
	}
	if err := writer.Forget(); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ProjectAssistantGrant("account", "viewer"); !errors.Is(err, ErrViewerNotPinned) {
		t.Fatalf("deleted file reused cached authority: %v", err)
	}
}

func TestProjectAssistantGrantDoesNotWriteOrExposePinMaterial(t *testing.T) {
	store := NewPinnedStore(t.TempDir())
	pin := assistantPin(t, "synthetic-viewer")
	if err := store.Pin("synthetic-account", pin); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	grant := requireAssistantGrant(t, store, "synthetic-account", "synthetic-viewer")
	for i := 0; i < 3; i++ {
		if got := requireAssistantGrant(t, store, "synthetic-account", "synthetic-viewer"); got != grant {
			t.Fatal("unchanged authorization was unstable")
		}
	}
	after, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	afterInfo, err := os.Stat(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	files, err := os.ReadDir(store.dir)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) || !os.SameFile(info, afterInfo) || !info.ModTime().Equal(afterInfo.ModTime()) || len(files) != 1 {
		t.Fatal("reading a grant changed pairing storage")
	}
	body, err := json.Marshal(grant)
	if err != nil {
		t.Fatal(err)
	}
	for _, material := range []string{pin.PublicKey, pin.Fingerprint, pin.DeviceID, "synthetic-account"} {
		if bytes.Contains(body, []byte(material)) {
			t.Fatal("a grant exposed raw pairing material")
		}
	}
}

func TestProjectAssistantGrantSerializesWithScopeChanges(t *testing.T) {
	store := NewPinnedStore(t.TempDir())
	if err := store.Pin("account", assistantPin(t, "viewer")); err != nil {
		t.Fatal(err)
	}
	if err := store.SetScope("viewer", ScopeReadOnly); err != nil {
		t.Fatal(err)
	}
	readonly := requireAssistantGrant(t, store, "account", "viewer")
	if err := store.SetScope("viewer", ScopeOperate); err != nil {
		t.Fatal(err)
	}
	operate := requireAssistantGrant(t, store, "account", "viewer")
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			scope := ScopeReadOnly
			if i%2 == 0 {
				scope = ScopeOperate
			}
			if err := store.SetScope("viewer", scope); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 40; i++ {
			grant, err := store.ProjectAssistantGrant("account", "viewer")
			if err != nil || (grant != readonly && grant != operate) {
				t.Errorf("mixed authorization snapshot: %+v %v", grant, err)
				return
			}
		}
	}()
	wg.Wait()
}
