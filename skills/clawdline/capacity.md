# Capacity contract in this build

Generated directly from `internal/domain/capacity.Register()`; these are default bounds. A runtime override may only lower one. Read the target machine's live capacity state before an action. If a reading is missing or capacity is full, follow the current guide and typed refusal; a default bound is not available room. `GET /v1/capacity` needs a paired device; `GET /v1/diagnostics` needs the machine key.

| Name | Default bound | Unit | At limit | Told through | Current deviation |
| --- | ---: | --- | --- | --- | --- |
| `antigravity.ancestry_rows` | 32 | `rows` | `refuse` | `diagnostics, sender` | — |
| `antigravity.hook_config_bytes` | 1048576 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `antigravity.hook_payload_bytes` | 1048576 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `antigravity.hook_timeout_seconds` | 3 | `seconds` | `refuse` | `diagnostics, sender` | — |
| `antigravity.live_bindings` | 512 | `rows` | `refuse` | `diagnostics, sender` | — |
| `antigravity.managed_launches` | 512 | `rows` | `refuse` | `diagnostics, sender` | — |
| `antigravity.native_process_file_bytes` | 1048576 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `antigravity.native_tty_entries` | 4096 | `rows` | `refuse` | `diagnostics, sender` | — |
| `antigravity.observation_bytes` | 4194304 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `antigravity.process_read_seconds` | 3 | `seconds` | `refuse` | `diagnostics, sender` | — |
| `antigravity.retention_seconds` | 86400 | `seconds` | `expire` | `diagnostics, sender` | — |
| `antigravity.state_age_seconds` | 15 | `seconds` | `expire` | `diagnostics, sender` | — |
| `artifacts.drops` | 268435456 | `bytes` | `evict_oldest` | `diagnostics, log` | — |
| `artifacts.drops_young` | 1 | `rows` | `evict_oldest` | `diagnostics, notice, log` | — |
| `artifacts.image_bytes` | 67108864 | `bytes` | `evict_oldest` | `diagnostics, notice, log` | — |
| `artifacts.images` | 64 | `rows` | `evict_oldest` | `diagnostics, notice, log` | — |
| `artifacts.report_image_pixels` | 16777216 | `rows` | `refuse` | `diagnostics, sender` | — |
| `artifacts.report_image_side` | 8192 | `rows` | `refuse` | `diagnostics, sender` | — |
| `artifacts.report_total_image_pixels` | 67108864 | `rows` | `refuse` | `diagnostics, sender` | — |
| `audit.security` | 8388608 | `bytes` | `rotate` | `diagnostics, notice, health` | — |
| `board.receipts` | 4096 | `rows` | `refuse` | `diagnostics, notice` | — |
| `cache.assistant_quota` | 32 | `rows` | `evict_oldest` | `diagnostics, notice` | — |
| `cache.background_agents` | 256 | `rows` | `evict_oldest` | `diagnostics, notice` | — |
| `cache.git_not_repo` | 60 | `seconds` | `expire` | `diagnostics` | — |
| `cache.git_not_repo_rows` | 256 | `rows` | `evict_oldest` | `diagnostics` | — |
| `cache.image_thumbs` | 8388608 | `bytes` | `evict_oldest` | `diagnostics` | — |
| `cache.pinned_read` | 3 | `seconds` | `expire` | `diagnostics` | — |
| `cache.provider_quota` | 4 | `rows` | `evict_oldest` | `diagnostics` | — |
| `cache.provider_quota_refresh_seconds` | 900 | `seconds` | `evict_oldest` | `diagnostics` | — |
| `cache.provider_quota_request_seconds` | 10 | `seconds` | `evict_oldest` | `diagnostics` | — |
| `cache.provider_quota_response_bytes` | 1048576 | `bytes` | `refuse` | `diagnostics` | — |
| `cache.reclaim_summary` | 30 | `seconds` | `expire` | `diagnostics` | — |
| `cache.session_activity` | 64 | `rows` | `evict_oldest` | `diagnostics, notice` | — |
| `cache.session_execution_memo` | 4096 | `rows` | `evict_oldest` | `diagnostics` | — |
| `cache.session_inventory` | 120 | `seconds` | `expire` | `diagnostics` | — |
| `cache.session_links` | 64 | `rows` | `evict_oldest` | `diagnostics, notice` | — |
| `cache.session_project_keys` | 256 | `rows` | `evict_oldest` | `diagnostics` | — |
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
| `cloud.content_key_epoch_record_bytes` | 4096 | `bytes` | `refuse` | `diagnostics` | — |
| `cloud.envelope_key_id` | 128 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `cloud.header_read_diagnostic_rows` | 24 | `rows` | `evict_oldest` | `diagnostics` | — |
| `cloud.header_read_diagnostic_seconds` | 3600 | `seconds` | `expire` | `diagnostics` | — |
| `cloud.health_active_slots` | 128 | `rows` | `refuse` | `diagnostics` | — |
| `cloud.health_capture_budget_seconds` | 2 | `seconds` | `refuse` | `diagnostics` | — |
| `cloud.health_capture_export_bytes` | 524288 | `bytes` | `refuse` | `diagnostics` | — |
| `cloud.health_capture_frames` | 64 | `rows` | `refuse` | `diagnostics` | — |
| `cloud.health_capture_raw_bytes` | 1048576 | `bytes` | `refuse` | `diagnostics` | — |
| `cloud.health_captures_per_minute` | 4 | `rows` | `refuse` | `diagnostics` | — |
| `cloud.health_investigation_bundle_bytes` | 1048576 | `bytes` | `refuse` | `diagnostics` | — |
| `cloud.health_investigation_read_seconds` | 5 | `seconds` | `disconnect` | `diagnostics` | — |
| `cloud.health_investigation_report_bytes` | 2048 | `bytes` | `refuse` | `diagnostics` | — |
| `cloud.health_investigation_report_refs` | 8 | `rows` | `refuse` | `diagnostics` | — |
| `cloud.health_journal_bytes` | 4194304 | `bytes` | `evict_oldest` | `diagnostics` | — |
| `cloud.health_journal_rows` | 4096 | `rows` | `evict_oldest` | `diagnostics` | — |
| `cloud.health_owner_incidents` | 32 | `rows` | `refuse` | `diagnostics` | — |
| `cloud.health_pending_transitions` | 256 | `rows` | `refuse` | `diagnostics` | — |
| `cloud.health_query_bytes` | 1048576 | `bytes` | `evict_oldest` | `diagnostics, sender` | — |
| `cloud.health_query_rows` | 200 | `rows` | `evict_oldest` | `diagnostics, sender` | — |
| `cloud.health_record_bytes` | 1024 | `bytes` | `refuse` | `diagnostics` | — |
| `cloud.health_reserved_slots` | 8 | `rows` | `refuse` | `diagnostics` | — |
| `cloud.health_sealed_attempt_seconds` | 10 | `seconds` | `disconnect` | `diagnostics` | — |
| `cloud.health_sealed_attempts` | 4 | `rows` | `expire` | `diagnostics` | — |
| `cloud.health_sealed_ciphertext_bytes` | 262144 | `bytes` | `refuse` | `diagnostics` | — |
| `cloud.health_sealed_life_seconds` | 604800 | `seconds` | `expire` | `diagnostics` | — |
| `cloud.health_sealed_queued_jobs` | 8 | `rows` | `refuse` | `diagnostics` | — |
| `cloud.health_sealed_request_bytes` | 409600 | `bytes` | `refuse` | `diagnostics` | — |
| `cloud.health_sealed_response_bytes` | 16384 | `bytes` | `refuse` | `diagnostics` | — |
| `cloud.health_sealed_revisions` | 8 | `rows` | `refuse` | `diagnostics` | — |
| `cloud.health_sealed_snapshot_bytes` | 16384 | `bytes` | `refuse` | `diagnostics` | — |
| `cloud.health_sealed_snapshot_coverage` | 32 | `rows` | `refuse` | `diagnostics` | — |
| `cloud.health_sealed_snapshot_field_chars` | 512 | `characters` | `refuse` | `diagnostics` | — |
| `cloud.health_sealed_snapshot_frames` | 24 | `rows` | `refuse` | `diagnostics` | — |
| `cloud.health_sealed_snapshot_sources` | 8 | `rows` | `refuse` | `diagnostics` | — |
| `cloud.health_sealed_store_attachments` | 16 | `rows` | `refuse` | `diagnostics` | — |
| `cloud.health_sealed_store_bytes` | 4194304 | `bytes` | `refuse` | `diagnostics` | — |
| `cloud.health_store_bytes` | 104857600 | `bytes` | `evict_oldest` | `diagnostics` | — |
| `cloud.health_store_dir_entries` | 4096 | `rows` | `refuse` | `diagnostics` | — |
| `cloud.health_store_frame_bytes` | 4096 | `bytes` | `refuse` | `diagnostics` | — |
| `cloud.health_store_lost_ranges` | 64 | `rows` | `evict_oldest` | `diagnostics` | — |
| `cloud.health_store_queue_bytes` | 2097152 | `bytes` | `refuse` | `diagnostics` | — |
| `cloud.health_store_queue_rows` | 1024 | `rows` | `refuse` | `diagnostics` | — |
| `cloud.health_store_readers` | 4 | `rows` | `refuse` | `diagnostics, sender` | — |
| `cloud.health_store_seconds` | 604800 | `seconds` | `evict_oldest` | `diagnostics` | — |
| `cloud.health_store_segment_bytes` | 8388608 | `bytes` | `rotate` | `diagnostics` | — |
| `cloud.health_store_segments` | 256 | `rows` | `evict_oldest` | `diagnostics` | — |
| `cloud.health_upload_attempt_seconds` | 10 | `seconds` | `disconnect` | `diagnostics` | — |
| `cloud.health_upload_attempts` | 6 | `rows` | `expire` | `diagnostics` | — |
| `cloud.health_upload_batch_bytes` | 32768 | `bytes` | `refuse` | `diagnostics` | — |
| `cloud.health_upload_batch_events` | 100 | `rows` | `refuse` | `diagnostics` | — |
| `cloud.health_upload_hourly_bytes` | 983040 | `bytes` | `refuse` | `diagnostics` | — |
| `cloud.health_upload_hourly_requests` | 120 | `rows` | `refuse` | `diagnostics` | — |
| `cloud.health_upload_receipts` | 16 | `rows` | `evict_oldest` | `diagnostics` | — |
| `cloud.health_upload_response_bytes` | 16384 | `bytes` | `refuse` | `diagnostics` | — |
| `cloud.health_upload_retry_after_seconds` | 3600 | `seconds` | `coalesce` | `diagnostics` | — |
| `cloud.health_upload_retry_seconds` | 3600 | `seconds` | `expire` | `diagnostics` | — |
| `cloud.health_upload_state_bytes` | 65536 | `bytes` | `refuse` | `diagnostics` | — |
| `cloud.health_upload_step_pages` | 8 | `rows` | `refuse` | `diagnostics` | — |
| `cloud.identity_pairing_decision_bytes` | 1024 | `bytes` | `refuse` | `diagnostics` | — |
| `cloud.identity_pairing_poll_attempts` | 600 | `rows` | `refuse` | `diagnostics` | — |
| `cloud.identity_pairing_run_seconds` | 660 | `seconds` | `disconnect` | `diagnostics` | — |
| `cloud.peer_body_bytes` | 524288 | `bytes` | `refuse` | `diagnostics, notice, sender` | — |
| `cloud.peer_discovery_reply` | 1 | `rows` | `refuse` | `sender, diagnostics` | — |
| `cloud.peer_frame_bytes` | 1048576 | `bytes` | `refuse` | `diagnostics, log, sender` | — |
| `cloud.peer_grants` | 512 | `rows` | `refuse` | `diagnostics, notice, health` | — |
| `cloud.peer_inbox` | 512 | `rows` | `refuse` | `diagnostics, notice, health` | — |
| `cloud.peer_inbox_page` | 1 | `rows` | `refuse` | `diagnostics, log` | — |
| `cloud.peer_ingress` | 16 | `rows` | `refuse` | `diagnostics, log, sender` | — |
| `cloud.peer_outbox` | 512 | `rows` | `refuse` | `diagnostics, notice, health` | — |
| `cloud.peer_pairs` | 128 | `rows` | `refuse` | `diagnostics, notice, health` | — |
| `cloud.rekey_body` | 4096 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `cloud.rekey_interval` | 2 | `seconds` | `refuse` | `diagnostics, sender` | — |
| `cloud.rekey_lifetime` | 120 | `seconds` | `expire` | `diagnostics, sender` | — |
| `cloud.rekey_pending` | 8 | `rows` | `refuse` | `diagnostics, sender` | — |
| `cloud.relay_queue` | 64 | `rows` | `refuse` | `diagnostics, notice, cloud_status, log, sender` | — |
| `cloud.reload_stop_seconds` | 15 | `seconds` | `refuse` | `diagnostics, sender, log` | — |
| `cloud.roster_short_read_attempts` | 2 | `rows` | `refuse` | `diagnostics, sender` | — |
| `cloud.session_row_asks` | 64 | `rows` | `refuse` | `diagnostics, sender` | — |
| `cloud.session_row_id_bytes` | 256 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `cloud.session_row_volatile_seconds` | 15 | `seconds` | `expire` | `diagnostics` | — |
| `cloud.spool` | 2000 | `rows` | `refuse` | `diagnostics, notice, log` | — |
| `cloud.spool_bytes` | 16777216 | `bytes` | `refuse` | `diagnostics, notice, log` | — |
| `cloud.spool_channel_bytes` | 4194304 | `bytes` | `refuse` | `diagnostics, notice, log` | — |
| `cloud.spool_receipts` | 4096 | `rows` | `expire` | `diagnostics, log` | — |
| `cloud.spool_refusals` | 64 | `rows` | `refuse` | `diagnostics, log` | — |
| `cloud.task_list_reread_seconds` | 240 | `seconds` | `expire` | `diagnostics` | — |
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
| `cloud.viewer_events_batch_bytes` | 262144 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `cloud.viewer_events_line_bytes` | 1024 | `bytes` | `coalesce` | `diagnostics, log` | — |
| `cloud.viewer_events_logged_rows` | 128 | `rows` | `coalesce` | `diagnostics, log` | — |
| `cloud.viewer_events_seen_batches` | 256 | `rows` | `evict_oldest` | `diagnostics` | — |
| `cloud.viewer_rekey_cooldown` | 120 | `seconds` | `expire` | `diagnostics` | — |
| `cloud.viewer_rekey_pending` | 1 | `rows` | `refuse` | `diagnostics` | — |
| `console.carrier_receive_bytes` | 33554432 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `console.carrier_receive_rows` | 32 | `rows` | `refuse` | `diagnostics, sender` | — |
| `console.direct_history_hours` | 24 | `rows` | `evict_oldest` | `diagnostics` | — |
| `console.direct_history_machines` | 32 | `rows` | `evict_oldest` | `diagnostics` | — |
| `console.execution_recovery_attempts` | 1 | `rows` | `refuse` | `sender, diagnostics` | — |
| `console.health_live_seconds` | 90 | `seconds` | `expire` | `diagnostics` | — |
| `console.health_ui_body_bytes` | 1572864 | `bytes` | `refuse` | `diagnostics` | — |
| `console.health_ui_budget_seconds` | 12 | `seconds` | `expire` | `diagnostics` | — |
| `console.health_ui_event_pages` | 5 | `rows` | `refuse` | `diagnostics` | — |
| `console.health_ui_export_bytes` | 524288 | `bytes` | `refuse` | `diagnostics` | — |
| `console.health_ui_id_characters` | 64 | `characters` | `refuse` | `diagnostics` | — |
| `console.health_ui_incident_export_bytes` | 1048576 | `bytes` | `refuse` | `diagnostics` | — |
| `console.health_ui_incident_pages` | 5 | `rows` | `refuse` | `diagnostics` | — |
| `console.health_ui_incident_rows` | 50 | `rows` | `refuse` | `diagnostics` | — |
| `console.health_ui_incident_window_seconds` | 604800 | `seconds` | `expire` | `diagnostics` | — |
| `console.health_ui_investigation_refs` | 8 | `rows` | `refuse` | `diagnostics` | — |
| `console.health_ui_investigations` | 5 | `rows` | `refuse` | `diagnostics` | — |
| `console.health_ui_page_rows` | 200 | `rows` | `refuse` | `diagnostics` | — |
| `console.health_ui_parallel_reads` | 2 | `rows` | `refuse` | `diagnostics` | — |
| `console.health_ui_probe_claims` | 8 | `rows` | `refuse` | `diagnostics` | — |
| `console.health_ui_probe_hosts` | 204 | `rows` | `refuse` | `diagnostics` | — |
| `console.health_ui_probe_targets` | 64 | `rows` | `refuse` | `diagnostics` | — |
| `console.health_ui_reads_per_load` | 3 | `rows` | `refuse` | `diagnostics` | — |
| `console.health_ui_refresh_seconds` | 15 | `seconds` | `expire` | `diagnostics` | — |
| `console.health_ui_request_seconds` | 6 | `seconds` | `expire` | `diagnostics` | — |
| `console.health_ui_trend_lanes` | 12 | `rows` | `refuse` | `diagnostics` | — |
| `console.health_ui_trend_signals` | 20 | `rows` | `refuse` | `diagnostics` | — |
| `console.machine_list_poll_seconds` | 5 | `seconds` | `expire` | `diagnostics` | — |
| `console.native_pairing_save_seconds` | 15 | `seconds` | `expire` | `sender, diagnostics` | — |
| `console.opened_row_quiet_seconds` | 8 | `seconds` | `expire` | `diagnostics` | — |
| `console.opened_row_restate_seconds` | 5 | `seconds` | `expire` | `diagnostics` | — |
| `console.owner_retention_poll_seconds` | 5 | `seconds` | `expire` | `diagnostics` | — |
| `console.owner_retention_request_seconds` | 5 | `seconds` | `expire` | `diagnostics` | — |
| `console.persona_retry_seconds` | 5 | `seconds` | `expire` | `diagnostics` | — |
| `console.pinned_list_read_timeout_seconds` | 10 | `seconds` | `expire` | `diagnostics` | — |
| `console.relay_feature_wait_seconds` | 5 | `seconds` | `expire` | `diagnostics` | — |
| `console.relay_transcript_expect_reask_seconds` | 4 | `seconds` | `expire` | `diagnostics` | — |
| `console.session_notes_safety_seconds` | 60 | `seconds` | `expire` | `diagnostics` | — |
| `console.status_gap_retry_seconds` | 5 | `seconds` | `expire` | `diagnostics` | — |
| `console.status_marker_recovery_wait_seconds` | 5 | `seconds` | `expire` | `diagnostics` | — |
| `console.status_marker_retry_seconds` | 60 | `seconds` | `expire` | `diagnostics` | — |
| `console.transcript_backoff_seconds` | 30 | `seconds` | `expire` | `diagnostics` | — |
| `console.transcript_fill_pages` | 32 | `rows` | `refuse` | `sender, diagnostics` | — |
| `console.transcript_follow_fast_seconds` | 10 | `seconds` | `expire` | `diagnostics` | — |
| `console.transcript_follow_later_seconds` | 4 | `seconds` | `expire` | `diagnostics` | — |
| `console.transcript_follow_seconds` | 90 | `seconds` | `expire` | `diagnostics` | — |
| `console.transcript_merged_rows` | 1000 | `rows` | `expire` | `diagnostics` | — |
| `console.transcript_safety_seconds` | 30 | `seconds` | `expire` | `diagnostics` | — |
| `console.viewer_health_attempts` | 4 | `rows` | `refuse` | `diagnostics` | — |
| `console.viewer_health_backoff_seconds` | 60 | `seconds` | `expire` | `diagnostics` | — |
| `console.viewer_health_batch_rows` | 50 | `rows` | `refuse` | `diagnostics` | — |
| `console.viewer_health_body_bytes` | 16384 | `bytes` | `refuse` | `diagnostics` | — |
| `console.viewer_health_buffer_rows` | 200 | `rows` | `evict_oldest` | `diagnostics` | — |
| `console.viewer_health_coalesce_seconds` | 5 | `seconds` | `expire` | `diagnostics` | — |
| `console.viewer_health_count_rows` | 1000000 | `rows` | `coalesce` | `diagnostics` | — |
| `console.viewer_health_elapsed_seconds` | 3600 | `seconds` | `coalesce` | `diagnostics` | — |
| `console.viewer_health_hourly_bytes` | 262144 | `bytes` | `refuse` | `diagnostics` | — |
| `console.viewer_health_hourly_requests` | 60 | `rows` | `refuse` | `diagnostics` | — |
| `console.viewer_health_request_seconds` | 10 | `seconds` | `expire` | `diagnostics` | — |
| `console.viewer_health_retry_after_seconds` | 3600 | `seconds` | `expire` | `diagnostics` | — |
| `console.viewer_health_send_every_seconds` | 60 | `seconds` | `expire` | `diagnostics` | — |
| `coordinator.aliases` | 32 | `rows` | `evict_oldest` | `diagnostics` | — |
| `coordinator.bind_attempts` | 6 | `rows` | `refuse` | `diagnostics, sender` | — |
| `decisions.open` | 256 | `rows` | `refuse` | `diagnostics, sender` | — |
| `deploy.health_seconds` | 30 | `seconds` | `refuse` | `diagnostics` | — |
| `devices.list` | 512 | `rows` | `refuse` | `diagnostics, notice, health` | — |
| `diagnostics.route_latency_samples` | 256 | `rows` | `evict_oldest` | `diagnostics` | — |
| `diagnostics.route_stat_keys` | 512 | `rows` | `coalesce` | `diagnostics` | — |
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
| `iterm.viewer_concurrent` | 1 | `rows` | `refuse` | `diagnostics, log` | — |
| `iterm.viewer_open_seconds` | 20 | `seconds` | `refuse` | `diagnostics, log` | — |
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
| `notification.rule_label_characters` | 80 | `characters` | `refuse` | `diagnostics, sender` | — |
| `notification.rule_wait_seconds` | 7200 | `seconds` | `evict_oldest` | `diagnostics, sender` | — |
| `notification.rules_rows` | 16 | `rows` | `refuse` | `diagnostics, health, sender` | — |
| `orchestrator.opening_stuck_seconds` | 1800 | `seconds` | `evict_oldest` | `diagnostics` | — |
| `personas.catalog` | 64 | `rows` | `refuse` | `diagnostics` | — |
| `personas.suggestion_context_bytes` | 16384 | `bytes` | `evict_oldest` | `diagnostics` | — |
| `personas.text_bytes` | 8192 | `bytes` | `refuse` | `diagnostics` | — |
| `places.antigravity_history_bytes` | 67108864 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `places.antigravity_history_entries` | 512 | `rows` | `refuse` | `diagnostics, sender` | — |
| `places.antigravity_history_milliseconds` | 250 | `milliseconds` | `refuse` | `diagnostics, sender` | — |
| `places.antigravity_history_title_characters` | 80 | `characters` | `refuse` | `diagnostics, sender` | — |
| `places.antigravity_probe_output_bytes` | 65536 | `bytes` | `refuse` | `diagnostics` | — |
| `places.antigravity_probe_seconds` | 3 | `seconds` | `refuse` | `diagnostics` | — |
| `places.git_config_bytes` | 65536 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `places.registered` | 512 | `rows` | `refuse` | `diagnostics, notice, health` | — |
| `places.repo_lookups` | 40 | `rows` | `evict_oldest` | `diagnostics` | — |
| `places.repo_readers` | 4 | `rows` | `refuse` | `diagnostics` | — |
| `places.repo_wait_seconds` | 1 | `seconds` | `refuse` | `diagnostics` | — |
| `places.setup_age_seconds` | 60 | `seconds` | `evict_oldest` | `diagnostics` | — |
| `places.setup_lookups` | 40 | `rows` | `evict_oldest` | `diagnostics` | — |
| `places.setup_readers` | 4 | `rows` | `refuse` | `diagnostics` | — |
| `places.setup_wait_seconds` | 1 | `seconds` | `refuse` | `diagnostics` | — |
| `planreview.blocking_listed` | 8 | `rows` | `coalesce` | `diagnostics, sender` | — |
| `projectfiles.file_bytes` | 131072 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `projectfiles.list` | 128 | `rows` | `refuse` | `diagnostics, sender` | — |
| `projectfiles.scan_entries` | 1024 | `rows` | `refuse` | `diagnostics, sender` | — |
| `projectfiles.skill_summary_runes` | 240 | `characters` | `evict_oldest` | `diagnostics, sender` | — |
| `projectfiles.tree_depth` | 64 | `rows` | `refuse` | `diagnostics, sender` | — |
| `projectfiles.tree_path_bytes` | 4096 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `projectfiles.write_bytes` | 262144 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `projects.folder_rows` | 128 | `rows` | `refuse` | `diagnostics, sender` | — |
| `projectsync.clones` | 2 | `rows` | `refuse` | `diagnostics, sender` | — |
| `projectsync.entry_bytes` | 4194304 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `projectsync.file_bytes` | 262144 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `projectsync.manifest_projects` | 256 | `rows` | `refuse` | `diagnostics, sender` | — |
| `projectsync.mirrored` | 512 | `rows` | `refuse` | `diagnostics, sender, health` | — |
| `projectsync.path_bytes` | 512 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `projectsync.project_files` | 64 | `rows` | `refuse` | `diagnostics, sender` | — |
| `proposals.open` | 500 | `rows` | `refuse` | `diagnostics, sender` | — |
| `push.subscriptions` | 128 | `rows` | `refuse` | `diagnostics, notice, health` | — |
| `release.app_relist_seconds` | 60 | `seconds` | `expire` | `diagnostics` | — |
| `release.app_swap_poll_seconds` | 2 | `seconds` | `evict_oldest` | `diagnostics` | — |
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
| `schedule.run_transcript_head` | 65536 | `bytes` | `evict_oldest` | `diagnostics` | — |
| `schedule.run_transcripts_probed` | 256 | `rows` | `evict_oldest` | `diagnostics` | — |
| `screens.capture_slots` | 2 | `rows` | `refuse` | `diagnostics` | — |
| `session.close_scheduled` | 16 | `rows` | `refuse` | `diagnostics, sender` | — |
| `session.close_scheduled_per_terminal` | 1 | `rows` | `coalesce` | `diagnostics, sender` | — |
| `session.close_scheduled_seconds` | 900 | `seconds` | `expire` | `diagnostics, log` | — |
| `session.codex_past_page` | 20 | `rows` | `evict_oldest` | `diagnostics, sender` | — |
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
| `session.secret_handoff_bytes` | 16384 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `session.secret_handoff_delivery_seconds` | 5 | `seconds` | `refuse` | `diagnostics, sender` | — |
| `session.secret_handoff_pending` | 128 | `rows` | `refuse` | `diagnostics, sender` | — |
| `session.secret_handoff_seconds` | 900 | `seconds` | `expire` | `diagnostics, sender` | — |
| `session.title_age` | 7776000 | `seconds` | `evict_oldest` | `diagnostics, notice` | — |
| `session.title_characters` | 200 | `characters` | `refuse` | `diagnostics, sender` | — |
| `session.title_request_bytes` | 16384 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `session.title_rows` | 200 | `rows` | `evict_oldest` | `diagnostics, notice` | — |
| `session.todo_batch_rows` | 20 | `rows` | `refuse` | `diagnostics, sender` | — |
| `session.waiting_for` | 8 | `rows` | `evict_oldest` | `diagnostics` | — |
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
| `store.done_effect_retention_seconds` | 604800 | `seconds` | `evict_oldest` | `diagnostics` | — |
| `store.events` | 150000 | `rows` | `evict_oldest` | `diagnostics` | — |
| `store.read_connections` | 4 | `rows` | `refuse` | `diagnostics` | — |
| `store.receipt_tombstone_retention_seconds` | 604800 | `seconds` | `expire` | `diagnostics` | — |
| `store.receipts` | 4096 | `rows` | `refuse` | `diagnostics, notice` | — |
| `store.retention_prune_batch_rows` | 500 | `rows` | `evict_oldest` | `diagnostics` | — |
| `terminal.body_bytes` | 6295552 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `terminal.count` | 8 | `rows` | `refuse` | `diagnostics, sender` | — |
| `terminal.history_lines` | 2000 | `rows` | `evict_oldest` | `diagnostics` | — |
| `terminal.input_bytes` | 4096 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `terminal.lane` | 16 | `rows` | `refuse` | `diagnostics, sender` | — |
| `terminal.launch_line_bytes` | 512 | `bytes` | `refuse` | `diagnostics` | — |
| `terminal.launch_scripts` | 64 | `rows` | `evict_oldest` | `diagnostics` | — |
| `terminal.lease_seconds` | 30 | `seconds` | `expire` | `diagnostics, sender` | — |
| `terminal.paste_bytes` | 1048576 | `bytes` | `refuse` | `diagnostics, sender` | — |
| `terminal.streams` | 16 | `rows` | `refuse` | `diagnostics, sender` | — |
| `terminal.viewers` | 8 | `rows` | `refuse` | `diagnostics, sender` | — |
| `update.apply_body_bytes` | 4096 | `bytes` | `refuse` | `diagnostics` | — |
| `update.build_body_bytes` | 4096 | `bytes` | `refuse` | `diagnostics` | — |
| `update.fetch_timeout_seconds` | 10 | `seconds` | `evict_oldest` | `diagnostics` | — |
| `update.refresh_seconds` | 1800 | `seconds` | `evict_oldest` | `diagnostics` | — |
| `usage.daily_days` | 57 | `rows` | `evict_oldest` | `diagnostics, sender` | — |
| `usage.daily_rows` | 50000 | `rows` | `evict_oldest` | `diagnostics, sender` | — |
| `usage.overview_tasks` | 10000 | `rows` | `evict_oldest` | `diagnostics, sender` | — |
| `usage.work_cursor_queue` | 1024 | `rows` | `refuse` | `diagnostics, log` | — |
| `usage.work_cursor_rows` | 50000 | `rows` | `evict_oldest` | `diagnostics` | — |
| `usage.work_units_per_answer` | 500 | `rows` | `evict_oldest` | `diagnostics, sender` | — |
| `voice.model_download_bytes` | 574041195 | `bytes` | `refuse` | `diagnostics, sender` | — |
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
