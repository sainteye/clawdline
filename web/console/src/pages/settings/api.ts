import type {
  SettingsRequest,
  SettingsSnapshot,
  WorkGateSettingsRequest,
  WorkGateSettingsSnapshot,
} from "@clawdline/contract"
import { RefusalError, TransportError, isRefusal } from "@clawdline/core"
import { client } from "../../client.js"

export type DefaultModelAssistant = "codex" | "claude"
export type DefaultModelOption = { value: string; label: string }
type DefaultModelValues = Pick<SettingsSnapshot, "codex_default_model" | "claude_default_model" | "codex_default_effort">
export type DefaultModelsSnapshot = DefaultModelValues & {
  models: Record<DefaultModelAssistant, DefaultModelOption[]>
}

/** Options for one picker, retaining a hand-edited legacy value instead of silently changing it. */
export function defaultModelOptions(
  snapshot: DefaultModelsSnapshot | null,
  assistant: DefaultModelAssistant,
  current: string,
  providerDefault: string,
): DefaultModelOption[] {
  const available = Array.isArray(snapshot?.models?.[assistant]) ? snapshot.models[assistant] : []
  const options: DefaultModelOption[] = [{ value: "", label: providerDefault }]
  if (current && !available.some((option) => option.value === current)) options.push({ value: current, label: current })
  for (const option of available) {
    if (option?.value && !options.some((row) => row.value === option.value)) {
      options.push({ value: option.value, label: option.label || option.value })
    }
  }
  return options
}

/**
 * `/v1/settings`, this app's own config.json.
 *
 * Asked here rather than through the shared client because the route is this
 * page's and nobody else's; the request and its failures are the client's own
 * shape all the same, so `failureSentence` reads them the way it reads any
 * other refusal.
 */
async function call(init: RequestInit, path = "/v1/settings"): Promise<SettingsSnapshot> {
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), 10_000)
  let res: Response
  try {
    res = await fetch(client.url(path), { ...init, signal: controller.signal })
  } catch (cause) {
    throw new TransportError(`${init.method ?? "GET"} ${path} did not complete`, cause)
  } finally {
    clearTimeout(timer)
  }
  const text = await res.text()
  let parsed: unknown
  try {
    parsed = text ? JSON.parse(text) : null
  } catch (cause) {
    throw new TransportError(`${path} answered with something that is not JSON`, cause)
  }
  if (!res.ok) {
    if (isRefusal(parsed)) throw new RefusalError(res.status, parsed, path)
    throw new TransportError(`${path} answered ${res.status} with no refusal in it`)
  }
  return parsed as SettingsSnapshot
}

export function readSettings(): Promise<SettingsSnapshot> {
  return call({ method: "GET" })
}

/** The daemon console's paired-browser surface excludes native input-bar keys. */
export function readBrowserSettings(): Promise<SettingsSnapshot> {
  return call({ method: "GET" }, "/v1/settings/browser")
}

/**
 * Change the keys given; a key left out, or sent as null, is left as the file
 * has it.
 *
 * Only what was asked for goes on the wire. The settings window has thirty-six
 * rows and writes one of them at a time, and a body carrying the other
 * thirty-five would turn every switch into a chance to overwrite a hand edit
 * made since this page last read the file.
 */
export function writeSettings(change: Partial<SettingsRequest>): Promise<SettingsSnapshot> {
  return writeSettingsTo("/v1/settings", change)
}

export function writeBrowserSettings(change: Partial<SettingsRequest>): Promise<SettingsSnapshot> {
  return writeSettingsTo("/v1/settings/browser", change)
}

function writeSettingsTo(path: string, change: Partial<SettingsRequest>): Promise<SettingsSnapshot> {
  const body: Record<string, unknown> = {}
  for (const [key, value] of Object.entries(change)) {
    if (value !== undefined) body[key] = value
  }
  return call({
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  }, path)
}

async function callDefaultModels(init: RequestInit): Promise<DefaultModelsSnapshot> {
  const path = "/v1/settings/default-models"
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), 10_000)
  let res: Response
  try {
    res = await fetch(client.url(path), { ...init, signal: controller.signal })
  } catch (cause) {
    throw new TransportError(`${init.method ?? "GET"} ${path} did not complete`, cause)
  } finally {
    clearTimeout(timer)
  }
  const text = await res.text()
  let parsed: unknown
  try {
    parsed = text ? JSON.parse(text) : null
  } catch (cause) {
    throw new TransportError(`${path} answered with something that is not JSON`, cause)
  }
  if (!res.ok) {
    if (isRefusal(parsed)) throw new RefusalError(res.status, parsed, path)
    throw new TransportError(`${path} answered ${res.status} with no refusal in it`)
  }
  return parsed as DefaultModelsSnapshot
}

export function readDefaultModels(): Promise<DefaultModelsSnapshot> {
  return callDefaultModels({ method: "GET" })
}

export function writeDefaultModels(change: Partial<DefaultModelValues>): Promise<DefaultModelsSnapshot> {
  return callDefaultModels({
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(change),
  })
}

async function callWorkGates(init: RequestInit): Promise<WorkGateSettingsSnapshot> {
  const path = "/v1/settings/work-gates"
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), 10_000)
  let res: Response
  try {
    res = await fetch(client.url(path), { ...init, signal: controller.signal })
  } catch (cause) {
    throw new TransportError(`${init.method ?? "GET"} ${path} did not complete`, cause)
  } finally {
    clearTimeout(timer)
  }
  const text = await res.text()
  let parsed: unknown
  try {
    parsed = text ? JSON.parse(text) : null
  } catch (cause) {
    throw new TransportError(`${path} answered with something that is not JSON`, cause)
  }
  if (!res.ok) {
    if (isRefusal(parsed)) throw new RefusalError(res.status, parsed, path)
    throw new TransportError(`${path} answered ${res.status} with no refusal in it`)
  }
  return parsed as WorkGateSettingsSnapshot
}

export function readWorkGateSettings(): Promise<WorkGateSettingsSnapshot> {
  return callWorkGates({ method: "GET" })
}

export function writeWorkGateSettings(change: Partial<WorkGateSettingsRequest>): Promise<WorkGateSettingsSnapshot> {
  return callWorkGates({
    method: "POST",
    headers: { "Content-Type": "application/json", "Idempotency-Key": crypto.randomUUID() },
    body: JSON.stringify(change),
  })
}
