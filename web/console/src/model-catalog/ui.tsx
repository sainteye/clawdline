import { useMemo } from "react"
import { catalogWord } from "../catalog.js"
import { client } from "../client.js"
import { useMachineVersion } from "../machine/NeedsUpdate.js"
import { ChoiceMenu } from "../ui-language/ChoiceMenu.js"
import { agentName } from "../project-assistants/fallback-settings"
import { machineCatalogSource } from "./machine"
import "./model-catalog.css"
import { catalogPicker, type Picker, type PickerStatus, type PickerWords } from "./picker"
import { useModelCatalog, type ModelCatalog } from "./useModelCatalog"

export type { Picker, PickerOption, PickerStatus } from "./picker"

const word = (key: string) => catalogWord("modelCatalog", key)

/** The catalog words every picker uses for its default row and status line. */
export function pickerWords(): PickerWords {
  return {
    defaultNamed: word("defaultNamed"), defaultPlain: word("defaultPlain"), loading: word("loading"),
    failed: word("failed"), empty: word("empty"), older: word("older"), missing: word("missing"), retry: word("retry"),
  }
}

/**
 * The model catalog of the machine this page reads, for a picker that has no
 * captured Bot source. One read answers every agent.
 */
export function useMachineModelCatalog(): ModelCatalog {
  const { apiLevel } = useMachineVersion()
  // Known-old and known-new are the only two levels that matter.
  const supported = apiLevel === undefined ? undefined : apiLevel >= 120
  const source = useMemo(() => machineCatalogSource(supported === undefined ? undefined : supported ? 120 : 0, undefined, path => client.url(path)), [supported])
  return useModelCatalog(source)
}

/** One agent's picker from a catalog, with the shared words. */
export function usePicker(catalog: ModelCatalog, agent: string, value: string | undefined): Picker {
  return catalogPicker(catalog.get(agent), value, agentName(agent), pickerWords())
}

/**
 * The status line under a picker that is not a PaSelect: loading with a
 * spinner, failed with Retry, an older machine or an empty answer as a note.
 */
export function CatalogStatus({ status, onRetry, className = "work-note", disabled }: { status: PickerStatus | undefined; onRetry: () => void; className?: string; disabled?: boolean }) {
  if (!status) return null
  if (status.kind === "loading") {
    return <p className={`${className} model-catalog-status`} role="status" data-model-catalog="loading">
      <span className="model-catalog-spinner" aria-hidden="true" /> {status.text}
    </p>
  }
  if (status.kind === "failed") {
    return <p className={`${className} model-catalog-status`} role="alert" data-model-catalog="failed">
      {status.text} <button type="button" className="chip" disabled={disabled} onClick={onRetry}>{status.retryLabel}</button>
    </p>
  }
  return <p className={`${className} model-catalog-status`} role="status" data-model-catalog={status.kind}>{status.text}</p>
}

/**
 * A model menu for one agent on a settings screen: the catalog's options in a
 * ChoiceMenu, and the status line under it. The menu stays usable while the
 * list loads: the agent default and a saved model are always there to pick.
 */
export function CatalogModelMenu({ id, agent, label, value, catalog, disabled, compact, onPick }: {
  id?: string; agent: string; label: string; value: string; catalog: ModelCatalog; disabled?: boolean; compact?: boolean; onPick: (value: string) => void
}) {
  const picker = usePicker(catalog, agent, value)
  return <>
    <ChoiceMenu id={id} label={label} value={value} options={picker.options} disabled={disabled} compact={compact} onPick={onPick} />
    <CatalogStatus status={picker.status} onRetry={catalog.retry} disabled={disabled} />
  </>
}

/** The agent's efforts from the catalog; a saved effort stays visible before the list answers. */
export function catalogEffortOptions(efforts: readonly string[], value: string, defaultLabel: string) {
  const options = [{ value: "", label: defaultLabel }, ...efforts.map(e => ({ value: e, label: e }))]
  if (value && !options.some(o => o.value === value)) options.push({ value, label: value })
  return options
}

export function CatalogEffortMenu({ id, agent, label, value, defaultLabel, catalog, disabled, compact, onPick }: {
  id?: string; agent: string; label: string; value: string; defaultLabel: string; catalog: ModelCatalog; disabled?: boolean; compact?: boolean; onPick: (value: string) => void
}) {
  const picker = usePicker(catalog, agent, undefined)
  return <ChoiceMenu id={id} label={label} value={value} options={catalogEffortOptions(picker.efforts, value, defaultLabel)} disabled={disabled} compact={compact} onPick={onPick} />
}

/** CatalogStatus for a screen drawn without React: the same line, appended to `row`. */
export function appendCatalogStatus(row: HTMLElement, status: PickerStatus | undefined, onRetry: () => void): void {
  if (!status) return
  const note = document.createElement("p")
  note.className = "note model-catalog-status"
  note.setAttribute("role", status.kind === "failed" ? "alert" : "status")
  note.dataset.modelCatalog = status.kind
  if (status.kind === "loading") {
    const spin = document.createElement("span")
    spin.className = "model-catalog-spinner"
    spin.setAttribute("aria-hidden", "true")
    note.appendChild(spin)
  }
  note.appendChild(document.createTextNode(status.text))
  if (status.kind === "failed") {
    const retry = document.createElement("button")
    retry.type = "button"
    retry.className = "chip"
    retry.textContent = status.retryLabel
    retry.onclick = onRetry
    note.appendChild(retry)
  }
  row.appendChild(note)
}
