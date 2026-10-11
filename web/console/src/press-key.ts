/**
 * One press, one request id.
 *
 * A write a person starts by pressing something carries an `Idempotency-Key`
 * so that the same press is carried out once: the daemon answers a retry under
 * the key with the first answer rather than typing the line again (D03, F2,
 * `sessionWrite` in internal/transport/http/actions.go), and across Clawdline
 * Cloud the relay may re-send an envelope whose answer was lost. A Session
 * write read through the relay is refused outright without one
 * (`cloud/relay-writer.ts`, `send`), which is what a model switch pressed on a
 * machine read through Cloud answered until this existed:
 * `idempotency_key_required`.
 *
 * Per press, not per module: two presses are two decisions and each may be
 * carried out. A caller that waits and asks again by itself mints again, but
 * only where the refusal it waited on proves nothing was typed — the start
 * sheet's `showing_a_menu`. A Cloud receipt keeps a refusal as well as a
 * result (`completeSessionReceipt`, internal/app/cloudops/session_receipts.go),
 * so an attempt repeated under its old key is answered with that refusal
 * again and never reaches the machine; an attempt whose answer is unknown is
 * looked at rather than minted again (`session/sender.ts`).
 *
 * `crypto.randomUUID` only exists in a secure context and this console is also
 * served over plain http on a LAN, so there is a fallback: the id has to be
 * unrepeated, not unguessable.
 */
let minted = 0

export function pressKey(what = "press"): string {
  const c = globalThis.crypto
  if (typeof c?.randomUUID === "function") {
    try {
      return c.randomUUID()
    } catch {
      /* below */
    }
  }
  minted += 1
  let tail = Date.now().toString(16)
  try {
    const bytes = new Uint8Array(8)
    c.getRandomValues(bytes)
    tail = Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("")
  } catch {
    tail += "-" + Math.random().toString(16).slice(2)
  }
  return what + "-" + minted + "-" + tail
}
