import { useState } from "react"
import type { SessionRow } from "@clawdline/contract"
import { RefusalError } from "@clawdline/core"
import { client } from "../client.js"
import * as L from "../legacy/bridge.js"

export function Composer({ row, onDid }: { row: SessionRow | null; onDid: () => void }) {
  const [text, setText] = useState("")
  const [busy, setBusy] = useState(false)
  const [why, setWhy] = useState("")
  const T = L.strings

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!row || !text.trim()) return
    setBusy(true)
    try {
      await client.send(row.id, text)
      // The strongest thing a send can claim. Whether the assistant took the
      // turn is a separate fact with its own evidence, read from the list.
      // The original's composer says nothing on success — the turn appearing in
      // the transcript is the answer. Inventing a confirmation here would claim
      // delivery, which a send cannot.
      setWhy("")
      setText("")
      onDid()
    } catch (err) {
      setWhy(err instanceof RefusalError ? `${err.code} — ${err.detail}` : String(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form className="composer" id="composer" onSubmit={submit} data-write={row ? "on" : "off"}>
      <div className="box">
        <textarea
          className={text ? "msg" : "msg blank"}
          id="msg"
          rows={1}
          placeholder={T.placeholder}
          aria-label={T.placeholder}
          value={text}
          disabled={!row || busy}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.shiftKey) {
              e.preventDefault()
              void submit(e)
            }
          }}
        />
        <button className="send" id="send" type="submit" disabled={!row || busy || !text.trim()}>
          {busy ? T.webSending : T.webSend}
        </button>
      </div>
      <div className="why" id="why">
        {why}
      </div>
    </form>
  )
}
