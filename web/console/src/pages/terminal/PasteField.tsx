import { useId, useLayoutEffect, useRef, useState } from "react"
import { nextWord } from "../../next-strings.js"

/** Native paste target for browsers that refuse programmatic clipboard reads. */
export function PasteField({ available, send, close, failure }: {
  available: boolean
  send: (text: string) => Promise<boolean> | boolean
  close: () => void
  failure?: string
}) {
  const dialog = useRef<HTMLDialogElement>(null)
  const field = useRef<HTMLTextAreaElement>(null)
  const label = useId()
  useLayoutEffect(() => {
    dialog.current?.showModal()
    field.current?.focus({ preventScroll: true })
  }, [])
  const [text, setText] = useState("")
  const [busy, setBusy] = useState(false)
  const [failed, setFailed] = useState(false)
  return <dialog ref={dialog} className="terminal-paste-field" aria-labelledby={label}
    onCancel={event => { event.preventDefault(); if (!busy) close() }}>
    <label id={label}>{nextWord("terminalPasteFieldHelp")}
      <textarea ref={field} autoFocus value={text} onChange={event => setText(event.target.value)}
        autoCapitalize="off" autoComplete="off" autoCorrect="off" spellCheck={false} rows={3} />
    </label>
    <div className="terminal-paste-actions">
      <button className="board-button" type="button" disabled={!available || !text || busy} onClick={async () => {
        setBusy(true); setFailed(false)
        try { if (await send(text)) close(); else setFailed(true) } finally { setBusy(false) }
      }}>{nextWord("terminalPasteSend")}</button>
      <button className="board-button" type="button" disabled={busy} onClick={close}>{nextWord("terminalPasteCancel")}</button>
    </div>
    {failed && <p className="terminal-note" role="alert">{failure || nextWord("terminalPasteUnavailable")}</p>}
  </dialog>
}
