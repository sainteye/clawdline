/* Agent investigations of one stored incident (the Cloud's private investigation contract v1).
 *
 * The operator detail may carry `investigations`: `{status: "ok", items}` newest first, or
 * `{status: "unavailable", items: null}` when Cloud could not read its storage. An older Cloud has
 * no field at all; that is "not supported", never an empty list, and nothing may be started.
 *
 * Unlike the incident's own records, one investigation row that this page cannot read never
 * refuses the whole incident: its state, reason and report conclusion keep an `unknown` value and
 * the row is still shown. Only the contract's enums and references are read; no free text exists.
 *
 * Nothing is imported at run time, so node's tests and the stored adapter load it as is. */
import type { HealthSay, HealthWord } from "./words.ts"

/** The Cloud's own bounds (contract §8): at most this many investigations per incident, refs per report. */
export const INVESTIGATION_BOUNDS = { perIncident: 5, reportRefs: 8 } as const

export const INVESTIGATION_STATES = ["investigating", "needs_you", "verified_recovered"] as const
export type InvestigationState = (typeof INVESTIGATION_STATES)[number]

/** Every reason the state rule `investigation-state/1` gives, by the state it belongs to (contract §2). */
export const INVESTIGATION_REASONS = [
  "queued_unclaimed", "leased", "accepted", "dispatch_deferred", "running",
  "undelivered", "canceled", "refused", "deadline_exceeded", "task_not_successful", "report_missing",
  "report_needs_evidence", "report_verification_failed", "recovery_not_observed", "recovery_unverified",
  "delivery_unknown", "incident_unknown",
] as const
export type InvestigationReason = (typeof INVESTIGATION_REASONS)[number]

export const REPORT_CONCLUSIONS = ["needs_evidence", "fix_prepared", "verification_failed"] as const
export type ReportConclusion = (typeof REPORT_CONCLUSIONS)[number]

export const TASK_TERMINAL_STATES = ["success", "failure", "timed_out", "cancelled", "spawn_failed"] as const
export const RECOVERY_BASES = ["evidence_ref", "fix_commit"] as const

/** A value the contract names, or `unknown` with what was actually sent, so a newer Cloud's word is seen. */
export type Known<T extends string> = { known: true; value: T } | { known: false; raw: string | null }

export type Investigation = {
  investigation_id: string
  incident_revision: number | null
  incident_content_hash: string | null
  hook_id: string | null
  machine_id: string | null
  delivery_id: string | null
  started_at: string | null
  deadline_at: string | null
  closed_at: string | null
  state: Known<InvestigationState>
  /** null only when the rule gave none (verified_recovered). */
  reason: Known<InvestigationReason> | null
  delivery_state: string | null
  task_terminal_state: Known<(typeof TASK_TERMINAL_STATES)[number]> | null
  report: { conclusion: Known<ReportConclusion>; fix_commit: string | null; evidence_refs: string[] } | null
  recovery: { event_key: string; producer_seq: number | null; received_at: string | null; basis: Known<(typeof RECOVERY_BASES)[number]> } | null
}

export type Investigations =
  | { status: "unsupported" }
  | { status: "unavailable" }
  /** `unreadable` counts rows Cloud sent that carried no readable id at all. */
  | { status: "ok"; items: Investigation[]; unreadable: number }

type Obj = Record<string, unknown>
const isObj = (v: unknown): v is Obj => v !== null && typeof v === "object" && !Array.isArray(v)
const REF = /^[A-Za-z0-9_-]{1,128}$/
const HASH = /^[0-9a-f]{64}$/
const COMMIT = /^[0-9a-f]{40}$/
const ref = (v: unknown): string | null => (typeof v === "string" && REF.test(v) ? v : null)
const instant = (v: unknown): string | null => (typeof v === "string" && v.length <= 40 && Number.isFinite(Date.parse(v)) ? v : null)
const nat = (v: unknown): number | null => (typeof v === "number" && Number.isSafeInteger(v) && v >= 0 ? v : null)
function known<T extends readonly string[]>(v: unknown, list: T): Known<T[number]> {
  if (typeof v === "string" && (list as readonly string[]).includes(v)) return { known: true, value: v as T[number] }
  return { known: false, raw: typeof v === "string" && REF.test(v) ? v : null }
}

export function parseInvestigation(v: unknown): Investigation | null {
  if (!isObj(v)) return null
  const id = ref(v.investigation_id)
  if (id === null) return null
  const report = isObj(v.report) ? v.report : null
  const recovery = isObj(v.recovery) ? v.recovery : null
  const refs = report && Array.isArray(report.evidence_refs)
    ? report.evidence_refs.slice(0, INVESTIGATION_BOUNDS.reportRefs).map(ref).filter((x): x is string => x !== null)
    : []
  return {
    investigation_id: id,
    incident_revision: nat(v.incident_revision),
    incident_content_hash: typeof v.incident_content_hash === "string" && HASH.test(v.incident_content_hash) ? v.incident_content_hash : null,
    hook_id: ref(v.hook_id),
    machine_id: ref(v.machine_id),
    delivery_id: ref(v.delivery_id),
    started_at: instant(v.started_at),
    deadline_at: instant(v.deadline_at),
    closed_at: instant(v.closed_at),
    state: known(v.state, INVESTIGATION_STATES),
    reason: v.reason === null || v.reason === undefined ? null : known(v.reason, INVESTIGATION_REASONS),
    delivery_state: ref(v.delivery_state),
    task_terminal_state: v.task_terminal_state === null || v.task_terminal_state === undefined ? null : known(v.task_terminal_state, TASK_TERMINAL_STATES),
    report: report === null ? null : {
      conclusion: known(report.conclusion, REPORT_CONCLUSIONS),
      fix_commit: typeof report.fix_commit === "string" && COMMIT.test(report.fix_commit) ? report.fix_commit : null,
      evidence_refs: refs,
    },
    recovery: recovery === null || ref(recovery.event_key) === null ? null : {
      event_key: ref(recovery.event_key)!,
      producer_seq: nat(recovery.producer_seq),
      received_at: instant(recovery.received_at),
      basis: known(recovery.basis, RECOVERY_BASES),
    },
  }
}

/** The detail's `investigations` field. Absent is an older Cloud; anything but `ok` with a list is unavailable. */
export function parseInvestigations(v: unknown): Investigations {
  if (v === undefined) return { status: "unsupported" }
  if (!isObj(v) || v.status !== "ok" || !Array.isArray(v.items)) return { status: "unavailable" }
  const rows = v.items.slice(0, INVESTIGATION_BOUNDS.perIncident)
  const items = rows.map(parseInvestigation).filter((x): x is Investigation => x !== null)
  return { status: "ok", items, unreadable: rows.length - items.length }
}

/** The investigation that still holds the incident's one live slot, if Cloud says one does. */
export function liveInvestigation(inv: Investigations): Investigation | null {
  if (inv.status !== "ok") return null
  return inv.items.find((i) => i.closed_at === null && i.state.known && i.state.value === "investigating") ?? null
}

/** Whether this page may offer a start, and why not when it may not. */
export type StartGate = "ok" | "unsupported" | "unavailable" | "live" | "limit" | "older_revision" | "expired" | "unknown_state"
export function startGate(inv: Investigations, latest: boolean, expired: boolean): StartGate {
  if (inv.status === "unsupported") return "unsupported"
  if (inv.status === "unavailable") return "unavailable"
  if (expired) return "expired"
  if (!latest) return "older_revision"
  if (liveInvestigation(inv)) return "live"
  // A row this page cannot place may be the live one; Cloud would refuse, so the page does not offer it.
  if (inv.unreadable > 0 || inv.items.some((i) => i.closed_at === null && !i.state.known)) return "unknown_state"
  if (inv.items.length + inv.unreadable >= INVESTIGATION_BOUNDS.perIncident) return "limit"
  return "ok"
}


/** The reason in words and its next step. A verified recovery has no reason; an unknown one says so. */
export function reasonWords(inv: Investigation, say: HealthSay): { why: string; next: string } {
  if (inv.reason === null) {
    if (inv.state.known && inv.state.value === "verified_recovered") return { why: say("iv_why_verified"), next: say("iv_next_verified") }
    return { why: say("iv_why_none"), next: say("iv_next_unknown") }
  }
  if (!inv.reason.known) return { why: say("iv_why_unknown", { raw: inv.reason.raw ?? "—" }), next: say("iv_next_unknown") }
  return { why: say(`iv_why_${inv.reason.value}` as HealthWord), next: say(`iv_next_${inv.reason.value}` as HealthWord) }
}
