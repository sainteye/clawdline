import { useEffect, useRef, useState } from "react"
import type { SessionRow } from "@clawdline/contract"
import * as L from "./legacy/bridge.js"
import { Row } from "./session/List.js"
import { Detail } from "./session/Detail.js"

/**
 * The session list page: the list, and the conversation beside it.
 *
 * The parts live in ./session/ so they can be worked on without three people
 * editing one file. The markup in each is the original's — same elements, same
 * class names — because the stylesheet beside them is the original's, copied.
 */
export function SessionsPage({
  rows,
  loaded,
  emptyAuthoritative,
  error,
  selected,
  onSelect,
  onDid,
}: {
  rows: SessionRow[]
  loaded: boolean
  emptyAuthoritative: boolean
  error: string | null
  selected: string | null
  onSelect: (id: string | null) => void
  onDid: () => void
}) {
  const [filter, setFilter] = useState("")
  // The copied modules read a module-level object, so it is filled before
  // anything is drawn from them, and the list's own order and filter are used.
  L.publish(rows, selected, filter)
  const shown = L.orderedRows()
  const open = shown.find((r) => r.id === selected) ?? null
  const T = L.strings

  // Every spinner in the list, handed to the one clock that drives them all.
  // Re-registered after each render because the rows are rebuilt: a canvas that
  // has left the document must leave the list with it.
  const listRef = useRef<HTMLUListElement>(null)
  useEffect(() => {
    L.registerSpinners([...(listRef.current?.querySelectorAll<HTMLCanvasElement>("canvas.spin") ?? [])])
  })

  return (
    <main className="app" id="app" data-pane={open ? "detail" : "list"}>
      <section className="pane pane-list">
        <div className="filter-row">
          <input
            id="filter"
            type="search"
            placeholder={T.webFilterPlaceholder}
            aria-label={T.webFilterLabel}
            autoComplete="off"
            autoCapitalize="off"
            autoCorrect="off"
            spellCheck={false}
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
          />
          <span className="slash">/</span>
        </div>
        <div className="scroller list-scroll" id="list-scroll">
          {error && <div className="empty">{error}</div>}
          {!loaded && <div className="empty">{T.webLoading}</div>}
          {loaded && shown.length === 0 && (
            // An empty list only means "nothing is running" when the reading
            // was complete; otherwise it means the reading could not see.
            <div className="empty">
              {emptyAuthoritative ? T.webCountNone : T.webEmptyWaitTitle}
            </div>
          )}
          <ul className="rows" id="rows" role="listbox" aria-label="Sessions" tabIndex={0} ref={listRef}>
            {shown.map((r) => (
              <Row key={r.id} row={r} open={r.id === selected} onSelect={onSelect} />
            ))}
          </ul>
        </div>
      </section>

      <Detail row={open} onBack={() => onSelect(null)} onDid={onDid} />
    </main>
  )
}

