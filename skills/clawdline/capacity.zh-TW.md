# 此 build 的容量契約

此表直接由 `internal/domain/capacity.Register()` 產生，列的是預設上限。執行時覆寫只能降低上限；操作前請讀目標機器的即時容量狀態。缺少讀數或容量已滿時，依目前指南與具型別拒絕停止或處理，勿把預設值當成可用餘額。`GET /v1/capacity` 需要已配對裝置；`GET /v1/diagnostics` 需要本機金鑰。

| 名稱 | 預設上限 | 單位 | 達上限時 | 告知管道 | 偏離既定規則 |
| --- | ---: | --- | --- | --- | --- |
| `artifacts.drops` | 268435456 | `bytes` | `evict_oldest` | `diagnostics, log` | — |
| `artifacts.drops_young` | 1 | `rows` | `evict_oldest` | `diagnostics, notice, log` | — |
| `artifacts.image_bytes` | 67108864 | `bytes` | `evict_oldest` | `diagnostics, notice, log` | — |
| `artifacts.images` | 64 | `rows` | `evict_oldest` | `diagnostics, notice, log` | — |
| `audit.security` | 8388608 | `bytes` | `rotate` | `diagnostics, notice, health` | — |
| `board.receipts` | 4096 | `rows` | `refuse` | `diagnostics, notice` | — |
| `cache.assistant_quota` | 32 | `rows` | `evict_oldest` | `diagnostics, notice` | — |
| `cache.background_agents` | 256 | `rows` | `evict_oldest` | `diagnostics, notice` | — |
| `cache.git_not_repo` | 60 | `seconds` | `expire` | `diagnostics` | — |
| `cache.git_not_repo_rows` | 256 | `rows` | `evict_oldest` | `diagnostics` | — |
| `cache.image_thumbs` | 8388608 | `bytes` | `evict_oldest` | `diagnostics` | — |
| `cache.pinned_read` | 3 | `seconds` | `expire` | `diagnostics` | — |
| `cache.reclaim_summary` | 30 | `seconds` | `expire` | `diagnostics` | — |
| `cache.session_activity` | 64 | `rows` | `evict_oldest` | `diagnostics, notice` | — |
| `cache.session_execution_memo` | 4096 | `rows` | `evict_oldest` | `diagnostics` | — |
| `cache.session_inventory` | 120 | `seconds` | `expire` | `diagnostics` | — |
| `cache.session_links` | 64 | `rows` | `evict_oldest` | `diagnostics, notice` | — |
| `cache.session_skills` | 64 | `rows` | `evict_oldest` | `diagnostics, notice` | — |
| `cache.source_answer` | 30 | `seconds` | `expire` | `diagnostics` | — |
| `cache.terminal_screens` | 64 | `rows` | `evict_oldest` | `diagnostics, notice` | — |
| `cache.transcript_titles` | 256 | `rows` | `evict_oldest` | `diagnostics, notice` | — |
| `callback.args` | 64 | `rows` | `refuse` | `diagnostics, sender` | — |
| `callback.command_bytes` | 16384 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `callback.output_bytes` | 8388608 | `bytes` | `evict_oldest` | `diagnostics` | — |
| `callback.per_machine` | 16 | `rows` | `refuse` | `diagnostics, sender` | — |
| `callback.per_root` | 8 | `rows` | `refuse` | `diagnostics, sender` | — |
| `callback.tail_bytes` | 8192 | `bytes` | `evict_oldest` | `diagnostics` | — |
| `cloud.header_read_diagnostic_rows` | 24 | `rows` | `evict_oldest` | `diagnostics` | — |
| `cloud.header_read_diagnostic_seconds` | 3600 | `seconds` | `expire` | `diagnostics` | — |
| `cloud.peer_body_bytes` | 524288 | `bytes` | `refuse` | `diagnostics, notice, sender` | — |
| `cloud.peer_frame_bytes` | 1048576 | `bytes` | `refuse` | `diagnostics, log, sender` | — |
| `cloud.peer_grants` | 512 | `rows` | `refuse` | `diagnostics, notice, health` | — |
| `cloud.peer_inbox` | 512 | `rows` | `refuse` | `diagnostics, notice, health` | — |
| `cloud.peer_inbox_page` | 1 | `rows` | `refuse` | `diagnostics, log` | — |
| `cloud.peer_ingress` | 16 | `rows` | `refuse` | `diagnostics, log, sender` | — |
| `cloud.peer_outbox` | 512 | `rows` | `refuse` | `diagnostics, notice, health` | — |
| `cloud.peer_pairs` | 128 | `rows` | `refuse` | `diagnostics, notice, health` | — |
| `cloud.relay_queue` | 64 | `rows` | `refuse` | `diagnostics, notice, cloud_status, log, sender` | — |
| `cloud.spool` | 2000 | `rows` | `refuse` | `diagnostics, notice, log` | — |
| `cloud.spool_bytes` | 16777216 | `bytes` | `refuse` | `diagnostics, notice, log` | — |
| `cloud.spool_channel_bytes` | 4194304 | `bytes` | `refuse` | `diagnostics, notice, log` | — |
| `cloud.spool_receipts` | 4096 | `rows` | `expire` | `diagnostics, log` | — |
| `cloud.spool_refusals` | 64 | `rows` | `refuse` | `diagnostics, log` | — |
| `cloud.terminal_connections` | 16 | `rows` | `refuse` | `diagnostics, sender` | — |
| `cloud.terminal_direct_ack_seconds` | 3 | `seconds` | `expire` | `diagnostics, sender` | — |
| `cloud.terminal_direct_candidates` | 32 | `rows` | `refuse` | `diagnostics, sender` | — |
| `cloud.terminal_direct_chunk_seconds` | 2 | `seconds` | `disconnect` | `diagnostics, sender` | — |
| `cloud.terminal_direct_gather_seconds` | 3 | `seconds` | `expire` | `diagnostics, sender` | — |
| `cloud.terminal_direct_message_bytes` | 9437184 | `bytes` | `disconnect` | `diagnostics, sender` | — |
| `cloud.terminal_direct_negotiate_seconds` | 5 | `seconds` | `expire` | `diagnostics, sender` | — |
| `cloud.terminal_direct_offers_per_minute` | 6 | `rows` | `refuse` | `diagnostics, sender` | — |
| `cloud.terminal_direct_peers` | 8 | `rows` | `refuse` | `diagnostics, sender` | — |
| `cloud.terminal_direct_probe_seconds` | 1 | `seconds` | `expire` | `diagnostics, sender` | — |
| `cloud.terminal_direct_probe_unsettled_seconds` | 2 | `seconds` | `expire` | `diagnostics, sender` | — |
| `cloud.terminal_direct_sdp_bytes` | 16384 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `cloud.terminal_early_frames` | 1 | `rows` | `coalesce` | `diagnostics` | — |
| `cloud.terminal_frame_heartbeat_seconds` | 3 | `seconds` | `expire` | `diagnostics, sender` | — |
| `cloud.terminal_history_capture_bytes` | 4194304 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `cloud.terminal_history_line_bytes` | 4096 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `cloud.terminal_history_receipt_bytes` | 8192 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `cloud.terminal_ingress` | 16 | `rows` | `refuse` | `diagnostics, sender` | — |
| `cloud.terminal_key_seconds` | 600 | `seconds` | `expire` | `diagnostics, sender` | — |
| `cloud.terminal_list_ingress` | 16 | `rows` | `refuse` | `diagnostics, sender` | — |
| `cloud.terminal_list_retries` | 1 | `rows` | `refuse` | `diagnostics` | — |
| `cloud.terminal_observation_rows` | 128 | `rows` | `evict_oldest` | `diagnostics` | — |
| `cloud.terminal_receipt_busy_retries` | 3 | `rows` | `disconnect` | `diagnostics, sender` | — |
| `cloud.terminal_receipt_busy_retry_seconds` | 2 | `seconds` | `expire` | `diagnostics, sender` | — |
| `cloud.terminal_receipt_seconds` | 15 | `seconds` | `expire` | `diagnostics` | — |
| `cloud.terminal_receipts` | 512 | `rows` | `evict_oldest` | `diagnostics, log` | limits §4.2 has an idempotency row refuse new requests rather than evict inside its window. Decided 2026-10-07: refusing here refused every key of a key held down past 12.8 s (40 a second against 512 in 15 s), while the bound exists only for memory and the viewer never resends a request id itself (cloud-terminal-wire.md); the oldest receipt, the least likely to be asked for again, goes instead, counted as Evicted and logged as stage receipt_evicted. |
| `cloud.terminal_refusals` | 16 | `rows` | `refuse` | `diagnostics, log` | — |
| `cloud.terminal_request_bytes` | 6295552 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `cloud.terminal_revocation_retire_seconds` | 3 | `seconds` | `expire` | `diagnostics, sender` | — |
| `cloud.terminal_roster_deadline_seconds` | 2 | `seconds` | `expire` | `diagnostics, sender` | — |
| `cloud.terminal_roster_refresh_seconds` | 2 | `seconds` | `expire` | `diagnostics, sender` | — |
| `cloud.terminal_roster_retry_seconds` | 1 | `seconds` | `expire` | `diagnostics, sender` | — |
| `cloud.terminal_sweep_seconds` | 1 | `seconds` | `expire` | `diagnostics, sender` | — |
| `cloud.terminal_unconfirmed_seconds` | 15 | `seconds` | `expire` | `diagnostics, sender` | — |
| `cloud.terminal_unverified_retire_seconds` | 10 | `seconds` | `expire` | `diagnostics, sender` | — |
| `cloud.terminal_viewer_connections` | 2 | `rows` | `refuse` | `diagnostics, sender` | — |
| `console.relay_transcript_expect_reask_seconds` | 4 | `seconds` | `expire` | `diagnostics` | — |
| `console.transcript_backoff_seconds` | 30 | `seconds` | `expire` | `diagnostics` | — |
| `console.transcript_follow_seconds` | 90 | `seconds` | `expire` | `diagnostics` | — |
| `console.transcript_merged_rows` | 1000 | `rows` | `expire` | `diagnostics` | — |
| `console.transcript_safety_seconds` | 30 | `seconds` | `expire` | `diagnostics` | — |
| `coordinator.aliases` | 32 | `rows` | `evict_oldest` | `diagnostics` | — |
| `coordinator.bind_attempts` | 6 | `rows` | `refuse` | `diagnostics, sender` | — |
| `decisions.open` | 256 | `rows` | `refuse` | `diagnostics, sender` | — |
| `deploy.health_seconds` | 30 | `seconds` | `refuse` | `diagnostics` | — |
| `devices.list` | 512 | `rows` | `refuse` | `diagnostics, notice, health` | — |
| `dispatch.also_work_ids` | 7 | `rows` | `refuse` | `diagnostics, sender` | — |
| `epic.child_items` | 64 | `rows` | `refuse` | `diagnostics, sender` | — |
| `handoff.pack_bytes` | 33554432 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `handoff.patch_bytes` | 8388608 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `handoff.worktrees` | 16 | `rows` | `refuse` | `diagnostics, sender` | — |
| `icons.request_bytes` | 98304 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `icons.saved` | 512 | `rows` | `refuse` | `diagnostics, sender, health` | — |
| `icons.side` | 64 | `rows` | `refuse` | `diagnostics, sender` | — |
| `intent.cloud_wait_seconds` | 130 | `seconds` | `refuse` | `diagnostics, sender` | — |
| `intent.planner_queue` | 2 | `rows` | `refuse` | `diagnostics, sender` | — |
| `intent.planner_seconds` | 30 | `seconds` | `refuse` | `diagnostics, sender` | — |
| `intent.request_bytes` | 4096 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `intent.stderr_bytes` | 4096 | `bytes` | `evict_oldest` | `diagnostics` | — |
| `iterm.osascript_kinds` | 32 | `rows` | `coalesce` | `diagnostics, log` | — |
| `iterm.stall_cooldown_seconds` | 1800 | `seconds` | `refuse` | `diagnostics, log` | — |
| `iterm.stall_diagnoses` | 20 | `rows` | `rotate` | `diagnostics, log` | — |
| `iterm.stall_failures` | 32 | `rows` | `evict_oldest` | `diagnostics, log` | — |
| `iterm.stall_said_bytes` | 512 | `bytes` | `evict_oldest` | `diagnostics` | — |
| `iterm.stall_section_bytes` | 524288 | `bytes` | `refuse` | `diagnostics` | — |
| `iterm.stall_step_seconds` | 30 | `seconds` | `refuse` | `diagnostics` | — |
| `leases.queue` | 64 | `rows` | `refuse` | `diagnostics, sender` | — |
| `log.daemon` | 10485760 | `bytes` | `rotate` | `diagnostics, notice` | — |
| `memory.description_bytes` | 512 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `memory.entries` | 512 | `rows` | `refuse` | `diagnostics, sender, health` | — |
| `memory.entry_bytes` | 65536 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `memory.group_description_bytes` | 256 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `memory.groups` | 16 | `rows` | `refuse` | `diagnostics, sender, health` | — |
| `memory.name_bytes` | 64 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `naming.context_bytes` | 12288 | `bytes` | `evict_oldest` | `diagnostics` | — |
| `naming.tail_entries` | 400 | `rows` | `evict_oldest` | `diagnostics` | — |
| `orchestrator.opening_stuck_seconds` | 1800 | `seconds` | `evict_oldest` | `diagnostics` | — |
| `personas.catalog` | 64 | `rows` | `refuse` | `diagnostics` | — |
| `personas.suggestion_context_bytes` | 16384 | `bytes` | `evict_oldest` | `diagnostics` | — |
| `personas.text_bytes` | 8192 | `bytes` | `refuse` | `diagnostics` | — |
| `places.git_config_bytes` | 65536 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `places.registered` | 512 | `rows` | `refuse` | `diagnostics, notice, health` | — |
| `planreview.blocking_listed` | 8 | `rows` | `coalesce` | `diagnostics, sender` | — |
| `projectfiles.file_bytes` | 131072 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `projectfiles.list` | 128 | `rows` | `refuse` | `diagnostics, sender` | — |
| `projectfiles.scan_entries` | 1024 | `rows` | `refuse` | `diagnostics, sender` | — |
| `projectfiles.tree_depth` | 64 | `rows` | `refuse` | `diagnostics, sender` | — |
| `projectfiles.tree_path_bytes` | 4096 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `projectfiles.write_bytes` | 262144 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `projectsync.clones` | 2 | `rows` | `refuse` | `diagnostics, sender` | — |
| `projectsync.entry_bytes` | 4194304 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `projectsync.file_bytes` | 262144 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `projectsync.manifest_projects` | 256 | `rows` | `refuse` | `diagnostics, sender` | — |
| `projectsync.mirrored` | 512 | `rows` | `refuse` | `diagnostics, sender, health` | — |
| `projectsync.path_bytes` | 512 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `projectsync.project_files` | 64 | `rows` | `refuse` | `diagnostics, sender` | — |
| `proposals.open` | 500 | `rows` | `refuse` | `diagnostics, sender` | — |
| `push.subscriptions` | 128 | `rows` | `refuse` | `diagnostics, notice, health` | — |
| `release.app_swap_poll_seconds` | 60 | `seconds` | `evict_oldest` | `diagnostics` | — |
| `release.apply_follow_seconds` | 1500 | `seconds` | `refuse` | `diagnostics` | — |
| `release.archive_entries` | 20000 | `rows` | `refuse` | `diagnostics` | — |
| `release.artifact_bytes` | 536870912 | `bytes` | `refuse` | `diagnostics` | — |
| `release.auto_apply_retry_seconds` | 300 | `seconds` | `evict_oldest` | `diagnostics` | — |
| `release.backups_kept` | 2 | `rows` | `evict_oldest` | `diagnostics` | — |
| `release.boot_attempts` | 3 | `rows` | `refuse` | `diagnostics` | — |
| `release.check_interval_seconds` | 21600 | `seconds` | `evict_oldest` | `diagnostics` | — |
| `release.check_jitter_seconds` | 1800 | `seconds` | `evict_oldest` | `diagnostics` | — |
| `release.download_timeout_seconds` | 900 | `seconds` | `refuse` | `diagnostics` | — |
| `release.failed_versions` | 16 | `rows` | `evict_oldest` | `diagnostics` | — |
| `release.fetch_timeout_seconds` | 30 | `seconds` | `evict_oldest` | `diagnostics` | — |
| `release.health_wait_seconds` | 60 | `seconds` | `refuse` | `diagnostics` | — |
| `release.list_bytes` | 1048576 | `bytes` | `refuse` | `diagnostics` | — |
| `release.lock_stale_seconds` | 1800 | `seconds` | `expire` | `diagnostics` | — |
| `release.manifest_bytes` | 65536 | `bytes` | `refuse` | `diagnostics` | — |
| `release.pending_deadline_seconds` | 600 | `seconds` | `refuse` | `diagnostics` | — |
| `release.previous_releases_kept` | 2 | `rows` | `evict_oldest` | `diagnostics` | — |
| `release.signature_bytes` | 16384 | `bytes` | `refuse` | `diagnostics` | — |
| `release.smoke_timeout_seconds` | 15 | `seconds` | `refuse` | `diagnostics` | — |
| `release.state_file_bytes` | 65536 | `bytes` | `refuse` | `diagnostics` | — |
| `release.supervisor_runs` | 5 | `rows` | `refuse` | `diagnostics` | — |
| `release.unpacked_bytes` | 2147483648 | `bytes` | `refuse` | `diagnostics` | — |
| `run.claimed_items` | 5 | `rows` | `refuse` | `diagnostics, sender` | — |
| `run.created_items` | 5 | `rows` | `refuse` | `diagnostics, sender` | — |
| `screens.capture_slots` | 2 | `rows` | `refuse` | `diagnostics` | — |
| `session.close_scheduled` | 16 | `rows` | `refuse` | `diagnostics, sender` | — |
| `session.close_scheduled_per_terminal` | 1 | `rows` | `coalesce` | `diagnostics, sender` | — |
| `session.close_scheduled_seconds` | 900 | `seconds` | `expire` | `diagnostics, log` | — |
| `session.direct_todo_bytes` | 8192 | `bytes` | `refuse` | `diagnostics, sender, health` | — |
| `session.direct_todos` | 500 | `rows` | `refuse` | `diagnostics, sender, health` | — |
| `session.executions` | 4096 | `rows` | `refuse` | `diagnostics, health, sender, log` | — |
| `session.human_intervention_detail_bytes` | 16384 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `session.human_intervention_document_bytes` | 2048 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `session.human_intervention_draft_bytes` | 2048 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `session.human_intervention_text_bytes` | 500 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `session.human_intervention_title_bytes` | 120 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `session.human_interventions_open` | 8 | `rows` | `refuse` | `diagnostics, sender, health` | — |
| `session.human_interventions_recent` | 5 | `rows` | `evict_oldest` | `diagnostics` | — |
| `session.human_interventions_total` | 2000 | `rows` | `evict_oldest` | `diagnostics, sender` | — |
| `session.no_movement_seconds` | 1800 | `seconds` | `expire` | `diagnostics, sender` | — |
| `session.report_open_todo_characters` | 120 | `characters` | `refuse` | `diagnostics, sender` | — |
| `session.report_open_todo_rows` | 20 | `rows` | `refuse` | `diagnostics, sender` | — |
| `session.title_age` | 7776000 | `seconds` | `evict_oldest` | `diagnostics, notice` | — |
| `session.title_characters` | 200 | `characters` | `refuse` | `diagnostics, sender` | — |
| `session.title_request_bytes` | 16384 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `session.title_rows` | 200 | `rows` | `evict_oldest` | `diagnostics, notice` | — |
| `session.todo_batch_rows` | 20 | `rows` | `refuse` | `diagnostics, sender` | — |
| `sessions.activity_reads` | 64 | `rows` | `refuse` | `diagnostics` | — |
| `sessions.agent_rows` | 6 | `rows` | `evict_oldest` | `diagnostics` | — |
| `sessions.archive_restore_batch` | 20 | `rows` | `refuse` | `diagnostics, sender` | — |
| `sessions.archive_rows` | 500 | `rows` | `evict_oldest` | `diagnostics` | — |
| `sessions.restore_batch` | 20 | `rows` | `refuse` | `diagnostics, sender` | — |
| `sessions.restore_boots` | 2 | `rows` | `evict_oldest` | `diagnostics` | — |
| `sessions.restore_grace` | 180 | `seconds` | `expire` | `diagnostics` | — |
| `sessions.restore_heartbeat` | 60 | `seconds` | `expire` | `diagnostics` | — |
| `sessions.restore_rows` | 200 | `rows` | `evict_oldest` | `diagnostics` | — |
| `sessions.restore_seen_age` | 300 | `seconds` | `expire` | `diagnostics` | — |
| `sessions.shell_output_bytes` | 1048576 | `bytes` | `evict_oldest` | `diagnostics` | — |
| `settings.request_body_bytes` | 65536 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `snippets.scope` | 50 | `rows` | `refuse` | `diagnostics, notice, health` | — |
| `snippets.total` | 100 | `rows` | `refuse` | `diagnostics, notice, health` | — |
| `squad.body_bytes` | 65536 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `squad.console_active_skill_bytes` | 524288 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `squad.entities` | 1024 | `rows` | `refuse` | `diagnostics, health, sender` | — |
| `squad.event_body_bytes` | 4096 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `squad.event_id_bytes` | 128 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `squad.event_page_rows` | 100 | `rows` | `refuse` | `diagnostics, sender` | — |
| `squad.launch_skill_when_runes` | 240 | `characters` | `evict_oldest` | `diagnostics, sender` | — |
| `squad.place_lookup` | 1024 | `rows` | `evict_oldest` | `diagnostics, sender` | — |
| `squad.receipts` | 10000 | `rows` | `refuse` | `diagnostics, sender` | — |
| `squad.recovery_page_rows` | 256 | `rows` | `refuse` | `diagnostics` | — |
| `squad.request_bytes` | 262144 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `squad.settings_rows` | 10000 | `rows` | `refuse` | `diagnostics, health, sender` | — |
| `squad.skill_source_summary_runes` | 240 | `characters` | `evict_oldest` | `diagnostics, sender` | — |
| `squad.snapshot_bytes` | 1048576 | `bytes` | `refuse` | `diagnostics, sender, health` | — |
| `squadpackage.archive_bytes` | 524288 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `squadpackage.entries` | 128 | `rows` | `refuse` | `diagnostics, sender` | — |
| `squadpackage.expanded_bytes` | 4194304 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `squadpackage.expansion_ratio` | 64 | `multipliers` | `refuse` | `diagnostics, sender` | — |
| `squadpackage.file_bytes` | 65536 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `squadpackage.manifest_bytes` | 131072 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `squadpackage.preview_age` | 900 | `seconds` | `expire` | `diagnostics, sender` | — |
| `squadpackage.preview_rows` | 1024 | `rows` | `refuse` | `diagnostics, sender` | — |
| `squadpackage.request_bytes` | 1048576 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `sse.screen_pending` | 64 | `rows` | `disconnect` | `diagnostics, notice` | — |
| `store.db` | 1073741824 | `bytes` | `none` | `diagnostics, notice, health` | limits §7.1: C1 only measures this row. Its class refuses new writes at the limit (limits §4.4 step 5, 507 storage_exhausted); nothing refuses yet, and ok:false on /v1/health is the only effect of full. |
| `store.read_connections` | 4 | `rows` | `refuse` | `diagnostics` | — |
| `store.receipts` | 4096 | `rows` | `refuse` | `diagnostics, notice` | — |
| `terminal.body_bytes` | 6295552 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `terminal.count` | 8 | `rows` | `refuse` | `diagnostics, sender` | — |
| `terminal.grants_bytes` | 262144 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `terminal.history_lines` | 2000 | `rows` | `evict_oldest` | `diagnostics` | — |
| `terminal.input_bytes` | 4096 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `terminal.lane` | 16 | `rows` | `refuse` | `diagnostics, sender` | — |
| `terminal.launch_line_bytes` | 512 | `bytes` | `refuse` | `diagnostics` | — |
| `terminal.launch_scripts` | 64 | `rows` | `evict_oldest` | `diagnostics` | — |
| `terminal.lease_seconds` | 30 | `seconds` | `expire` | `diagnostics, sender` | — |
| `terminal.paste_bytes` | 1048576 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `terminal.streams` | 16 | `rows` | `refuse` | `diagnostics, sender` | — |
| `terminal.viewers` | 8 | `rows` | `refuse` | `diagnostics, sender` | — |
| `timeline.entries` | 500 | `rows` | `evict_oldest` | `diagnostics` | — |
| `update.apply_body_bytes` | 4096 | `bytes` | `refuse` | `diagnostics` | — |
| `update.build_body_bytes` | 4096 | `bytes` | `refuse` | `diagnostics` | — |
| `update.fetch_timeout_seconds` | 10 | `seconds` | `evict_oldest` | `diagnostics` | — |
| `update.refresh_seconds` | 1800 | `seconds` | `evict_oldest` | `diagnostics` | — |
| `usage.work_cursor_queue` | 1024 | `rows` | `refuse` | `diagnostics, log` | — |
| `usage.work_cursor_rows` | 50000 | `rows` | `evict_oldest` | `diagnostics` | — |
| `usage.work_units_per_answer` | 500 | `rows` | `evict_oldest` | `diagnostics, sender` | — |
| `waits.open` | 256 | `rows` | `refuse` | `diagnostics, sender` | — |
| `work.assignments` | 4000 | `rows` | `refuse` | `diagnostics, sender, health` | — |
| `work.completion_reason_bytes` | 8192 | `bytes` | `refuse` | `diagnostics, sender, health` | — |
| `work.digests` | 800 | `rows` | `evict_oldest` | `diagnostics` | — |
| `work.documents_per_item` | 32 | `rows` | `refuse` | `diagnostics, sender, health` | — |
| `work.image_bytes` | 5242880 | `bytes` | `refuse` | `diagnostics, sender, health` | — |
| `work.image_bytes_per_item` | 15728640 | `bytes` | `refuse` | `diagnostics, sender, health` | — |
| `work.image_bytes_total` | 536870912 | `bytes` | `refuse` | `diagnostics, sender, health` | — |
| `work.image_request_body_bytes` | 18874368 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `work.images_per_item` | 6 | `rows` | `refuse` | `diagnostics, sender, health` | — |
| `work.item_description_bytes` | 65536 | `bytes` | `refuse` | `diagnostics, sender, health` | — |
| `work.item_title_bytes` | 240 | `bytes` | `refuse` | `diagnostics, sender, health` | — |
| `work.item_user_action_bytes` | 8192 | `bytes` | `refuse` | `diagnostics, sender, health` | — |
| `work.list_page_rows` | 24 | `rows` | `evict_oldest` | `diagnostics` | — |
| `work.open` | 2000 | `rows` | `refuse` | `diagnostics, notice, health` | — |
| `work.planning` | 1000 | `rows` | `refuse` | `diagnostics, notice, health` | — |
| `work.request_body_bytes` | 98304 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `work.root_landings_per_item` | 64 | `rows` | `refuse` | `diagnostics, sender, health` | — |
| `work.steps_per_item` | 128 | `rows` | `refuse` | `diagnostics, sender, health` | — |
| `work_gate_claims_per_round` | 32 | `rows` | `refuse` | `diagnostics, sender` | — |
| `work_gate_due_rows_per_pass` | 20 | `rows` | `evict_oldest` | `diagnostics` | — |
| `work_gate_evidence_artifact_bytes` | 2097152 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `work_gate_evidence_artifacts_per_task` | 8 | `rows` | `refuse` | `diagnostics, sender` | — |
| `work_gate_evidence_string_bytes` | 500 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `work_gate_evidence_strings_per_claim` | 8 | `rows` | `refuse` | `diagnostics, sender` | — |
| `work_gate_evidence_total_bytes_per_task` | 8388608 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `work_gate_owner_offline_grace_seconds` | 900 | `seconds` | `evict_oldest` | `diagnostics, notice` | — |
| `work_gate_recent_rounds_per_item_read` | 10 | `rows` | `evict_oldest` | `diagnostics` | — |
| `work_gate_result_bytes` | 65536 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `work_gate_retry_backoff_seconds` | 300 | `seconds` | `expire` | `diagnostics` | — |
| `work_gate_round_details_per_item` | 64 | `rows` | `refuse` | `diagnostics, notice, health` | — |
| `work_gate_round_details_per_store` | 10000 | `rows` | `refuse` | `diagnostics, notice, health` | — |
| `work_gate_tasks_per_round` | 2 | `rows` | `refuse` | `diagnostics, sender` | — |
