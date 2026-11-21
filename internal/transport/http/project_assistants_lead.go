package http

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func projectAssistantDefaultLeadAgent(now time.Time) pa.Agent {
	for _, agent := range []pa.Agent{pa.ClaudeCode, pa.Codex} {
		if quotaReader().Observe(string(agent), now).Installed {
			return agent
		}
	}
	// The lead's workspace and conversation exist before any provider setup.
	return pa.Codex
}

func projectAssistantHostTimeZone() string {
	zone := strings.TrimPrefix(os.Getenv("TZ"), ":")
	if zone == "" {
		if path, err := filepath.EvalSymlinks("/etc/localtime"); err == nil {
			_, zone, _ = strings.Cut(filepath.ToSlash(path), "/zoneinfo/")
		}
	}
	if zone != "" {
		if _, err := time.LoadLocation(zone); err == nil {
			return zone
		}
	}
	return pa.HostRoutineTimeZone
}

func projectAssistantLeadQuotaNear(bot pa.Bot, now time.Time) bool {
	if bot.LoginProfile != "" {
		return false // machine counters do not describe this selected identity
	}
	quota := quotaReader().Observe(string(bot.Agent), now)
	if quota.Stale {
		return false
	}
	for _, window := range quota.Windows {
		if window.Hit || window.UsedPercent != nil && *window.UsedPercent > 75 {
			return true
		}
	}
	return false
}
