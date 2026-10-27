package http

import (
	"context"
	"time"

	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// sessionCapacityMeasures measures every row of the register's "session" area
// (capacity.AreaOf), one reading per row.
func (s *Server) sessionCapacityMeasures() map[string]func() capacity.Reading {
	return map[string]func() capacity.Reading{
		"session.tool_preview_bytes": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-read byte guard; no retained buffer"}
		},
		"schedule.history_page_rows": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-console reveal batch; older returned runs remain available"}
		},
		capacity.SessionCodexPastPage: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-provider-page guard; no retained buffer"}
		},
		capacity.SecretHandoffBytes: func() capacity.Reading {
			if s.secretHandoffs == nil {
				return capacity.Reading{Known: true, Note: "no handoff service attached"}
			}
			_, largest := s.secretHandoffs.Usage()
			return capacity.Reading{Known: true, Used: largest, Note: "largest pending value; content and identifiers omitted"}
		},
		capacity.SecretHandoffPending: func() capacity.Reading {
			if s.secretHandoffs == nil {
				return capacity.Reading{Known: true, Note: "no handoff service attached"}
			}
			pending, _ := s.secretHandoffs.Usage()
			return capacity.Reading{Known: true, Used: pending}
		},
		capacity.SecretHandoffSeconds: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-value expiry; no retained wait"}
		},
		capacity.SecretHandoffDeliverySeconds: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-loopback delivery deadline; no retained wait"}
		},
		capacity.ScheduleRunTranscriptsProbed: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-lookup guard; no retained buffer"}
		},
		capacity.ScheduleRunTranscriptHead: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-file read guard; no retained buffer"}
		},
		capacity.SessionWaitingFor:     waitTargetsReading,
		capacity.SessionCloseScheduled: s.scheduledClosesReading,
		capacity.SessionCloseScheduledPerTerminal: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-terminal guard: a repeat is the same request"}
		},
		capacity.SessionCloseScheduledSeconds: func() capacity.Reading { return capacity.Reading{Known: true, Note: "per-request clock"} },
		// The two snippet rows (snippets.go): one COUNT each, over at most a
		// hundred rows.
		capacity.SnippetsTotal: func() capacity.Reading { return s.snippetsReading(false) },
		capacity.SnippetsScope: func() capacity.Reading { return s.snippetsReading(true) },
		capacity.SessionTitleRequestBytes: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-request guard; no retained buffer"}
		},
		capacity.SessionTitleCharacters: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-write guard; no retained buffer"}
		},
		capacity.SessionTitleRows: func() capacity.Reading {
			v, err := s.settingsFile().Read()
			if err != nil {
				return capacity.Unmeasured(err.Error())
			}
			return capacity.Reading{Known: true, Used: int64(len(ownSessionTitles(v, time.Now())))}
		},
		capacity.SessionTitleAge: func() capacity.Reading {
			v, err := s.settingsFile().Read()
			if err != nil {
				return capacity.Unmeasured(err.Error())
			}
			now := time.Now()
			rows := ownSessionTitles(v, now)
			reading := capacity.Reading{Known: true}
			if len(rows) > 0 {
				reading.Used = max(0, int64(now.Sub(rows[0].UpdatedAt.Time()).Seconds()))
				reading.OldestAt = rows[0].UpdatedAt.Time()
			}
			return reading
		},
		capacity.SessionExecutions: func() capacity.Reading {
			used, err := s.store.ExecutionCount(context.Background(), s.executionMachine())
			if err != nil {
				return capacity.Unmeasured(err.Error())
			}
			return capacity.Reading{Known: true, Used: used, Note: "current Cloud machine's durable terminal executions"}
		},
		capacity.SessionNoMovement: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-row record-movement threshold; no retained wait"}
		},
		capacity.SessionDirectTodos: func() capacity.Reading {
			return s.workV2CapacityReading("direct_todos")
		},
		capacity.HumanInterventionsOpen: func() capacity.Reading {
			return s.humanInterventionCapacityReading(true)
		},
		capacity.HumanInterventionsTotal: func() capacity.Reading {
			return s.humanInterventionCapacityReading(false)
		},
		capacity.HumanInterventionsRecent: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-read recent-history window"}
		},
		capacity.HumanInterventionTitle: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-write field guard"}
		},
		capacity.HumanInterventionText: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-write field guard"}
		},
		capacity.HumanInterventionDetail: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-write field guard"}
		},
		capacity.HumanInterventionDocument: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-write field guard"}
		},
		capacity.HumanInterventionDraft: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-write field guard"}
		},
		capacity.SessionDirectTodoBytes: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-write guard; no retained buffer"}
		},
		capacity.SessionTodoBatchRows: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-request guard; no retained buffer"}
		},
		capacity.ReportOpenTodoRows: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-answer window over the open to-dos; no retained buffer"}
		},
		capacity.ReportOpenTodoCharacters: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-row preview in one answer; no retained buffer"}
		},
		capacity.NamingContextBytes: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-turn naming input; built and released with the turn"}
		},
		capacity.NamingTailEntries: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-turn transcript rows read for naming; released with the turn"}
		},
	}
}
