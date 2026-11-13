/**
 * A request the voice copilot makes for its own call, as opposed to the same
 * route asked by a Settings page.
 *
 * On a page that reads several machines the call is held by one of them — a
 * Mac when one can give the pass (`copilotMachines`, relay-writer.ts) — and
 * not necessarily by the machine the page is reading. The call's settings
 * read and its speed or answer-length change carry this header so they reach
 * the machine holding the call; the Settings page sends none and keeps
 * reading and changing the machine it shows. Without Cloud the header reaches
 * the one machine there is, which ignores it. Nothing is imported here, so
 * `node --test` loads it as it is.
 */
export const VOICE_CALL_HEADER = "X-Clawdline-Voice-Call"

export function isVoiceCall(init?: RequestInit): boolean {
  const headers = init?.headers
  if (!headers) return false
  if (headers instanceof Headers) return headers.get(VOICE_CALL_HEADER) === "1"
  const lower = VOICE_CALL_HEADER.toLowerCase()
  if (Array.isArray(headers)) return headers.some(([k, v]) => k.toLowerCase() === lower && v === "1")
  return Object.entries(headers).some(([k, v]) => k.toLowerCase() === lower && v === "1")
}
