package cloud

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSlowRawRekeyHandlerDoesNotBlockReaderAndIsBounded(t *testing.T) {
	entered := make(chan struct{}, RawRekeyPendingLimit)
	release := make(chan struct{})
	transport := &Transport{opts: Options{Now: time.Now, RawRekeyOffer: func([]byte) error { entered <- struct{}{}; <-release; return nil }}}
	defer close(release)
	for i := 0; i < RawRekeyPendingLimit; i++ {
		if err := transport.handle(nil, []byte(`{"type":"content_key_offer"}`)); err != nil {
			t.Fatal(err)
		}
		<-entered
	}
	if err := transport.handle(nil, []byte(`{"type":"content_key_offer"}`)); err != nil {
		t.Fatal(err)
	}
	if transport.RekeyPending() != RawRekeyPendingLimit {
		t.Fatal("unbounded raw handlers")
	}
	done := make(chan error, 1)
	go func() { done <- transport.handle(nil, []byte(`{"type":"pong"}`)) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("normal reader blocked behind raw handler")
	}
}

func TestFailedRevocationWriteDoesNotPretendToPersist(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "pins")
	s := NewPinnedStore(dir)
	if err := s.Pin("account", PinnedDevice{DeviceID: "A", PublicKey: base64.StdEncoding.EncodeToString(make([]byte, 32)), Scope: ScopeReadOnly, PairedAt: 1}); err != nil {
		t.Fatal(err)
	}
	moved := dir + "-saved"
	if err := os.Rename(dir, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir, []byte("block directory"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Revoke("A", time.Now()); err == nil {
		t.Fatal("failed persistence reported success")
	}
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(moved, dir); err != nil {
		t.Fatal(err)
	}
	changed, err := s.Revoke("A", time.Now())
	if err != nil || !changed {
		t.Fatal("retry skipped durable deny", changed, err)
	}
	fresh := NewPinnedStore(dir)
	if refused, err := fresh.Refused("A"); err != nil || !refused {
		t.Fatal("denial did not survive reload", err)
	}
}
