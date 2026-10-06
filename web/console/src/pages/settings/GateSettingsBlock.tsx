import { catalogWord } from "../../catalog.js"
import { useCallback, useEffect, useState } from "react"
import type { WorkGateSettingsSnapshot } from "@clawdline/contract"
import * as L from "../../legacy/bridge.js"
import { readWorkGateSettings, writeWorkGateSettings } from "./api.js"
import "./gate-settings.css"

type GateKey = keyof WorkGateSettingsSnapshot

function mode(planning: boolean, verification: boolean): { label: string; description: string } {
  if (planning && verification) {
    return {
      label: "NEXUS",
      description: catalogWord("literal", "09fc8876cbf4"),
    }
  }
  if (planning) {
    return {
      label: catalogWord("literal", "f02322e08ccd"),
      description: catalogWord("literal", "dae4fe00826a"),
    }
  }
  if (verification) {
    return {
      label: catalogWord("literal", "c07c2d8f4a0d"),
      description: catalogWord("literal", "8a6f40cf0be9"),
    }
  }
  return {
    label: catalogWord("literal", "7c1f5f313fa9"),
    description: catalogWord("literal", "d6b66e033d46"),
  }
}

/** The two machine-wide defaults that future successful Board assignments capture. */
export function GateSettingsBlock({ shown }: { shown: boolean }) {
  const [snapshot, setSnapshot] = useState<WorkGateSettingsSnapshot | null>(null)
  const [busy, setBusy] = useState<GateKey | "read" | null>(null)
  const [said, setSaid] = useState("")

  const read = useCallback(() => {
    setBusy("read")
    setSaid("")
    void readWorkGateSettings().then(
      (answer) => {
        setSnapshot(answer)
        setBusy(null)
      },
      (error: unknown) => {
        setBusy(null)
        setSaid(L.failureSentence(error, catalogWord("literal", "b505f585baf9")))
      },
    )
  }, [])

  useEffect(() => {
    if (shown) read()
  }, [shown, read])

  const commit = (key: GateKey) => {
    if (!snapshot || busy) return
    setBusy(key)
    setSaid("")
    void writeWorkGateSettings({ [key]: !snapshot[key] }).then(
      (answer) => {
        setSnapshot(answer)
        setBusy(null)
        setSaid(catalogWord("literal", "bc1c88162612"))
      },
      (error: unknown) => {
        setBusy(null)
        setSaid(L.failureSentence(error, catalogWord("literal", "f201e051e1ac")))
      },
    )
  }

  const current = snapshot ? mode(snapshot.planning_gate, snapshot.verify_gate) : null
  const toggle = (key: GateKey, label: string, hint: string) => {
    const on = snapshot?.[key] === true
    return (
      <div className="settings-gate-row">
        <div>
          <strong>{label}</strong>
          <p className="say">{hint}</p>
        </div>
        <button
          className={on ? "chip on" : "chip"}
          type="button"
          aria-pressed={snapshot ? String(on) as "true" | "false" : "false"}
          disabled={!snapshot || busy !== null}
          onClick={() => commit(key)}
        >
          {snapshot ? (on ? catalogWord("literal", "b6bef29d23da") : catalogWord("literal", "b6c180ac93e9")) : catalogWord("literal", "af53543a73b6")}
        </button>
      </div>
    )
  }

  return (
    <div className="block settings-gates" id="settings-work-gates" aria-busy={busy !== null}>
      <b id="settings-work-gates-title">{catalogWord("literal", "6eded7a7db6a")}</b>
      <p className="say">
        {catalogWord("literal", "dff03055ebea")}
      </p>
      {toggle(
        "planning_gate",
        catalogWord("literal", "2df24b862ead"),
        catalogWord("literal", "3a75f2cb35d9"),
      )}
      {toggle(
        "verify_gate",
        catalogWord("literal", "f7b57f4896e3"),
        catalogWord("literal", "c2a3e5a9d8e2"),
      )}
      {current ? (
        <div className="settings-gate-mode" role="status" aria-live="polite">
          <strong>{catalogWord("literal", "cd9dd176be81")}：{current.label}</strong>
          <span>{current.description}</span>
        </div>
      ) : (
        <button className="chip" type="button" disabled={busy === "read"} onClick={read}>
          {catalogWord("literal", "a0747c50f20b")}
        </button>
      )}
      <p className="said" role="status" aria-live="polite">{said}</p>
    </div>
  )
}
