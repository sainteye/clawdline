package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"sort"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/contract"
)

var ErrUsageOverviewProject = errors.New("unknown overview project")
var ErrUsageOverviewDays = errors.New("overview days must be 7 or 28")

func overviewProject(name string) contract.UsageOverviewProjectOption {
	if name == "" {
		return contract.UsageOverviewProjectOption{ID: "unattributed", Name: "unattributed"}
	}
	sum := sha256.Sum256([]byte(name))
	return contract.UsageOverviewProjectOption{ID: hex.EncodeToString(sum[:]), Name: filepath.Base(name)}
}

// Overview reads durable summaries only. The source inventory is a bounded
// lookback, so observed history is partial even after every known source is
// caught up. Empty historical days remain null until completeness is proven.
// Dispatches use creation dates; successful durations use completion dates.
func (u *UsageLedger) Overview(ctx context.Context, days int, project string) (contract.UsageOverview, error) {
	if days != 7 && days != 28 {
		return contract.UsageOverview{}, ErrUsageOverviewDays
	}
	now := u.now().In(time.Local)
	until := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	since := until.AddDate(0, 0, -days)
	previous := since.AddDate(0, 0, -days)
	out := contract.UsageOverview{At: now.Unix(), Days: int64(days), Timezone: time.Local.String(), Since: since.Unix(), Until: until.Unix(), PreviousSince: previous.Unix(), PreviousUntil: since.Unix(), Project: project,
		Daily: []contract.UsageOverviewDay{}, PreviousDaily: []contract.UsageOverviewDay{}, Projects: []contract.UsageOverviewProject{}, ProjectOptions: []contract.UsageOverviewProjectOption{}}
	rows, err := u.Store.UsageDaily(ctx, previous.Format("2006-01-02"), until.Format("2006-01-02"))
	if err != nil {
		return out, err
	}
	tasks, err := u.Store.UsageOverviewTasks(ctx, previous, until)
	if err != nil {
		return out, err
	}
	var lifetimeEvicted int64
	out.Coverage.SessionsRead, out.Coverage.SessionsTotal, lifetimeEvicted, err = u.Store.UsageDailyCoverage(ctx)
	if err != nil {
		return out, err
	}
	out.Coverage.State = contract.UsageOverviewCoverageStateMissing
	_ = lifetimeEvicted
	evicted, err := u.Store.UsageDailyEvictedInRange(ctx, previous.Format("2006-01-02"), until.Format("2006-01-02"))
	if err != nil {
		return out, err
	}
	out.Coverage.Truncated = evicted > 0
	out.Coverage.TasksTruncated = len(tasks) >= store.UsageOverviewTaskLimit
	options := map[string]contract.UsageOverviewProjectOption{"unattributed": overviewProject("")}
	for _, r := range rows {
		p := overviewProject(r.Project)
		options[p.ID] = p
	}
	for _, t := range tasks {
		p := overviewProject(t.Project)
		options[p.ID] = p
	}
	if project != "" {
		if _, ok := options[project]; !ok {
			return out, ErrUsageOverviewProject
		}
	}
	for _, p := range options {
		out.ProjectOptions = append(out.ProjectOptions, p)
	}
	sort.Slice(out.ProjectOptions, func(i, j int) bool { return out.ProjectOptions[i].ID < out.ProjectOptions[j].ID })
	daily := map[string]*contract.UsageOverviewDay{}
	durations := map[string][]float64{}
	for at := previous; at.Before(until); at = at.AddDate(0, 0, 1) {
		date := at.Format("2006-01-02")
		daily[date] = &contract.UsageOverviewDay{Date: date, Coverage: contract.UsageOverviewCoverageStateMissing}
	}
	bars := map[string]float64{}
	agents := map[string]*float64{"claude": nil, "codex": nil, "gemini": nil, "grok": nil}
	var currentDurations, previousDurations []float64
	for _, r := range rows {
		p := overviewProject(r.Project)
		if project != "" && project != p.ID {
			continue
		}
		day := daily[r.Date]
		if day == nil {
			continue
		}
		value := r.Input + r.CacheRead + r.CacheWrite + r.Output
		if day.Tokens == nil {
			day.Tokens = new(float64)
		}
		*day.Tokens += value
		day.Coverage = contract.UsageOverviewCoverageStatePartial
		out.Coverage.State = contract.UsageOverviewCoverageStatePartial
		at, _ := time.ParseInLocation("2006-01-02", r.Date, time.Local)
		if out.Coverage.HistoryFrom == 0 || at.Unix() < out.Coverage.HistoryFrom {
			out.Coverage.HistoryFrom = at.Unix()
		}
		totals := &out.PreviousTotals
		if !at.Before(since) {
			totals = &out.Totals
			bars[p.ID] += value
			id := r.Assistant
			// The native Gemini driver retains its historical storage identity.
			if id == "antigravity" {
				id = "gemini"
			}
			if id == "" {
				id = "unknown"
			}
			if agents[id] == nil {
				agents[id] = new(float64)
			}
			*agents[id] += value
		}
		if totals.Tokens == nil {
			totals.Tokens = new(float64)
		}
		*totals.Tokens += value
		totals.Parts.Input += r.Input
		totals.Parts.CacheRead += r.CacheRead
		totals.Parts.CacheWrite += r.CacheWrite
		totals.Parts.Output += r.Output
	}
	for _, t := range tasks {
		p := overviewProject(t.Project)
		if project != "" && project != p.ID {
			continue
		}
		date := t.CreatedAt.In(time.Local).Format("2006-01-02")
		if day := daily[date]; day != nil {
			day.Dispatches++
			totals := &out.PreviousTotals
			if !t.CreatedAt.Before(since) {
				totals = &out.Totals
			}
			totals.Dispatches++
		}
		if t.Result == nil || t.Result.Status != "success" || t.SpawnedAt.IsZero() || t.FinishedAt.Before(t.SpawnedAt) {
			continue
		}
		date = t.FinishedAt.In(time.Local).Format("2006-01-02")
		if daily[date] == nil {
			continue
		}
		seconds := t.FinishedAt.Sub(t.SpawnedAt).Seconds()
		durations[date] = append(durations[date], seconds)
		if !t.FinishedAt.Before(since) {
			currentDurations = append(currentDurations, seconds)
		} else {
			previousDurations = append(previousDurations, seconds)
		}
	}
	for at := previous; at.Before(until); at = at.AddDate(0, 0, 1) {
		date := at.Format("2006-01-02")
		d := daily[date]
		d.DurationSamples = int64(len(durations[date]))
		d.DurationMedianSeconds = overviewMedian(durations[date])
		if at.Before(since) {
			out.PreviousDaily = append(out.PreviousDaily, *d)
		} else {
			out.Daily = append(out.Daily, *d)
		}
	}
	out.Totals.DurationSamples = int64(len(currentDurations))
	out.Totals.DurationMedianSeconds = overviewMedian(currentDurations)
	out.PreviousTotals.DurationSamples = int64(len(previousDurations))
	out.PreviousTotals.DurationMedianSeconds = overviewMedian(previousDurations)
	for id, tokens := range bars {
		out.Projects = append(out.Projects, contract.UsageOverviewProject{ID: id, Name: options[id].Name, Tokens: tokens})
	}
	sort.Slice(out.Projects, func(i, j int) bool {
		if out.Projects[i].Tokens == out.Projects[j].Tokens {
			return out.Projects[i].ID < out.Projects[j].ID
		}
		return out.Projects[i].Tokens > out.Projects[j].Tokens
	})
	for id, tokens := range agents {
		out.Agents = append(out.Agents, contract.UsageOverviewAgent{ID: id, Tokens: tokens})
	}
	sort.Slice(out.Agents, func(i, j int) bool {
		a, b := out.Agents[i], out.Agents[j]
		if a.Tokens == nil || b.Tokens == nil {
			if (a.Tokens == nil) != (b.Tokens == nil) {
				return a.Tokens != nil
			}
		} else if *a.Tokens != *b.Tokens {
			return *a.Tokens > *b.Tokens
		}
		return a.ID < b.ID
	})
	return out, nil
}

func overviewMedian(v []float64) *float64 {
	if len(v) == 0 {
		return nil
	}
	n := medianFloat(v)
	return &n
}
