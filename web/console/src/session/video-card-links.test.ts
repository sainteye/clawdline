import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node's source test runner needs the TypeScript extension.
import { videoCardsInMarkup } from "./video-card-links.ts"
const oldDocument = globalThis.document
// The renderer also imports the legacy DOM registry; rendering itself is pure.
Object.defineProperty(globalThis, "document", { configurable: true, value: { getElementById() { return null } } })
const { richText } = await import("../legacy/js/view/markdown.js")
Object.defineProperty(globalThis, "document", { configurable: true, value: oldDocument })
import { documentHash } from "../legacy/js/net/document-links.js"

const base = "https://app.clawdline.com/#session=session-a"
const locator = { machine: "machine-a", session: "session-a", scope: "project", path: "clip.mp4" }
const href = "https://app.clawdline.com/" + documentHash(locator)

test("rendered video links produce one complete preview identity even when repeated", () => {
  const markup = richText(`[Open video](${href}) and [again](${href})`)
  assert.deepEqual(videoCardsInMarkup(markup, base), [{ href, locator }])
})

test("code examples, external links and incomplete locators do not become background reads", () => {
  const text = `\`${href}\`\n\n\`\`\`\n[example](${href})\n\`\`\`\n\n[external](${href.replace('app.clawdline.com','other.example')})\n\n[document](${href.replace('clip.mp4','report.md')})\n\n[incomplete](https://app.clawdline.com/#document=1&path=clip.mp4)`
  assert.deepEqual(videoCardsInMarkup(richText(text), base), [])
})
