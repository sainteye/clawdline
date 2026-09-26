// Moving a schedule from one machine to another.
//
// There is no machine-to-machine channel, and none may be added: Cloud carries
// only ciphertext and each machine has its own key. The browser is the courier,
// as it is for project settings (docs/project-sync.md): it reads the schedule
// from the source, writes a copy to the target, and removes the source.
//
// **The order is what keeps one occurrence from running twice.** (a) The
// source is disabled; (b) the copy is created on the target with every field
// carried over; (c) the source is deleted. At no instant are two enabled copies
// stored. The target does not run an occurrence from before the copy existed:
// a created schedule is first seen, and created, at that instant
// (`internal/app/schedule_move_test.go`). If (b) fails the source is switched
// back to what it was; if (c) fails the source stays disabled and the page says
// where it is.
//
// A Cloud webhook bound to the schedule moves with it and keeps its URL, so
// whoever calls it changes nothing. Between (b) and (c) the hook is moved on
// Cloud (`POST /v1/schedule-webhooks/:id/move`, which leaves it paused as
// `pending_binding`), then bound to the copy on the target with the same
// `schedule-webhook-bind-v1` step a new hook takes; the target activates it
// with its own machine credential. The hook is never active while pointing at
// a schedule that does not exist: it is paused from the Cloud move until the
// target has bound the copy. Deleting the source drops its local binding
// (`DeleteScheduleFile`). A disabled hook cannot be moved and is not needed:
// the schedule moves without it and the page says so.
//
// Everything that can refuse is asked before (a), so a refusal changes nothing.
// A place id is a digest of a path on one machine, so the project is found on
// the target by the repository it clones (`repo`, GET /v1/places).
//
// Nothing here is imported at run time, so `node --test` loads it as it is.

/** A place as `GET /v1/places` answers it; `repo` is absent from a daemon older than it. */
export interface MovePlace {
  id: string
  path: string
  label?: string
  repo?: string
}

/** The fields of `GET /v1/orchestrator/schedules/:id`'s record this reads. */
export interface MoveRecord {
  id: string
  title?: string
  enabled?: boolean
  when?: { at?: string; days?: string | string[]; on?: string }
  task?: Record<string, unknown> & {
    assistant?: string
    model?: string
    project_dir?: string
    instructions?: string
    timeout_minutes?: number
  }
  close_tab?: string
  catch_up_hours?: number
  notify_on_failure?: boolean
  fired_at?: number
  webhook_binding_availability?: string
  webhook_hook_id?: string
}

/** The bound hook as Cloud answers it; `null` when Cloud could not be read. */
export interface MoveHook {
  hook_id: string
  state: string
  revision: number
}

/**
 * Template fields a stored schedule may carry that the form has no control
 * for, and that travel: a save on the same machine keeps them (`build`,
 * internal/app/schedules.go), and a create takes them in `template`
 * (`createTemplate`), so the copy carries what the source's file said.
 * `permission_mode` is not one: it is a form field, so it travels in the form
 * the copy is made from, and `template` refuses it.
 */
export const CARRIED_TASK_FIELDS = [
  "claims", "serialize", "isolation", "isolation_base",
  "deliverables", "kind", "plan", "graph", "reasoning_effort",
] as const

export type MoveRefusal =
  | { code: "schedule_move_target_offline"; machine: string }
  | { code: "schedule_move_target_outdated"; machine: string }
  | { code: "schedule_move_source_outdated"; machine: string }
  | { code: "schedule_move_no_origin"; project: string }
  | { code: "schedule_move_source_unlisted"; project: string; machine: string }
  | { code: "schedule_move_no_project"; machine: string; repo: string }
  | { code: "schedule_move_webhook_unknown"; title: string; machine: string }
  | { code: "schedule_move_spent"; title: string }

export interface MovePlan {
  /** The source's place id for the project, which the disable and the restore send. */
  sourcePlace: string
  repo: string
  /** Every target place cloning that repository; the page preselects the first. */
  targets: MovePlace[]
  /** The hook that moves with the schedule, at the revision read before anything was written. */
  hook: { id: string; revision: number } | null
  /** A disabled hook was bound: it stays where it is and the schedule moves without it. */
  hookLeftDisabled: boolean
}

function projectName(path: string): string {
  const parts = path.replace(/\/+$/, "").split("/")
  return parts[parts.length - 1] || path
}

/**
 * Everything that can refuse a move, asked before anything is written.
 *
 * `targetPlaces` is null when the target's list could not be read: that is the
 * target being offline, whatever else it might also be.
 */
export function planScheduleMove(input: {
  record: MoveRecord
  source: { id: string; name: string }
  target: { id: string; name: string; online: boolean }
  sourcePlaces: readonly MovePlace[]
  targetPlaces: readonly MovePlace[] | null
  /** The bound hook read from Cloud; null or absent when it could not be. Read only when bound. */
  hook?: MoveHook | null
}): { refusal: MoveRefusal } | { plan: MovePlan } {
  const { record, source, target } = input
  const title = record.title || ""
  if (record.fired_at) return { refusal: { code: "schedule_move_spent", title } }
  // A bound webhook moves with the schedule. "Could not read the binding",
  // or a hook Cloud cannot say anything about, is not "unbound": the move
  // would leave a hook calling a schedule that is gone.
  const binding = record.webhook_binding_availability
  const unknown = { refusal: { code: "schedule_move_webhook_unknown", title, machine: source.name } } as const
  let hook: MovePlan["hook"] = null
  let hookLeftDisabled = false
  if (binding === "active") {
    const read = input.hook
    if (!read || !record.webhook_hook_id || read.hook_id !== record.webhook_hook_id) return unknown
    if (read.state === "disabled") hookLeftDisabled = true
    else if (read.state === "active" || read.state === "pending_binding") hook = { id: read.hook_id, revision: read.revision }
    else return unknown
  } else if (binding && binding !== "unbound") {
    return unknown
  }
  if (!target.online || input.targetPlaces === null) {
    return { refusal: { code: "schedule_move_target_offline", machine: target.name } }
  }
  const path = record.task?.project_dir || ""
  const here = input.sourcePlaces.find((place) => place.path === path)
  // The disable is a whole save, and a save names its project by the source's
  // place id: a project that fell off the source's list cannot be disabled.
  if (!here) return { refusal: { code: "schedule_move_source_unlisted", project: projectName(path), machine: source.name } }
  if (!("repo" in here)) return { refusal: { code: "schedule_move_source_outdated", machine: source.name } }
  if (!here.repo) return { refusal: { code: "schedule_move_no_origin", project: here.label || projectName(path) } }
  if (input.targetPlaces.some((place) => !("repo" in place))) {
    return { refusal: { code: "schedule_move_target_outdated", machine: target.name } }
  }
  const targets = input.targetPlaces.filter((place) => place.repo === here.repo)
  if (!targets.length) return { refusal: { code: "schedule_move_no_project", machine: target.name, repo: here.repo } }
  return { plan: { sourcePlace: here.id, repo: here.repo, targets, hook, hookLeftDisabled } }
}

/**
 * The stored record as the flat body the schedule routes read, pointed at
 * `place` and with `enabled` as given. `model` is left out on purpose: an
 * absent key leaves the stored model alone (`build`), so the disable cannot
 * change which model runs.
 */
export function recordBody(record: MoveRecord, place: string, enabled: boolean): Record<string, unknown> {
  const when = record.when || {}
  const task = record.task || {}
  const body: Record<string, unknown> = {
    title: record.title || "",
    at: when.at || "",
    place_id: place,
    assistant: task.assistant || "",
    instructions: task.instructions || "",
    enabled,
  }
  if (when.days !== undefined) body.days = when.days
  if (when.on !== undefined) body.on = when.on
  if (record.close_tab !== undefined) body.close_tab = record.close_tab
  if (record.catch_up_hours !== undefined) body.catch_up_hours = record.catch_up_hours
  if (record.notify_on_failure !== undefined) body.notify_on_failure = record.notify_on_failure
  if (task.timeout_minutes !== undefined) body.timeout_minutes = task.timeout_minutes
  return body
}

/** The source's template fields that travel, or null when it has none. */
export function carriedTemplate(record: MoveRecord): Record<string, unknown> | null {
  const task = record.task || {}
  const template: Record<string, unknown> = {}
  for (const key of CARRIED_TASK_FIELDS) if (key in task) template[key] = task[key]
  return Object.keys(template).length ? template : null
}

/** A character that continues a path name: `/a/dual` does not end inside `/a/dual-astro`. */
const PATH_CHARACTER = /[A-Za-z0-9_~\-]/

/**
 * `text` with every mention of the project directory `from`, as a whole path,
 * changed to `to`. A mention is `from` exactly, not preceded by a character
 * that would make it the tail of a longer path, and followed by the end, a `/`
 * or anything that does not continue a name — a `.` ends a sentence, and
 * continues a name only when a name character follows it (`/a/dual.git`).
 * Nothing else in the text is touched.
 */
export function retargetInstructions(text: string, from: string, to: string): string {
  const source = from.replace(/\/+$/, "")
  const target = to.replace(/\/+$/, "")
  if (!source || !target || source === target) return text
  let out = ""
  let at = 0
  for (let found = text.indexOf(source); found !== -1; found = text.indexOf(source, found + 1)) {
    if (found < at) continue
    const before = found > 0 ? text[found - 1] : ""
    const end = found + source.length
    const after = text[end] ?? ""
    const continues =
      PATH_CHARACTER.test(after) || (after === "." && PATH_CHARACTER.test(text[end + 1] ?? ""))
    if ((before && (PATH_CHARACTER.test(before) || before === "/" || before === ".")) || continues) continue
    out += text.slice(at, found) + target
    at = end
  }
  return out + text.slice(at)
}

/**
 * The copy's body: what the form holds, on the target's place. A one-time
 * schedule's date is carried, since the form has no control for it and a copy
 * without it would repeat daily. The form's permission goes as the form field
 * it is, and an empty one — nothing to take off a schedule being made — is left
 * out. The source's hidden template fields go in `template` — only when there
 * are any. Either key is sent only when it says something, so a schedule
 * without them still moves to a machine too old to read it. The project
 * directory named in the instructions becomes the target's.
 */
export function targetBody(form: Record<string, unknown>, record: MoveRecord, place: MovePlace): Record<string, unknown> {
  const body: Record<string, unknown> = { ...form, place_id: place.id }
  if (!body.permission_mode) delete body.permission_mode
  if (record.when?.on !== undefined) {
    body.on = record.when.on
    if (record.when.days === undefined) delete body.days
  }
  const template = carriedTemplate(record)
  if (template) body.template = template
  if (typeof body.instructions === "string" && record.task?.project_dir) {
    body.instructions = retargetInstructions(body.instructions, record.task.project_dir, place.path)
  }
  return body
}

/**
 * Whether a create was refused because the target daemon predates a key the
 * copy carries — `template`, or the form's `permission_mode`: it answers
 * `unknown field: …` naming it. The move says the target needs updating, and
 * never tries again without the fields.
 */
export function refusedByOlderTarget(error: unknown): boolean {
  const message = error && typeof error === "object" ? (error as { message?: unknown }).message : undefined
  return typeof message === "string" && /unknown field:[^.]*\b(template|permission_mode)\b/.test(message)
}

/** The three writes, each to the machine it names. `create` routes by the body's place. */
export interface MoveWrites {
  update(id: string, body: Record<string, unknown>, machine: string): Promise<unknown>
  create(body: Record<string, unknown>): Promise<unknown>
  remove(id: string, machine: string): Promise<unknown>
  /**
   * Cloud's move of a hook to `machine`. `revision` is the one read before the
   * move began; null reads the current one first (the rollback, after the
   * forward move changed it).
   */
  moveHook?(hookID: string, machine: string, revision: number | null): Promise<unknown>
  /** `schedule-webhook-bind-v1` on `machine`, resolved once Cloud shows the hook active. */
  bindHook?(hookID: string, scheduleID: string, machine: string): Promise<unknown>
}

/**
 * Cloud's move of a hook, as the request the copied webhook client sends: the
 * body has exactly these two keys (the Cloud's PROTOCOL.md). The copy of that
 * client is pinned byte for byte, so the route is described here and sent
 * through its `send`, which adds the session cookie and an `Idempotency-Key`.
 */
export function hookMoveRequest(hookID: string, machine: string, revision: number) {
  return {
    method: "POST",
    path: "/v1/schedule-webhooks/" + encodeURIComponent(hookID) + "/move",
    body: { machine_id: machine, expected_revision: revision },
  } as const
}

/** The id the target gave the copy: `{schedule: {id}}`, as a create answers. */
export function createdScheduleID(created: unknown): string | null {
  const schedule = created && typeof created === "object" ? (created as { schedule?: unknown }).schedule : undefined
  const id = schedule && typeof schedule === "object" ? (schedule as { id?: unknown }).id : undefined
  return typeof id === "string" && id ? id : null
}

/** What the rollback after a failed bind managed; each false is said by name. */
export interface HookRollback {
  /** The hook is back on the source machine (still paused until re-bound). */
  hookBack: boolean
  /** The source bound it again and Cloud shows it active. */
  rebound: boolean
  /** The copy on the target was deleted. */
  copyRemoved: boolean
  /** The source is enabled again as it was. */
  restored: boolean
}

export type MoveOutcome =
  | { state: "moved"; created: unknown }
  /** (a) was refused: nothing changed anywhere. */
  | { state: "not_started"; error: unknown }
  /** (b) was refused; `restored` says whether the source is back as it was. */
  | { state: "create_failed"; error: unknown; restored: boolean }
  /** (c) was refused: the copy runs on the target, the source is stored disabled. */
  | { state: "delete_failed"; error: unknown; created: unknown }
  /**
   * Cloud refused to move the hook: it is where it was, untouched. The copy is
   * deleted and the source switched back; each says whether that worked.
   */
  | { state: "hook_move_failed"; error: unknown; copyRemoved: boolean; restored: boolean }
  /** The target did not bind the hook; the rollback's every step is reported. */
  | { state: "hook_bind_failed"; error: unknown; rollback: HookRollback }

/** Whether a write went through; the caller says by name what did not. */
async function succeeded(write: () => Promise<unknown>): Promise<boolean> {
  try {
    await write()
    return true
  } catch {
    // refusal-ok: the step that failed first is the one said; this one is reported as a boolean
    return false
  }
}

export async function moveSchedule(
  writes: MoveWrites,
  input: { record: MoveRecord; source: string; target: string; plan: MovePlan; copy: Record<string, unknown> },
): Promise<MoveOutcome> {
  const { record, source, target, plan } = input
  const restore = () => writes.update(record.id, recordBody(record, plan.sourcePlace, record.enabled !== false), source)
  try {
    await writes.update(record.id, recordBody(record, plan.sourcePlace, false), source)
  } catch (error) {
    return { state: "not_started", error }
  }
  let created: unknown
  try {
    created = await writes.create(input.copy)
  } catch (error) {
    // The create's refusal is the one said; the page says the source stayed disabled.
    return { state: "create_failed", error, restored: await succeeded(restore) }
  }
  if (plan.hook) {
    const hook = plan.hook.id
    const copyID = createdScheduleID(created)
    try {
      if (!copyID) throw new Error("the target's answer named no schedule id")
      if (!writes.moveHook) throw new Error("no Cloud webhook management on this page")
      await writes.moveHook(hook, target, plan.hook.revision)
    } catch (error) {
      const copyRemoved = copyID ? await succeeded(() => writes.remove(copyID, target)) : false
      return { state: "hook_move_failed", error, copyRemoved, restored: await succeeded(restore) }
    }
    try {
      if (!writes.bindHook) throw new Error("no Cloud webhook management on this page")
      await writes.bindHook(hook, copyID, target)
    } catch (error) {
      // Back in the reverse order: the hook to the source and bound to the
      // schedule it was bound to (the source still holds that binding), then
      // the copy, then the source enabled.
      const hookBack = await succeeded(() => writes.moveHook!(hook, source, null))
      const rebound = hookBack && writes.bindHook ? await succeeded(() => writes.bindHook!(hook, record.id, source)) : false
      const copyRemoved = await succeeded(() => writes.remove(copyID, target))
      const restored = await succeeded(restore)
      return { state: "hook_bind_failed", error, rollback: { hookBack, rebound, copyRemoved, restored } }
    }
  }
  try {
    await writes.remove(record.id, source)
  } catch (error) {
    return { state: "delete_failed", error, created }
  }
  return { state: "moved", created }
}
