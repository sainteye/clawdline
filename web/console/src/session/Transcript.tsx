import { useMemo } from "react"
import type { Turn } from "@clawdline/contract"
import { client } from "../client.js"
import { usePoll } from "../useFleet.js"
import * as L from "../legacy/bridge.js"

export function Transcript({ id }: { id: string }) {
  const read = useMemo(() => () => client.transcript(id, 60), [id])
  const { data, error, pending } = usePoll(read, 4000)
  if (pending && !data) return <p className="tx-note">{L.strings.webLoading}</p>
  if (error) return <p className="tx-note">{error}</p>
  if (!data) return null
  if (data.evidence === "none") return <p className="tx-note">{data.note || L.strings.webTranscriptFailed}</p>
  if (!data.turns.length) return <p className="tx-note">{L.strings.webTranscriptFailed}</p>
  return (
    <>
      {data.turns.map((t, i) => (
        <TurnView key={i} turn={t} />
      ))}
    </>
  )
}

function TurnView({ turn }: { turn: Turn }) {
  const mine = turn.role === "user"
  return (
    <div className={mine ? "turn me" : "turn them"} data-role={turn.role}>
      <div className="bubble">
        {turn.tool && <div className="tool-name">{turn.tool}</div>}
        {turn.text ? <div className="text">{turn.text}</div> : null}
      </div>
    </div>
  )
}

