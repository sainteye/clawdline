import { documentLocatorFromHash, isVideoPath } from "../legacy/js/net/document-links.js"
import type { DocumentLocator } from "../legacy/documents-bridge.js"

export function videoLocatorFromHash(hash: string): DocumentLocator | null {
  const locator = documentLocatorFromHash(hash) as DocumentLocator | null
  return locator && isVideoPath(locator.path) ? locator : null
}

/** Only a complete, same-origin publication locator can open this player. */
export function videoLocatorFromLink(href: string, base: string): DocumentLocator | null {
  try {
    const url = new URL(href, base), here = new URL(base)
    if (url.origin !== here.origin || url.pathname !== "/" || url.search || url.username || url.password) return null
    return videoLocatorFromHash(url.hash)
  } catch { return null }
}
