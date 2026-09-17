/**
 * The native shell's half of the settings page, as the page sees it.
 *
 * In a browser there is no shell and none of this exists. Inside
 * shell/darwin the window is given three things:
 *
 * - `window.__clawdlineShell.settings.words`, set before the page's first
 *   script runs: the native settings window's own words, copied there from
 *   the Swift app's `Copy+Chinese.swift`. The console's catalog has no keys for
 *   them, because in the Swift app they were never on a web page.
 * - a `shellSettings` message handler, which is how the page asks: for the
 *   current reading, to record a combination, and — after this page has
 *   written the file through the daemon — to apply it. The last is the Swift
 *   app's `clawdlineConfigChanged`: the settings window writes, then tells the
 *   app, and the app re-applies the hotkey in one place.
 * - two window events carrying the shell's answers, because a message handler
 *   has no reply.
 */

/** The native words the shell hands over; every one is a `Copy+Chinese.swift` property. */
export interface ShellSettingsWords {
  /** `settingsHotkey` */
  hotkey: string
  /** `settingsRecording` */
  recording: string
  /** `settingsScope` */
  scope: string
  /** `settingsScopeGlobal` */
  scopeGlobal: string
  /** `settingsOff`, drawn for a hotkey that is not set: this app has no default one. */
  off: string
}

/** What the shell has actually applied, after its last reading of the file. */
export interface ShellSettingsState {
  /** The file's combination as the shell read it; empty for none. */
  hotkey: string
  /** `HotKey.display` of it. */
  display: string
  /** Whether the combination is registered right now. */
  registered: boolean
  /** `hotKeyFailedTitle` when a set combination could not be registered; otherwise empty. */
  failure: string
  /** The effective `scope_app`, the shell's default included. Empty is every app. */
  scopeApp: string
}

/** One answer to a recording: a combination, or that the recording ended without one. */
export interface ShellRecording {
  spec?: string
  display?: string
  cancelled?: boolean
}

export const SHELL_STATE_EVENT = "clawdline-shell-settings"
export const SHELL_RECORDING_EVENT = "clawdline-shell-hotkey"

type Handler = { postMessage: (body: unknown) => void }

declare global {
  interface Window {
    __clawdlineShell?: { settings?: { words?: Partial<ShellSettingsWords> } }
    webkit?: { messageHandlers?: Record<string, Handler | undefined> }
  }
}

function handler(): Handler | null {
  return window.webkit?.messageHandlers?.shellSettings ?? null
}

/**
 * The native words, or null outside the shell. All five must be there: a block
 * with a missing label would be a block with an invented one.
 */
export function shellSettingsWords(): ShellSettingsWords | null {
  if (!handler()) return null
  const w = window.__clawdlineShell?.settings?.words
  if (!w) return null
  const keys: (keyof ShellSettingsWords)[] = ["hotkey", "recording", "scope", "scopeGlobal", "off"]
  if (!keys.every((k) => typeof w[k] === "string" && w[k])) return null
  return w as ShellSettingsWords
}

/** Ask the shell for something. False when there is no shell to ask. */
export function askShell(kind: "state" | "record" | "stopRecording" | "changed"): boolean {
  const h = handler()
  if (!h) return false
  try {
    h.postMessage({ kind })
    return true
  } catch {
    return false
  }
}
