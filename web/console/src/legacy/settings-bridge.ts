// What the settings page needs from the copied modules, typed.
//
// `input/settings.js` is not copied: it binds listeners to elements it looks up
// at load, and here the elements are React's. Its rules are ported beside the
// page (pages/settings.tsx) and everything it reads or writes in the copied
// state goes through this file, so the one object both halves share has one
// way in. Root re-exports this from bridge.ts; until then the page imports it
// directly.
import { S, storeBool } from "./js/core/state.js"
import { Diagnostics } from "./js/core/layout-diagnostics.js"

type SettingsState = { assistantIcons: boolean; newestFirst: boolean; version: string }
const state = S as unknown as SettingsState

/** `S.assistantIcons`: this browser's own preference, read at load from storage. */
export const settingsAssistantIcons = (): boolean => !!state.assistantIcons

/** The Settings row's press: flip it and keep it for this browser (`clawdline.assistant-icons`). */
export function setSettingsAssistantIcons(on: boolean): void {
  state.assistantIcons = on
  storeBool("clawdline.assistant-icons", on)
}

/** `S.newestFirst`: which end the transcript reads from. Not stored, as there. */
export const settingsNewestFirst = (): boolean => !!state.newestFirst

/** `toggleOrder`'s state half (`input/keys.js`). */
export function setSettingsNewestFirst(on: boolean): void {
  state.newestFirst = on
}

/** `S.version`, the Mac's version as `/v1/health` carries it; empty when it does not. */
export const settingsMacVersion = (): string => String(state.version || "")

/**
 * `settingsBuildVersion` (`input/settings.js`): the Mac version is friendlier; the
 * immutable Cloud build keeps the line real before its first snapshot arrives.
 */
export function settingsBuildVersion(macVersion: string, cloud?: { build?: string } | null): string {
  return String(macVersion || (cloud && cloud.build) || "")
}

/**
 * `Diagnostics.reveal` (`core/layout-diagnostics.js`): the recorder's panel,
 * behind five presses on the version line.
 */
export const revealSettingsDiagnostics = (): void => {
  ;(Diagnostics as { reveal: () => void }).reveal()
}
