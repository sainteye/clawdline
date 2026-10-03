import { useEffect, useState } from "react"
import { useCloudAccount } from "../../cloud/account-context.js"
import { readTerminalPermissionDevice } from "../../cloud/terminal-permission.js"
import { nextWord } from "../../next-strings.js"

type State = "loading" | "send" | "read" | "failed"

/** Terminal access follows the existing send permission and each machine's pairing. */
export function CloudTerminalPermission({ shown }: { shown: boolean }) {
  const account = useCloudAccount()
  const [state, setState] = useState<State>("loading")

  useEffect(() => {
    if (!shown || !account) return
    let live = true
    setState("loading")
    readTerminalPermissionDevice(account.apiOrigin, account.deviceID).then(
      (device) => { if (live) setState(device.caps.includes("send_prompt") ? "send" : "read") },
      () => { if (live) setState("failed") },
    )
    return () => { live = false }
  }, [shown, account?.apiOrigin, account?.deviceID])

  if (!account || !shown) return null
  const status = state === "send" ? "cloudTerminalDefaultSend" :
    state === "read" ? "cloudTerminalDefaultRead" :
      state === "failed" ? "cloudTerminalPermissionConnectionFailed" : "cloudTerminalDefaultLoading"
  return <section className="signed-in cloud-terminal-permission" aria-labelledby="cloud-terminal-permission-title">
    <h2 id="cloud-terminal-permission-title">{nextWord("cloudTerminalPermissionTitle")}</h2>
    <p className="device-help">{nextWord("cloudTerminalDefaultHelp")}</p>
    <p className="devices-status" role="status" aria-live="polite">{nextWord(status)}</p>
  </section>
}
