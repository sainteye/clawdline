import { useEffect, useRef, useState } from "react"
import { useCloudAccount } from "../../cloud/account-context.js"
import { changeTerminalPermission, readTerminalPermissionDevice, TerminalPermissionError, type TerminalPermissionDevice } from "../../cloud/terminal-permission.js"
import { nextWord } from "../../next-strings.js"

type State = { kind: "loading" } | { kind: "ready"; device: TerminalPermissionDevice } |
  { kind: "failed"; why: string } | { kind: "unknown" }

function failureWord(code: string): string {
  if (code === "device_unavailable") return nextWord("cloudTerminalPermissionDeviceMissing")
  if (code === "terminal_control_unavailable") return nextWord("cloudTerminalPermissionUnavailable")
  if (code === "stale_capability_epoch") return nextWord("cloudTerminalPermissionChangedElsewhere")
  if (code === "permission_unconfirmed") return nextWord("cloudTerminalPermissionNotApplied")
  return nextWord("cloudTerminalPermissionConnectionFailed")
}

/** Only the currently signed-in browser may be changed here. The account API owns the epoch. */
export function CloudTerminalPermission({ shown }: { shown: boolean }) {
  const account = useCloudAccount()
  const [state, setState] = useState<State>({ kind: "loading" })
  const [asking, setAsking] = useState(false)
  const [busy, setBusy] = useState(false)
  const [said, setSaid] = useState("")
  const [needsReload, setNeedsReload] = useState(false)
  const mainButton = useRef<HTMLButtonElement>(null)
  const confirmButton = useRef<HTMLButtonElement>(null)
  const retryButton = useRef<HTMLButtonElement>(null)
  const returnFocus = useRef(false)

  const read = async (): Promise<TerminalPermissionDevice> => {
    if (!account) throw new Error("no_account")
    return readTerminalPermissionDevice(account.apiOrigin, account.deviceID)
  }

  useEffect(() => {
    if (!shown || !account) return
    let live = true
    setState({ kind: "loading" })
    setAsking(false)
    setSaid("")
    read().then((device) => { if (live) setState({ kind: "ready", device }) },
      (error) => { if (live) setState({ kind: "failed", why: String((error as Error).message) }) })
    return () => { live = false }
  }, [shown, account?.apiOrigin, account?.deviceID])

  useEffect(() => {
    if (asking) confirmButton.current?.focus()
    else if (returnFocus.current) {
      (mainButton.current ?? retryButton.current)?.focus()
      returnFocus.current = false
    }
  }, [asking, state])

  if (!account || !shown) return null
  const enabled = state.kind === "ready" && state.device.caps.includes("terminal_control")

  const retry = async () => {
    if (busy) return
    setBusy(true)
    returnFocus.current = true
    try {
      setState({ kind: "ready", device: await read() })
      setSaid("")
    } catch (error) {
      setState({ kind: "failed", why: String((error as Error).message) })
    } finally {
      setBusy(false)
    }
  }

  const change = async () => {
    if (busy || state.kind !== "ready") return
    setBusy(true)
    setSaid("")
    const want = !enabled
    try {
      const device = await changeTerminalPermission(account.apiOrigin, account.deviceID, want)
      setState({ kind: "ready", device })
      setNeedsReload(want)
      setSaid(nextWord(want ? "cloudTerminalPermissionChangedOn" : "cloudTerminalPermissionChangedOff"))
    } catch (error) {
      if (error instanceof TerminalPermissionError && error.device) {
        setState({ kind: "ready", device: error.device })
        setSaid(failureWord(error.message))
      } else if (error instanceof TerminalPermissionError && error.message === "permission_unknown") {
        setState({ kind: "unknown" })
        setSaid("")
      } else {
        setState({ kind: "failed", why: String((error as Error).message) })
      }
    } finally {
      returnFocus.current = true
      setAsking(false)
      setBusy(false)
    }
  }

  const status = busy ? nextWord("cloudTerminalPermissionChanging") : said ||
    (state.kind === "ready" ? nextWord(enabled ? "cloudTerminalPermissionOn" : "cloudTerminalPermissionOff") :
      state.kind === "unknown" ? nextWord("cloudTerminalPermissionUnknown") :
        state.kind === "failed" ? failureWord(state.why) : nextWord("terminalGrantReading"))

  return <section className="signed-in cloud-terminal-permission" aria-labelledby="cloud-terminal-permission-title">
    <h2 id="cloud-terminal-permission-title">{nextWord("cloudTerminalPermissionTitle")}</h2>
    <p className="device-help">{nextWord("cloudTerminalPermissionHelp")}</p>
    <p className="devices-status" role={state.kind === "unknown" ? "alert" : "status"} aria-live="polite">{status}</p>
    {state.kind === "ready" && <>
      {asking ? <div className="signed-in-ask" role="group" aria-label={nextWord("cloudTerminalPermissionTitle")}>
        <p>{nextWord(enabled ? "cloudTerminalPermissionRemoveAsk" : "cloudTerminalPermissionAllowAsk")}</p>
        <button ref={confirmButton} className={"device-start" + (enabled ? "" : " signed-in-danger")} type="button" disabled={busy} onClick={change}>
          {nextWord(enabled ? "cloudTerminalPermissionRemove" : "cloudTerminalPermissionAllow")}
        </button>
        <button className="device-start" type="button" disabled={busy} onClick={() => { returnFocus.current = true; setAsking(false) }}>
          {nextWord("signedInCancel")}
        </button>
      </div> : <button ref={mainButton} className="device-start" type="button" disabled={busy} onClick={() => setAsking(true)}>
        {nextWord(enabled ? "cloudTerminalPermissionRemove" : "cloudTerminalPermissionAllow")}
      </button>}
      {enabled && needsReload && <button className="device-start" type="button" onClick={() => location.reload()}>
        {nextWord("terminalReload")}
      </button>}
    </>}
    {(state.kind === "failed" || state.kind === "unknown") && <button ref={retryButton} className="device-start" type="button" disabled={busy} onClick={retry}>
      {nextWord("terminalRetry")}
    </button>}
  </section>
}
