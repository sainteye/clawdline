import { GRAPHIFY_SETTING, graphWord } from "../../graphify/graphify.js"
import { useEffect, useRef, useState } from "react"
import { catalogFormat, catalogWord } from "../../catalog.js"
import * as L from "../../legacy/bridge.js"
import { readIntegrationSettings, writeIntegrationSettings, type IntegrationSettingsSnapshot, type IntegrationSettingsRequest } from "./api.js"
import { WORKFLOW_PROFILES, workflowName, workflowWord } from "../../session/workflow-profile.js"
import { Switch } from "./Switch.js"
import "./workflow-settings.css"

const keys = ["graphify_enabled"] as const
type Key = typeof keys[number] | "workflow_skills_enabled"
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

  const save = async (change: IntegrationSettingsRequest) => {
    if (!snapshot || busy) return
    const started = generation.current
    setBusy(true); setSaid("")
    try {
      const saved = await writeIntegrationSettings(change)
      if ("graphify_enabled" in change) window.dispatchEvent(new CustomEvent(GRAPHIFY_SETTING, {detail: saved.graphify_enabled === true}))
      if (started === generation.current) { setSnapshot(saved); setSaid("graphify_enabled" in change ? graphWord("saved") : word("saved")) }
    }
    catch (error) { if (started === generation.current) setSaid(L.failureSentence(error, word("saveFailed"))) }
    finally { if (started === generation.current) setBusy(false) }
  }

  return <div className="block settings-workflow settings-integrations" aria-busy={busy}>
    <b>{word("title")}</b>
    <p className="say">{workflowWord("intro")}</p>
    <div className="workflow-setting">
      <div className="workflow-setting-row">
        <div className="workflow-setting-label">
          <strong id="workflow-profile-title">{workflowWord("title")}</strong>
          <button type="button" className="workflow-info" aria-label={catalogFormat("integrationSettings", "explain", [workflowWord("title")])}
            aria-expanded={open === "workflow_skills_enabled"} aria-controls="workflow-profile-help"
            onClick={() => setOpen(open === "workflow_skills_enabled" ? null : "workflow_skills_enabled")}><span aria-hidden="true">ⓘ</span></button>
        </div>
        {typeof snapshot?.workflow_profile === "string" ? <span className="workflow-profile-picker">
          <select id="workflow-profile-select" aria-labelledby="workflow-profile-title" disabled={busy}
            aria-describedby={open === "workflow_skills_enabled" ? "workflow-profile-help" : undefined}
            value={snapshot.workflow_profile} onChange={event => void save({ workflow_profile: event.target.value })}>
            {!WORKFLOW_PROFILES.includes(snapshot.workflow_profile as typeof WORKFLOW_PROFILES[number]) &&
              <option value={snapshot.workflow_profile}>{snapshot.workflow_profile}</option>}
            {WORKFLOW_PROFILES.map(id => <option key={id} value={id}>{workflowName(id)}</option>)}
          </select>
          <svg viewBox="0 0 16 16" aria-hidden="true" focusable="false"><path d="m4 6 4 4 4-4" /></svg>
        </span> : <Switch labelledBy="workflow-profile-title" on={snapshot?.workflow_skills_enabled === true}
          disabled={!snapshot || typeof snapshot.workflow_skills_enabled !== "boolean" || busy}
          stateText={snapshot ? word(snapshot.workflow_skills_enabled ? "on" : "off") : word("loading")}
          onToggle={() => void save({ workflow_skills_enabled: !snapshot?.workflow_skills_enabled })} />}
      </div>
      <div id="workflow-profile-help" className="workflow-setting-help" hidden={open !== "workflow_skills_enabled"}>
        <p>{workflowWord("description")}</p>
        {snapshot && typeof snapshot.workflow_profile !== "string" && <p>{workflowWord("legacySettings")}</p>}
        <p>{word("scope")}</p>
      </div>
    </div>
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
            onToggle={() => void save({ [key]: !snapshot?.[key] })} />
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
