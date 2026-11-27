package http

import (
	"database/sql"
	"errors"
	"maps"
	"sync"

	"github.com/sainteye/clawdline/internal/domain/capacity"
)

type charterCapacityUsage struct {
	versions, proposals, projects, textBytes, reasonBytes int64
	openConflicts, handledConflicts, conflictBytes        int64
	playbooks, playbookText, playbookTitle, playbookBots  int64
	playbookID, documentBytes                             int64
	workflowStates, workflowTransitions, workflowTitle    int64
	workflowNote, workflowID                              int64
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
	if err = readPlaybookCapacity(db, &usage); err != nil {
		return usage, err
	}
	if err = readWorkflowCapacity(db, &usage); err != nil {
		return usage, err
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

// readPlaybookCapacity reads the playbooks of every retained version, when
// the version table has the playbook column yet.
func readPlaybookCapacity(db interface {
	QueryRow(string, ...any) *sql.Row
}, usage *charterCapacityUsage) error {
	var column int
	if err := db.QueryRow(`SELECT count(*) FROM pragma_table_info('project_assistant_charter_versions') WHERE name='playbooks'`).Scan(&column); err != nil {
		return errors.New("Bot charter playbook column cannot be inspected")
	}
	if column == 0 {
		return nil
	}
	err := db.QueryRow(`WITH p AS (
		SELECT v.machine, v.project, v.version, octet_length(v.text) AS charter_bytes,
			octet_length(json_extract(e.value,'$.text')) AS text_bytes, octet_length(json_extract(e.value,'$.title')) AS title_bytes,
			octet_length(json_extract(e.value,'$.id')) AS id_bytes, json_array_length(e.value,'$.bot_ids') AS bots
		FROM project_assistant_charter_versions v, json_each(v.playbooks) e
	), per AS (
		SELECT count(*) AS n, max(charter_bytes) + sum(text_bytes) AS document FROM p GROUP BY machine, project, version
	)
	SELECT coalesce((SELECT max(n) FROM per), 0), coalesce((SELECT max(text_bytes) FROM p), 0), coalesce((SELECT max(title_bytes) FROM p), 0),
		coalesce((SELECT max(bots) FROM p), 0), coalesce((SELECT max(id_bytes) FROM p), 0),
		max(coalesce((SELECT max(document) FROM per), 0), coalesce((SELECT max(octet_length(text)) FROM project_assistant_charter_versions), 0))`).
		Scan(&usage.playbooks, &usage.playbookText, &usage.playbookTitle, &usage.playbookBots, &usage.playbookID, &usage.documentBytes)
	if err != nil {
		return errors.New("Bot charter playbooks cannot be read")
	}
	return nil
}

// readWorkflowCapacity reads the workflows of every retained version and
// pending proposal, when both tables have the workflow column yet.
func readWorkflowCapacity(db interface {
	QueryRow(string, ...any) *sql.Row
}, usage *charterCapacityUsage) error {
	var columns int
	if err := db.QueryRow(`SELECT (SELECT count(*) FROM pragma_table_info('project_assistant_charter_versions') WHERE name='workflow') +
		(SELECT count(*) FROM pragma_table_info('project_assistant_charter_proposals') WHERE name='workflow')`).Scan(&columns); err != nil {
		return errors.New("Bot charter workflow columns cannot be inspected")
	}
	if columns < 2 {
		return nil
	}
	err := db.QueryRow(`WITH w AS (
		SELECT workflow AS flow FROM project_assistant_charter_versions WHERE workflow<>''
		UNION ALL SELECT workflow FROM project_assistant_charter_proposals WHERE workflow<>''
	), st AS (
		SELECT octet_length(json_extract(e.value,'$.title')) AS title, octet_length(json_extract(e.value,'$.note')) AS note, octet_length(json_extract(e.value,'$.id')) AS id
		FROM w, json_each(w.flow,'$.states') e
	), tr AS (
		SELECT octet_length(json_extract(e.value,'$.label')) AS label FROM w, json_each(w.flow,'$.transitions') e
	)
	SELECT coalesce((SELECT max(json_array_length(flow,'$.states')) FROM w), 0), coalesce((SELECT max(json_array_length(flow,'$.transitions')) FROM w), 0),
		max(coalesce((SELECT max(title) FROM st), 0), coalesce((SELECT max(label) FROM tr), 0)), coalesce((SELECT max(note) FROM st), 0), coalesce((SELECT max(id) FROM st), 0)`).
		Scan(&usage.workflowStates, &usage.workflowTransitions, &usage.workflowTitle, &usage.workflowNote, &usage.workflowID)
	if err != nil {
		return errors.New("Bot charter workflows cannot be read")
	}
	return nil
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
		"bots.charter_playbooks":               charter(func(u charterCapacityUsage) int64 { return u.playbooks }, "largest playbook count of one retained charter version"),
		"bots.charter_playbook_text_bytes":     charter(func(u charterCapacityUsage) int64 { return u.playbookText }, "largest retained playbook text in UTF-8 bytes"),
		"bots.charter_playbook_title_bytes":    charter(func(u charterCapacityUsage) int64 { return u.playbookTitle }, "largest retained playbook title in UTF-8 bytes"),
		"bots.charter_playbook_bots":           charter(func(u charterCapacityUsage) int64 { return u.playbookBots }, "largest count of Bots one retained playbook is assigned to"),
		"bots.charter_playbook_id_bytes":       charter(func(u charterCapacityUsage) int64 { return u.playbookID }, "largest retained daemon-issued playbook identifier in bytes"),
		"bots.charter_document_bytes":          charter(func(u charterCapacityUsage) int64 { return u.documentBytes }, "largest retained charter text plus all its playbook text in UTF-8 bytes"),
		"bots.charter_workflow_states":         charter(func(u charterCapacityUsage) int64 { return u.workflowStates }, "largest state count of one retained or proposed workflow"),
		"bots.charter_workflow_transitions":    charter(func(u charterCapacityUsage) int64 { return u.workflowTransitions }, "largest transition count of one retained or proposed workflow"),
		"bots.charter_workflow_title_bytes":    charter(func(u charterCapacityUsage) int64 { return u.workflowTitle }, "largest retained or proposed workflow state title or transition label in UTF-8 bytes"),
		"bots.charter_workflow_note_bytes":     charter(func(u charterCapacityUsage) int64 { return u.workflowNote }, "largest retained or proposed workflow state note in UTF-8 bytes"),
		"bots.charter_workflow_id_bytes":       charter(func(u charterCapacityUsage) int64 { return u.workflowID }, "largest retained or proposed workflow state identifier in bytes"),
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
