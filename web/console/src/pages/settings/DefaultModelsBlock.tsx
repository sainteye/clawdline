import { catalogWord } from "../../catalog.js"
import { useCallback, useEffect, useState } from "react"
import { RefusalError } from "@clawdline/core"
import * as L from "../../legacy/bridge.js"
import { nextWord } from "../../next-strings.js"
import {
  defaultModelOptions,
  readDefaultModels,
  writeDefaultModels,
  type DefaultModelAssistant,
  type DefaultModelsSnapshot,
} from "./api.js"
import "./default-models.css"

type ModelKey = "codex_default_model" | "claude_default_model" | "codex_default_effort"

function modelOf(snapshot: DefaultModelsSnapshot | null, key: ModelKey): string {
  if (!snapshot) return ""
  return String(snapshot[key] ?? "")
}

/** Machine-wide defaults for sessions Clawdline opens, on every console. */
export function DefaultModelsBlock({ shown }: { shown: boolean }) {
  const [snapshot, setSnapshot] = useState<DefaultModelsSnapshot | null>(null)
  const [busy, setBusy] = useState<ModelKey | "read" | null>(null)
  const [said, setSaid] = useState("")

  const accept = useCallback((answer: DefaultModelsSnapshot) => {
    setSnapshot(answer)
  }, [])

  const read = useCallback(() => {
    setBusy("read")
    setSaid("")
    void readDefaultModels().then(
      (answer) => {
        accept(answer)
        setBusy(null)
      },
      (error: unknown) => {
        setBusy(null)
        setSaid(L.failureSentence(error, { fallback: nextWord("defaultModelsFailed") }))
      },
    )
  }, [accept])

  useEffect(() => {
    if (shown) read()
  }, [shown, read])

  const commit = (key: ModelKey, value: string) => {
    const next = value.trim()
    const before = modelOf(snapshot, key)
    if (next === before || busy) return
    setBusy(key)
    setSaid("")
    void writeDefaultModels({ [key]: next }).then(
      (answer) => {
        accept(answer)
        setBusy(null)
        setSaid(nextWord("defaultModelsSaved"))
      },
      (error: unknown) => {
        setBusy(null)
        setSaid(error instanceof RefusalError && error.code === "invalid_default_effort"
          ? nextWord("defaultEffortInvalid")
          : error instanceof RefusalError && error.code === "invalid_default_model"
          ? nextWord("defaultModelInvalid")
          : L.failureSentence(error, { fallback: nextWord("defaultModelsFailed") }))
      },
    )
  }

  const disabled = !snapshot || busy !== null
  const picker = (assistant: DefaultModelAssistant, key: ModelKey, label: string) => {
    const value = modelOf(snapshot, key)
    return (
      <div className="settings-model-field">
        <label htmlFor={`settings-${assistant}-default-model`}>{label}</label>
        <select
          className="find settings-model-select"
          id={`settings-${assistant}-default-model`}
          value={value}
          disabled={disabled}
          onChange={(event) => commit(key, event.currentTarget.value)}
        >
          {defaultModelOptions(snapshot, assistant, value, nextWord("defaultModelPlaceholder")).map((option) => (
            <option key={option.value} value={option.value}>{option.label}</option>
          ))}
        </select>
      </div>
    )
  }
  return (
    <div className="block" id="settings-default-models" aria-busy={busy !== null}>
      <b id="settings-default-models-title">{nextWord("defaultModelsTitle")}</b>
      <p className="say">{nextWord("defaultModelsHint")}</p>
      {picker("codex", "codex_default_model", nextWord("defaultCodexModel"))}
      <div className="settings-model-field">
        <label htmlFor="settings-codex-default-effort">{nextWord("defaultCodexEffort")}</label>
        <select className="find settings-model-select" id="settings-codex-default-effort"
          value={modelOf(snapshot, "codex_default_effort")} disabled={disabled}
          onChange={(event) => commit("codex_default_effort", event.currentTarget.value)}>
          <option value="">{nextWord("defaultModelPlaceholder")}</option>
          <option value="high">{catalogWord("inline", "6ef7c9b15ecd")}</option><option value="xhigh">{catalogWord("inline", "b5255978be8e")}</option>
        </select>
      </div>
      {picker("claude", "claude_default_model", nextWord("defaultClaudeModel"))}
      {snapshot ? null : (
        <button className="chip" type="button" disabled={busy === "read"} onClick={read}>
          {nextWord("defaultModelsRetry")}
        </button>
      )}
      <p className="said" role="status" aria-live="polite">{said}</p>
    </div>
  )
}
