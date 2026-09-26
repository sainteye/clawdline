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
}

/**
 * Template fields a stored schedule may carry that the form has no control
 * for. A save on the same machine keeps them (`build`, internal/app/schedules.go);
 * a create on another machine cannot send them, so a schedule carrying any is
 * not moved rather than moved without them.
 */
export const UNFORMED_TASK_FIELDS = [
  "claims", "permission_mode", "serialize", "isolation", "isolation_base",
  "deliverables", "kind", "plan", "graph", "reasoning_effort",
] as const

export type MoveRefusal =
  | { code: "schedule_move_target_offline"; machine: string }
  | { code: "schedule_move_target_outdated"; machine: string }
  | { code: "schedule_move_source_outdated"; machine: string }
  | { code: "schedule_move_no_origin"; project: string }
  | { code: "schedule_move_source_unlisted"; project: string; machine: string }
  | { code: "schedule_move_no_project"; machine: string; repo: string }
  | { code: "schedule_move_webhook_bound"; title: string; machine: string }
  | { code: "schedule_move_webhook_unknown"; title: string; machine: string }
  | { code: "schedule_move_spent"; title: string }
  | { code: "schedule_move_unformed_fields"; fields: string[] }

export interface MovePlan {
  /** The source's place id for the project, which the disable and the restore send. */
  sourcePlace: string
  repo: string
  /** Every target place cloning that repository; the page preselects the first. */
  targets: MovePlace[]
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
}): { refusal: MoveRefusal } | { plan: MovePlan } {
  const { record, source, target } = input
  const title = record.title || ""
  if (record.fired_at) return { refusal: { code: "schedule_move_spent", title } }
  const unformed = UNFORMED_TASK_FIELDS.filter((key) => record.task && key in record.task)
  if (unformed.length) return { refusal: { code: "schedule_move_unformed_fields", fields: [...unformed] } }
  // A webhook is bound to a schedule id on the source machine. The copy has a
  // new id on another machine, so the hook would keep calling a schedule that
  // is gone; and "could not read the binding" is not "unbound".
  const binding = record.webhook_binding_availability
  if (binding === "active") return { refusal: { code: "schedule_move_webhook_bound", title, machine: source.name } }
  if (binding && binding !== "unbound") {
    return { refusal: { code: "schedule_move_webhook_unknown", title, machine: source.name } }
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
  return { plan: { sourcePlace: here.id, repo: here.repo, targets } }
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

/**
 * The copy's body: what the form holds, on the target's place. A one-time
 * schedule's date is carried, since the form has no control for it and a copy
 * without it would repeat daily.
 */
export function targetBody(form: Record<string, unknown>, record: MoveRecord, place: string): Record<string, unknown> {
  const body: Record<string, unknown> = { ...form, place_id: place }
  if (record.when?.on !== undefined) {
    body.on = record.when.on
    if (record.when.days === undefined) delete body.days
  }
  return body
}

/** The three writes, each to the machine it names. `create` routes by the body's place. */
export interface MoveWrites {
  update(id: string, body: Record<string, unknown>, machine: string): Promise<unknown>
  create(body: Record<string, unknown>): Promise<unknown>
  remove(id: string, machine: string): Promise<unknown>
}

export type MoveOutcome =
  | { state: "moved"; created: unknown }
  /** (a) was refused: nothing changed anywhere. */
  | { state: "not_started"; error: unknown }
  /** (b) was refused; `restored` says whether the source is back as it was. */
  | { state: "create_failed"; error: unknown; restored: boolean }
  /** (c) was refused: the copy runs on the target, the source is stored disabled. */
  | { state: "delete_failed"; error: unknown; created: unknown }

export async function moveSchedule(
  writes: MoveWrites,
  input: { record: MoveRecord; source: string; plan: MovePlan; copy: Record<string, unknown> },
): Promise<MoveOutcome> {
  const { record, source, plan } = input
  try {
    await writes.update(record.id, recordBody(record, plan.sourcePlace, false), source)
  } catch (error) {
    return { state: "not_started", error }
  }
  let created: unknown
  try {
    created = await writes.create(input.copy)
  } catch (error) {
    let restored = true
    try {
      await writes.update(record.id, recordBody(record, plan.sourcePlace, record.enabled !== false), source)
    } catch {
      // refusal-ok: the create's refusal is the one said; the page says the source stayed disabled
      restored = false
    }
    return { state: "create_failed", error, restored }
  }
  try {
    await writes.remove(record.id, source)
  } catch (error) {
    return { state: "delete_failed", error, created }
  }
  return { state: "moved", created }
}
