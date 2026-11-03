import { test } from "node:test"
import assert from "node:assert/strict"
const oldDocument = globalThis.document
// The renderer imports the legacy DOM registry; rendering itself is pure.
Object.defineProperty(globalThis, "document", { configurable: true, value: { getElementById() { return null } } })
const { richText } = await import("../legacy/js/view/markdown.js")
Object.defineProperty(globalThis, "document", { configurable: true, value: oldDocument })

function linkOf(markup: string): { href: string, text: string, after: string } {
  const m = /<a href="([^"]*)"[^>]*>([^<]*)<\/a>([^<]*)/.exec(markup)
  assert.ok(m, `no link in ${markup}`)
  return { href: m[1], text: m[2], after: m[3] }
}

test("a full-width parenthesis right after an ASCII address starts the sentence, not the path", () => {
  const link = linkOf(richText("https://litter.catbox.moe/f5bxa3.mp4（24 小時後過期）"))
  assert.equal(link.href, "https://litter.catbox.moe/f5bxa3.mp4")
  assert.equal(link.text, "https://litter.catbox.moe/f5bxa3.mp4")
  assert.ok(link.after.startsWith("（24"), link.after)
})

test("the same holds without a space inside the parentheses", () => {
  const link = linkOf(richText("看這裡 https://example.com/a.mp4（一天後過期）然後"))
  assert.equal(link.href, "https://example.com/a.mp4")
  assert.ok(link.after.startsWith("（一天後過期）"), link.after)
})

test("full-width brackets that belong to a non-ASCII path stay inside the link", () => {
  const link = linkOf(richText("https://zh.wikipedia.org/wiki/中文（消歧義） 是一頁"))
  assert.equal(link.href, "https://zh.wikipedia.org/wiki/中文（消歧義）")
})

test("a sentence in full-width parentheses still closes outside the link", () => {
  const link = linkOf(richText("（見 https://example.com/a）"))
  assert.equal(link.href, "https://example.com/a")
  assert.ok(link.after.startsWith("）"), link.after)
})
