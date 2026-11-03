package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/limits"
	"github.com/sainteye/clawdline/internal/adapters/providerquota"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/contract"
)

func TestDoctorReadsRunningVersionWithoutProviderRequests(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	s := &Server{cfg: config.Config{Dir: t.TempDir()}, version: "dev-123abc", providerQuotaService: providerquota.New(t.TempDir(), limits.NewReader(t.TempDir(), nil))}
	w := httptest.NewRecorder()
	s.doctorReport(w, httptest.NewRequest("GET", "/v1/doctor", nil))
	var got contract.DoctorReport
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil || got.Version != "dev-123abc" || len(got.Providers) != 4 || len(got.Gaps) == 0 || got.Coverage.State != "not_recorded" {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	for _, p := range got.Providers {
		if p.UsedPercent != nil {
			t.Fatalf("unknown became a number: %+v", p)
		}
	}
	w = httptest.NewRecorder()
	s.doctorReport(w, httptest.NewRequest("POST", "/v1/doctor", nil))
	if w.Code != 405 {
		t.Fatalf("POST: %d", w.Code)
	}
	s.doctorBusy.Store(true)
	w = httptest.NewRecorder()
	s.doctorReport(w, httptest.NewRequest("GET", "/v1/doctor", nil))
	if w.Code != 409 {
		t.Fatalf("concurrent check: %d", w.Code)
	}
}

func TestDoctorGateAcceptsCLIAndPairedReadersButRefusesAnonymousReads(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	f, _ := newGateFixture(t)
	s := &Server{cfg: config.Config{Dir: t.TempDir()}, version: "devel+123abcd", providerQuotaService: providerquota.New(t.TempDir(), limits.NewReader(t.TempDir(), nil))}
	h := f.g.wrap(http.HandlerFunc(s.doctorReport))
	for _, headers := range []map[string]string{{machineHeader: f.machine}, {"Authorization": "Bearer " + f.read}} {
		w := (call{method: http.MethodGet, path: "/v1/doctor", headers: headers}).do(h)
		if w.Code != 200 {
			t.Fatalf("authenticated Doctor read: %d %s", w.Code, w.Body)
		}
	}
	if w := (call{method: http.MethodGet, path: "/v1/doctor"}).do(h); w.Code != 401 {
		t.Fatalf("anonymous read: %d", w.Code)
	}
}
