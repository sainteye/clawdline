package http

import (
	"context"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	"time"
)

func (s *Server) projectAssistantCapacityMeasures() map[string]func() capacity.Reading {
	usage := func() (app.RuntimeUsage, bool) {
		p := &s.projectAssistants
		p.mu.RLock()
		h, failed := p.host, p.inspectionFailed
		p.mu.RUnlock()
		if h == nil {
			return app.RuntimeUsage{Known: !failed}, failed
		}
		_, err := h.Service()
		u := h.Usage()
		u.Known = u.Known && !failed
		return u, err == nil
	}
	count := func(selectCount func(app.RuntimeUsage) int, note string) func() capacity.Reading {
		return func() capacity.Reading {
			u, installed := usage()
			if !installed {
				return capacity.Reading{Known: true, Note: "source unconfigured or Bot schema never installed; no runtime allocation"}
			}
			return capacity.Reading{Known: u.Known, Used: int64(selectCount(u)), Note: note}
		}
	}
	sessions := func(selectCount func(app.SessionUsage) int, note string) func() capacity.Reading {
		return count(func(u app.RuntimeUsage) int {
			// Each register bound applies to one native session. Expose its
			// largest observed occupancy, not an invented aggregate RPC count.
			n := 0
			for _, session := range u.Sessions {
				n = max(n, selectCount(session))
			}
			return n
		}, note)
	}
	pure := func() capacity.Reading {
		return capacity.Reading{Known: true, Note: "effect-free input validator or request-owned bounded page; no retained allocation"}
	}
	unobserved := func() capacity.Reading {
		_, installed := usage()
		return capacity.Reading{Known: !installed, Note: "no allocation before installation; installed durable queues, native descendants and IO duration are not exposed by Host.Usage"}
	}
	reviews := func(sender bool) func() capacity.Reading {
		return func() capacity.Reading {
			p := &s.projectAssistants
			p.mu.RLock()
			h, failed := p.host, p.inspectionFailed
			p.mu.RUnlock()
			if h == nil {
				return capacity.Reading{Known: !failed, Note: "no installed Bot collaboration store"}
			}
			total, largest, err := h.CollaborationUsage(context.Background())
			if err != nil {
				return capacity.Unmeasured("durable collaboration count unavailable")
			}
			if sender {
				total = largest
			}
			return capacity.Reading{Known: true, Used: total, Note: "durable outstanding exchanges; historical facts and owned process cleanup are counted separately"}
		}
	}
	return map[string]func() capacity.Reading{
		"bots.routine_event_name_bytes":     pure,
		"bots.routine_event_filters":        pure,
		"bots.routine_filter_key_bytes":     pure,
		"bots.routine_filter_value_bytes":   pure,
		"bots.routine_webhook_body_bytes":   pure,
		"bots.routine_delivery_id_bytes":    pure,
		"bots.routine_github_event_bytes":   pure,
		"bots.routine_hook_secret_bytes":    unobserved,
		"bots.follow_up_pending":            unobserved,
		"bots.follow_up_seconds":            pure,
		"bots.follow_up_chain":              pure,
		"bots.follow_up_tokens":             pure,
		"bots.routine_retry_attempts":       unobserved,
		"bots.routine_retry_delay_seconds":  pure,
		capacity.BotsContinuationMessages:   pure,
		capacity.BotsContinuationQuoteBytes: pure,
		capacity.BotsReviewPending:          reviews(false),
		capacity.BotsReviewPerSender:        reviews(true),
		capacity.BotsReviewHops:             pure,
		capacity.BotsReviewRequest:          pure,
		capacity.BotsReviewReply:            pure,
		capacity.BotsFallbacks:              pure,
		capacity.BotsAgentRestSeconds: func() capacity.Reading {
			p := &s.projectAssistants
			p.mu.RLock()
			h, failed, clock := p.host, p.inspectionFailed, p.clock
			p.mu.RUnlock()
			if h == nil {
				return capacity.Reading{Known: !failed, Note: "no installed Bot store; no machine rest allocated"}
			}
			rests, err := h.AgentRests(context.Background())
			if err != nil {
				return capacity.Unmeasured("machine rest read failed")
			}
			now := time.Now()
			if clock != nil {
				now = clock()
			}
			var seconds int64
			for _, rest := range rests {
				if left := int64(rest.Until.Sub(now).Seconds()); left > seconds {
					seconds = left
				}
			}
			return capacity.Reading{Known: true, Used: seconds, Note: "largest remaining durable default-login rest on this machine, read without provider IO"}
		},
		capacity.BotsErrorKindBytes:          pure,
		capacity.BotsRoutineDefinitions:      unobserved,
		capacity.BotsRoutineProgressTokens:   pure,
		capacity.BotsRoutineTriggers:         pure,
		capacity.BotsRoutineTickMilliseconds: pure,
		capacity.BotsRoutinePending:          unobserved,
		capacity.BotsRoutineNameBytes:        pure,
		capacity.BotsRoutineInstructionBytes: pure,
		capacity.BotsRoutineIntervalSeconds:  pure,
		capacity.BotsRoutineTimeoutSeconds:   unobserved,
		capacity.BotsRoutineFinishedRuns:     unobserved,

		capacity.BotsInputBytes: pure, capacity.BotsInputChunkBytes: pure, capacity.BotsInputsPerMessage: pure, capacity.BotsInputsPerBot: unobserved,
		capacity.BotsRosterSubscribers: unobserved,
		capacity.BotsRosterWakeups:     unobserved,
		capacity.BotsRosterLifetime:    pure,
		capacity.BotsRosterRecheck:     pure,
		capacity.BotsRosterClientRows: func() capacity.Reading {
			return capacity.Unmeasured("each browser owns its retained Bot rows and removal revisions; the source does not observe client occupancy")
		},
		capacity.BotsArtifactTitleBytes:    pure,
		capacity.BotsOutputBytes:           pure,
		capacity.BotsArtifactBytes:         pure,
		capacity.BotsOutputChunkBytes:      pure,
		capacity.BotsOwnedDescendants:      unobserved,
		capacity.BotsReadViewCursorBytes:   pure,
		capacity.BotsNameRunes:             pure,
		capacity.BotsInstructionBytes:      pure,
		capacity.BotsMessageBytes:          pure,
		capacity.BotsPendingTurns:          unobserved,
		capacity.BotsPageRows:              pure,
		capacity.BotsEventRows:             pure,
		capacity.BotsActors:                count(func(u app.RuntimeUsage) int { return u.ActiveActors }, "actual Host.Usage actors; pending or unknown cleanup is not known zero"),
		capacity.BotsProcesses:             count(func(u app.RuntimeUsage) int { return u.Processes }, "actual Host.Usage process slots, including unresolved owners; not native readiness"),
		capacity.BotsRPCPending:            sessions(func(u app.SessionUsage) int { return u.PendingRPC }, "largest shared ACP request/owned MCP connection occupancy; no provider is configured in production"),
		capacity.BotsPermissions:           sessions(func(u app.SessionUsage) int { return u.PendingPermissions }, "largest session-reported pending permission occupancy"),
		capacity.BotsFrames:                unobserved,
		capacity.BotsProcessCensusBytes:    unobserved,
		capacity.BotsStderr:                sessions(func(u app.SessionUsage) int { return u.StderrBytes }, "largest session-reported stderr byte count; no content is exposed"),
		capacity.BotsWrite:                 unobserved,
		capacity.BotsCleanup:               unobserved,
		capacity.BotsRPCDeadline:           unobserved,
		capacity.BotsPrompt:                unobserved,
		capacity.BotsSurvey:                pure,
		capacity.BotsChatGap:               pure,
		capacity.BotsEventRefresh:          pure,
		capacity.BotsDispatchSeconds:       pure,
		capacity.BotsAdapterInstallSeconds: pure,
		capacity.BotsToolProbe:             pure,
		capacity.BotsToolProbeBytes:        pure,
		capacity.BotsFeatureProbeBytes:     pure,
		capacity.BotsPermissionActionBytes: pure,
		capacity.BotsSendBodyBytes:         pure,
		capacity.BotsAttachmentsPerMessage: pure,
		capacity.BotsAttachmentBytes:       pure,
		capacity.BotsMemoryEntries:         pure,
		capacity.BotsMemorySearchResults:   pure,
		capacity.BotsSteerAnswer:           pure,
		capacity.BotsRulesFileBytes:        pure,

		// Group rooms: per-room-turn and per-prompt bounds hold no occupancy.
		// Bots created by managers is a live count that no surface reads yet.
		capacity.BotsGroupRounds:            pure,
		capacity.BotsGroupReplies:           pure,
		capacity.BotsGroupLateTurns:         pure,
		capacity.BotsGroupMemberWaitSeconds: pure,
		capacity.BotsGroupHistoryLines:      pure,
		capacity.BotsGroupPeerLineChars:     pure,
		capacity.BotsManagerCreated:         unobserved,
	}
}
