import type { AgentModels } from "./reader"

// What every model picker draws from one agent's catalog answer: the options
// (the agent default first), and the one status line under them. Every picker
// that offers a model for an agent goes through this, so loading, failure,
// an older machine and an empty answer read the same on every screen.

export type PickerOption = { value: string; label: string; hint?: string }

export type PickerStatus =
  | { kind: "loading"; text: string }
  | { kind: "failed"; text: string; retryLabel: string }
  | { kind: "unsupported"; text: string }
  | { kind: "empty"; text: string }

export type PickerWords = {
  defaultNamed: string
  defaultPlain: string
  loading: string
  failed: string
  empty: string
  older: string
  missing: string
  retry: string
}

export type Picker = {
  options: PickerOption[]
  status?: PickerStatus
  /** True while the list waits inside itself: nothing to offer yet beyond the default. */
  loading: boolean
  /** The agent's efforts, from the daemon's one effort table; empty until the catalog answers. */
  efforts: string[]
  /** What an unset model runs, as the agent names it; "" when unknown. */
  defaultName: string
}

/**
 * The options and status of one agent's model picker.
 *
 * `value` is the saved model. It stays an option while the list loads, and
 * after it with a "not in the current list" hint when the agent no longer
 * lists it: a saved choice is never silently changed.
 */
export function catalogPicker(models: AgentModels, value: string | undefined, agentName: string, words: PickerWords): Picker {
  const catalog = models.status === "ready" ? models.catalog : undefined
  const listed = catalog?.models ?? []
  const loading = models.status === "loading" || (models.status === "ready" && models.waiting && !listed.length)
  const defaultName = catalog?.default_name ?? ""
  const options: PickerOption[] = [{ value: "", label: defaultName ? words.defaultNamed.replace("{name}", defaultName) : words.defaultPlain }]
  for (const m of listed) {
    if (m.value && !options.some(o => o.value === m.value)) options.push({ value: m.value, label: m.name || m.value, hint: m.description || undefined })
  }
  if (value && !options.some(o => o.value === value)) options.push({ value, label: value, hint: catalog && !loading ? words.missing : undefined })
  const fill = (text: string) => text.replace("{agent}", agentName)
  let status: PickerStatus | undefined
  if (loading) status = { kind: "loading", text: fill(words.loading) }
  else if (models.status === "failed") status = { kind: "failed", text: fill(words.failed), retryLabel: words.retry }
  else if (models.status === "unsupported") status = { kind: "unsupported", text: fill(words.older) }
  else if (catalog && !listed.length) status = { kind: "empty", text: fill(words.empty) }
  return { options, status, loading, efforts: catalog?.efforts ?? [], defaultName }
}

/** The status as the Bots drop-down (PaSelect) draws it. */
export function paSelectStatus(status: PickerStatus | undefined, retry: () => void) {
  if (!status) return undefined
  if (status.kind === "loading") return { kind: "loading" as const, text: status.text }
  if (status.kind === "failed") return { kind: "error" as const, text: status.text, retryLabel: status.retryLabel, onRetry: retry }
  return { kind: "empty" as const, text: status.text }
}
