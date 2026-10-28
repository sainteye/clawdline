package http

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/antigravity"
	"github.com/sainteye/clawdline/internal/adapters/antigravitylaunch"
	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// SetAntigravityBridge publishes the same verified bridge used by inventory.
func (s *Server) SetAntigravityBridge(bridge *antigravity.Bridge) { s.antigravityBridge.Store(bridge) }

func (s *Server) antigravityCapacityMeasures() map[string]func() capacity.Reading {
	// Each capacity pass rebuilds this map. Share one integrity-checked snapshot
	// across its owned rows, never five complete filesystem reads or a boot cache.
	var once sync.Once
	var launchStats antigravitylaunch.Stats
	var launchErr error
	launchReading := func(part string) capacity.Reading {
		once.Do(func() {
			owner := s.antigravityLaunchStore.Load()
			if owner == nil {
				launchErr = &antigravitylaunch.Error{Code: "antigravity_context_owner_unavailable"}
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), antigravity.MaxProcessReadTime)
			defer cancel()
			launchStats, launchErr = owner.Stats(ctx)
		})
		if launchErr != nil {
			return capacity.Unmeasured(launchErr.Error())
		}
		var n int64
		switch part {
		case "launches":
			n = int64(launchStats.Launches)
		case "history":
			n = int64(launchStats.History)
		case "first_inputs":
			n = int64(launchStats.FirstInputs)
		case "receipts":
			n = int64(launchStats.Receipts)
		case "index":
			n = launchStats.IndexBytes
		case "context":
			n = launchStats.LargestContextBytes
		}
		return capacity.Reading{Known: true, Used: n}
	}

	stats := func(part string) capacity.Reading {
		b := s.antigravityBridge.Load()
		if b == nil {
			return capacity.Reading{Known: true, Note: "bridge inactive; no runtime observation cache is allocated"}
		}
		st := b.ObservationStats()
		switch part {
		case "hook_connections":
			return capacity.Reading{Known: true, Used: int64(st.HookConnections)}
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
		"antigravity.history_connections": func() capacity.Reading {
			if h := s.antigravitySessions.Load(); h != nil {
				open, inUse, waits, waited := h.HistoryConnectionStats()
				return capacity.Reading{Known: true, Used: int64(open), Note: fmt.Sprintf("%d open, %d in use; %d waits totaling %s since this process started", open, inUse, waits, waited)}
			}
			return capacity.Reading{Known: true, Note: "history connection pool is inactive"}
		},
		"antigravity.acp_sessions": func() capacity.Reading {
			var n int
			if h := s.antigravitySessions.Load(); h != nil {
				n, _ = h.Stats()
			}
			return capacity.Reading{Known: true, Used: int64(n)}
		},
		"antigravity.acp_view_bytes": func() capacity.Reading {
			var n int
			if h := s.antigravitySessions.Load(); h != nil {
				_, n = h.Stats()
			}
			return capacity.Reading{Known: true, Used: int64(n)}
		},
		"antigravity.acp_frame_bytes": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-frame guard; no replay queue"}
		},
		"antigravity.acp_call_seconds": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-call deadline; no automatic retry"}
		},
		// Missing, closed or unreadable owners remain unknown.
		"antigravity.first_input_slots":    func() capacity.Reading { return launchReading("first_inputs") },
		"antigravity.saved_launches":       func() capacity.Reading { return launchReading("launches") },
		"antigravity.context_history":      func() capacity.Reading { return launchReading("history") },
		"antigravity.context_receipts":     func() capacity.Reading { return launchReading("receipts") },
		"antigravity.context_index_bytes":  func() capacity.Reading { return launchReading("index") },
		"antigravity.launch_context_bytes": func() capacity.Reading { return launchReading("context") },
		"antigravity.hook_connections":     func() capacity.Reading { return stats("hook_connections") },
		"antigravity.hook_frame_bytes": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-frame guard; no retained frame buffer"}
		},
		"antigravity.hook_request_seconds": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-request deadline; no retry or work queue"}
		},
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
