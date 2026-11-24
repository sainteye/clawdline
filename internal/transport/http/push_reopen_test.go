package http

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	adapterpush "github.com/sainteye/clawdline/internal/adapters/push"
)

// TestAFailedPushStoreOpenIsTriedAgain injects one failed open — the push
// directory's name is taken by a file, so it cannot be made — and then clears
// the fault. The next request has to open the store; a daemon that kept the
// first error would refuse every push route until it restarted.
func TestAFailedPushStoreOpenIsTriedAgain(t *testing.T) {
	s := pushServer(t)
	t.Cleanup(func() { pushStores.Delete(s.cfg.Dir) })
	if err := os.MkdirAll(s.cfg.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	blocker := filepath.Join(s.cfg.Dir, adapterpush.DirName)
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if rec := pushCall(t, s, http.MethodGet, "/v1/push/key", "", "phone", false); rec.Code == http.StatusOK {
		t.Fatalf("the injected open failure was not seen: %d %s", rec.Code, rec.Body)
	}
	if err := os.Remove(blocker); err != nil {
		t.Fatal(err)
	}
	if rec := pushCall(t, s, http.MethodGet, "/v1/push/key", "", "phone", false); rec.Code != http.StatusOK {
		t.Fatalf("the store was not reopened after the fault cleared: %d %s", rec.Code, rec.Body)
	}
}
