package swiftstore

import (
	"path/filepath"
)

// QuotaSettings are the Swift app's settings that decide how its plan windows
// are read. Empty and zero are the defaults.
type QuotaSettings struct {
	StatusDir    string
	CodexHome    string
	LowThreshold float64
}

type quotaConfigFile struct {
	StatusDir    any `json:"status_dir"`
	CodexHome    any `json:"codex_home"`
	LowThreshold any `json:"assistant_quota_low_threshold"`
}

// QuotaConfig decodes config.json only when it changed.
type QuotaConfig struct {
	config *file[quotaConfigFile]
}

// OpenQuotaConfig prepares a reader of dir/config.json.
func OpenQuotaConfig(dir string) *QuotaConfig {
	if Disabled() {
		return &QuotaConfig{}
	}
	return &QuotaConfig{config: newFile[quotaConfigFile](filepath.Join(dir, "config.json"))}
}

// Read returns valid settings and leaves malformed or unavailable values at
// their defaults.
func (q *QuotaConfig) Read() QuotaSettings {
	var out QuotaSettings
	if q == nil || q.config == nil {
		return out
	}
	r := q.config.read()
	if !r.Known || r.Missing {
		return out
	}
	if v, ok := r.Value.StatusDir.(string); ok {
		out.StatusDir = v
	}
	if v, ok := r.Value.CodexHome.(string); ok {
		out.CodexHome = v
	}
	if v, ok := r.Value.LowThreshold.(float64); ok && v > 0 && v < 100 {
		out.LowThreshold = v
	}
	return out
}
