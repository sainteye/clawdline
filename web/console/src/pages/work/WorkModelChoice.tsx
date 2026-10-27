import { useEffect, useRef, useState } from "react"
import type { Assistant } from "@clawdline/contract"
import { catalogWord } from "../../catalog.js"
import * as L from "../../legacy/bridge.js"
import { nextWord } from "../../next-strings.js"
import { defaultModelOptions, readDefaultModels, type DefaultModelOption, type DefaultModelsSnapshot } from "../settings/api.js"

/** A per-launch choice, read from the machine's existing model catalog. */
export function WorkModelChoice({ id, assistant, choice, disabled, onPick }: {
  id: string
  assistant: Assistant
  choice: DefaultModelOption | null
  disabled: boolean
  onPick: (choice: DefaultModelOption | null) => void
}) {
  const [open, setOpen] = useState(false)
  const [attempt, setAttempt] = useState(0)
  const [reading, setReading] = useState<{ snapshot?: DefaultModelsSnapshot; failed?: boolean }>({})
  const trigger = useRef<HTMLButtonElement>(null)
  useEffect(() => {
    if (!open) return
    let active = true
    setReading({})
    void readDefaultModels().then(
      (snapshot) => { if (active) setReading({ snapshot }) },
      () => { if (active) setReading({ failed: true }) },
    )
    return () => { active = false }
  }, [open, attempt])
  const label = catalogWord("work", "launchModelDefault")
  const options = defaultModelOptions(reading.snapshot ?? null, assistant, choice?.value ?? "", label)
  return <div className="work-launch-option">
    <button ref={trigger} type="button" className="work-launch-toggle" disabled={disabled}
      aria-expanded={open} aria-controls={id} onClick={() => setOpen(!open)}>
      <span>{L.strings.webInfoModel}</span>
      <span className={`work-launch-summary${choice ? " chosen" : ""}`} title={choice?.label}>
        <span>{choice?.label || label}</span><i aria-hidden="true" />
      </span>
    </button>
    <div id={id} className="work-launch-body" hidden={!open}>
      <div className="work-launch-models" role="radiogroup" aria-label={L.strings.webInfoModel}>
        {options.map((option) => <button key={option.value} type="button" className="m" role="radio"
          aria-checked={option.value === (choice?.value ?? "")} disabled={disabled}
          onClick={() => { onPick(option.value ? option : null); setOpen(false); trigger.current?.focus({ preventScroll: true }) }}>
          {option.label}
        </button>)}
      </div>
      {reading.failed ? <p className="work-note" role="alert">{catalogWord("work", "launchModelsFailed")} <button type="button" className="chip"
        disabled={disabled} onClick={() => setAttempt(attempt + 1)}>{nextWord("defaultModelsRetry")}</button></p>
        : !reading.snapshot ? <p className="work-note" role="status">{L.strings.webLoading}</p>
        : <p className="work-note">{catalogWord("work", "launchModelHint")}</p>}
    </div>
  </div>
}
