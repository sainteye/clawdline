// @ts-expect-error -- Node source tests and Vite share this module.
import { videoLocatorFromLink } from "./video-preview-links.ts"
import { documentHash } from "../legacy/js/net/document-links.js"
import type { DocumentLocator } from "../legacy/documents-bridge.js"

/** Read only anchors produced by the escaped Markdown renderer, never code spans. */
export function videoCardsInMarkup(markup: string, base: string): { href: string; locator: DocumentLocator }[] {
  const seen = new Set<string>(), cards = []
  for (const match of markup.matchAll(/<a\s+href="([^"]+)"/g)) {
    const href = match[1].replace(/&(amp|quot|#39|lt|gt);/g, (_, entity: string) =>
      ({ amp: "&", quot: '"', "#39": "'", lt: "<", gt: ">" })[entity]!)
    const locator = videoLocatorFromLink(href, base)
    if (!locator) continue
    const canonical = new URL("/" + documentHash(locator), base).href
    if (seen.has(canonical)) continue
    seen.add(canonical); cards.push({ href: canonical, locator })
  }
  return cards
}
