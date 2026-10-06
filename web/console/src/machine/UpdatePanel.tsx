import { useEffect, useRef, useState, useSyncExternalStore } from "react"
import type { UpdateStatus } from "@clawdline/contract"
import * as L from "../legacy/bridge.js"
import { followsRelay } from "../client.js"
import { nextWord } from "../next-strings.js"
import { writeSettings } from "../pages/settings/api.js"
import { useMachineVersion } from "./NeedsUpdate.js"
import { needsUpdateWords } from "./needs-update-model.js"
import { applyUpdate, currentUpdateRead, publishUpdateRead, refreshUpdate, servedConsoleChanged, subscribeUpdate } from "./update.js"
import { applyMoving, buildName, shouldLookForNewConsole, updatePanel } from "./update-model.js"
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
      <p className="say" id="settings-update-notice" role="status">
        {view.legacyLine}
      </p>
    )
  }
  if (view.kind === "older") {
    const words = needsUpdateWords(null, known, nextWord)
    return (
      <p className="say" id="settings-update-notice" role="status" data-needs-update="update-apply">
        {words.sentence}
        {words.version ? <> {words.version}</> : null}
      </p>
    )
  }

  const running = last.current ? buildName(last.current.running) : ""
  return (
    <div className="block settings-update" id="settings-update" aria-busy={sending || view.restarting}>
      <b id="settings-update-title">{nextWord("updateTitle")}</b>
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
      {view.stateLine ? <p className="say">{view.stateLine}</p> : null}
      {view.sourceNote ? <p className="say update-source">{view.sourceNote}</p> : null}
      {view.press.shown ? (
        <div className="row">
          <button className="chip update-now" id="settings-update-now" type="button" disabled={!view.press.enabled} onClick={press}>
            {view.press.label}
          </button>
        </div>
      ) : null}
      <p className={view.restarting ? "said calm update-progress" : "said update-progress"} role="status" aria-live="polite">
        {reloading ? nextWord("updateReloading", { to: running }) : view.progress ?? ""}
      </p>
      {view.problem ? <p className="update-problem" role="alert">{view.problem}</p> : null}
      {view.staged ? <p className="say">{view.staged}</p> : null}
      {view.auto.shown ? (
        <div className="update-auto">
          <div>
            <strong id="settings-update-auto-title">{nextWord("updateAuto")}</strong>
            <p className="say">{nextWord("updateAutoSay")}</p>
          </div>
          <button
            className={view.auto.on ? "chip on" : "chip"}
            id="settings-update-auto"
            type="button"
            aria-pressed={view.auto.on ? "true" : "false"}
            aria-labelledby="settings-update-auto-title settings-update-auto"
            disabled={!view.auto.enabled}
            onClick={toggleAuto}
          >
            {view.auto.label}
          </button>
        </div>
      ) : null}
      {view.auto.note ? <p className="said" role="status">{view.auto.note}</p> : null}
    </div>
  )
}
