package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/contract"
)

// `clawdline usage --freeze-baseline` and `--work-report`: did a change make
// one unit of work cheaper (docs/token-ledger.md, the section of that name).
//
// The daemon answers raw samples only (GET /v1/usage/work-samples). Freezing
// a baseline writes that answer to a file, outside any repository; a report
// reads the file and the live ledger's samples since, and folds both here
// with app.FoldWorkReport. Nothing but raw records goes into the report, so
// anyone holding the file can recompute it.

// workBaselineReadLimit is the most of a frozen baseline file read: a
// work-samples answer is at most workSampleLimit samples of a few hundred
// bytes each.
const workBaselineReadLimit = 16 << 20

// workBaselineDir is where a baseline is frozen when no --out is named: under
// the daemon's own state directory, never a repository.
func workBaselineDir() string { return filepath.Join(config.Dir(), "usage-baselines") }

// fetchWorkSamples is one GET to /v1/usage/work-samples.
func fetchWorkSamples(b *broker, since, until string) (contract.UsageWorkSamples, answer, error) {
	q := url.Values{}
	if since != "" {
		q.Set("since", since)
	}
	if until != "" {
		q.Set("until", until)
	}
	a, err := b.request(http.MethodGet, "/v1/usage/work-samples", q, nil, "")
	if err != nil || !a.ok() {
		return contract.UsageWorkSamples{}, a, err
	}
	var ws contract.UsageWorkSamples
	if err := json.Unmarshal(a.Body, &ws); err != nil {
		return contract.UsageWorkSamples{}, a, errors.New(cliCopy("ops", "usage.unreadable", "the daemon's answer could not be read"))
	}
	return ws, a, nil
}

// freezeWorkBaseline is `usage --freeze-baseline`: the samples of the range,
// written to out, which must not exist yet — a frozen baseline is never
// written over.
func freezeWorkBaseline(stdout, stderr io.Writer, b *broker, since, out string, now time.Time) int {
	if since == "" {
		since = "14d"
	}
	ws, a, err := fetchWorkSamples(b, since, "")
	if err != nil {
		fmt.Fprintln(stderr, "clawdline usage:", err)
		return 1
	}
	if !a.ok() {
		return report(stdout, stderr, "usage", a)
	}
	if out == "" {
		out = filepath.Join(workBaselineDir(), now.UTC().Format("2006-01-02")+".json")
	}
	if err := writeWorkBaseline(out, ws); err != nil {
		fmt.Fprintln(stderr, "clawdline usage:", err)
		return 1
	}
	unreadable := 0
	for _, s := range ws.Samples {
		if !workSampleOf(s).Readable() {
			unreadable++
		}
	}
	fmt.Fprintf(stdout, cliCopy("ops", "usage.froze", "froze %d units (%d with no whole reading) created %s – %s, definition %d, to %s\n"),
		len(ws.Samples), unreadable, workTime(ws.Since), workTime(ws.Until), ws.DefinitionVersion, out)
	if ws.Truncated {
		fmt.Fprintln(stdout, cliCopy("ops", "usage.frozen_truncated", "truncated: the range held more tasks than one answer reads; only the newest are frozen"))
	}
	return 0
}

// writeWorkBaseline writes the samples to path, refusing one already there.
func writeWorkBaseline(path string, ws contract.UsageWorkSamples) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	body, err := json.MarshalIndent(ws, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return fmt.Errorf(cliCopy("ops", "usage.baseline_exists", "%s already holds a frozen baseline; name another with --out"), path)
	}
	if err != nil {
		return err
	}
	if _, err := f.Write(append(body, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// readWorkBaseline reads a frozen baseline and refuses one of another
// definition: its fields would not mean what this build's fold reads.
func readWorkBaseline(path string) (contract.UsageWorkSamples, error) {
	f, err := os.Open(path)
	if err != nil {
		return contract.UsageWorkSamples{}, err
	}
	defer f.Close()
	body, err := io.ReadAll(io.LimitReader(f, workBaselineReadLimit+1))
	if err != nil {
		return contract.UsageWorkSamples{}, err
	}
	if len(body) > workBaselineReadLimit {
		return contract.UsageWorkSamples{}, fmt.Errorf(cliCopy("ops", "usage.baseline_too_large", "%s is larger than a frozen baseline can be"), path)
	}
	var ws contract.UsageWorkSamples
	if err := json.Unmarshal(body, &ws); err != nil {
		return contract.UsageWorkSamples{}, fmt.Errorf(cliCopy("ops", "usage.invalid_baseline", "%s is not a frozen baseline: %v"), path, err)
	}
	if ws.DefinitionVersion != app.WorkReportDefinitionVersion {
		return contract.UsageWorkSamples{}, fmt.Errorf(cliCopy("ops", "usage.definition_mismatch_file", "%s was frozen with definition %d and this build reads %d"),
			path, ws.DefinitionVersion, app.WorkReportDefinitionVersion)
	}
	return ws, nil
}

// workSampleOf is a wire sample as the fold reads it.
func workSampleOf(s contract.UsageWorkSample) app.WorkUnitSample {
	out := app.WorkUnitSample{Kind: s.Kind, ID: s.ID, WorkKind: s.WorkKind, Scope: s.Scope, Model: s.Model,
		CrossEnd: s.CrossEnd, Input: s.Input, CacheWrite: s.CacheWrite, CacheRead: s.CacheRead, Output: s.Output,
		Calls: s.Calls, CallsAbove: s.CallsAbove, Cost: s.Cost, CostKnown: s.CostKnown,
		DurationSeconds: s.DurationSeconds, Ending: string(s.Ending), Respawn: s.Respawn, Respawned: s.Respawned,
		Data: string(s.Data)}
	if s.CreatedAt != 0 {
		out.CreatedAt = time.Unix(s.CreatedAt, 0)
	}
	if s.EndedAt != 0 {
		out.EndedAt = time.Unix(s.EndedAt, 0)
	}
	return out
}

func workSamplesOf(ws contract.UsageWorkSamples) []app.WorkUnitSample {
	out := make([]app.WorkUnitSample, 0, len(ws.Samples))
	for _, s := range ws.Samples {
		out = append(out, workSampleOf(s))
	}
	return out
}

// workReportAnswer is what `--work-report --json` prints: the two periods'
// ranges and the report.
type workReportAnswer struct {
	Baseline workReportRange `json:"baseline"`
	Trial    workReportRange `json:"trial"`
	Report   app.WorkReport  `json:"report"`
}

type workReportRange struct {
	Since     int64 `json:"since"`
	Until     int64 `json:"until"`
	Units     int   `json:"units"`
	Truncated bool  `json:"truncated"`
}

// recomputeWorkReport is the report of a baseline and a trial, from their
// raw samples alone.
func recomputeWorkReport(baseline, trial contract.UsageWorkSamples) workReportAnswer {
	return workReportAnswer{
		Baseline: workReportRange{baseline.Since, baseline.Until, len(baseline.Samples), baseline.Truncated},
		Trial:    workReportRange{trial.Since, trial.Until, len(trial.Samples), trial.Truncated},
		Report:   app.FoldWorkReport(workSamplesOf(baseline), workSamplesOf(trial)),
	}
}

// showWorkReport is `usage --work-report`: the baseline from its file, the
// trial from the live ledger since `since` (the baseline's end when empty).
func showWorkReport(stdout, stderr io.Writer, b *broker, baselinePath, since string, asJSON bool) int {
	baseline, err := readWorkBaseline(baselinePath)
	if err != nil {
		fmt.Fprintln(stderr, "clawdline usage:", err)
		return 1
	}
	if since == "" {
		since = strconv.FormatInt(baseline.Until, 10)
	}
	trial, a, err := fetchWorkSamples(b, since, "")
	if err != nil {
		fmt.Fprintln(stderr, "clawdline usage:", err)
		return 1
	}
	if !a.ok() {
		return report(stdout, stderr, "usage", a)
	}
	if trial.DefinitionVersion != baseline.DefinitionVersion {
		fmt.Fprintf(stderr, cliCopy("ops", "usage.definition_mismatch_daemon", "clawdline usage: the daemon reads definition %d and the baseline was frozen with %d; freeze a new baseline with this daemon\n"), trial.DefinitionVersion, baseline.DefinitionVersion)
		return 1
	}
	got := recomputeWorkReport(baseline, trial)
	if asJSON {
		body, _ := json.MarshalIndent(got, "", "  ")
		fmt.Fprintln(stdout, string(body))
		return 0
	}
	writeWorkReport(stdout, got)
	return 0
}

// writeWorkReport is the report as a person reads it: a block per group,
// baseline beside trial, then the overall verdict and what is not recorded.
func writeWorkReport(w io.Writer, got workReportAnswer) {
	fmt.Fprintf(w, cliCopy("ops", "usage.periods", "baseline: %d units created %s – %s; trial: %d units created %s – %s\n"),
		got.Baseline.Units, workTime(got.Baseline.Since), workTime(got.Baseline.Until),
		got.Trial.Units, workTime(got.Trial.Since), workTime(got.Trial.Until))
	if got.Trial.Since < got.Baseline.Until {
		fmt.Fprintln(w, cliCopy("ops", "usage.overlap", "the trial starts before the baseline ends: some units may be in both periods"))
	}
	for _, r := range []struct {
		name string
		rng  workReportRange
	}{{cliCopy("ops", "usage.baseline", "baseline"), got.Baseline}, {cliCopy("ops", "usage.trial", "trial"), got.Trial}} {
		if r.rng.Truncated {
			fmt.Fprintf(w, cliCopy("ops", "usage.period_truncated", "%s truncated: the range held more tasks than one answer reads; only the newest are in it\n"), r.name)
		}
	}
	rep := got.Report
	if len(rep.Groups) == 0 {
		fmt.Fprintln(w, cliCopy("ops", "usage.empty", "no unit of work in either period"))
	}
	for _, g := range rep.Groups {
		fmt.Fprintf(w, "\n%s\n", app.WorkGroupName(g))
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		b, t := g.Baseline, g.Trial
		row := func(name, before, after, change string) {
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\n", name, before, after, change)
		}
		row("", cliCopy("ops", "usage.baseline", "baseline"), cliCopy("ops", "usage.trial", "trial"), "")
		row(cliCopy("ops", "usage.units_unreadable", "units (unreadable)"), fmt.Sprintf("%d (%d)", b.Units, b.Unreadable), fmt.Sprintf("%d (%d)", t.Units, t.Unreadable), "")
		row(cliCopy("ops", "usage.ended_running", "ended / running"), fmt.Sprintf("%d / %d", b.Ended, b.Running), fmt.Sprintf("%d / %d", t.Ended, t.Running), "")
		row(cliCopy("ops", "usage.successful_readable", "successful & readable"), strconv.Itoa(b.Comparable), strconv.Itoa(t.Comparable),
			fmt.Sprintf(cliCopy("ops", "usage.needed_each", "(%d needed in each)"), rep.MinUnits))
		row(cliCopy("ops", "usage.cache_median", "cache-read median"), workTokens(b.CacheReadMedian), workTokens(t.CacheReadMedian), workChange(g.CacheReadChange))
		row(strings.ReplaceAll(cliCopy("ops", "usage.interval", "  95%% interval"), "%%", "%"), workInterval(b.CacheReadInterval), workInterval(t.CacheReadInterval), "")
		row(cliCopy("ops", "usage.token_median", "total-token median"), workTokens(b.TotalMedian), workTokens(t.TotalMedian), "")
		row(cliCopy("ops", "usage.calls_above", "calls above 200k"), workRate(b.AboveShare), workRate(t.AboveShare), "")
		row(cliCopy("ops", "usage.cost_median", "cost median"), workCostOf(b), workCostOf(t), "")
		row(cliCopy("ops", "usage.duration_median", "duration median"), workDuration(b.DurationMedian), workDuration(t.DurationMedian), "")
		row(cliCopy("ops", "usage.duration_p75", "duration p75"), workDuration(b.DurationP75), workDuration(t.DurationP75), workChange(g.DurationP75Change))
		row(cliCopy("ops", "usage.failure_rate", "failure rate"), workRate(b.FailureRate), workRate(t.FailureRate), "")
		row(cliCopy("ops", "usage.redo_rate", "redo rate"), workRate(b.RedoRate), workRate(t.RedoRate), "")
		row(cliCopy("ops", "usage.timeout_rate", "timeout/stalled rate"), workRate(b.TimeoutStalledRate), workRate(t.TimeoutStalledRate), "")
		_ = tw.Flush()
		verdict := cliCopy("ops", "usage.verdict_prefix", "  verdict: ") + g.Verdict
		if len(g.Failed) > 0 {
			verdict += " (" + strings.Join(g.Failed, ", ") + ")"
		}
		fmt.Fprintln(w, verdict)
	}
	fmt.Fprintf(w, cliCopy("ops", "usage.overall", "\noverall: %s — %s\n"), rep.Verdict, rep.Why)
	fmt.Fprintln(w, strings.ReplaceAll(cliCopy("ops", "usage.met_needs", "met needs, in a group with enough units: cache-read median down at least 30%%, duration p75 up at most 10%%, no guardrail rate worse."), "%%", "%"))
	fmt.Fprintln(w, cliCopy("ops", "usage.causality", "units are not assigned to periods at random: this says what changed, not why."))
	for _, m := range rep.NotRecorded {
		fmt.Fprintf(w, cliCopy("ops", "usage.not_recorded", "not recorded: %s — %s\n"), m.Name, m.Why)
	}
}

func workTime(unix int64) string {
	if unix == 0 {
		return "?"
	}
	return time.Unix(unix, 0).UTC().Format("2006-01-02 15:04Z")
}

func workTokens(v *float64) string {
	if v == nil {
		return "—"
	}
	return usageCount(*v)
}

func workInterval(v *app.WorkInterval) string {
	if v == nil {
		return "—"
	}
	return usageCount(v.Low) + "–" + usageCount(v.High)
}

func workRate(v *float64) string {
	if v == nil {
		return "—"
	}
	return fmt.Sprintf("%.0f%%", *v*100)
}

func workChange(v *float64) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%+.0f%%", *v*100)
}

func workDuration(v *int64) string {
	if v == nil {
		return "—"
	}
	return (time.Duration(*v) * time.Second).String()
}

func workCostOf(p app.WorkPeriod) string {
	if p.CostMedian == nil {
		return "—"
	}
	if !p.CostKnown {
		// A model with no price: the cost is unknown, never zero.
		return cliCopy("ops", "usage.unpriced", "unknown (unpriced)")
	}
	return fmt.Sprintf("$%.2f", *p.CostMedian)
}
