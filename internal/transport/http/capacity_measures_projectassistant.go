package http

import (
	"context"

	"github.com/sainteye/clawdline/internal/adapters/projectassistantacp"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
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
	configCache := func(age bool) func() capacity.Reading {
		return func() capacity.Reading {
			p := &s.projectAssistants
			p.mu.RLock()
			h, failed := p.host, p.inspectionFailed
			p.mu.RUnlock()
			if failed {
				return capacity.Unmeasured("Bot Host inspection failed")
			}
			if h == nil {
				return capacity.Reading{Known: true, Note: "no Bot configuration cache before installation"}
			}
			service, err := h.Service()
			if err != nil {
				return capacity.Reading{Known: true, Note: "Bot configuration service has not been installed"}
			}
			entries, oldestSeconds := service.ConfigurationCacheUsage()
			if age {
				return capacity.Reading{Known: true, Used: oldestSeconds, Note: "age of the oldest catalog still eligible for reuse"}
			}
			return capacity.Reading{Known: true, Used: int64(entries), Note: "retained native configuration catalogs"}
		}
	}
	measures := s.botRoutineHookCapacityMeasures()
	for key, measure := range map[string]func() capacity.Reading{
		"bots.server_definitions":     serverCount(false),
		"bots.server_processes":       serverCount(true),
		"bots.server_spec_bytes":      pure,
		"bots.server_log_bytes":       serverUnobserved,
		"bots.server_caller_receipts": serverUnobserved,
		"bots.server_unconfirmed_notice_ms": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "time an individual server's authority can be unknown before a notice is due"}
		},
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
		"bots.decision_offers":              pure,
		"bots.decision_suggested_replies":   pure,
		"bots.decision_question_bytes":      pure,
		"bots.decision_reply_bytes":         pure,
		"bots.decision_request_quote_bytes": pure,
		"bots.decision_offer_seconds":       unobserved,
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
		capacity.BotsReviewQuoteRounds:      pure,
		capacity.BotsReviewQuoteBytes:       pure,
		capacity.BotsReviewRequest:          pure,
		capacity.BotsReviewReply: func() capacity.Reading {
			u := pa.CollaborationReplyUsage()
			return capacity.Reading{Known: true, Used: u.Largest, Counters: capacity.Counters{Refused: u.Truncated}, Note: "largest stored Bot reply since start; refused counts replies whose tail was cut and marked for the requester"}
		},
		capacity.BotsFallbacks: pure,
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

		capacity.BotsInputBytes: pure, capacity.BotsInputChunkBytes: pure,
		capacity.BotsRosterSubscribers: unobserved,
		capacity.BotsRosterWakeups:     unobserved,
		capacity.BotsRosterLifetime:    pure,
		capacity.BotsRosterRecheck:     pure,
		capacity.BotsDispatchedStripRows: func() capacity.Reading {
			return capacity.Unmeasured("each browser composes its dispatched-work strip; the source does not observe the rendered row count")
		},
		capacity.BotsCompletionWakeRetrySeconds: pure,
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
		capacity.BotsInlineImageBytes:      pure,
		capacity.BotsOwnedDescendants:      unobserved,
		capacity.BotsReadViewCursorBytes:   pure,
		capacity.BotsNameRunes:             pure,
		capacity.BotsGroupDescriptionRunes: pure,
		capacity.BotsInstructionBytes:      pure,
		capacity.BotsMessageBytes:          pure,
		capacity.BotsUserMessageBytes:      pure,
		capacity.BotsAutomationBacklog:     unobserved,
		capacity.BotsPageRows:              pure,
		capacity.BotsEventRows:             pure,
		"bots.chat_initial_page_rows":      pure,
		capacity.BotsActors:                count(func(u app.RuntimeUsage) int { return u.ActiveActors }, "actual Host.Usage actors; pending or unknown cleanup is not known zero"),
		capacity.BotsProcesses:             count(func(u app.RuntimeUsage) int { return u.Processes }, "actual Host.Usage process slots, including unresolved owners; not native readiness"),
		capacity.BotsRPCPending:            sessions(func(u app.SessionUsage) int { return u.PendingRPC }, "largest occupied owned MCP HTTP request pool"),
		capacity.BotsFrames:                unobserved,
		capacity.BotsProcessCensusBytes:    unobserved,
		capacity.BotsProcessCensusAttempts: unobserved,
		capacity.BotsStderr:                sessions(func(u app.SessionUsage) int { return u.StderrBytes }, "largest session-reported stderr byte count; no content is exposed"),
		capacity.BotsMCPWrite:              unobserved,
		capacity.BotsCleanup:               unobserved,
		capacity.BotsCleanupRetries:        unobserved,
		capacity.BotsCleanupRetryDelay:     unobserved,
		capacity.BotsCleanupSlowRetryDelay: unobserved,
		capacity.BotsUnscannedCensusGap:    unobserved,
		capacity.BotsCleanupSlowRetries:    unobserved,
		capacity.BotsRPCDeadline:           unobserved,
		capacity.BotsTurnControlWindow:     unobserved,
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
		"bots.config_cache_entries":   configCache(false),
		"bots.config_cache_seconds":   configCache(true),
		"bots.dispatch_rules":         s.botDispatchRulesMeasure,
		capacity.BotsWorkMessageBytes: pure,
		"bots.login_profiles":         s.loginProfileMeasure,
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
		"bots.extensions":         s.memorySettingsMeasure(`SELECT coalesce(max(coalesce(json_array_length(CAST(data AS TEXT),'$.Bot.extensions.skills'),0)+coalesce(json_array_length(CAST(data AS TEXT),'$.Bot.extensions.connectors'),0)),0) FROM project_assistant_bots`, "largest persisted application extension selection per Bot"),
		"bots.config_options":     s.memorySettingsMeasure(`SELECT coalesce(max(n),0) FROM (SELECT count(*) n FROM project_assistant_bots b,json_each(CAST(b.data AS TEXT),'$.Bot.runtime_settings.options') GROUP BY b.machine,b.bot)`, "largest persisted configuration option count per Bot; discovery catalogs hold no retained allocation"),
		"bots.config_value_bytes": s.memorySettingsMeasure(`SELECT coalesce(max(max(length(CAST(t.value AS BLOB)),length(CAST(t.key AS BLOB)))),0) FROM project_assistant_bots b,json_tree(CAST(b.data AS TEXT),'$.Bot.runtime_settings') t WHERE t.type='text'`, "largest persisted model, option key or value in UTF-8 bytes per Bot"),
		"bots.login_probe_attempts": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "attempt limit per login-profile probe, with no shared retry queue"}
		},
		"bots.login_probe_backoff_milliseconds": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "maximum wait between attempts inside a login-profile probe"}
		},
		"bots.login_status_milliseconds": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "deadline for one native sign-in status read inside a check; nothing is retained after it"}
		},
		"bots.config_choices": pure,
		"bots.memory_preview": pure,
		// Shared Bots settings are refused at write; no live surface reads them yet.
		"bots.dispatch_hint_bytes":        pure,
		"bots.dispatch_kind_bytes":        pure,
		"bots.dispatch_kind_rules":        pure,
		"bots.dispatch_model_bytes":       pure,
		"bots.dispatch_rule_agents":       pure,
		"bots.shared_memory_entries":      pure,
		"bots.shared_memory_entry_bytes":  pure,
		"bots.shared_memory_opt_outs":     pure,
		"bots.shared_memory_total_bytes":  pure,
		"bots.shared_scope_projects":      pure,
		"bots.shared_settings_body_bytes": pure,
		capacity.BotsMemorySearchResults:  pure,
		capacity.BotsSteerAnswer:          pure,
		capacity.BotsRulesFileBytes:       pure,

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

// A rule is durable and the limit is per Bot. Read through the query-only pool
// so periodic diagnostics do not wait for the Bot writer.
func (s *Server) botDispatchRulesMeasure() capacity.Reading {
	if s.store == nil {
		return capacity.Unmeasured("Bot dispatch rule store unavailable")
	}
	db := s.store.ProjectAssistantReader()
	var exists, used int64
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE type='table' AND name='project_assistant_dispatch_rules'`).Scan(&exists); err != nil {
		return capacity.Unmeasured("Bot dispatch rule table cannot be inspected")
	}
	if exists == 0 {
		return capacity.Reading{Known: true, Note: "optional Bot dispatch rule table is absent"}
	}
	if err := db.QueryRow(`SELECT coalesce(max(n),0) FROM (SELECT count(*) n FROM project_assistant_dispatch_rules GROUP BY machine,bot)`).Scan(&used); err != nil {
		return capacity.Unmeasured("Bot dispatch rules cannot be read")
	}
	return capacity.Reading{Known: true, Used: used, Note: "largest saved dispatch permission rule count per Bot"}
}
