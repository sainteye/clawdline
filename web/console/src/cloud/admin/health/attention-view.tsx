/* "Waiting for you" on the connection-health home: what the Cloud says an operator has to act
 * on, each item in plain words with the one thing to do and a link to its incident. Reading the
 * list changes nothing; no item here approves anything. */
import { useEffect, useMemo, useState } from "react"
import { attentionWatch, type Attention } from "./attention"
import type { HealthSay } from "./words"

export function AttentionPanel({ apiOrigin, get, say, go }: { apiOrigin: string; get: typeof fetch; say: HealthSay; go: (href: string) => void }) {
  const watch = useMemo(() => attentionWatch(apiOrigin, get), [apiOrigin, get])
  const [value, setValue] = useState<Attention | null>(null)
  useEffect(() => watch.subscribe(setValue), [watch])
  return (
    <section className="adh-attention" aria-labelledby="adh-attention-title">
      <h2 id="adh-attention-title">{say("at_title")}</h2>
      <AttentionBody value={value} say={say} go={go} />
    </section>
  )
}

export function AttentionBody({ value, say, go }: { value: Attention | null; say: HealthSay; go: (href: string) => void }) {
  if (value === null) return <p className="adm-fine" role="status">{say("at_loading")}</p>
  if (value.state === "failed") return <p className="adm-fine" role="status">{say("at_failed", { code: value.code })}</p>
  if (value.state === "unavailable") return <p className="adm-fine" role="status">{say("at_unavailable")}</p>
  if (value.items.length === 0) return <p className="adm-fine" role="status">{value.complete ? say("at_none") : say("at_noneIncomplete")}</p>
  return (
    <>
      {value.complete ? null : <p className="adm-fine" role="note">{say("at_incomplete")}</p>}
      <ul className="adh-attention-list">
        {value.items.map((item) => (
          <li key={item.id} data-action={item.action}>
            <p>{say(`at_reason_${item.reason}`)}</p>
            <a className="adh-link" href={item.href}
              onClick={(e) => { if (e.button === 0 && !e.metaKey && !e.ctrlKey && !e.shiftKey && !e.altKey) { e.preventDefault(); go(item.href) } }}>
              {say(`at_action_${item.action}`)}
            </a>
            <span className="adm-fine"> · {say("at_incident", { id: item.incident_id, revision: item.incident_revision })}</span>
          </li>
        ))}
      </ul>
    </>
  )
}
