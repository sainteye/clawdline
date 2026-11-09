import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error Node's strip-types test runner needs the source extension.
import { playbookCardText, playbookRunOf, playbookSendText, playbookTodoText } from "./playbook-run.ts"
// @ts-expect-error Node's strip-types test runner needs the source extension.
import { localizedPlaybook } from "../playbook-words.ts"
// @ts-expect-error Node's strip-types test runner needs the source extension.
import { activateCatalog, resetCatalogForTest } from "../catalog.ts"
import english from "../../public/catalogs/en.json" with { type: "json" }
import traditionalChinese from "../../public/catalogs/zh-Hant.json" with { type: "json" }
import type { Snippet } from "../legacy/snippets-bridge.js"

const ID = "00000000-0000-4000-8000-000000000041"

test("a run keeps the launch-time title and full message after the definition changes", () => {
  const message = "Check the release.\nThen ship it."
  const sent = playbookTodoText(ID, "今天出貨", message)
  assert.deepEqual(playbookRunOf(sent), { id: ID, title: "今天出貨", message })
})

test("ordinary to-dos and malformed markers never become Playbook runs", () => {
  assert.equal(playbookRunOf("Check the release."), null)
  assert.equal(playbookRunOf("Check the release.\n\n[Clawdline-Workflow " + ID + " Title]"), null)
  assert.equal(playbookRunOf("Check the release.\n\n[Clawdline-Playbook missing Title]"), null)
  assert.equal(playbookRunOf("Check the release.\n\n[Clawdline-Playbook " + ID + " %ZZ]"), null)
})

test("the shipped global Playbook follows the UI language until edited", () => {
  const row: Snippet = { id: "00000000-0000-4000-8000-000000000101", title: "Zero-Based Review",
    body: "Run a zero-based review.", scope: "playbook", builtin: "zero_review",
    position: 100, created_at: 1, updated_at: 1 }
  const prior = globalThis.document
  try {
    globalThis.document = { documentElement: { lang: "zh-Hant-TW" } } as Document
    activateCatalog(traditionalChinese, "zh-Hant")
    const zh = localizedPlaybook(row)
    assert.equal(zh.title, "歸零審查")
    assert.match(zh.body, /每項結論都要附上證據/)
    assert.doesNotMatch(zh.body, /唯讀/)
    globalThis.document = { documentElement: { lang: "en" } } as Document
    activateCatalog(english, "en")
    const en = localizedPlaybook(row)
    assert.equal(en.title, "Zero-Based Review")
    assert.match(en.body, /Require evidence for every finding/)
    assert.deepEqual(localizedPlaybook({ ...row, builtin: undefined, title: "My review" }),
      { ...row, builtin: undefined, title: "My review" })
  } finally { resetCatalogForTest(); globalThis.document = prior }
})

test("cross-surface review localizes the full launch message and keeps authored edits", () => {
  const row: Snippet = { id: "00000000-0000-4000-8000-000000000102", title: "Cross-Surface Code Review",
    body: "Source message", scope: "playbook", builtin: "cross_surface_review", position: 200, created_at: 1, updated_at: 1 }
  const prior = globalThis.document
  try {
    for (const [lang, catalog, title] of [["en", english, "Cross-Surface Code Review"], ["zh-Hant", traditionalChinese, "跨面向 Code Review"]] as const) {
      globalThis.document = { documentElement: { lang } } as Document
      activateCatalog(catalog, lang)
      const localized = localizedPlaybook(row)
      assert.equal(localized.title, title)
      assert.match(localized.body, /INTENDED\? author must confirm:/)
      assert.ok(new TextEncoder().encode(localized.body).length <= 4000)
      const sent = playbookTodoText(row.id, localized.title, localized.body)
      assert.equal(playbookRunOf(sent)?.message, localized.body)
      const edited = { ...row, builtin: undefined, title: "My checklist", body: "Keep my words" }
      assert.equal(localizedPlaybook(edited), edited)
    }
  } finally { resetCatalogForTest(); globalThis.document = prior }
})


test("the Playbook card describes the exact native to-do message", () => {
  const text = playbookTodoText(ID, "Review", "Inspect the change.")
  assert.equal(playbookSendText("todo-fixture", text), text +
    "\n\n(Clawdline to-do todo-fixture. When it is done: clawdline todo done todo-fixture)")
})


test("the Playbook card displays the launch text without changing its native echo identity", () => {
  const message = "Read every step."
  const sent = playbookSendText("todo-fixture", playbookTodoText(ID, "Review", message))
  assert.equal(playbookCardText(sent, "todo-fixture"), message)
  assert.equal(playbookCardText(sent, "another-todo"), sent)
  assert.equal(playbookCardText("Ordinary message"), "Ordinary message")
})
