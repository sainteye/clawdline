package http

import (
	"errors"
	"maps"
	"sync"

	"github.com/sainteye/clawdline/internal/domain/capacity"
)

type charterCapacityUsage struct {
	versions, proposals, projects, textBytes, reasonBytes int64
	openConflicts, handledConflicts, conflictBytes        int64
}

func (s *Server) readCharterCapacity() (charterCapacityUsage, error) {
	var usage charterCapacityUsage
	if s.store == nil {
		return usage, errors.New("Bot charter store unavailable")
	}
	db := s.store.ProjectAssistantReader()
	var tables int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE type='table'
		AND name IN ('project_assistant_charter_versions', 'project_assistant_charter_proposals')`).Scan(&tables); err != nil {
		return usage, errors.New("Bot charter tables cannot be inspected")
	}
	if tables == 0 {
		return usage, nil // The optional Bot schema has not installed the charter tables.
	}
	// SQLite can read octet_length of a text column without loading its contents.
	err := db.QueryRow(`WITH versions AS (
		SELECT machine, project, count(*) AS n, max(octet_length(text)) AS text_bytes,
			max(octet_length(reason)) AS reason_bytes
		FROM project_assistant_charter_versions GROUP BY machine, project
	), proposals AS (
		SELECT count(*) AS n, max(octet_length(text)) AS text_bytes,
			max(octet_length(summary)) AS reason_bytes
		FROM project_assistant_charter_proposals GROUP BY machine, project
	), projects AS (
		SELECT count(*) AS n FROM versions GROUP BY machine
	)
	SELECT coalesce((SELECT max(n) FROM versions), 0), coalesce((SELECT max(n) FROM proposals), 0),
		coalesce((SELECT max(n) FROM projects), 0),
		max(coalesce((SELECT max(text_bytes) FROM versions), 0), coalesce((SELECT max(text_bytes) FROM proposals), 0)),
		max(coalesce((SELECT max(reason_bytes) FROM versions), 0), coalesce((SELECT max(reason_bytes) FROM proposals), 0))`).
		Scan(&usage.versions, &usage.proposals, &usage.projects, &usage.textBytes, &usage.reasonBytes)
	if err != nil {
		return usage, errors.New("Bot charter rows cannot be read")
	}
	var conflicts int
	if err = db.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE type='table' AND name='project_assistant_charter_conflicts'`).Scan(&conflicts); err != nil {
		return usage, errors.New("Bot charter conflict table cannot be inspected")
	}
	if conflicts == 0 {
		return usage, nil
	}
	err = db.QueryRow(`WITH per AS (
		SELECT sum(state='open') AS open, sum(state<>'open') AS handled FROM project_assistant_charter_conflicts GROUP BY machine, project
	)
	SELECT coalesce((SELECT max(open) FROM per), 0), coalesce((SELECT max(handled) FROM per), 0),
		coalesce((SELECT max(max(octet_length(bot_sentence)), max(octet_length(charter_sentence)), max(octet_length(section)), max(octet_length(suggested)), max(octet_length(path)), max(octet_length(memory_entry))) FROM project_assistant_charter_conflicts), 0)`).
		Scan(&usage.openConflicts, &usage.handledConflicts, &usage.conflictBytes)
	if err != nil {
		return usage, errors.New("Bot charter conflict rows cannot be read")
	}
	return usage, nil
}

func (s *Server) withCharterCapacityMeasures(measures map[string]func() capacity.Reading) map[string]func() capacity.Reading {
	usage := sync.OnceValues(s.readCharterCapacity)
	charter := func(value func(charterCapacityUsage) int64, note string) func() capacity.Reading {
		return func() capacity.Reading {
			read, err := usage()
			if err != nil {
				return capacity.Unmeasured(err.Error())
			}
			return capacity.Reading{Known: true, Used: value(read), Note: note}
		}
	}
	maps.Copy(measures, map[string]func() capacity.Reading{
		"bots.charter_text_bytes":   charter(func(u charterCapacityUsage) int64 { return u.textBytes }, "largest retained charter version or proposal text in UTF-8 bytes"),
		"bots.charter_reason_bytes": charter(func(u charterCapacityUsage) int64 { return u.reasonBytes }, "largest retained version reason or proposal summary in UTF-8 bytes"),
		"bots.charter_versions":     charter(func(u charterCapacityUsage) int64 { return u.versions }, "largest retained charter version count per Project and machine"),
		"bots.charter_proposals":    charter(func(u charterCapacityUsage) int64 { return u.proposals }, "largest pending charter proposal count per Project and machine"),
		"bots.charter_projects":     charter(func(u charterCapacityUsage) int64 { return u.projects }, "largest count of Projects with a saved charter per machine"),
		"bots.charter_conflicts":    charter(func(u charterCapacityUsage) int64 { return u.openConflicts }, "largest open charter conflict report count per Project and machine"),
		"bots.charter_conflicts_resolved": charter(func(u charterCapacityUsage) int64 { return u.handledConflicts },
			"largest kept handled or superseded charter conflict report count per Project and machine"),
		"bots.charter_conflict_sentence_bytes": charter(func(u charterCapacityUsage) int64 { return u.conflictBytes }, "largest quoted sentence, heading, path or entry id of a kept conflict report in UTF-8 bytes"),
		"bots.charter_request_bytes": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-change-request text guard; accepted text belongs to Bot conversation storage"}
		},
		"bots.charter_body_bytes": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-charter-save HTTP decode guard; no retained request body"}
		},
	})
	return measures
}

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
	return capacity.Reading{Known: true, Used: used, Note: "largest actual managed login profile count per source account and machine"}
}
