import { ActionProgress } from "../../ui/ActionProgress.js"
import { useLayoutEffect, useRef } from "react"
import { catalogWord } from "../../catalog.js"
import { focusModalContainer } from "../../modal-focus.js"
import type { WorkV2Item } from "./api.js"
import { completeConfirmWords } from "./complete-item.js"
import { WorkIcon } from "./WorkIcon.js"

export function CompleteWorkDialog({ item, busy, failure, onConfirm, onCancel }: {
  item: WorkV2Item
  busy: boolean
  failure: string
  onConfirm: () => void
  onCancel: () => void
}) {
  const dialog = useRef<HTMLDivElement>(null)
  useLayoutEffect(() => focusModalContainer(dialog.current), [])
  return <div className="session-todo-modal" ref={dialog} role="dialog" aria-modal="true" aria-labelledby={`complete-work-title-${item.id}`}>
    <form onSubmit={(event) => { event.preventDefault(); if (!busy) onConfirm() }}>
      <h2 id={`complete-work-title-${item.id}`}>{catalogWord("inline", "5b58c9188a2f")}</h2>
      <p>{completeConfirmWords(item)}</p>
      <div className="work-actions">
        <button className="chip on" type="submit" disabled={busy} aria-busy={busy}>
          <ActionProgress busy={busy}>{!busy && <WorkIcon name="check" />}{busy ? catalogWord("literal", "935d3ef2ee45") : catalogWord("literal", "5a651fa3c7ca")}</ActionProgress></button>
        <button className="chip" type="button" disabled={busy} onClick={onCancel}>{catalogWord("inline", "2cd0f3be8738")}</button>
      </div>
      {failure && <p role="alert">{failure}</p>}
    </form>
  </div>
}
