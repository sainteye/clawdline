package http

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/claudeusage"
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

func TestManualProviderQuotaRefreshChecksClaudeDespiteAFreshStatusLine(t *testing.T) {
	home := t.TempDir()
	now := time.Now()
	record, err := json.Marshal(map[string]any{
		"at": now.Add(-2 * time.Minute).Unix(),
		"rate_limits": map[string]any{"seven_day": map[string]any{
			"used_percentage": 81, "resets_at": now.Add(time.Hour).Unix(),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, ".claude", "statusline-cache")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "rate-limits.json"), record, 0600); err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	finish := sync.OnceFunc(func() { close(release) })
	t.Cleanup(finish)
	asked := make(chan struct{}, 2)
	local := limits.NewReader(home, nil).WithClaudeCode(func(ctx context.Context) ([]claudeusage.Window, claudeusage.Outcome) {
		asked <- struct{}{}
		select {
		case <-release:
			return []claudeusage.Window{{Name: "7d", UsedPercent: 27}}, ""
		case <-ctx.Done():
			return nil, claudeusage.Failed
		}
	})
	s := &Server{providerQuotaService: providerquota.New(home, local)}
	read := func(path string) contract.ProviderQuotaSnapshot {
		t.Helper()
		rec := httptest.NewRecorder()
		s.providerQuotas(rec, httptest.NewRequest("GET", path, nil))
		var got contract.ProviderQuotaSnapshot
		if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &got) != nil {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Body.String())
		}
		return got
	}
	initial := read("/v1/provider-quotas")
	if initial.Updating || initial.Providers[0].Windows[0].UsedPercent != 81 {
		t.Fatalf("initial reading: %+v", initial)
	}
	for i := 0; i < 2; i++ {
		got := read("/v1/provider-quotas/refresh")
		if !got.Updating || got.Providers[0].Windows[0].UsedPercent != 81 {
			t.Fatalf("refresh should report work in flight and keep the previous reading: %+v", got)
		}
	}
	select {
	case <-asked:
	case <-time.After(time.Second):
		t.Fatal("manual refresh did not ask Claude Code")
	}
	finish()
	deadline := time.Now().Add(time.Second)
	for {
		got := read("/v1/provider-quotas")
		if !got.Updating {
			row := got.Providers[0]
			if row.Source != "provider_cli" || len(row.Windows) != 1 || row.Windows[0].UsedPercent != 27 {
				t.Fatalf("the newer Claude answer did not reach the shared snapshot: %+v", row)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the Claude check did not settle")
		}
		time.Sleep(time.Millisecond)
	}
	if got := read("/v1/provider-quotas/refresh"); got.Updating {
		t.Fatal("another check started inside the cooldown")
	}
	select {
	case <-asked:
		t.Fatal("manual refresh repeated an in-flight check")
	default:
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
