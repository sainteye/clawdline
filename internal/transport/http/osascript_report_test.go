package http

import (
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/terminal"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// The hourly line is what a week of daemon.log is compared on, so its shape
// is fixed here: the switch, the hour's length, the sums, and each kind as
// runs/failures/total_ms/max_ms.
func TestOsascriptLineShape(t *testing.T) {
	since := time.Unix(1_700_000_000, 0)
	w := terminal.OsascriptWindow{Since: since, Kinds: []terminal.OsascriptCount{
		{Kind: "capture", Runs: 3, Total: 412 * time.Millisecond, Max: 180 * time.Millisecond},
		{Kind: "list", Runs: 1200, Failures: 2, Total: 281043 * time.Millisecond, Max: 10012 * time.Millisecond},
	}}
	got := osascriptLine(w, since.Add(time.Hour), true)
	want := "osascript: hour iterm_scan=on seconds=3600 runs=1203 failures=2 total_ms=281455 max_ms=10012 " +
		"kinds=capture=3/0/412/180,list=1200/2/281043/10012"
	if got != want {
		t.Fatalf("the hourly line\n got: %s\nwant: %s", got, want)
	}
	quiet := osascriptLine(terminal.OsascriptWindow{Since: since}, since.Add(time.Hour), false)
	if quiet != "osascript: hour iterm_scan=off seconds=3600 runs=0 failures=0 total_ms=0 max_ms=0 kinds=-" {
		t.Fatalf("an hour with no run: %s", quiet)
	}
}

// A turned-off source travels as its own entry, incomplete and saying why,
// beside the sources that answered.
func TestScanSourcesCarryATurnedOffSource(t *testing.T) {
	got := scanSources(map[string]bool{"ps": true, "tmux": true}, []session.Gap{},
		map[string]string{"iterm": "setting"})
	if len(got) != 3 || got[0].Source != "iterm" || got[0].Complete ||
		got[0].Disabled != contract.ScanSourceDisabledSetting || got[1].Disabled != "" || !got[2].Complete {
		t.Fatalf("scan sources: %+v", got)
	}
}
