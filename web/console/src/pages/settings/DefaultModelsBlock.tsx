import { catalogWord } from "../../catalog.js"
import { useCallback, useEffect, useState } from "react"
import { RefusalError } from "@clawdline/core"
import * as L from "../../legacy/bridge.js"
import { nextWord } from "../../next-strings.js"
import { ChoiceMenu } from "../../ui-language/ChoiceMenu.js"
import {
  defaultModelOptions,
  readDefaultModels,
  writeDefaultModels,
  type DefaultModelAssistant,
  type DefaultModelsSnapshot,
} from "./api.js"
import "./default-models.css"

type ModelKey = "codex_default_model" | "claude_default_model" | "codex_default_effort" | "antigravity_default_model" | "antigravity_default_effort"

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
          ? (key === "antigravity_default_effort" ? catalogWord("next", "defaultAntigravityEffortInvalid") : nextWord("defaultEffortInvalid"))
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
        <div className="settings-model-label">{label}</div>
        <ChoiceMenu id={`settings-${assistant}-default-model`} label={label} value={value} disabled={disabled}
          options={defaultModelOptions(snapshot, assistant, value, nextWord("defaultModelPlaceholder"))}
          onPick={(selected) => commit(key, selected)} />
      </div>
    )
  }
  return (
    <div className="block" id="settings-default-models" aria-busy={busy !== null}>
      <b id="settings-default-models-title">{nextWord("defaultModelsTitle")}</b>
      <p className="say">{nextWord("defaultModelsHint")}</p>
      {picker("codex", "codex_default_model", nextWord("defaultCodexModel"))}
      <div className="settings-model-field">
        <div className="settings-model-label">{nextWord("defaultCodexEffort")}</div>
        <ChoiceMenu id="settings-codex-default-effort" label={nextWord("defaultCodexEffort")}
          value={modelOf(snapshot, "codex_default_effort")} disabled={disabled}
          options={[{ value: "", label: nextWord("defaultModelPlaceholder") }, { value: "high", label: catalogWord("inline", "6ef7c9b15ecd") }, { value: "xhigh", label: catalogWord("inline", "b5255978be8e") }]}
          onPick={(selected) => commit("codex_default_effort", selected)} />
      </div>
      {picker("claude", "claude_default_model", nextWord("defaultClaudeModel"))}
      {picker("antigravity", "antigravity_default_model", catalogWord("next", "defaultAntigravityModel"))}
      <p className="say">{catalogWord("next", "defaultAntigravityCatalogUnknown")}</p>
      <div className="settings-model-field">
        <div className="settings-model-label">{catalogWord("next", "defaultAntigravityEffort")}</div>
        <ChoiceMenu id="settings-antigravity-default-effort" label={catalogWord("next", "defaultAntigravityEffort")}
          value={modelOf(snapshot, "antigravity_default_effort")} disabled={disabled}
          options={Array.from(new Set(["", "low", "medium", "high", "xhigh", "max", modelOf(snapshot, "antigravity_default_effort")])).map(value => ({ value, label: value || nextWord("defaultModelPlaceholder") }))}
          onPick={(selected) => commit("antigravity_default_effort", selected)} />
      </div>
      {snapshot ? null : (
        <button className="chip" type="button" disabled={busy === "read"} onClick={read}>
          {nextWord("defaultModelsRetry")}
        </button>
      )}
      <p className="said" role="status" aria-live="polite">{said}</p>
    </div>
  )
}
