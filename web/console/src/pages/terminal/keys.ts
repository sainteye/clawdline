import type { NextWord } from "../../next-strings.js"

/**
 * The terminal page's keyboard and status-line rules, with no DOM in them so
 * each can be driven by a test (keys.test.ts).
 */

/**
 * The key that leaves the terminal for its header, and comes back: F6, the
 * browser's own "move between regions" key. It is never sent to the program,
 * so a program that uses F6 (`mc`) cannot have it here; everything else the
 * keyboard sends, Tab and Escape included, stays the program's.
 */
export function isRegionKey(ev: { key: string; altKey: boolean; ctrlKey: boolean; metaKey: boolean }): boolean {
  return ev.key === "F6" && !ev.altKey && !ev.ctrlKey && !ev.metaKey
}

/** Let xterm's textarea handle program keys, then contain browser page shortcuts. */
export function bindTerminalKeyboard(element: HTMLElement, onRegion: () => void): () => void {
  const region = (event: KeyboardEvent) => {
    if (!isRegionKey(event)) return
    event.preventDefault()
    event.stopPropagation()
    onRegion()
  }
  const contain = (event: KeyboardEvent) => event.stopPropagation()
  element.addEventListener("keydown", region, true)
  element.addEventListener("keydown", contain)
  return () => {
    element.removeEventListener("keydown", region, true)
    element.removeEventListener("keydown", contain)
  }
}

/**
 * What the key row's Ctrl does to the next input. Ctrl applies to one key: a
 * single character that has a control form becomes it; anything else — a
 * character without one, several characters an IME committed at once, or a
 * special key from the row (Esc, Tab, an arrow) — goes as it is. Either way
 * Ctrl is let go, so it never lingers onto a key the person did not mean.
 */
export function withCtrl(armed: boolean, data: string): { out: string; armed: false } {
  if (!armed || data.length !== 1) return { out: data, armed: false }
  const code = data.toUpperCase().charCodeAt(0)
  return { out: code >= 0x40 && code <= 0x5f ? String.fromCharCode(code & 0x1f) : data, armed: false }
}

/** Labels and values shared by the local and hosted phone key rows. */
export const KEY_ROW: [string, string, NextWord | null][] = [
  ["Esc", "\x1b", null], ["Ctrl", "ctrl", "terminalKeyCtrl"], ["Tab", "\t", null],
  ["←", "D", "terminalKeyLeft"], ["↑", "A", "terminalKeyUp"], ["↓", "B", "terminalKeyDown"], ["→", "C", "terminalKeyRight"],
]

/** Why a paste was not sent, or null when it was: a paste is never dropped in silence. */
export function pasteRefusal(state: { holding: boolean; stale: boolean } | null): NextWord | null {
  if (!state?.holding) return "terminalPasteWatching"
  if (state.stale) return "terminalPasteStale"
  return null
}

/**
 * Touch devices open a native paste target in the original gesture, without waiting for a
 * clipboard permission prompt. Desktop reads still recheck authority before sending.
 */
export async function readClipboardPaste(
  clipboard: Pick<Clipboard, "readText"> | undefined,
  refusal: () => NextWord | null,
  send: (text: string) => void | Promise<void>,
  native?: { preferred: boolean; open: () => void },
  observe: (stage: string) => void = () => {},
): Promise<NextWord | null> {
  const record = (stage: string) => { try { observe(stage) } catch { /* Diagnostics cannot affect pasting. */ } }
  const open = () => { native?.open(); record("native_opened") }
  const before = refusal()
  if (before) { record("refused"); return before }
  if (native?.preferred) { open(); return null }
  if (!clipboard?.readText) { record("read_unavailable"); if (native) open(); return "terminalPasteReadFailed" }
  record("read_started")
  let text: string
  try { text = await clipboard.readText() } catch {
    record("read_failed"); if (native) open(); return "terminalPasteReadFailed"
  }
  if (!text) { record("read_empty"); if (native) open(); return "terminalPasteEmpty" }
  const after = refusal()
  if (after) { record("refused"); return after }
  record("send_started")
  try { await send(text) } catch (failure) { record("send_failed"); throw failure }
  record("sent")
  return null
}

/** How long a sentence the page said about an action stays in the status line. */
export const SAID_MS = 8_000

interface Timer {
  setTimeout(fn: () => void, ms: number): unknown
  clearTimeout(handle: unknown): void
}

/**
 * The status line's own sentence (an action's failure, a paste not sent, who
 * holds control now): it goes away after `SAID_MS`, or as soon as control
 * changes hands, so a sentence about an earlier moment is never read next to
 * a later one.
 */
export class StatusMessage {
  private words = ""
  private timer: unknown = null
  private readonly clock: Timer
  private readonly onChange: (words: string) => void

  constructor(onChange: (words: string) => void, clock: Timer = globalThis) {
    this.onChange = onChange
    this.clock = clock
  }

  get text(): string {
    return this.words
  }

  say(words: string): void {
    this.stop()
    this.words = words
    this.onChange(words)
    if (words) {
      this.timer = this.clock.setTimeout(() => {
        this.timer = null
        this.words = ""
        this.onChange("")
      }, SAID_MS)
    }
  }

  clear(): void {
    if (!this.words && this.timer === null) return
    this.say("")
  }

  dispose(): void {
    this.stop()
  }

  private stop(): void {
    if (this.timer !== null) this.clock.clearTimeout(this.timer)
    this.timer = null
  }
}

/** Who holds control, compared as the status line announces a change of hands. */
export function holderKey(c: { held: boolean; holder?: { same_client: boolean; same_device: boolean; name: string } | null } | null): string {
  if (!c) return "?"
  if (!c.held || !c.holder) return "nobody"
  return c.holder.same_client ? "you" : c.holder.same_device ? "tab" : "device:" + c.holder.name
}

/**
 * The hosted page's key panel: each control combination is a key of its own, with what it does
 * said under it. ^D is marked apart: on an empty shell line it ends the shell, and with it the terminal.
 */
export const COMBO_KEYS: { name: string; value: string; hint: NextWord; danger?: boolean }[] = [
  { name: "^C", value: "\x03", hint: "terminalKeyHintInterrupt" },
  { name: "^Z", value: "\x1a", hint: "terminalKeyHintSuspend" },
  { name: "^L", value: "\x0c", hint: "terminalKeyHintClear" },
  { name: "^R", value: "\x12", hint: "terminalKeyHintSearch" },
  { name: "^A", value: "\x01", hint: "terminalKeyHintLineStart" },
  { name: "^E", value: "\x05", hint: "terminalKeyHintLineEnd" },
  { name: "^U", value: "\x15", hint: "terminalKeyHintDeleteLine" },
  { name: "^D", value: "\x04", hint: "terminalKeyHintEndShell", danger: true },
]
/** Keys without a hint: arrows go through `sendKey`, which picks the program's cursor mode. */
export const PANEL_KEYS: [string, string, NextWord | null][] = [
  ["Esc", "\x1b", null], ["Tab", "\t", null], ["⇧Tab", "\x1b[Z", "terminalKeyBackTab"], ["Enter", "\r", null],
  ["←", "D", "terminalKeyLeft"], ["↑", "A", "terminalKeyUp"], ["↓", "B", "terminalKeyDown"], ["→", "C", "terminalKeyRight"],
]
/** The row under the screen: the keys a phone reaches for most. */
export const QUICK_KEYS: [string, string, NextWord | null][] = [
  ["^C", "\x03", "terminalKeyHintInterrupt"], ["Esc", "\x1b", null], ["Tab", "\t", null],
  ["↑", "A", "terminalKeyUp"], ["↓", "B", "terminalKeyDown"],
]
