package http

import (
	"context"
	"sync"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	"github.com/sainteye/clawdline/internal/domain/persona"
)

// squadCapacityMeasures measures every row of the register's "squad" area
// (capacity.AreaOf), one reading per row.
func (s *Server) squadCapacityMeasures() map[string]func() capacity.Reading {
	// The three squad rows are one query's three counts.
	squadUsage := sync.OnceValues(func() (store.SquadCounts, error) {
		return s.store.SquadUsage(context.Background())
	})
	workflowUsage := sync.OnceValues(func() (store.WorkflowCounts, error) {
		return s.store.WorkflowUsage(context.Background())
	})
	return map[string]func() capacity.Reading{
		capacity.WorkflowLaunches: func() capacity.Reading {
			if s.store == nil {
				return capacity.Reading{Known: true, Note: "no store attached"}
			}
			usage, err := workflowUsage()
			if err != nil {
				return capacity.Unmeasured(err.Error())
			}
			return capacity.Reading{Known: true, Used: usage.Launches, Note: "retained workflow launch identities"}
		},
		capacity.WorkflowSnapshotBytes: func() capacity.Reading {
			if s.store == nil {
				return capacity.Reading{Known: true, Note: "no store attached"}
			}
			usage, err := workflowUsage()
			if err != nil {
				return capacity.Unmeasured(err.Error())
			}
			return capacity.Reading{Known: true, Used: usage.LargestSnapshotBytes, Note: "largest immutable workflow launch snapshot"}
		},
		capacity.PersonasCatalog: func() capacity.Reading {
			return capacity.Reading{Known: true, Used: int64(len(persona.All())), Note: "compiled in; refused at load"}
		},
		capacity.PersonasTextBytes: func() capacity.Reading {
			largest := 0
			for _, p := range persona.All() {
				largest = max(largest, len(p.Text()))
			}
			return capacity.Reading{Known: true, Used: int64(largest), Note: "the largest persona's injected text"}
		},
		capacity.SquadSnapshotBytes: func() capacity.Reading {
			if s.store == nil {
				return capacity.Reading{Known: true, Note: "no store attached"}
			}
			used, err := s.store.SquadLargestSnapshotBytes(context.Background())
			if err != nil {
				return capacity.Reading{Err: err.Error()}
			}
			return capacity.Reading{Known: true, Used: used, Note: "largest immutable squad launch document"}
		},
		capacity.SquadConsoleActiveSkillBytes: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-persona Console guard; no retained measurement"}
		},
		capacity.SquadRecoveryRows: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "bounded recovery page; later pending intents continue on following pages"}
		},
		capacity.SquadEventIDBytes: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-event idempotency key; refused above the limit"}
		},
		capacity.SquadEventBodyBytes: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-request skill event JSON body"}
		},
		capacity.SquadEventPageRows: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "cursor page; additional events remain in the store"}
		},
		capacity.PersonaSuggestionContextBytes: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-turn Board role-classification input; released with the turn"}
		},
		capacity.SquadEntities: func() capacity.Reading {
			counts, err := squadUsage()
			if err != nil {
				return capacity.Unmeasured(err.Error())
			}
			return capacity.Reading{Known: true, Used: counts.Entities}
		},
		capacity.SquadSettingsRows: func() capacity.Reading {
			counts, err := squadUsage()
			if err != nil {
				return capacity.Unmeasured(err.Error())
			}
			return capacity.Reading{Known: true, Used: counts.Settings}
		},
		capacity.SquadReceipts: func() capacity.Reading {
			counts, err := squadUsage()
			if err != nil {
				return capacity.Unmeasured(err.Error())
			}
			return capacity.Reading{Known: true, Used: counts.Receipts}
		},
		capacity.SquadBodyBytes: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-body guard; no retained buffer"}
		},
		capacity.SkillSourceSummaryRunes: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-discovered-skill name and description; clipped in each list"}
		},
		capacity.SquadLaunchSkillWhenRunes: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-skill line quoted into each launch prompt; clipped at publish"}
		},
		capacity.SquadRequestBytes: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-request guard; no retained buffer"}
		},
		capacity.SquadPlaceLookup: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-read place catalog bound"}
		},
	}
}
