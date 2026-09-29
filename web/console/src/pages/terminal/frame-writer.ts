import type { TerminalCursor, TerminalFrame, TerminalModes } from "@clawdline/contract"

/**
 * One frame as the bytes that draw it in the viewer's own terminal (plan v3
 * D2): a frame is the whole visible screen and replaces the one before, so it
 * is written over it in place.
 *
 * First the modes the program set, so a key pressed here encodes as the
 * program expects — DECCKM (`ESC[?1h`/`l`), the keypad (`ESC=`/`ESC>`), the
 * mouse reporting it asked for (1000, 1002 or 1003, and 1006) and the cursor's
 * shape (`ESC[N q`). Then every row: its position, `ESC[K` to clear what an
 * earlier, longer row left, and its text with its SGR — never a newline, which
 * at the bottom row would scroll the screen. The clear comes before the text,
 * not after it: a row that fills the last column leaves the cursor on that
 * column waiting to wrap, and `ESC[K` there would erase the row's last cell. Last, the cursor where the
 * program left it, shown or hidden as it was.
 *
 * The viewer's terminal is kept at the frame's own size before this is
 * written (TerminalView), so a row never wraps.
 */

const ESC = "\x1b"

/** DECSCUSR's parameter: 1-2 block, 3-4 underline, 5-6 bar; odd blinks. */
export function cursorStyle(cursor: Pick<TerminalCursor, "shape" | "blinking">): number {
  const base = cursor.shape === "underline" ? 3 : cursor.shape === "bar" ? 5 : 1
  return cursor.blinking === false ? base + 1 : base
}

export function modeBytes(modes: TerminalModes, cursor: Pick<TerminalCursor, "shape" | "blinking">): string {
  let out = ESC + (modes.app_cursor ? "[?1h" : "[?1l")
  out += ESC + (modes.app_keypad ? "=" : ">")
  // Only one mouse mode is on at a time: the others are turned off first.
  out += ESC + "[?1000l" + ESC + "[?1002l" + ESC + "[?1003l"
  if (modes.mouse === "standard") out += ESC + "[?1000h"
  else if (modes.mouse === "button") out += ESC + "[?1002h"
  else if (modes.mouse === "any") out += ESC + "[?1003h"
  out += ESC + (modes.mouse_sgr ? "[?1006h" : "[?1006l")
  out += ESC + "[" + cursorStyle(cursor) + " q"
  return out
}

export function frameBytes(frame: TerminalFrame): string {
  let out = ESC + "[?25l" + modeBytes(frame.modes, frame.cursor)
  const rows = Math.max(0, frame.rows)
  for (let r = 0; r < rows; r++) {
    out += ESC + "[" + (r + 1) + ";1H" + ESC + "[0m" + ESC + "[K" + (frame.lines[r] ?? "") + ESC + "[0m"
  }
  const y = Math.min(Math.max(0, frame.cursor.y), Math.max(0, rows - 1))
  const x = Math.min(Math.max(0, frame.cursor.x), Math.max(0, frame.cols - 1))
  out += ESC + "[" + (y + 1) + ";" + (x + 1) + "H"
  if (frame.cursor.visible) out += ESC + "[?25h"
  return out
}
