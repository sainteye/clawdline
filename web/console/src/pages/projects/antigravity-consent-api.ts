import type { AntigravityConsentChange, AntigravityConsentStatus } from "@clawdline/contract"

export class AntigravityConsentError extends Error {
  readonly code: string
  readonly uncertain: boolean
  constructor(code: string, uncertain: boolean) { super(code); this.code = code; this.uncertain = uncertain }
}
export const ANTIGRAVITY_CONSENT_REQUEST_MS = 15_000
const scope = "project-and-linked-worktrees"
export function consentStatus(value: unknown): AntigravityConsentStatus {
  const v = value as AntigravityConsentStatus | null
  const digest = (x: unknown) => typeof x === "string" && /^[a-f0-9]{64}$/.test(x)
  if (!v || !["absent", "preparing", "active", "disabled"].includes(v.phase) ||
      !Number.isSafeInteger(v.revision) || v.revision < 0 || v.scope !== scope ||
      !["unknown", "installed", "removed"].includes(v.hooks) ||
      (v.reason !== undefined && typeof v.reason !== "string") ||
      (v.preview !== undefined && (!v.preview || (v.preview.before_digest !== "absent" && !digest(v.preview.before_digest)) || !digest(v.preview.helper_digest) || !digest(v.preview.managed_digest)))) {
    throw new AntigravityConsentError("invalid_response", false)
  }
  return v
}
export const consentRoute = (place: string) => `/v1/projects/${encodeURIComponent(place)}/antigravity-consent`
async function answer(place: string, init?: RequestInit): Promise<AntigravityConsentStatus> {
  const write = init?.method === "POST"
  const controller = new AbortController()
  let timer: ReturnType<typeof setTimeout> | undefined
  const expired = new Promise<never>((_, reject) => {
    timer = setTimeout(() => { reject(new AntigravityConsentError("timeout", write)); controller.abort() }, ANTIGRAVITY_CONSENT_REQUEST_MS)
  })
  try { return await Promise.race([expired, (async () => {
    const response = await fetch(consentRoute(place), { credentials: "same-origin", ...init, signal: controller.signal })
    const data: unknown = await response.json()
    if (!response.ok) {
      const body = data as { error?: string | { code?: string } }
      const code = typeof body?.error === "string" ? body.error : body?.error?.code
      throw new AntigravityConsentError(code || "unavailable", write)
    }
    return consentStatus(data)
  })()]) } catch (err) {
    if (err instanceof AntigravityConsentError) throw new AntigravityConsentError(err.code, write)
    throw new AntigravityConsentError("network", write)
  } finally { clearTimeout(timer) }
}
export const readAntigravityConsent = (place: string) => answer(place)
export const changeAntigravityConsent = (place: string, input: AntigravityConsentChange, key: string) => answer(place, {
  method: "POST", headers: { "Content-Type": "application/json", "Idempotency-Key": key }, body: JSON.stringify(input),
})
