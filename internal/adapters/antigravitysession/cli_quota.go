package antigravitysession

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/contract"
)

// Public CLI 1.5 --print /usage emits four tab-separated columns. Parse only
// recognized numeric Gemini buckets; model output can never supply a balance.
func parseCLIQuota(text string, at time.Time) *contract.SessionLimits {
	unknown := func() *contract.SessionLimits {
		q := unknownQuota(contract.AssistantUnknownReasonNoReading)
		q.Detail = "Gemini · CLI /usage"
		return q
	}
	out := unknown()
	found := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		fields := strings.Split(strings.TrimSuffix(line, "\r"), "\t")
		if fields[0] != "Gemini Models" && fields[0] != "Gemini 4 Argon" {
			continue
		}
		if len(fields) != 4 {
			return unknown()
		}
		name := ""
		switch fields[0] + ":" + fields[1] {
		case "Gemini Models:Weekly Limit Remaining":
			name = "7d"
		case "Gemini Models:Five Hour Limit Remaining":
			name = "5h"
		case "Gemini 4 Argon:Gemini 4 Argon Weekly Limit":
			name = "Argon 7d"
		default:
			return unknown()
		}
		// Reuse the strict numeric vocabulary of the legacy metadata report.
		match := quotaRemaining.FindStringSubmatch("- **" + fields[1] + ":** " + strings.TrimSuffix(fields[2], "%") + "% remaining")
		if match == nil || !strings.HasSuffix(fields[2], "%") || found[name] {
			return unknown()
		}
		remaining, err := strconv.ParseFloat(match[2], 64)
		if err != nil || remaining < 0 || remaining > 100 {
			return unknown()
		}
		reset, err := time.Parse(time.RFC3339, fields[3])
		if err != nil {
			return unknown()
		}
		out.Windows = append(out.Windows, contract.SessionLimitWindow{Name: name, UsedPercent: 100 - remaining, Hit: remaining == 0, ResetsAt: reset.Unix()})
		found[name] = true
	}
	if len(found) > 0 {
		sort.SliceStable(out.Windows, func(i, j int) bool { return out.Windows[i].Name < out.Windows[j].Name })
		out.UnknownReason = ""
		out.At, out.ReadAtMs = at.Unix(), at.UnixMilli()
		age := int64(0)
		out.AgeSeconds = &age
	}
	return out
}
