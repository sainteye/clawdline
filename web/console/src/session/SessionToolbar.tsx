import * as L from "../legacy/bridge.js"
import { catalogWord } from "../catalog.js"
import { nextWord } from "../next-strings.js"
import type { RefObject } from "react"

/** The original Session toolbar, with the destination decision supplied by its page. */
export function SessionToolbar({ filter, onFilter, terminalMode, onSessions, onTerminals, onVoice, onWork, onStart,
  placeholder, label, inputRef }: {
  filter?: string
  onFilter: (value: string) => void
  terminalMode: boolean
  onSessions: () => void
  onTerminals: () => void
  onVoice: () => void
  onWork: () => void
  onStart: () => void
  placeholder?: string
  label?: string
  inputRef?: RefObject<HTMLInputElement | null>
}) {
  const T = L.strings
  return <div className="filter-row">
    <input ref={inputRef} id="filter" type="search" name="q7f3" placeholder={placeholder ?? T.webFilterPlaceholder}
      value={filter} autoComplete="off" autoCapitalize="off" autoCorrect="off" spellCheck={false}
      data-1p-ignore="" data-lpignore="true" data-bwignore="" data-form-type="other"
      aria-label={label ?? T.webFilterLabel} onChange={(event) => onFilter(event.target.value)} />
    <div className="session-mode-tabs">
      <button className="start session-mode-choice" type="button" title={T.webListLabel} aria-label={T.webListLabel}
        aria-pressed={!terminalMode} onClick={onSessions}>
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.75" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><rect x="4" y="4" width="16" height="16" rx="2.5"/><path d="M8 9h8M8 12h8M8 15h6"/></svg>
      </button>
      <button className="start session-mode-choice" type="button" title={nextWord("terminalListMode")}
        aria-label={nextWord("terminalListMode")} aria-pressed={terminalMode} onClick={onTerminals}>
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.75" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><rect x="3.5" y="5" width="17" height="14" rx="2.5"/><path d="m7.5 10 3 2.5-3 2.5M12.5 15h4"/></svg>
      </button>
    </div>
    <button className="start" id="voice-go" type="button" title={T.webCommand} aria-label={T.webCommandLabel}
      aria-pressed="false" onClick={onVoice}>
      <svg className="ico ico-mic" viewBox="0 0 24 24" aria-hidden="true" focusable="false">
        <rect x="9" y="3" width="6" height="11" rx="3" fill="currentColor" />
        <path d="M5.75 11.75v0.5a6.25 6.25 0 0 0 12.5 0v-0.5M12 18.5V21" fill="none"
          stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" />
      </svg>
      <svg className="ico ico-stop" viewBox="0 0 24 24" aria-hidden="true" focusable="false">
        <rect x="7" y="7" width="10" height="10" rx="2.5" fill="currentColor" />
      </svg>
    </button>
    <button className="start" id="work-create-go" type="button" title={catalogWord("inline", "2c58f0c0b5ae")}
      aria-label={catalogWord("inline", "2c58f0c0b5ae")} onClick={onWork}>
      <svg className="ico ico-work-add" viewBox="0 0 24 24" aria-hidden="true" focusable="false">
        <rect x="4" y="5" width="11" height="14" rx="2" fill="none" stroke="currentColor" strokeWidth="1.7" />
        <path d="M7.5 9h4M7.5 12.5h4M18.5 10.5v7M15 14h7" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" />
      </svg>
    </button>
    <button className="start" id="start-go" type="button" title={terminalMode ? nextWord("terminalStartTitle") : T.webStart}
      aria-label={terminalMode ? nextWord("terminalStartTitle") : T.webStart} onClick={onStart}>
      <svg viewBox="0 0 14 14" aria-hidden="true" focusable="false">
        <path d="M7 2.6v8.8M2.6 7h8.8" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" />
      </svg>
    </button>
  </div>
}
