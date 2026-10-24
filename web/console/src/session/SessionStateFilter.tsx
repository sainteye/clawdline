import { useEffect, useRef, useState, type ReactNode } from "react"
import { createPortal } from "react-dom"
import { nextWord } from "../next-strings.js"
import { showModalWithoutChoice } from "../modal-focus.js"
import { SESSION_STATE_FILTERS, type SessionStateFilter, type StateFilterCounts } from "./state-filter.js"
import "./state-filter.css"

export function stateFilterLabel(value: SessionStateFilter): string {
  return nextWord(value === "all" ? "sessionStateFilterAll" : value === "attention" ? "cloudAllAttention" :
    value === "working" ? "cloudAllStateWorking" : "cloudAllFilterIdle")
}

export function SessionStateFilterControl({ value, onChange, counts, summary, incomplete = false,
  onRefresh, refreshState = "idle" }: {
  value: SessionStateFilter
  onChange: (value: SessionStateFilter) => void
  counts: StateFilterCounts
  summary: ReactNode
  incomplete?: boolean
  onRefresh: () => void
  refreshState?: "idle" | "loading" | "done" | "failed"
}) {
  const [open, setOpen] = useState(false)
  const entry = useRef<HTMLButtonElement>(null)
  const dialog = useRef<HTMLDialogElement>(null)
  useEffect(() => {
    if (open) showModalWithoutChoice(dialog.current)
  }, [open])
  const close = () => {
    dialog.current?.close()
    setOpen(false)
    entry.current?.focus({ preventScroll: true })
  }
  const active = value !== "all"
  const hints = { all: "sessionStateFilterAllHint", attention: "sessionStateFilterAttentionHint",
    working: "sessionStateFilterWorkingHint", idle: "sessionStateFilterIdleHint" } as const
  return <>
    <button type="button" className="counts session-state-entry" id="session-state-filter-open" ref={entry}
      data-active={active} aria-haspopup="dialog" aria-expanded={open} aria-controls="session-state-filter"
      aria-label={active ? nextWord("sessionStateFilterActive", { state: stateFilterLabel(value) }) : nextWord("sessionStateFilterTitle")}
      onClick={() => setOpen(true)}>
      {active ? <><span className="session-state-selected">{stateFilterLabel(value)}</span>
        <span className="session-state-count">{incomplete ? "" : counts[value]}</span></> : summary}
      <svg className="session-state-caret" viewBox="0 0 12 12" aria-hidden="true"><path d="m3 4.5 3 3 3-3" /></svg>
    </button>
    {open && createPortal(<dialog ref={dialog} id="session-state-filter" className="session-state-dialog"
      aria-labelledby="session-state-filter-title" onCancel={(event) => { event.preventDefault(); close() }}
      onClick={(event) => { if (event.target === event.currentTarget) close() }}>
      <div className="session-state-sheet" onClick={(event) => event.stopPropagation()}>
        <header className="session-state-head">
          <h2 id="session-state-filter-title">{nextWord("sessionStateFilterTitle")}</h2>
          <button type="button" className="session-state-close" onClick={close} aria-label={nextWord("sessionStateFilterClose")}>×</button>
        </header>
        {incomplete && <p className="session-state-incomplete" role="status">{nextWord("sessionStateFilterIncomplete")}</p>}
        <div role="radiogroup" aria-labelledby="session-state-filter-title" onKeyDown={(event) => {
          const choices = [...event.currentTarget.querySelectorAll<HTMLButtonElement>('[role="radio"]')]
          const at = choices.indexOf(document.activeElement as HTMLButtonElement)
          let next = at
          if (event.key === "ArrowDown" || event.key === "ArrowRight") next = (at + 1) % choices.length
          else if (event.key === "ArrowUp" || event.key === "ArrowLeft") next = (at - 1 + choices.length) % choices.length
          else if (event.key === "Home") next = 0
          else if (event.key === "End") next = choices.length - 1
          else return
          event.preventDefault()
          choices[next]?.focus()
        }}>
          {SESSION_STATE_FILTERS.map((filter) => <button type="button" className="session-state-option" role="radio"
            aria-checked={value === filter} data-filter={filter} key={filter}
            onClick={() => { onChange(filter); close() }}>
            <span className="session-state-radio" aria-hidden="true" />
            <span className="session-state-words"><b>{stateFilterLabel(filter)}</b><small>{nextWord(hints[filter])}</small></span>
            {!incomplete && <span className="session-state-count">{counts[filter]}</span>}
          </button>)}
        </div>
        <footer className="session-state-footer">
          <button type="button" className="session-state-refresh" onClick={onRefresh}
            disabled={refreshState === "loading"} aria-busy={refreshState === "loading"} data-state={refreshState}>
            <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M19 12a7 7 0 1 1-2.05-4.95M19 4.5V9h-4.5" /></svg>
            <span aria-live="polite">{nextWord(refreshState === "loading" ? "cloudAllRefreshing" : refreshState === "failed" ?
              "cloudAllRefreshFailed" : refreshState === "done" ? "cloudAllRefreshDone" : "cloudAllRefresh")}</span>
          </button>
        </footer>
      </div>
    </dialog>, document.body)}
  </>
}
