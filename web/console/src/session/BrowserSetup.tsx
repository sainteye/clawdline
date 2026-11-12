import { useCallback, useEffect, useRef, useState, type ReactNode } from "react"
import { createRoot, type Root } from "react-dom/client"
import * as L from "../legacy/bridge.js"
import { showModalWithoutChoice } from "../modal-focus.js"
import { nextWord, type NextWord } from "../next-strings.js"
import { writeChromeSettings } from "../pages/settings/api.js"
import { browserCall } from "./browser-call.js"
import "../setup/setup.css"
import "./browser-setup.css"

/**
 * The browser setup guide: what browser use is for, Clawdline's own Chrome
 * opened from the console, and the two machine settings it needs
 * (docs/browser.md). It is built like the setup wizard (`setup/SetupGuide.tsx`)
 * and needs no terminal: `GET /v1/browser/setup` and `POST /v1/browser/launch`
 * are carried through Clawdline Cloud as chrome-setup and chrome-launch, so a
 * phone can start the machine's Chrome too.
 *
 * Any screen opens it with `openBrowserSetup()`, or by dispatching
 * `OPEN_BROWSER_SETUP_EVENT` on `window`. It mounts its own root, so it does
 * not depend on where in the page it was asked for.
 */
export const OPEN_BROWSER_SETUP_EVENT = "clawdline-open-browser-setup"

/** How often the guide asks whether the extension has connected. */
const POLL_MS = 2_000
/** How long the guide waits before it lists what may have gone wrong. */
const SLOW_MS = 30_000

type Step = "benefits" | "prepare" | "confirm" | "start" | "done"
const STEPS: readonly Step[] = ["benefits", "prepare", "confirm", "start"]
const STEP_WORD: Record<Step, NextWord> = {
  benefits: "browserSetupStepBenefits", prepare: "browserSetupStepPrepare", confirm: "browserSetupStepConfirm",
  start: "browserSetupStepStart", done: "browserSetupStepDone",
}

type Setup = {
  mode?: "native" | "user_chrome" | "isolated"
  chromium_found: boolean
  connected: boolean
  running?: boolean
  enabled?: boolean
  notify_new_site?: boolean
  install_command?: string
}

const INSTALL_COMMAND = "npx playwright install chromium"

let root: Root | null = null
let host: HTMLElement | null = null

/** Open the guide over whatever is on screen; a second call while it is open does nothing. */
export function openBrowserSetup(): void {
  if (root) return
  host = document.createElement("div")
  host.className = "browser-setup-root"
  document.body.appendChild(host)
  root = createRoot(host)
  root.render(<BrowserSetup onClose={closeBrowserSetup} />)
}

function closeBrowserSetup(): void {
  const r = root
  root = null
  // Unmounting during the click that asked for it is React's warning case.
  queueMicrotask(() => {
    r?.unmount()
    host?.remove()
    host = null
  })
}

if (typeof window !== "undefined") window.addEventListener(OPEN_BROWSER_SETUP_EVENT, openBrowserSetup)

function refusalCode(error: unknown): string {
  const code = (error as { code?: unknown } | null)?.code
  return typeof code === "string" ? code : ""
}

/** A refusal in the console's own words, with its code kept (refusals/scan.ts). */
function said(error: unknown, lead: "browserSetupLaunchFailed" | "browserSetupReadFailed" | "browserSetupSaveFailed"): string {
  return nextWord(lead, { reason: L.failureSentence(error, { fallback: (error as Error)?.message || "" }) })
}

export function BrowserSetup({ onClose }: { onClose: () => void }) {
  const dialog = useRef<HTMLDialogElement>(null)
  const [step, setStep] = useState<Step>("benefits")
  const [enabled, setEnabled] = useState(true)
  const [notify, setNotify] = useState(true)
  const [connected, setConnected] = useState(false)
  const [mode, setMode] = useState<string | null>(null)
  const [modeError, setModeError] = useState<unknown>(null)
  useEffect(() => {
    let alive = true
    void browserCall<Setup>("GET", "/v1/browser/setup").then(value => { if (alive) setMode(value.mode ?? "isolated") }, error => { if (alive) setModeError(error) })
    return () => { alive = false }
  }, [])
  useEffect(() => { showModalWithoutChoice(dialog.current) }, [])
  const at = step === "done" ? STEPS.length : STEPS.indexOf(step)
  return (
    <dialog ref={dialog} className="setup-dialog browser-setup" aria-label={nextWord("browserSetupDialog")}
      onCancel={(event) => { event.preventDefault(); onClose() }}>
      <div className="setup-top">
        {mode !== "user_chrome" && <ol className="setup-track browser-setup-track">
          {STEPS.map((item, i) => (
            <li key={item} data-current={item === step ? "true" : undefined} data-done={i < at ? "true" : undefined}
              aria-current={item === step ? "step" : undefined}>
              {i < at ? <CheckIcon /> : <span className="setup-track-dot" aria-hidden="true" />}
              <span>{nextWord(STEP_WORD[item])}</span>
            </li>
          ))}
        </ol>}
        <button type="button" className="setup-x" aria-label={nextWord("browserSetupClose")} onClick={onClose}>
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true"><path d="M6 6l12 12M18 6L6 18" /></svg>
        </button>
      </div>
      {modeError ? <p className="setup-said" role="alert">{said(modeError, "browserSetupReadFailed")}</p> : mode === null ? <p className="setup-body">{nextWord("browserSettingsLoading")}</p> : mode === "user_chrome" ? <UserChromeGuide onClose={onClose} /> : <>
      {step === "benefits" && <Benefits onLater={onClose} onNext={() => setStep("prepare")} />}
      {step === "prepare" && <Prepare onBack={() => setStep("benefits")}
        onConnected={() => { setConnected(true); setStep("confirm") }} />}
      {step === "confirm" && <Confirm onBack={() => setStep("prepare")} onNext={() => setStep("start")} />}
      {step === "start" && <Start enabled={enabled} notify={notify} onEnabled={setEnabled} onNotify={setNotify}
        onBack={() => setStep("confirm")} onDone={() => setStep("done")} />}
      {step === "done" && <Done connected={connected} enabled={enabled} notify={notify} onClose={onClose} />}
      </>}
    </dialog>
  )
}

function UserChromeGuide({ onClose }: { onClose: () => void }) {
  return <div className="setup-body">
    <h2>{nextWord("browserUserChromeTitle")}</h2>
    <p>{nextWord("browserUserChromeIntro")}</p>
    <ol>
      <li><p>{nextWord("browserUserChromePrepare")}</p><CopyLine text="clawdline browser prepare --mode user_chrome" /></li>
      <li><p>{nextWord("browserUserChromeLoad")}</p></li>
      <li><p>{nextWord("browserUserChromeConnect")}</p></li>
    </ol>
    <div className="setup-actions"><button type="button" className="setup-primary" onClick={onClose}>{nextWord("browserSetupClose")}</button></div>
  </div>
}

function Benefits({ onLater, onNext }: { onLater: () => void; onNext: () => void }) {
  const rows: { icon: ReactNode; title: NextWord; hint: NextWord }[] = [
    { icon: <EyeIcon />, title: "browserSetupWatch", hint: "browserSetupWatchHint" },
    { icon: <HandIcon />, title: "browserSetupTakeOver", hint: "browserSetupTakeOverHint" },
    { icon: <PeopleIcon />, title: "browserSetupShared", hint: "browserSetupSharedHint" },
    { icon: <ListIcon />, title: "browserSetupAudit", hint: "browserSetupAuditHint" },
  ]
  return (
    <div className="setup-body">
      <p className="setup-eyebrow">{nextWord("browserSetupEyebrow")}</p>
      <h2>{nextWord("browserSetupTitle")}</h2>
      <ul className="setup-features browser-setup-features">
        {rows.map((row) => (
          <li key={row.title}>
            <span className="setup-feature-icon">{row.icon}</span>
            <span><b>{nextWord(row.title)}</b><span>{nextWord(row.hint)}</span></span>
          </li>
        ))}
      </ul>
      <p className="browser-setup-plain">{nextWord("browserSetupPlain")}</p>
      <div className="setup-actions">
        <button type="button" className="setup-quiet" onClick={onLater}>{nextWord("browserSetupLater")}</button>
        <button type="button" className="setup-primary" onClick={onNext}>{nextWord("browserSetupNext")}</button>
      </div>
    </div>
  )
}

/**
 * Open Clawdline's Chrome and wait for its extension. The daemon answers
 * `browser_chromium_missing` when there is no Chromium to open; a Chrome that
 * is already open on that profile opens one more window, which is harmless.
 */
function Prepare({ onBack, onConnected }: { onBack: () => void; onConnected: () => void }) {
  const [setup, setSetup] = useState<Setup | null>(null)
  const [readError, setReadError] = useState<unknown>(null)
  const [launchError, setLaunchError] = useState<unknown>(null)
  const [missing, setMissing] = useState(false)
  const [launching, setLaunching] = useState(false)
  const [waitingSince, setWaitingSince] = useState(0)
  const [now, setNow] = useState(() => Date.now())
  const moved = useRef(false)

  const read = useCallback(() => browserCall<Setup>("GET", "/v1/browser/setup").then(
    (answer) => { setSetup(answer); setReadError(null); return answer },
    (error: unknown) => { setReadError(error); return null },
  ), [])

  useEffect(() => {
    void read()
    const timer = window.setInterval(() => {
      setNow(Date.now())
      if (!document.hidden) void read()
    }, POLL_MS)
    return () => window.clearInterval(timer)
  }, [read])

  const waiting = waitingSince > 0
  useEffect(() => {
    if (!waiting || !setup?.connected || moved.current) return
    moved.current = true
    onConnected()
  }, [waiting, setup?.connected, onConnected])

  const launch = async () => {
    setLaunching(true)
    setLaunchError(null)
    setMissing(false)
    try {
      await browserCall("POST", "/v1/browser/launch", {})
      setWaitingSince(Date.now())
      setNow(Date.now())
      void read()
    } catch (error) {
      if (refusalCode(error) === "browser_chromium_missing") setMissing(true)
      else setLaunchError(error)
    } finally {
      setLaunching(false)
    }
  }

  const already = !!setup?.connected && !waiting
  const slow = waiting && now - waitingSince >= SLOW_MS
  const noChromium = missing || setup?.chromium_found === false
  return (
    <div className="setup-body">
      <h2>{nextWord("browserSetupPrepareTitle")}</h2>
      <p className="setup-lede">{nextWord("browserSetupPrepareBody")}</p>
      <div className="browser-setup-state" aria-live="polite">
        {already && <p className="browser-setup-ok" role="status"><CheckIcon />{nextWord("browserSetupConnected")}</p>}
        {waiting && !setup?.connected && <p className="browser-setup-wait" role="status"><span className="browser-setup-spin" aria-hidden="true" />{nextWord("browserSetupWaiting")}</p>}
      </div>
      {(slow || missing) && (
        <div className="browser-setup-causes">
          <p>{nextWord("browserSetupSlow")}</p>
          <ul>
            {noChromium && (
              <li data-cause="chromium">
                <span>{nextWord("browserSetupNoChromium")}</span>
                <CopyLine text={setup?.install_command || INSTALL_COMMAND} />
              </li>
            )}
            {!missing && (
              <li data-cause="closed">
                <span>{nextWord("browserSetupClosed")}</span>
                <button type="button" className="setup-secondary" disabled={launching} onClick={() => void launch()}>{nextWord("browserSetupLaunchAgain")}</button>
              </li>
            )}
            {waiting && <li data-cause="waiting"><span>{nextWord("browserSetupStillWaiting")}</span></li>}
          </ul>
        </div>
      )}
      {launchError ? <p className="setup-said" role="alert">{said(launchError, "browserSetupLaunchFailed")}</p> : null}
      {readError ? <p className="setup-said" role="alert">{said(readError, "browserSetupReadFailed")}</p> : null}
      <div className="setup-actions">
        <button type="button" className="setup-quiet" onClick={onBack}>{nextWord("browserSetupBack")}</button>
        {already
          ? <button type="button" className="setup-primary" onClick={onConnected}>{nextWord("browserSetupNext")}</button>
          : <button type="button" className="setup-primary" disabled={launching || waiting} onClick={() => void launch()}>
            {missing ? nextWord("browserSetupLaunchAgain") : nextWord("browserSetupLaunch")}
          </button>}
      </div>
    </div>
  )
}

function CopyLine({ text }: { text: string }) {
  const input = useRef<HTMLInputElement>(null)
  const [copied, setCopied] = useState(false)
  const copy = async () => {
    input.current?.select()
    try {
      await navigator.clipboard.writeText(text)
      setCopied(true)
    } catch {
      // Without clipboard access the text stays selected for a manual copy.
      setCopied(false)
    }
  }
  return (
    <span className="browser-setup-copy">
      <input ref={input} readOnly value={text} aria-label={nextWord("browserSetupCopy")} onFocus={(e) => e.currentTarget.select()} />
      <button type="button" className="setup-secondary" onClick={() => void copy()}>{copied ? nextWord("browserSetupCopied") : nextWord("browserSetupCopy")}</button>
    </span>
  )
}

function Confirm({ onBack, onNext }: { onBack: () => void; onNext: () => void }) {
  return (
    <div className="setup-body">
      <h2>{nextWord("browserSetupConfirmTitle")}</h2>
      <div className="setup-card">
        <span className="setup-feature-icon"><KeyIcon /></span>
        <span><span>{nextWord("browserSetupConfirmBody")}</span></span>
      </div>
      <div className="setup-actions">
        <button type="button" className="setup-quiet" onClick={onBack}>{nextWord("browserSetupBack")}</button>
        <button type="button" className="setup-primary" onClick={onNext}>{nextWord("browserSetupNext")}</button>
      </div>
    </div>
  )
}

/** The two machine settings, written only when the person presses Finish. */
function Start({ enabled, notify, onEnabled, onNotify, onBack, onDone }: {
  enabled: boolean
  notify: boolean
  onEnabled: (on: boolean) => void
  onNotify: (on: boolean) => void
  onBack: () => void
  onDone: () => void
}) {
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const finish = async () => {
    setSaving(true)
    setError(null)
    try {
      await writeChromeSettings({ browser_enabled: enabled, browser_notify_new_site: notify })
      onDone()
    } catch (e) {
      setError(e)
    } finally {
      setSaving(false)
    }
  }
  return (
    <div className="setup-body">
      <h2>{nextWord("browserSetupStartTitle")}</h2>
      <div className="browser-setup-switches">
        <Switch id="browser-setup-enabled" on={enabled} onChange={onEnabled} label={nextWord("browserSetupEnable")} />
        <Switch id="browser-setup-notify" on={notify} onChange={onNotify} label={nextWord("browserSetupNotify")} />
      </div>
      {error ? <p className="setup-said" role="alert">{said(error, "browserSetupSaveFailed")}</p> : null}
      <div className="setup-actions">
        <button type="button" className="setup-quiet" onClick={onBack}>{nextWord("browserSetupBack")}</button>
        <button type="button" className="setup-primary" disabled={saving} onClick={() => void finish()}>{nextWord("browserSetupFinish")}</button>
      </div>
    </div>
  )
}

function Switch({ id, on, onChange, label }: { id: string; on: boolean; onChange: (on: boolean) => void; label: string }) {
  return (
    <label className="browser-setup-switch" htmlFor={id}>
      <span>{label}</span>
      <input id={id} type="checkbox" role="switch" checked={on} aria-checked={on} onChange={(e) => onChange(e.currentTarget.checked)} />
      <i aria-hidden="true" />
    </label>
  )
}

function Done({ connected, enabled, notify, onClose }: { connected: boolean; enabled: boolean; notify: boolean; onClose: () => void }) {
  const lines: { on: boolean; word: NextWord }[] = [
    { on: connected, word: "browserSetupDoneConnected" },
    { on: enabled, word: enabled ? "browserSetupDoneEnabled" : "browserSetupDoneDisabled" },
    { on: notify, word: notify ? "browserSetupDoneNotify" : "browserSetupDoneQuiet" },
  ]
  return (
    <div className="setup-body setup-ending">
      <span className="setup-ending-mark"><CheckIcon /></span>
      <h2>{nextWord("browserSetupDoneTitle")}</h2>
      <ul className="setup-summary">
        {lines.map((line) => (
          <li key={line.word} data-done={line.on ? "true" : undefined}>
            {line.on ? <CheckIcon /> : <span className="setup-track-dot" aria-hidden="true" />}
            <span>{nextWord(line.word)}</span>
          </li>
        ))}
      </ul>
      <p className="setup-lede">{nextWord("browserSetupDoneBody")}</p>
      <div className="setup-ending-actions">
        <button type="button" className="setup-primary" onClick={onClose}>{nextWord("browserSetupClose")}</button>
      </div>
    </div>
  )
}

const stroke = { fill: "none", stroke: "currentColor", strokeWidth: 1.8, strokeLinecap: "round", strokeLinejoin: "round" } as const

function CheckIcon() {
  return <svg width="14" height="14" viewBox="0 0 24 24" {...stroke} strokeWidth={2.4} aria-hidden="true"><path d="M5 12.5l4.5 4.5L19 7.5" /></svg>
}
function EyeIcon() {
  return <svg width="20" height="20" viewBox="0 0 24 24" {...stroke} aria-hidden="true"><path d="M2.5 12S6 5.5 12 5.5 21.5 12 21.5 12 18 18.5 12 18.5 2.5 12 2.5 12z" /><circle cx="12" cy="12" r="3" /></svg>
}
function HandIcon() {
  return <svg width="20" height="20" viewBox="0 0 24 24" {...stroke} aria-hidden="true"><path d="M8 11V5.5a1.5 1.5 0 0 1 3 0V10m0-1.5V4.5a1.5 1.5 0 0 1 3 0V10m0-3.5a1.5 1.5 0 0 1 3 0V14a7 7 0 0 1-7 7h-.5a6 6 0 0 1-4.6-2.2L3.6 16a1.6 1.6 0 0 1 2.4-2.1L8 15.5V11" /></svg>
}
function PeopleIcon() {
  return <svg width="20" height="20" viewBox="0 0 24 24" {...stroke} aria-hidden="true"><circle cx="9" cy="8" r="3.2" /><path d="M3 19.5c.6-3.3 3-5.2 6-5.2s5.4 1.9 6 5.2" /><path d="M15.5 4.9a3.2 3.2 0 0 1 0 6.2M18 14.6c1.6.8 2.7 2.4 3 4.9" /></svg>
}
function ListIcon() {
  return <svg width="20" height="20" viewBox="0 0 24 24" {...stroke} aria-hidden="true"><path d="M8 6h12M8 12h12M8 18h12" /><circle cx="4" cy="6" r="1" fill="currentColor" /><circle cx="4" cy="12" r="1" fill="currentColor" /><circle cx="4" cy="18" r="1" fill="currentColor" /></svg>
}
function KeyIcon() {
  return <svg width="20" height="20" viewBox="0 0 24 24" {...stroke} aria-hidden="true"><circle cx="8" cy="15" r="4" /><path d="M11 12l8.5-8.5M16 7l2.5 2.5M14 9l1.8 1.8" /></svg>
}
