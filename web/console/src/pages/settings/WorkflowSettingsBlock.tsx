import { LocalMachineSetup } from "../../cloud/LocalMachineSetup.js"
import { machineManager } from "../../cloud/machine-manager.js"
import { useEffect, useState } from "react"
import type { WorkflowSettingsSnapshot } from "@clawdline/contract"
import { catalogFormat, catalogWord } from "../../catalog.js"
import * as L from "../../legacy/bridge.js"
import { readWorkflowSettings, writeWorkflowSettings } from "./api.js"
import { Switch } from "./Switch.js"
import "./workflow-settings.css"
import { BrowserPairingBlock } from "./BrowserPairingBlock.js"

type Key = keyof WorkflowSettingsSnapshot
const word = (key: string) => catalogWord("workflowSettings", key)

export function WorkflowSettingsBlock({ shown }: { shown: boolean }) {
  const [snapshot, setSnapshot] = useState<WorkflowSettingsSnapshot | null>(null)
  const [busy, setBusy] = useState(false)
  const [said, setSaid] = useState("")
  const [open, setOpen] = useState<Key | null>(null)
  const [retry, setRetry] = useState(0)
  useEffect(() => {
    if (!shown) return
    let live = true
    setSnapshot(null)
    setBusy(true)
    setSaid("")
    void readWorkflowSettings().then(
      value => { if (live) { setSnapshot(value); setBusy(false) } },
      error => { if (live) { setSaid(L.failureSentence(error, word("readFailed"))); setBusy(false) } },
    )
    return () => { live = false }
  }, [shown, retry])

  const toggle = async (key: Key) => {
    if (!snapshot || busy) return
    setBusy(true)
    setSaid("")
    try {
      setSnapshot(await writeWorkflowSettings({ [key]: !snapshot[key] }))
      setSaid(word("saved"))
    } catch (error) { setSaid(L.failureSentence(error, word("saveFailed"))) }
    finally { setBusy(false) }
  }

  return <div className="block settings-workflow" aria-busy={busy}>
    <b>{word("tab")}</b>
    <p className="say">{word("introWithITerm")}</p>
    {(["dispatch_full_permission", "session_auto_discovery", "iterm_integration"] as const).map(key => {
      const id = "workflow-" + key
      const title = word(key + "Title")
      const expanded = open === key
      const unsupported = snapshot !== null && key === "iterm_integration" && typeof snapshot[key] !== "boolean"
      return <div className="workflow-setting" key={key}>
        <div className="workflow-setting-row">
          <div className="workflow-setting-label">
            <strong id={id + "-title"}>{title}</strong>
            <button type="button" className="workflow-info" aria-label={catalogFormat("workflowSettings", "explain", [title])}
              aria-expanded={expanded} aria-controls={id + "-help"} onClick={() => setOpen(expanded ? null : key)}>
              <span aria-hidden="true">ⓘ</span>
            </button>
          </div>
          {unsupported ? <span className="say" role="status">{word("itermUpdateRequired")}</span> : <Switch labelledBy={id + "-title"} describedBy={expanded ? id + "-help" : undefined}
            on={snapshot?.[key] === true} disabled={!snapshot || busy}
            stateText={snapshot ? word(snapshot[key] ? "on" : "off") : word("loading")}
            onToggle={() => void toggle(key)} />}
        </div>
        <div id={id + "-help"} className="workflow-setting-help" hidden={!expanded}>
          <p><strong>{word("whenOn")}</strong> {word(key + "On")}</p>
          <p><strong>{word("whenOff")}</strong> {word(key + "Off")}</p>
          <p>{word(key + "Scope")}</p>
        </div>
      </div>
    })}
    <p className="said" role="status" aria-live="polite">{said}</p>
    {!snapshot && !busy && <button type="button" className="chip" onClick={() => setRetry(value => value + 1)}>{word("retry")}</button>}
    {shown && <LocalMachineSetup onPair={machine => machineManager()?.pair(machine.id)}>
      <BrowserPairingBlock shown={shown} />
    </LocalMachineSetup>}
  </div>
}
