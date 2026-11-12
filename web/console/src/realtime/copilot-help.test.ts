import { test } from "node:test"
import assert from "node:assert/strict"
import { readFileSync, readdirSync } from "node:fs"
// @ts-expect-error -- Node's strip-types runner needs source extensions.
import { HELP_SECTION_CHARS, HELP_SECTIONS, helpSections, searchHelp } from "./copilot-help.ts"

const guide = new URL("../../../../docs/user/", import.meta.url)
const docs = readdirSync(guide).filter((f) => f.endsWith(".md")).map((f) => ({ name: f.replace(/\.md$/, ""), text: readFileSync(new URL(f, guide), "utf8") }))

test("a question about using Clawdline finds the guide's own section on it", () => {
  const sections = helpSections(docs, "zh-Hant")
  assert.ok(sections.length > 20)
  const found = searchHelp(sections, "怎麼把 Session 釘選或封存？")
  assert.ok(found.length > 0 && found.length <= HELP_SECTIONS)
  assert.ok(found.every((s: { text: string }) => s.text.length <= HELP_SECTION_CHARS + 2))
  const shortcuts = searchHelp(sections, "keyboard shortcuts")
  assert.ok(shortcuts.some((s: { doc: string }) => s.doc === "keyboard-shortcuts"))
})

test("the guide is read in the person's language where it has one, and nothing found is nothing", () => {
  const zh = helpSections([{ name: "doctor", text: "# Doctor\nEnglish" }, { name: "doctor.zh-TW", text: "# 健檢\n中文" }], "zh-Hant")
  assert.deepEqual(zh.map((s: { doc: string }) => s.doc), ["doctor.zh-TW"])
  const en = helpSections([{ name: "doctor", text: "# Doctor\nEnglish" }, { name: "doctor.zh-TW", text: "# 健檢\n中文" }], "en")
  assert.deepEqual(en.map((s: { doc: string }) => s.doc), ["doctor"])
  assert.deepEqual(searchHelp(en, "??"), [])
})
