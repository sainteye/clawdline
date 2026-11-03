import { test } from "node:test"
import assert from "node:assert/strict"

const oldDocument = globalThis.document
Object.defineProperty(globalThis, "document", { configurable: true, value: { getElementById() { return null } } })
const { richText } = await import("../legacy/js/view/markdown.js")
Object.defineProperty(globalThis, "document", { configurable: true, value: oldDocument })

test("support answers display emphasis and lists as readable markup", () => {
  const html = richText("Two entries:\n\n- **Home** opens the console.\n- **Settings** changes preferences.", { codeCopy: false })
  assert.match(html, /<strong>Home<\/strong>/)
  assert.match(html, /<strong>Settings<\/strong>/)
  assert.match(html, /<ul>/)
  assert.equal((html.match(/<li>/g) ?? []).length, 2)
  assert.doesNotMatch(html, /\*\*/)
})

test("read-only help code blocks preserve code without adding unwired controls", () => {
  const input = "```html\n<script>example()</script>\n```"
  const html = richText(input, { codeCopy: false })
  assert.match(html, /<pre/)
  assert.match(html, /&lt;script&gt;example\(\)&lt;\/script&gt;/)
  assert.doesNotMatch(html, /<button|<svg/)
  assert.match(richText(input), /data-code-copy=/)
})

test("Markdown cannot turn a support answer into active HTML or an unsafe link", () => {
  const html = richText('<img src=x onerror="example()">\n\n[unsafe](javascript:example())\n\n[also unsafe](data:text/html,example)\n\n[safe](https://example.invalid/help)', { codeCopy: false })
  assert.doesNotMatch(html, /<img|<script|href="(?:javascript|data):/)
  assert.match(html, /&lt;img/)
  assert.match(html, /href="https:\/\/example.invalid\/help"/)
  assert.match(html, /rel="noopener noreferrer"/)
})
