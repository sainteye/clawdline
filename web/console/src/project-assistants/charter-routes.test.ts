import { test } from "node:test"
import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
// @ts-expect-error Node executes source extensions.
import { ProjectAssistantSourceClient, assistantRelayRequest, type AssistantGrant } from "../cloud/project-assistants.ts"
// @ts-expect-error Node executes source extensions.
import { CharterRefusal, charterApiFor, charterProjectKey, charterSummaries, charterWorkflow, personalWorkspaceNames, playbookReaders, proposalKind, workflowProposals, workflowRefusalSubject } from "./charter-api.ts"
// @ts-expect-error Node executes source extensions.
import { createCharterFixture, samplePlaybooks, sampleWorkflow, sampleWorkflowProposal } from "./charter-fixture.ts"

// The charter screens' every call, sent through a real captured source, must be
// one of the fifteen routes the daemon serves with exactly its Cloud word's local
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
  assert.equal(rows.length, 15)
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

test("a Playbook approval carries the reviewed Playbook on API 131", async () => {
  const seen: Parameters<typeof source>[2] = []
  const api = charterApiFor(await source(grant(131), () => ({ contract_version: "project-assistants.v1" }), seen))!
  const review = { expected_version: 7, playbook_id: "pb-one" }
  await api.applyProposal(project, "cp-one", review, signal())
  const row = rows.find(r => r.op === "project-assistants.charter-proposal-apply")!
  assert.deepEqual(Object.keys(seen[0].fields).sort(), [...row.shapes[1].local_body!].sort())
  assert.equal(seen[0].fields.playbook_id, review.playbook_id)

  const older: Parameters<typeof source>[2] = []
  const old = charterApiFor(await source(grant(130), () => ({ contract_version: "project-assistants.v1" }), older))!
  await assert.rejects(old.applyProposal(project, "cp-one", review, signal()))
  assert.equal(older.length, 0)
})

test("a workflow change request rides the change-request word's second shape, only at API 128", async () => {
  const seen: Parameters<typeof source>[2] = []
  const api = charterApiFor(await source(grant(128), () => ({ contract_version: "project-assistants.v1" }), seen))!
  assert.equal(api.workflow, true)
  await api.requestChange(project, { text: "Send a failed reliability review back.", target: "workflow" }, signal())
  await api.requestChange(project, { text: "Add the review step.", target: "text" }, signal())
  const row = rows.find(r => r.op === "project-assistants.charter-change-request")!
  assert.deepEqual(seen.map(r => r.word), ["project-assistants.charter-change-request", "project-assistants.charter-change-request"])
  assert.deepEqual(Object.keys(JSON.parse(String(seen[0].init?.body))).sort(), [...row.shapes[1].local_body!].sort())
  assert.equal(seen[0].fields.target, "workflow")
  // The text target is the default and is left out, as an older machine reads it.
  assert.deepEqual(Object.keys(JSON.parse(String(seen[1].init?.body))).sort(), [...row.shapes[0].local_body!].sort())

  // The workflow write relays as its own word with exactly its local body; a bad target never leaves.
  const write = { machine_id: "machine", project_id: "project-one", idempotency_key: "wf-1", expected_version: 2, reason: "", workflow: { states: [], transitions: [] } }
  const relayed = assistantRelayRequest("/v1/project-assistant-charters/workflow", { method: "PUT", body: JSON.stringify(write), headers: { "Idempotency-Key": "wf-1" } })
  assert.equal(relayed.word, "project-assistants.charter-workflow-save")
  assert.deepEqual(Object.keys(relayed.fields).sort(), [...rows.find(r => r.op === relayed.word)!.shapes[0].local_body!].sort())
  assert.throws(() => assistantRelayRequest("/v1/project-assistant-charters/change-request", { method: "POST", body: JSON.stringify({ machine_id: "machine", project_id: "p", idempotency_key: "k", text: "x", target: "sideways" }), headers: { "Idempotency-Key": "k" } }))

  const older: Parameters<typeof source>[2] = []
  const old = charterApiFor(await source(grant(127), () => ({ contract_version: "project-assistants.v1" }), older))!
  assert.equal(old.workflow, false)
  await assert.rejects(old.requestChange(project, { text: "x", target: "workflow" }, signal()), (e: unknown) => e instanceof CharterRefusal && e.field === "target")
  assert.equal(older.length, 0)
})

test("the fixture carries the team's workflow and the lead's pending workflow proposal", async () => {
  const bots = [{ bot_id: "atlas", name: "Atlas", lead: true }, { bot_id: "xavia", name: "Xavia" }, { bot_id: "fuse", name: "Fuse" }, { bot_id: "lens", name: "Lens" }, { bot_id: "beacon", name: "Beacon" }]
  const workflow = sampleWorkflow({ atlas: "atlas", xavia: "xavia", fuse: "fuse", lens: "lens", beacon: "beacon" })
  const fixture = createCharterFixture("machine", [{ project_id: "project-one", bots, versions: [{ text: "## Who\n\nLead plans.", author: "template", reason: "" }], workflow, workflow_proposal: sampleWorkflowProposal(workflow) }])
  const api = charterApiFor({ machineID: "machine", ...fixture } as never)!
  assert.equal(api.workflow, true)
  const charter = await api.read(project, 0, signal())
  const flow = charterWorkflow(charter)!
  assert.equal(charter.version, 2)
  assert.deepEqual(flow.states.map(s => [s.id, s.title, s.bot_id, s.kind]), [["card", "開交辦卡", "", "start"], ["review", "派工・審證據", "atlas", "step"], ["integrate", "整合・推 GitHub・裝 dev", "xavia", "step"],
    ["reliability", "可靠性審查", "fuse", "step"], ["visual", "畫面驗收", "lens", "step"], ["deploy", "部署 Cloud", "xavia", "step"], ["website", "更新官網與 README", "beacon", "step"], ["done", "完成", "", "done"]])
  assert.deepEqual(flow.transitions.filter(t => t.from === "visual").map(t => [t.to, t.label, t.kind]), [["deploy", "通過", "pass"], ["review", "不通過", "return"]])
  assert.deepEqual(flow.transitions.filter(t => t.from === "deploy").map(t => [t.to, t.label]), [["website", "對外看得到"], ["done", "其他"]])
  // Every Bot heard the version before the workflow, so the workflow is news to all of them.
  assert.ok((await api.readers(project, signal())).readers.every(r => r.delivery_version === 2 && r.version === 1))
  const pending = await api.proposals(project, signal())
  assert.deepEqual(workflowProposals(pending).map(p => [proposalKind(p), p.text, p.stale]), [["workflow", "", false]])
  const added = workflowProposals(pending)[0].workflow!.transitions.filter(t => !flow.transitions.some(o => o.from === t.from && o.to === t.to))
  assert.deepEqual(added, [{ from: "reliability", to: "review", label: "不通過", kind: "return" }])
  // Applying it keeps the text and reaches every Bot; a later text save keeps it.
  const applied = await api.applyProposal(project, pending.proposals[0].id, { expected_version: 2 }, signal())
  assert.equal(applied.text, charter.text)
  assert.equal(applied.history[0].author, "lead_proposal")
  assert.equal(charterWorkflow(applied)!.transitions.length, 11)
  const saved = await api.save(project, { expected_version: 3, text: "## Who\n\nLead plans; makers build.", reason: "" }, signal())
  assert.equal(charterWorkflow(saved)!.transitions.length, 11)
  assert.equal(charterWorkflow(await api.read(project, 1, signal()), true), null)
  // A workflow change request comes back as a workflow proposal.
  await api.requestChange(project, { text: "Lens checks both widths.", target: "workflow" }, signal())
  const scripted = (await api.proposals(project, signal())).proposals[0]
  assert.equal(proposalKind(scripted), "workflow")
  assert.equal(scripted.workflow!.states[0].note, "Lens checks both widths.")
  assert.deepEqual(workflowRefusalSubject("workflow_unreachable:review"), { rule: "workflow_unreachable", subject: "review" })
  assert.deepEqual(workflowRefusalSubject("workflow_done"), { rule: "workflow_done", subject: "" })
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
