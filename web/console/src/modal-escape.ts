/** Keep a handled modal Escape from reaching the system's default key action. */
export function bindModalEscape(win: Window): () => void {
  const doc = win.document
  const onKey = (event: KeyboardEvent) => {
    if (event.key !== "Escape" || event.defaultPrevented || event.isComposing || event.keyCode === 229 || doc.fullscreenElement) return
    const target = event.target instanceof Element ? event.target : doc.activeElement
    const modal = target?.closest<HTMLElement>('dialog, [role="dialog"], [role="alertdialog"]') ??
      [...doc.querySelectorAll<HTMLElement>('dialog[open], [aria-modal="true"]')].reverse()
        .find(node => node.getClientRects().length > 0)
    if (!modal) return
    event.preventDefault()
    if (modal.tagName !== "DIALOG") return
    const dialog = modal as HTMLDialogElement
    if (!dialog.open) return
    // Cancelling keydown avoids the native invalid-key sound. Preserve the
    // dialog's normal, vetoable cancel lifecycle instead of bypassing busy
    // guards or React's onCancel. Inner controls get their key first.
    const cancel = new Event("cancel", { cancelable: true })
    if (dialog.dispatchEvent(cancel) && dialog.open) dialog.close()
  }
  // Bubble after React and the sheet's own handlers. Capture would take Escape
  // away from nested pickers and could close a parent on the same key.
  win.addEventListener("keydown", onKey)
  return () => win.removeEventListener("keydown", onKey)
}
