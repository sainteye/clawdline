import { useCallback, useEffect, useState } from "react"
import { asMachineNeedsUpdate, type MachineNeedsUpdate } from "@clawdline/core"
import { onMachineChanged } from "../../../cloud/machine-switch.js"
import { NeedsUpdate } from "../../../machine/NeedsUpdate.js"
import { nextWord } from "../../../next-strings.js"
import { COPILOT_TOOLS, sessionUpdateEvent } from "../../../realtime/copilot-tools.js"
import { getStoredOpenAIKey, previewVoice, setStoredOpenAIKey } from "../../../realtime/realtime-copilot.js"
import { formatUSD } from "../../../realtime/copilot-cost.js"
import { readCopilotSettings, writeCopilotSettings, type CopilotSettingsChange, type CopilotSettingsSnapshot } from "../api.js"
import { Chip, PopUp, Row, Switch } from "./controls.js"
import { settingsFailureSentence } from "./failure.js"

/**
 * Settings › Voice copilot (docs/voice.md, "Voice copilot"): everything the
 * call is given, kept on the machine. The OpenAI key is written and never
 * read back; the instructions and tool descriptions are shown as they are sent.
 */
export function CopilotSettingsCard({ shown }: { shown: boolean }) {
  const [snapshot, setSnapshot] = useState<CopilotSettingsSnapshot | null>(null)
  const [failure, setFailure] = useState("")
  const [said, setSaid] = useState("")
  const [outdated, setOutdated] = useState<MachineNeedsUpdate | null>(null)
  const [key, setKey] = useState("")
  const [extra, setExtra] = useState("")
  const [onDevice, setOnDevice] = useState(getStoredOpenAIKey)
  const [previewing, setPreviewing] = useState(false)
  const [previewSaid, setPreviewSaid] = useState("")

  const read = useCallback(() => {
    void readCopilotSettings().then(
      (answer) => { setSnapshot(answer); setExtra(answer.extra_instructions ?? ""); setFailure(""); setOutdated(null) },
      (error: unknown) => {
        setSnapshot(null)
        const update = asMachineNeedsUpdate(error)
        if (update) { setOutdated(update); return }
        setFailure(settingsFailureSentence(error))
      },
    )
  }, [])

  useEffect(() => { if (shown) { read(); setOnDevice(getStoredOpenAIKey()); setSaid("") } }, [shown, read])
  useEffect(() => (shown ? onMachineChanged(read) : undefined), [shown, read])

  const change = (c: CopilotSettingsChange, done?: string): Promise<void> => {
    setFailure("")
    setSaid("")
    return writeCopilotSettings(c).then(
      (answer) => { setSnapshot(answer); if (done) setSaid(done) },
      (error: unknown) => { setFailure(settingsFailureSentence(error)); throw error },
    )
  }

  if (outdated) return <NeedsUpdate update={outdated} className="sw-said" />
  if (!snapshot) return failure ? <p className="sw-said" role="alert">{failure}</p> : null

  const max = snapshot.max_extra_instructions || 2000
  const saveKey = (value: string) => {
    void change({ openai_key: value.trim() }, nextWord(value.trim() ? "copilotKeySaved" : "copilotKeyRemoved")).then(() => {
      setKey("")
      // A key this browser kept from before now lives on the machine only.
      if (value.trim() && value.trim() === getStoredOpenAIKey()) { setStoredOpenAIKey(""); setOnDevice("") }
    }, () => undefined)
  }

  return (
    <section className="sw-voice-card" aria-labelledby="sw-voice-copilot">
      <h3 className="sw-voice-head" id="sw-voice-copilot">{nextWord("copilotSettingsTitle")}</h3>
      <p className="sw-hint">{nextWord("copilotSettingsHint")}</p>

      <Row label={nextWord("copilotKeyLabel")} first>
        <span className="sw-copilot-key-state" role="status">
          {snapshot.key_set ? nextWord("copilotKeySet", { hint: snapshot.key_hint || "…" }) : nextWord("copilotKeyUnset")}
        </span>
      </Row>
      <div className="sw-voice-key-input-row">
        <input id="sw-copilot-key" type="password" className="sw-voice-key-input" placeholder="sk-..."
          aria-label={nextWord("copilotKeyLabel")} value={key} autoComplete="off" spellCheck={false}
          onChange={(e) => setKey(e.target.value)} />
        <Chip disabled={!key.trim()} onClick={() => saveKey(key)}>{nextWord("copilotKeySave")}</Chip>
        {snapshot.key_set ? <Chip onClick={() => saveKey("")}>{nextWord("copilotKeyRemove")}</Chip> : null}
      </div>
      {!snapshot.key_set && onDevice ? (
        <p className="sw-hint">
          {nextWord("copilotKeyOnDevice")}{" "}
          <Chip onClick={() => saveKey(onDevice)}>{nextWord("copilotKeyMove")}</Chip>
        </p>
      ) : null}

      <Row label={nextWord("copilotModelLabel")} hint={nextWord("copilotModelHint")}>
        <PopUp label={nextWord("copilotModelLabel")} value={snapshot.model}
          options={snapshot.models.map((m) => ({ value: m, label: m }))}
          onPick={(picked) => { void change({ model: picked }).catch(() => undefined) }} />
      </Row>

      <Row label={nextWord("copilotVoiceLabel")} hint={nextWord("copilotVoiceHint")}>
        <PopUp label={nextWord("copilotVoiceLabel")} value={snapshot.voice}
          options={(snapshot.voices ?? []).map((v, i) => ({ value: v, label: i < 2 ? nextWord("copilotVoiceRecommended", { voice: v }) : v }))}
          onPick={(picked) => { setPreviewSaid(""); void change({ voice: picked }).catch(() => undefined) }} />
        <Chip disabled={previewing || !snapshot.key_set} title={snapshot.key_set ? undefined : nextWord("copilotKeyUnset")}
          onClick={() => {
            setPreviewing(true)
            setPreviewSaid("")
            void previewVoice(snapshot.voice, nextWord("copilotVoicePreviewLine")).then(
              ({ usd }) => setPreviewSaid(nextWord("copilotVoicePreviewCost", { cost: formatUSD(usd) })),
              (error: unknown) => {
                const code = error instanceof Error ? error.message : String(error)
                // refusal-ok: a missing key has its own sentence; any other refusal is shown by its machine code, which the person reports as is.
                setPreviewSaid(code === "copilot_key_missing" ? nextWord("copilotKeyUnset") : nextWord("copilotVoicePreviewFailed", { reason: code }))
              },
            ).finally(() => setPreviewing(false))
          }}>
          {nextWord(previewing ? "copilotVoicePreviewing" : "copilotVoicePreview")}
        </Chip>
      </Row>
      {previewSaid ? <p className="sw-hint" role="status">{previewSaid}</p> : null}

      <Row label={nextWord("copilotSpeedLabel")} hint={nextWord("copilotSpeedHint")}>
        <PopUp label={nextWord("copilotSpeedLabel")} value={snapshot.speed}
          options={[
            { value: "0.8", label: nextWord("copilotSpeedSlow") },
            { value: "0.9", label: nextWord("copilotSpeedCalm") },
            { value: "1.0", label: nextWord("copilotSpeedNormal") },
            { value: "1.1", label: nextWord("copilotSpeedQuick") },
          ]}
          onPick={(picked) => { void change({ speed: picked }).catch(() => undefined) }} />
      </Row>

      <Row label={nextWord("copilotBriefLabel")} hint={nextWord("copilotBriefHint")}>
        <Switch label={nextWord("copilotBriefLabel")} on={snapshot.brief !== false}
          onChange={(on) => { void change({ brief: on }).catch(() => undefined) }} />
      </Row>

      <Row label={nextWord("copilotEagernessLabel")} hint={nextWord("copilotEagernessHint")}>
        <PopUp label={nextWord("copilotEagernessLabel")} value={snapshot.eagerness}
          options={[
            { value: "low", label: nextWord("copilotEagernessLow") },
            { value: "auto", label: nextWord("copilotEagernessAuto") },
            { value: "high", label: nextWord("copilotEagernessHigh") },
          ]}
          onPick={(picked) => { void change({ eagerness: picked }).catch(() => undefined) }} />
      </Row>

      <Row label={nextWord("voiceWorkEnable")} hint={nextWord("voiceWorkEnableHint")}>
        <Switch label={nextWord("voiceWorkEnable")} on={snapshot.work_enabled === true}
          onChange={(on) => { void change({ work_enabled: on }).catch(() => undefined) }} />
      </Row>
      {snapshot.max_auto_continue&&snapshot.work_auto_continue?<Row label={nextWord("voiceWorkBatchLimit")} hint={nextWord("voiceWorkBatchHint")}>
        <PopUp label={nextWord("voiceWorkBatchLimit")} value={String(snapshot.work_auto_continue)} options={Array.from({length:snapshot.max_auto_continue},(_,i)=>({value:String(i+1),label:String(i+1)}))} onPick={picked=>{void change({work_auto_continue:Number(picked)}).catch(()=>undefined)}}/>
      </Row>:null}
      <Row label={nextWord("voiceWorkPlanner")}>
        <PopUp label={nextWord("voiceWorkPlanner")} value={snapshot.work_assistant ?? snapshot.briefing_assistant}
          options={[{value:"claude",label:nextWord("voiceWorkClaude")},{value:"codex",label:nextWord("voiceWorkCodex")}]}
          onPick={(picked) => { void change({work_assistant:picked}).catch(() => undefined) }} />
      </Row>
      {snapshot.work_enabled ? <p className="sw-hint">{nextWord("voiceWorkBriefingReplaced")}</p> : null}
      <Row label={nextWord("copilotBriefingLabel")} hint={nextWord("copilotBriefingHint")}>
        <Switch label={nextWord("copilotBriefingLabel")} on={snapshot.briefing} disabled={snapshot.work_enabled}
          onChange={(on) => { void change({ briefing: on }).catch(() => undefined) }} />
      </Row>
      <Row label={nextWord("copilotBriefingAssistantLabel")}>
        <PopUp label={nextWord("copilotBriefingAssistantLabel")} value={snapshot.briefing_assistant} disabled={!snapshot.briefing || snapshot.work_enabled}
          options={[
            { value: "claude", label: nextWord("copilotBriefingClaude") },
            { value: "codex", label: nextWord("copilotBriefingCodex") },
          ]}
          onPick={(picked) => { void change({ briefing_assistant: picked }).catch(() => undefined) }} />
      </Row>

      <label className="sw-copilot-extra-label" htmlFor="sw-copilot-extra">{nextWord("copilotExtraLabel")}</label>
      <p className="sw-hint">{nextWord("copilotExtraHint")}</p>
      <textarea id="sw-copilot-extra" className="sw-copilot-extra" rows={4} maxLength={max} value={extra}
        onChange={(e) => setExtra(e.target.value)} />
      <div className="sw-voice-key-input-row">
        <span className="sw-hint">{extra.length} / {max}</span>
        <Chip disabled={extra === snapshot.extra_instructions} onClick={() => { void change({ extra_instructions: extra }, nextWord("copilotExtraSaved")).catch(() => undefined) }}>
          {nextWord("copilotExtraSave")}
        </Chip>
      </div>

      <details className="sw-copilot-source">
        <summary>{snapshot.work_enabled ? nextWord("voiceWorkManagedSource") : nextWord("copilotSourceSummary", { tools: COPILOT_TOOLS.length })}</summary>
        <p className="sw-hint">{nextWord(snapshot.work_enabled ? "voiceWorkManagedSourceHint" : "copilotSourceHint")}</p>
        <h4>{nextWord("copilotSourceInstructions")}</h4>
        <pre className="sw-copilot-pre">{String((sessionUpdateEvent("","",{workEnabled:snapshot.work_enabled,brief:snapshot.brief,language:document.documentElement.lang}) as {session:{instructions:string}}).session.instructions)}</pre>
        {!snapshot.work_enabled ? <><h4>{nextWord("copilotSourceTools")}</h4>
        <ul className="sw-copilot-tools">
          {COPILOT_TOOLS.map((tool) => (
            <li key={tool.name}><code>{tool.name}</code><span>{tool.description}</span></li>
          ))}
        </ul></> : null}
      </details>

      {said ? <p className="sw-said sw-voice-saved" role="status">{said}</p> : null}
      {failure ? <p className="sw-said" role="alert">{failure}</p> : null}
    </section>
  )
}
