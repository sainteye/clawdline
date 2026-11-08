import "../../src/loading-skeleton.css"
import "../../src/legacy/tokens.css"
import "../../src/legacy/base.css"
import "../../src/legacy/header.css"
import "../../src/legacy/shell.css"
import "../../src/legacy/list.css"
import "../../src/legacy/detail.css"
import "../../src/legacy/transcript.css"
import "../../src/legacy/composer.css"
import "../../src/legacy/live.css"
import "../../src/legacy/agents.css"
import "../../src/legacy/status-line.css"
import "../../src/legacy/sheets.css"
import "../../src/legacy/responsive.css"
import "../../src/legacy/pages.css"
import "../../src/shell.css"
import "../../src/cloud/cloud.css"
// Synthetic data only. Every /v1/ read is answered here, per `machine=`;
// nothing reaches a daemon or a relay.
// Query: lang=en|zh-Hant, view=fleet|list|transcript, rows=<role rows per
// machine>, receipts=<receipts already in each machine's log>, motion=off,
// filter=off (a machine that ignores `conversation=`).
// window.requests lists every /v1/ path asked; window.skillEmit(machine, row,
// skillIndex, status) adds a live receipt; window.switchTo(machine) is the
// Cloud console starting to read another machine (CloudGate `forgetSkillFeeds`).
import { createRoot } from "react-dom/client"
import { useState } from "react"
import { FleetSessionList } from "../../src/cloud/FleetSessionList.js"
import { Row } from "../../src/session/List.js"
import { Detail } from "../../src/session/Detail.js"
import { forgetSkillFeeds } from "../../src/session/skill-feed.js"
import { activateCatalog } from "../../src/catalog.js"
import { publish, loadStrings } from "../../src/legacy/bridge.js"

const q = new URLSearchParams(location.search)
const lang = q.get("lang") === "en" ? "en" : "zh-Hant"
localStorage.setItem("ui_language", lang)
const view = q.get("view") ?? "fleet"
const perMachine = Number(q.get("rows") ?? 2)
const receipts = Number(q.get("receipts") ?? 3)
const filters = q.get("filter") !== "off"
const at = 1_793_000_000

const pixels = (rows: string[], palette: Record<string, string>) => rows.map((row) => [...row].map((c) => palette[c] ?? null))
const bot = { accent: "#d97757", cells: pixels(["..oooo..", ".oooooo.", ".o.oo.o.", ".oooooo.", "..o..o..", ".oo..oo."], { o: "#d97757" }) }
const icon = (color: string, rows: string[]) => ({ accent: color, cells: pixels(rows, { x: color, w: "#f3efe8" }) })
const skillsOf: Record<string, { id: string; version: string; enabled: boolean; name: Record<string, string>; purpose: Record<string, string>; content?: string; icon: unknown }[]> = {
  alpha: [
    { id: "clawdline.skill.code-review", version: "1", enabled: true, name: { en: "Code review", "zh-Hant": "程式碼審查" }, purpose: { en: "Review", "zh-Hant": "審查" },
      content: "Use this skill when a change is ready for review.", icon: icon("#5fa8d3", ["xxxxxx", "xwwwwx", "xwxxwx", "xwwwwx", "xxxxxx"]) },
    { id: "community.skill.test-plan", version: "2", enabled: true, name: { en: "Test plan", "zh-Hant": "測試計畫" }, purpose: { en: "Plan", "zh-Hant": "計畫" },
      icon: icon("#7bc47f", ["x....x", "xx..xx", "x.xx.x", "x....x", "xxxxxx"]) },
  ],
  beta: [
    { id: "community.skill.release-notes", version: "1", enabled: true, name: { en: "Release notes", "zh-Hant": "版本說明" }, purpose: { en: "Write the notes a release ships with", "zh-Hant": "寫出版本附帶的說明" },
      icon: icon("#e0b45c", ["xxxxx.", "x...xx", "xxxx.x", "x....x", "xxxxxx"]) },
  ],
}
const machineIDs = view === "list" ? ["alpha"] : ["alpha", "beta"]
const conversationOf = (machine: string, index: number) => `00000000-0000-4000-8000-${machine === "alpha" ? "a" : "b"}${String(index).padStart(11, "0")}`
type Event = { seq: number; receipt_id: string; snapshot_id: string; conversation_id: string; definition_id: string; scope_id: string; skill_id: string; skill_version: string; status: string; at: number }
const logs: Record<string, Event[]> = {}
const receipt = (machine: string, index: number, skill: number, status: string) => {
  const log = logs[machine]
  const chosen = skillsOf[machine][skill % skillsOf[machine].length]
  log.push({ seq: log.length + 1, receipt_id: machine + "-" + (log.length + 1), snapshot_id: "snap-" + machine, conversation_id: conversationOf(machine, index),
    definition_id: "clawdline.persona.reviewer", scope_id: "repo-" + machine, skill_id: chosen.id, skill_version: chosen.version, status, at: (at + log.length) * 1000 })
}
for (const machine of ["alpha", "beta"]) {
  logs[machine] = []
  // The receipts already there: the shown rows' first Skill applied, the rest other Sessions'.
  for (let n = 0; n < receipts; n++) {
    const index = n < perMachine ? n : perMachine + (n % 40)
    receipt(machine, index, 0, n < perMachine ? "applied" : "read")
  }
}
const requests: string[] = []
const win = window as unknown as Record<string, unknown>
win.requests = requests
win.skillEmit = (machine: string, index: number, skill: number, status: string) => receipt(machine, index, skill, status)
let reader = "alpha"

const json = (value: unknown, status = 200) => new Response(JSON.stringify(value), { status, headers: { "Content-Type": "application/json" } })
const snapshotOf = (machine: string) => ({ snapshot_id: "snap-" + machine, snapshot: { definition_id: "clawdline.persona.reviewer", scope_id: "repo-" + machine, scope_kind: "repo", skills: skillsOf[machine] } })
const real = window.fetch.bind(window)
window.fetch = async (input, init) => {
  const url = new URL(typeof input === "string" ? input : input instanceof URL ? input.href : input.url, location.href)
  if (!url.pathname.startsWith("/v1/")) return real(input, init)
  requests.push(url.pathname + url.search)
  const machine = url.searchParams.get("machine") || reader
  const path = url.pathname
  const log = logs[machine]
  const mine = (conversation: string) => conversation.startsWith(conversationOf(machine, 0).slice(0, 25))
  if (path === "/v1/personas") return json({ personas: [{ id: "reviewer", name: { en: "Reviewer", "zh-Hant": "審查員" }, summary: { en: "Reads changes closely", "zh-Hant": "仔細看變更" }, icon: bot, source: "builtin", suggested_kinds: [], teams: [] }] })
  if (path === "/v1/transcript") return json({ id: conversationOf(machine, 0), evidence: {}, entries: [
    { role: "user", text: lang === "en" ? "Write the notes for this release." : "幫這次發版寫說明。", at: at + 10 },
    { role: "assistant", text: lang === "en" ? "Drafting them from the merged changes." : "我從已合併的變更整理草稿。", at: at + 5000 },
  ] })
  if (path === "/v1/squad/session-snapshots") {
    const asked = url.searchParams.getAll("conversation").flatMap((value) => value.split(","))
    return json({ snapshots: asked.filter(mine).map((conversation) => ({ conversation_id: conversation, snapshot_id: "snap-" + machine, definition_id: "clawdline.persona.reviewer",
      scope_id: "repo-" + machine, skills: skillsOf[machine].map(({ id, version, name, purpose, icon }) => ({ id, version, name, purpose, icon })) })) })
  }
  if (path.startsWith("/v1/squad/session-snapshots/")) return mine(decodeURIComponent(path.slice(28))) ? json(snapshotOf(machine)) : json({ error: "snapshot_not_found" }, 404)
  if (path === "/v1/squad/events/head") return json({ seq: log.length })
  if (path === "/v1/squad/events") {
    const after = Number(url.searchParams.get("after") ?? 0), limit = Number(url.searchParams.get("limit") ?? 50)
    const named = filters ? url.searchParams.getAll("conversation").flatMap((value) => value.split(",")) : []
    const rows = log.filter((event) => event.seq > after && (!named.length || named.includes(event.conversation_id)))
    const page = rows.slice(0, limit)
    return json({ events: page, next_after: page.at(-1)?.seq ?? after, has_more: rows.length > limit })
  }
  if (path === "/v1/squad/settings") return json({ motion: { value: q.get("motion") !== "off" } })
  return json({ error: { code: "not_found", message: "Synthetic fixture" } }, 404)
}

const words = await real("/catalogs/" + lang + ".json").then((r) => r.json())
activateCatalog(words, lang)
await loadStrings(async () => words)
document.documentElement.classList.remove("booting")

const title = (machine: string, index: number) => (lang === "en" ? ["Release notes", "Branch review", "Flaky test", "Migration"] : ["發版說明", "分支審查", "不穩的測試", "資料遷移"])[index % 4] + (index >= 4 ? " " + index : "") + " · " + machine
const destination = (machine: string, index: number) => ({ machineID: machine, sessionID: "%" + machine + index, executionGeneration: "1".padStart(32, "0") })
const rowOf = (machine: string, index: number) => ({ id: "%" + machine + index, sessionId: conversationOf(machine, index), persona: "reviewer", assistant: "claude", backend: "tmux",
  state: "working", work_state: "working", title: title(machine, index), cwd: "/synthetic/" + machine, agents: [], shells: [], activity: { at: at + 60 } })
const source = {
  subscribe: () => () => {},
  readMachine: async (machine: string) => ({ kind: "ready", complete: true, observedAt: Date.now(), snapshotGeneration: "1", presentationGeneration: "1",
    rows: Array.from({ length: perMachine }, (_, index) => ({ destination: destination(machine, index), state: "working", freshness: "current", needsAttention: false, observedAt: Date.now() })) }),
  readMachinePresentations: async (machine: string) => Array.from({ length: perMachine }, (_, index) => ({ destination: destination(machine, index), title: title(machine, index), assistant: "claude",
    cwd: "/synthetic/" + machine, status: rowOf(machine, index) })),
}
const machines = machineIDs.map((id) => ({ id, name: id === "alpha" ? "studio" : "build-box", platform: "Linux", freshness: "current" }))

function Transcript() {
  const [machine, setMachine] = useState(reader)
  win.switchTo = (next: string) => { reader = next; forgetSkillFeeds(next); const row = rowOf(next, 0); publish([row] as never, row.id, "", row.id, [] as never); setMachine(next) }
  const row = rowOf(machine, 0)
  return <Detail key={machine} row={row as never} tasks={[]} onOpenSession={() => {}} onBack={() => {}} onDid={() => {}} />
}

if (view === "transcript") { const row = rowOf(reader, 0); publish([row] as never, row.id, "", row.id, [] as never) }
else publish([] as never, "", "", "", [] as never)
createRoot(document.getElementById("root")!).render(<main className="app" data-view={view === "transcript" ? "detail" : "list"} style={{ width: "100%", height: "100dvh", display: view === "transcript" ? "flex" : "block", overflow: "auto" }}>
  {view === "fleet" && <FleetSessionList {...{ machines, source, target: null, filter: "", onFilter: () => {}, onOpen: () => {}, onConnection: () => {}, onCloseRequest: () => {}, onArchiveRequest: () => {} } as never} />}
  {view === "list" && <ul className="list" style={{ width: "100%", margin: 0, padding: 0, listStyle: "none" }}>
    {Array.from({ length: perMachine }, (_, index) => <Row key={index} row={rowOf("alpha", index) as never} depth={0} branchThrough={false} ancestorThrough={false} selected={false} open={false} swiped={false} onOpen={() => {}} />)}
  </ul>}
  {view === "transcript" && <Transcript />}
</main>)
