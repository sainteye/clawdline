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
