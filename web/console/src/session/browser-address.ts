/**
 * The address the person typed into the Chrome panel's open form, as the
 * daemon will accept it (browserbridge webURL): an http or https address
 * with a host. A bare host such as `github.com/settings` gains `https://`;
 * any other scheme (javascript:, file:, chrome:, data:…) is refused here so
 * the person reads why instead of a refusal code.
 */
const OTHER_SCHEME = /^(javascript|data|file|chrome|chrome-extension|about|blob|vbscript|mailto|view-source|ftp|ws|wss):/i
const WITH_SCHEME = /^[a-z][a-z0-9+.-]*:\/\//i

export function webAddress(raw: string): string | null {
  const text = raw.trim()
  if (!text || /\s/.test(text) || OTHER_SCHEME.test(text)) return null
  const candidate = WITH_SCHEME.test(text) ? text : "https://" + text
  let u: URL
  try {
    u = new URL(candidate)
  } catch {
    return null
  }
  if ((u.protocol !== "http:" && u.protocol !== "https:") || !u.hostname) return null
  return u.href
}
