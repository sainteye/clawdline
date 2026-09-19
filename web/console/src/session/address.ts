/**
 * The open session in the address: `#session=<id>`.
 *
 * The same fragment a notification carries (`SessionURL` in
 * `internal/adapters/push/message.go`) and the one `route.js` reads in the
 * original, so a link copied from the address bar, a reload and a tap on a
 * notification all arrive the same way.
 *
 * **A session id is not URL text.** A tmux pane is `%14`, and written straight
 * into a fragment it is read back by `decodeURIComponent` as U+0014: a complete
 * escape, which nothing refuses, naming a session that has never existed. So the
 * id is escaped byte by byte, keeping only RFC 3986's unreserved characters —
 * the set `SessionURL` keeps, so both spell every id identically — and `&`, `=`
 * and `#`, which a fragment may contain, are escaped too, because the reader
 * below stops at `&`.
 */

const HEX = "0123456789ABCDEF"

function unreserved(byte: number): boolean {
  return (
    (byte >= 0x30 && byte <= 0x39) || // 0-9
    (byte >= 0x41 && byte <= 0x5a) || // A-Z
    (byte >= 0x61 && byte <= 0x7a) || // a-z
    byte === 0x2d || // -
    byte === 0x2e || // .
    byte === 0x5f || // _
    byte === 0x7e // ~
  )
}

/** The fragment that names one session, `#session=` and the escaped id. */
export function sessionFragment(id: string): string {
  let out = "#session="
  for (const byte of new TextEncoder().encode(id)) {
    out += unreserved(byte) ? String.fromCharCode(byte) : "%" + HEX[byte >> 4] + HEX[byte & 0x0f]
  }
  return out
}

/**
 * The session a fragment asks for, as the ids to look for in order, or null
 * when it asks for none.
 *
 * Usually one id. Two when the address is an old one that carried a pane id
 * unescaped: decoding pane `%14` gives U+0014, and a control character is
 * the evidence that an escape was not meant — no id this daemon lists has
 * one — so the spelling as written is kept as the second guess. Anything
 * else that decodes is taken as decoded: keeping the raw spelling as well
 * would let `%2541` (pane `%41`, escaped) open pane `%2541` when `%41` has
 * gone. An escape that will not decode at all — `%80` and whatever follows,
 * a lone continuation byte — is the id as written. `sessionCandidates` in
 * the original's `input/route.js`, rule for rule.
 */
export function sessionsInFragment(hash: string): string[] | null {
  const found = /(?:^|[#&])session=([^&]*)/.exec(String(hash || ""))
  if (!found || !found[1]) return null
  const raw = found[1]
  let decoded = raw
  try {
    decoded = decodeURIComponent(raw)
  } catch {
    decoded = raw
  }
  if (decoded === raw) return [raw]
  return /[\u0000-\u001f\u007f-\u009f]/.test(decoded) ? [decoded, raw] : [decoded]
}

/** Whether the fragment names a session at all. */
export function namesSession(hash: string): boolean {
  return sessionsInFragment(hash) !== null
}
