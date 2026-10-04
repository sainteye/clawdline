package updatecheck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/contract"
)

func TestCompareStates(t *testing.T) {
	const a, b = "4b7c3f8d0000000000000000000000000000aaaa", "08b4277b0000000000000000000000000000bbbb"
	early, late := "2026-10-01T00:00:00Z", "2026-10-04T00:00:00Z"
	for _, tc := range []struct {
		name            string
		running, latest contract.BuildStamp
		want            contract.UpdateState
	}{
		{"same commit", contract.BuildStamp{Stamp: a, CommittedAt: early}, contract.BuildStamp{Stamp: a}, contract.UpdateStateCurrent},
		{"abbreviated stamp", contract.BuildStamp{Stamp: a[:8]}, contract.BuildStamp{Stamp: a}, contract.UpdateStateCurrent},
		{"cloud later", contract.BuildStamp{Stamp: b, CommittedAt: early}, contract.BuildStamp{Stamp: a, CommittedAt: late}, contract.UpdateStateUpdateAvailable},
		{"machine later", contract.BuildStamp{Stamp: b, CommittedAt: late}, contract.BuildStamp{Stamp: a, CommittedAt: early}, contract.UpdateStateAhead},
		{"old BUILD.json without a time", contract.BuildStamp{Stamp: b}, contract.BuildStamp{Stamp: a, CommittedAt: late}, contract.UpdateStateDiffers},
		{"same second", contract.BuildStamp{Stamp: b, CommittedAt: late}, contract.BuildStamp{Stamp: a, CommittedAt: late}, contract.UpdateStateDiffers},
		{"own commit unknown", contract.BuildStamp{}, contract.BuildStamp{Stamp: a}, contract.UpdateStateUnknown},
		{"latest never read", contract.BuildStamp{Stamp: a}, contract.BuildStamp{}, contract.UpdateStateUnknown},
	} {
		got, reason := Compare(tc.running, tc.latest)
		if got != tc.want {
			t.Errorf("%s: %s, want %s", tc.name, got, tc.want)
		}
		if (got == contract.UpdateStateUnknown) != (reason != "") {
			t.Errorf("%s: reason %q with state %s", tc.name, reason, got)
		}
	}
}

func TestParseBuildAcceptsAFileWithoutCommittedAt(t *testing.T) {
	b, err := ParseBuild([]byte(`{"stamp":"08b4277b"}`))
	if err != nil || b.Stamp != "08b4277b" || b.CommittedAt != "" {
		t.Fatalf("%+v %v", b, err)
	}
	if _, err := ParseBuild([]byte(`{}`)); err == nil {
		t.Fatal("a BUILD.json with no stamp was accepted")
	}
}

func TestRunningReadsTheServedDistFirst(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "BUILD.json"), []byte(`{"stamp":"abcdef1234","committed_at":"2026-10-04T00:00:00Z"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := Running(dir); got.Stamp != "abcdef1234" || got.CommittedAt != "2026-10-04T00:00:00Z" {
		t.Fatalf("%+v", got)
	}
	// No dist: the binary's own revision, with no time (empty under go test).
	if got := Running(""); got.CommittedAt != "" {
		t.Fatalf("%+v", got)
	}
}

func TestAFailedRefreshKeepsTheLastGoodAnswer(t *testing.T) {
	fail := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail {
			http.Error(w, "down", http.StatusBadGateway)
			return
		}
		w.Write([]byte(`{"stamp":"4b7c3f8d","committed_at":"2026-10-04T00:00:00Z"}`))
	}))
	defer srv.Close()
	c := &Checker{URL: srv.URL, Client: srv.Client(), Running: func() contract.BuildStamp {
		return contract.BuildStamp{Stamp: "08b4277b", CommittedAt: "2026-10-01T00:00:00Z"}
	}}
	if st := c.Status(); st.State != contract.UpdateStateUnknown || st.CheckedAt != "" {
		t.Fatalf("before any check: %+v", st)
	}
	c.Refresh(context.Background())
	first := c.Status()
	if first.State != contract.UpdateStateUpdateAvailable || first.CheckedAt == "" || first.Error != "" {
		t.Fatalf("after a good check: %+v", first)
	}
	fail = true
	c.Refresh(context.Background())
	st := c.Status()
	if st.State != contract.UpdateStateUpdateAvailable || st.Latest.Stamp != "4b7c3f8d" || st.CheckedAt != first.CheckedAt {
		t.Fatalf("a failed check lost the last good answer: %+v", st)
	}
	if !strings.Contains(st.Error, "502") {
		t.Fatalf("the failure is not said: %q", st.Error)
	}
}

func TestAnOversizedBodyIsRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"stamp":"` + strings.Repeat("a", maxBuildBodyBytes) + `"}`))
	}))
	defer srv.Close()
	if _, err := Fetch(context.Background(), srv.Client(), srv.URL); err == nil {
		t.Fatal("a body over the cap was accepted")
	}
}

func TestTheCheckCanBeTurnedOff(t *testing.T) {
	asked := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { asked = true }))
	defer srv.Close()
	t.Setenv(CheckEnv, "off")
	t.Setenv(URLEnv, srv.URL)
	c := New("")
	if !c.Disabled || c.URL != srv.URL {
		t.Fatalf("%+v", c)
	}
	c.Refresh(context.Background())
	if st := c.Status(); asked || st.State != contract.UpdateStateUnknown || !strings.Contains(st.Reason, "off") {
		t.Fatalf("asked=%v %+v", asked, st)
	}
}

func TestSourceURLDefaultsToTheHostedConsole(t *testing.T) {
	t.Setenv(URLEnv, "")
	if got := SourceURL(); got != "https://app.clawdline.com/BUILD.json" {
		t.Fatal(got)
	}
}
