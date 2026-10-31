# Statically declared refusal codes in this build

Generated from typed refusal calls in the current Go source. It lists directly declared codes and their source files; dynamically computed codes are not included, so absence is not proof that a code cannot occur. Branch on the `error` field, never on prose. Run `clawdline guide refused <code>` for handling. If this build's guide has no rule for a code, or state or receipt evidence is unknown, stop the dependent write and report it; do not retry by guesswork.

| Code | Declared in |
| --- | --- |
| `acceptance_required` | `internal/domain/work/v2.go` |
| `actor_not_manager` | `internal/transport/http/squad.go` |
| `actor_unverified` | `internal/transport/http/squad.go` |
| `antigravity_consent_body_too_large` | `internal/transport/http/antigravity_project_consent.go` |
| `antigravity_project_unavailable` | `internal/transport/http/antigravity_project_consent.go` |
| `archive_batch_too_large` | `internal/transport/http/session_archive.go` |
| `artifact_expired` | `internal/transport/http/images.go` |
| `artifact_not_found` | `internal/transport/http/images.go` |
| `artifact_storage_failed` | `internal/transport/http/images.go` |
| `bad_assistant` | `internal/transport/http/cloud_agent.go` |
| `bad_machine` | `internal/transport/http/cloud_agent.go` |
| `bad_offer` | `internal/transport/http/cloud_agent.go` |
| `bad_path` | `internal/transport/http/page.go` |
| `bad_query` | `internal/transport/http/images.go`, `internal/transport/http/work_v2_reference_images.go` |
| `bad_request` | `internal/transport/http/actions.go`, `internal/transport/http/antigravity_project_consent.go`, `internal/transport/http/auth.go`, `internal/transport/http/board.go`, `internal/transport/http/body.go`, `internal/transport/http/callbacks.go`, `internal/transport/http/cloud.go`, `internal/transport/http/cloud_agent.go`, `internal/transport/http/cloud_identity_pairing.go`, `internal/transport/http/cloud_onboarding.go`, `internal/transport/http/cloud_peer.go`, `internal/transport/http/cloud_remote_discovery.go`, `internal/transport/http/cloud_remote_routes.go`, `internal/transport/http/cloud_viewer.go`, `internal/transport/http/connection_health.go`, `internal/transport/http/diagnostics.go`, `internal/transport/http/dispatch_policy_settings.go`, `internal/transport/http/documents.go`, `internal/transport/http/gate.go`, `internal/transport/http/git.go`, `internal/transport/http/graphify.go`, `internal/transport/http/images.go`, `internal/transport/http/integration_settings.go`, `internal/transport/http/intent.go`, `internal/transport/http/keys.go`, `internal/transport/http/machine_usage.go`, `internal/transport/http/notification_rules.go`, `internal/transport/http/orchestrator.go`, `internal/transport/http/project_files.go`, `internal/transport/http/project_folders.go`, `internal/transport/http/project_memory.go`, `internal/transport/http/project_sync.go`, `internal/transport/http/project_tree.go`, `internal/transport/http/project_unify.go`, `internal/transport/http/projects.go`, `internal/transport/http/push.go`, `internal/transport/http/pwa.go`, `internal/transport/http/schedule_webhook_create.go`, `internal/transport/http/schedules.go`, `internal/transport/http/server.go`, `internal/transport/http/session_archive.go`, `internal/transport/http/session_restore.go`, `internal/transport/http/settings.go`, `internal/transport/http/skill_install.go`, `internal/transport/http/snippets.go`, `internal/transport/http/squad.go`, `internal/transport/http/squad_packages.go`, `internal/transport/http/squad_role_draft.go`, `internal/transport/http/squad_skill_sources.go`, `internal/transport/http/start.go`, `internal/transport/http/tasks.go`, `internal/transport/http/todos.go`, `internal/transport/http/tracks.go`, `internal/transport/http/transcript.go`, `internal/transport/http/update.go`, `internal/transport/http/usage.go`, `internal/transport/http/usage_overview.go`, `internal/transport/http/usage_work_units.go`, `internal/transport/http/verify.go`, `internal/transport/http/voice.go`, `internal/transport/http/waits.go`, `internal/transport/http/work.go`, `internal/transport/http/work_v2.go`, `internal/transport/http/workflow.go` |
| `bad_task_id` | `internal/transport/http/cloud_remote_routes.go` |
| `bad_tree_path` | `internal/transport/http/project_tree.go` |
| `board_command_too_large` | `internal/transport/http/board.go` |
| `board_response_too_large` | `internal/transport/http/board.go` |
| `board_selector_not_implemented` | `internal/transport/http/board.go` |
| `board_store_unreadable` | `internal/transport/http/board_settings.go` |
| `board_unavailable` | `internal/transport/http/board.go` |
| `body_too_large` | `internal/transport/http/body.go`, `internal/transport/http/project_files.go`, `internal/transport/http/project_memory.go`, `internal/transport/http/project_sync.go`, `internal/transport/http/projects.go`, `internal/transport/http/session_restore.go`, `internal/transport/http/snippets.go`, `internal/transport/http/squad.go`, `internal/transport/http/squad_packages.go`, `internal/transport/http/squad_role_draft.go`, `internal/transport/http/work.go`, `internal/transport/http/work_v2.go`, `internal/transport/http/work_v2_session_name.go` |
| `broker_unavailable` | `internal/transport/http/cloud_agent.go` |
| `browser_setting_unavailable` | `internal/transport/http/settings.go` |
| `builtin_read_only` | `internal/transport/http/squad.go` |
| `busy` | `internal/transport/http/intent.go`, `internal/transport/http/session_smart_title.go`, `internal/transport/http/snippets.go`, `internal/transport/http/squad.go`, `internal/transport/http/squad_role_draft.go`, `internal/transport/http/voice.go`, `internal/transport/http/work_v2_persona_suggestion.go` |
| `capacity_full` | `internal/transport/http/squad.go` |
| `catalog_unreadable` | `internal/transport/http/tasks.go` |
| `child_kind_not_allowed` | `internal/domain/work/v2.go` |
| `claude_remote_read_failed` | `internal/transport/http/transcript.go` |
| `clone_busy` | `internal/transport/http/project_sync.go` |
| `clone_root_unavailable` | `internal/transport/http/project_sync.go` |
| `clone_target_exists` | `internal/transport/http/project_sync.go` |
| `close_inventory_unavailable` | `internal/transport/http/actions.go` |
| `close_not_proven` | `internal/transport/http/actions.go`, `internal/transport/http/agent_session_close.go` |
| `close_schedule_full` | `internal/transport/http/agent_session_close.go` |
| `closeability_unknown` | `internal/transport/http/actions.go` |
| `cloud_not_signed_in` | `internal/transport/http/cloud.go`, `internal/transport/http/cloud_identity_pairing.go`, `internal/transport/http/cloud_onboarding.go` |
| `cloud_off` | `internal/transport/http/cloud.go`, `internal/transport/http/cloud_identity_pairing.go` |
| `cloud_runtime_unavailable` | `internal/transport/http/cloud.go`, `internal/transport/http/cloud_onboarding.go` |
| `cloud_unavailable` | `internal/transport/http/schedule_webhook_create.go` |
| `confirmation_mismatch` | `internal/transport/http/work_v2.go` |
| `conflicting_workflow_settings` | `internal/transport/http/integration_settings.go` |
| `conversation_empty` | `internal/transport/http/session_smart_title.go` |
| `conversation_id_malformed` | `internal/transport/http/human_interventions.go`, `internal/transport/http/work_v2_session_todos.go` |
| `conversation_id_required` | `internal/transport/http/orchestrator.go` |
| `conversation_unknown` | `internal/transport/http/todos.go` |
| `conversation_unreadable` | `internal/transport/http/session_smart_title.go` |
| `decision_is_for_sessions` | `internal/transport/http/proposals.go` |
| `definition_version_conflict` | `internal/transport/http/squad.go` |
| `delivery_evidence_invalid` | `internal/transport/http/orchestrator.go` |
| `description_required` | `internal/domain/work/v2.go` |
| `device_list_full` | `internal/transport/http/auth.go` |
| `digest_mismatch` | `internal/transport/http/squad.go` |
| `doctor_busy` | `internal/transport/http/doctor.go` |
| `doctor_report_too_large` | `internal/transport/http/doctor.go` |
| `document_role_not_applicable` | `internal/domain/work/v2.go` |
| `empty_text` | `internal/transport/http/actions.go` |
| `encoding_failed` | `internal/transport/http/images.go` |
| `epic_children_open` | `internal/domain/work/v2.go` |
| `epic_not_planned` | `internal/domain/work/v2.go` |
| `event_conflict` | `internal/transport/http/squad_events.go` |
| `execution_check_unavailable` | `internal/transport/http/sessions.go` |
| `execution_generation_changed` | `internal/transport/http/cloud_peer.go` |
| `execution_source_unknown` | `internal/transport/http/transcript_details.go`, `internal/transport/http/transcript_tools.go` |
| `execution_target_required` | `internal/transport/http/images.go` |
| `expired` | `internal/transport/http/auth.go` |
| `file_permission` | `internal/transport/http/project_files.go`, `internal/transport/http/project_memory.go`, `internal/transport/http/project_unify.go` |
| `forbidden` | `internal/transport/http/actions.go`, `internal/transport/http/antigravity_project_consent.go`, `internal/transport/http/auth.go`, `internal/transport/http/board.go`, `internal/transport/http/callbacks.go`, `internal/transport/http/cloud.go`, `internal/transport/http/cloud_onboarding.go`, `internal/transport/http/cloud_peer.go`, `internal/transport/http/coordinator.go`, `internal/transport/http/focus.go`, `internal/transport/http/gate.go`, `internal/transport/http/graphify.go`, `internal/transport/http/handoffs.go`, `internal/transport/http/human_interventions.go`, `internal/transport/http/images.go`, `internal/transport/http/intent.go`, `internal/transport/http/orchestrator.go`, `internal/transport/http/project_files.go`, `internal/transport/http/project_memory.go`, `internal/transport/http/project_sync.go`, `internal/transport/http/project_unify.go`, `internal/transport/http/runs.go`, `internal/transport/http/schedule_webhook_create.go`, `internal/transport/http/schedules.go`, `internal/transport/http/session_outputs.go`, `internal/transport/http/session_pauses.go`, `internal/transport/http/session_restore.go`, `internal/transport/http/skill_install.go`, `internal/transport/http/snippets.go`, `internal/transport/http/squad.go`, `internal/transport/http/squad_packages.go`, `internal/transport/http/squad_skill_sources.go`, `internal/transport/http/start.go`, `internal/transport/http/todos.go`, `internal/transport/http/update.go`, `internal/transport/http/update_dev_release.go`, `internal/transport/http/voice.go`, `internal/transport/http/waits.go`, `internal/transport/http/work.go`, `internal/transport/http/work_v2.go`, `internal/transport/http/workflow.go` |
| `force_refused` | `internal/transport/http/agent_session_close.go` |
| `gate_snapshot_required` | `internal/domain/work/v2.go` |
| `git_failed` | `internal/transport/http/git.go` |
| `git_file_not_changed` | `internal/transport/http/git.go` |
| `git_no_directory` | `internal/transport/http/git.go` |
| `git_permission_denied` | `internal/transport/http/git.go` |
| `git_timeout` | `internal/transport/http/git.go` |
| `git_too_large` | `internal/transport/http/git.go` |
| `git_unavailable` | `internal/transport/http/git.go` |
| `graphify_directory_missing` | `internal/transport/http/graphify.go` |
| `graphify_disabled` | `internal/transport/http/graphify.go` |
| `graphify_unavailable` | `internal/transport/http/graphify.go` |
| `home_unavailable` | `internal/transport/http/project_files.go`, `internal/transport/http/project_folders.go` |
| `icon_store_unavailable` | `internal/transport/http/projects.go`, `internal/transport/http/sessions.go` |
| `idempotency_conflict` | `internal/transport/http/squad.go` |
| `idempotency_key_required` | `internal/transport/http/orchestrator.go`, `internal/transport/http/snippets.go`, `internal/transport/http/squad.go`, `internal/transport/http/work.go` |
| `idempotency_key_reused` | `internal/transport/http/receipts.go` |
| `image_not_found` | `internal/transport/http/work_v2_reference_images.go` |
| `internal` | `internal/transport/http/actions.go`, `internal/transport/http/auth.go`, `internal/transport/http/board.go`, `internal/transport/http/orchestrator.go`, `internal/transport/http/push.go`, `internal/transport/http/terminals.go`, `internal/transport/http/tracks.go` |
| `invalid_audience_selector` | `internal/transport/http/board.go` |
| `invalid_command` | `internal/transport/http/board.go`, `internal/transport/http/work.go` |
| `invalid_cursor` | `internal/transport/http/agents.go`, `internal/transport/http/squad_events.go`, `internal/transport/http/tracks.go`, `internal/transport/http/transcript.go` |
| `invalid_deployment_policy` | `internal/domain/work/v2.go` |
| `invalid_event` | `internal/transport/http/squad_events.go` |
| `invalid_gate_result` | `internal/transport/http/orchestrator.go` |
| `invalid_initial_phase` | `internal/domain/work/v2.go` |
| `invalid_kind` | `internal/domain/work/v2.go`, `internal/transport/http/proposals.go` |
| `invalid_landing_evidence` | `internal/domain/work/v2.go` |
| `invalid_notification_rule` | `internal/transport/http/notification_rules.go` |
| `invalid_page_size` | `internal/transport/http/squad_events.go` |
| `invalid_persona_view` | `internal/transport/http/personas.go` |
| `invalid_phase` | `internal/transport/http/work_v2.go` |
| `invalid_project` | `internal/transport/http/project_sync.go` |
| `invalid_project_path` | `internal/transport/http/project_folders.go` |
| `invalid_project_paths` | `internal/transport/http/projects.go` |
| `invalid_request` | `internal/transport/http/work_v2.go`, `internal/transport/http/work_v2_session_name.go` |
| `invalid_source` | `internal/transport/http/squad_skill_sources.go` |
| `invalid_state` | `internal/transport/http/proposals.go` |
| `invalid_summary` | `internal/transport/http/work_v2_session_todos.go` |
| `invalid_title` | `internal/transport/http/work_v2_session_name.go` |
| `invalid_tool_result` | `internal/transport/http/transcript_details.go`, `internal/transport/http/transcript_tools.go` |
| `invalid_track` | `internal/transport/http/tracks.go` |
| `invalid_transcript_view` | `internal/transport/http/transcript.go` |
| `invalid_transition` | `internal/domain/work/v2.go` |
| `invalid_user_authorization` | `internal/transport/http/schedule_webhook_create.go` |
| `invalid_viewer_scope` | `internal/transport/http/cloud.go`, `internal/transport/http/cloud_agent.go` |
| `invalid_work_id` | `internal/transport/http/orchestrator.go` |
| `invalid_workflow_profile` | `internal/transport/http/integration_settings.go` |
| `issue_sync_authorization_required` | `internal/domain/work/issuesync.go` |
| `issue_sync_target_invalid` | `internal/domain/work/issuesync.go` |
| `item_terminal` | `internal/domain/work/v2.go` |
| `item_unassigned` | `internal/domain/work/v2.go` |
| `landing_owed` | `internal/domain/work/v2.go` |
| `landing_project_not_found` | `internal/transport/http/work_v2_agent.go`, `internal/transport/http/work_v2_landing.go` |
| `landing_recorded` | `internal/domain/work/v2.go` |
| `login_bad_response` | `internal/transport/http/cloud_onboarding.go` |
| `machine_required` | `internal/transport/http/projects.go`, `internal/transport/http/work_v2.go`, `internal/transport/http/work_v2_agent.go` |
| `machine_usage_unreadable` | `internal/transport/http/machine_usage.go` |
| `machine_usage_unsupported` | `internal/transport/http/machine_usage.go` |
| `memory_entry_exists` | `internal/transport/http/project_memory.go` |
| `memory_entry_invalid` | `internal/transport/http/project_memory.go` |
| `memory_entry_not_found` | `internal/transport/http/project_memory.go` |
| `memory_full` | `internal/transport/http/project_memory.go` |
| `memory_group_not_found` | `internal/transport/http/project_memory.go` |
| `memory_unreadable` | `internal/transport/http/project_memory.go` |
| `method_not_allowed` | `internal/transport/http/actions.go`, `internal/transport/http/agents.go`, `internal/transport/http/antigravity_project_consent.go`, `internal/transport/http/callbacks.go`, `internal/transport/http/cloud_remote_discovery.go`, `internal/transport/http/cloud_remote_routes.go`, `internal/transport/http/devstacks.go`, `internal/transport/http/dispatch_policy_settings.go`, `internal/transport/http/doctor.go`, `internal/transport/http/graphify.go`, `internal/transport/http/handoffs.go`, `internal/transport/http/images.go`, `internal/transport/http/integration_settings.go`, `internal/transport/http/intent.go`, `internal/transport/http/notification_rules.go`, `internal/transport/http/orchestrator.go`, `internal/transport/http/personas.go`, `internal/transport/http/project_files.go`, `internal/transport/http/project_folders.go`, `internal/transport/http/project_memory.go`, `internal/transport/http/project_sync.go`, `internal/transport/http/project_tree.go`, `internal/transport/http/project_unify.go`, `internal/transport/http/projects.go`, `internal/transport/http/proposals.go`, `internal/transport/http/provider_quotas.go`, `internal/transport/http/reports.go`, `internal/transport/http/runs.go`, `internal/transport/http/session_archive.go`, `internal/transport/http/session_outputs.go`, `internal/transport/http/session_restore.go`, `internal/transport/http/settings.go`, `internal/transport/http/shells.go`, `internal/transport/http/squad.go`, `internal/transport/http/squad_events.go`, `internal/transport/http/squad_packages.go`, `internal/transport/http/squad_role_draft.go`, `internal/transport/http/squad_skill_sources.go`, `internal/transport/http/terminals.go`, `internal/transport/http/tracks.go`, `internal/transport/http/update.go`, `internal/transport/http/update_dev_release.go`, `internal/transport/http/usage.go`, `internal/transport/http/verify.go`, `internal/transport/http/voice.go`, `internal/transport/http/voice_setup.go`, `internal/transport/http/waits.go`, `internal/transport/http/work.go`, `internal/transport/http/work_v2.go`, `internal/transport/http/work_v2_persona_suggestion.go`, `internal/transport/http/work_v2_reference_images.go`, `internal/transport/http/work_v2_session_todos.go`, `internal/transport/http/workflow.go` |
| `mirror_capacity` | `internal/transport/http/project_sync.go` |
| `mirror_source_mismatch` | `internal/transport/http/project_sync.go` |
| `mirror_store_unavailable` | `internal/transport/http/project_sync.go` |
| `namer_out_of_quota` | `internal/transport/http/session_smart_title.go` |
| `naming_failed` | `internal/transport/http/session_smart_title.go` |
| `no_document` | `internal/transport/http/page.go` |
| `no_flush` | `internal/transport/http/stream.go` |
| `no_folder` | `internal/transport/http/cloud_agent.go` |
| `no_namer` | `internal/transport/http/session_smart_title.go` |
| `no_persona_suggester` | `internal/transport/http/work_v2_persona_suggestion.go` |
| `no_planner` | `internal/transport/http/intent.go` |
| `no_role_drafter` | `internal/transport/http/squad_role_draft.go` |
| `no_root` | `internal/transport/http/proposals.go` |
| `no_run` | `internal/transport/http/runs.go` |
| `no_web_root` | `internal/transport/http/page.go` |
| `not_a_repo` | `internal/transport/http/git.go` |
| `not_an_agent_session` | `internal/transport/http/agent_session_only.go` |
| `not_epic_owner` | `internal/domain/work/v2.go` |
| `not_found` | `internal/transport/http/agents.go`, `internal/transport/http/auth.go`, `internal/transport/http/cloud_agent.go`, `internal/transport/http/git.go`, `internal/transport/http/links.go`, `internal/transport/http/page.go`, `internal/transport/http/project_sync.go`, `internal/transport/http/proposals.go`, `internal/transport/http/pwa.go`, `internal/transport/http/runs.go`, `internal/transport/http/schedules.go`, `internal/transport/http/session_callbacks.go`, `internal/transport/http/shells.go`, `internal/transport/http/skills.go`, `internal/transport/http/snippets.go`, `internal/transport/http/verify.go` |
| `not_subscribed` | `internal/transport/http/push.go` |
| `notification_rule_conflict` | `internal/transport/http/notification_rules.go` |
| `notification_rules_invalid` | `internal/transport/http/notification_rules.go` |
| `notification_situation_unsupported` | `internal/transport/http/notification_rules.go` |
| `output_identity_unavailable` | `internal/transport/http/session_outputs.go` |
| `output_path_invalid` | `internal/transport/http/session_outputs.go` |
| `pairing_expired` | `internal/transport/http/cloud.go` |
| `pairing_failed` | `internal/transport/http/cloud.go` |
| `pairing_not_waiting` | `internal/transport/http/cloud_identity_pairing.go` |
| `pairing_pending` | `internal/transport/http/cloud.go` |
| `pairing_refused` | `internal/transport/http/cloud.go` |
| `parent_not_epic` | `internal/domain/work/v2.go` |
| `peer_control_refused` | `internal/transport/http/cloud_peer.go` |
| `peer_cursor_not_found` | `internal/transport/http/cloud_peer.go` |
| `peer_receipt_unavailable` | `internal/transport/http/cloud_peer.go` |
| `peer_request_missing` | `internal/transport/http/cloud_peer.go`, `internal/transport/http/cloud_remote_discovery.go` |
| `peer_unavailable` | `internal/transport/http/cloud_peer.go` |
| `persona_catalog_unavailable` | `internal/transport/http/work_v2_persona_suggestion.go` |
| `persona_suggester_out_of_quota` | `internal/transport/http/work_v2_persona_suggestion.go` |
| `persona_suggestion_failed` | `internal/transport/http/work_v2_persona_suggestion.go` |
| `phase_not_editable` | `internal/transport/http/work_v2.go` |
| `plan_failed` | `internal/transport/http/intent.go` |
| `policy_changed` | `internal/transport/http/dispatch_policy_settings.go` |
| `policy_unreadable` | `internal/transport/http/dispatch_policy_settings.go` |
| `policy_write_failed` | `internal/transport/http/dispatch_policy_settings.go` |
| `project_folder_unavailable` | `internal/transport/http/project_folders.go` |
| `project_folder_unreadable` | `internal/transport/http/project_folders.go` |
| `project_not_found` | `internal/transport/http/antigravity_project_consent.go`, `internal/transport/http/graphify.go`, `internal/transport/http/project_files.go`, `internal/transport/http/project_memory.go`, `internal/transport/http/project_tree.go`, `internal/transport/http/project_unify.go`, `internal/transport/http/tracks.go`, `internal/transport/http/work_v2.go`, `internal/transport/http/work_v2_agent.go` |
| `project_not_offered` | `internal/transport/http/project_sync.go` |
| `project_registry_failed` | `internal/transport/http/projects.go` |
| `project_required` | `internal/domain/work/v2.go`, `internal/transport/http/squad_skill_sources.go` |
| `project_sync_unavailable` | `internal/transport/http/project_sync.go` |
| `project_unreadable` | `internal/transport/http/project_sync.go` |
| `proposal_is_for_sessions` | `internal/transport/http/proposals.go` |
| `rate_limited` | `internal/transport/http/auth.go`, `internal/transport/http/snippets.go` |
| `receipt_expired` | `internal/transport/http/receipts.go`, `internal/transport/http/work.go` |
| `receipt_unavailable` | `internal/transport/http/secret_handoff.go` |
| `receipt_update_failed` | `internal/transport/http/work_v2_agent.go` |
| `receipts_full` | `internal/transport/http/receipts.go`, `internal/transport/http/work.go` |
| `remote_ack_refused` | `internal/transport/http/cloud_remote_routes.go` |
| `remote_discovery_unavailable` | `internal/transport/http/cloud_remote_discovery.go` |
| `remote_dispatch_refused` | `internal/transport/http/cloud_remote_routes.go` |
| `remote_event_refused` | `internal/transport/http/cloud_remote_routes.go` |
| `remote_finish_missing` | `internal/transport/http/cloud_remote_routes.go` |
| `remote_finish_refused` | `internal/transport/http/cloud_remote_routes.go` |
| `remote_inventory_invalid` | `internal/transport/http/cloud_remote_discovery.go` |
| `remote_observation_unavailable` | `internal/transport/http/cloud_remote_routes.go` |
| `remote_source_unverified` | `internal/transport/http/cloud_remote_discovery.go`, `internal/transport/http/cloud_remote_routes.go` |
| `remote_store_unavailable` | `internal/transport/http/cloud_remote_discovery.go`, `internal/transport/http/cloud_remote_routes.go` |
| `remote_task_missing` | `internal/transport/http/cloud_remote_routes.go` |
| `remote_task_state_changed` | `internal/transport/http/cloud_remote_routes.go` |
| `remote_unavailable` | `internal/transport/http/cloud_remote_routes.go` |
| `report_local_only` | `internal/transport/http/reports.go` |
| `report_not_found` | `internal/transport/http/reports.go` |
| `report_not_over_cloud` | `internal/transport/http/reports.go` |
| `request_in_progress` | `internal/transport/http/receipts.go`, `internal/transport/http/work.go` |
| `request_outcome_unknown` | `internal/transport/http/receipts.go`, `internal/transport/http/work.go` |
| `reset_failed` | `internal/transport/http/work_v2.go` |
| `restore_batch_too_large` | `internal/transport/http/session_restore.go` |
| `review_required_not_applicable` | `internal/domain/work/v2.go` |
| `role_draft_failed` | `internal/transport/http/squad_role_draft.go` |
| `role_drafter_out_of_quota` | `internal/transport/http/squad_role_draft.go` |
| `scope_mismatch` | `internal/transport/http/squad.go` |
| `secret_empty` | `internal/transport/http/secret_handoff.go` |
| `secret_handoff_callback_invalid` | `internal/transport/http/secret_handoff.go` |
| `secret_handoff_delivery_failed` | `internal/transport/http/secret_handoff.go` |
| `secret_handoff_target_changed` | `internal/transport/http/secret_handoff.go` |
| `secret_handoff_unavailable` | `internal/transport/http/secret_handoff.go` |
| `secret_handoffs_full` | `internal/transport/http/secret_handoff.go` |
| `secret_too_large` | `internal/transport/http/secret_handoff.go` |
| `send_required` | `internal/transport/http/projects.go` |
| `session_actor_required` | `internal/transport/http/squad_actor.go`, `internal/transport/http/squad_events.go` |
| `session_cannot_create_item` | `internal/transport/http/work_v2.go` |
| `session_cannot_decide` | `internal/transport/http/work.go` |
| `session_has_no_record` | `internal/transport/http/images.go` |
| `session_not_found` | `internal/transport/http/actions.go`, `internal/transport/http/images.go` |
| `session_required` | `internal/transport/http/proposals.go`, `internal/transport/http/work_v2_session_name.go` |
| `session_unavailable` | `internal/transport/http/work_v2_session_todos.go` |
| `session_unresolved` | `internal/transport/http/human_interventions.go`, `internal/transport/http/work_v2_agent.go` |
| `settings_dir_refused` | `internal/transport/http/settings.go` |
| `settings_file_invalid` | `internal/transport/http/settings.go` |
| `settings_unavailable` | `internal/transport/http/settings.go` |
| `skill_folder_export_unsupported` | `internal/transport/http/squad_packages.go` |
| `skill_source_changed` | `internal/transport/http/squad_skill_sources.go` |
| `skill_source_missing` | `internal/transport/http/squad_skill_sources.go` |
| `skill_unreadable` | `internal/transport/http/skill_install.go` |
| `snapshot_not_found` | `internal/transport/http/squad.go` |
| `squad_auto_dispatch_disabled` | `internal/transport/http/squad.go` |
| `store_busy` | `internal/transport/http/receipts.go`, `internal/transport/http/verify.go` |
| `store_unavailable` | `internal/transport/http/auth.go`, `internal/transport/http/gate.go`, `internal/transport/http/images.go`, `internal/transport/http/push.go`, `internal/transport/http/receipts.go`, `internal/transport/http/runs.go`, `internal/transport/http/session_archive.go`, `internal/transport/http/session_restore.go`, `internal/transport/http/squad_actor.go`, `internal/transport/http/squad_events.go`, `internal/transport/http/usage.go`, `internal/transport/http/verify.go`, `internal/transport/http/work.go`, `internal/transport/http/work_v2.go`, `internal/transport/http/work_v2_reference_images.go` |
| `store_unreadable` | `internal/transport/http/obligations.go`, `internal/transport/http/schedules.go`, `internal/transport/http/snippets.go`, `internal/transport/http/squad.go`, `internal/transport/http/tasks.go` |
| `subscriptions_full` | `internal/transport/http/push.go` |
| `target_session_ambiguous` | `internal/transport/http/human_interventions.go` |
| `target_session_unavailable` | `internal/transport/http/human_interventions.go` |
| `title_not_saved` | `internal/transport/http/actions.go`, `internal/transport/http/session_smart_title.go` |
| `title_required` | `internal/domain/work/v2.go` |
| `too_large` | `internal/transport/http/actions.go`, `internal/transport/http/voice.go` |
| `tool_image_unavailable` | `internal/transport/http/transcript_tools.go` |
| `tool_result_stale` | `internal/transport/http/transcript_details.go`, `internal/transport/http/transcript_tools.go` |
| `transcript_cursor_stale` | `internal/transport/http/transcript.go` |
| `unauthorized` | `internal/transport/http/auth.go`, `internal/transport/http/gate.go`, `internal/transport/http/proposals.go`, `internal/transport/http/push.go`, `internal/transport/http/skill_install.go`, `internal/transport/http/terminals.go` |
| `unknown_definition` | `internal/transport/http/squad.go` |
| `unknown_item` | `internal/transport/http/usage.go` |
| `unknown_pairing` | `internal/transport/http/cloud.go`, `internal/transport/http/cloud_identity_pairing.go` |
| `unknown_project` | `internal/transport/http/squad.go`, `internal/transport/http/squad_packages.go`, `internal/transport/http/squad_skill_sources.go`, `internal/transport/http/usage_overview.go` |
| `unknown_reference` | `internal/transport/http/squad.go` |
| `unknown_session` | `internal/transport/http/usage.go` |
| `unknown_task` | `internal/transport/http/usage.go` |
| `unsupported_image` | `internal/transport/http/work_v2_reference_images.go`, `internal/transport/http/work_v2_session_todos.go` |
| `unsupported_media_type` | `internal/transport/http/dispatch_policy_settings.go`, `internal/transport/http/gate.go`, `internal/transport/http/integration_settings.go`, `internal/transport/http/notification_rules.go`, `internal/transport/http/settings.go` |
| `update_failed` | `internal/transport/http/update.go` |
| `verification_gate_on` | `internal/domain/work/v2.go` |
| `version_conflict` | `internal/transport/http/work_v2_persona_suggestion.go` |
| `via_is_for_sessions` | `internal/transport/http/work.go` |
| `video_range_invalid` | `internal/transport/http/videos.go` |
| `viewer_not_pinned` | `internal/transport/http/cloud.go` |
| `viewer_scope_applied_cleanup_failed` | `internal/transport/http/cloud.go` |
| `voice_model_abandoned` | `internal/transport/http/voice_setup.go` |
| `voice_model_busy` | `internal/transport/http/voice_setup.go` |
| `voice_model_install_failed` | `internal/transport/http/voice_setup.go` |
| `worktree_lifecycle_busy` | `internal/transport/http/projects.go` |
| `worktree_lifecycle_failed` | `internal/transport/http/projects.go` |
| `worktree_remove_failed` | `internal/transport/http/projects.go` |
| `write_failed` | `internal/transport/http/snippets.go`, `internal/transport/http/squad.go` |
