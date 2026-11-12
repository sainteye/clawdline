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

/**
 * The machine an unopened read or write is for, when the page shows every
 * machine's Sessions: the voice copilot names it beside `UNOPENED_READ_HEADER`
 * so a Session on another machine is resolved there, not on the reading one.
 */
export const UNOPENED_MACHINE_HEADER = "X-Clawdline-Unopened-Machine"

/** The machine an unopened request names, or null for the reading machine. */
export function unopenedMachine(init?: RequestInit): string | null {
  const headers = init?.headers
  if (!headers) return null
  const lower = UNOPENED_MACHINE_HEADER.toLowerCase()
  let value: string | null = null
  if (headers instanceof Headers) value = headers.get(UNOPENED_MACHINE_HEADER)
  else if (Array.isArray(headers)) value = headers.find(([k]) => k.toLowerCase() === lower)?.[1] ?? null
  else value = Object.entries(headers).find(([k]) => k.toLowerCase() === lower)?.[1] as string | undefined ?? null
  return value && /^[A-Za-z0-9._:-]{1,128}$/.test(value) ? value : null
}
