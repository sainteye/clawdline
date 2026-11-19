import { useEffect, useRef, useState } from "react"
import type { WorkflowSettingsRequest, WorkflowSettingsSnapshot } from "@clawdline/contract"
import { catalogFormat, catalogWord } from "../../catalog.js"
import * as L from "../../legacy/bridge.js"
import { readWorkflowSettings, writeWorkflowSettings } from "./api.js"
import { Switch } from "./Switch.js"
import "./workflow-settings.css"
import { ToolSuggestionsSetting } from "../../tool-library/ToolSuggestionsSetting.js"

type Key = Exclude<keyof WorkflowSettingsSnapshot, "terminal">
const terminals = ["auto", "iterm", "iterm_native", "tmux"] as const
const word = (key: string) => catalogWord("workflowSettings", key)

export function WorkflowSettingsBlock({ shown }: { shown: boolean }) {
  const [snapshot, setSnapshot] = useState<WorkflowSettingsSnapshot | null>(null)
  const [busy, setBusy] = useState(false)
  const [said, setSaid] = useState("")
  const [open, setOpen] = useState<Key | null>(null)
  const [retry, setRetry] = useState(0)
  const generation = useRef(0)
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
    return () => { live = false; generation.current++ }
  }, [shown, retry])

  const save = async (change: Partial<WorkflowSettingsRequest>) => {
    if (!snapshot || busy) return
    const started = generation.current
    setBusy(true)
    setSaid("")
    try {
      const saved = await writeWorkflowSettings(change)
      if (started === generation.current) {
        setSnapshot(saved)
        setSaid(word("saved"))
      }
    } catch (error) {
      if (started === generation.current) setSaid(L.failureSentence(error, word("saveFailed")))
    } finally {
      if (started === generation.current) setBusy(false)
    }
  }

  return <div className="block settings-workflow" aria-busy={busy}>
    <b>{word("tab")}</b>
    <p className="say">{word("introWithITerm")}</p>
    {(["dispatch_full_permission", "session_auto_discovery", "iterm_integration", "gemini_step_explanations", "grok_full_permission", "session_todo_auto_take"] as const).map(key => {
      const id = "workflow-" + key
      const title = word(key + "Title")
      const expanded = open === key
      // A daemon from before this setting omits it: say so instead of drawing a switch that cannot be read.
      const unsupported = snapshot !== null && (key === "iterm_integration" || key === "session_todo_auto_take" || key === "grok_full_permission") && typeof snapshot[key] !== "boolean"
      return <div className="workflow-setting" key={key}>
        <div className="workflow-setting-row">
          <div className="workflow-setting-label">
            <strong id={id + "-title"}>{title}</strong>
            <button type="button" className="workflow-info" aria-label={catalogFormat("workflowSettings", "explain", [title])}
              aria-expanded={expanded} aria-controls={id + "-help"} onClick={() => setOpen(expanded ? null : key)}>
              <span aria-hidden="true">ⓘ</span>
            </button>
          </div>
          {unsupported ? <span className="say" role="status">{word(key === "iterm_integration" ? "itermUpdateRequired" : key + "UpdateRequired")}</span> : <Switch labelledBy={id + "-title"} describedBy={expanded ? id + "-help" : undefined}
            on={snapshot?.[key] === true} disabled={!snapshot || busy}
            stateText={snapshot ? word(snapshot[key] ? "on" : "off") : word("loading")}
            onToggle={() => void save({ [key]: !snapshot?.[key] })} />}
        </div>
        <div id={id + "-help"} className="workflow-setting-help" hidden={!expanded}>
          <p><strong>{word("whenOn")}</strong> {word(key + "On")}</p>
          <p><strong>{word("whenOff")}</strong> {word(key + "Off")}</p>
          <p>{word(key + "Scope")}</p>
        </div>
        {key === "iterm_integration" && <>
          <p className="say" id="workflow-terminal-help">{word("terminalKeepIntegration")}</p>
          <div className="workflow-setting-row workflow-terminal-row">
            <label htmlFor="workflow-terminal-select">{word("terminalTitle")}</label>
            {snapshot?.terminal ? <span className="workflow-profile-picker">
              <select id="workflow-terminal-select" aria-describedby="workflow-terminal-help" disabled={busy}
                value={snapshot.terminal} onChange={event => {
                  const choice = terminals.find(value => value === event.target.value)
                  if (choice) void save({ terminal: choice })
                }}>
                {terminals.map(value => <option key={value} value={value}>{word("terminalOption_" + value)}</option>)}
              </select>
              <svg viewBox="0 0 16 16" aria-hidden="true" focusable="false"><path d="m4 6 4 4 4-4" /></svg>
            </span> : snapshot && <span className="say" role="status">{word("terminalUpdateRequired")}</span>}
          </div>
        </>}
      </div>
    })}
    {shown && <ToolSuggestionsSetting />}
    <p className="said" role="status" aria-live="polite">{said}</p>
    {!snapshot && !busy && <button type="button" className="chip" onClick={() => setRetry(value => value + 1)}>{word("retry")}</button>}
  </div>
}
