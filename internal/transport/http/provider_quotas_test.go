package http

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/limits"
	"github.com/sainteye/clawdline/internal/adapters/providerquota"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func TestProviderQuotaEndpointIsReadOnlyAndUsesThePublishedContract(t *testing.T) {
	home := t.TempDir()
	s := &Server{providerQuotaService: providerquota.New(home, limits.NewReader(home, nil))}
	rec := httptest.NewRecorder()
	s.providerQuotas(rec, httptest.NewRequest("GET", "/v1/provider-quotas", nil))
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	var got contract.ProviderQuotaSnapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Providers) != 4 || got.Updating {
		t.Fatalf("%+v", got)
	}
	for _, r := range got.Providers {
		if len(r.Windows) != 0 {
			t.Fatalf("a missing reading became quota: %+v", r)
		}
	}
	rec = httptest.NewRecorder()
	s.providerQuotas(rec, httptest.NewRequest("POST", "/v1/provider-quotas", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("write accepted: %d", rec.Code)
	}
}

func TestGrokSessionInfoIncludesAccountQuotaStateInBothChannels(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GROK_HOME", filepath.Join(home, ".grok"))
	p := &pane{s: session.Session{ID: "%19", Assistant: session.AssistantGrok, Backend: session.BackendTmux, State: session.StateIdle}}
	s := paneServer(t, p)
	s.providerQuotaService = providerquota.New(home, nil)
	for _, parts := range []string{"summary", "all"} {
		rec := httptest.NewRecorder()
		s.sessionInfoRoute(rec, httptest.NewRequest("GET", "/v1/sessions/fixture/info?parts="+parts, nil), "%19")
		var got contract.SessionInfoReply
		if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &got) != nil {
			t.Fatal(rec.Code, rec.Body.String())
		}
		q := got.Info.Limits
		if q == nil || len(q.Windows) != 0 || q.UnknownReason != contract.AssistantUnknownReasonNoRecord || q.ReadAtMs == 0 {
			t.Fatalf("%s lost account state: %+v", parts, q)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".grok")); !os.IsNotExist(err) {
		t.Fatal("read created account state")
	}
}

func TestGrokLimitsKeepMeasuredZeroStalenessAndUnknownDistinct(t *testing.T) {
	now := time.Unix(2000000, 0)
	at, reset := now.Unix()-60, now.Unix()+3600
	row := providerquota.Row{ID: "grok", ObservedAt: &at, Windows: []providerquota.Window{{Name: "7d", UsedPercent: 0, ResetsAt: &reset}}}
	got := grokLimits(row, now)
	if len(got.Windows) != 1 || got.Windows[0].UsedPercent != 0 || got.Windows[0].ResetsAt != reset || got.Stale || got.UnknownReason != "" || *got.AgeSeconds != 60 {
		t.Fatal(got)
	}
	row.Problem = "unavailable"
	if got = grokLimits(row, now); !got.Stale || len(got.Windows) != 1 {
		t.Fatal(got)
	}
	row.Problem = ""
	if got = grokLimits(row, now.Add(time.Hour)); !got.Stale {
		t.Fatal(got)
	}
	row.Windows = []providerquota.Window{{Name: "7d", UsedPercent: math.NaN()}, {Name: "7d", UsedPercent: 101}}
	if got = grokLimits(row, now); len(got.Windows) != 0 || got.UnknownReason != "no_reading" {
		t.Fatal(got)
	}
}
