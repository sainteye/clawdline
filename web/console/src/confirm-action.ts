import { catalogWord } from "./catalog.js"
import { showModalWithoutChoice } from "./modal-focus.js"
import "./confirm-action.css"

export interface ConfirmationOptions {
  title?: string
  confirmLabel?: string
  cancelLabel?: string
  signal?: AbortSignal
}

let pending = false

/** One explicit decision, shared by the console, Cloud and standalone pages.
 * Concurrent requests are declined rather than queued behind an older action.
 * Closing, Escape, unloading and an aborted owner always mean cancellation.
 */
export function confirmAction(message: string, options: ConfirmationOptions = {}): Promise<boolean> {
  if (pending || options.signal?.aborted) return Promise.resolve(false)
  pending = true
  return new Promise(resolve => {
    const opener = document.activeElement instanceof HTMLElement ? document.activeElement : null
    const dialog = document.createElement("dialog")
    dialog.className = "app-confirm"
    dialog.dataset.modalDismiss = "true"
    dialog.setAttribute("role", "alertdialog")
    dialog.setAttribute("aria-labelledby", "app-confirm-title")
    dialog.setAttribute("aria-describedby", "app-confirm-message")
    const heading = document.createElement("h2")
    heading.id = "app-confirm-title"
    heading.textContent = options.title ?? catalogWord("legacy", "webConfirm")
    const body = document.createElement("p")
    body.id = "app-confirm-message"
    body.textContent = message
    const actions = document.createElement("div")
    actions.className = "app-confirm-actions"
    const cancel = document.createElement("button")
    cancel.type = "button"
    cancel.dataset.choice = "cancel"
    cancel.textContent = options.cancelLabel ?? catalogWord("legacy", "webCancel")
    const accept = document.createElement("button")
    accept.type = "button"
    accept.dataset.choice = "confirm"
    accept.textContent = options.confirmLabel ?? catalogWord("legacy", "webConfirm")
    const header = document.createElement("header")
    header.className = "app-confirm-heading"
    header.append(heading, cancel)
    actions.append(accept)
    dialog.append(header, body, actions)
    let settled = false
    const finish = (answer: boolean) => {
      if (settled) return
      settled = true
      options.signal?.removeEventListener("abort", abort)
      window.removeEventListener("pagehide", abort)
      observer.disconnect()
      if (dialog.open) dialog.close()
      dialog.remove()
      pending = false
      if (opener?.isConnected) opener.focus({ preventScroll: true })
      resolve(answer)
    }
    const abort = () => finish(false)
    // An owner unmounted while the question was open cannot approve stale work.
    const observer = new MutationObserver(() => {
      if (!dialog.isConnected || (opener && !opener.isConnected)) abort()
    })
    cancel.addEventListener("click", abort)
    accept.addEventListener("click", () => finish(true))
    dialog.addEventListener("cancel", event => { event.preventDefault(); abort() })
    dialog.addEventListener("close", abort)
    dialog.addEventListener("keydown", event => {
      if (event.key === "Tab" && !event.defaultPrevented) {
        const edge = event.shiftKey ? cancel : accept
        if (document.activeElement === edge) {
          event.preventDefault()
          ;(event.shiftKey ? accept : cancel).focus()
        }
      }
      if (event.key === "Escape" && !event.isComposing) {
        event.preventDefault()
        event.stopPropagation()
        abort()
      }
    })
    options.signal?.addEventListener("abort", abort, { once: true })
    window.addEventListener("pagehide", abort, { once: true })
    document.body.append(dialog)
    observer.observe(document.body, { childList: true, subtree: true })
    try { showModalWithoutChoice(dialog) } catch { abort() }
  })
}
