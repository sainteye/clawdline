import { useEffect, useState } from "react"
import { useCloudAccount } from "../../cloud/account-context.js"
import { readTerminalPermissionDevice } from "../../cloud/terminal-permission.js"
import { nextWord } from "../../next-strings.js"

type State = "loading" | "send" | "missing" | "failed"

/** Terminal access follows sign-in, each machine's pairing and its Cloud command switch. */
export function CloudTerminalPermission({ shown }: { shown: boolean }) {
  const account = useCloudAccount()
  const [state, setState] = useState<State>("loading")

  useEffect(() => {
    if (!shown || !account) return
    let live = true
    setState("loading")
    readTerminalPermissionDevice(account.apiOrigin, account.deviceID).then(
      () => { if (live) setState("send") },
      (error) => { if (live) setState(error instanceof Error && error.message === "device_unavailable" ? "missing" : "failed") },
    )
    return () => { live = false }
  }, [shown, account?.apiOrigin, account?.deviceID])

  if (!account || !shown) return null
  const status = state === "send" ? "cloudTerminalDefaultSend" :
    state === "missing" ? "cloudTerminalPermissionDeviceMissing" :
    state === "failed" ? "cloudTerminalPermissionConnectionFailed" : "cloudTerminalDefaultLoading"
  return <section className="signed-in cloud-terminal-permission" aria-labelledby="cloud-terminal-permission-title">
    <h2 id="cloud-terminal-permission-title">{nextWord("cloudTerminalPermissionTitle")}</h2>
    <p className="device-help">{nextWord("cloudTerminalDefaultHelp")}</p>
    <p className="devices-status" role="status" aria-live="polite">{nextWord(status)}</p>
  </section>
}
