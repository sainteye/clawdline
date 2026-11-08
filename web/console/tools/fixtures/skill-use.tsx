import "../../src/loading-skeleton.css"
import "../../src/legacy/tokens.css"
import "../../src/legacy/base.css"
import "../../src/legacy/header.css"
import "../../src/legacy/shell.css"
import "../../src/legacy/list.css"
import "../../src/legacy/schedules.css"
import "../../src/pages/schedules/run-loading.css"
import "../../src/legacy/detail.css"
import "../../src/legacy/transcript.css"
import "../../src/legacy/composer.css"
import "../../src/legacy/live.css"
import "../../src/legacy/agents.css"
import "../../src/legacy/status-line.css"
import "../../src/legacy/sheets.css"
import "../../src/legacy/coordinator.css"
import "../../src/legacy/door.css"
import "../../src/legacy/responsive.css"
import "../../src/legacy/pages.css"
import "../../src/legacy/devices.css"
import "../../src/legacy/projects.css"
import "../../src/legacy/worktrees.css"
import "../../src/legacy/plan.css"
import "../../src/legacy/board.css"
import "../../src/legacy/session-board.css"
import "../../src/legacy/documents.css"
import "../../src/legacy/user-messages.css"
import "../../src/legacy/snippets.css"
import "../../src/side-border-overrides.css"
import "../../src/shell.css"
import "../../src/sidebar-scroll.css"
import "../../src/ui/disclosure-motion.css"
import "../../src/brand-handle.css"
import "../../src/font-scale.css"
// Synthetic data only. Every /v1/ read is answered here; nothing reaches a daemon.
// Query: lang=en|zh-Hant, view=detail|list|both, task=done, motion=off,
// events=none|read|applied|failed (receipts already there before the head).
// window.skillEmit(skillId, status) adds a live receipt after the head.
import { createRoot } from "react-dom/client"
import { Detail } from "../../src/session/Detail.js"
import { Row } from "../../src/session/List.js"
import { activateCatalog } from "../../src/catalog.js"
import { publish, loadStrings } from "../../src/legacy/bridge.js"

const q = new URLSearchParams(location.search)
const lang = q.get("lang") === "en" ? "en" : "zh-Hant"
const view = q.get("view") ?? "both"
const conversation = "00000000-0000-4000-8000-00000000a11e"
const at = 1_793_000_000

const pixels = (rows: string[], palette: Record<string, string>) => rows.map((row) => [...row].map((c) => palette[c] ?? null))
const bot = { accent: "#d97757", cells: pixels(["..oooo..", ".oooooo.", ".o.oo.o.", ".oooooo.", "..o..o..", ".oo..oo."], { o: "#d97757" }) }
const icon = (color: string, rows: string[]) => ({ accent: color, cells: pixels(rows, { x: color, w: "#f3efe8" }) })
const skills = [
  { id: "clawdline.skill.code-review", version: "1", enabled: true, name: { en: "Code review", "zh-Hant": "程式碼審查" }, purpose: { en: "Review", "zh-Hant": "審查" },
    icon: icon("#5fa8d3", ["xxxxxx", "xwwwwx", "xwxxwx", "xwwwwx", "xxxxxx"]) },
  { id: "community.skill.test-plan", version: "2", enabled: true, name: { en: "Test plan", "zh-Hant": "測試計畫" }, purpose: { en: "Plan", "zh-Hant": "計畫" },
    icon: icon("#7bc47f", ["x....x", "xx..xx", "x.xx.x", "x....x", "xxxxxx"]) },
  { id: "community.skill.release-notes", version: "1", enabled: true, name: { en: "Release notes", "zh-Hant": "版本說明" }, purpose: { en: "Notes", "zh-Hant": "說明" },
    icon: icon("#e0b45c", ["xxxxx.", "x...xx", "xxxx.x", "x....x", "xxxxxx"]) },
]
const events: { seq: number; receipt_id: string; snapshot_id: string; conversation_id: string; definition_id: string; scope_id: string; skill_id: string; skill_version: string; status: string; at: number }[] = []
const receipt = (skill: typeof skills[number], status: string, when: number) => events.push({ seq: events.length + 1, receipt_id: "fixture-" + (events.length + 1),
  snapshot_id: "snap-fixture", conversation_id: conversation, definition_id: "clawdline.persona.reviewer", scope_id: "repo-fixture",
  skill_id: skill.id, skill_version: skill.version, status, at: when * 1000 })
const before = q.get("events") ?? "none"
if (before === "read" || before === "applied") receipt(skills[0], "read", at + 30)
if (before === "applied") receipt(skills[0], "applied", at + 50)
if (before === "failed") receipt(skills[1], "failed", at + 30)
const head = events.length
;(window as unknown as { skillEmit: (id: string, status: string) => void }).skillEmit = (id, status) =>
  receipt(skills.find((skill) => skill.id === id) ?? skills[0], status, at + 70 + events.length)

const transcript = { id: conversation, evidence: {}, entries: [
  { role: "user", text: lang === "en" ? "Review the release branch and list what blocks it." : "幫我審查 release 分支，列出擋住發版的地方。", at: at + 10 },
  { role: "assistant", text: lang === "en" ? "I will read the diff with the review Skill first." : "我先用審查 Skill 讀過這次的差異。", at: at + 20 },
  { role: "assistant", text: lang === "en" ? "Two findings: a missing migration test and an unguarded retry." : "兩個發現：缺少 migration 測試，以及沒有上限的重試。", at: at + 60 },
] }
const json = (value: unknown, status = 200) => new Response(JSON.stringify(value), { status, headers: { "Content-Type": "application/json" } })
const real = window.fetch.bind(window)
window.fetch = async (input, init) => {
  const url = new URL(typeof input === "string" ? input : input instanceof URL ? input.href : input.url, location.href)
  if (!url.pathname.startsWith("/v1/")) return real(input, init)
  const path = url.pathname
  if (path === "/v1/personas") return json({ personas: [{ id: "reviewer", name: { en: "Reviewer", "zh-Hant": "審查員" }, summary: { en: "Reads changes closely", "zh-Hant": "仔細看變更" }, icon: bot, source: "builtin", suggested_kinds: [], teams: [] }] })
  if (path === "/v1/transcript") return json(transcript)
  if (path === "/v1/squad/session-snapshots/" + conversation) return json({ snapshot_id: "snap-fixture", snapshot: { definition_id: "clawdline.persona.reviewer", scope_id: "repo-fixture", scope_kind: "repo", skills } })
  if (path === "/v1/squad/events/head") return json({ seq: head })
  if (path === "/v1/squad/events") {
    const after = Number(url.searchParams.get("after") ?? 0)
    const page = events.filter((event) => event.seq > after).slice(0, 50)
    return json({ events: page, next_after: page.at(-1)?.seq ?? after, has_more: false })
  }
  if (path === "/v1/squad/settings") return json({ motion: { value: q.get("motion") !== "off" } })
  return json({ error: { code: "not_found", message: "Synthetic fixture" } }, 404)
}

const words = await real("/catalogs/" + lang + ".json").then((r) => r.json())
activateCatalog(words, lang)
await loadStrings(async () => words)
const row = { id: "%review", sessionId: conversation, persona: "reviewer", assistant: "claude", backend: "tmux", state: q.get("task") === "done" ? "idle" : "working",
  title: lang === "en" ? "Release branch review" : "Release 分支審查", cwd: "/synthetic/release", project: "Release", agents: [], shells: [], activity: { at: at + 60 } }
const tasks = q.get("task") === "done" ? [{ id: "task-fixture", state: "success", title: lang === "en" ? "Review release" : "審查 release", created: at, child: { terminalId: row.id } }] : []
publish([row] as never, row.id, "", row.id, tasks as never)
document.documentElement.classList.remove("booting")
createRoot(document.getElementById("root")!).render(<main className="app" data-view={view === "list" ? "list" : "detail"} style={{ width: "100%", height: "100dvh", display: "flex" }}>
  {view !== "detail" && <ul className="list" style={{ width: view === "list" ? "100%" : 340, margin: 0, padding: 0, listStyle: "none", borderRight: "1px solid var(--line)" }}>
    <Row row={row as never} depth={0} branchThrough={false} ancestorThrough={false} selected={true} open={true} swiped={false} onOpen={() => {}} />
  </ul>}
  {view !== "list" && <Detail row={row as never} tasks={[]} onOpenSession={() => {}} onBack={() => {}} onDid={() => {}} />}
</main>)
