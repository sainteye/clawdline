import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node executes source extensions.
import { markdownWithMentions } from "./mention-markdown.ts"

const oldDocument = globalThis.document
Object.defineProperty(globalThis, "document", { configurable: true, value: { getElementById() { return null } } })
const { richText } = await import("../legacy/js/view/markdown.js")
Object.defineProperty(globalThis, "document", { configurable: true, value: oldDocument })

const render = (source: string) => richText(source, { codeCopy: true })
const unescape = (s: string) => s.replace(/&#10;/g, "\n").replace(/&lt;/g, "<").replace(/&gt;/g, ">").replace(/&quot;/g, '"').replace(/&#39;/g, "'").replace(/&amp;/g, "&")
// Every @alice in the text is a mention, as the source reports them.
const everyAlice = (text: string) => [...text.matchAll(/@alice/g)].map(m => ({ start: m.index, end: m.index + 6, id: "bot-a", title: "Alice" }))
const shown = (text: string) => markdownWithMentions(text, everyAlice(text), render)
const chips = (html: string) => (html.match(/<span class="pa-mention-chip"/g) ?? []).length

test("a mention in a sentence is drawn as a chip", () => {
  const html = shown("ask @alice **now**")
  assert.equal(chips(html), 1)
  assert.match(html, /<span class="pa-mention-chip" data-mention="bot-a" title="Alice">@alice<\/span> <strong>now<\/strong>/)
})

test("a mention inside a code block is copied and shown as the code was written", () => {
  const html = shown("@alice run this:\n\n```\nssh @alice && echo \"hi\"\n```")
  const copied = /data-code-copy="([^"]*)"/.exec(html)
  assert.ok(copied, "the block keeps its copy button")
  assert.equal(unescape(copied[1]), 'ssh @alice && echo "hi"', "the clipboard receives the code, not markup")
  assert.match(html, /<pre class="code">ssh @alice &amp;&amp; echo &quot;hi&quot;<\/pre>/)
  assert.equal(chips(html), 1, "only the sentence's mention is a chip")
})

test("a mention inside inline code stays code", () => {
  const html = shown("type `@alice` to ask @alice")
  assert.match(html, /<code>@alice<\/code>/)
  assert.equal(chips(html), 1)
})

test("a mention inside a link's address leaves the link whole", () => {
  for (const text of ["see https://example.invalid/@alice/post now", "see [her page](https://example.invalid/@alice) now", "see <https://example.invalid/@alice> now"]) {
    const html = shown(text)
    const href = /<a href="([^"]*)"/.exec(html)
    assert.ok(href, text)
    assert.match(href[1], /^https:\/\/example\.invalid\/@alice(\/post)?$/, text)
    assert.equal(chips(html), 0, text)
    assert.doesNotMatch(html, /[]/, text)
  }
  assert.match(shown("see https://example.invalid/@alice/post now"), />https:\/\/example\.invalid\/@alice\/post<\/a> now/)
})

test("no attribute ever carries a chip", () => {
  const html = shown("@alice\n\n```\n@alice\n```\n\n[@alice](https://example.invalid/@alice) `@alice`")
  for (const tag of html.match(/<[^>]*>/g) ?? []) assert.doesNotMatch(tag.slice(1), /</, tag)
  assert.equal(chips(html), 1)
})
