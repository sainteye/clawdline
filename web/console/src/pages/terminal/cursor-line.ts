import type { Terminal } from "@xterm/xterm"

/**
 * The screen is sized by whoever holds the terminal, so it can be taller than
 * its box. Focusing it, or the cursor moving to another row while it has the
 * focus, brings the cursor's row to the middle of the box rather than leaving
 * it wherever the browser's own caret scroll happens to put it, which used to
 * be nowhere until the first key.
 *
 * A row near the bottom of the screen can only reach the middle if there is
 * room under it, so a box whose screen overflows gets an empty tail of half
 * its height. A screen that fits gets no tail, and nothing moves.
 */
export function centerCursorLine(box: HTMLElement | null, term: Terminal | null): void {
  if (!box || !term || term.rows <= 0) return
  const screen = box.querySelector<HTMLElement>(".xterm-screen")
  if (!screen) return
  const boxRect = box.getBoundingClientRect()
  const screenRect = screen.getBoundingClientRect()
  const top = screenRect.top - boxRect.top + box.scrollTop
  const over = top + screenRect.height - box.clientHeight
  let tail = box.querySelector<HTMLElement>(":scope > .terminal-tail")
  if (over > 0) {
    if (!tail) {
      tail = document.createElement("div")
      tail.className = "terminal-tail"
      tail.setAttribute("aria-hidden", "true")
      box.append(tail)
    }
    tail.style.height = `${Math.ceil(over + box.clientHeight / 2)}px`
  } else if (tail) tail.remove()
  const row = screenRect.height / term.rows
  const line = top + (term.buffer.active.cursorY + 0.5) * row
  box.scrollTop = Math.max(0, line - box.clientHeight / 2)
}

/** Keeps the cursor's row centred while the screen has the focus. */
export function followCursorLine(box: () => HTMLElement | null, term: Terminal): () => void {
  let row = -1
  const center = () => requestAnimationFrame(() => {
    row = term.buffer.active.cursorY
    centerCursorLine(box(), term)
  })
  const focused = () => term.textarea !== undefined && document.activeElement === term.textarea
  const moved = term.onCursorMove(() => {
    if (focused() && term.buffer.active.cursorY !== row) center()
  })
  const textarea = term.textarea
  textarea?.addEventListener("focus", center)
  return () => {
    moved.dispose()
    textarea?.removeEventListener("focus", center)
  }
}
