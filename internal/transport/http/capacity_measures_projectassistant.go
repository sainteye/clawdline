package http

import (
	"context"

	"github.com/sainteye/clawdline/internal/adapters/projectassistantacp"
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
	serverUnobserved := func() capacity.Reading {
		_, installed := usage()
		return capacity.Reading{Known: !installed, Note: "no source runtime allocation before installation; Host does not expose retained server log/caller occupancy"}
	}
	serverCount := func(processes bool) func() capacity.Reading {
		return func() capacity.Reading {
			p := &s.projectAssistants
			p.mu.RLock()
			h, failed := p.host, p.inspectionFailed
			p.mu.RUnlock()
			if h == nil {
				return capacity.Reading{Known: !failed, Note: "no source server registry before feature installation"}
			}
			if _, err := h.Service(); err != nil {
				return capacity.Reading{Known: !failed, Note: "source server schema never installed"}
			}
			u, err := h.ServerUsage(context.Background())
			if err != nil || failed {
				return capacity.Unmeasured("source server catalog or registry unavailable")
			}
			if processes {
				return capacity.Reading{Known: u.Known, Used: int64(u.Processes), Note: "owned source attempts; unknown cleanup retains occupancy and is not zero"}
			}
			return capacity.Reading{Known: true, Used: int64(u.Definitions), Note: "actual admitted source definitions; removed-ID tombstones are separate"}
		}
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
	measures := s.botRoutineHookCapacityMeasures()
	for key, measure := range map[string]func() capacity.Reading{
		"bots.server_definitions":     serverCount(false),
		"bots.server_processes":       serverCount(true),
		"bots.server_spec_bytes":      pure,
		"bots.server_log_bytes":       serverUnobserved,
		"bots.server_caller_receipts": serverUnobserved,
		"bots.server_probe_readers": func() capacity.Reading {
			return capacity.Reading{Known: true, Used: int64(projectassistantacp.SourceTCPReaders()), Note: "actual active source TCP observations; busy readers refuse without queuing another OS probe"}
		},
		"bots.archive_bytes": func() capacity.Reading {
			return capacity.Unmeasured("offline CLI input is not retained or observed by the daemon")
		},
		"bots.cloud_receipts":             s.botCloudMeasure("receipts"),
		"bots.cloud_pending":              s.botCloudMeasure("pending"),
		"bots.cloud_frames":               s.botCloudMeasure("frames"),
		"bots.cloud_endpoints":            s.botCloudMeasure("endpoints"),
		"bots.cloud_lease_seconds":        s.botCloudMeasure("lease"),
		"bots.cloud_refresh_seconds":      s.botCloudMeasure("refresh"),
		"bots.network_grants":             s.botNetworkGrantMeasure,
		"bots.assistant_frames":           s.botAssistantFramesMeasure,
		"bots.assistant_grants":           s.botAssistantRowsMeasure("project_assistant_gateway_grants"),
		"bots.assistant_receipts":         s.botAssistantRowsMeasure("project_assistant_gateway_receipts"),
		"bots.network_receipts":           s.botNetworkReceiptMeasure,
		"bots.connector_credentials":      func() capacity.Reading { return s.connectorRows("project_assistant_connector_credentials") },
		"bots.connector_credential_holds": func() capacity.Reading { return s.connectorRows("project_assistant_connector_holds") },
		"bots.connector_secret_targets": func() capacity.Reading {
			p := &s.projectAssistants
			p.mu.RLock()
			h, failed := p.host, p.inspectionFailed
			p.mu.RUnlock()
			if h == nil {
				return capacity.Reading{Known: !failed, Note: "no active connector target registry"}
			}
			n, known := h.ConnectorSecretTargetUsage()
			return capacity.Reading{Known: known, Used: int64(n), Note: "actual live expiring connector credential targets"}
		},
		capacity.BotsSkillAssetFiles:        func() capacity.Reading { return s.extensionFilesMeasure(true) },
		capacity.BotsConnectorProcesses:     func() capacity.Reading { return s.extensionFilesMeasure(false) },
		capacity.BotsManagerCardUnits:       pure,
		capacity.BotsManagerApprovalSeconds: unobserved,
		capacity.BotsManagerPending:         unobserved,
		"bots.routine_event_name_bytes":     pure,
		"bots.tower_zone_shown":             pure,
		"bots.tower_floors":                 pure,
		"bots.tower_floor_projects":         pure,
		"bots.tower_floor_project_bytes":    pure,
		"bots.routine_preview_field_bytes":  pure,
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
		"bots.background_waiting":            unobserved,
		"bots.background_threads":            unobserved,
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
		capacity.BotsGroupPreviewAvatars: func() capacity.Reading {
			return capacity.Unmeasured("each browser composes its group avatar preview; the source does not observe the rendered member count")
		},
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
		"bots.config_probes": func() capacity.Reading {
			p := &s.projectAssistants
			p.mu.RLock()
			h, failed := p.host, p.inspectionFailed
			p.mu.RUnlock()
			if failed {
				return capacity.Unmeasured("Bot Host inspection failed")
			}
			if h == nil {
				return capacity.Reading{Known: true, Note: "no Bot Host or discovery allocation"}
			}
			return capacity.Reading{Known: true, Used: int64(h.ConfigurationUsage()), Note: "currently owned disposable native configuration probes"}
		},
		"bots.login_profiles": s.loginProfileMeasure,
		"bots.login_secret_targets": func() capacity.Reading {
			p := &s.projectAssistants
			p.mu.RLock()
			h, failed := p.host, p.inspectionFailed
			p.mu.RUnlock()
			if failed {
				return capacity.Unmeasured("Bot Host inspection failed")
			}
			if h == nil {
				return capacity.Reading{Known: true, Note: "no Bot secret target owner"}
			}
			used, known := h.LoginSecretTargetUsage()
			if !known {
				return capacity.Unmeasured("Bot secret target owner cannot be inspected")
			}
			return capacity.Reading{Known: true, Used: int64(used), Note: "current unexpired Bot profile Secret Handoff targets"}
		},
		"bots.extensions":                s.memorySettingsMeasure(`SELECT coalesce(max(coalesce(json_array_length(CAST(data AS TEXT),'$.Bot.extensions.skills'),0)+coalesce(json_array_length(CAST(data AS TEXT),'$.Bot.extensions.connectors'),0)),0) FROM project_assistant_bots`, "largest persisted application extension selection per Bot"),
		"bots.config_options":            s.memorySettingsMeasure(`SELECT coalesce(max(n),0) FROM (SELECT count(*) n FROM project_assistant_bots b,json_each(CAST(b.data AS TEXT),'$.Bot.runtime_settings.options') GROUP BY b.machine,b.bot)`, "largest persisted configuration option count per Bot; discovery catalogs hold no retained allocation"),
		"bots.config_value_bytes":        s.memorySettingsMeasure(`SELECT coalesce(max(max(length(CAST(t.value AS BLOB)),length(CAST(t.key AS BLOB)))),0) FROM project_assistant_bots b,json_tree(CAST(b.data AS TEXT),'$.Bot.runtime_settings') t WHERE t.type='text'`, "largest persisted model, option key or value in UTF-8 bytes per Bot"),
		"bots.config_choices":            pure,
		"bots.memory_preview":            pure,
		capacity.BotsMemorySearchResults: pure,
		capacity.BotsSteerAnswer:         pure,
		capacity.BotsRulesFileBytes:      pure,

		// Group rooms: per-room-turn and per-prompt bounds hold no occupancy.
		// Bots created by managers is a live count that no surface reads yet.
		capacity.BotsGroupRounds:            pure,
		capacity.BotsGroupReplies:           pure,
		capacity.BotsGroupLateTurns:         pure,
		capacity.BotsGroupMemberWaitSeconds: pure,
		"bots.human_reactions":              pure,
		"bots.reaction_emoji_bytes":         pure,
		capacity.BotsGroupHistoryLines:      pure,
		capacity.BotsGroupPeerLineChars:     pure,
		capacity.BotsManagerCreated:         unobserved,
	} {
		measures[key] = measure
	}
	return measures
}
