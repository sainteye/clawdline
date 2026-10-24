import { useEffect, useRef, useState, useSyncExternalStore } from "react"
import type { UpdateStatus } from "@clawdline/contract"
import { catalogWordLanguage } from "../catalog.js"
import { nextWord } from "../next-strings.js"
import { followsRelay } from "../client.js"
import { applyUpdate, checkUpdateNow, currentUpdateRead, publishUpdateRead, refreshUpdate, requestDevRelease, subscribeUpdate } from "./update.js"
import { canRestartStagedApp, restartStagedApp } from "./restart-app.js"
import { INSTALL_COMMAND, UPDATE_BANNER_DISMISSED_KEY, buildName, canStartUpdateAfterFresh, devReleaseSentence, devStrip, updateBannerVersion, updatePanel } from "./update-model.js"
import "./update.css"

function dismissedVersion(): string | null {
  try {
    return localStorage.getItem(UPDATE_BANNER_DISMISSED_KEY)
  } catch {
    return null
  }
}

/**
 * One line at the top of the session list: a newer release, with the press
 * that installs it, or — always, while a dev build runs — which commit this
 * is, how it stands against the release, and the ways on from it. It starts an
 * update directly and stays to say how it went; the words wrap only then.
 */
export function UpdateBanner({ embedded = false }: { embedded?: boolean } = {}) {
  const read = useSyncExternalStore(subscribeUpdate, currentUpdateRead, currentUpdateRead)
  const [dismissed, setDismissed] = useState(dismissedVersion)
  const [target, setTarget] = useState("")
  const [sending, setSending] = useState(false)
  const [following, setFollowing] = useState(false)
  const [completed, setCompleted] = useState(false)
  const [dismissedResult, setDismissedResult] = useState(false)
  const [checkFailed, setCheckFailed] = useState(false)
  const [pressRefused, setPressRefused] = useState<{ code: string; detail: string } | null>(null)
  const [pressOlder, setPressOlder] = useState(false)
  const [releasing, setReleasing] = useState(false)
  const [releaseSaid, setReleaseSaid] = useState("")
  const last = useRef<UpdateStatus | null>(null)
  if (read?.kind === "status") last.current = read.status
  // A fresh check — after a machine switch, or the page waking — answers for the
  // machine now read; what was kept from before is not shown meanwhile.
  else if (read?.kind === "checking") last.current = null

  const availableVersion = updateBannerVersion(read?.kind === "status" ? read.status : null, null)
  const version = availableVersion === dismissed ? null : availableVersion
  const view = updatePanel({
    read, last: last.current, following, sending, pressRefused, pressOlder,
    overCloud: followsRelay(), autoSaving: false, autoFailed: "",
  }, nextWord, (at) => new Date(at).toLocaleString(document.documentElement.lang || undefined))
  const problem = target && (!version || version === target) ? view.problem : null
  const finishedVersion = last.current?.apply?.state === "healthy" ? last.current.apply.to : ""
  // A newer offer retires the previous completion even after the offer is
  // dismissed. Otherwise closing the new release brings the old result back.
  const done = availableVersion && availableVersion !== finishedVersion ? null : view.done
  const showCompleted = completed && (!availableVersion || availableVersion === target)

  useEffect(() => {
    if (completed && availableVersion && availableVersion !== target) setCompleted(false)
  }, [completed, availableVersion, target])

  useEffect(() => {
    if (!following) return
    const timer = setInterval(() => void refreshUpdate(), 2_000)
    return () => clearInterval(timer)
  }, [following])

  useEffect(() => {
    if (!following || read?.kind !== "status") return
    const apply = read.status.apply
    if (apply?.state === "healthy" || (read.status.state === "current" && buildName(read.status.running) === target)) {
      setFollowing(false)
      setCompleted(true)
    } else if (apply?.state === "failed" || apply?.state === "rolled_back") {
      setFollowing(false)
    }
  }, [following, read, target])

  const press = async () => {
    if (sending || following) return
    setSending(true)
    setCheckFailed(false)
    setPressRefused(null)
    setPressOlder(false)
    setCompleted(false)
    setDismissedResult(false)
    setTarget(version || buildName(last.current?.latest))

    // A banner may have been visible since before the release was withdrawn.
    // Recheck its signed source immediately before sending the install command.
    const checked = await checkUpdateNow()
    if (!canStartUpdateAfterFresh(checked, currentUpdateRead())) {
      setSending(false)
      setTarget("")
      if (checked.kind === "unreachable" || checked.kind === "refused") setCheckFailed(true)
      return
    }
    if (checked.kind !== "status") return
    setTarget(buildName(checked.status.latest))
    const answer = await applyUpdate()
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
      // The daemon may already be restarting; its next status settles this.
      setFollowing(true)
      void refreshUpdate()
    }
  }

  const overCloud = followsRelay()
  const dev = devStrip(read?.kind === "status" ? read.status : last.current, overCloud, nextWord)
  const visible = Boolean(dev || version || sending || following || releaseSaid || (!dismissedResult && (
    showCompleted || checkFailed || pressRefused || pressOlder || problem || view.progress || (done && dismissed !== finishedVersion)
  )))
  if (!visible) return null
  const dismiss = () => {
    // Remember the release actually shown, never an offer hidden by a result.
    const dismissedRelease = offer ? version : done ? finishedVersion : ""
    if (dismissedRelease) {
      setDismissed(dismissedRelease)
      try { localStorage.setItem(UPDATE_BANNER_DISMISSED_KEY, dismissedRelease) } catch { /* current page only */ }
    }
    setCompleted(false)
    setDismissedResult(true)
    setCheckFailed(false)
    setPressRefused(null)
    setPressOlder(false)
    setReleaseSaid("")
  }
  const pressRelease = () => {
    if (releasing || !dev) return
    setReleasing(true)
    setReleaseSaid(nextWord("updateDevReleaseSending", { commit: dev.commit }))
    void requestDevRelease().then((answer) => {
      setReleasing(false)
      setReleaseSaid(devReleaseSentence(answer, nextWord))
    })
  }
  const canPress = read?.kind === "status" && (Boolean(version) || view.press.shown) && !sending && !following && !showCompleted
  const busy = Boolean(sending || following || view.progress || releasing)
  const restartVersion = view.staged ? last.current?.apply?.to || "" : ""
  const message = checkFailed ? nextWord("updateIsUnknown")
    : releaseSaid || (dismissedResult ? null : problem?.sentence || view.progress || (sending ? nextWord("updateVerifying", { to: target }) : null)
      || (showCompleted ? done || nextWord("updateHealthy", { to: target }) : null)
      || (done && dismissed !== finishedVersion ? done : null))
  const offer = !message && !sending && !following && !showCompleted && !problem
  const notes = version && read?.kind === "status" ? read.status.latest.notes_url : undefined
  // After a failure or a rollback the press retries, beside the sentence that says what went wrong.
  const pressLabel = problem ? view.press.label : dev?.back || (version ? nextWord("updateNow") : view.press.label)
  const pressShown = canPress && !checkFailed && (!message || Boolean(problem)) &&
    Boolean(problem ? view.press.shown : dev ? dev.back : version)
  // A dev build's line stays; a release's and a result's can be put away.
  const closable = !sending && !following && !releasing && (Boolean(message) || !dev)

  return (
    <div className={"update-strip" + (message ? " is-saying" : "") + (embedded ? " is-embedded" : "")} id={embedded ? undefined : "update-banner"} role="status"
      aria-busy={busy} data-version={version || target} data-dev={dev?.relation}>
      {dev ? (
        <span className={"update-strip-tag" + (dev.relation === "behind" ? " is-behind" : "")} title={nextWord("updateBannerDev", { commit: dev.commit })}
          lang={catalogWordLanguage("next", "updateStripTagDev")}>{nextWord("updateStripTagDev")}</span>
      ) : version && offer ? (
        <span className="update-strip-tag is-new" lang={catalogWordLanguage("next", "updateStripTagNew")}>{nextWord("updateStripTagNew")}</span>
      ) : null}
      <span className="update-strip-say" aria-live={message ? "polite" : undefined}>
        {message ? <>
          {busy ? <span className="update-banner-spinner" aria-hidden="true" /> : null}
          {message}
          {view.staged ? <small className="update-banner-staged">{view.staged}</small> : null}
          {pressOlder ? <code className="update-command">{INSTALL_COMMAND}</code> : null}
        </> : dev ? <>
          <b className="update-strip-commit">{dev.commit}</b>
          {dev.standing ? <> · {dev.standing}</> : null}
        </> : version ? (
          <span lang={catalogWordLanguage("next", "updateBanner")}>{nextWord("updateBanner", { version })}</span>
        ) : null}
      </span>
      {dev?.release && !message ? (
        <button className="update-strip-link is-go" type="button" disabled={releasing} title={nextWord("updateDevRelease")}
          onClick={pressRelease}>{nextWord("updateStripRelease")}</button>
      ) : null}
      {pressShown ? (
        <button className={"update-strip-link" + (dev && dev.relation !== "behind" ? "" : " is-go")} type="button"
          onClick={() => void press()}>{pressLabel}</button>
      ) : null}
      {notes && offer ? (
        <a className="update-strip-link" href={notes} target="_blank" rel="noreferrer">{nextWord("updateNotes")}</a>
      ) : null}
      {restartVersion && canRestartStagedApp() ? (
        <button className="update-strip-link is-go" type="button" onClick={() => restartStagedApp(restartVersion)}>
          {nextWord("updateRestartApp")}
        </button>
      ) : null}
      {closable ? (
        <button className="update-strip-x" type="button" aria-label={nextWord("updateBannerDismiss")} title={nextWord("updateBannerDismiss")} onClick={dismiss}>×</button>
      ) : null}
    </div>
  )
}
