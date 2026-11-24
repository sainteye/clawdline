package cloud

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/adapters/cloudkeys"
)

func TestOpenRetryDelayDoublesUpToItsCeiling(t *testing.T) {
	want := []time.Duration{5, 10, 20, 40, 80, 160, 300, 300}
	for i, w := range want {
		if got := OpenRetryDelay(i + 1); got != w*time.Second {
			t.Errorf("failure %d: %s, want %s", i+1, got, w*time.Second)
		}
	}
	if got := OpenRetryDelay(1 << 20); got != LineOpenRetryMaxSecondsLimit*time.Second {
		t.Errorf("a long streak waits %s", got)
	}
}

func TestOnlyAuthoritativeOpenFailuresAreFinal(t *testing.T) {
	final := []error{adaptercloud.ErrNoIdentity, adaptercloud.ErrOtherEnvironment, adaptercloud.ErrUnauthorized,
		&adaptercloud.RelayError{Code: "device_revoked"}}
	for _, err := range final {
		if !OpenRefusedForGood(err) {
			t.Errorf("%v was retried", err)
		}
	}
	for _, err := range []error{nil, os.ErrPermission, errors.New("input/output error"), &adaptercloud.RelayError{Code: "token_expired"}} {
		if OpenRefusedForGood(err) {
			t.Errorf("%v was treated as final", err)
		}
	}
}

// TestALineWhoseKeysCannotBeOpenedSaysSoToItsStarter injects a key directory
// that cannot be made. Open still answers a link for the status route, and
// OpenFailure carries the transient cause, so the starter retries it rather
// than treating the line as up.
func TestALineWhoseKeysCannotBeOpenedSaysSoToItsStarter(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.json"),
		[]byte(`{"cloud_enabled":true,"cloud_api_base":"http://127.0.0.1:9","cloud_relay_url":"ws://127.0.0.1:9/v1/connect"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, cloudkeys.DirName), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	link, err := Open(LinkOptions{Dir: dir})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	failure := link.OpenFailure()
	if failure == nil || OpenRefusedForGood(failure) {
		t.Fatalf("OpenFailure = %v, want a transient failure", failure)
	}
	if link.Status().LastError == "" {
		t.Error("the status route cannot say why the line is not up")
	}
	if err := os.Remove(filepath.Join(dir, cloudkeys.DirName)); err != nil {
		t.Fatal(err)
	}
	again, err := Open(LinkOptions{Dir: dir})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if failure := again.OpenFailure(); !errors.Is(failure, adaptercloud.ErrNoIdentity) || !OpenRefusedForGood(failure) {
		t.Fatalf("after the fault cleared, OpenFailure = %v, want the authoritative no-identity answer", failure)
	}
}
