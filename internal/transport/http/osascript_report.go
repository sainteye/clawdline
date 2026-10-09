package http

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/terminal"
	"github.com/sainteye/clawdline/internal/contract"
)

// osascriptReportPeriod is how often daemon.log gets the osascript line. An
// hour is the unit the iTerm2 scan experiment compares (docs/interface.md
// "Turning iTerm2 scanning off"): a week is 168 lines, and one line can be read
// without a tool.
const osascriptReportPeriod = time.Hour

// runOsascriptReport writes osascriptLine every period until ctx ends. The
// window starts when the clock does, so the first line covers the daemon's
// first hour and every later one the hour before it.
func runOsascriptReport(ctx context.Context, period time.Duration) {
	terminal.TakeOsascriptWindow(time.Now())
	tick := time.NewTicker(period)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-tick.C:
			log.Print(osascriptLine(terminal.TakeOsascriptWindow(now), now, terminal.ITermScan()))
		}
	}
}

// osascriptLine is the hourly line:
//
//	osascript: hour iterm_scan=on seconds=3600 runs=1203 failures=2 total_ms=281455 max_ms=10012 kinds=capture=3/0/412/180,list=1200/2/281043/10012
//
// Each kind is runs/failures/total_ms/max_ms; `kinds=-` is an hour with no
// run at all, which is what a machine with scanning off and nothing asked of
// iTerm2 should write. iterm_scan is the switch as it is when the line is
// written; a line whose hour saw it change still counts every run in it.
func osascriptLine(w terminal.OsascriptWindow, now time.Time, scan bool) string {
	runs, failures, total, longest := w.Runs()
	kinds := make([]string, 0, len(w.Kinds))
	for _, k := range w.Kinds {
		kinds = append(kinds, fmt.Sprintf("%s=%d/%d/%d/%d", k.Kind, k.Runs, k.Failures,
			k.Total.Milliseconds(), k.Max.Milliseconds()))
	}
	listed := strings.Join(kinds, ",")
	if listed == "" {
		listed = "-"
	}
	state := "on"
	if !scan {
		state = "off"
	}
	return fmt.Sprintf("osascript: hour iterm_scan=%s seconds=%d runs=%d failures=%d total_ms=%d max_ms=%d kinds=%s",
		state, int64(now.Sub(w.Since).Round(time.Second)/time.Second), runs, failures,
		total.Milliseconds(), longest.Milliseconds(), listed)
}

// osascriptDiagnostics is /v1/diagnostics.terminals.osascript: the same
// counts since this daemon started and since the last hourly line.
func osascriptDiagnostics() *contract.OsascriptDiagnostics {
	total, window := terminal.OsascriptReading()
	return &contract.OsascriptDiagnostics{
		ItermScan: terminal.ITermScan(),
		Since:     total.Since.Unix(),
		Kinds:     osascriptKinds(total),
		HourSince: window.Since.Unix(),
		HourKinds: osascriptKinds(window),
	}
}

func osascriptKinds(w terminal.OsascriptWindow) []contract.OsascriptKind {
	out := make([]contract.OsascriptKind, 0, len(w.Kinds))
	for _, k := range w.Kinds {
		out = append(out, contract.OsascriptKind{Kind: k.Kind, Runs: k.Runs, Failures: k.Failures,
			TotalMs: k.Total.Milliseconds(), MaxMs: k.Max.Milliseconds()})
	}
	return out
}
