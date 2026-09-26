// Moving a schedule between machines, and the list that shows every machine's:
// `node --test web/console/src/cloud/schedule-move.test.ts`.
import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { moveSchedule, planScheduleMove, recordBody, refusedByOlderTarget, retargetInstructions, targetBody, type MoveRecord, type MovePlace } from "./schedule-move.ts"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { choosesMachine, groupSchedules, type ScheduleFleet } from "./schedule-machines.ts"

const record: MoveRecord = {
  id: "s1",
  title: "Nightly",
  enabled: true,
  when: { at: "09:00", days: "daily" },
  task: { assistant: "claude", model: "opus", project_dir: "/Users/alice/tool", instructions: "run it", timeout_minutes: 45 },
  close_tab: "always",
  catch_up_hours: 2,
  notify_on_failure: false,
  webhook_binding_availability: "unbound",
}
const mac = { id: "mac-a", name: "Studio", online: true }
const linux = { id: "linux-b", name: "Runner", online: true }
const sourcePlaces: MovePlace[] = [{ id: "cloud.src", path: "/Users/alice/tool", label: "tool", repo: "example.com/team/tool" }]
const targetPlaces: MovePlace[] = [
  { id: "cloud.other", path: "/home/bob/other", label: "other", repo: "example.com/team/other" },
  { id: "cloud.dst", path: "/home/bob/tool", label: "tool", repo: "example.com/team/tool" },
]

function plan(over: Partial<Parameters<typeof planScheduleMove>[0]> = {}) {
  return planScheduleMove({ record, source: mac, target: linux, sourcePlaces, targetPlaces, ...over })
}

test("a move finds the project on the target by the repository it clones, not its path", () => {
  const answer = plan()
  assert.ok("plan" in answer)
  assert.equal(answer.plan.sourcePlace, "cloud.src")
  assert.deepEqual(answer.plan.targets.map((p: MovePlace) => p.id), ["cloud.dst"])
})

test("every refusal is asked before anything is written, and names what to act on", () => {
  const cases: [string, Partial<Parameters<typeof planScheduleMove>[0]>, unknown][] = [
    ["an offline target", { target: { ...linux, online: false } },
      { code: "schedule_move_target_offline", machine: "Runner" }],
    ["an unreadable target list", { targetPlaces: null },
      { code: "schedule_move_target_offline", machine: "Runner" }],
    ["a target older than `repo`", { targetPlaces: [{ id: "x", path: "/home/bob/tool", label: "tool" }] },
      { code: "schedule_move_target_outdated", machine: "Runner" }],
    ["a source older than `repo`", { sourcePlaces: [{ id: "cloud.src", path: "/Users/alice/tool", label: "tool" }] },
      { code: "schedule_move_source_outdated", machine: "Studio" }],
    ["a project with no origin", { sourcePlaces: [{ ...sourcePlaces[0], repo: "" }] },
      { code: "schedule_move_no_origin", project: "tool" }],
    ["a project the source no longer lists", { sourcePlaces: [] },
      { code: "schedule_move_source_unlisted", project: "tool", machine: "Studio" }],
    ["a target without the project", { targetPlaces: [targetPlaces[0]] },
      { code: "schedule_move_no_project", machine: "Runner", repo: "example.com/team/tool" }],
    ["a bound webhook", { record: { ...record, webhook_binding_availability: "active" } },
      { code: "schedule_move_webhook_bound", title: "Nightly", machine: "Studio" }],
    ["an unreadable binding", { record: { ...record, webhook_binding_availability: "binding_store_unavailable" } },
      { code: "schedule_move_webhook_unknown", title: "Nightly", machine: "Studio" }],
    ["a one-time schedule that ran", { record: { ...record, fired_at: 1_790_000_000 } },
      { code: "schedule_move_spent", title: "Nightly" }],
  ]
  for (const [name, over, refusal] of cases) {
    assert.deepEqual(plan(over), { refusal }, name)
  }
})

function recorder(fail: Partial<Record<"disable" | "create" | "restore" | "remove", boolean>> = {}) {
  const calls: string[] = []
  let updates = 0
  return {
    calls,
    writes: {
      async update(id: string, body: Record<string, unknown>, machine: string) {
        const step = updates++ === 0 ? "disable" : "restore"
        calls.push(`${step} ${id}@${machine} enabled=${body.enabled}`)
        if (fail[step]) throw Object.assign(new Error(step), { code: "machine_offline" })
      },
      async create(body: Record<string, unknown>) {
        calls.push(`create ${body.place_id}`)
        if (fail.create) throw Object.assign(new Error("create"), { code: "bad_request" })
        return { schedule: { id: "s2" } }
      },
      async remove(id: string, machine: string) {
        calls.push(`remove ${id}@${machine}`)
        if (fail.remove) throw Object.assign(new Error("remove"), { code: "machine_offline" })
      },
    },
  }
}

const movePlan = { sourcePlace: "cloud.src", repo: "example.com/team/tool", targets: [targetPlaces[1]] }
const copy = { title: "Nightly", at: "09:00", days: "daily", place_id: "cloud.dst", enabled: true }

test("a move disables the source, creates the copy, then deletes the source — in that order", async () => {
  const r = recorder()
  const outcome = await moveSchedule(r.writes, { record, source: "mac-a", plan: movePlan, copy })
  assert.equal(outcome.state, "moved")
  assert.deepEqual(r.calls, ["disable s1@mac-a enabled=false", "create cloud.dst", "remove s1@mac-a"])
})

test("a refused copy switches the source back to what it was", async () => {
  const r = recorder({ create: true })
  const outcome = await moveSchedule(r.writes, { record, source: "mac-a", plan: movePlan, copy })
  assert.deepEqual({ state: outcome.state, restored: (outcome as { restored?: boolean }).restored }, { state: "create_failed", restored: true })
  assert.deepEqual(r.calls, ["disable s1@mac-a enabled=false", "create cloud.dst", "restore s1@mac-a enabled=true"])
  // A source that was disabled before the move is left disabled, as it was.
  const off = recorder({ create: true })
  await moveSchedule(off.writes, { record: { ...record, enabled: false }, source: "mac-a", plan: movePlan, copy })
  assert.equal(off.calls[2], "restore s1@mac-a enabled=false")
})

test("a restore that also fails is said, not hidden", async () => {
  const r = recorder({ create: true, restore: true })
  const outcome = await moveSchedule(r.writes, { record, source: "mac-a", plan: movePlan, copy })
  assert.deepEqual({ state: outcome.state, restored: (outcome as { restored?: boolean }).restored }, { state: "create_failed", restored: false })
})

test("a refused disable writes nothing else, and a refused delete leaves the source disabled", async () => {
  const first = recorder({ disable: true })
  assert.equal((await moveSchedule(first.writes, { record, source: "mac-a", plan: movePlan, copy })).state, "not_started")
  assert.deepEqual(first.calls, ["disable s1@mac-a enabled=false"])
  const last = recorder({ remove: true })
  assert.equal((await moveSchedule(last.writes, { record, source: "mac-a", plan: movePlan, copy })).state, "delete_failed")
  assert.deepEqual(last.calls, ["disable s1@mac-a enabled=false", "create cloud.dst", "remove s1@mac-a"])
})

test("the disable carries every stored field and leaves the model alone", () => {
  assert.deepEqual(recordBody(record, "cloud.src", false), {
    title: "Nightly", at: "09:00", days: "daily", place_id: "cloud.src", assistant: "claude",
    instructions: "run it", enabled: false, close_tab: "always", catch_up_hours: 2,
    notify_on_failure: false, timeout_minutes: 45,
  })
  // A one-time schedule's date goes with the copy; the form has no control for it.
  const once = { ...record, when: { at: "09:00", on: "2026-10-01" } }
  assert.deepEqual(targetBody({ title: "x", days: "daily", place_id: "old" }, once, targetPlaces[1]),
    { title: "x", place_id: "cloud.dst", on: "2026-10-01" })
})

test("the copy carries the settings the form does not show, and sends no template when there are none", () => {
  const hidden: MoveRecord = {
    ...record,
    task: { ...record.task, deliverables: ["docs/report.md"], claims: ["web"], isolation: "worktree" },
  }
  assert.ok("plan" in plan({ record: hidden }), "a schedule with deliverables and claims is moved")
  const copy = targetBody({ title: "Nightly", instructions: "run it" }, hidden, targetPlaces[1])
  assert.deepEqual(copy.template, { claims: ["web"], isolation: "worktree", deliverables: ["docs/report.md"] })
  // Without any, the key is absent, so a target older than it still takes the copy.
  assert.equal("template" in targetBody({ title: "Nightly" }, record, targetPlaces[1]), false)
})

test("the copy carries the form's permission as the form field, never in template", () => {
  const full: MoveRecord = { ...record, task: { ...record.task, claims: ["web"], permission_mode: "full" } }
  assert.ok("plan" in plan({ record: full }), "a schedule with a permission setting is moved")
  const copy = targetBody({ title: "Nightly", permission_mode: "full" }, full, targetPlaces[1])
  assert.equal(copy.permission_mode, "full")
  assert.deepEqual(copy.template, { claims: ["web"] })
  // "Not set" has nothing to take off a copy being made, so the key is not
  // sent, and a target older than the field still takes it.
  assert.equal("permission_mode" in targetBody({ title: "Nightly", permission_mode: "" }, record, targetPlaces[1]), false)
  // The disable is a save that leaves the stored permission alone.
  assert.equal("permission_mode" in recordBody(full, "cloud.src", false), false)
})

test("the project directory in the first message becomes the target's, as a whole path only", () => {
  const from = "/Users/alice/code/dual"
  const to = "/home/bob/dual"
  const cases: [string, string][] = [
    ["你在 /Users/alice/code/dual。先讀 README。", "你在 /home/bob/dual。先讀 README。"],
    ["cd /Users/alice/code/dual/web && npm test", "cd /home/bob/dual/web && npm test"],
    ["Work in /Users/alice/code/dual.", "Work in /home/bob/dual."],
    ["`/Users/alice/code/dual` then /Users/alice/code/dual", "`/home/bob/dual` then /home/bob/dual"],
    // Not this project: a longer name, a longer path, another file.
    ["/Users/alice/code/dual-astro stays", "/Users/alice/code/dual-astro stays"],
    ["/Users/alice/code/dual.git stays", "/Users/alice/code/dual.git stays"],
    ["/mnt/Users/alice/code/dual stays", "/mnt/Users/alice/code/dual stays"],
    ["no path here", "no path here"],
  ]
  for (const [text, want] of cases) assert.equal(retargetInstructions(text, from, to), want, text)
  assert.equal(retargetInstructions("in /Users/alice/code/dual", from + "/", to + "/"), "in /home/bob/dual")
  const copy = targetBody({ instructions: "你在 /Users/alice/tool。" }, record, targetPlaces[1])
  assert.equal(copy.instructions, "你在 /home/bob/tool。")
})

test("a target older than template or the permission field is told apart from any other refusal", () => {
  assert.equal(refusedByOlderTarget(new Error("unknown field: permission_mode")), true)
  assert.equal(refusedByOlderTarget(new Error("unknown field: permission_mode, template")), true)
  assert.equal(refusedByOlderTarget(new Error("task.permission_mode must be one of: ask, edits, full")), false)
  assert.equal(refusedByOlderTarget(Object.assign(new Error("unknown field: template"), { code: "bad_request" })), true)
  assert.equal(refusedByOlderTarget(new Error("unknown field: on, template")), true)
  assert.equal(refusedByOlderTarget(new Error("unknown template field: model")), false)
  assert.equal(refusedByOlderTarget(new Error("place_id must be one of the ids GET /v1/places lists.")), false)
  assert.equal(refusedByOlderTarget(null), false)
})

test("a target that refuses the template is not asked again without it", async () => {
  const sent: Record<string, unknown>[] = []
  const writes = {
    update: async () => ({}),
    create: async (body: Record<string, unknown>) => {
      sent.push(body)
      throw new Error("unknown field: template")
    },
    remove: async () => ({}),
  }
  const hidden: MoveRecord = { ...record, task: { ...record.task, deliverables: ["docs/report.md"] } }
  const answer = plan({ record: hidden })
  assert.ok("plan" in answer)
  const outcome = await moveSchedule(writes, {
    record: hidden, source: "mac-a", plan: answer.plan,
    copy: targetBody({ title: "Nightly" }, hidden, targetPlaces[1]),
  })
  assert.equal(outcome.state, "create_failed")
  assert.equal(sent.length, 1)
  assert.ok(refusedByOlderTarget((outcome as { error: unknown }).error))
})

test("a target that refuses the permission field is not asked again without it", async () => {
  const sent: Record<string, unknown>[] = []
  const writes = {
    update: async () => ({}),
    create: async (body: Record<string, unknown>) => {
      sent.push(body)
      throw Object.assign(new Error("unknown field: permission_mode"), { code: "bad_request" })
    },
    remove: async () => ({}),
  }
  const full: MoveRecord = { ...record, task: { ...record.task, permission_mode: "full" } }
  const answer = plan({ record: full })
  assert.ok("plan" in answer)
  const outcome = await moveSchedule(writes, {
    record: full, source: "mac-a", plan: answer.plan,
    copy: targetBody({ title: "Nightly", permission_mode: "full" }, full, targetPlaces[1]),
  })
  assert.equal(outcome.state, "create_failed")
  assert.equal(sent.length, 1)
  assert.equal(sent[0].permission_mode, "full")
  assert.ok(refusedByOlderTarget((outcome as { error: unknown }).error))
})

const fleet: ScheduleFleet = {
  current: "linux-b",
  machines: [
    { id: "mac-a", name: "Studio", platform: "macOS", seenAt: null, online: true },
    { id: "linux-b", name: "Runner", platform: "Linux", seenAt: null, online: true },
    { id: "mac-c", name: "Spare", platform: "macOS", seenAt: 1_790_000_000_000, online: false },
  ],
}

test("the list is one group per machine, the header's first; a silent machine is a named group, never an empty list", () => {
  const rows = [
    { id: "1", machine: "mac-a" }, { id: "2", machine: "linux-b" }, { id: "3", machine: "gone" },
  ]
  const groups = groupSchedules(rows, [{ machine: "mac-c", code: "machine_offline" }], fleet)
  assert.deepEqual(groups.map((g: { machine: { id: string }; rows: { id: string }[]; unanswered: string | null }) =>
    [g.machine.id, g.rows.map((r) => r.id), g.unanswered]), [
    ["linux-b", ["2"], null],
    ["mac-a", ["1"], null],
    ["mac-c", [], "machine_offline"],
  ])
  // A machine that answered with nothing draws nothing.
  assert.deepEqual(groupSchedules([], [], fleet), [])
})

test("the machine is only chosen when there is more than one to choose from", () => {
  assert.equal(choosesMachine(null), false)
  assert.equal(choosesMachine({ current: "a", machines: [fleet.machines[0]] }), false)
  assert.equal(choosesMachine(fleet), true)
})
