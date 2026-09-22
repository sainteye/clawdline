import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import test from "node:test"

const source = readFileSync(new URL("./WorkV2.tsx", import.meta.url), "utf8")
const styles = readFileSync(new URL("./work.css", import.meta.url), "utf8")

test("the Project picker draws each Project mark in its trigger and menu", () => {
  assert.match(source, /function ProjectPicker/)
  assert.match(source, /places\.map[\s\S]*<Mark icon=\{place\.icon/)
  assert.doesNotMatch(source, /<option key=\{p\.id\} value=\{p\.id\}>\{p\.label\}<\/option>/)
})

test("work kinds are an explained icon list instead of a select", () => {
  for (const kind of ["Feature", "Issue", "Epic", "Refactor", "Plan"]) assert.match(source, new RegExp(`label: "${kind}"`))
  assert.match(source, /className="work-kind-list"/)
  assert.match(source, /className="work-kind-icon"/)
  assert.doesNotMatch(source, /<select[^>]*value=\{kind\}/)
})

test("the create modal closes from its close button, Escape, or the backdrop", () => {
  assert.match(source, /aria-label="關閉"/)
  assert.match(source, /event\.key === "Escape"/)
  assert.match(source, /event\.target === event\.currentTarget/)
})

test("interactive work controls announce the pointer and disabled state", () => {
  assert.match(styles, /\.work-page button[^}]*cursor: pointer/)
  assert.match(styles, /button:disabled[^}]*cursor: not-allowed/)
})

test("work cards add, show, open, and remove durable reference images", () => {
  assert.match(source, /＋ 參考圖片/)
  assert.match(source, /accept="image\/\*,\.heic,\.heif"/)
  assert.match(source, /prepareReferencePicture/)
  assert.match(source, /\/v1\/work\/v2\/images\/\$\{image\.id\}/)
  assert.match(source, /deleteWorkV2Image/)
  assert.match(styles, /\.work-reference-images/)
})

test("unassigned executable work is visible and assignable", () => {
  assert.match(source, /const unassigned = items\.filter\(\(item\) => item\.area === "unassigned"/)
  assert.match(source, /<BoardRegion title="待指派" items=\{unassigned\}/)
  assert.match(source, /const assignable = item\.area !== "planning"/)
  assert.doesNotMatch(source, /item\.area === "execution"/)
})

test("the create modal accepts reference pictures before creating the item", () => {
  assert.match(source, /＋ 加入參考圖片/)
  assert.match(source, /建立項目後上傳/)
  assert.match(source, /deployment_policy: "agent_decides" }, images\)/)
  assert.match(source, /addWorkV2Image\(answer\.item\.id/)
})

test("reference pictures use fetch-backed object URLs so Cloud can render their bytes", () => {
  assert.match(source, /function WorkReferenceImage/)
  assert.match(source, /fetch\(`\/v1\/work\/v2\/images\/\$\{image\.id\}`/)
  assert.match(source, /URL\.createObjectURL/)
  assert.match(source, /URL\.revokeObjectURL/)
})
