package http

import "github.com/sainteye/clawdline/internal/domain/capacity"

func (s *Server) botRoutineHookCapacityMeasures() map[string]func() capacity.Reading {
	validator := func() capacity.Reading {
		return capacity.Reading{Known: true, Note: "request-owned webhook validator bound; no retained allocation"}
	}
	timerPolicy := func() capacity.Reading {
		return capacity.Reading{Known: true, Note: "configured webhook timer policy; no retained occupancy or measured request latency"}
	}
	return map[string]func() capacity.Reading{
		"bots.hook_hooks":           validator,
		"bots.hook_pending":         s.botRoutineHookStoredMeasure("project_assistant_cloud_hook_pending", `SELECT coalesce(max(n),0) FROM (SELECT count(*) n FROM project_assistant_cloud_hook_pending GROUP BY account,machine)`, "largest durable forwarding barrier count per account and machine"),
		"bots.hook_enroll_bytes":    validator,
		"bots.hook_event_bytes":     validator,
		"bots.hook_header_bytes":    validator,
		"bots.hook_id_bytes":        validator,
		"bots.hook_response_bytes":  validator,
		"bots.hook_local_bytes":     validator,
		"bots.hook_http_seconds":    timerPolicy,
		"bots.hook_poll_seconds":    timerPolicy,
		"bots.hook_backoff_seconds": timerPolicy,
		"bots.hook_revision":        s.botRoutineHookStoredMeasure("project_assistant_cloud_hooks", `SELECT coalesce(max(revision),0) FROM project_assistant_cloud_hooks`, "highest durable enrollment revision across account and machine namespaces"),
	}
}

// Capacity reads never install the optional module or create hook authority.
func (s *Server) botRoutineHookStoredMeasure(table, query, note string) func() capacity.Reading {
	return func() capacity.Reading {
		if s.store == nil {
			return capacity.Unmeasured("webhook source store is unavailable")
		}
		db := s.store.ProjectAssistantDatabase()
		var exists, used int64
		if err := db.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE type='table' AND name=?`, table).Scan(&exists); err != nil {
			return capacity.Unmeasured("webhook source table cannot be inspected")
		}
		if exists == 0 {
			return capacity.Reading{Known: true, Note: "optional webhook source table is absent; no durable rows"}
		}
		if err := db.QueryRow(query).Scan(&used); err != nil {
			return capacity.Unmeasured("webhook source rows cannot be read")
		}
		return capacity.Reading{Known: true, Used: used, Note: note}
	}
}
