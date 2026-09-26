import { RefusalError, TransportError, isRefusal } from "@clawdline/core"
import type {
  Verification,
  VerificationCriterionState,
  VerificationDetail,
  VerificationList,
  VerificationNoteResult,
} from "@clawdline/contract"

/**
 * The 驗收 page's routes (docs/verifications.md), and nothing else.
 *
 * Every call spells its method and its path on one line, beside each other:
 * `cloud/carry.test.ts` reads this file for them and fails on a path that
 * would not cross Clawdline Cloud, which is how a phone keeps this page.
 *
 * Each write carries an Idempotency-Key minted once per press, so a retry
 * after a dropped connection is the same note, not a second one.
 */

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), 15_000)
  const init: RequestInit = { method, credentials: "same-origin", signal: controller.signal }
  if (method !== "GET") {
    init.headers = { "Idempotency-Key": mintKey(), ...(body === undefined ? {} : { "Content-Type": "application/json" }) }
    if (body !== undefined) init.body = JSON.stringify(body)
  }
  let res: Response
  try {
    res = await fetch(path, init)
  } catch (cause) {
    throw new TransportError(`${method} ${path} did not complete`, cause)
  } finally {
    clearTimeout(timer)
  }
  const text = await res.text()
  let parsed: unknown = null
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

/** `getRandomValues` exists on plain http too, where a paired phone may be. */
function mintKey(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(16))
  return "web-" + Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("")
}

const at = (id: string) => encodeURIComponent(id)

export const readVerifications = () => call<VerificationList>("GET", "/v1/verifications")

/** One record, with its data source read now. */
export const readVerification = (id: string) => call<VerificationDetail>("GET", `/v1/verifications/${at(id)}`)

export const addNote = (id: string, text: string) =>
  call<VerificationNoteResult>("POST", `/v1/verifications/${at(id)}/notes`, { text })

export const markCriterion = (id: string, index: number, state: VerificationCriterionState) =>
  call<VerificationDetail>("POST", `/v1/verifications/${at(id)}/criteria/${index}`, { state })

export const closeVerification = (id: string, status: "accepted" | "rejected", reason: string) =>
  call<VerificationDetail>("POST", `/v1/verifications/${at(id)}/close`, { status, reason })

/** An open record is deleted only when the person said so twice: `force`. */
export const deleteVerification = (row: Pick<Verification, "id" | "status">) =>
  row.status === "open"
    ? call<unknown>("DELETE", `/v1/verifications/${at(row.id)}?force=1`)
    : call<unknown>("DELETE", `/v1/verifications/${at(row.id)}`)
