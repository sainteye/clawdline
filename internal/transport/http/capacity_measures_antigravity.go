package http

import (
	"github.com/sainteye/clawdline/internal/adapters/antigravity"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	"time"
)

// SetAntigravityBridge publishes the same verified bridge used by inventory.
func (s *Server) SetAntigravityBridge(bridge *antigravity.Bridge) { s.antigravityBridge.Store(bridge) }

func (s *Server) antigravityCapacityMeasures() map[string]func() capacity.Reading {
	stats := func(part string) capacity.Reading {
		b := s.antigravityBridge.Load()
		if b == nil {
			return capacity.Reading{Known: true, Note: "bridge inactive; no runtime observation cache is allocated"}
		}
		st := b.ObservationStats()
		switch part {
		case "bindings":
			return capacity.Reading{Known: true, Used: int64(st.Bindings)}
		case "launches":
			return capacity.Reading{Known: true, Used: int64(st.Launches)}
		case "bytes":
			return capacity.Reading{Known: true, Used: st.Bytes}
		case "age":
			var age int64
			if !st.OldestAt.IsZero() {
				age = max(0, int64(time.Since(st.OldestAt).Seconds()))
			}
			return capacity.Reading{Known: true, Used: age, Note: "expired observations are reclaimed on new input"}
		}
		return capacity.Unmeasured("unknown bridge measure")
	}
	return map[string]func() capacity.Reading{
		"antigravity.native_process_file_bytes": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-operation guard; no retained buffer"}
		},
		"antigravity.native_tty_entries": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-operation guard; no retained buffer"}
		},
		"antigravity.hook_payload_bytes": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-operation guard; no retained buffer"}
		},
		"antigravity.observation_bytes": func() capacity.Reading { return stats("bytes") },
		"antigravity.live_bindings":     func() capacity.Reading { return stats("bindings") },
		"antigravity.managed_launches":  func() capacity.Reading { return stats("launches") },
		"antigravity.ancestry_rows": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-operation guard; no retained buffer"}
		},
		"antigravity.state_age_seconds": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-operation guard; no retained buffer"}
		},
		"antigravity.retention_seconds": func() capacity.Reading { return stats("age") },
		"antigravity.process_read_seconds": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-operation guard; no retained buffer"}
		},
		"antigravity.hook_config_bytes": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-operation guard; no retained buffer"}
		},
		"antigravity.hook_timeout_seconds": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-operation guard; no retained buffer"}
		},
	}
}
