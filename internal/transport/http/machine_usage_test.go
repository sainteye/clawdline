package http

import (
	"encoding/json"
	"net/http"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// The dashboard behind the session counts is a person's screen, read from a
// paired device as the capacity panel is. Each session is its whole tree and
// this daemon is a row of its own: here the test binary stands for the daemon
// and `go test`, its parent, for a session that started it — so the daemon's
// process is counted once, as the daemon, and not again in the session above.
func TestAPairedDeviceReadsTheMachinesUsage(t *testing.T) {
	f, _ := newGateFixture(t)
	parent := os.Getppid()
	reading := session.Inventory{Provenance: "ps", Complete: true, Sessions: []session.Session{
		{ID: "%7", Assistant: session.AssistantClaude, PID: parent, TTY: "/dev/pts/7"},
		// A shell is not a session row, as on the session list.
		{ID: "%8", PID: parent},
	}}
	s := &Server{inventory: app.Inventory{Process: todoProcesses{reading}}}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/machine/usage", s.machineUsageRoute)
	h := f.g.wrap(mux)

	reader := map[string]string{"Authorization": "Bearer " + f.read}
	rec := call{method: http.MethodGet, path: "/v1/machine/usage", headers: reader}.do(h)
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		if rec.Code != http.StatusNotImplemented || !strings.Contains(rec.Body.String(), "machine_usage_unsupported") {
			t.Fatalf("a machine without a reader: %d %s", rec.Code, rec.Body)
		}
		return
	}
	var u contract.MachineUsage
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &u) != nil {
		t.Fatalf("a read-only device: %d %s", rec.Code, rec.Body)
	}
	if u.Cores < 1 || u.MemoryTotalBytes <= 0 || u.MemoryUsedBytes <= 0 || u.IntervalMs <= 0 || len(u.Load) != 3 {
		t.Fatalf("the machine: %+v", u)
	}
	if u.CpuPercent < 0 || u.CpuPercent > 100 {
		t.Fatalf("cpu %v", u.CpuPercent)
	}
	var daemon, row *contract.MachineUsageGroup
	for i := range u.Groups {
		g := &u.Groups[i]
		switch {
		case g.Kind == contract.MachineUsageGroupKindDaemon:
			daemon = g
		case g.ID == "%7":
			row = g
		case g.ID == "%8":
			t.Fatalf("a shell became a row: %+v", g)
		}
	}
	if daemon == nil || daemon.PID != int64(os.Getpid()) || daemon.Processes < 1 || daemon.RssBytes <= 0 {
		t.Fatalf("the daemon's row: %+v in %s", daemon, rec.Body)
	}
	if row == nil || row.PID != int64(parent) || row.Assistant != "claude" || row.TTY != "/dev/pts/7" {
		t.Fatalf("the session's row: %+v in %s", row, rec.Body)
	}
	// Its tree holds the parent but not the daemon under it.
	if row.RssBytes <= 0 || row.Processes < 1 {
		t.Fatalf("the session's tree: %+v", row)
	}

	if rec := (call{method: http.MethodPost, path: "/v1/machine/usage", headers: reader}).do(h); rec.Code == http.StatusOK {
		t.Fatalf("a POST was answered: %d %s", rec.Code, rec.Body)
	}
	if rec := (call{method: http.MethodGet, path: "/v1/machine/usage"}).do(h); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token: %d %s", rec.Code, rec.Body)
	}
}
