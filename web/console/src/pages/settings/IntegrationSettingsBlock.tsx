import { GRAPHIFY_SETTING, graphWord } from "../../graphify/graphify.js"
import { useEffect, useRef, useState } from "react"
import type { IntegrationSettingsSnapshot } from "@clawdline/contract"
import { catalogFormat, catalogWord } from "../../catalog.js"
import * as L from "../../legacy/bridge.js"
import { readIntegrationSettings, writeIntegrationSettings } from "./api.js"
import { Switch } from "./Switch.js"
import "./workflow-settings.css"

const keys = ["workflow_skills_enabled", "graphify_enabled"] as const
type Key = typeof keys[number]
const word = (key: string) => catalogWord("integrationSettings", key)

export function IntegrationSettingsBlock({ shown }: { shown: boolean }) {
  const [snapshot, setSnapshot] = useState<IntegrationSettingsSnapshot | null>(null)
  const [busy, setBusy] = useState(false)
  const [said, setSaid] = useState("")
  const [open, setOpen] = useState<Key | null>(null)
  const [retry, setRetry] = useState(0)
  const generation = useRef(0)
  useEffect(() => {
    if (!shown) return
    generation.current++
    const controller = new AbortController()
    let live = true
    setSnapshot(null); setBusy(true); setSaid("")
    void readIntegrationSettings(controller.signal).then(
      value => { if (live) { setSnapshot(value); setBusy(false) } },
      error => { if (live) { setSaid(L.failureSentence(error, word("readFailed"))); setBusy(false) } },
    )
    return () => { live = false; generation.current++; controller.abort() }
  }, [shown, retry])

  const toggle = async (key: Key) => {
    if (!snapshot || busy) return
    const started = generation.current
    setBusy(true); setSaid("")
    try {
      const saved = await writeIntegrationSettings({ [key]: !snapshot[key] })
      window.dispatchEvent(new CustomEvent(GRAPHIFY_SETTING, {detail: saved.graphify_enabled === true}))
      if (started === generation.current) { setSnapshot(saved); setSaid(key === "graphify_enabled" ? graphWord("saved") : word("saved")) }
    }
    catch (error) { if (started === generation.current) setSaid(L.failureSentence(error, word("saveFailed"))) }
    finally { if (started === generation.current) setBusy(false) }
  }

  return <div className="block settings-workflow settings-integrations" aria-busy={busy}>
    <b>{word("title")}</b>
    <p className="say">{word("intro")}</p>
    {keys.map(key => {
      const id = "integration-" + key
      const title = word(key + "Title")
      const expanded = open === key
      const available = snapshot !== null && typeof snapshot[key] === "boolean"
      return <div className="workflow-setting" key={key}>
        <div className="workflow-setting-row">
          <div className="workflow-setting-label">
            <strong id={id + "-title"}>{title}</strong>
            <button type="button" className="workflow-info" aria-label={catalogFormat("integrationSettings", "explain", [title])}
              aria-expanded={expanded} aria-controls={id + "-help"} onClick={() => setOpen(expanded ? null : key)}>
              <span aria-hidden="true">ⓘ</span>
            </button>
          </div>
          <Switch labelledBy={id + "-title"} describedBy={expanded ? id + "-help" : undefined}
            on={snapshot?.[key] === true} disabled={!available || busy}
            stateText={snapshot ? word(available ? snapshot[key] ? "on" : "off" : "unavailable") : word("loading")}
            onToggle={() => void toggle(key)} />
        </div>
        <div id={id + "-help"} className="workflow-setting-help" hidden={!expanded}>
          <p>{word(key + "Description")}</p>
          <p>{key === "graphify_enabled" ? graphWord("scope") : word("scope")}</p>
        </div>
      </div>
    })}
    <p className="said" role="status" aria-live="polite">{said}</p>
    {!snapshot && !busy && <button type="button" className="chip" onClick={() => setRetry(value => value + 1)}>{word("retry")}</button>}
  </div>
}
