import { useCallback, useEffect, useState } from "react"
import { RefusalError } from "@clawdline/core"
import * as L from "../../legacy/bridge.js"
import { nextWord } from "../../next-strings.js"
import { readDefaultModels, writeDefaultModels, type DefaultModelsSnapshot } from "./api.js"
import "./default-models.css"

type ModelKey = "codex_default_model" | "claude_default_model"

function modelOf(snapshot: DefaultModelsSnapshot | null, key: ModelKey): string {
  if (!snapshot) return ""
  return String(snapshot[key] ?? "")
}

/** Machine-wide defaults for sessions Clawdline opens, on every console. */
export function DefaultModelsBlock({ shown }: { shown: boolean }) {
  const [snapshot, setSnapshot] = useState<DefaultModelsSnapshot | null>(null)
  const [codex, setCodex] = useState("")
  const [claude, setClaude] = useState("")
  const [busy, setBusy] = useState<ModelKey | "read" | null>(null)
  const [said, setSaid] = useState("")

  const accept = useCallback((answer: DefaultModelsSnapshot) => {
    setSnapshot(answer)
    setCodex(modelOf(answer, "codex_default_model"))
    setClaude(modelOf(answer, "claude_default_model"))
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
    if (key === "codex_default_model") setCodex(next)
    else setClaude(next)
    setBusy(key)
    setSaid("")
    void writeDefaultModels({ [key]: next }).then(
      (answer) => {
        accept(answer)
        setBusy(null)
        setSaid(nextWord("defaultModelsSaved"))
      },
      (error: unknown) => {
        if (key === "codex_default_model") setCodex(before)
        else setClaude(before)
        setBusy(null)
        setSaid(error instanceof RefusalError && error.code === "invalid_default_model"
          ? nextWord("defaultModelInvalid")
          : L.failureSentence(error, { fallback: nextWord("defaultModelsFailed") }))
      },
    )
  }

  const disabled = !snapshot || busy !== null
  return (
    <div className="block" id="settings-default-models" aria-busy={busy !== null}>
      <b id="settings-default-models-title">{nextWord("defaultModelsTitle")}</b>
      <p className="say">{nextWord("defaultModelsHint")}</p>
      <div className="settings-model-field">
        <label htmlFor="settings-codex-default-model">{nextWord("defaultCodexModel")}</label>
        <input
          className="find"
          id="settings-codex-default-model"
          value={codex}
          placeholder={nextWord("defaultModelPlaceholder")}
          disabled={disabled}
          spellCheck={false}
          autoComplete="off"
          onChange={(event) => setCodex(event.currentTarget.value)}
          onBlur={(event) => commit("codex_default_model", event.currentTarget.value)}
          onKeyDown={(event) => {
            if (event.key === "Enter") event.currentTarget.blur()
          }}
        />
      </div>
      <div className="settings-model-field">
        <label htmlFor="settings-claude-default-model">{nextWord("defaultClaudeModel")}</label>
        <input
          className="find"
          id="settings-claude-default-model"
          value={claude}
          placeholder={nextWord("defaultModelPlaceholder")}
          disabled={disabled}
          spellCheck={false}
          autoComplete="off"
          onChange={(event) => setClaude(event.currentTarget.value)}
          onBlur={(event) => commit("claude_default_model", event.currentTarget.value)}
          onKeyDown={(event) => {
            if (event.key === "Enter") event.currentTarget.blur()
          }}
        />
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
