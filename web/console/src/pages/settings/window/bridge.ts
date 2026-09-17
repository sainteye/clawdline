/**
 * The settings window's half of the shell bridge.
 *
 * The window itself is this web page. Everything it draws — the tabs, the rows,
 * the words, which control a setting gets — is here, and everything it stores
 * goes through the daemon's `/v1/settings`. What is left for the shell is the
 * short list of things a web page on this machine cannot do at all:
 *
 * - see a key combination before the page does, so one can be recorded;
 * - say what an application is called and what its icon looks like, given a
 *   bundle identifier, and offer the ones that are open;
 * - open a file picker that returns an application;
 * - show a file in the machine's file manager;
 * - write the Claude Code hook into `~/.claude/settings.json`;
 * - read the machine for a list of monospaced faces, the installed mascot
 *   packs and whether whisper is installed;
 * - re-apply what was written, because the shell is what registered it.
 *
 * Nothing else goes over this. In particular the shell sends no sentences: a
 * reading comes across as a fact (`{kind: "noModel"}`) and the words for it are
 * in copy.ts, so the next platform's shell gets them for free.
 *
 * The transport is a shell's to choose. macOS uses a `WKScriptMessageHandler`
 * named `shellSettingsWindow`; anything else sets `window.__clawdlineShell.post`.
 * A message handler has no reply, so every answer comes back as a window event.
 * See docs/shell-bridge.md.
 */

/** One application, as the shell can name it and this page cannot. */
export interface ShellApp {
  /** The bundle identifier, which is what `scope_app` stores. */
  id: string
  /** What the machine calls it, or the id itself when nothing here is installed. */
  name: string
  /** A small icon as a data URL, or absent. */
  icon?: string
  /** True when no application with this id is installed — drawn plain, kept as written. */
  unresolved?: boolean
}

/** Whether whisper is installed, as a fact rather than a sentence. */
export interface ShellDictation {
  kind: "ready" | "noBinary" | "noModel"
  model?: string
}

/** What the Claude Code hook row can say, and whether the shell will act on it. */
export interface ShellHooks {
  /** False when this build has nothing that reads a hook's notes; the button is off. */
  supported: boolean
  /** Whether this app's own entries are in the settings file named by `path`. */
  installed: boolean
  /** Whether a note arrived within the day. */
  heard: boolean
  /** Whose file the button would write into. */
  path: string
}

/** Everything the shell knows and the page cannot ask anyone else for. */
export interface ShellState {
  /** The file's combination as the shell read it; empty for none. */
  hotkey: string
  /** That combination written the way this platform writes it. */
  display: string
  /** Whether it is registered right now. */
  registered: boolean
  /** True when a set combination could not be registered. The sentence for it is
   *  `hotkeyFailedTitle` in copy.ts: the shell reports the fact, this side has the words. */
  failed: boolean
  /** The effective `scope_app`, the shell's own default included. Empty is every app. */
  scopeApp: string
  /** The scope's identifiers, named and drawn. In `scopeApp` order. */
  apps: ShellApp[]
  /** What is open right now and not already in the scope. */
  runningApps: ShellApp[]
  /** Mascot packs this machine has. */
  mascots: string[]
  /** Monospaced families this machine has. */
  fonts: string[]
  dictation: ShellDictation
  /** Where the settings file is, with the home directory as `~`. */
  configPath: string
  hooks: ShellHooks
  /** `darwin`, `linux`, `windows` — for the report, not for a behaviour. */
  platform: string
}

/** One answer to a recording: a combination, or that it ended without one. */
export interface ShellRecording {
  spec?: string
  display?: string
  cancelled?: boolean
}

/** One answer to a file picker: an application, or that it was dismissed. */
export interface ShellChosenApp {
  id?: string
  cancelled?: boolean
}

export const STATE_EVENT = "clawdline-settings-state"
export const HOTKEY_EVENT = "clawdline-settings-hotkey"
export const APP_EVENT = "clawdline-settings-app"

/** What the page may ask the shell for. */
export type Ask =
  | { kind: "state" }
  | { kind: "record" }
  | { kind: "stopRecording" }
  | { kind: "changed" }
  | { kind: "chooseApp" }
  | { kind: "reveal"; what: "config" | "hooks" }
  | { kind: "hooks"; install: boolean }
  | { kind: "close" }

type Post = (body: unknown) => void

/**
 * The shell's whole surface on `window`, declared once for the console and the
 * settings window both. The console's own settings page reads `settings.words`
 * (pages/settings/shell.ts) and predates this file; a second declaration of the
 * same property would have to match this one exactly, so there is only one.
 */
declare global {
  interface Window {
    __clawdlineShell?: {
      /** The console settings page's shell-only block, set before its first script runs. */
      settings?: { words?: Record<string, string> }
      /** A shell that is not macOS puts its transport here. */
      post?: Post
      /** Set by a shell that opened the settings window. */
      settingsWindow?: { platform?: string }
    }
    webkit?: { messageHandlers?: Record<string, { postMessage: (body: unknown) => void } | undefined> }
  }
}

function post(): Post | null {
  const own = window.__clawdlineShell?.post
  if (typeof own === "function") return own
  const handler = window.webkit?.messageHandlers?.shellSettingsWindow
  if (handler) return (body: unknown) => handler.postMessage(body)
  return null
}

/**
 * Whether there is a shell at all.
 *
 * In a browser there is not, and the window still draws: every row that is only
 * a value in the file works, and the handful that need the machine say so
 * rather than pretending. That is also what the next platform's shell sees on
 * the day before it is written.
 */
export function inShell(): boolean {
  return post() !== null
}

/** Ask the shell for something. False when there is no shell to ask. */
export function ask(message: Ask): boolean {
  const send = post()
  if (!send) return false
  try {
    send(message)
    return true
  } catch {
    return false
  }
}

/** Listen for one of the shell's answers. Returns the undo. */
export function listen<T>(event: string, handle: (detail: T) => void): () => void {
  const onEvent = (ev: Event) => {
    const detail = (ev as CustomEvent<T>).detail
    if (detail) handle(detail)
  }
  window.addEventListener(event, onEvent)
  return () => window.removeEventListener(event, onEvent)
}
