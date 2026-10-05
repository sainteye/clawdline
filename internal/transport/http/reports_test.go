package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/turnreport"
	"github.com/sainteye/clawdline/internal/app/cloudops"
	"github.com/sainteye/clawdline/internal/transport/cloud"
)

const reportID = "2026-10-05-0123456789abcdef0123456789abcdef"

// turnReportServer is the whole handler, gate included, with one report kept in
// its state directory the way `clawdline report` keeps one.
func turnReportServer(t *testing.T) (*Server, http.Handler, string, string) {
	t.Helper()
	p := shellPane("")
	s, handler, local, machine := wholeServer(t, p, p)
	page, err := turnreport.Render(&turnreport.Collection{}, turnreport.Status{Cards: []turnreport.Card{{Title: "✅ Done"}}},
		"en", "2026-10-05", "demo")
	if err != nil {
		t.Fatal(err)
	}
	writeReportPage(t, s.cfg.Dir, reportID, page)
	return s, handler, local, machine
}

func writeReportPage(t *testing.T, stateDir, id string, page []byte) string {
	t.Helper()
	dir := filepath.Join(turnreport.ReportsDir(stateDir), id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, turnreport.PageName)
	if err := os.WriteFile(p, page, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// getReport asks as a browser on this machine would: over loopback, at
// 127.0.0.1, with this machine's credential.
func getReport(handler http.Handler, local, path string, edit func(*http.Request)) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = "127.0.0.1:7757"
	req.RemoteAddr = "127.0.0.1:52000"
	req.Header.Set("Authorization", "Bearer "+local)
	if edit != nil {
		edit(req)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestAReportIsServedToThisMachineInASandbox(t *testing.T) {
	_, handler, local, _ := turnReportServer(t)
	rec := getReport(handler, local, "/reports/"+reportID, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("answered %d: %s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	csp := rec.Header().Get("Content-Security-Policy")
	for _, want := range []string{"sandbox allow-scripts", "default-src 'none'", "frame-ancestors 'none'", "form-action 'none'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("the policy lacks %q: %s", want, csp)
		}
	}
	if strings.Contains(csp, "allow-same-origin") {
		t.Errorf("the sandbox gives the page this daemon's origin: %s", csp)
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" || rec.Header().Get("Cache-Control") != "private, no-store" {
		t.Errorf("headers = %v", rec.Header())
	}
	if !strings.Contains(rec.Body.String(), `content="clawdline report"`) {
		t.Error("the body is not the report")
	}
	// The gate still wants a credential.
	if rec := getReport(handler, local, "/reports/"+reportID, func(r *http.Request) { r.Header.Del("Authorization") }); rec.Code != http.StatusUnauthorized {
		t.Errorf("without a credential: %d", rec.Code)
	}
}

// A Cloud viewer's request reaches this handler in process, with this
// machine's own credentials; it is refused all the same.
func TestAReportNeverTravelsOverCloud(t *testing.T) {
	_, handler, local, machine := turnReportServer(t)
	router := cloud.Router{Handler: handler, Authorize: cloud.LocalAuthorizer(local, machine)}
	got, err := router.Do(context.Background(), cloudops.LocalRequest{Method: http.MethodGet, Path: "/reports/" + reportID})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != http.StatusForbidden || !strings.Contains(string(got.Body), "report_not_over_cloud") ||
		strings.Contains(string(got.Body), "clawdline report") {
		t.Fatalf("a Cloud read answered %d %s", got.Status, got.Body)
	}
}

// The tunnel's requests come from 127.0.0.1 with the tunnel's name as Host;
// a request from elsewhere has a peer that is not loopback.
func TestAReportIsRefusedThroughTheTunnelAndFromElsewhere(t *testing.T) {
	_, handler, local, _ := turnReportServer(t)
	for name, edit := range map[string]func(*http.Request){
		"quick tunnel":      func(r *http.Request) { r.Host = "abc.trycloudflare.com" },
		"cloudflare header": func(r *http.Request) { r.Header.Set("Cf-Connecting-Ip", "198.51.100.7") },
		"another machine":   func(r *http.Request) { r.RemoteAddr = "192.0.2.1:52000" },
		"no peer":           func(r *http.Request) { r.RemoteAddr = "" },
	} {
		rec := getReport(handler, local, "/reports/"+reportID, edit)
		if rec.Code == http.StatusOK || strings.Contains(rec.Body.String(), "clawdline report") {
			t.Errorf("%s: answered %d with the report", name, rec.Code)
		}
	}
	if rec := getReport(handler, local, "/reports/"+reportID, func(r *http.Request) { r.Host = "localhost:7757" }); rec.Code != http.StatusOK {
		t.Errorf("localhost: %d", rec.Code)
	}
}

// Nothing but a report's own page is answered: no path is built from the
// request, and a file that is a link, or that `clawdline report` did not
// write, is not found.
func TestOnlyAReportsOwnPageIsAnswered(t *testing.T) {
	s, handler, local, _ := turnReportServer(t)
	secret := filepath.Join(t.TempDir(), "secret.html")
	if err := os.WriteFile(secret, []byte(`<meta name="generator" content="clawdline report">secret`), 0o600); err != nil {
		t.Fatal(err)
	}
	// A page without the generator's mark, in a directory of the right shape.
	writeReportPage(t, s.cfg.Dir, "2026-10-05-ffffffffffffffffffffffffffffffff", []byte("<script>alert(1)</script>"))
	// A page that is a link to a file elsewhere.
	linkDir := filepath.Join(turnreport.ReportsDir(s.cfg.Dir), "2026-10-05-eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee")
	if err := os.MkdirAll(linkDir, 0o700); err != nil {
		t.Fatal(err)
	}
	linked := os.Symlink(secret, filepath.Join(linkDir, turnreport.PageName)) == nil
	// A report directory that is a link.
	other := filepath.Join(t.TempDir(), "other")
	writeReportPage(t, other, reportID, []byte(`<meta name="generator" content="clawdline report">secret`))
	if linked {
		if err := os.Symlink(filepath.Join(turnreport.ReportsDir(other), reportID),
			filepath.Join(turnreport.ReportsDir(s.cfg.Dir), "2026-10-05-dddddddddddddddddddddddddddddddd")); err != nil {
			t.Fatal(err)
		}
	}
	// A file beside the reports.
	if err := os.WriteFile(filepath.Join(turnreport.ReportsDir(s.cfg.Dir), "2026-10-05-demo.html"),
		[]byte(`<meta name="generator" content="clawdline report">secret`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"/reports/",
		"/reports/2026-10-05-ffffffffffffffffffffffffffffffff",
		"/reports/2026-10-05-eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
		"/reports/2026-10-05-dddddddddddddddddddddddddddddddd",
		"/reports/2026-10-05-cccccccccccccccccccccccccccccccc",
		"/reports/2026-10-05-demo.html",
		"/reports/" + reportID + "/report.html",
		"/reports/" + reportID + "/../" + reportID,
		"/reports/..%2Fdevices.json",
		"/reports/%2e%2e/%2e%2e/etc/passwd",
		"/reports/../v1/health",
		"/reports/" + strings.ToUpper(reportID),
		"/reports/" + reportID + "%00",
	} {
		rec := getReport(handler, local, path, nil)
		if rec.Code == http.StatusOK {
			t.Errorf("%s answered 200: %.80s", path, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "secret") || strings.Contains(rec.Body.String(), "alert(1)") {
			t.Errorf("%s answered what is not a report: %.80s", path, rec.Body.String())
		}
	}
	if !linked {
		t.Log("this system made no symbolic links; the two link cases were not run")
	}
	if rec := getReport(handler, local, "/reports/"+reportID, func(r *http.Request) { r.Method = http.MethodPost }); rec.Code == http.StatusOK {
		t.Errorf("POST answered %d", rec.Code)
	}
}
