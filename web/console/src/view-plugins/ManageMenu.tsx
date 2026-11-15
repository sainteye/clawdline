import { useEffect, useId, useRef, useState } from "react"
import { nextMenuState, type MenuEvent, type MenuState } from "./menu-state.js"

export interface ManageMenuItem { label: string; onSelect: () => void }

/**
 * An installed view's Manage control. The options float under the button
 * instead of growing the card, so Open and Manage keep their places; Remove
 * asks once before it deletes the package and any previous version.
 */
export function ManageMenu({ label, busy, items, packageId, remove }: {
  label: string; busy: boolean; items: readonly ManageMenuItem[]; packageId: string
  remove: { label: string; question: string; confirm: string; cancel: string; onConfirm: () => void }
}) {
  const [state, setState] = useState<MenuState>("closed")
  const root = useRef<HTMLDivElement>(null), button = useRef<HTMLButtonElement>(null), id = useId()
  const send = (event: MenuEvent) => setState(current => nextMenuState(current, event))
  const dismiss = (focus: boolean) => {
    send("dismiss")
    if (focus) requestAnimationFrame(() => button.current?.focus({ preventScroll: true }))
  }
  const dismissRef = useRef(dismiss); dismissRef.current = dismiss
  useEffect(() => {
    if (state === "closed") return
    const node = root.current
    // Focus the first choice on open, and the safer Cancel when confirming.
    requestAnimationFrame(() => node?.querySelector<HTMLButtonElement>(state === "confirm" ? ".vp-menu-cancel" : "[role=menuitem]")?.focus({ preventScroll: true }))
    const outside = (e: PointerEvent) => { if (!node?.contains(e.target as Node)) dismissRef.current(false) }
    // The view host defers Escape while a menu is open, so this closes only the menu.
    const key = (e: KeyboardEvent) => {
      if (e.key === "Escape") { e.preventDefault(); e.stopImmediatePropagation(); dismissRef.current(true); return }
      if (state !== "open" || (e.key !== "ArrowDown" && e.key !== "ArrowUp")) return
      const choices = [...(node?.querySelectorAll<HTMLButtonElement>("[role=menuitem]:not([disabled])") ?? [])]
      if (!choices.length) return
      e.preventDefault()
      const at = choices.indexOf(document.activeElement as HTMLButtonElement)
      choices[(at + (e.key === "ArrowDown" ? 1 : -1) + choices.length) % choices.length].focus()
    }
    document.addEventListener("pointerdown", outside, true)
    window.addEventListener("keydown", key, true)
    return () => { document.removeEventListener("pointerdown", outside, true); window.removeEventListener("keydown", key, true) }
  }, [state])
  const choose = (fn: () => void) => { dismiss(true); fn() }
  return <div className="vp-menu-wrap" ref={root} data-open={state !== "closed"}>
    <button ref={button} type="button" className="vp-menu-button" aria-haspopup="menu" aria-expanded={state !== "closed"} aria-controls={state !== "closed" ? id : undefined}
      onClick={() => state === "closed" ? send("toggle") : dismiss(false)}>
      {label} <span aria-hidden="true">⌄</span>
    </button>
    {state === "open" && <div className="vp-menu" id={id} role="menu" aria-label={label}>
      {items.map(item => <button key={item.label} type="button" role="menuitem" disabled={busy} onClick={() => choose(item.onSelect)}>{item.label}</button>)}
      <button type="button" role="menuitem" className="vp-remove" disabled={busy} onClick={() => send("remove")}>{remove.label}</button>
      <small className="vp-menu-id" title={packageId}>{packageId}</small>
    </div>}
    {state === "confirm" && <div className="vp-menu vp-menu-confirm" id={id} role="alertdialog" aria-label={remove.label}>
      <p>{remove.question}</p>
      <div className="vp-menu-actions">
        <button type="button" className="vp-menu-cancel" onClick={() => dismiss(true)}>{remove.cancel}</button>
        <button type="button" className="vp-danger" disabled={busy} onClick={() => choose(remove.onConfirm)}>{remove.confirm}</button>
      </div>
    </div>}
  </div>
}
