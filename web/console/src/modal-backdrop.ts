import { useRef } from "react"

type Point = { clientX: number; clientY: number }
const moved = (from: Point, to: Point) => Math.hypot(to.clientX - from.clientX, to.clientY - from.clientY) > 8

/** A press that starts in the content (including a text-selection drag) never dismisses. */
export function useModalBackdrop(dismiss: () => void) {
  const press = useRef<Point | null>(null)
  return {
    onPointerDown(event: React.PointerEvent<HTMLElement>) {
      press.current = event.isPrimary && event.button === 0 && event.target === event.currentTarget
        ? { clientX: event.clientX, clientY: event.clientY } : null
    },
    onPointerMove(event: React.PointerEvent<HTMLElement>) {
      if (press.current && moved(press.current, event)) press.current = null
    },
    onPointerCancel() { press.current = null },
    onClick(event: React.MouseEvent<HTMLElement>) {
      const from = press.current
      press.current = null
      if (!from || moved(from, event) || event.target !== event.currentTarget) return
      event.stopPropagation()
      dismiss()
    },
  }
}

export function requestDialogCancel(dialog: HTMLDialogElement): void {
  if (!dialog.open) return
  const cancel = new Event("cancel", { cancelable: true })
  if (dialog.dispatchEvent(cancel) && dialog.open) dialog.close()
}

const bound = new WeakSet<HTMLDialogElement>()

/** A native backdrop targets the dialog itself. Its padding does too, so check the box. */
export function bindNativeModalBackdrop(dialog: HTMLDialogElement): void {
  if (bound.has(dialog)) return
  bound.add(dialog)
  let press: Point | null = null
  const outside = (event: MouseEvent) => {
    if (event.target !== dialog) return false
    const { left, right, top, bottom } = dialog.getBoundingClientRect()
    return event.clientX < left || event.clientX >= right || event.clientY < top || event.clientY >= bottom
  }
  dialog.addEventListener("pointerdown", event => {
    press = dialog.dataset.modalDismiss === "true" && event.isPrimary && event.button === 0 && outside(event)
      ? { clientX: event.clientX, clientY: event.clientY } : null
  })
  dialog.addEventListener("pointermove", event => { if (press && moved(press, event)) press = null })
  dialog.addEventListener("pointercancel", () => { press = null })
  dialog.addEventListener("close", () => { press = null })
  dialog.addEventListener("click", event => {
    const from = press
    press = null
    if (!from || moved(from, event) || dialog.dataset.modalDismiss !== "true" || !outside(event)) return
    event.stopPropagation()
    requestDialogCancel(dialog)
  })
}
