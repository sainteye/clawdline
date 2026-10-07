import { useEffect, useLayoutEffect, useRef, useState } from "react"
import { nextWord } from "../next-strings.js"
import { DeviceLimitRun, knownTier, type DeviceLimitState, type RecoveryDevice, type RecoverySession } from "./device-limit.js"

/**
 * The card for an account that has no viewer-device slot left
 * (`device-limit.ts`): what the limit is, the devices using it, and a way to
 * remove one so this browser can carry on signing in. Every state it can be in
 * has a button on it, because a card with none is a dead end for a browser
 * that has no other page to go to (docs/records/first-run-audit.md B12).
 */
export function DeviceLimitPanel(props: {
  session: (RecoverySession & { signInURL(): string }) | null
  tier: string
  limit: number | null
  /** Ask for the session again; the gate's own start. */
  onContinue: () => void
  signInLabel: string
}) {
  const { session, tier, limit, onContinue, signInLabel } = props
  const [state, setState] = useState<DeviceLimitState>({ phase: "loading" })
  const run = useRef<DeviceLimitRun | null>(null)
  const keep = useRef<HTMLButtonElement>(null)
  useEffect(() => {
    if (!session) return
    const next = new DeviceLimitRun(session, { onState: setState, continueSignIn: onContinue, tier, limit })
    run.current = next
    void next.load()
    return () => {
      next.stop()
      if (run.current === next) run.current = null
    }
  }, [session, tier, limit, onContinue])
  const asking = state.phase === "listed" ? state.asking : null
  // The safe answer holds the focus, as on the machine list's Forget question.
  useLayoutEffect(() => {
    if (asking) keep.current?.focus({ preventScroll: true })
  }, [asking])

  const shownTier = state.phase === "listed" ? state.tier : knownTier(tier)
  const shownLimit = state.phase === "listed" ? state.limit : limit
  const lede =
    shownLimit === null
      ? nextWord("cloudDeviceLimitNoCount")
      : shownTier
        ? nextWord("cloudDeviceLimit", { limit: shownLimit, tier: shownTier })
        : nextWord("cloudDeviceLimitNoTier", { limit: shownLimit })
  const signIn = (
    <button className="go" type="button" id="cloud-device-sign-in" onClick={() => session && location.assign(session.signInURL())}>
      {signInLabel}
    </button>
  )

  function rows(listed: Extract<DeviceLimitState, { phase: "listed" }>) {
    return (
      <ul className="cloud-devices" id="cloud-devices">
        {listed.devices.map((device) => (
          <li key={device.id} data-device={device.id}>
            <span className="cloud-device-name">{device.name}</span>
            <span className="cloud-device-facts">{facts(device)}</span>
            {listed.asking === device.id ? (
              <div className="cloud-device-ask" role="alertdialog" aria-describedby={"cloud-device-ask-" + device.id}>
                <p className="say" id={"cloud-device-ask-" + device.id}>
                  {nextWord("cloudDeviceRemoveAsk", { name: device.name })}
                </p>
                <div className="buttons">
                  <button type="button" className="chip" ref={keep} onClick={() => run.current?.ask(null)}>
                    {nextWord("cloudDeviceRemoveCancel")}
                  </button>
                  <button type="button" className="chip confirm-go" onClick={() => void run.current?.revoke(device.id)}>
                    {nextWord("cloudDeviceRemoveConfirm")}
                  </button>
                </div>
              </div>
            ) : listed.revoking === device.id ? (
              <span className="say calm">{nextWord("cloudDeviceRemoving", { name: device.name })}</span>
            ) : (
              <button
                type="button"
                className="chip cloud-device-remove"
                disabled={listed.revoking !== null}
                onClick={() => run.current?.ask(device.id)}
              >
                {nextWord("cloudDeviceRemove")}
              </button>
            )}
          </li>
        ))}
      </ul>
    )
  }

  function content() {
    switch (state.phase) {
      case "loading":
        return (
          <>
            <p className="say calm" aria-busy="true">{nextWord("cloudDeviceLimitLoading")}</p>
            {/* Waiting has a way out too: the list may never come. */}
            <button className="go" type="button" onClick={onContinue}>
              {nextWord("cloudDeviceLimitContinue")}
            </button>
          </>
        )
      case "list_failed":
        return state.failure.expired ? (
          <>
            <p className="say">{nextWord("cloudDeviceTicketExpired", { code: state.failure.code })}</p>
            {signIn}
          </>
        ) : (
          <>
            <p className="say">{nextWord("cloudDeviceListFailed", { code: state.failure.code })}</p>
            <button className="go" type="button" onClick={() => void run.current?.load()}>
              {nextWord("cloudRetry")}
            </button>
          </>
        )
      case "continuing":
        return <p className="say calm">{nextWord("cloudDeviceRemoved", { name: state.removed.name })}</p>
      case "listed":
        return (
          <>
            {state.failure &&
              (state.failure.expired ? (
                <>
                  <p className="say">{nextWord("cloudDeviceTicketExpired", { code: state.failure.code })}</p>
                  {signIn}
                </>
              ) : (
                <p className="say">{nextWord("cloudDeviceRemoveFailed", { code: state.failure.code })}</p>
              ))}
            {state.devices.length === 0 ? (
              <>
                <p className="say">{nextWord("cloudDeviceLimitEmpty")}</p>
                <button className="go" type="button" onClick={onContinue}>
                  {nextWord("cloudDeviceLimitContinue")}
                </button>
              </>
            ) : (
              <>
                {rows(state)}
                {state.failure && !state.failure.expired && (
                  <button className="go" type="button" onClick={() => void run.current?.load()}>
                    {nextWord("cloudRetry")}
                  </button>
                )}
              </>
            )}
          </>
        )
    }
  }

  return (
    <div className="cloud-device-limit">
      <p className="fine">{lede}</p>
      {session ? content() : signIn}
    </div>
  )
}

function kindWord(kind: string): string {
  switch (kind) {
    case "ios":
      return nextWord("cloudDeviceKindIOS")
    case "android":
      return nextWord("cloudDeviceKindAndroid")
    default:
      return nextWord("cloudDeviceKindBrowser")
  }
}

/** A time the control plane wrote, in the page's language; the raw text if it will not parse. */
function when(at: string): string {
  const parsed = Date.parse(at)
  if (!Number.isFinite(parsed)) return at
  try {
    return new Intl.DateTimeFormat(document.documentElement.lang || undefined, {
      dateStyle: "medium",
      timeStyle: "short",
    }).format(parsed)
  } catch {
    // refusal-ok: a browser refusing a locale is not a control-plane refusal.
    return new Date(parsed).toLocaleString()
  }
}

function facts(device: RecoveryDevice): string {
  const parts = [kindWord(device.kind)]
  if (device.created_at) parts.push(nextWord("cloudDeviceAdded", { time: when(device.created_at) }))
  parts.push(
    device.last_seen_at
      ? nextWord("cloudDeviceSeen", { time: when(device.last_seen_at) })
      : nextWord("cloudDeviceSeenNever"),
  )
  return parts.join(" · ")
}
