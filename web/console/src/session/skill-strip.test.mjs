import assert from "node:assert/strict"
import { test } from "node:test"
import { readFileSync } from "node:fs"
import { createRequire } from "node:module"
import { fileURLToPath } from "node:url"
import { build } from "esbuild"
import React from "react"
import { renderToStaticMarkup } from "react-dom/server"

const here = fileURLToPath(new URL(".", import.meta.url))
const { outputFiles } = await build({ stdin: { contents: 'export {SkillStrip,SkillStatusMark} from "./SkillUse.tsx"', resolveDir: here, loader: "ts" },
  bundle: true, platform: "node", format: "cjs", packages: "external", write: false, jsx: "automatic", loader: { ".css": "empty" },
  // The legacy page, the persona registry and the daemon client start a live page when they load; markup needs none of them.
  plugins: [{ name: "page-stubs", setup(b) {
    b.onResolve({ filter: /(legacy\/bridge|\/personas|\/client)\.js$/ }, (args) => ({ path: args.path, namespace: "stub" }))
    b.onLoad({ filter: /.*/, namespace: "stub" }, () => ({ loader: "js", contents:
      "export const paintIcon = () => false; export const escapeHTML = (s) => String(s); export const strings = {};" +
      "export const personaById = () => undefined; export const personaName = () => ''; export const personaTitle = () => '';" +
      "export const personasNow = () => []; export const watchPersonas = () => () => {}; export const client = { url: (p) => p };" }))
  } }] })
const compiled = { exports: {} }
new Function("require", "module", "exports", outputFiles[0].text)(createRequire(import.meta.url), compiled, compiled.exports)
const { SkillStrip, SkillStatusMark } = compiled.exports

const snapshot = { snapshotId: "s", conversationId: "c", definitionId: "d", scopeId: "p", skills: [
  { id: "skill.a", version: "1", name: "Alpha", when: "Use this skill when alpha." },
  { id: "skill.b", version: "1", name: "Beta", when: "" },
] }
const skills = (said) => ({ snapshot, uses: new Map([["skill.a", { skillId: "skill.a", status: "applied", firstAt: 1, firstSeq: 1, at: 2, seq: 2 }]]),
  motion: true, playing: null, said, open: true, entering: false, toggle() {} })

test("every Skill chip in the strip is a button that opens the list", () => {
  const html = renderToStaticMarkup(React.createElement(SkillStrip, { skills: skills(null), role: "Designer" }))
  const buttons = html.match(/<button[^>]*class="skill-chip"[^>]*>/g) ?? []
  assert.equal(buttons.length, 2)
  for (const button of buttons) {
    assert.match(button, /type="button"/)
    assert.match(button, /aria-haspopup="dialog"/)
  }
  assert.match(buttons[0], /data-state="applied"/)
  assert.match(buttons[1], /data-state="unused"/)
})

test("nothing transient is said without a live use, and a live use's words carry its mark", () => {
  const still = renderToStaticMarkup(React.createElement(SkillStrip, { skills: skills(null), role: "Designer" }))
  assert.match(still, /<p class="skill-strip-said"[^>]*><\/p>/)
  const live = renderToStaticMarkup(React.createElement(SkillStrip, { skills: skills({ skillId: "skill.a", status: "read", seq: 3, leaving: false }), role: "Designer" }))
  assert.match(live, /skill-mark-read/)
  assert.match(live, /Alpha/)
})

test("each state's mark is hidden from a screen reader; applied is a check, unused a dash", () => {
  for (const status of ["read", "applied", "failed", undefined]) {
    const html = renderToStaticMarkup(React.createElement(SkillStatusMark, { status }))
    assert.match(html, /^<[^>]*aria-hidden="true"/)
  }
  assert.match(renderToStaticMarkup(React.createElement(SkillStatusMark, { status: "applied" })), /<svg[^>]*skill-mark-applied/)
  assert.match(renderToStaticMarkup(React.createElement(SkillStatusMark, {})), /skill-mark-unused/)
})

test("the strip's and the list's words exist in English and Taiwan Traditional Chinese", () => {
  const en = JSON.parse(readFileSync(new URL("../../public/catalogs/en.json", import.meta.url), "utf8"))
  const zh = JSON.parse(readFileSync(new URL("../../public/catalogs/zh-Hant.json", import.meta.url), "utf8"))
  const keys = ["skillUseReading", "skillUseApplied", "skillStatusRead", "skillStatusApplied", "skillStatusUnused", "skillListTitle", "skillListNote", "skillListClose"]
  for (const key of keys) {
    assert.ok(en["next." + key], "en " + key)
    assert.ok(zh["next." + key], "zh-Hant " + key)
  }
  assert.equal(en["next.skillUseReading"], "Consulting “{skill}”")
  assert.equal(zh["next.skillUseReading"], "正在參考「{skill}」")
  assert.equal(zh["next.skillUseApplied"], "已套用「{skill}」")
  assert.equal(zh["next.skillListTitle"], "{role}的 Skill")
})
