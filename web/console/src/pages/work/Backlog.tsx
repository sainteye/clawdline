import { useState } from "react"
import * as L from "../../legacy/bridge.js"
import type { BacklogPage, Command, Item } from "./api.js"
import type { Run } from "./Board.js"
import { today, when } from "./shared.js"
import { workWord } from "./words.js"

/**
 * The Backlog tab (board-redesign §3.3, §5.3): the planned list in its order,
 * each row with 開始做 and 排入. It is a list rather than the board's cards on
 * purpose — nothing here is happening, and it should not look as if it were.
 * Nothing leaves it except by a person: start, schedule, or discard.
 */
export function BacklogView({
  page,
  busy,
  run,
  onCommand,
  onMore,
}: {
  page: BacklogPage | null
  busy: boolean
  run: Run
  onCommand: (it: Item, c: Command) => Promise<unknown>
  onMore: () => void
}) {
  const rows = page?.rows ?? []
  return (
    <>
      {page && rows.length === 0 ? (
        <p className="work-empty" style={{ marginTop: 20 }}>
          {workWord("sectionEmpty")}
        </p>
      ) : (
        <ol className="work-backlog" aria-label={workWord("backlogTitle")}>
          {rows.map((it) => (
            <BacklogRow key={it.id} item={it} busy={busy} run={run} onCommand={onCommand} />
          ))}
        </ol>
      )}
      {page?.next_cursor && (
        <button className="board-button work-more" type="button" disabled={busy} onClick={onMore}>
          {workWord("more", { n: Math.max(0, page.counts.planned - rows.length) })}
        </button>
      )}
    </>
  )
}

function BacklogRow({
  item,
  busy,
  run,
  onCommand,
}: {
  item: Item
  busy: boolean
  run: Run
  onCommand: (it: Item, c: Command) => Promise<unknown>
}) {
  const [asking, setAsking] = useState<"schedule" | "rank" | "drop" | null>(null)
  const [date, setDate] = useState(item.start_on ?? today())
  const [rank, setRank] = useState(String(item.rank ?? 0))
  return (
    <li className="work-backlog-row" data-work-id={item.id}>
      <span className="work-rank">{item.rank ? workWord("rank", { n: item.rank }) : workWord("rankNone")}</span>
      <div>
        <h3>{item.title}</h3>
        <div className="work-meta">
          <span>{item.project}</span>
          {item.start_on && <span>{workWord("startOn", { date: item.start_on })}</span>}
          <span className="work-clock">{when(item.placed_at)}</span>
        </div>
      </div>
      {asking === "schedule" ? (
        <form
          className="work-actions"
          onSubmit={(ev) => {
            ev.preventDefault()
            setAsking(null)
            run(() => onCommand(item, { op: "schedule", start_on: date }))
          }}
        >
          <input
            className="work-input"
            type="date"
            aria-label={workWord("scheduleOn")}
            value={date}
            required
            autoFocus
            onChange={(ev) => setDate(ev.target.value)}
          />
          <button className="chip on" type="submit" disabled={busy || !date}>
            {workWord("opSchedule")}
          </button>
          <button className="chip" type="button" onClick={() => setAsking(null)}>
            {L.strings.webCancel}
          </button>
        </form>
      ) : asking === "rank" ? (
        <form
          className="work-actions"
          onSubmit={(ev) => {
            ev.preventDefault()
            const n = Number.parseInt(rank, 10)
            if (!Number.isInteger(n) || n < 0) return
            setAsking(null)
            run(() => onCommand(item, { op: "rank", rank: n }))
          }}
        >
          <input
            className="work-input"
            type="number"
            min={0}
            step={1}
            style={{ width: 90 }}
            aria-label={workWord("rankTo")}
            title={workWord("rankTo")}
            value={rank}
            autoFocus
            onChange={(ev) => setRank(ev.target.value)}
          />
          <button className="chip on" type="submit" disabled={busy}>
            {workWord("apply")}
          </button>
          <button className="chip" type="button" onClick={() => setAsking(null)}>
            {L.strings.webCancel}
          </button>
        </form>
      ) : asking === "drop" ? (
        <div className="work-actions" role="group">
          <button
            className="chip danger"
            type="button"
            disabled={busy}
            autoFocus
            onClick={() => {
              setAsking(null)
              run(() => onCommand(item, { op: "drop" }))
            }}
          >
            {workWord("opDiscard")} — {item.title}
          </button>
          <button className="chip" type="button" onClick={() => setAsking(null)}>
            {L.strings.webCancel}
          </button>
        </div>
      ) : (
        <div className="work-actions">
          <button className="chip on" type="button" data-op="start" disabled={busy}
            onClick={() => run(() => onCommand(item, { op: "start" }))}>
            {workWord("opStart")}
          </button>
          <button className="chip" type="button" data-op="schedule" disabled={busy} onClick={() => setAsking("schedule")}>
            {workWord("opSchedule")}
          </button>
          <button className="chip" type="button" data-op="rank" disabled={busy} onClick={() => setAsking("rank")}>
            {workWord("opRank")}
          </button>
          <button className="chip danger" type="button" data-op="drop" disabled={busy} onClick={() => setAsking("drop")}>
            {workWord("opDiscard")}
          </button>
        </div>
      )}
    </li>
  )
}
