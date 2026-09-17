import type { SettingsSnapshot } from "@clawdline/contract"

/**
 * What a key means when the file does not carry it.
 *
 * The Swift app's `Config` declarations, value for value, because a window that
 * showed a different default from the one the program uses would be a window
 * that lies about the state it is in. Two differ on purpose and are marked:
 * this app is not the app holding ⌥Space.
 */
export const DEFAULTS = {
  /** No default. The Swift app is running and owns option+space; a second app taking it
   *  breaks the one the person is using (shell/darwin/NextConfig.swift). */
  hotkey: "",
  scope_app: "com.googlecode.iterm2",
  language: "auto",
  mascot: "clawd",
  terminal: "auto",
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
  voice_settle_seconds: 1.8,
  voice_stop_seconds: 4.0,
  remote: false,
  remote_write: false,
  remote_tunnel: "off",
  remote_hostname: "",
  push_on_delivery: true,
  push_on_fanout: true,
  smart_notifications: false,
  push_on_deploy: false,
  orchestrator_agent_notify: true,
  orchestrator_enabled: true,
  orchestrator_max_children: 5,
  orchestrator_permission: "full",
  orchestrator_notify_root: true,
  orchestrator_child_linger: 180,
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
