/**
 * A read that names a Session other than the opened one, on purpose.
 *
 * A hosted page pins Session content to the Session it has open, so a panel
 * that outlives a switch cannot show the previous Session's words. The voice
 * copilot is opened from one Session's composer and asked about any other;
 * its reads carry this header, and the seam then resolves and verifies the
 * named Session's current execution the way a published link does
 * (`RelayWriter.contentTarget`) instead of refusing it as a changed
 * destination. Nothing is imported here, so `node --test` loads it as it is.
 */
export const UNOPENED_READ_HEADER = "X-Clawdline-Unopened-Read"

export function isUnopenedRead(init?: RequestInit): boolean {
  const headers = init?.headers
  if (!headers) return false
  if (headers instanceof Headers) return headers.get(UNOPENED_READ_HEADER) === "1"
  const lower = UNOPENED_READ_HEADER.toLowerCase()
  if (Array.isArray(headers)) return headers.some(([k, v]) => k.toLowerCase() === lower && v === "1")
  return Object.entries(headers).some(([k, v]) => k.toLowerCase() === lower && v === "1")
}
