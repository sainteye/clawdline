import { openBrowserSetup } from "../../session/BrowserSetup.js"
import type { SessionRow } from "@clawdline/contract"
import { useEffect, useRef, useState } from "react"
import { sessionName } from "../../brand-mark.js"
import { S } from "../../legacy/js/core/state.js"
import * as L from "../../legacy/bridge.js"
import { nextWord, type NextWord } from "../../next-strings.js"
import {
  readBrowserAudit, readChromeSettings, readBrowserStatus, writeChromeSettings, writeBrowserStop,
  type BrowserMode, type BrowserAudit, type BrowserAuditRow, type ChromeSettings, type BrowserStatus,
} from "./api.js"
import { Switch } from "./Switch.js"
import "./workflow-settings.css"
import "./browser-settings.css"

/**
 * The Browser block on Settings (docs/browser.md): whether the Clawdline
 * extension is connected, the two machine switches, the emergency stop, and
 * the audit of every browser action an agent took.
 *
 * Every read and write is one of the narrow `/v1/browser/*` routes, which
 * Clawdline Cloud carries as the chrome-settings, chrome-settings-update,
 * chrome-stop and chrome-audit words, so the block works the same on a phone.
 * The switches change only these two values; no other machine setting is
 * read or written here.
 */

const STATUS_MS = 5000
const AUDIT_MS = 15000
const AUDIT_ROWS = 200

type Range = "today" | "week" | "all"
type SettingKey = "browser_notify_new_site"

const OP_WORDS: Record<string, NextWord> = {
  viewport: "browserOpViewport",
  close: "browserOpClose",
  open: "browserAuditOpOpen", read: "browserAuditOpRead", click: "browserAuditOpClick", type: "browserAuditOpType",
  key: "browserAuditOpKey", scroll: "browserAuditOpScroll", screenshot: "browserAuditOpScreenshot",
  takeover: "browserAuditOpTakeover", handback: "browserAuditOpHandback", release: "browserAuditOpRelease",
  tabs: "browserAuditOpTabs",
}

function failed(lead: NextWord, error: unknown): string {
  return nextWord(lead, { reason: L.failureSentence(error, { fallback: (error as Error)?.message || "" }) })
}

/** The first second of the chosen range, or 0 for every row. */
function sinceOf(range: Range, now = new Date()): number {
  if (range === "all") return 0
  if (range === "week") return Math.floor(now.getTime() / 1000) - 7 * 86400
  const midnight = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  return Math.floor(midnight.getTime() / 1000)
}

/** The extension's browser as a person names it: "Chrome 131", from its user agent. */
function browserName(agent: string | undefined): string {
  if (!agent) return ""
  const match = /\b(Edg|OPR|HeadlessChrome|Chromium|Chrome)\/(\d+)/.exec(agent)
  if (!match) return agent.length > 40 ? agent.slice(0, 40) + "…" : agent
  const name = match[1] === "Edg" ? "Edge" : match[1] === "OPR" ? "Opera" : match[1] === "HeadlessChrome" ? "Chrome" : match[1]
  return name + " " + match[2]
}

/** A Session's name from the fleet when this console knows it, else its id's first characters. */
function sessionLabel(conversation: string): string {
  if (!conversation) return "—"
  const row = ((S.sessions ?? []) as SessionRow[]).find((r) => r.sessionId === conversation)
  return row ? sessionName(row) : conversation.slice(0, 8)
}

function pathOf(raw: string | undefined): { host: string; path: string } {
  if (!raw) return { host: "", path: "" }
  try {
    const u = new URL(raw)
    const path = (u.pathname === "/" ? "" : u.pathname) + u.search
    let shown = path
    try { shown = decodeURIComponent(path) } catch { /* keep the encoded path */ }
    return { host: u.host, path: shown }
  } catch {
    return { host: raw, path: "" }
  }
}

function timeOf(at: number): string {
  const when = new Date(at * 1000)
  const lang = document.documentElement.lang || undefined
  const today = new Date()
  const sameDay = when.toDateString() === today.toDateString()
  return sameDay
    ? when.toLocaleTimeString(lang, { hour: "2-digit", minute: "2-digit" })
    : when.toLocaleString(lang, { month: "numeric", day: "numeric", hour: "2-digit", minute: "2-digit" })
}

const WHY: Record<string, NextWord> = {
 browser_disabled:"browserAuditWhyDisabled", browser_wrong_mode:"browserAuditWhyMode",browser_tab_closing:"browserAuditWhyClosing",
  browser_stopped: "browserAuditWhyStopped", browser_person_controlling: "browserAuditWhyPerson", browser_timeout: "browserAuditWhyTimeout",
}

/** Why an action was not done: the bridge's own refusals in words, the extension's message as it said it. */
function whyNot(error: string): string {
  return Object.hasOwn(WHY,error) ? nextWord(WHY[error]) : error
}

function AuditEntry({ row }: { row: BrowserAuditRow }) {
  const where = pathOf(row.url)
  const op = OP_WORDS[row.op] ? nextWord(OP_WORDS[row.op]) : row.op
  let typed = ""
  if (row.op === "type") {
    if (row.password) typed = nextWord("browserAuditPassword", { count: row.text_length ?? 0 })
    else if (row.text) typed = row.text
  }
  return <li className={row.ok ? "browser-audit-row" : "browser-audit-row refused"}>
    <div className="browser-audit-head">
      <time dateTime={new Date(row.at * 1000).toISOString()}>{timeOf(row.at)}</time>
      <span className="browser-audit-session">{sessionLabel(row.conversation)}</span>
      <strong className="browser-audit-op">{op}</strong>
    </div>
    {(where.host || row.site) && <div className="browser-audit-where">
      <span className="browser-audit-host">{where.host || row.site}</span>
      {where.path && <span className="browser-audit-path">{where.path}</span>}
    </div>}
    {row.target && <div className="browser-audit-target">{nextWord("browserAuditTarget", { target: row.op === "viewport" && (row.target === "mobile" || row.target === "desktop") ? nextWord(row.target === "mobile" ? "browserPanelMobile" : "browserPanelDesktop") : row.target })}</div>}
    {typed && <div className={row.password ? "browser-audit-typed secret" : "browser-audit-typed"}>{typed}</div>}
    {typed && row.text_cut && !row.password && <div className="browser-audit-cut">{nextWord("browserAuditCut")}</div>}
    {!row.ok && row.error && <div className="browser-audit-error">{nextWord("browserAuditRefused", { reason: whyNot(row.error) })}</div>}
  </li>
}

export function BrowserSettingsBlock({ shown }: { shown: boolean }) {
  const [settings, setSettings] = useState<ChromeSettings | null>(null)
  const [status, setStatus] = useState<BrowserStatus | null>(null)
  const [audit, setAudit] = useState<BrowserAudit | null>(null)
  const [readError, setReadError] = useState("")
  const [auditError, setAuditError] = useState("")
  const [said, setSaid] = useState("")
  const [busy, setBusy] = useState<SettingKey | "mode" | "stop" | null>(null)
  const [retry, setRetry] = useState(0)
  const [conversation, setConversation] = useState("")
  const [site, setSite] = useState("")
  const [range, setRange] = useState<Range>("week")
  // Every Session and site seen in any answer since the block opened, so
  // choosing one does not empty the other choices out of their lists.
  const [known, setKnown] = useState<{ sessions: string[]; sites: string[] }>({ sessions: [], sites: [] })
  const generation = useRef(0)

  useEffect(() => {
    if (!shown) return
    let live = true
    const controller = new AbortController()
    setReadError("")
    void readChromeSettings(controller.signal).then(
      (value) => { if (live) setSettings(value) },
      (error) => { if (live) setReadError(failed("browserSettingsReadFailed", error)) },
    )
    const readStatus = () => readBrowserStatus(controller.signal).then(
      (value) => { if (live) setStatus(value) },
      (error) => { if (live && !controller.signal.aborted) setReadError(failed("browserSettingsReadFailed", error)) },
    )
    void readStatus()
    const timer = window.setInterval(() => { if (!document.hidden) void readStatus() }, STATUS_MS)
    return () => { live = false; controller.abort(); window.clearInterval(timer) }
  }, [shown, retry])

  useEffect(() => {
    if (!shown) return
    let live = true
    const controller = new AbortController()
    const read = () => readBrowserAudit({ conversation, site, since: sinceOf(range), limit: AUDIT_ROWS }, controller.signal).then(
      (value) => {
        if (!live) return
        setAudit(value); setAuditError("")
        setKnown((before) => {
          const sessions = new Set(before.sessions), sites = new Set(before.sites)
          for (const row of value.rows ?? []) {
            if (row.conversation) sessions.add(row.conversation)
            if (row.site) sites.add(row.site)
          }
          return sessions.size === before.sessions.length && sites.size === before.sites.length
            ? before : { sessions: [...sessions], sites: [...sites].sort() }
        })
      },
      (error) => { if (live && !controller.signal.aborted) setAuditError(failed("browserAuditReadFailed", error)) },
    )
    void read()
    const timer = window.setInterval(() => { if (!document.hidden) void read() }, AUDIT_MS)
    return () => { live = false; controller.abort(); window.clearInterval(timer) }
  }, [shown, retry, conversation, site, range])

  const save = async (key: SettingKey) => {
    if (!settings || busy) return
    const started = ++generation.current
    setBusy(key); setSaid("")
    try {
      const saved = await writeChromeSettings({ [key]: !settings[key] })
      if (started === generation.current) { setSettings(saved); setSaid(nextWord("browserSettingsSaved")) }
    } catch (error) {
      if (started === generation.current) setSaid(failed("browserSettingsSaveFailed", error))
    } finally {
      if (started === generation.current) setBusy(null)
    }
  }

  const saveMode = async (mode: BrowserMode) => {
    if (!settings || busy) return
    const started = ++generation.current
    setBusy("mode"); setSaid("")
    try {
      const saved = await writeChromeSettings(settings.browser_mode === undefined ? { browser_enabled: mode !== "native" } : { browser_mode: mode })
      if (started === generation.current) {
        setSettings(saved); setSaid(nextWord("browserSettingsSaved"))
        void readBrowserStatus().then(value => { if (started === generation.current) setStatus(value) }, () => {})
      }
    } catch (error) {
      if (started === generation.current) {
        setSaid(failed("browserSettingsSaveFailed", error))
        void readChromeSettings().then(value => { if (started === generation.current) setSettings(value) }, () => {})
      }
    } finally { if (started === generation.current) setBusy(null) }
  }

  const stop = async (stopped: boolean) => {
    if (busy) return
    const started = ++generation.current
    setBusy("stop"); setSaid("")
    try {
      const answer = await writeBrowserStop(stopped)
      if (started === generation.current) {
        setStatus((before) => before ? { ...before, status: { ...before.status, stopped: answer.stopped, state_error: false } } : before)
        setSaid(nextWord(answer.stopped ? "browserSettingsStoppedSaid" : "browserSettingsResumedSaid"))
      }
    } catch (error) {
      if (started === generation.current) {
        setSaid(failed(stopped ? "browserSettingsStopFailed" : "browserSettingsSaveFailed", error))
        void readBrowserStatus().then(value => {if (started === generation.current) setStatus(value)}, () => {})
      }
    } finally {
      if (started === generation.current) setBusy(null)
    }
  }

  const connected = status?.status.connected === true
  const stopped = status?.status.stopped === true
  const host = status?.status.host
  const sessions = [...known.sessions]
  if (conversation && !sessions.includes(conversation)) sessions.push(conversation)
  const sites = [...known.sites]
  if (site && !sites.includes(site)) sites.push(site)
  const filtered = !!conversation || !!site || range !== "all"
  const rows = audit?.rows ?? []

  const switches: { key: SettingKey; title: NextWord; help: NextWord }[] = [
    { key: "browser_notify_new_site", title: "browserSettingsNotify", help: "browserSettingsNotifyHelp" },
  ]

  const mode = settings?.browser_mode ?? (settings?.browser_enabled ? "isolated" : "native")
  const modes: { value: BrowserMode; title: NextWord; help: NextWord }[] = [
    { value: "native", title: "browserModeNative", help: "browserModeNativeHelp" },
    { value: "user_chrome", title: "browserModeUserChrome", help: "browserModeUserChromeHelp" },
    { value: "isolated", title: "browserModeIsolated", help: "browserModeIsolatedHelp" },
  ]
  return <div className="block settings-workflow settings-browser" id="settings-browser" aria-busy={busy !== null}>
    <b>{nextWord("browserSettingsTitle")}</b>
    <p className="say">{nextWord("browserSettingsIntro")}</p>
    <fieldset className="browser-modes" disabled={!settings || busy !== null}>
      <legend>{nextWord("browserModeTitle")}</legend>
      {modes.map(choice => <label className="browser-mode-choice" key={choice.value}>
        <input type="radio" name="browser-mode" value={choice.value} checked={mode === choice.value} disabled={choice.value === "user_chrome" && settings?.browser_mode === undefined} onChange={() => void saveMode(choice.value)} />
        <span><strong>{nextWord(choice.title)}</strong><span>{nextWord(choice.help)}</span></span>
      </label>)}
    </fieldset>
    <p className="say">{nextWord("browserModeSwitchHelp")}</p>
    {settings && mode !== "native" && <button type="button" className="chip" onClick={openBrowserSetup}>{nextWord("browserSetupOpen")}</button>}

    {mode !== "native" && <div className={connected ? "browser-connection connected" : "browser-connection"} role="status" aria-live="polite">
      <span className="browser-connection-dot" aria-hidden="true" />
      <div>
        <strong>{status ? nextWord(connected ? "browserSettingsConnected" : "browserSettingsNotConnected") : nextWord("browserSettingsLoading")}</strong>
        {status && connected && (host?.browser || host?.version) && <p>{nextWord("browserSettingsConnectedHost", {
          browser: browserName(host?.browser) || "Chrome", version: host?.version || "?" })}</p>}
        {status && !connected && <p>{nextWord("browserSettingsNotConnectedHint")}</p>}
      </div>
    </div>}

    {switches.map(({ key, title, help }) => {
      const id = "browser-setting-" + key
      const available = settings !== null && typeof settings[key] === "boolean"
      return <div className="workflow-setting" key={key}>
        <div className="workflow-setting-row">
          <div className="workflow-setting-label browser-setting-label">
            <strong id={id + "-title"}>{nextWord(title)}</strong>
            <span id={id + "-help"}>{nextWord(help)}</span>
          </div>
          <Switch labelledBy={id + "-title"} describedBy={id + "-help"} id={id}
            on={settings?.[key] === true} disabled={!available || busy !== null}
            stateText={settings ? nextWord(settings[key] ? "browserSettingsOn" : "browserSettingsOff") : nextWord("browserSettingsLoading")}
            onToggle={() => void save(key)} />
        </div>
      </div>
    })}

    <div className="browser-stop-area">
      {status?.status.state_error && <div className="screen-note err" role="alert"><p>{nextWord("browserStateUnsaved")}</p><button type="button" className="chip" id="browser-stop-retry" disabled={busy !== null} onClick={() => void stop(true)}>{nextWord("browserStateRetry")}</button></div>}
      {stopped
        ? <div className="browser-stopped" role="alert">
            <p>{nextWord("browserSettingsStoppedBanner")}</p>
            <button type="button" className="chip" id="browser-resume" disabled={busy !== null} onClick={() => void stop(false)}>
              {nextWord("browserSettingsResume")}
            </button>
          </div>
        : <button type="button" className="browser-stop" id="browser-stop" disabled={!status || busy !== null} onClick={() => void stop(true)}>
            <svg viewBox="0 0 16 16" aria-hidden="true" focusable="false"><rect x="3.5" y="3.5" width="9" height="9" rx="1.5" /></svg>
            {nextWord("browserSettingsStop")}
          </button>}
    </div>

    <p className="said" role="status" aria-live="polite">{said || readError}</p>
    {readError && <button type="button" className="chip" onClick={() => setRetry((n) => n + 1)}>{nextWord("browserSettingsRetry")}</button>}

    <div className="browser-audit" aria-labelledby="browser-audit-title">
      <strong id="browser-audit-title" className="browser-audit-title">{nextWord("browserAuditTitle")}</strong>
      <div className="browser-audit-filters">
        <label>
          <span>{nextWord("browserAuditSession")}</span>
          <span className="workflow-profile-picker">
            <select id="browser-audit-session" value={conversation} onChange={(e) => setConversation(e.target.value)}>
              <option value="">{nextWord("browserAuditAllSessions")}</option>
              {sessions.map((c) => <option key={c} value={c}>{sessionLabel(c)}</option>)}
            </select>
            <svg viewBox="0 0 16 16" aria-hidden="true" focusable="false"><path d="m4 6 4 4 4-4" /></svg>
          </span>
        </label>
        <label>
          <span>{nextWord("browserAuditSite")}</span>
          <span className="workflow-profile-picker">
            <select id="browser-audit-site" value={site} onChange={(e) => setSite(e.target.value)}>
              <option value="">{nextWord("browserAuditAllSites")}</option>
              {sites.map((s) => <option key={s} value={s}>{s}</option>)}
            </select>
            <svg viewBox="0 0 16 16" aria-hidden="true" focusable="false"><path d="m4 6 4 4 4-4" /></svg>
          </span>
        </label>
        <label>
          <span>{nextWord("browserAuditDate")}</span>
          <span className="workflow-profile-picker">
            <select id="browser-audit-range" value={range} onChange={(e) => setRange(e.target.value as Range)}>
              <option value="today">{nextWord("browserAuditToday")}</option>
              <option value="week">{nextWord("browserAuditWeek")}</option>
              <option value="all">{nextWord("browserAuditAll")}</option>
            </select>
            <svg viewBox="0 0 16 16" aria-hidden="true" focusable="false"><path d="m4 6 4 4 4-4" /></svg>
          </span>
        </label>
      </div>
      {auditError && <p className="browser-audit-note bad" role="alert">{auditError}</p>}
      {audit?.journal_error && <p className="browser-audit-note bad">{nextWord("browserAuditJournalError")}</p>}
      {audit && audit.pruned > 0 && <p className="browser-audit-note">{nextWord("browserAuditPruned", { count: audit.pruned })}</p>}
      {audit && rows.length === 0 && <p className="browser-audit-empty">
        {nextWord(filtered && known.sessions.length > 0 ? "browserAuditEmptyFiltered" : "browserAuditEmpty")}
      </p>}
      {rows.length > 0 && <ol className="browser-audit-list">
        {rows.map((row, i) => <AuditEntry key={row.at + ":" + i} row={row} />)}
      </ol>}
    </div>
  </div>
}
