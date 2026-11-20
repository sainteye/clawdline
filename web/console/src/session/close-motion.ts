import "./close-motion.css"

const returnMotion = "session-close-return"

function reveal(pane: HTMLElement | null): void {
  if (!pane) return
  pane.getAnimations().find((motion) => motion.id === returnMotion)?.cancel()
  pane.animate([{ opacity: .78 }, { opacity: 1 }], {
    id: returnMotion, duration: 180, easing: "ease-out",
  })
}

function leave(row: HTMLElement): (() => void) | undefined {
  const rect = row.getBoundingClientRect()
  const scroll = row.closest(".list-scroll")?.getBoundingClientRect()
  if (!rect.width || !rect.height || getComputedStyle(row).visibility !== "visible" ||
      (scroll && (rect.top < scroll.top || rect.bottom > scroll.bottom))) return

  // The real row and its controls disappear immediately. Keep only a visual
  // copy outside the scroller while the other rows settle into their new places.
  const layer = document.createElement("ul")
  layer.className = "rows session-close-visual"
  layer.inert = true
  layer.setAttribute("aria-hidden", "true")
  Object.assign(layer.style, {
    top: `${rect.top}px`, left: `${rect.left}px`, width: `${rect.width}px`, height: `${rect.height}px`,
  })
  const copy = row.cloneNode(true) as HTMLElement
  for (const name of ["id", "data-id", "data-selection-key", "role", "tabindex"]) copy.removeAttribute(name)
  for (const child of copy.querySelectorAll("[id]")) child.removeAttribute("id")
  const originals = row.querySelectorAll("canvas")
  copy.querySelectorAll("canvas").forEach((canvas, index) => {
    const source = originals[index]
    if (source?.width && source.height) canvas.getContext("2d")?.drawImage(source, 0, 0)
  })
  layer.append(copy)
  const animation = layer.animate([
    { opacity: 1, transform: "none" },
    { opacity: 0, transform: "translateX(-10px) scale(.985)" },
  ], { duration: 180, easing: "ease-out" })
  document.body.append(layer)
  void animation.finished.then(() => layer.remove(), () => layer.remove())
  return () => { layer.remove(); animation.cancel() }
}

export function playSessionCloseMotion(keys: readonly string[], leavingDetail: boolean): (() => void) | undefined {
  if (window.matchMedia("(prefers-reduced-motion: reduce)").matches ||
      typeof HTMLElement.prototype.animate !== "function") return
  const app = document.querySelector<HTMLElement>("#app:not([hidden])")
  if (!app) return
  const list = app.querySelector<HTMLElement>(".pane-list")
  // Keep the phone's fixed transcript pane on its atomic visibility swap.
  // The list is already mounted and can fade in without moving the pane.
  if (leavingDetail && list && getComputedStyle(list).visibility === "hidden") {
    reveal(list)
    return
  }
  const selector = keys.map((key) => `li.row[data-selection-key="${CSS.escape(key)}"]`).join(",")
  const row = app.querySelector<HTMLElement>(selector)
  const cancel = row && leave(row)
  if (!cancel && leavingDetail) {
    requestAnimationFrame(() => {
      if (app.querySelector("#pane-detail .detail-identity-block[hidden]")) {
        reveal(app.querySelector("#pane-detail"))
      }
    })
  }
  return cancel || undefined
}
