// The live screen's part of the bridge.
//
// `view/terminal.js` is not copied under the guard. It is the panel *and* its
// transport in one module, and it reaches the page through `core/dom.js` and
// `input/detail-actions.js`, which bind listeners at import — before React has
// drawn any of the ids they look up. So the half that is a pure function of a
// capture is restated here line for line (view/terminal.js lines 70–521), with
// the same regexes, the same thresholds and the same escaping, and
// `session/ScreenPanel.tsx` owns the state, the lease and the clicks. If
// `view/terminal.js` is ever copied, this goes and its exports are used.
//
// What is restated is the renderer and nothing else: `paintRows` decides
// nothing about the conversation. What tmux drew is what is drawn, and a line
// it cannot explain is a line it does not have to.
import type { Screen } from "@clawdline/contract"
import { RefusalError, TransportError, isRefusal } from "@clawdline/core"
import { client } from "../client.js"
import { esc } from "./js/core/esc.js"
import { T } from "./js/core/i18n.js"
import { createVisibleInterval } from "./js/core/visibility.js"

const e = esc as (s: unknown) => string
const S = T as Record<string, string>

/* ---- SGR, and the one other thing a status line is made of -----------------
   `capture-pane -e` re-serialises a grid, so what comes back is text and colour
   and no cursor motion at all. Anything that is not an SGR sequence is dropped
   rather than rendered.

   **OSC 8 is the exception, and it is one because a status line is largely made
   of it.** Claude Code writes its links that way, and a scanner matching ESC
   plus one byte ate `ESC ]` and then printed `8;id=…;https://…` as words. An
   OSC string runs until BEL or ST; the hyperlink inside it is drawn as a link
   rather than swallowed with the rest.
   -------------------------------------------------------------------------- */

// Four alternatives and their order matters: an SGR sequence is captured so its
// parameters can be read, an OSC string is captured so the hyperlink in it can
// be, and every other escape sequence is matched only so that it can be thrown
// away rather than printed. `\u001b` and `\u0007` are spelled out because an
// editor or a copy that ate a literal control byte would leave a regex that
// silently matches nothing — which reads exactly like a screen that happened to
// have no colour, or no links, in it.
const CSI =
  /\u001b\[([0-9;:]*)m|\u001b\]((?:[^\u0007\u001b]|\u001b(?!\\))*)(?:\u0007|\u001b\\)?|\u001b\[[0-9;:?]*[ -\/]*[@-~]|\u001b[@-Z\\-_]/g

// Control bytes that survived tmux's own serialisation are not content. A
// carriage return in particular would make a line look complete and then be
// drawn on top of itself.
const CONTROL = /[\u0000-\u0008\u000b-\u001f\u007f]/g

/**
 * The URLs this panel is willing to turn into a link, which is two schemes and
 * no others.
 *
 * **A link that silently fails is worse than the words it replaced.** A status
 * line carries `file:///…` and `x-github-client://…`; neither does anything in
 * a phone browser, and a tappable label that does nothing is a worse reading of
 * the screen than the label alone. Every other scheme keeps its text and gets
 * no anchor — which is also, and not by coincidence, what refuses `javascript:`
 * and `data:`: the rule is an allowlist of what works, not a blocklist of what
 * is known to be dangerous.
 *
 * **The character class is the second wall and not the first.** `esc()` is what
 * makes the attribute safe. This refuses to draw a link at all out of a URL
 * carrying a quote, an angle bracket or a space — characters a real URL
 * percent-encodes and an attack does not.
 */
const LINKABLE = /^https?:\/\/[^\s"'<>`\\^{}|\u0000-\u001f\u007f]+$/i

interface SGRState {
  fg: string | null
  bg: string | null
  bold: boolean
  dim: boolean
  italic: boolean
  underline: boolean
  inverse: boolean
  href: string | null
}

/**
 * What one OSC string means to what is drawn, which for all but one of them is
 * nothing. `8;params;URI` opens a hyperlink and `8;;` closes it.
 *
 * **An OSC 8 this cannot read closes rather than opens.** `8;params` with no
 * second semicolon is malformed; treating it as a close means a capture can
 * lose a link it should have had, and treating it as an open means a capture
 * can invent one. Losing a link is the cheaper mistake, and the only one of the
 * two that cannot put an attribute on the page.
 */
function osc(state: SGRState, body: string): void {
  if (body !== "8" && body.slice(0, 2) !== "8;") return
  const rest = body.slice(2)
  const cut = rest.indexOf(";")
  if (cut < 0) {
    state.href = null
    return
  }
  // The URI is everything after the second semicolon, because a URI may contain one.
  const url = rest.slice(cut + 1)
  state.href = LINKABLE.test(url) ? url : null
}

/**
 * The opening tag of a link, and the only place here that writes one.
 *
 * `target="_blank"` because this panel is somebody's session and navigating
 * away from it loses the screen they were reading; `rel="noopener noreferrer"`
 * because the page opened is named by a program running in somebody else's
 * terminal.
 */
function anchor(url: string): string {
  return '<a href="' + e(url) + '" target="_blank" rel="noopener noreferrer">'
}

function colour(n: number): string {
  // The sixteen are named against the stylesheet so the panel follows the
  // page's theme; 256-colour and true colour are computed, because a palette in
  // CSS for those would be 240 declarations nobody reads.
  if (n < 16) return "var(--term-" + n + ")"
  if (n < 232) {
    const c = n - 16
    const steps = [0, 95, 135, 175, 215, 255]
    return (
      "rgb(" +
      steps[Math.floor(c / 36) % 6] +
      "," +
      steps[Math.floor(c / 6) % 6] +
      "," +
      steps[c % 6] +
      ")"
    )
  }
  const grey = 8 + (n - 232) * 10
  return "rgb(" + grey + "," + grey + "," + grey + ")"
}

function style(state: SGRState): string {
  const css: string[] = []
  let fg = state.inverse ? state.bg : state.fg
  let bg = state.inverse ? state.fg : state.bg
  if (state.inverse && !state.fg) bg = "var(--term-fg)"
  if (state.inverse && !state.bg) fg = "var(--term-bg)"
  if (fg) css.push("color:" + fg)
  if (bg) css.push("background:" + bg)
  if (state.bold) css.push("font-weight:600")
  if (state.dim) css.push("opacity:.6")
  if (state.italic) css.push("font-style:italic")
  if (state.underline) css.push("text-decoration:underline")
  return css.join(";")
}

function apply(state: SGRState, params: string): void {
  const codes =
    params === ""
      ? [0]
      : params.split(/[;:]/).map((p) => (p === "" ? 0 : parseInt(p, 10)))
  for (let i = 0; i < codes.length; i += 1) {
    const c = codes[i]
    if (c === 0) {
      state.fg = null
      state.bg = null
      state.bold = false
      state.dim = false
      state.italic = false
      state.underline = false
      state.inverse = false
    } else if (c === 1) state.bold = true
    else if (c === 2) state.dim = true
    else if (c === 3) state.italic = true
    else if (c === 4) state.underline = true
    else if (c === 7) state.inverse = true
    else if (c === 22) {
      state.bold = false
      state.dim = false
    } else if (c === 23) state.italic = false
    else if (c === 24) state.underline = false
    else if (c === 27) state.inverse = false
    else if (c >= 30 && c <= 37) state.fg = colour(c - 30)
    else if (c >= 90 && c <= 97) state.fg = colour(c - 90 + 8)
    else if (c >= 40 && c <= 47) state.bg = colour(c - 40)
    else if (c >= 100 && c <= 107) state.bg = colour(c - 100 + 8)
    else if (c === 39) state.fg = null
    else if (c === 49) state.bg = null
    else if (c === 38 || c === 48) {
      const into = c === 38 ? "fg" : "bg"
      if (codes[i + 1] === 5) {
        state[into] = colour(codes[i + 2] | 0)
        i += 2
      } else if (codes[i + 1] === 2) {
        state[into] =
          "rgb(" + (codes[i + 2] | 0) + "," + (codes[i + 3] | 0) + "," + (codes[i + 4] | 0) + ")"
        i += 4
      }
    }
  }
}

/**
 * One run of text at one colour, as HTML — the only place here that turns a
 * capture into markup, so there is one line to read when somebody asks whether
 * the escaping happens before the markup is built.
 *
 * `esc(css)` cannot fire today and is kept anyway: every value `style()` can
 * produce is a `var(--term-N)`, an `rgb()` of integers forced through `| 0`, or
 * a literal from this file, so no capture can reach it. It is the guard for the
 * day somebody adds an SGR code whose parameter reaches the declaration.
 */
function paintSegment(text: string, css: string): string {
  const body = e(text)
  if (!body) return ""
  return css ? '<span style="' + e(css) + '">' + body + "</span>" : body
}

/* ---- the layout: the capture, laid out for a phone -------------------------
   Measured across five live panes: every pane is 243 columns wide, and the
   trailing padding `-J` preserves is most of what arrives — a mean row of 149.8
   display columns against 62.3 once it is stripped. Once it is gone only 23 of
   59 rows are wider than a phone, so wrapping turns 59 rows into about 113
   rather than into a four-fold wall of text.

   Nothing here changes what is captured. This is one client-side decision about
   how to lay 243 columns out on a viewport that has fifty, and there is no
   control to change it: a preference would be a promise to keep two renderers
   alive for ever.
   -------------------------------------------------------------------------- */

// Twelve columns of hanging indent, and no more. In those same five panes every
// real leading indent was 0, 2, 3, 4 or 9 columns — and two rows were
// right-aligned status lines whose leading run was 203 and 238 columns. Twelve
// keeps every genuine indent exactly where the Mac put it, and stops the
// right-aligned two from leaving a two-column reading channel.
const INDENT_CAP = 12

// A row that is essentially a horizontal rule. Wrapped, one becomes five rows
// of dashes, which is worse than the sideways scroll it replaced — so these
// keep `white-space: pre` and are clipped at the edge instead.
//
// **A share of the row, not an exact match.** A long rule with a label buried
// in it is still a rule: twenty box dashes, ` 3 lines `, twenty more scores
// 0.87 and counts. `│ path │ 12 │` is a table row and scores 0.15. Across those
// five panes the two populations do not overlap at all, so 0.8 has a wide
// margin on both sides of it and is not a number tuned against one screen.
//
// Box drawing only, deliberately: a row of ASCII hyphens is a rule too, but so
// is `-- 3 --` and so is a diff's `--- a/file`, and no share threshold
// separates those from each other.
const BOX = /[─-╿]/g
const RULE_SHARE = 0.8
// Below this a row is too short to be anything, whatever it is made of: a lone
// `│` is 1.00 box drawing and is a table's left edge, not a rule.
const RULE_FLOOR = 8

/**
 * How many cells of the grid a string occupies.
 *
 * **Cells, not characters.** The indent below is written in `ch`, which is a
 * cell, and East Asian Wide and Fullwidth glyphs take two of them — so counting
 * `.length` here would under-indent every row of Chinese and be invisible in
 * every row of English. Ambiguous-width characters — box drawing among them —
 * count as one, which is what a terminal gives them.
 *
 * **This is not a general width function, and the difference is the whole of
 * what makes it safe.** Its only caller is `indentOf`, which hands it a row's
 * leading whitespace — and in a grid tmux has already laid out that population
 * is U+0020 and U+3000, both of which the ranges below get right. A row of
 * *content* would not be: emoji and the symbol blocks come back as one cell
 * where a terminal gives them two, and ZWJ sequences worse. The day somebody
 * measures a whole row with this, those are the ranges to add.
 */
function wideAt(code: number): boolean {
  return (
    (code >= 0x1100 && code <= 0x115f) ||
    (code >= 0x2e80 && code <= 0x303e) ||
    (code >= 0x3041 && code <= 0x33ff) ||
    (code >= 0x3400 && code <= 0x4dbf) ||
    (code >= 0x4e00 && code <= 0x9fff) ||
    (code >= 0xa000 && code <= 0xa4cf) ||
    (code >= 0xa960 && code <= 0xa97f) ||
    (code >= 0xac00 && code <= 0xd7a3) ||
    (code >= 0xf900 && code <= 0xfaff) ||
    (code >= 0xfe10 && code <= 0xfe19) ||
    (code >= 0xfe30 && code <= 0xfe6f) ||
    (code >= 0xff00 && code <= 0xff60) ||
    (code >= 0xffe0 && code <= 0xffe6) ||
    (code >= 0x1f300 && code <= 0x1f64f) ||
    (code >= 0x1f900 && code <= 0x1f9ff) ||
    (code >= 0x20000 && code <= 0x3fffd)
  )
}

export function columns(text: string): number {
  let total = 0
  for (let i = 0; i < text.length; i += 1) {
    let code = text.charCodeAt(i)
    // A surrogate pair is one character on the grid and two units in this
    // string, so read past it. Not reading past it does not double a wide
    // glyph — neither half is in any range above — but it breaks every
    // supplementary character that is *narrow*.
    if (code >= 0xd800 && code <= 0xdbff && i + 1 < text.length) {
      const low = text.charCodeAt(i + 1)
      if (low >= 0xdc00 && low <= 0xdfff) {
        code = (code - 0xd800) * 0x400 + (low - 0xdc00) + 0x10000
        i += 1
      }
    }
    total += wideAt(code) ? 2 : 1
  }
  return total
}

interface Run {
  text: string
  css: string
  href: string | null
}

/**
 * One walk of the capture, emitting one row per screen line.
 *
 * **The state object outlives the row, and that is the whole reason this is one
 * walk rather than one walk per row.** tmux opens a colour once and lets it run
 * to wherever it is closed, which is routinely several rows later; splitting
 * the source on newlines first and painting each row on its own would rebuild
 * the state at every boundary and lose the colour of every row after the first.
 *
 * Each row comes back as its runs rather than as markup, because the two things
 * done to a row next — stripping its padding and measuring its indent — are
 * done to text and not to HTML.
 */
function segments(text: string | null | undefined): Run[][] {
  const source = String(text == null ? "" : text)
  if (!source) return []
  const state: SGRState = {
    fg: null,
    bg: null,
    bold: false,
    dim: false,
    italic: false,
    underline: false,
    inverse: false,
    href: null,
  }
  const all: Run[][] = []
  let row: Run[] = []
  // Whether the text seen so far ended on a row boundary. `capture-pane`
  // **terminates** the last row rather than separating rows, so the final
  // newline is punctuation and not a row of the grid. Read from the stripped
  // text rather than from the source: a control byte after the newline is
  // dropped and must not make an already-terminated row look unfinished.
  let ended = false
  const take = (chunk: string) => {
    const visible = chunk.replace(CONTROL, "")
    const parts = visible.split("\n")
    for (let i = 0; i < parts.length; i += 1) {
      if (i > 0) {
        all.push(row)
        row = []
      }
      if (parts[i]) row.push({ text: parts[i], css: style(state), href: state.href })
    }
    if (visible) ended = visible.charAt(visible.length - 1) === "\n"
  }
  let last = 0
  let found: RegExpExecArray | null
  CSI.lastIndex = 0
  while ((found = CSI.exec(source)) !== null) {
    if (found.index > last) take(source.slice(last, found.index))
    last = found.index + found[0].length
    if (found[1] !== undefined) apply(state, found[1])
    else if (found[2] !== undefined) osc(state, found[2])
    if (found[0].length === 0) CSI.lastIndex += 1
  }
  if (last < source.length) take(source.slice(last))
  // A blank row **inside** the grid is a real row and keeps its height — that
  // is what the stylesheet's `min-height` is for. The only row refused here is
  // the one the terminating newline would have invented after the last real one.
  if (row.length || !ended) all.push(row)
  return all
}

/**
 * The trailing padding, dropped.
 *
 * `capture-pane -J` preserves it and it is about seventy per cent of what
 * crosses the wire. Kept, it would trail blank continuation rows behind every
 * short line, which is the exact thing the wrapping exists to remove.
 *
 * **What it costs, said here rather than left to be discovered:** a trailing run
 * that carried a background colour loses its block, so a highlight that ran to
 * the right margin now stops at the last visible character.
 */
function unpad(row: Run[]): Run[] {
  const out = row.slice()
  while (out.length) {
    const last = out[out.length - 1]
    const kept = last.text.replace(/\s+$/, "")
    if (kept === last.text) break
    if (kept) {
      out[out.length - 1] = { text: kept, css: last.css, href: last.href }
      break
    }
    out.pop()
  }
  return out
}

/**
 * How far in this row starts, in cells, capped.
 *
 * The leading whitespace and nothing more: a marker-aware indent would need a
 * taxonomy of markers this file does not have, and a wrong one moves text that
 * was already aligned.
 */
function indentOf(row: Run[]): number {
  let lead = ""
  for (let i = 0; i < row.length; i += 1) {
    // `\s` and not `[ \t]`: the ideographic space U+3000 is whitespace, is two
    // cells wide, and is how CJK text is indented — the one place where the
    // difference between counting characters and counting cells is not
    // theoretical.
    const run = /^\s*/.exec(row[i].text)![0]
    lead += run
    if (run.length < row[i].text.length) break
  }
  return Math.min(INDENT_CAP, columns(lead))
}

function isRule(plain: string): boolean {
  const solid = plain.replace(/\s+/g, "")
  if (solid.length < RULE_FLOOR) return false
  const box = solid.match(BOX)
  return (box ? box.length : 0) / solid.length >= RULE_SHARE
}

/**
 * A captured screen as one element per row, each hanging under its own indent.
 *
 * Escaped first and wrapped in spans afterwards, because the content is chosen
 * by whatever program somebody else is running. The two attributes this writes
 * that the capture can reach at all are the indent, which is a number this file
 * computed, and a link's `href`, which passed `LINKABLE` and then `esc()`.
 *
 * **A link never straddles a row.** Rows are elements here rather than
 * newlines, so an anchor left open across one would be a tag closed by the
 * wrong `</div>` — markup the browser then repairs in whatever way it likes.
 */
export function paintRows(text: string | null | undefined): string {
  const all = segments(text)
  let out = ""
  for (let i = 0; i < all.length; i += 1) {
    const row = unpad(all[i])
    let plain = ""
    let body = ""
    let open: string | null = null
    for (let j = 0; j < row.length; j += 1) {
      plain += row[j].text
      const piece = paintSegment(row[j].text, row[j].css)
      if (!piece) continue
      if (row[j].href !== open) {
        if (open) body += "</a>"
        open = row[j].href
        if (open) body += anchor(open)
      }
      body += piece
    }
    if (open) body += "</a>"
    if (isRule(plain)) {
      out += '<div class="screen-row rule">' + body + "</div>"
      continue
    }
    const indent = indentOf(row)
    out +=
      indent > 0
        ? '<div class="screen-row" style="padding-left:' +
          indent +
          "ch;text-indent:-" +
          indent +
          'ch">' +
          body +
          "</div>"
        : '<div class="screen-row">' + body + "</div>"
  }
  return out
}

/**
 * The one line that says what this panel is: which terminal it is reading, and
 * whether that terminal can tell it when something changed (`badge`).
 *
 * The header says which backend it is looking at, always. On tmux a `pipe-pane`
 * signal makes this live within about four milliseconds of the pane moving; on
 * a backend with no such signal the same panel is a sample taken when somebody
 * asks. Those are very different things and drawing them identically is a
 * defect this repository already had.
 */
export function screenBadgeHTML(screen: Screen | null): string {
  if (!screen) return ""
  const backend = screen.backend === "tmux" ? "tmux" : "iTerm2"
  const live = screen.channel === "signalled"
  const word = live ? S.webScreenLive : S.webScreenOnDemand
  const lines = typeof screen.lines === "number" ? " · " + screen.lines : ""
  return (
    '<span class="screen-badge" data-channel="' +
    e(screen.channel || "") +
    '">' +
    e(backend) +
    " · " +
    e(word) +
    e(lines) +
    "</span>"
  )
}

/**
 * Ask for the screen, which is also how the page says it is still watching.
 *
 * **Reading is the subscription.** The daemon attaches its pipe because
 * somebody read, and takes it off when nobody has read for thirty seconds — so
 * the panel's keepalive is not a poll for content, it is the lease. A refusal
 * comes back as the daemon's typed error so the panel can choose its sentence
 * by the code; anything else is a transport failure, which is a different fact.
 */
export async function readScreen(id: string): Promise<Screen | null> {
  const path = "/v1/sessions/" + encodeURIComponent(id) + "/screen"
  let res: Response
  try {
    res = await fetch(client.url(path), { credentials: "same-origin" })
  } catch (err) {
    throw new TransportError(path + " could not be reached", err)
  }
  let parsed: unknown = null
  try {
    parsed = await res.json()
  } catch {
    parsed = null
  }
  if (!res.ok) {
    if (isRefusal(parsed)) throw new RefusalError(res.status, parsed, path)
    throw new TransportError(path + " answered " + res.status + " with no refusal in it")
  }
  return (parsed as { screen?: Screen } | null)?.screen ?? null
}

/** `api.focus`: bring one session's terminal to the front on the Mac. */
export async function askFocus(id: string): Promise<void> {
  const path = "/v1/sessions/" + encodeURIComponent(id) + "/focus"
  let res: Response
  try {
    res = await fetch(client.url(path), {
      method: "POST",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json" },
      body: "{}",
    })
  } catch (err) {
    throw new TransportError(path + " could not be reached", err)
  }
  if (res.ok) return
  let parsed: unknown = null
  try {
    parsed = await res.json()
  } catch {
    parsed = null
  }
  if (isRefusal(parsed)) throw new RefusalError(res.status, parsed, path)
  throw new TransportError(path + " answered " + res.status + " with no refusal in it")
}

/**
 * The copied `createVisibleInterval` (`core/visibility.js`), typed.
 *
 * A `setInterval` that does no work while the page is hidden — hidden means the
 * timer itself is gone, not a tick that returns early, because an armed
 * interval wakes a backgrounded phone just to find out it has nothing to do.
 * Coming back runs the work once if at least one interval elapsed, then arms
 * the timer again.
 */
export const visibleInterval = createVisibleInterval as (
  work: () => void,
  intervalMs: number,
  options?: { catchUp?: boolean },
) => { start(): void; stop(): void; running(): boolean }
