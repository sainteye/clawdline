package http

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/auth"
)

// These are existing sensitive route shapes, not an invented authorization
// policy. A spy immediately behind the real gate proves that refusal happens
// before a handler can read content or execute an operation. Each route also
// has an admitted control, so a broken fixture cannot look like protection.
func TestSecurityBoundaryStopsBeforeSensitiveHandlers(t *testing.T) {
	f, _ := newGateFixture(t)
	pending, err := f.g.auth.BeginPairing("synthetic pending browser")
	if err != nil || pending.ID == "" {
		t.Fatalf("begin pairing: %v", err)
	}
	id, revoked, err := f.g.auth.AddDevice("synthetic revoked browser", auth.NewCaps(auth.Read, auth.Send), false)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.g.auth.Revoke(id); err != nil {
		t.Fatal(err)
	}
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/v1/transcript?session=%254"},
		{http.MethodGet, "/v1/sessions/%254/documents/project/report.html"},
		{http.MethodGet, "/v1/artifacts/images/1a000000-0000-4000-8000-000000000001"},
		{http.MethodGet, "/v1/terminals/synthetic-terminal/history"},
		{http.MethodPost, "/v1/terminals/synthetic-terminal/input"},
		{http.MethodPost, "/v1/sessions/%254/send"},
	} {
		t.Run(route.method+route.path, func(t *testing.T) {
			reads, writes := 0, 0
			const content = "synthetic-private-content"
			backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					reads++
				} else {
					writes++
				}
				_, _ = w.Write([]byte(content))
			})
			h := f.g.wrap(backend)
			for _, scenario := range []struct {
				name, token, host, site string
				status                  int
			}{
				{"no credential", "", "", "", http.StatusUnauthorized},
				{"pending pairing is not a credential", pending.ID, "", "", http.StatusUnauthorized},
				{"invalid bearer", "synthetic-invalid-credential", "", "", http.StatusUnauthorized},
				{"revoked bearer", revoked, "", "", http.StatusUnauthorized},
				{"rebinding from loopback", f.send, "rebound.invalid:7757", "same-origin", http.StatusForbidden},
				{"loopback suffix is not loopback", f.send, "localhost.attacker.invalid:7757", "", http.StatusForbidden},
				{"cross-site fetch", f.send, "", "cross-site", http.StatusForbidden},
			} {
				t.Run(scenario.name, func(t *testing.T) {
					reads, writes = 0, 0
					headers := map[string]string{"Content-Type": "application/json", "Sec-Fetch-Site": scenario.site}
					if scenario.token != "" {
						headers["Authorization"] = "Bearer " + scenario.token
					}
					rec := call{method: route.method, path: route.path, host: scenario.host, body: `{}`, headers: headers}.do(h)
					if rec.Code != scenario.status || reads != 0 || writes != 0 || strings.Contains(rec.Body.String(), content) {
						t.Fatalf("status=%d want=%d reads=%d writes=%d leaked=%v", rec.Code, scenario.status, reads, writes, strings.Contains(rec.Body.String(), content))
					}
				})
			}
			if route.method == http.MethodPost {
				for _, scenario := range []struct{ name, origin, site, media string }{
					{"cookie without origin", "", "", "application/json"},
					{"cookie from another port", "http://127.0.0.1:9999", "same-site", "application/json"},
					{"cookie from another host", "https://attacker.invalid", "", "application/json"},
					{"opaque origin", "null", "", "application/json"},
					{"same-site is not same-origin", "http://127.0.0.1:7757", "same-site", "application/json"},
					{"form post", "http://127.0.0.1:7757", "same-origin", "application/x-www-form-urlencoded"},
				} {
					t.Run(scenario.name, func(t *testing.T) {
						reads, writes = 0, 0
						want := http.StatusForbidden
						if scenario.media != "application/json" {
							want = http.StatusUnsupportedMediaType
						}
						rec := call{method: route.method, path: route.path, body: `{}`, headers: map[string]string{
							"Cookie": sessionCookie + "=" + f.send, "Origin": scenario.origin, "Sec-Fetch-Site": scenario.site, "Content-Type": scenario.media,
						}}.do(h)
						if rec.Code != want || reads != 0 || writes != 0 {
							t.Fatalf("status=%d want=%d reads=%d writes=%d", rec.Code, want, reads, writes)
						}
					})
				}
			}
			reads, writes = 0, 0
			rec := call{method: route.method, path: route.path, body: `{}`, headers: map[string]string{
				"Authorization": "Bearer " + f.send, "Content-Type": "application/json",
			}}.do(h)
			if rec.Code != http.StatusOK || rec.Body.String() != content || reads+writes != 1 {
				t.Fatalf("admitted control: status=%d reads=%d writes=%d", rec.Code, reads, writes)
			}
		})
	}
}

// Exercise the actual route registration with existing synthetic documents
// and image bytes as well as a pane that records reads and sends. The gate
// spy above separately proves no handler invocation, even for handlers whose
// filesystem reader has no spy seam.
func TestSecurityBoundaryRouterDeniesContentAndSessionSideEffects(t *testing.T) {
	p := waitingPane("%4", "synthetic-private-screen")
	p.s.CWD = t.TempDir()
	if err := os.Mkdir(filepath.Join(p.s.CWD, "artifacts"), 0700); err != nil {
		t.Fatal(err)
	}
	const content = "synthetic-private-document"
	if err := os.WriteFile(filepath.Join(p.s.CWD, "artifacts", "report.md"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	s, h, local, _ := wholeServer(t, touchPane{p}, touchPane{p})
	pictures, imageID := picturesFixture(t)
	s.pictures = pictures.pictures
	id, token, err := s.gate().auth.AddDevice("synthetic browser", auth.NewCaps(auth.Read, auth.Send), false)
	if err != nil {
		t.Fatal(err)
	}
	document := "/v1/sessions/%254/documents/project/report.md"
	image := "/v1/artifacts/images/" + imageID
	// Actual file and image reads must succeed before exercising denial.
	for _, path := range []string{document, image} {
		rec := call{method: http.MethodGet, path: path, headers: map[string]string{"Authorization": "Bearer " + token}}.do(h)
		if rec.Code != http.StatusOK || (path == document && rec.Body.String() != content) ||
			(path == image && !bytes.HasPrefix(rec.Body.Bytes(), []byte("\x89PNG"))) {
			t.Fatalf("admitted content control %s: %d", path, rec.Code)
		}
	}
	if err := s.gate().auth.Revoke(id); err != nil {
		t.Fatal(err)
	}
	before := len(p.done())
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, document},
		{http.MethodGet, image},
		{http.MethodGet, "/v1/transcript?session=%254"},
		{http.MethodPost, "/v1/sessions/%254/send"},
	} {
		for _, credential := range []string{"", "synthetic-invalid-credential", token} {
			for _, cookie := range []bool{false, true} {
				headers := map[string]string{"Content-Type": "application/json", "Origin": "http://127.0.0.1:7757"}
				if credential != "" {
					if cookie {
						headers["Cookie"] = sessionCookie + "=" + credential
					} else {
						headers["Authorization"] = "Bearer " + credential
					}
				}
				rec := call{method: route.method, path: route.path, body: `{"text":"synthetic-command"}`, headers: headers}.do(h)
				if rec.Code != http.StatusUnauthorized || strings.Contains(rec.Body.String(), content) ||
					bytes.HasPrefix(rec.Body.Bytes(), []byte("\x89PNG")) || len(p.done()) != before {
					t.Fatalf("%s %s cookie=%v: status=%d pane calls=%d", route.method, route.path, cookie, rec.Code, len(p.done())-before)
				}
			}
		}
	}
	// A different valid paired browser's cookie still cannot submit a send
	// from another local port, even though cookies are shared across ports.
	_, active, err := s.gate().auth.AddDevice("synthetic active browser", auth.NewCaps(auth.Read, auth.Send), false)
	if err != nil {
		t.Fatal(err)
	}
	rec := call{path: "/v1/sessions/%254/send", body: `{"text":"synthetic-command"}`, headers: map[string]string{
		"Cookie": sessionCookie + "=" + active, "Origin": "http://127.0.0.1:9999", "Content-Type": "application/json",
	}}.do(h)
	if rec.Code != http.StatusForbidden || len(p.done()) != before {
		t.Fatal("cross-origin send reached the Session host")
	}
	if !s.gate().auth.Verify(local).Allowed {
		t.Fatal("revoking a browser removed the local credential")
	}
}

// The production router and terminal service supplement the gate spy above:
// revoke-all must close an already admitted stream and release its lease,
// without letting the old token's next input reach the terminal host.
func TestSecurityBoundaryRevokeAllEndsStreamAndReleasesLease(t *testing.T) {
	host := newFakeTerms()
	f := newTermFixture(t, host)
	term := f.open(f.local)
	_, token := f.device("synthetic browser", true)
	control, rec := f.control(term.ID, token, "old-client", "takeover")
	if rec.Code != http.StatusOK {
		t.Fatalf("takeover: %d", rec.Code)
	}
	stream, status := f.stream(f.server(), term.ID, token, "old-client")
	if status != http.StatusOK {
		t.Fatalf("stream: %d", status)
	}
	stream.until(t, 2*time.Second, "initial frame", func(e sseEvent) bool { return e.name == "frame" })
	if rec := f.input(term.ID, token, "old-client", control.Epoch, 1, "synthetic-before"); rec.Code != http.StatusOK {
		t.Fatalf("admitted input: %d", rec.Code)
	}
	if len(host.typedNow()) != 1 {
		t.Fatal("admitted control did not reach terminal host")
	}
	if rec := f.do(http.MethodPost, "/v1/auth/devices/revoke-all", f.local, ""); rec.Code != http.StatusOK {
		t.Fatalf("revoke-all: %d", rec.Code)
	}
	_, events := stream.ended(t, time.Second)
	if len(events) == 0 || !strings.Contains(events[len(events)-1].data, "terminal_access_revoked") {
		t.Fatal("stream ended without the revocation refusal")
	}
	if rec := f.input(term.ID, token, "old-client", control.Epoch, 2, "synthetic-after"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("revoked input: %d", rec.Code)
	}
	if got := host.typedNow(); len(got) != 1 || got[0] != "synthetic-before" {
		t.Fatal("revoked input reached terminal host")
	}
	if next, rec := f.control(term.ID, f.local, "new-client", "acquire"); rec.Code != http.StatusOK || !next.Held {
		t.Fatal("revoke-all did not release the terminal lease")
	}
}
