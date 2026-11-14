import { shortcutAria, shortcutLabel } from "../shortcuts.js"
import * as L from "../legacy/bridge.js"
import { catalogWord } from "../catalog.js"
import { nextWord } from "../next-strings.js"
import { useEffect, useRef, useState, type KeyboardEvent, type RefObject } from "react"
import { CopilotCallButton } from "../realtime/CopilotCallButton.js"
import { useFirstRun, voiceStartShown } from "./first-run.js"
import "./first-run.css"

const SESSIONS_ICON = <svg className="session-mode-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.75" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><rect x="4" y="4" width="16" height="16" rx="2.5"/><path d="M8 9h8M8 12h8M8 15h6"/></svg>
const TERMINAL_ICON = <svg className="session-mode-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.75" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><rect x="3.5" y="5" width="17" height="14" rx="2.5"/><path d="m7.5 10 3 2.5-3 2.5M12.5 15h4"/></svg>
const CHECK_ICON = <svg className="session-mode-check" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="m5 12.5 4.5 4.5L19 7.5"/></svg>

/** The original Session toolbar, with the destination decision supplied by its page. */
export function SessionToolbar({ filter, onFilter, terminalMode, onSessions, onTerminals, onVoice, onWork, onStart,
  placeholder, label, inputRef, firstRun = false }: {
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
  /** No Session has been started or listed yet: the + keeps the accent until the first one. */
  firstRun?: boolean
}) {
  const T = L.strings
  useFirstRun()
  // Hidden unless chosen in Settings: its V shortcut presses only a visible button.
  const voice = voiceStartShown()
  const [menuOpen, setMenuOpen] = useState(false)
  const searchRef = useRef<HTMLDivElement>(null)
  const pickRef = useRef<HTMLButtonElement>(null)
  const current = terminalMode ? nextWord("terminalListMode") : nextWord("sessionListMode")
  useEffect(() => {
    if (!menuOpen) return
    searchRef.current?.querySelector<HTMLButtonElement>('.session-mode-choice[aria-checked="true"]')?.focus()
    const away = (event: PointerEvent) => { if (!searchRef.current?.contains(event.target as Node)) setMenuOpen(false) }
    document.addEventListener("pointerdown", away)
    return () => document.removeEventListener("pointerdown", away)
  }, [menuOpen])
  const choose = (pick: () => void) => { setMenuOpen(false); pickRef.current?.focus(); pick() }
  const onMenuKey = (event: KeyboardEvent<HTMLDivElement>) => {
    const items = [...event.currentTarget.querySelectorAll<HTMLButtonElement>(".session-mode-choice")]
    const at = items.indexOf(document.activeElement as HTMLButtonElement)
    if (event.key === "Escape") { event.preventDefault(); setMenuOpen(false); pickRef.current?.focus() }
    else if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault()
      items[(at + (event.key === "ArrowDown" ? 1 : items.length - 1)) % items.length]?.focus()
    } else if (event.key === "Tab") setMenuOpen(false)
  }
  return <div className="filter-row">
    <div className="session-search" ref={searchRef}>
      <button ref={pickRef} id="session-mode-pick" className="session-mode-pick" type="button" aria-haspopup="menu"
        aria-expanded={menuOpen} aria-controls={menuOpen ? "session-mode-menu" : undefined}
        aria-label={nextWord("listModeChoose", { mode: current })} title={nextWord("listModeChoose", { mode: current })}
        onClick={() => setMenuOpen((open) => !open)}>
        {terminalMode ? TERMINAL_ICON : SESSIONS_ICON}
        <svg className="session-mode-chevron" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" aria-hidden="true"><path d="m7 10 5 5 5-5"/></svg>
      </button>
      <input ref={inputRef} id="filter" type="search" name="q7f3" placeholder={placeholder ?? T.webFilterPlaceholder}
        value={filter} autoComplete="off" autoCapitalize="off" autoCorrect="off" spellCheck={false}
        data-1p-ignore="" data-lpignore="true" data-bwignore="" data-form-type="other"
        aria-label={label ?? T.webFilterLabel} onChange={(event) => onFilter(event.target.value)} />
      {menuOpen && <div id="session-mode-menu" className="session-mode-menu" role="menu" aria-label={nextWord("listModeChoose", { mode: current })}
        onKeyDown={onMenuKey}>
        <button className="session-mode-choice" data-mode="sessions" type="button" role="menuitemradio" aria-checked={!terminalMode}
          onClick={() => choose(onSessions)}>
          {SESSIONS_ICON}<span>{nextWord("sessionListMode")}</span>{CHECK_ICON}
        </button>
        <button className="session-mode-choice" data-mode="terminals" type="button" role="menuitemradio" aria-checked={terminalMode}
          onClick={() => choose(onTerminals)}>
          {TERMINAL_ICON}<span>{nextWord("terminalListMode")}</span>{CHECK_ICON}
        </button>
      </div>}
    </div>
    <CopilotCallButton />
    <button className="start" id="voice-go" type="button" hidden={!voice} title={T.webCommand + " · " + shortcutLabel("voice")} aria-label={T.webCommandLabel} aria-keyshortcuts={shortcutAria("voice")}
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
    <button className="start" id="work-create-go" type="button" title={catalogWord("inline", "2c58f0c0b5ae") + " · " + shortcutLabel("task")} aria-keyshortcuts={shortcutAria("task")}
      aria-label={catalogWord("inline", "2c58f0c0b5ae")} onClick={onWork}>
      <svg className="ico ico-work-add" viewBox="0 0 24 24" aria-hidden="true" focusable="false">
        <rect x="4" y="5" width="11" height="14" rx="2" fill="none" stroke="currentColor" strokeWidth="1.7" />
        <path d="M7.5 9h4M7.5 12.5h4M18.5 10.5v7M15 14h7" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" />
      </svg>
    </button>
    <button className="start" id="start-go" type="button" data-first-run={firstRun ? "" : undefined} title={(terminalMode ? nextWord("terminalStartTitle") : T.webStart) + " · " + shortcutLabel("start")} aria-keyshortcuts={shortcutAria("start")}
      aria-label={terminalMode ? nextWord("terminalStartTitle") : T.webStart} onClick={onStart}>
      <svg viewBox="0 0 14 14" aria-hidden="true" focusable="false">
        <path d="M7 2.6v8.8M2.6 7h8.8" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" />
      </svg>
    </button>
  </div>
}
