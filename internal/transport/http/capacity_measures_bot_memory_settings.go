package http

import "github.com/sainteye/clawdline/internal/domain/capacity"

func (s *Server) memorySettingsMeasure(query, note string) func() capacity.Reading {
	return func() capacity.Reading {
		if s.store == nil {
			return capacity.Unmeasured("Bot settings store unavailable")
		}
		db := s.store.ProjectAssistantDatabase()
		var exists, used int64
		if err := db.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE type='table' AND name='project_assistant_bots'`).Scan(&exists); err != nil {
			return capacity.Unmeasured("Bot settings table cannot be inspected")
		}
		if exists == 0 {
			return capacity.Reading{Known: true, Note: "optional Bot settings table is absent; no durable settings"}
		}
		if err := db.QueryRow(query).Scan(&used); err != nil {
			return capacity.Unmeasured("Bot settings rows cannot be read")
		}
		return capacity.Reading{Known: true, Used: used, Note: note}
	}
}

func (s *Server) loginProfileMeasure() capacity.Reading {
	if s.store == nil {
		return capacity.Unmeasured("Bot login store unavailable")
	}
	db := s.store.ProjectAssistantDatabase()
	var exists, used int64
	if e := db.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE type='table' AND name='project_assistant_login_profiles'`).Scan(&exists); e != nil {
		return capacity.Unmeasured("Bot login table cannot be inspected")
	}
	if exists == 0 {
		return capacity.Reading{Known: true, Note: "optional Bot login table is absent"}
	}
	if e := db.QueryRow(`SELECT coalesce(max(n),0) FROM (SELECT count(*) n FROM project_assistant_login_profiles WHERE removed=0 OR state IN ('unknown','cleanup_unknown') GROUP BY source)`).Scan(&used); e != nil {
		return capacity.Unmeasured("Bot login rows cannot be read")
	}
	return capacity.Reading{Known: true, Used: used, Note: "largest actual managed account profile count per source account and machine"}
}
