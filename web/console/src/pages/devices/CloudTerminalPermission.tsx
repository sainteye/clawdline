import { useEffect, useState } from "react"
import { useCloudAccount } from "../../cloud/account-context.js"
import { nextWord } from "../../next-strings.js"
import { changeTerminalPermission, readTerminalPermissionDevice, type TerminalPermissionDevice } from "../../cloud/terminal-permission.js"

type State = { kind: "loading" } | { kind: "ready"; device: TerminalPermissionDevice } | { kind: "failed"; why: string }

/** Only the currently signed-in browser may be changed here. The account API owns the epoch. */
export function CloudTerminalPermission({ shown }: { shown: boolean }) {
  const account = useCloudAccount()
  const [state, setState] = useState<State>({ kind: "loading" })
  const [asking, setAsking] = useState(false)
  const [busy, setBusy] = useState(false)
  const [said, setSaid] = useState("")

  const read = async (): Promise<TerminalPermissionDevice> => {
    if (!account) throw new Error("no_account")
    return readTerminalPermissionDevice(account.apiOrigin, account.deviceID)
  }

  useEffect(() => {
    if (!shown || !account) return
    let live = true
    setState({ kind: "loading" })
    setAsking(false)
    read().then((device) => { if (live) setState({ kind: "ready", device }) },
      (error) => { if (live) setState({ kind: "failed", why: String((error as Error).message) }) })
    return () => { live = false }
  }, [shown, account?.apiOrigin, account?.deviceID])

  if (!account || !shown) return null
  const enabled = state.kind === "ready" && state.device.caps.includes("terminal_control")

  const change = async () => {
    if (busy || state.kind !== "ready") return
    setBusy(true)
    setSaid("")
    try {
      setState({ kind: "ready", device: await changeTerminalPermission(account.apiOrigin, account.deviceID, !enabled) })
      setSaid(nextWord(enabled ? "cloudTerminalPermissionOff" : "cloudTerminalPermissionOn"))
    } catch (error) {
      setState({ kind: "failed", why: String((error as Error).message) })
    } finally {
      setAsking(false)
      setBusy(false)
    }
  }

  return <section className="signed-in cloud-terminal-permission" aria-labelledby="cloud-terminal-permission-title">
    <h2 id="cloud-terminal-permission-title">{nextWord("cloudTerminalPermissionTitle")}</h2>
    <p className="device-help">{nextWord("cloudTerminalPermissionHelp")}</p>
    {state.kind === "loading" && <p role="status">{nextWord("terminalGrantReading")}</p>}
    {state.kind === "failed" && <p role="alert">{nextWord("cloudTerminalPermissionFailed", { why: state.why })}</p>}
    {state.kind === "ready" && <>
      <p role="status">{said || nextWord(enabled ? "cloudTerminalPermissionOn" : "cloudTerminalPermissionOff")}</p>
      {asking ? <div className="signed-in-ask" role="group" aria-label={nextWord("cloudTerminalPermissionTitle")}>
        <p>{nextWord("cloudTerminalPermissionAsk")}</p>
        <button className="device-start" type="button" disabled={busy} onClick={change}>{nextWord(enabled ? "cloudTerminalPermissionRemove" : "cloudTerminalPermissionAllow")}</button>
        <button className="device-start" type="button" disabled={busy} onClick={() => setAsking(false)}>{nextWord("signedInCancel")}</button>
      </div> : <button className="device-start" type="button" disabled={busy} onClick={() => setAsking(true)}>
        {nextWord(enabled ? "cloudTerminalPermissionRemove" : "cloudTerminalPermissionAllow")}
      </button>}
    </>}
    {state.kind === "failed" && <button className="device-start" type="button" onClick={() => read().then((device) => setState({ kind: "ready", device }),
      (error) => setState({ kind: "failed", why: String((error as Error).message) }))}>{nextWord("terminalRetry")}</button>}
  </section>
}
