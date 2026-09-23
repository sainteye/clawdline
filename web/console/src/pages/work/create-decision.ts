export interface WorkV2CreateDecision {
  fingerprint: string
  key: string
}

function mintCreateKey(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(16))
  return "web-" + Array.from(bytes, (byte) => byte.toString(16).padStart(2, "0")).join("")
}

/**
 * Keep one identity for one still-unconfirmed create decision.
 *
 * Transport retries already reuse their key. This also covers a person
 * pressing Create again after the page timed out: unchanged content is the
 * same decision, while an edit is deliberately a new one.
 */
export function workV2CreateDecision(body: unknown, current?: WorkV2CreateDecision | null): WorkV2CreateDecision {
  const fingerprint = JSON.stringify(body)
  return current?.fingerprint === fingerprint ? current : { fingerprint, key: mintCreateKey() }
}
