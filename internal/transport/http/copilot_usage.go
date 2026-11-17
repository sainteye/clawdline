package http

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/google/uuid"
)

// Call snapshots are cumulative, private estimates. Only the newest snapshot
// per call is counted, including after reconnects, duplicate delivery or reordering.
// A call belongs to its UTC start date; a midnight-spanning call stays together.
type copilotUsageSnapshot struct {
	Seq     int                `json:"seq"`
	Started int64              `json:"started"`
	Engine  string             `json:"engine"`
	Model   string             `json:"model"`
	USD     float64            `json:"usd"`
	Parts   map[string]float64 `json:"parts"`
	Seconds float64            `json:"seconds"`
	Input   float64            `json:"input_tokens"`
	Output  float64            `json:"output_tokens"`
	Cached  float64            `json:"cached_tokens"`
	Final   bool               `json:"final"`
	Priced  bool               `json:"priced"`
}

type copilotUsageDay struct {
	Known   int                `json:"known"`
	Date    string             `json:"date"`
	Calls   int                `json:"calls"`
	Missing int                `json:"missing"`
	Partial int                `json:"partial"`
	USD     float64            `json:"usd"`
	Parts   map[string]float64 `json:"parts"`
	Seconds float64            `json:"seconds"`
	Input   float64            `json:"input_tokens"`
	Output  float64            `json:"output_tokens"`
	Cached  float64            `json:"cached_tokens"`
}

type copilotUsageHistory struct {
	Days  []copilotUsageDay `json:"days"`
	Error string            `json:"error,omitempty"`
}

func (s *Server) copilotUsageHistory(now time.Time) copilotUsageHistory {
	out := copilotUsageHistory{Days: []copilotUsageDay{}}
	names, err := os.ReadDir(s.copilotJournalDir())
	if errors.Is(err, os.ErrNotExist) {
		return out
	}
	if err != nil {
		out.Error = "voice_usage_unreadable"
		return out
	}
	type held struct {
		at    int64
		start int64
		usage *copilotUsageSnapshot
	}
	calls := map[string]held{}
	oldest := now.Add(-maxCopilotJournalAge).Format("2006-01-02")
	for _, name := range names {
		if !copilotJournalFile.MatchString(name.Name()) || name.Name() < oldest {
			continue
		}
		f, err := os.Open(filepath.Join(s.copilotJournalDir(), name.Name()))
		if err != nil {
			out.Error = "voice_usage_unreadable"
			continue
		}
		dec := json.NewDecoder(f)
		for {
			var e struct {
				Call string `json:"call"`
				copilotJournalEntry
			}
			err = dec.Decode(&e)
			if err == io.EOF {
				break
			}
			if err != nil {
				out.Error = "voice_usage_incomplete"
				break
			}
			if e.Call == "" {
				continue
			}
			c := calls[e.Call]
			if e.Kind == "start" && (c.start == 0 || e.At < c.start) {
				c.start = e.At
			}
			if e.Kind == "notice" && e.Text == "usage_snapshot" {
				var u copilotUsageSnapshot
				if json.Unmarshal(e.Tool, &u) == nil && validCopilotUsage(u) && (c.usage == nil || u.Seq > c.usage.Seq || u.Seq == c.usage.Seq && (e.At > c.at || e.At == c.at && (!c.usage.Final || u.Final))) {
					c.at = e.At
					c.usage = &u
				}
			}
			calls[e.Call] = c
		}
		_ = f.Close()
	}
	days := map[string]*copilotUsageDay{}
	for _, c := range calls {
		start := c.start
		if c.usage != nil && c.usage.Started > 0 {
			start = c.usage.Started
		}
		if start <= 0 {
			continue
		}
		date := time.UnixMilli(start).UTC().Format("2006-01-02")
		if date < now.UTC().Add(-maxCopilotJournalAge+24*time.Hour).Format("2006-01-02") {
			continue
		}
		d := days[date]
		if d == nil {
			d = &copilotUsageDay{Date: date, Parts: map[string]float64{}}
			days[date] = d
		}
		if c.usage == nil {
			d.Calls++
			d.Missing++
			continue
		}
		u := c.usage
		if u.Engine != "analysis" {
			d.Calls++
		}
		if u.Priced {
			d.Known++
		}
		if !u.Final || !u.Priced {
			d.Partial++
		}
		d.USD += u.USD
		d.Seconds += u.Seconds
		d.Input += u.Input
		d.Output += u.Output
		d.Cached += u.Cached
		for k, v := range u.Parts {
			d.Parts[k] += v
		}
	}
	for _, d := range days {
		out.Days = append(out.Days, *d)
	}
	sort.Slice(out.Days, func(i, j int) bool { return out.Days[i].Date > out.Days[j].Date })
	return out
}

func validCopilotUsage(u copilotUsageSnapshot) bool {
	if u.Started <= 0 || u.Engine == "" {
		return false
	}
	for _, n := range []float64{u.USD, u.Seconds, u.Input, u.Output, u.Cached} {
		if n < 0 || math.IsInf(n, 0) || math.IsNaN(n) {
			return false
		}
	}
	sum := 0.0
	for _, n := range u.Parts {
		if n < 0 || math.IsInf(n, 0) || math.IsNaN(n) {
			return false
		}
		sum += n
	}
	return math.Abs(sum-u.USD) < 0.000001
}

// Independent analysis requests also cost money, even if their generated answer
// fails validation. Usage is recorded on the machine that made the request.
func (s *Server) recordCopilotChatUsage(model string, usage copilotChatUsage) {
	if usage.Prompt+usage.Completion <= 0 {
		return
	}
	rates := map[string][3]float64{"gpt-4.1-mini": {0.4, 0.1, 1.6}}
	rate, known := rates[model]
	cached := min(usage.Prompt, max(0, usage.Details.Cached))
	usd := (float64(max(0, usage.Prompt-cached))*rate[0] + float64(cached)*rate[1] + float64(max(0, usage.Completion))*rate[2]) / 1e6
	now := time.Now()
	u := copilotUsageSnapshot{Started: now.UnixMilli(), Engine: "analysis", Model: model, USD: usd, Parts: map[string]float64{"analysis": usd}, Input: float64(max(0, usage.Prompt)), Output: float64(max(0, usage.Completion)), Cached: float64(cached), Final: true, Priced: known}
	raw, _ := json.Marshal(u)
	line, _ := json.Marshal(struct {
		Call string `json:"call"`
		copilotJournalEntry
	}{Call: "analysis-" + uuid.NewString(), copilotJournalEntry: copilotJournalEntry{At: now.UnixMilli(), Kind: "notice", Text: "usage_snapshot", Tool: raw}})
	if err := s.appendCopilotJournal(now, append(line, '\n')); err != nil {
		log.Printf("voice usage journal: %v", err)
	}
}
