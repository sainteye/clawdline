// Moving a schedule between machines, and the list that shows every machine's:
// `node --test web/console/src/cloud/schedule-move.test.ts`.
import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { askAgainBeforeRefusing, createdScheduleID, hookMoveRequest, movedRevision, moveSchedule, planScheduleMove, recordBody, refusedAsStaleRevision, refusedByOlderBinder, refusedByOlderTarget, retargetInstructions, targetBody, type MoveHook, type MoveRecord, type MovePlace } from "./schedule-move.ts"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { answerSchedulePresence, choosesMachine, groupSchedules, noteScheduleMachineAnswered, publishScheduleFleet, recheckSchedulePresence, scheduleFleet, type ScheduleFleet } from "./schedule-machines.ts"

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

const bound: MoveRecord = { ...record, webhook_binding_availability: "active", webhook_hook_id: "swh_1" }
const hook: MoveHook = { hook_id: "swh_1", state: "active", revision: 3 }

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
    ["a bound webhook Cloud could not be read about", { record: bound },
      { code: "schedule_move_webhook_unknown", title: "Nightly", machine: "Studio" }],
    ["a bound webhook Cloud answers for another hook", { record: bound, hook: { ...hook, hook_id: "swh_other" } },
      { code: "schedule_move_webhook_unknown", title: "Nightly", machine: "Studio" }],
    ["a bound webhook in a state nothing moves", { record: bound, hook: { ...hook, state: "unknown" } },
      { code: "schedule_move_webhook_unknown", title: "Nightly", machine: "Studio" }],
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

const movePlan = { sourcePlace: "cloud.src", repo: "example.com/team/tool", targets: [targetPlaces[1]], hook: null, hookLeftDisabled: false }
const copy = { title: "Nightly", at: "09:00", days: "daily", place_id: "cloud.dst", enabled: true }

test("a move disables the source, creates the copy, then deletes the source — in that order", async () => {
  const r = recorder()
  const outcome = await moveSchedule(r.writes, { record, source: "mac-a", target: "linux-b", plan: movePlan, copy })
  assert.equal(outcome.state, "moved")
  assert.deepEqual(r.calls, ["disable s1@mac-a enabled=false", "create cloud.dst", "remove s1@mac-a"])
})

test("a refused copy switches the source back to what it was", async () => {
  const r = recorder({ create: true })
  const outcome = await moveSchedule(r.writes, { record, source: "mac-a", target: "linux-b", plan: movePlan, copy })
  assert.deepEqual({ state: outcome.state, restored: (outcome as { restored?: boolean }).restored }, { state: "create_failed", restored: true })
  assert.deepEqual(r.calls, ["disable s1@mac-a enabled=false", "create cloud.dst", "restore s1@mac-a enabled=true"])
  // A source that was disabled before the move is left disabled, as it was.
  const off = recorder({ create: true })
  await moveSchedule(off.writes, { record: { ...record, enabled: false }, source: "mac-a", target: "linux-b", plan: movePlan, copy })
  assert.equal(off.calls[2], "restore s1@mac-a enabled=false")
})

test("a restore that also fails is said, not hidden", async () => {
  const r = recorder({ create: true, restore: true })
  const outcome = await moveSchedule(r.writes, { record, source: "mac-a", target: "linux-b", plan: movePlan, copy })
  assert.deepEqual({ state: outcome.state, restored: (outcome as { restored?: boolean }).restored }, { state: "create_failed", restored: false })
})

test("a refused disable writes nothing else, and a refused delete leaves the source disabled", async () => {
  const first = recorder({ disable: true })
  assert.equal((await moveSchedule(first.writes, { record, source: "mac-a", target: "linux-b", plan: movePlan, copy })).state, "not_started")
  assert.deepEqual(first.calls, ["disable s1@mac-a enabled=false"])
  const last = recorder({ remove: true })
  assert.equal((await moveSchedule(last.writes, { record, source: "mac-a", target: "linux-b", plan: movePlan, copy })).state, "delete_failed")
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
    record: hidden, source: "mac-a", target: "linux-b", plan: answer.plan,
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
    record: full, source: "mac-a", target: "linux-b", plan: answer.plan,
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

// ---- A bound webhook moves with its schedule and keeps its URL ------------

test("a bound webhook is planned to move with the schedule; a disabled one is left and said", () => {
  for (const state of ["active", "pending_binding"]) {
    const answer = plan({ record: bound, hook: { ...hook, state } })
    assert.ok("plan" in answer, state)
    assert.deepEqual({ hook: answer.plan.hook, left: answer.plan.hookLeftDisabled },
      { hook: { id: "swh_1", revision: 3 }, left: false }, state)
  }
  const disabled = plan({ record: bound, hook: { ...hook, state: "disabled" } })
  assert.ok("plan" in disabled)
  assert.deepEqual({ hook: disabled.plan.hook, left: disabled.plan.hookLeftDisabled }, { hook: null, left: true })
  // Unbound: nothing to move, and Cloud is not needed.
  const plain = plan()
  assert.ok("plan" in plain)
  assert.deepEqual({ hook: plain.plan.hook, left: plain.plan.hookLeftDisabled }, { hook: null, left: false })
})

type HookFail = Partial<Record<"disable" | "create" | "restore" | "remove" | "copy" | "move" | "moveBack" | "bind" | "rebind", boolean | string>>

/**
 * Every write, the hook's included, in the order it happened. Cloud's moves
 * answer the hook at the revision they raised it to: 4 forward, 5 back.
 * A string in `fail` is the refusal's code.
 */
function hookRecorder(fail: HookFail = {}, created: unknown = { ok: true, schedule: { id: "s2" } }) {
  const calls: string[] = []
  let updates = 0
  let moves = 0
  let binds = 0
  const refuse = (step: keyof HookFail) => {
    const code = typeof fail[step] === "string" ? fail[step] : step === "move" ? "stale_revision" : "machine_offline"
    if (fail[step]) throw Object.assign(new Error(step), { code })
  }
  return {
    calls,
    writes: {
      async update(id: string, body: Record<string, unknown>, machine: string) {
        const step = updates++ === 0 ? "disable" : "restore"
        calls.push(`${step} ${id}@${machine} enabled=${body.enabled}`)
        refuse(step)
      },
      async create(body: Record<string, unknown>) {
        calls.push(`create ${body.place_id}`)
        refuse("create")
        return created
      },
      async remove(id: string, machine: string) {
        calls.push(`remove ${id}@${machine}`)
        refuse(id === "s1" ? "remove" : "copy")
      },
      async moveHook(hookID: string, machine: string, revision: number | null) {
        const step = moves++ === 0 ? "move" : "moveBack"
        calls.push(`${step} ${hookID}->${machine} rev=${revision}`)
        refuse(step)
        return { schema: "clawdline.schedule_webhook.management.v1",
          hook: { hook_id: hookID, state: "pending_binding", revision: step === "move" ? 4 : 5 } }
      },
      async bindHook(hookID: string, scheduleID: string, machine: string, revision: number | null) {
        const step = binds++ === 0 ? "bind" : "rebind"
        calls.push(`${step} ${hookID}->${scheduleID}@${machine} rev=${revision}`)
        refuse(step)
      },
    },
  }
}

const hookPlan = { ...movePlan, hook: { id: "swh_1", revision: 3 } }
const hookMove = (r: ReturnType<typeof hookRecorder>) =>
  moveSchedule(r.writes, { record: bound, source: "mac-a", target: "linux-b", plan: hookPlan, copy })

test("the hook moves after the copy exists and before the source is deleted, bound to the copy's id", async () => {
  const r = hookRecorder()
  assert.equal((await hookMove(r)).state, "moved")
  assert.deepEqual(r.calls, [
    "disable s1@mac-a enabled=false",
    "create cloud.dst",
    "move swh_1->linux-b rev=3",
    "bind swh_1->s2@linux-b rev=4",
    "remove s1@mac-a",
  ])
})

test("Cloud refusing the hook's move deletes the copy and switches the source back; the hook is untouched", async () => {
  const r = hookRecorder({ move: true })
  const outcome = await hookMove(r)
  assert.deepEqual({ ...outcome, error: undefined },
    { state: "hook_move_failed", error: undefined, copyRemoved: true, restored: true })
  assert.deepEqual(r.calls, [
    "disable s1@mac-a enabled=false",
    "create cloud.dst",
    "move swh_1->linux-b rev=3",
    "remove s2@linux-b",
    "restore s1@mac-a enabled=true",
  ])
  // And when the undo cannot finish, each part says so.
  const stuck = await hookMove(hookRecorder({ move: true, copy: true, restore: true }))
  assert.deepEqual({ ...stuck, error: undefined },
    { state: "hook_move_failed", error: undefined, copyRemoved: false, restored: false })
})

test("a target that does not bind moves the hook back, binds it to the source schedule again, then undoes the copy", async () => {
  const r = hookRecorder({ bind: true })
  const outcome = await hookMove(r)
  assert.equal(outcome.state, "hook_bind_failed")
  assert.deepEqual((outcome as { rollback: unknown }).rollback,
    { hookBack: true, rebound: true, copyRemoved: true, restored: true })
  assert.deepEqual(r.calls, [
    "disable s1@mac-a enabled=false",
    "create cloud.dst",
    "move swh_1->linux-b rev=3",
    "bind swh_1->s2@linux-b rev=4",
    // The forward move changed the revision: the way back reads it again.
    "moveBack swh_1->mac-a rev=null",
    "rebind swh_1->s1@mac-a rev=5",
    "remove s2@linux-b",
    "restore s1@mac-a enabled=true",
  ])
  assert.ok(!r.calls.includes("remove s1@mac-a"), "the source is never deleted on a failed bind")
})

test("each bind carries the revision Cloud's move answered, not the one read before it", async () => {
  // The move raised 3 to 4 and the move back 4 to 5; Cloud activates a hook
  // only at the revision it is at, so a bind at any other number is refused.
  const r = hookRecorder({ bind: true })
  await hookMove(r)
  assert.deepEqual(r.calls.filter((c) => c.includes("bind")), [
    "bind swh_1->s2@linux-b rev=4",
    "rebind swh_1->s1@mac-a rev=5",
  ])
  // A move answer that names no revision leaves it to be read before the bind.
  assert.equal(movedRevision({ hook: { revision: 4 } }), 4)
  assert.equal(movedRevision({ revision: 0 }), 0)
  assert.equal(movedRevision({ hook: {} }), null)
  assert.equal(movedRevision(undefined), null)
  assert.equal(movedRevision({ hook: { revision: -1 } }), null)
})

test("a target too old for hook_revision is undone before anything is lost, and a source as old leaves the hook paused there", async () => {
  // The target's Cloud bridge refuses the unknown key; the move is undone and
  // the source, being new enough, binds the hook again at the move-back revision.
  const target = hookRecorder({ bind: "malformed_command" })
  const t = await hookMove(target)
  assert.equal(t.state, "hook_bind_failed")
  assert.ok(refusedByOlderBinder((t as { error: unknown }).error))
  assert.deepEqual((t as { rollback: unknown }).rollback, { hookBack: true, rebound: true, copyRemoved: true, restored: true })
  assert.ok(!target.calls.includes("remove s1@mac-a"), "the source is never deleted")
  // Both too old: the schedule is back on the source, enabled, the copy gone,
  // and the hook on the source, paused — the page names that machine.
  const both = await hookMove(hookRecorder({ bind: "malformed_command", rebind: "malformed_command" }))
  assert.deepEqual((both as { rollback: unknown }).rollback, { hookBack: true, rebound: false, copyRemoved: true, restored: true })
})

test("the bind's refusals are told apart: an older machine, and a hook at another revision", () => {
  assert.equal(refusedByOlderBinder({ code: "malformed_command", status: 400 }), true)
  assert.equal(refusedByOlderBinder({ code: "bad_request", status: 400 }), true)
  assert.equal(refusedByOlderBinder({ code: "stale_revision", status: 409 }), false)
  assert.equal(refusedByOlderBinder({ code: "temporarily_unavailable", status: 503 }), false)
  assert.equal(refusedByOlderBinder(null), false)
  assert.equal(refusedAsStaleRevision({ code: "stale_revision", status: 409 }), true)
  assert.equal(refusedAsStaleRevision({ code: "temporarily_unavailable", status: 503 }), false)
})

test("a rollback that cannot finish says which step did not happen", async () => {
  // The hook could not come back: it stays on the target, paused, and is not re-bound.
  const away = hookRecorder({ bind: true, moveBack: true })
  const a = await hookMove(away)
  assert.deepEqual((a as { rollback: unknown }).rollback, { hookBack: false, rebound: false, copyRemoved: true, restored: true })
  assert.ok(!away.calls.some((c) => c.startsWith("rebind")), "a hook that did not come back is not bound here")
  // The hook came back and the source did not activate it: paused on the source.
  const paused = await hookMove(hookRecorder({ bind: true, rebind: true, copy: true }))
  assert.deepEqual((paused as { rollback: unknown }).rollback, { hookBack: true, rebound: false, copyRemoved: false, restored: true })
})

test("a copy whose id the target did not answer is removed from nowhere and the hook is never moved", async () => {
  const r = hookRecorder({}, { ok: true })
  const outcome = await hookMove(r)
  assert.deepEqual({ ...outcome, error: undefined },
    { state: "hook_move_failed", error: undefined, copyRemoved: false, restored: true })
  assert.ok(!r.calls.some((c) => c.startsWith("move")), r.calls.join(", "))
  assert.equal(createdScheduleID({ schedule: { id: "s2" } }), "s2")
  assert.equal(createdScheduleID({ schedule: {} }), null)
})

test("a refused delete after the hook moved leaves it on the target, bound to the copy", async () => {
  const r = hookRecorder({ remove: true })
  assert.equal((await hookMove(r)).state, "delete_failed")
  assert.deepEqual(r.calls.slice(-3), ["move swh_1->linux-b rev=3", "bind swh_1->s2@linux-b rev=4", "remove s1@mac-a"])
})

test("a page without Cloud webhook management cannot move a bound hook, and undoes the copy", async () => {
  const r = hookRecorder()
  const { moveHook: _m, bindHook: _b, ...plain } = r.writes
  const outcome = await moveSchedule(plain, { record: bound, source: "mac-a", target: "linux-b", plan: hookPlan, copy })
  assert.deepEqual({ ...outcome, error: undefined },
    { state: "hook_move_failed", error: undefined, copyRemoved: true, restored: true })
})

test("the Cloud move is POST /v1/schedule-webhooks/:id/move with exactly machine_id and expected_revision", async () => {
  assert.deepEqual(hookMoveRequest("swh_1", "mac_target", 3), {
    method: "POST",
    path: "/v1/schedule-webhooks/swh_1/move",
    body: { machine_id: "mac_target", expected_revision: 3 },
  })
  // Sent through the copied client's own seam, which adds the session and an Idempotency-Key.
  const { ScheduleWebhookClient } = await import("../legacy/js/net/schedule-webhooks.js")
  const sent: { url: string; method: string; headers: Record<string, string>; body: unknown }[] = []
  const client = new ScheduleWebhookClient({
    origin: "https://api.example.test/",
    idempotencyKey: () => "intent-1",
    fetch: async (url: string, init: { method: string; headers: Record<string, string>; body: string }) => {
      sent.push({ url, method: init.method, headers: init.headers, body: JSON.parse(init.body) })
      return new Response(JSON.stringify({ hook: { hook_id: "swh_1", state: "pending_binding", revision: 4 } }), { status: 200 })
    },
  })
  const request = hookMoveRequest("swh_1", "mac_target", 3)
  const answer = await client.send(request.method, request.path, request.body, true)
  assert.deepEqual(sent, [{
    url: "https://api.example.test/v1/schedule-webhooks/swh_1/move",
    method: "POST",
    headers: { Accept: "application/json", "Content-Type": "application/json", "Idempotency-Key": "intent-1" },
    body: { machine_id: "mac_target", expected_revision: 3 },
  }])
  assert.equal(answer.hook.state, "pending_binding")
})

// A save asks again before it calls a machine offline: the fleet row and the
// places are snapshots from when the page opened and the machine was picked.

/** The save's decision as the page makes it: ask again, then plan from what came back. */
async function saveFrom(held: { targetOnline: boolean; targetPlaces: MovePlace[] | null; sourcePlaces: MovePlace[] | null }, ask: {
  presence?: () => Promise<ReadonlyMap<string, boolean> | null>
  places?: (machine: string) => Promise<MovePlace[] | null>
}) {
  const asked: string[] = []
  const fresh = await askAgainBeforeRefusing(held, { source: mac.id, target: linux.id }, {
    presence: async () => {
      asked.push("presence")
      return ask.presence ? ask.presence() : null
    },
    places: async (machine: string) => {
      asked.push("places " + machine)
      if (!ask.places) throw new Error("offline")
      return ask.places(machine)
    },
  })
  if (!fresh.sourcePlaces) return { asked, fresh, answer: { refusal: { code: "source_offline" } } }
  const answer = planScheduleMove({
    record,
    source: mac,
    target: { ...linux, online: fresh.targetOnline },
    sourcePlaces: fresh.sourcePlaces,
    targetPlaces: fresh.targetPlaces,
  })
  return { asked, fresh, answer }
}

test("a target the page saw offline that answers now is moved to, not refused", async () => {
  const { asked, answer } = await saveFrom(
    { targetOnline: false, targetPlaces: null, sourcePlaces },
    { presence: async () => new Map([[linux.id, true]]), places: async () => targetPlaces },
  )
  assert.deepEqual(asked.sort(), ["places linux-b", "presence"])
  assert.ok("plan" in answer, JSON.stringify(answer))
  assert.deepEqual(answer.plan.targets.map((p: MovePlace) => p.id), ["cloud.dst"])
})

test("a target that answers its places is online, whatever an older presence reading says", async () => {
  const { answer } = await saveFrom(
    { targetOnline: false, targetPlaces: targetPlaces, sourcePlaces },
    { presence: async () => new Map([[linux.id, false]]), places: async () => targetPlaces },
  )
  assert.ok("plan" in answer, JSON.stringify(answer))
})

test("a target whose fresh read also fails is refused as offline", async () => {
  for (const ask of [
    {},
    { presence: async () => new Map([[linux.id, true]]) },
    { presence: async () => new Map([[linux.id, false]]), places: async () => null },
  ]) {
    const { asked, answer } = await saveFrom({ targetOnline: false, targetPlaces: null, sourcePlaces }, ask)
    assert.ok(asked.includes("places linux-b"))
    assert.deepEqual(answer, { refusal: { code: "schedule_move_target_offline", machine: "Runner" } })
  }
})

test("a source the page could not read is asked again before it is called offline", async () => {
  const back = await saveFrom(
    { targetOnline: true, targetPlaces, sourcePlaces: null },
    { places: async (machine: string) => (machine === mac.id ? sourcePlaces : null) },
  )
  assert.deepEqual(back.asked, ["places mac-a"])
  assert.ok("plan" in back.answer, JSON.stringify(back.answer))
  const gone = await saveFrom({ targetOnline: true, targetPlaces, sourcePlaces: null }, {})
  assert.deepEqual(gone.answer, { refusal: { code: "source_offline" } })
})

test("readings that said nothing was offline are not asked again", async () => {
  const { asked, fresh } = await saveFrom({ targetOnline: true, targetPlaces, sourcePlaces }, {})
  assert.deepEqual(asked, [])
  assert.equal(fresh.asked, false)
})

test("asking which machines report in brings the published fleet up to the answer", async () => {
  const fleet: ScheduleFleet = {
    current: "mac-a",
    machines: [
      { id: "mac-a", name: "Studio", platform: "macOS", seenAt: 10, online: true },
      { id: "linux-b", name: "Runner", platform: "Linux", seenAt: 5, online: false },
    ],
  }
  publishScheduleFleet(fleet)
  try {
    assert.equal(await recheckSchedulePresence(), null, "no gate: nothing to ask")
    answerSchedulePresence(async () => [{ id: "linux-b", online: true, seenAt: 20 }])
    const now = await recheckSchedulePresence()
    assert.equal(now?.get("linux-b"), true)
    assert.deepEqual(scheduleFleet()?.machines.map((m) => [m.id, m.online, m.seenAt]), [
      ["mac-a", true, 10],
      ["linux-b", true, 20],
    ])
    publishScheduleFleet(fleet)
    noteScheduleMachineAnswered("linux-b")
    assert.equal(scheduleFleet()?.machines[1].online, true, "a machine that answered a read is online")
  } finally {
    answerSchedulePresence(null)
    publishScheduleFleet(null)
  }
})
