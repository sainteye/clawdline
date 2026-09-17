/**
 * What has been sent from the bar, and walking back through it.
 *
 * `Controller.handleArrow` and `Controller.submit`, rule for rule: the newest
 * is last, a repeat moves rather than duplicates, sixty are kept, and the
 * cursor counts back from the end. ↑ only starts walking from an empty box —
 * otherwise it is a caret key inside a message somebody is writing — and ↓ only
 * does anything once the walk has started, with one step past the newest
 * emptying the box.
 *
 * **Where it is kept differs, and has to.** The Swift app writes it into
 * `~/.config/clawdline/config.json` beside the hotkey; this file is a page, and
 * a page that wrote sixty of somebody's messages through the settings route
 * would be putting a transcript in a configuration file that the console shows.
 * So it lives in this origin's `localStorage`, which is per-browser, never
 * leaves the machine, and is not read by the daemon. The consequence is real
 * and is written down in `docs/cross-platform.md`: a bar opened in a different
 * webview — or after somebody cleared this one's data — starts with no history,
 * where the Swift panel's survives a reinstall.
 */

const KEY = "clawdline.bar.history"
/** `Config.shared.history = Array(hist.suffix(60))`. */
const KEPT = 60

function read(): string[] {
  try {
    const raw = window.localStorage.getItem(KEY)
    if (!raw) return []
    const parsed: unknown = JSON.parse(raw)
    if (!Array.isArray(parsed)) return []
    return parsed.filter((v): v is string => typeof v === "string")
  } catch {
    // A private window, blocked site data, or a value somebody else wrote.
    // No history is a working bar; a throw here is not.
    return []
  }
}

function write(list: string[]): void {
  try {
    window.localStorage.setItem(KEY, JSON.stringify(list))
  } catch {
    /* the walk still works for this summon; nothing else depends on it */
  }
}

/** `submit()`: the sent text goes on the end, and a repeat moves rather than doubles. */
export function remember(text: string): void {
  const list = read().filter((said) => said !== text)
  list.push(text)
  write(list.slice(-KEPT))
  historyBack.reset()
}

/**
 * The cursor `Controller` keeps beside the box: -1 is "not walking", 0 is the
 * newest, and it counts back from the end of the list.
 */
export const historyBack = {
  cursor: -1,

  /** `show()` and every send: the walk starts again from where the box is. */
  reset(): void {
    this.cursor = -1
  },

  /** ↑. Null means "this key is not mine": the caret should move instead. */
  older(box: string): string | null {
    const list = read()
    if (!list.length) return null
    if (box !== "" && this.cursor < 0) return null
    this.cursor = Math.min(list.length - 1, this.cursor + 1)
    return list[list.length - 1 - this.cursor]
  },

  /** ↓. Null until a walk has started; one step past the newest empties the box. */
  newer(): string | null {
    const list = read()
    if (!list.length) return null
    if (this.cursor < 0) return null
    this.cursor -= 1
    return this.cursor < 0 ? "" : list[list.length - 1 - this.cursor]
  },
}
