import type { ReactNode } from "react"
import "./action-progress.css"

/** A request indicator; completion still comes from the action's own receipt. */
export function ActionSpinner() {
  return <span className="action-spinner" aria-hidden="true" />
}

export function ActionProgress({ busy, label, children }: { busy: boolean; label?: ReactNode; children: ReactNode }) {
  return <span className="action-progress">{busy && <ActionSpinner />}{busy ? label ?? children : children}</span>
}

/** The schedule form owns its buttons imperatively rather than through React. */
export function paintActionProgress(button: HTMLButtonElement, busy: boolean, label: string): void {
  button.setAttribute("aria-busy", String(busy))
  const content = document.createElement("span")
  content.className = "action-progress"
  if (busy) {
    const spinner = document.createElement("span")
    spinner.className = "action-spinner"
    spinner.setAttribute("aria-hidden", "true")
    content.append(spinner)
  }
  content.append(document.createTextNode(label))
  button.replaceChildren(content)
}
