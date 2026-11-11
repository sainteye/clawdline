import { useCallback, useEffect, useState } from "react"
import { asMachineNeedsUpdate, type MachineNeedsUpdate } from "@clawdline/core"
import { followsRelay } from "../../../client.js"
import { onMachineChanged } from "../../../cloud/machine-switch.js"
import { NeedsUpdate } from "../../../machine/NeedsUpdate.js"
import { readVoiceSettings, writeVoiceSettings, type VoiceSettingsSnapshot, type VoiceSettingsValues } from "../api.js"
import { Chip, Note, PopUp, Row, Switch } from "./controls.js"
import { W, fill } from "./copy.js"
import { settingsFailureSentence } from "./failure.js"
import { cleanVocabulary, validTerm, VOICE_VOCABULARY_RUNES, VOICE_VOCABULARY_TERMS } from "./vocabulary.js"

/**
 * The LLM cleanup and personal vocabulary cards (docs/voice.md).
 *
 * They read and write the narrow `/v1/settings/voice` route themselves, so the
 * same cards work in the machine's own console and on a phone through
 * Clawdline Cloud, where that route is carried as `voice-settings`. A machine
 * from before the route answers that it needs an update, and the cards say so
 * instead of drawing controls that cannot save.
 */
export function VoiceSettingsCards({ shown }: { shown: boolean }) {
  const [snapshot, setSnapshot] = useState<VoiceSettingsSnapshot | null>(null)
  const [failure, setFailure] = useState("")
  const [outdated, setOutdated] = useState<MachineNeedsUpdate | null>(null)
  const [term, setTerm] = useState("")
  const [termSaid, setTermSaid] = useState("")
  const [pending, setPending] = useState<VoiceSettingsValues>({})

  const read = useCallback(() => {
    void readVoiceSettings().then(
      (answer) => { setSnapshot(answer); setFailure(""); setOutdated(null) },
      (error: unknown) => {
        setSnapshot(null)
        const update = asMachineNeedsUpdate(error)
        if (update) { setOutdated(update); return }
        setFailure(settingsFailureSentence(error))
      },
    )
  }, [])

  useEffect(() => {
    if (shown) read()
  }, [shown, read])

  // A Cloud console switches machines in place; the cards then belong to the
  // other machine and are read again rather than kept from the first one.
  useEffect(() => (shown ? onMachineChanged(read) : undefined), [shown, read])

  const change = (keys: VoiceSettingsValues): Promise<void> => {
    setPending((prior) => ({ ...prior, ...keys }))
    setFailure("")
    return writeVoiceSettings(keys).then(
      (answer) => {
        setSnapshot(answer)
        setPending((prior) => {
          const next = { ...prior }
          for (const key of Object.keys(keys)) delete next[key as keyof VoiceSettingsValues]
          return next
        })
      },
      (error: unknown) => {
        setPending((prior) => {
          const next = { ...prior }
          for (const key of Object.keys(keys)) delete next[key as keyof VoiceSettingsValues]
          return next
        })
        setFailure(settingsFailureSentence(error))
        throw error
      },
    )
  }

  if (outdated) return <NeedsUpdate update={outdated} className="sw-said" />
  if (!snapshot) return failure ? <p className="sw-said" role="alert">{failure}</p> : null

  const value = <K extends keyof VoiceSettingsValues>(key: K): VoiceSettingsValues[K] =>
    pending[key] !== undefined ? pending[key] : snapshot[key]
  const polishOn = !!value("voice_polish")
  const assistant = value("voice_polish_assistant") === "codex" ? "codex" : "claude"
  const level = value("voice_polish_level") === "punctuation" ? "punctuation" : "tidy"
  const custom = cleanVocabulary(value("voice_vocabulary"))
  const projectsOn = !!value("voice_vocabulary_projects")
  const installed = Array.isArray(snapshot.polish_assistants) ? snapshot.polish_assistants : []
  const projects = Array.isArray(snapshot.vocabulary_projects) ? snapshot.vocabulary_projects : []
  const builtin = Array.isArray(snapshot.vocabulary_builtin) ? snapshot.vocabulary_builtin : []

  const addTerm = () => {
    const next = term.trim()
    if (!next) return
    if (custom.length >= VOICE_VOCABULARY_TERMS) { setTermSaid(W.voiceVocabularyFull); return }
    if (!validTerm(next) || custom.some((known) => known.toLowerCase() === next.toLowerCase())) {
      setTermSaid(W.voiceVocabularyInvalid)
      return
    }
    setTermSaid("")
    void change({ voice_vocabulary: [...custom, next] }).then(() => setTerm(""), () => undefined)
  }
  const removeTerm = (known: string) => {
    void change({ voice_vocabulary: custom.filter((entry) => entry !== known) }).catch(() => undefined)
  }

  return (
    <>
      {followsRelay() && snapshot.machine ? (
        <p className="sw-voice-machine" role="status">{fill(W.voiceSettingsMachine, { machine: snapshot.machine })}</p>
      ) : null}
      <section className="sw-voice-card" aria-labelledby="sw-voice-polish">
        <div className="sw-voice-headrow">
          <h3 className="sw-voice-head" id="sw-voice-polish">{W.voicePolishTitle}</h3>
          <Switch label={W.voicePolishSwitch} on={polishOn} onChange={(on) => { void change({ voice_polish: on }).catch(() => undefined) }} />
        </div>
        <p className="sw-hint sw-voice-lead">{W.voicePolishSwitch}</p>
        <p className="sw-hint">{W.voicePolishHint}</p>
        {!polishOn ? (
          <div className="sw-voice-pair">
            <div className="sw-voice-say">
              <span className="sw-voice-say-label">{W.voicePolishExampleRaw}</span>
              {W.voicePolishExampleRawText}
            </div>
            <div className="sw-voice-say fixed">
              <span className="sw-voice-say-label">{W.voicePolishExampleFixed}</span>
              {W.voicePolishExampleFixedText}
            </div>
          </div>
        ) : null}
        <div className={polishOn ? "" : "sw-voice-dim"}>
          <Row label={W.voicePolishModel} first>
            <PopUp
              label={W.voicePolishModel}
              value={assistant}
              disabled={!polishOn}
              options={[{ value: "claude", label: W.voicePolishClaude }, { value: "codex", label: W.voicePolishCodex }]}
              onPick={(picked) => { void change({ voice_polish_assistant: picked }).catch(() => undefined) }}
            />
          </Row>
          <Row label={W.voicePolishLevel}>
            <div className="sw-voice-seg" role="group" aria-label={W.voicePolishLevel}>
              <Chip armed={level === "punctuation"} disabled={!polishOn} onClick={() => { void change({ voice_polish_level: "punctuation" }).catch(() => undefined) }}>{W.voicePolishPunctuation}</Chip>
              <Chip armed={level === "tidy"} disabled={!polishOn} onClick={() => { void change({ voice_polish_level: "tidy" }).catch(() => undefined) }}>{W.voicePolishTidy}</Chip>
            </div>
          </Row>
        </div>
        {polishOn && !installed.includes(assistant) ? (
          <Note dot="warn">{fill(W.voicePolishMissing, { assistant: assistant === "codex" ? "Codex" : "Claude Code" })}</Note>
        ) : null}
      </section>
      <section className="sw-voice-card" aria-labelledby="sw-voice-vocabulary">
        <h3 className="sw-voice-head" id="sw-voice-vocabulary">{W.voiceVocabularyTitle}</h3>
        <p className="sw-hint">{W.voiceVocabularyHint}</p>
        <form className="sw-voice-add" onSubmit={(event) => { event.preventDefault(); addTerm() }}>
          <label className="sw-voice-hidden" htmlFor="sw-voice-term">{W.voiceVocabularyAddLabel}</label>
          <input
            id="sw-voice-term"
            value={term}
            maxLength={VOICE_VOCABULARY_RUNES * 2}
            placeholder={W.voiceVocabularyPlaceholder}
            autoComplete="off"
            spellCheck={false}
            onChange={(event) => { setTerm(event.target.value); setTermSaid("") }}
          />
          <button type="submit" className="sw-chip" disabled={!term.trim()}>{W.voiceVocabularyAdd}</button>
        </form>
        {termSaid ? <p className="sw-said" role="alert">{termSaid}</p> : null}
        <p className="sw-voice-group">{fill(W.voiceVocabularyCustom, { n: String(custom.length) })}</p>
        {custom.length ? (
          <ul className="sw-voice-chips">
            {custom.map((known) => (
              <li key={known} className="sw-voice-chip">
                <span>{known}</span>
                <button type="button" aria-label={fill(W.voiceVocabularyRemove, { term: known })} onClick={() => removeTerm(known)}>
                  <svg width="10" height="10" viewBox="0 0 10 10" aria-hidden="true"><path d="M2 2l6 6M8 2l-6 6" stroke="currentColor" strokeWidth="1.4" /></svg>
                </button>
              </li>
            ))}
          </ul>
        ) : <p className="sw-voice-empty">{W.voiceVocabularyEmpty}</p>}
        <label className="sw-voice-check">
          <input type="checkbox" checked={projectsOn} onChange={(event) => { void change({ voice_vocabulary_projects: event.target.checked }).catch(() => undefined) }} />
          <span>{W.voiceVocabularyProjects}</span>
        </label>
        {projectsOn ? (
          <>
            <p className="sw-voice-group">{fill(W.voiceVocabularyProjectsCount, { n: String(projects.length) })}</p>
            {projects.length ? (
              <ul className="sw-voice-chips">
                {projects.map((known) => <li key={known} className="sw-voice-chip auto"><span>{known}</span></li>)}
              </ul>
            ) : <p className="sw-voice-empty">{W.voiceVocabularyProjectsNone}</p>}
          </>
        ) : <p className="sw-hint">{W.voiceVocabularyProjectsOff}</p>}
        <p className="sw-voice-group">{fill(W.voiceVocabularyBuiltin, { n: String(builtin.length) })}</p>
        <ul className="sw-voice-chips">
          {builtin.map((known) => <li key={known} className="sw-voice-chip builtin"><span>{known}</span></li>)}
        </ul>
        {failure ? <p className="sw-said" role="alert">{failure}</p> : null}
      </section>
    </>
  )
}
