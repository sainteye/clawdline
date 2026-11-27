import { test } from "node:test"
import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
// @ts-expect-error Node executes source extensions.
import { ProjectAssistantSourceClient, assistantRelayRequest, type AssistantGrant } from "../cloud/project-assistants.ts"
// @ts-expect-error Node executes source extensions.
import { CharterRefusal, charterApiFor, charterProjectKey, charterSummaries, personalWorkspaceNames, playbookReaders } from "./charter-api.ts"
// @ts-expect-error Node executes source extensions.
import { createCharterFixture, samplePlaybooks } from "./charter-fixture.ts"

// The charter screens' every call, sent through a real captured source, must be
// one of the fourteen routes the daemon serves with exactly its Cloud word's local
// query or body (internal/domain/cloudvocab/vocabulary.json is the contract).

type Row = { op: string; local: { method: string }; shapes: { path: string; query?: string[]; local_body?: string[] }[] }
const vocabulary = JSON.parse(readFileSync(new URL("../../../../internal/domain/cloudvocab/vocabulary.json", import.meta.url), "utf8")) as { operations: Row[] }
const rows = vocabulary.operations.filter(row => row.op?.startsWith("project-assistants.charter"))

const signal = () => new AbortController().signal
const grant = (apiLevel = 127): AssistantGrant => ({ accountKey: "local", readerID: "person", machineID: "machine", authorizationRevision: "one", apiLevel, paired: true, online: true, writable: true })
const project = { machine_id: "machine", project_id: "project-one" }

const sourceStatus = { contract_version: "project-assistants.v1", sources: [{ machine_id: "machine", state: "online", access: "interactive", readable: true, writable: true, fresh: true, observed_at: "", capabilities: [] }] }

// A source as the Bots screen holds it: its status read, so it may write.
async function source(held: AssistantGrant, answer: (word: string) => unknown, seen: { path: string; init?: RequestInit; word: string; fields: Record<string, unknown>; kind: string; id: string }[]) {
  const client = new ProjectAssistantSourceClient(held, async (path: string, init?: RequestInit) => {
    if (path.endsWith("/status")) return Response.json(sourceStatus)
    const relay = assistantRelayRequest(path, init)
    seen.push({ path, init, ...relay })
    const reply = answer(relay.word)
    return Response.json(reply, { status: (reply as { error?: unknown }).error ? 409 : 200 })
  }, () => held)
  if ((held.apiLevel ?? 0) >= 122) await client.status(signal())
  return client
}

test("every charter call is one daemon route with exactly its Cloud word's fields", async () => {
  assert.equal(rows.length, 14)
  const seen: Parameters<typeof source>[2] = []
  const api = charterApiFor(await source(grant(), () => ({ contract_version: "project-assistants.v1" }), seen))
  assert.ok(api)
  await api.list(signal())
  await api.read(project, 0, signal())
  await api.read(project, 3, signal())
  await api.save(project, { expected_version: 2, text: "## Who\n\n- Orbit", reason: "" }, signal())
  await api.restore(project, { expected_version: 3, version: 1 }, signal())
  assert.ok(api.remove)
  await api.remove(project, { expected_version: 3 }, signal())
  await api.readers(project, signal())
  await api.proposals(project, signal())
  await api.applyProposal(project, "cp-1", { expected_version: 3 }, signal())
  await api.dismissProposal(project, "cp-1", signal())
  await api.requestChange(project, { text: "Add the review step." }, signal())
  assert.ok(api.conflicts && api.resolveConflict)
  await api.conflicts(project, signal())
  await api.resolveConflict(project, { conflict_id: "cc-1", action: "follow_charter" }, signal())
  assert.ok(api.savePlaybook && api.removePlaybook)
  await api.savePlaybook(project, { expected_version: 3, playbook: { title: "Release", text: "Tag.", bot_ids: ["bot-1"] } }, signal())
  await api.savePlaybook(project, { expected_version: 4, playbook: { id: "pb-1", title: "Release", text: "Tag, then ship.", bot_ids: [] }, reason: "edit" }, signal())
  await api.removePlaybook(project, { expected_version: 5, id: "pb-1" }, signal())
  const words = ["charters", "charter", "charter", "charter-save", "charter-restore", "charter-remove", "charter-readers", "charter-proposals", "charter-proposal-apply", "charter-proposal-dismiss", "charter-change-request", "charter-conflicts", "charter-conflict-resolve",
    "charter-playbook-save", "charter-playbook-save", "charter-playbook-remove"].map(w => "project-assistants." + w)
  assert.deepEqual(seen.map(r => r.word), words)
  for (const request of seen) {
    const row = rows.find(r => r.op === request.word)!, shape = row.shapes[0], url = new URL(request.path, "http://source.invalid")
    assert.equal(url.pathname, shape.path, request.word)
    assert.equal(request.init?.method ?? "GET", row.local.method, request.word)
    if (shape.query) assert.deepEqual([...url.searchParams.keys()].sort(), [...shape.query].sort(), request.word)
    if (shape.local_body) {
      const body = JSON.parse(String(request.init?.body))
      assert.deepEqual(Object.keys(body).sort(), [...shape.local_body].sort(), request.word)
      assert.equal(new Headers(request.init?.headers).get("Idempotency-Key"), body.idempotency_key)
      assert.equal(request.id, body.idempotency_key)
    }
  }
  assert.equal(seen[1].fields.version, 0)
  assert.equal(seen[2].fields.version, 3)
  // A save carries its reason even when it is empty, as Cloud requires.
  assert.equal(seen[3].fields.reason, "")
  // A new playbook names no id; an edit names its own. Both always carry a reason.
  assert.deepEqual(seen[13].fields.playbook, { title: "Release", text: "Tag.", bot_ids: ["bot-1"] })
  assert.equal(seen[13].fields.reason, "")
  assert.equal((seen[14].fields.playbook as { id: string }).id, "pb-1")
})

test("a machine at API 125 offers no playbook writes, and never sends one", async () => {
  const seen: Parameters<typeof source>[2] = []
  const api = charterApiFor(await source(grant(125), () => ({ contract_version: "project-assistants.v1" }), seen))
  assert.ok(api && api.conflicts)
  assert.equal(api.savePlaybook, undefined)
  assert.equal(api.removePlaybook, undefined)
  assert.equal(seen.length, 0)
})

test("the fixture's playbooks say which assigned Bots heard their latest content", async () => {
  const bots = ["bot-0", "bot-1", "bot-2", "bot-3"].map(bot_id => ({ bot_id, name: bot_id.toUpperCase() }))
  const fixture = createCharterFixture("machine", [{ project_id: "project-one", bots, versions: [{ text: "## Who\n\nLead plans.", author: "template", reason: "" }], playbooks: samplePlaybooks(bots.map(b => b.bot_id)) }])
  const api = charterApiFor({ machineID: "machine", ...fixture } as never)!
  assert.ok(api.savePlaybook && api.removePlaybook)
  const split = async () => playbookReaders(await api.read(project, 0, signal()), await api.readers(project, signal()))
    .map(row => [row.playbook.title, row.read.map(r => r.bot_id), row.unread.map(r => r.bot_id)])
  assert.equal(fixture.version("project-one"), 3)
  assert.deepEqual(await split(), [["Release steps", [], ["bot-0", "bot-1"]], ["Review checklist", [], ["bot-2"]]])
  fixture.markRead("project-one", "bot-0")
  assert.deepEqual(await split(), [["Release steps", ["bot-0"], ["bot-1"]], ["Review checklist", [], ["bot-2"]]])
  // bot-3 has no playbook, so it heard everything that is current for it at version 1.
  const summary = (await charterSummaries(api, "machine", signal()))[0]
  assert.equal(summary.readers_current, 2)
  // Editing the review checklist re-delivers only to its Bot; a text save keeps the playbooks.
  const charter = await api.read(project, 0, signal()), review = charter.playbooks![1]
  await api.savePlaybook(project, { expected_version: 3, playbook: { ...review, text: "- Read the whole diff." } }, signal())
  const saved = await api.save(project, { expected_version: 4, text: "## Who\n\nLead plans; makers build.", reason: "" }, signal())
  assert.deepEqual(saved.playbooks!.map(p => p.title), ["Release steps", "Review checklist"])
  await assert.rejects(api.savePlaybook(project, { expected_version: 5, playbook: { title: "Ghost", text: "", bot_ids: ["nobody"] } }, signal()), (e: unknown) => e instanceof CharterRefusal && e.field === "bot_ids")
  const removed = await api.removePlaybook(project, { expected_version: 5, id: review.id }, signal())
  assert.deepEqual(removed.playbooks!.map(p => p.title), ["Release steps"])
})

test("a stale write is a version conflict that names what was stale", async () => {
  const stale = (field: string) => ({ contract_version: "project-assistants.v1", error: { code: "pa_stale_version", field, retry_after_seconds: 0 } })
  const api = charterApiFor(await source(grant(), word => word.endsWith("proposal-apply") ? stale("charter_proposal_base") : word.endsWith("save") ? stale("charter_version")
    : word.endsWith("restore") ? { contract_version: "project-assistants.v1", error: { code: "pa_capacity_full", field: "charter_text", retry_after_seconds: 0 } }
      : { contract_version: "project-assistants.v1", error: { code: "pa_not_found", field: "project_id", retry_after_seconds: 0 } }, []))!
  await assert.rejects(api.save(project, { expected_version: 1, text: "x", reason: "" }, signal()), (e: unknown) => e instanceof CharterRefusal && e.code === "version_conflict" && e.field === "charter_version")
  await assert.rejects(api.applyProposal(project, "cp", { expected_version: 1 }, signal()), (e: unknown) => e instanceof CharterRefusal && e.code === "version_conflict" && e.field === "charter_proposal_base")
  await assert.rejects(api.restore(project, { expected_version: 1, version: 1 }, signal()), (e: unknown) => e instanceof CharterRefusal && e.code === "too_large")
  await assert.rejects(api.readers(project, signal()), (e: unknown) => e instanceof CharterRefusal && e.code === "not_found")
})

test("a machine older than API 122 has no charter client, and another machine's Project is refused", async () => {
  assert.equal(charterApiFor(await source(grant(121), () => ({}), [])), undefined)
  const seen: Parameters<typeof source>[2] = []
  const api = charterApiFor(await source(grant(), () => ({}), seen))!
  await assert.rejects(api.read({ machine_id: "other", project_id: "p" }, 0, signal()), (e: unknown) => e instanceof CharterRefusal && e.code === "not_found")
  assert.equal(seen.length, 0)
})

test("a machine at API 122 reads and edits charters but offers no removal", async () => {
  const seen: Parameters<typeof source>[2] = []
  const api = charterApiFor(await source(grant(122), () => ({ contract_version: "project-assistants.v1" }), seen))
  assert.ok(api)
  assert.equal(api.remove, undefined)
  await api.read(project, 0, signal())
  assert.deepEqual(seen.map(r => r.word), ["project-assistants.charter"])
})

test("a machine at API 123 offers no conflict reports, and never asks for them", async () => {
  const seen: Parameters<typeof source>[2] = []
  const api = charterApiFor(await source(grant(123), () => ({ contract_version: "project-assistants.v1" }), seen))
  assert.ok(api && api.remove)
  assert.equal(api.conflicts, undefined)
  assert.equal(api.resolveConflict, undefined)
  assert.equal(seen.length, 0)
})

test("a personal workspace is named for its Bot, keyed as its charter Project", () => {
  const bot = (id: string, kind: string, project_id: string) => ({ bot: { key: { machine_id: "machine", bot_id: id }, name: id === "atlas" ? "Atlas" : "Orbit", project: { machine_id: "machine", project_id, canonical_id: "", kind } } })
  const names = personalWorkspaceNames([bot("atlas", "personal", "bot-workspace-atlas"), bot("orbit", "place", "project-one")] as never)
  assert.deepEqual([...names], [[charterProjectKey({ machine_id: "machine", project_id: "bot-workspace-atlas" }), "Atlas"]])
})
