package http

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/sainteye/clawdline-go/internal/adapters/devices"
	"github.com/sainteye/clawdline-go/internal/domain/auth"
)

// bodyMux is the patterns the body bound tells apart, each answering how much
// of the body it was handed.
func bodyMux() *http.ServeMux {
	mux := http.NewServeMux()
	echo := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, _ := io.Copy(io.Discard, r.Body)
		_, _ = io.WriteString(w, strconv.FormatInt(n, 10))
	})
	for _, p := range []string{"/v1/orchestrator/messages", "/v1/orchestrator/tasks/", "/v1/voice", "/v1/auth/", "/"} {
		mux.Handle(p, echo)
	}
	return mux
}

func post(h http.Handler, path string, body []byte, chunked bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7757"+path, bytes.NewReader(body))
	if chunked {
		// No length: the body says how long it is only by ending.
		req.ContentLength = -1
		req.Body = io.NopCloser(bytes.NewReader(body))
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// limits N27: the broker's routes read a body with no bound at all. Past the
// route's bound a body is refused whole with 413 before the route runs,
// whether it declared its length or not; at the bound it reaches the route
// whole.
func TestABodyPastItsRouteBoundIsRefusedBeforeTheRoute(t *testing.T) {
	h := boundBodies(bodyMux())
	for _, c := range []struct {
		path  string
		limit int64
	}{
		{"/v1/orchestrator/messages", commandBodyLimit},
		{"/v1/orchestrator/tasks/x/progress", commandBodyLimit},
		{"/v1/auth/pair", authBodyLimit},
	} {
		for _, chunked := range []bool{false, true} {
			rec := post(h, c.path, make([]byte, c.limit+1), chunked)
			if rec.Code != http.StatusRequestEntityTooLarge || refusalCode(rec) != "body_too_large" {
				t.Errorf("%s, %d bytes, chunked=%v: %d %s", c.path, c.limit+1, chunked, rec.Code, rec.Body.String())
			}
			rec = post(h, c.path, make([]byte, c.limit), chunked)
			if rec.Code != http.StatusOK || rec.Body.String() != strconv.FormatInt(c.limit, 10) {
				t.Errorf("%s, %d bytes, chunked=%v: %d %s", c.path, c.limit, chunked, rec.Code, rec.Body.String())
			}
		}
	}
	// A route with a larger bound of its own is not cut to the default.
	if rec := post(h, "/v1/voice", make([]byte, commandBodyLimit+1), false); rec.Code != http.StatusOK {
		t.Errorf("voice at %d bytes: %d %s", commandBodyLimit+1, rec.Code, rec.Body.String())
	}
	// The fallback streams to the Swift app, which bounds its own.
	if rec := post(h, "/v1/not-ours", make([]byte, commandBodyLimit+1), true); rec.Code != http.StatusOK {
		t.Errorf("the fallback: %d %s", rec.Code, rec.Body.String())
	}
}

// The audit is two records (D25). A scheduler's run goes to the store's
// journal and never into the security audit; a subscription goes to the
// security audit and not the journal; an event nobody classified stays in the
// security audit, where nothing is deleted.
func TestTheAuditIsSplitByEvent(t *testing.T) {
	s := capacityServer(t)
	ctx := context.Background()
	auditLines := func() string {
		data, err := os.ReadFile(filepath.Join(s.cfg.Dir, devices.AuditFile))
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		return string(data)
	}
	events := func() int {
		n, _, _, err := s.store.Counts(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	before := events()
	s.audit("orchestrator.schedule.run", map[string]string{"schedule": "s1", "how": "timer"})
	if events() != before+1 || strings.Contains(auditLines(), "orchestrator.schedule.run") {
		t.Fatalf("a scheduler run: %d journal events, audit %q", events()-before, auditLines())
	}
	s.audit("push.subscribe", map[string]string{"id": "p1", "device": "d1"})
	if events() != before+1 || !strings.Contains(auditLines(), `"push.subscribe"`) {
		t.Fatalf("a subscription: %d journal events, audit %q", events()-before, auditLines())
	}
	s.audit("something.nobody.named", map[string]string{})
	if events() != before+1 || !strings.Contains(auditLines(), "something.nobody.named") {
		t.Fatalf("an unclassified event: %d journal events, audit %q", events()-before, auditLines())
	}
}

// A device list at its limit answers a typed refusal a page can act on, not
// "the device store could not be written".
func TestAFullDeviceListIsATypedRefusal(t *testing.T) {
	f, _ := newGateFixture(t)
	f.g.files.SetDeviceLimit(3) // the local device, the sender and the reader
	_, _, err := f.g.auth.AddDevice("one too many", auth.NewCaps(auth.Read), false)
	if err == nil {
		t.Fatal("a device was added past the limit")
	}
	rec := httptest.NewRecorder()
	writeStoreFailure(rec, err)
	if rec.Code != http.StatusInsufficientStorage || refusalCode(rec) != "device_list_full" {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	// Devices() lists the paired ones, not this machine's own.
	if n := len(f.g.auth.Devices()); n != 2 {
		t.Fatalf("%d paired devices after the refusal", n)
	}
}
