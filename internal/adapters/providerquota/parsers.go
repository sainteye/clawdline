package providerquota

import (
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"time"
)

func resetAt(raw string) *int64 {
	t, e := time.Parse(time.RFC3339Nano, raw)
	if e != nil {
		return nil
	}
	v := t.Unix()
	return &v
}
func valid(n float64) bool    { return !math.IsNaN(n) && !math.IsInf(n, 0) }
func clamp(n float64) float64 { return math.Min(100, math.Max(0, n)) }
func parseGemini(b []byte) ([]Window, error) {
	type bucket struct {
		Remaining *float64 `json:"remainingFraction"`
		Reset     string   `json:"resetTime"`
		Model     string   `json:"modelId"`
	}
	var data struct {
		Buckets []bucket `json:"buckets"`
	}
	var buckets []bucket
	if len(b) > 0 && b[0] == '[' {
		if e := json.Unmarshal(b, &buckets); e != nil {
			return nil, e
		}
	} else {
		if e := json.Unmarshal(b, &data); e != nil {
			return nil, e
		}
		buckets = data.Buckets
	}
	out := []Window{}
	seen := map[string]bool{}
	for _, v := range buckets {
		if v.Remaining == nil || !valid(*v.Remaining) || *v.Remaining < 0 || *v.Remaining > 1 || v.Model == "" {
			continue
		}
		key := v.Model + "\x00" + v.Reset
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, Window{Name: v.Model, UsedPercent: (1 - *v.Remaining) * 100, ResetsAt: resetAt(v.Reset)})
	}
	return out, nil
}

type money struct {
	Val json.RawMessage `json:"val"`
}

func (m *money) number() (float64, bool) {
	if m == nil || len(m.Val) == 0 {
		return 0, false
	}
	var n float64
	if json.Unmarshal(m.Val, &n) == nil {
		return n, valid(n)
	}
	var str string
	if json.Unmarshal(m.Val, &str) != nil {
		return 0, false
	}
	n, e := strconv.ParseFloat(str, 64)
	return n, e == nil && valid(n)
}
func parseGrok(b []byte) ([]Window, error) {
	type period struct {
		Type  string `json:"type"`
		Start string `json:"start"`
		End   string `json:"end"`
	}
	type config struct {
		Percent *float64 `json:"creditUsagePercent"`
		Period  *period  `json:"currentPeriod"`
		Start   string   `json:"billingPeriodStart"`
		End     string   `json:"billingPeriodEnd"`
		Monthly *money   `json:"monthlyLimit"`
		Used    *money   `json:"used"`
	}
	var wrapper struct {
		Config json.RawMessage `json:"config"`
	}
	if e := json.Unmarshal(b, &wrapper); e != nil {
		return nil, e
	}
	if len(wrapper.Config) > 0 && string(wrapper.Config) != "null" {
		b = wrapper.Config
	}
	var c config
	if e := json.Unmarshal(b, &c); e != nil {
		return nil, e
	}
	if c.Percent != nil {
		if !valid(*c.Percent) {
			return nil, errors.New("invalid percent")
		}
		end := c.End
		if c.Period != nil && c.Period.End != "" {
			end = c.Period.End
		}
		return []Window{{Name: "7d", UsedPercent: clamp(*c.Percent), ResetsAt: resetAt(end)}}, nil
	}
	if cap, ok := c.Monthly.number(); ok && cap > 0 {
		if used, ok := c.Used.number(); ok && used >= 0 {
			return []Window{{Name: "monthly", UsedPercent: clamp(used / cap * 100), ResetsAt: resetAt(c.End)}}, nil
		}
	}
	// Protobuf JSON may omit scalar zero. Infer it only from an explicitly weekly,
	// dated period, never from a missing field or an overflow-spending balance.
	if c.Period != nil && c.Period.Type == "USAGE_PERIOD_TYPE_WEEKLY" && resetAt(c.Period.Start) != nil && resetAt(c.Period.End) != nil && c.Start == c.Period.Start && c.End == c.Period.End {
		return []Window{{Name: "7d", UsedPercent: 0, ResetsAt: resetAt(c.Period.End)}}, nil
	}
	return []Window{}, nil
}
