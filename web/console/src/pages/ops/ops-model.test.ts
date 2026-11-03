import { test } from "node:test"
import assert from "node:assert/strict"
import {
  IntentKeys, buildTree, checkFileName, checkLinkUrl, filterCounts, includeInGitDefault, insertOpsIds, markdownName,
  needsGitChoice, opsIdsIn, opsReferences, phoneRows, resolveView, sameProjectSessions, statusView, typeWord, visibilityWord,
// @ts-expect-error -- Node strips source TypeScript in the browser-free test.
} from "./ops-model.ts"

const id = (n: number) => `ope_${n.toString(16).padStart(24, "0")}`
let serial = 0
function entry(fields: Record<string, unknown>) {
  serial++
  return {
    id: id(serial), kind: "link", title: `Entry ${serial}`, title_source: "file", collection: "", position: 0,
    type: "web", status: "ready", created_at: serial, updated_at: serial, ...fields,
  } as any
}

test("filter chips count each entry once per chip it belongs to", () => {
  const entries = [
    entry({ kind: "file", type: "markdown", path: "clawdline-ops/a.md" }),
    entry({ kind: "file", type: "markdown", path: "b.md", status: "missing" }),
    entry({ type: "notion_page", url: "https://www.notion.so/x", status: "needs_access" }),
    entry({ type: "google_doc", url: "https://docs.google.com/document/d/1" }),
    entry({ type: "google_sheet", url: "https://docs.google.com/spreadsheets/d/1", status: "resolving" }),
    entry({ type: "web", url: "https://example.com" }),
    entry({ kind: "file", type: "file", path: "x.bin", status: "unreadable" }),
  ]
  assert.deepEqual(filterCounts(entries), { all: 7, markdown: 2, notion: 1, google: 2, web: 1, attention: 3 })
  assert.deepEqual(filterCounts([]), { all: 0, markdown: 0, notion: 0, google: 0, web: 0, attention: 0 })
})

test("the phone list keeps only the chip's entries, most recently changed first", () => {
  const old = entry({ type: "web", updated_at: 10, title: "Old" })
  const fresh = entry({ type: "web", updated_at: 50, title: "Fresh" })
  const doc = entry({ type: "google_doc", updated_at: 99 })
  assert.deepEqual(phoneRows([old, doc, fresh], "web").map((row: any) => row.title), ["Fresh", "Old"])
  assert.equal(phoneRows([old, doc, fresh], "all")[0], doc)
})

test("the tree lists collections in order, then loose links, then loose Project files", () => {
  const collections = [{ id: "opc_b", name: "Billing", position: 2 }, { id: "opc_a", name: "Launch", position: 1 }, { id: "opc_e", name: "Empty", position: 3 }]
  const entries = [
    entry({ title: "Second", collection: "opc_a", position: 1 }),
    entry({ title: "First", collection: "opc_a", position: 0 }),
    entry({ title: "Invoice", collection: "opc_b" }),
    entry({ title: "Loose link" }),
    entry({ title: "Orphan", collection: "opc_gone" }),
    entry({ title: "Notes", kind: "file", type: "markdown", path: "clawdline-ops/notes.md" }),
  ]
  const tree = buildTree(collections, entries)
  assert.deepEqual(tree.map((group: any) => group.key), ["opc_a", "opc_b", "opc_e", "links", "files"])
  assert.deepEqual(tree[0].entries.map((row: any) => row.title), ["First", "Second"])
  assert.deepEqual(tree[2].entries, [])
  assert.deepEqual(tree[3].entries.map((row: any) => row.title).sort(), ["Loose link", "Orphan"])
  assert.deepEqual(tree[4].entries.map((row: any) => row.title), ["Notes"])
  assert.deepEqual(buildTree([], []).length, 0)
})

test("a resolve to-do that exists but was never delivered offers Send, not a new request", () => {
  assert.deepEqual(resolveView({ terminal: "%1", todo: "t1", todo_state: "unsent" }), { word: "resolveUnsent", action: "send" })
  assert.deepEqual(resolveView({ terminal: "%1", todo: "t1", todo_state: "sent" }), { word: "resolveSent", action: null })
  assert.deepEqual(resolveView({ terminal: "%1", todo: "t1", todo_state: "read" }), { word: "resolveReading", action: null })
  assert.deepEqual(resolveView({ terminal: "%1", todo: "t1", todo_state: "missing" }), { word: "resolveMissing", action: "retry" })
  const resolving = statusView(entry({ status: "resolving", resolve: { terminal: "%1", todo: "t1", todo_state: "unsent" } }))
  assert.equal(resolving.word, "statusResolving")
  assert.deepEqual(resolving.actions, ["send", "openOriginal"])
  const gone = statusView(entry({ status: "resolving", resolve: { terminal: "%1", todo: "t1", todo_state: "missing" } }))
  assert.deepEqual(gone.actions, ["retryResolve", "openOriginal"])
})

test("each status maps to its label and only the actions that apply to it", () => {
  assert.deepEqual(statusView(entry({ status: "needs_access", access_note: "private" })).actions, ["retryAccess", "openOriginal"])
  assert.deepEqual(statusView(entry({ kind: "file", status: "missing", path: "a.md" })).actions, ["removeFromOps"])
  assert.equal(statusView(entry({ kind: "file", status: "missing", path: "a.md" })).word, "statusMissing")
  assert.deepEqual(statusView(entry({ kind: "file", status: "unreadable", path: "a.bin" })).actions, [])
  assert.deepEqual(statusView(entry({ kind: "file", status: "ready", type: "markdown", path: "a.md" })).actions, [])
  assert.deepEqual(statusView(entry({ status: "ready" })).actions, ["openOriginal"])
  assert.equal(typeWord("google_slides"), "typeGoogleSlides")
  assert.equal(typeWord("notion_page"), "typeNotion")
})

test("Include in git is checked only when GitHub said the repository is private", () => {
  assert.equal(includeInGitDefault("private"), true)
  assert.equal(includeInGitDefault("public"), false)
  assert.equal(includeInGitDefault("unknown"), false)
  assert.equal(includeInGitDefault("unchecked"), false)
  assert.equal(visibilityWord("unknown"), "visibilityUnknown")
  assert.equal(visibilityWord("unchecked"), "visibilityUnknown")
  const setup = { folder: "clawdline-ops", folder_exists: false, git: "repo", excluded: false, visibility: "unchecked" } as const
  assert.equal(needsGitChoice(setup), true)
  assert.equal(needsGitChoice({ ...setup, folder_exists: true }), false)
  assert.equal(needsGitChoice({ ...setup, git: "none" }), false)
})

test("a Playbook body gains one ops read line, merged into an existing one without repeats", () => {
  const first = insertOpsIds("Review the launch plan.", [id(1), id(2)])
  assert.ok(!("overflow" in first))
  assert.equal((first as any).body, `Review the launch plan.\n\nclawdline ops read ${id(1)} ${id(2)}`)
  const merged = insertOpsIds(`Do it.\nclawdline ops read ${id(1)}\nThen report.`, [id(1), id(3), id(3)])
  assert.equal((merged as any).body, `Do it.\nclawdline ops read ${id(1)} ${id(3)}\nThen report.`)
  assert.deepEqual((merged as any).added, [id(3)])
  assert.equal((insertOpsIds("", [id(4)]) as any).body, `clawdline ops read ${id(4)}`)
  const full = `clawdline ops read ${Array.from({ length: 16 }, (_, n) => id(n + 1)).join(" ")}`
  assert.deepEqual(insertOpsIds(full, [id(99)]), { overflow: true })
  assert.equal((insertOpsIds(full, [id(1)]) as any).body, full)
})

test("references are read from every ops read line; a prose mention or an unknown id is handled", () => {
  const body = `clawdline ops read ${id(1)} ${id(7)}\nmention clawdline ops read in prose\nclawdline ops read ${id(1)}\n  clawdline ops read not-an-id`
  assert.deepEqual(opsIdsIn(body), [id(1), id(7)])
  const refs = opsReferences(body, [entry({ id: id(1), title: "Runbook" })])
  assert.deepEqual(refs, [{ id: id(1), title: "Runbook" }, { id: id(7), title: null }])
})

test("only Sessions in the same canonical Project are offered, and none without a key", () => {
  const sessions = [{ id: "%1", project_key: "/repo" }, { id: "%2", project_key: "/other" }, { id: "%3" }]
  assert.deepEqual(sameProjectSessions(sessions, "/repo").map((row: any) => row.id), ["%1"])
  assert.deepEqual(sameProjectSessions(sessions, ""), [])
})

test("input checks follow the contract's bounds", () => {
  assert.equal(checkLinkUrl("https://docs.google.com/document/d/1"), null)
  assert.equal(checkLinkUrl("ftp://example.com/x"), "badUrl")
  assert.equal(checkLinkUrl("javascript:alert(1)"), "badUrl")
  assert.equal(checkLinkUrl("  "), "empty")
  assert.equal(checkLinkUrl("https://example.com/" + "a".repeat(2048)), "tooLong")
  assert.equal(checkFileName("notes/today"), "slash")
  assert.equal(checkFileName("é".repeat(128)), "tooLong")
  assert.equal(checkFileName("launch plan.md"), null)
  assert.equal(markdownName(" Launch plan.md "), "Launch plan")
})

test("a retried action keeps its key until it settles", () => {
  let n = 0
  const keys = new IntentKeys(() => `k${++n}`)
  assert.equal(keys.key("create:link:https://a"), "k1")
  assert.equal(keys.key("create:link:https://a"), "k1")
  assert.equal(keys.key("create:link:https://b"), "k2")
  keys.settle("create:link:https://a")
  assert.equal(keys.key("create:link:https://a"), "k3")
})
