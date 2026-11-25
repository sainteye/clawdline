import { useRef, useState } from "react"
import type { Assistant } from "@clawdline/contract"
import { catalogWord } from "../../catalog.js"
import * as L from "../../legacy/bridge.js"
import { CatalogStatus, useMachineModelCatalog, usePicker, type PickerOption } from "../../model-catalog/ui.js"

/** A per-launch choice of model; null is the agent's default. */
export type LaunchModelChoice = PickerOption

/** A per-launch choice, read from the machine's one model catalog. */
export function WorkModelChoice({ id, assistant, choice, disabled, onPick }: {
  id: string
  assistant: Assistant
  choice: LaunchModelChoice | null
  disabled: boolean
  onPick: (choice: LaunchModelChoice | null) => void
}) {
  const [open, setOpen] = useState(false)
  const catalog = useMachineModelCatalog()
  const picker = usePicker(catalog, assistant, choice?.value)
  const trigger = useRef<HTMLButtonElement>(null)
  const label = catalogWord("work", "launchModelDefault")
  // The first row is the agent default; this row's own words say so.
  const options = picker.options.map((option, index) => index === 0 && !picker.defaultName ? { ...option, label } : option)
  return <div className="work-launch-option">
    <button ref={trigger} type="button" className="work-launch-toggle" disabled={disabled}
      aria-expanded={open} aria-controls={id} onClick={() => setOpen(!open)}>
      <span>{L.strings.webInfoModel}</span>
      <span className={`work-launch-summary${choice ? " chosen" : ""}`} title={choice?.label}>
        <span>{choice?.label || label}</span><i aria-hidden="true" />
      </span>
    </button>
    <div id={id} className="work-launch-body" hidden={!open}>
      <div className="work-launch-models" role="radiogroup" aria-label={L.strings.webInfoModel} aria-busy={picker.loading}>
        {options.map((option) => <button key={option.value} type="button" className="m" role="radio"
          aria-checked={option.value === (choice?.value ?? "")} disabled={disabled} title={option.hint}
          onClick={() => { onPick(option.value ? { value: option.value, label: option.label } : null); setOpen(false); trigger.current?.focus({ preventScroll: true }) }}>
          {option.label}
        </button>)}
      </div>
      {picker.status ? <CatalogStatus status={picker.status} onRetry={catalog.retry} disabled={disabled} />
        : <p className="work-note">{catalogWord("work", "launchModelHint")}</p>}
    </div>
  </div>
}
