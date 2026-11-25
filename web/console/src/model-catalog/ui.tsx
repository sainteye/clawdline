import { useMemo } from "react"
import { catalogWord } from "../catalog.js"
import { client } from "../client.js"
import { useMachineVersion } from "../machine/NeedsUpdate.js"
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
