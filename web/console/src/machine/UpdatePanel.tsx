import { useEffect, useRef, useState, useSyncExternalStore } from "react"
import type { UpdateStatus } from "@clawdline/contract"
import { catalogWordLanguage } from "../catalog.js"
import * as L from "../legacy/bridge.js"
import { followsRelay } from "../client.js"
import { nextWord } from "../next-strings.js"
import { writeSettings } from "../pages/settings/api.js"
import { Switch } from "../pages/settings/Switch.js"
import { useMachineVersion } from "./NeedsUpdate.js"
import { needsUpdateWords } from "./needs-update-model.js"
import { applyUpdate, currentUpdateRead, publishUpdateRead, refreshUpdate, servedConsoleChanged, subscribeUpdate } from "./update.js"
import {
  applyMoving,
  asksForUpdatePanel,
  buildName,
  detailsText,
  shouldLookForNewConsole,
  updatePanel,
  type UpdateDetails,
} from "./update-model.js"
import "./update.css"

/** An RFC3339 time as the reader's own clock and language print it. */
function when(iso: string): string {
  const at = new Date(iso)
  if (Number.isNaN(at.getTime())) return iso
  return at.toLocaleString(document.documentElement.lang || undefined, {
    year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit",
  })
}

/**
 * 「技術細節」: the code and the daemon's sentence behind a plain explanation,
 * folded away, with a button that copies them for a report.
 */
function TechnicalDetails({ details }: { details: UpdateDetails }) {
  const [copied, setCopied] = useState<"" | "yes" | "no">("")
  const text = detailsText(details)
  const copy = () => {
    const done = (ok: boolean) => setCopied(ok ? "yes" : "no")
    try {
      void navigator.clipboard.writeText(text).then(() => done(true), () => done(false))
    } catch {
      done(false)
    }
  }
  return (
    <details className="update-details">
      <summary lang={catalogWordLanguage("next", "updateDetails")}>{nextWord("updateDetails")}</summary>
      <pre className="update-details-text" lang="en">{text}</pre>
      <div className="update-details-row">
        <button className="chip" type="button" onClick={copy} lang={catalogWordLanguage("next", "updateCopy")}>{nextWord("updateCopy")}</button>
        <span className="update-details-said" role="status" lang={catalogWordLanguage("next", copied === "yes" ? "updateCopied" : "updateCopyFailed")}>
          {copied === "yes" ? nextWord("updateCopied") : copied === "no" ? nextWord("updateCopyFailed") : ""}
        </span>
      </div>
    </details>
  )
}

/**
 * Scroll to the panel and put the keyboard on its title, once per arrival by
 * an address that asks for it (`SETTINGS_UPDATE_HREF`). The panel appears only
 * after its first read, so this waits for it; the address is then put back to
 * plain `#page=settings`, so the same link followed again is a new arrival.
 */
export function landOnUpdatePanel(): boolean {
  if (!asksForUpdatePanel(location.hash)) return false
  const target = document.getElementById("settings-update-title") ?? document.getElementById("settings-update-notice")
  if (!target || target.closest("[hidden]")) return false
  target.scrollIntoView({ block: "start", behavior: "smooth" })
  target.focus({ preventScroll: true })
  try {
    history.replaceState(history.state, "", "#page=settings")
  } catch {
    /* the address keeps asking; landing again is harmless */
  }
  return true
}

/**
 * This machine's Clawdline release on the Settings page, and the one button
 * that updates it (docs/updates.md). It replaces the read-only notice that
 * stood here, and for a daemon older than release installs it still says
 * exactly what that notice said.
 *
 * The update itself runs on the machine; this panel presses it and then reads
 * `GET /v1/update` every two seconds until it settles. The daemon restarts in
 * the middle of that, and for a few seconds nothing answers: that silence is
 * drawn as the restart, never as a failure. When the new daemon answers and
 * serves a console this page is not running, the page reloads itself — the
 * person asked for the new version a moment ago, on this page.
 */
export function UpdatePanel({ shown }: { shown: boolean }) {
  const read = useSyncExternalStore(subscribeUpdate, currentUpdateRead, currentUpdateRead)
  const last = useRef<UpdateStatus | null>(null)
  if (read?.kind === "status") last.current = read.status
  const [following, setFollowing] = useState(false)
  const [sending, setSending] = useState(false)
  const [pressRefused, setPressRefused] = useState<{ code: string; detail: string } | null>(null)
  const [pressOlder, setPressOlder] = useState(false)
  const [autoSaving, setAutoSaving] = useState(false)
  const [autoFailed, setAutoFailed] = useState("")
  const [reloading, setReloading] = useState(false)
  const looked = useRef(false)
  const known = useMachineVersion()
  const overCloud = followsRelay()

  const view = updatePanel(
    { read, last: last.current, following, sending, pressRefused, pressOlder, overCloud, autoSaving, autoFailed },
    nextWord,
    when,
  )

  // An update somebody else started — the machine's CLI, or auto-apply — is
  // followed the same way once this page sees it moving.
  useEffect(() => {
    if (read?.kind === "status" && applyMoving(read.status)) setFollowing(true)
  }, [read])

  // Read on arrival, and again at the pace the view asks for while the page is
  // shown or an update is being followed.
  useEffect(() => {
    if (!shown && !following) return
    void refreshUpdate()
    const timer = setInterval(() => void refreshUpdate(), view.everyMs)
    return () => clearInterval(timer)
  }, [shown, following, view.everyMs])

  // Arrived by 「前往設定更新」: land on the panel once it is drawn, and again
  // whenever the same link is followed while the page is open.
  const [asked, setAsked] = useState(() => asksForUpdatePanel(location.hash))
  useEffect(() => {
    const listen = () => setAsked(asksForUpdatePanel(location.hash))
    window.addEventListener("hashchange", listen)
    return () => window.removeEventListener("hashchange", listen)
  }, [])
  useEffect(() => {
    if (!shown || !asked || view.kind === "nothing") return
    if (landOnUpdatePanel()) setAsked(false)
  }, [shown, asked, view.kind])

  // The new daemon answered healthy: if it serves a console this page is not,
  // load it. Asked once per update.
  useEffect(() => {
    if (looked.current || !shouldLookForNewConsole({ following, overCloud, read })) return
    looked.current = true
    void servedConsoleChanged().then((changed) => {
      if (!changed) return
      setReloading(true)
      setTimeout(() => location.reload(), 600)
    })
  }, [following, overCloud, read])

  const press = () => {
    if (sending) return
    setSending(true)
    setPressRefused(null)
    looked.current = false
    void applyUpdate().then((answer) => {
      setSending(false)
      if (answer.kind === "started") {
        setFollowing(true)
        if (answer.status) publishUpdateRead({ kind: "status", status: answer.status })
        else void refreshUpdate()
      } else if (answer.kind === "older") {
        setPressOlder(true)
      } else if (answer.kind === "refused") {
        setPressRefused({ code: answer.code, detail: answer.detail })
      } else {
        // Nothing answered the press itself. The update may or may not have
        // started; the next reads say which, and a restart reads as one.
        setFollowing(true)
        void refreshUpdate()
      }
    })
  }

  const toggleAuto = () => {
    if (autoSaving || !view.auto.enabled) return
    const next = !view.auto.on
    setAutoSaving(true)
    setAutoFailed("")
    void writeSettings({ update_auto_apply: next }).then(
      () => {
        setAutoSaving(false)
        void refreshUpdate()
      },
      (error: unknown) => {
        setAutoSaving(false)
        setAutoFailed(L.failureSentence(error, nextWord("updateAutoFailed")))
      },
    )
  }

  if (view.kind === "nothing") return null
  if (view.kind === "legacy") {
    return (
      <p className="say" id="settings-update-notice" role="status" tabIndex={-1}>
        {view.legacyLine}
      </p>
    )
  }
  if (view.kind === "older") {
    const words = needsUpdateWords(null, known, nextWord)
    return (
      <p className="say" id="settings-update-notice" role="status" tabIndex={-1} data-needs-update="update-apply" lang={catalogWordLanguage("next", "machineNeedsUpdate")}>
        {words.sentence}
        {words.version ? <> {words.version}</> : null}
      </p>
    )
  }

  const running = last.current ? buildName(last.current.running) : ""
  const pressButton = view.press.shown ? (
    <div className="row">
      <button className="chip update-now" id="settings-update-now" type="button" disabled={!view.press.enabled} onClick={press}>
        {view.press.label}
      </button>
    </div>
  ) : null
  const problem = view.problem
  return (
    <div className="block settings-update" id="settings-update" aria-busy={sending || view.restarting} lang={catalogWordLanguage("next", "updateTitle")}>
      <b id="settings-update-title" tabIndex={-1} lang={catalogWordLanguage("next", "updateTitle")}>{nextWord("updateTitle")}</b>
      {view.facts.length > 0 ? (
        <dl className="update-facts">
          {view.facts.map((fact) => (
            <div className="update-fact" key={fact.key} data-fact={fact.key}>
              <dt>{fact.label}</dt>
              <dd>
                <span className="update-value">{fact.value}</span>
                {fact.href ? (
                  <>
                    {" "}
                    <a href={fact.href} target="_blank" rel="noopener noreferrer">{fact.hrefLabel}</a>
                  </>
                ) : null}
              </dd>
            </div>
          ))}
        </dl>
      ) : null}
      {view.done ? <p className="say update-done">{view.done}</p> : null}
      {view.stateLine ? <p className="say update-state">{view.stateLine}</p> : null}
      {view.stateDetails ? <TechnicalDetails details={view.stateDetails} /> : null}
      {view.sourceNote ? <p className="say update-source">{view.sourceNote}</p> : null}
      {view.press.below ? null : pressButton}
      <p className="said calm update-progress" role="status" aria-live="polite">
        {reloading ? nextWord("updateReloading", { to: running }) : view.progress ?? ""}
      </p>
      {problem ? (
        <div className="update-problem" data-problem={problem.kind} role="alert">
          <p>{problem.sentence}</p>
          {problem.command ? <code className="update-command">{problem.command}</code> : null}
          {problem.details ? <TechnicalDetails details={problem.details} /> : null}
        </div>
      ) : null}
      {view.press.below ? pressButton : null}
      {view.staged ? <p className="say">{view.staged}</p> : null}
      {view.auto.shown ? (
        <div className="update-auto">
          <div>
            <strong id="settings-update-auto-title" lang={catalogWordLanguage("next", "updateAuto")}>{nextWord("updateAuto")}</strong>
            <p className="say" id="settings-update-auto-say">
              <span lang={catalogWordLanguage("next", "updateAutoSay")}>{nextWord("updateAutoSay")}</span>
              {view.auto.beta ? <> {view.auto.beta}</> : null}
            </p>
          </div>
          <Switch
            id="settings-update-auto"
            labelledBy="settings-update-auto-title"
            describedBy="settings-update-auto-say"
            on={view.auto.on}
            stateText={view.auto.state}
            disabled={!view.auto.enabled}
            onToggle={toggleAuto}
          />
        </div>
      ) : null}
      {view.auto.note ? <p className="said" role="status">{view.auto.note}</p> : null}
    </div>
  )
}
