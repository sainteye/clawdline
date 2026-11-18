import { bindNativeModalBackdrop } from "./modal-backdrop.js"

/** Keep the modal in the keyboard focus chain without choosing an action for the person. */
const keyboardRoots = new WeakSet<HTMLElement>()

export function showModalWithoutChoice(dialog: HTMLDialogElement | null | undefined): void {
  if (!dialog || dialog.open) return
  if (dialog.hasAttribute("data-modal-dismiss")) bindNativeModalBackdrop(dialog)
  dialog.tabIndex = -1
  dialog.showModal()
  focusModalContainer(dialog)
}

export function focusModalContainer(container: HTMLElement | null | undefined): void {
  if (!container) return
  container.tabIndex = -1
  container.dataset.modalInitialFocus = ""
  if (!keyboardRoots.has(container)) {
    keyboardRoots.add(container)
    container.addEventListener("keydown", (event) => {
      if (event.key !== "Tab" || event.defaultPrevented || event.target !== container) return
      const controls = [...container.querySelectorAll<HTMLElement>(
        'a[href], button:not([disabled]), input:not([disabled]), textarea:not([disabled]), select:not([disabled]), summary, [tabindex]:not([tabindex="-1"])',
      )].filter((item) => item.getClientRects().length > 0 && !item.closest("[hidden]"))
      event.preventDefault()
      controls[event.shiftKey ? controls.length - 1 : 0]?.focus()
    })
  }
  container.focus({ preventScroll: true })
}
