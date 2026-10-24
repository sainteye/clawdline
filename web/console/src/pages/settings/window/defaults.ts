import type { SettingsSnapshot } from "@clawdline/contract"

/**
 * What a key means when the file does not carry it.
 *
 * The Swift app's `Config` declarations, value for value, because a window that
 * showed a different default from the one the program uses would be a window
 * that lies about the state it is in.
 */
export const DEFAULTS = {
  /** The Swift app's default, now that it is retired and nothing else holds it. It
   *  applies only when the file names no hotkey; an empty string there is a deliberate
   *  "none" and is shown as off (shell/darwin/NextConfig.swift). */
  hotkey: "option+space",
  scope_app: "com.googlecode.iterm2",
  language: "auto",
  mascot: "clawd",
  terminal: "auto",
  antigravity_default_model: "",
  antigravity_default_effort: "",
  codex_default_model: "",
  codex_default_effort: "",
  claude_default_model: "",
  reopen_on_return: true,
  follow_target: true,
  codex_auto_name: false,
  auto_name_assistant: "codex",
  notch: true,
  y_fraction: 0.3,
  width: 720,
  card_opacity: 0.55,
  output_mode: "auto",
  output_height: 340,
  output_size: 11.5,
  output_font: "Menlo",
  backdrop: 0.5,
  output_newest_first: false,
  voice_engine: "auto",
  voice_language: "auto",
  voice_settle_seconds: 1.8,
  voice_stop_seconds: 4.0,
  remote: false,
  tailscale_hostname: "",
  push_on_delivery: true,
  push_on_fanout: true,
  smart_notifications: false,
  push_on_deploy: false,
  orchestrator_agent_notify: true,
  /** Polling preference inside an explicitly enabled integration. */
  iterm_scan: true,
  iterm_integration: false,
  session_auto_discovery: true,
  orchestrator_enabled: true,
  planning_gate: true,
  verify_gate: false,
  orchestrator_max_children: 5,
  orchestrator_permission: "full",
  orchestrator_notify_root: true,
  orchestrator_child_linger: 180,
  /** None: Claude Code compacts near its own window (orchestrator compact.go). */
  claude_auto_compact_window: 0,
} as const

export type SettingKey = keyof typeof DEFAULTS

/** A key's value as the file has it, or its default. */
export function reading<K extends SettingKey>(
  snapshot: SettingsSnapshot | null,
  key: K,
): (typeof DEFAULTS)[K] {
  const value = snapshot ? (snapshot as unknown as Record<string, unknown>)[key] : undefined
  if (value === undefined || value === null) return DEFAULTS[key]
  return value as (typeof DEFAULTS)[K]
}

/** New daemons always return this nullable key, even before consent is saved.
 * An older daemon omits it and must never be drawn as integration off. */
export function supportsITermIntegration(snapshot: Partial<Pick<SettingsSnapshot, "iterm_integration">> | null): boolean {
  return snapshot !== null && Object.hasOwn(snapshot, "iterm_integration")
}
