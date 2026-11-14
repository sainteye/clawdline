import type {
  IntegrationSettingsSnapshot,
  IntegrationSettingsRequest,
  WorkflowSettingsSnapshot,
  WorkflowSettingsRequest,
  SettingsRequest,
  SettingsSnapshot,
  WorkGateSettingsRequest,
  WorkGateSettingsSnapshot,
} from "@clawdline/contract"
import { RefusalError, TransportError, isRefusal } from "@clawdline/core"
import { client } from "../../client.js"

export type { IntegrationSettingsSnapshot, IntegrationSettingsRequest } from "@clawdline/contract"

export type DefaultModelAssistant = "codex" | "claude" | "antigravity"
export type DefaultModelOption = { value: string; label: string }
type DefaultModelValues = Pick<SettingsSnapshot, "codex_default_model" | "claude_default_model" | "codex_default_effort" | "antigravity_default_model" | "antigravity_default_effort">
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
 *
 * The deadline covers the whole exchange, the body included: a daemon that sends headers and then
 * stalls is a timeout, not a read that never ends. A caller's `signal` (an unmounted page, a read
 * made obsolete) aborts the same request; it never starts a second one.
 */
async function call<T = SettingsSnapshot>(init: RequestInit, path = "/v1/settings", signal?: AbortSignal): Promise<T> {
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), 10_000)
  const forward = () => controller.abort()
  if (signal?.aborted) controller.abort()
  else signal?.addEventListener("abort", forward, { once: true })
  let res: Response
  let text: string
  try {
    try {
      res = await fetch(client.url(path), { ...init, signal: controller.signal })
      text = await res.text()
    } catch (cause) {
      throw new TransportError(`${init.method ?? "GET"} ${path} did not complete`, cause)
    }
  } finally {
    clearTimeout(timer)
    signal?.removeEventListener("abort", forward)
  }
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
  return parsed as T
}

export function readSettings(options: { signal?: AbortSignal } = {}): Promise<SettingsSnapshot> {
  return call({ method: "GET" }, "/v1/settings", options.signal)
}

export function readIntegrationSettings(signal?: AbortSignal): Promise<IntegrationSettingsSnapshot> {
  return call<IntegrationSettingsSnapshot>({ method: "GET" }, "/v1/settings/integrations", signal)
}

export function writeIntegrationSettings(change: Partial<IntegrationSettingsRequest>): Promise<IntegrationSettingsSnapshot> {
  return call<IntegrationSettingsSnapshot>({ method: "POST",
    headers: { "Content-Type": "application/json", "Idempotency-Key": crypto.randomUUID() },
    body: JSON.stringify(change),
  }, "/v1/settings/integrations")
}

/** The daemon console's paired-browser surface excludes native input-bar keys. */
export function readBrowserSettings(): Promise<SettingsSnapshot> {
  return call({ method: "GET" }, "/v1/settings/browser")
}

export function readNotificationSettings(): Promise<SettingsSnapshot> {
  return call({ method: "GET" }, "/v1/settings/notifications")
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

export function writeNotificationSettings(change: Partial<SettingsRequest>): Promise<SettingsSnapshot> {
  return writeSettingsTo("/v1/settings/notifications", change)
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

/** `/v1/settings/voice`: the voice cards' keys and what the machine offers for them. */
export interface VoiceSettingsValues {
  voice_polish?: boolean | null
  voice_polish_assistant?: string | null
  voice_polish_level?: string | null
  voice_vocabulary?: string[] | null
  voice_vocabulary_projects?: boolean | null
}

export type VoiceSettingsSnapshot = VoiceSettingsValues & {
  polish_assistants: string[]
  vocabulary_builtin: string[]
  vocabulary_projects: string[]
  /** The machine's name as Cloud lists it; absent from a daemon before it was added. */
  machine?: string
}

/** The narrow voice route, carried over Cloud as `voice-settings`. */
async function callVoiceSettings(init: RequestInit): Promise<VoiceSettingsSnapshot> {
  const path = "/v1/settings/voice"
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
  return parsed as VoiceSettingsSnapshot
}

/** Settings › Voice copilot, as `/v1/settings/copilot` answers it: never the key. */
export interface CopilotSettingsSnapshot {
  work_auto_continue?:number
  max_auto_continue?:number
  work_enabled?: boolean
  work_assistant?: string
  key_set: boolean
  key_hint: string
  model: string
  eagerness: string
  voice: string
  speed: string
  extra_instructions: string
  briefing: boolean
  brief: boolean
  briefing_assistant: string
  /** When the machine writes Session recaps: off, background or on_demand (docs/session-recaps.md). */
  session_recaps?: string
  models: string[]
  eagernesses: string[]
  voices: string[]
  speeds: string[]
  briefing_assistants: string[]
  max_extra_instructions: number
}

export interface CopilotSettingsChange {
  work_auto_continue?:number
  work_enabled?: boolean
  work_assistant?: string
  openai_key?: string
  model?: string
  eagerness?: string
  voice?: string
  speed?: string
  extra_instructions?: string
  briefing_assistant?: string
  briefing?: boolean
  brief?: boolean
  session_recaps?: string
}

async function callCopilotSettings(init: RequestInit): Promise<CopilotSettingsSnapshot> {
  const path = "/v1/settings/copilot"
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
  return parsed as CopilotSettingsSnapshot
}

export function readCopilotSettings(): Promise<CopilotSettingsSnapshot> {
  return callCopilotSettings({ method: "GET" })
}

export function writeCopilotSettings(change: CopilotSettingsChange): Promise<CopilotSettingsSnapshot> {
  return callCopilotSettings({ method: "POST", headers: { "Content-Type": "application/json", "Idempotency-Key": crypto.randomUUID() }, body: JSON.stringify(change) })
}

export function readVoiceSettings(): Promise<VoiceSettingsSnapshot> {
  return callVoiceSettings({ method: "GET" })
}

export function writeVoiceSettings(change: VoiceSettingsValues): Promise<VoiceSettingsSnapshot> {
  return callVoiceSettings({
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

async function callWorkflow(init: RequestInit, request: typeof fetch = fetch): Promise<WorkflowSettingsSnapshot> {
  const path = "/v1/settings/workflow"
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), 10_000)
  let res: Response
  try {
    res = await request(client.url(path), { ...init, signal: controller.signal })
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
  return parsed as WorkflowSettingsSnapshot
}

export function readWorkflowSettings(request?: typeof fetch): Promise<WorkflowSettingsSnapshot> {
  return callWorkflow({ method: "GET" }, request)
}

export function writeWorkflowSettings(change: Partial<WorkflowSettingsRequest>, request?: typeof fetch): Promise<WorkflowSettingsSnapshot> {
  return callWorkflow({
    method: "POST",
    headers: { "Content-Type": "application/json", "Idempotency-Key": crypto.randomUUID() },
    body: JSON.stringify(change),
  }, request)
}

/** The Browser block's two machine switches (`/v1/browser/settings`, chrome-settings over Cloud). */
export type BrowserMode = "native" | "user_chrome" | "isolated"
export type BrowserPolicy = { order: BrowserMode[]; automatic: Record<BrowserMode, boolean> }
export type BrowserProfileReadiness = { ready: boolean; connected: boolean; state_error?: boolean; host?: { version?: string; browser?: string } }
export type ChromeSettings = { browser_policy?: BrowserPolicy; browser_mode?: BrowserMode; browser_enabled: boolean; browser_notify_new_site: boolean }

export function readChromeSettings(signal?: AbortSignal): Promise<ChromeSettings> {
  return call<ChromeSettings>({ method: "GET" }, "/v1/browser/settings", signal)
}

export function writeChromeSettings(change: Partial<ChromeSettings>): Promise<ChromeSettings> {
  return call<ChromeSettings>({ method: "POST",
    headers: { "Content-Type": "application/json", "Idempotency-Key": crypto.randomUUID() },
    body: JSON.stringify(change),
  }, "/v1/browser/settings")
}

/** What `/v1/browser/status` says about the extension and the stop. */
export type BrowserStatus = {
  profiles?: Partial<Record<BrowserMode, BrowserProfileReadiness>>
  status: { connected: boolean; stopped: boolean; state_error?: boolean; host?: { version?: string; browser?: string } }
}

export function readBrowserStatus(signal?: AbortSignal): Promise<BrowserStatus> {
  return call<BrowserStatus>({ method: "GET" }, "/v1/browser/status", signal)
}

/** The emergency stop for every agent's browser use; only a person resumes it. */
export function writeBrowserStop(stopped: boolean): Promise<{ stopped: boolean }> {
  return call<{ stopped: boolean }>({ method: "POST",
    headers: { "Content-Type": "application/json", "Idempotency-Key": crypto.randomUUID() },
    body: JSON.stringify({ stopped }),
  }, "/v1/browser/stop")
}

export type BrowserAuditRow = {
  at: number; conversation: string; op: string; tab?: number; url?: string; site?: string; target?: string
  text?: string; text_length?: number; text_cut?: boolean; password?: boolean; ok: boolean; error?: string
}
export type BrowserAudit = { rows: BrowserAuditRow[]; pruned: number; journal_error?: string }
export type BrowserAuditFilter = { conversation?: string; site?: string; since?: number; limit: number }

export function readBrowserAudit(filter: BrowserAuditFilter, signal?: AbortSignal): Promise<BrowserAudit> {
  const query = new URLSearchParams({ limit: String(filter.limit) })
  if (filter.conversation) query.set("conversation", filter.conversation)
  if (filter.site) query.set("site", filter.site)
  if (filter.since) query.set("since", String(filter.since))
  return call<BrowserAudit>({ method: "GET" }, "/v1/browser/audit?" + query, signal)
}
