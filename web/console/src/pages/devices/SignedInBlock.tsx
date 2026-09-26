import { useEffect, useState } from "react"
import type { PairedDevice } from "@clawdline/contract"
import { nextWord } from "../../next-strings.js"
import { readSignedIn, revokeDevice, signOutThisBrowser, type SignedIn } from "./signed-in.js"

function when(unix: number): string {
  return new Date(unix * 1000).toLocaleString(document.documentElement.lang || undefined)
}

/**
 * Who is signed in to this machine directly, below the machine cards: one card
 * per device with a Revoke that asks by name before it acts. Read each time
 * the page is shown.
 *
 * Where this page is looking from is marked, because it is the one row a
 * person must not take away by accident:
 *
 * - In the app window, which holds this machine's own key, a line says that
 *   key is not listed and cannot be revoked here — the daemon refuses it.
 * - In a browser signed in as a device of its own, the daemon will not show
 *   the list at all (403), so the block says this browser is a device and
 *   offers only to sign it out, again after asking.
 *
 * Nothing is drawn on the console Clawdline Cloud serves: the browsers there
 * are the Cloud's, and `clawdline cloud devices` is their list.
 */
export function SignedInBlock({ shown }: { shown: boolean }) {
  const [state, setState] = useState<SignedIn | null>(null)
  const [asking, setAsking] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [said, setSaid] = useState("")

  const reload = () => readSignedIn().then(setState)

  useEffect(() => {
    if (!shown) return
    setAsking(null)
    setSaid("")
    let live = true
    readSignedIn().then((next) => live && setState(next))
    return () => {
      live = false
    }
  }, [shown])

  if (!state) return null

  const revoke = async (device: PairedDevice) => {
    setBusy(true)
    try {
      await revokeDevice(device.id)
      setSaid(nextWord("signedInRevoked", { name: device.name }))
      setAsking(null)
      await reload()
    } catch (error) {
      setSaid(nextWord("signedInRevokeFailed", { name: device.name, why: (error as Error).message }))
    } finally {
      setBusy(false)
    }
  }

  const signOut = async () => {
    setBusy(true)
    try {
      await signOutThisBrowser()
      window.location.reload()
    } catch (error) {
      setSaid(nextWord("signedInSignOutFailed", { why: (error as Error).message }))
      setBusy(false)
    }
  }

  return (
    <div className="signed-in" aria-labelledby="signed-in-title">
      <h2 id="signed-in-title">{nextWord("signedInTitle")}</h2>
      <p className="devices-lede">{nextWord("signedInLede")}</p>
      <p className="devices-status" role="status" aria-live="polite">
        {said}
      </p>
      {state.kind === "failed" && <p className="devices-empty">{nextWord("signedInFailed", { why: state.why })}</p>}
      {state.kind === "device" && (
        <article className="device-card" data-connection="current">
          <p className="device-help">{nextWord("signedInThisBrowser")}</p>
          {asking === "self" ? (
            <div className="signed-in-ask" role="group">
              <p className="device-help">{nextWord("signedInSignOutAsk")}</p>
              <button className="device-start signed-in-danger" type="button" disabled={busy} onClick={signOut}>
                {nextWord("signedInSignOutConfirm")}
              </button>
              <button className="device-start" type="button" disabled={busy} onClick={() => setAsking(null)}>
                {nextWord("signedInCancel")}
              </button>
            </div>
          ) : (
            <button className="device-start" type="button" onClick={() => setAsking("self")}>
              {nextWord("signedInSignOut")}
            </button>
          )}
        </article>
      )}
      {state.kind === "list" && (
        <div className="devices-rows">
          <p className="device-help signed-in-self">{nextWord("signedInThisWindow")}</p>
          {state.list.devices.length === 0 && <p className="devices-empty">{nextWord("signedInNone")}</p>}
          {state.list.devices.map((device) => (
            <article className="device-card" key={device.id} data-device={device.id}>
              <div className="device-card-heading">
                <h3>{device.name}</h3>
                <code>{device.id}</code>
              </div>
              <div className="device-facts">
                <span>{nextWord(device.caps.includes("send") ? "signedInCapsSend" : "signedInCapsRead")}</span>
                <span>{nextWord("signedInSince", { time: when(device.created) })}</span>
                <span>
                  {device.last_seen
                    ? nextWord("signedInLastSeen", { time: when(device.last_seen) })
                    : nextWord("signedInNeverSeen")}
                </span>
              </div>
              {asking === device.id ? (
                <div className="signed-in-ask" role="group">
                  <p className="device-help">{nextWord("signedInRevokeAsk", { name: device.name, id: device.id })}</p>
                  <button
                    className="device-start signed-in-danger"
                    type="button"
                    disabled={busy}
                    onClick={() => revoke(device)}
                  >
                    {nextWord("signedInRevokeConfirm")}
                  </button>
                  <button className="device-start" type="button" disabled={busy} onClick={() => setAsking(null)}>
                    {nextWord("signedInCancel")}
                  </button>
                </div>
              ) : (
                <button
                  className="device-start"
                  type="button"
                  aria-label={nextWord("signedInRevokeOne", { name: device.name })}
                  onClick={() => setAsking(device.id)}
                >
                  {nextWord("signedInRevoke")}
                </button>
              )}
            </article>
          ))}
          {state.list.password && <p className="device-help">{nextWord("signedInPassword")}</p>}
        </div>
      )}
    </div>
  )
}
