import type * as W from "@clawdline/contract"
import type { ModelCatalogSource } from "./reader"

// The page's own machine as a catalog source: GET /v1/model-catalog on the
// daemon that serves this console, or, in a console built for Clawdline Cloud,
// the same request answered from the relay as the word model-catalog
// (cloud/relay-writer.ts). Every picker outside Bots reads through this.

export const MODEL_CATALOG_API_LEVEL = 120

/** Refusals that mean "this machine cannot list models", not "the read failed". */
const UNSUPPORTED = new Set(["pa_unsupported", "cloud_not_carried", "cloud_machine_unsupported", "unknown_command", "not_found", "unknown_route"])

type Fetch = (input: string, init?: RequestInit) => Promise<Response>

function refusalCode(body: unknown): string {
  const b = body as { error?: unknown; code?: unknown } | null
  const e = b?.error
  if (typeof e === "string") return e
  const nested = (e as { code?: unknown } | undefined)?.code
  if (typeof nested === "string") return nested
  return typeof b?.code === "string" ? b.code : ""
}

/**
 * A source for the machine this page reads. `apiLevel` is the machine's
 * route level when known; a machine known to be older than the catalog is
 * unsupported without asking, and one not yet known is asked (an older one
 * answers 404, which is unsupported too, never a failure to retry forever).
 */
export function machineCatalogSource(apiLevel: number | undefined, fetcher: Fetch = (input, init) => globalThis.fetch(input, init), url: (path: string) => string = p => p): ModelCatalogSource {
  return {
    machineID: "",
    canModelCatalog: () => apiLevel === undefined || apiLevel >= MODEL_CATALOG_API_LEVEL,
    async modelCatalog(agent, signal): Promise<W.ModelCatalogAnswer> {
      const path = "/v1/model-catalog" + (agent ? "?agent=" + encodeURIComponent(agent) : "")
      let res: Response
      try {
        res = await fetcher(url(path), { method: "GET", signal })
      } catch (cause) {
        if (signal.aborted) throw cause
        throw { code: "unavailable" }
      }
      const text = await res.text()
      let body: unknown = null
      try { body = text ? JSON.parse(text) : null } catch { body = null }
      if (!res.ok) {
        const code = refusalCode(body)
        throw { code: res.status === 404 || UNSUPPORTED.has(code) ? "pa_unsupported" : code || "unavailable" }
      }
      return body as W.ModelCatalogAnswer
    },
  }
}
