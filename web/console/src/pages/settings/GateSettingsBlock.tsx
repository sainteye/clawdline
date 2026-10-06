import { useCallback, useEffect, useState } from "react"
import type { WorkGateSettingsSnapshot } from "@clawdline/contract"
import * as L from "../../legacy/bridge.js"
import { readWorkGateSettings, writeWorkGateSettings } from "./api.js"
import { Switch } from "./Switch.js"
import "./gate-settings.css"

type GateKey = keyof WorkGateSettingsSnapshot

function words(en: string, zh: string): string {
  return /^zh(?:-|$)/i.test(document.documentElement.lang || navigator.language || "") ? zh : en
}

function mode(planning: boolean, verification: boolean): { label: string; description: string } {
  if (planning && verification) {
    return {
      label: "NEXUS",
      description: words(
        "Planning review and independent verification are both required.",
        "同時要求規劃檢查與獨立驗證。",
      ),
    }
  }
  if (planning) {
    return {
      label: words("Planning", "規劃"),
      description: words(
        "Features and Epics require a reviewed plan; independent verification is not required.",
        "Feature 與 Epic 要先通過規劃檢查；不要求獨立驗證。",
      ),
    }
  }
  if (verification) {
    return {
      label: words("Independent verification", "獨立驗證"),
      description: words(
        "A maker/checker gate is required; even Epics skip the forced planning review.",
        "要求 maker／checker 獨立驗證；Epic 也略過強制規劃檢查。",
      ),
    }
  }
  return {
    label: words("Standard workflow", "一般流程"),
    description: words(
      "No planning review or independent verification is forced.",
      "不強制規劃檢查或獨立驗證。",
    ),
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
        setSaid(L.failureSentence(error, words("Gate settings unavailable", "無法讀取 gate 設定")))
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
        setSaid(words("Saved for future assignments", "已儲存，套用於之後的指派"))
      },
      (error: unknown) => {
        setBusy(null)
        setSaid(L.failureSentence(error, words("Gate settings could not be saved", "無法儲存 gate 設定")))
      },
    )
  }

  const current = snapshot ? mode(snapshot.planning_gate, snapshot.verify_gate) : null
  const toggle = (key: GateKey, label: string, hint: string) => {
    const on = snapshot?.[key] === true
    const title = "settings-gate-" + key + "-title"
    const say = "settings-gate-" + key + "-say"
    return (
      <div className="settings-gate-row">
        <div>
          <strong id={title}>{label}</strong>
          <p className="say" id={say}>{hint}</p>
        </div>
        <Switch
          labelledBy={title}
          describedBy={say}
          on={on}
          stateText={snapshot ? (on ? words("On", "開") : words("Off", "關")) : words("Loading…", "讀取中…")}
          disabled={!snapshot || busy !== null}
          onToggle={() => commit(key)}
        />
      </div>
    )
  }

  return (
    <div className="block settings-gates" id="settings-work-gates" aria-busy={busy !== null}>
      <b id="settings-work-gates-title">{words("Planning and verification gates", "規劃與驗證 gate")}</b>
      <p className="say">
        {words(
          "These machine-wide defaults are captured when a Board item is successfully assigned. Changing them does not rewrite work already in flight.",
          "這是整台機器的全域預設值，只在看板項目成功指派時擷取；之後修改不會改動進行中的工作。",
        )}
      </p>
      {toggle(
        "planning_gate",
        words("Planning gate", "規劃 gate"),
        words("Require a reviewed plan for newly assigned Features and Epics. Issues are exempt.", "新指派的 Feature 與 Epic 必須先完成獨立審查的 Plan；Issue 不受影響。"),
      )}
      {toggle(
        "verify_gate",
        words("Independent verification gate", "獨立驗證 gate"),
        words("Require an independent checker to pass the fixed Git candidate before merging.", "合併前必須由獨立 checker 對固定 Git 候選提交驗證通過。"),
      )}
      {current ? (
        <div className="settings-gate-mode" role="status" aria-live="polite">
          <strong>{words("Current mode", "目前模式")}：{current.label}</strong>
          <span>{current.description}</span>
        </div>
      ) : (
        <button className="chip" type="button" disabled={busy === "read"} onClick={read}>
          {words("Retry", "重試")}
        </button>
      )}
      <p className="said" role="status" aria-live="polite">{said}</p>
    </div>
  )
}
