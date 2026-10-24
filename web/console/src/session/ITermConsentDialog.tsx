import { useEffect, useRef, useSyncExternalStore } from "react"
import { catalogWord } from "../catalog.js"
import { showModalWithoutChoice } from "../modal-focus.js"
import { failureSentence } from "../refusals/refusal-text.js"
import { onMachineChanged } from "../cloud/machine-switch.js"
import { itermConsent } from "./iterm-consent.js"
import "./iterm-consent.css"

const word = (key: string) => catalogWord("itermConsent", key)

export function ITermConsentDialog() {
  const state = useSyncExternalStore(itermConsent.subscribe, itermConsent.snapshot)
  const dialog = useRef<HTMLDialogElement>(null)
  const open = state.phase !== "closed"
  useEffect(() => onMachineChanged(itermConsent.close), [])
  useEffect(() => {
    if (open) showModalWithoutChoice(dialog.current)
    else if (dialog.current?.open) dialog.current.close()
  }, [open])
  const saving = state.phase === "saving"
  const saved = state.phase === "saved"
  return <dialog ref={dialog} className="iterm-consent-dialog"
    aria-labelledby="iterm-consent-title" aria-describedby="iterm-consent-description"
    onCancel={event => { event.preventDefault(); if (!saving) itermConsent.close() }}>
    <h2 id="iterm-consent-title">{word(saved ? "savedTitle" : "title")}</h2>
    <p id="iterm-consent-description">{word(saved ? "saved" : "description")}</p>
    {!saved && <p>{word("scope")}</p>}
    {state.phase === "failed" && <p role="alert">{failureSentence(state.error, word("failed"))}</p>}
    <div className="buttons">
      <button type="button" className="chip" disabled={saving} onClick={itermConsent.close}>{word(saved ? "done" : "cancel")}</button>
      {!saved && <button type="button" className="chip primary" disabled={saving}
        onClick={() => void itermConsent.confirm()}>{word(saving ? "saving" : "enable")}</button>}
    </div>
  </dialog>
}
