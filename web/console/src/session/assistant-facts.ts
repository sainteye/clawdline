import { useEffect, useRef, useState, useSyncExternalStore } from "react"
import type { SessionRow } from "@clawdline/contract"
// @ts-expect-error -- Node's strip-types runner needs the source extension.
import { onMachineChanged } from "../cloud/machine-switch.ts"
// @ts-expect-error -- Node's strip-types runner needs the source extension.
import { STATUS_FRESH_MS } from "../cloud/status-projection.ts"
// @ts-expect-error -- Node's strip-types runner needs the source extension.
import { ANTIGRAVITY_STATE_FRESH_MS, type AssistantFacts } from "./antigravity-ui.ts"

export type AssistantFactsReading = { kind: "loading" | "error" | "stale" | "unknown" | "ready"; assistants: AssistantFacts[]; expiresAt?: number }
const empty = (kind: AssistantFactsReading["kind"]): AssistantFactsReading => ({ kind, assistants: [] })

/** Each view owns one request and no cross-machine cache. Late answers cannot change its destination. */
export class AssistantFactsRequest {
  private generation = 0
  private controller: AbortController | null = null
  cancel(): void { this.generation++; this.controller?.abort(); this.controller = null }
  async read(machine: string, request: typeof fetch, publish: (reading: AssistantFactsReading) => void,
    now = Date.now): Promise<void> {
    this.cancel()
    const generation = this.generation
    const controller = this.controller = new AbortController()
    publish(empty("loading"))
    // The native observation ceiling also bounds a readiness read.
    const timer = setTimeout(() => controller.abort(), ANTIGRAVITY_STATE_FRESH_MS)
    try {
      const aborted = new Promise<never>((_, reject) => controller.signal.addEventListener("abort", () =>
        reject(new DOMException("Assistant inventory read canceled", "AbortError")), { once: true }))
      const response = await Promise.race([request("/v1/places" + (machine ? "?machine=" + encodeURIComponent(machine) : ""),
        { signal: controller.signal, cache: "no-store", credentials: "same-origin" }), aborted])
      if (!response.ok) throw new Error("Assistant inventory unavailable")
      const body = await Promise.race([response.json(), aborted])
      if (generation !== this.generation || controller.signal.aborted) return
      if (!Number.isFinite(body?.at) || Math.abs(now() - body.at * 1000) > STATUS_FRESH_MS) publish(empty("stale"))
      else if (!Array.isArray(body.assistants)) publish(empty("unknown"))
      else publish({ kind: "ready", expiresAt: body.at * 1000 + STATUS_FRESH_MS, assistants: body.assistants.filter((row: unknown): row is AssistantFacts =>
        !!row && typeof row === "object" && typeof (row as AssistantFacts).id === "string") })
    } catch {
      if (generation === this.generation) publish(empty("error"))
    } finally { clearTimeout(timer) }
  }
}

let transportGeneration = 0
const listeners = new Set<() => void>()
onMachineChanged(() => { transportGeneration++; for (const listener of listeners) listener() })
function subscribe(listener: () => void): () => void { listeners.add(listener); return () => { listeners.delete(listener) } }

/** The wire's machine id is explicit on Cloud rows; local rows use the current origin. */
export function assistantFactsMachine(row: SessionRow | null): string {
  const machine = (row as (SessionRow & { machine?: string }) | null)?.machine
  return machine && machine !== "this-mac" ? machine : ""
}

/** Refresh on the existing inventory observation, execution, transport, or visibility event; no poll. */
export function useAssistantFacts(machine = "", execution = "", observedAt = 0, enabled = true): AssistantFactsReading {
  const transport = useSyncExternalStore(subscribe, () => transportGeneration)
  const key = JSON.stringify([machine, execution, transport, observedAt, enabled])
  const [held, setHeld] = useState<{ key: string; reading: AssistantFactsReading } | null>(null)
  const latest = useRef(key); latest.current = key
  useEffect(() => {
    if (!enabled) return
    const owner = new AssistantFactsRequest()
    let expiry: ReturnType<typeof setTimeout> | undefined
    const read = () => {
      clearTimeout(expiry)
      void owner.read(machine, globalThis.fetch.bind(globalThis), reading => {
        if (latest.current !== key) return
        setHeld({ key, reading })
        if (reading.kind === "ready") expiry = setTimeout(() => setHeld({ key, reading: empty("stale") }), Math.max(0, Math.min(reading.expiresAt ?? Date.now(), observedAt ? observedAt * 1000 + ANTIGRAVITY_STATE_FRESH_MS : Infinity) - Date.now()))
      })
    }
    const offline = () => { owner.cancel(); clearTimeout(expiry); setHeld({ key, reading: empty("error") }) }
    const visible = () => { if (document.visibilityState === "visible") read(); else offline() }
    read()
    window.addEventListener("offline", offline); window.addEventListener("online", read)
    document.addEventListener("visibilitychange", visible)
    return () => { owner.cancel(); clearTimeout(expiry); window.removeEventListener("offline", offline)
      window.removeEventListener("online", read); document.removeEventListener("visibilitychange", visible) }
  }, [key])
  return enabled && held?.key === key ? held.reading : empty("loading")
}
