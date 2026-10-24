/* The Cloud's stored connection-health reads, `schema_version: 1`, read field by field.
 *
 * This is the explicit adapter between the private Cloud contract
 * (the Cloud repository's docs/connection-health-contract.md §3, §3a, §10) and the screens.
 * It keeps the API's own words: the overall outcome is always `unknown` with
 * `no_presence_contract`, per-group states are only what `cloud-derive/1`
 * derived, `null` is unknown and never zero, and coverage gaps are kept as the
 * server listed them. Only listed fields are copied, so a field the server adds
 * later never reaches a screen or an export by accident. Only the investigation
 * adapter, which itself imports nothing, is imported at run time, so node's
 * strip-types tests load it as it is. */
import type { Investigations } from "./investigations.ts"
// @ts-expect-error -- a `.ts` path, so node's strip-types tests can import this module.
import { parseInvestigations } from "./investigations.ts"

export const STORED_SCHEMA_VERSION = 1

/** The Cloud's own read bounds (contract §4). A list longer than its bound is refused, not cut. */
export const STORED_BOUNDS = {
  pageRows: 200,
  sources: 200,
  groups: 200,
  ranges: 200,
  gaps: 100,
  bootsPerSource: 8,
  coverageKeys: 16,
  ungroupedKeys: 32,
  byEventKeys: 32,
  /** Incident revisions, facts and context per revision, refusal rows per read (contract §11.5). */
  revisions: 16,
  bundleFacts: 200,
  bundleContext: 20,
  refusalRows: 50,
} as const

/** An opaque id as the Cloud admits it (`^[A-Za-z0-9_-]{1,64}$`), and the longest one the page carries. */
export const STORED_ID_MAX_CHARS = 64
const OPAQUE = /^[A-Za-z0-9_-]{1,64}$/
/** Stored ids that are not producer ids (`he_…`, `hs_…`, machine and account ids) are longer. */
const STORED_ID = /^[A-Za-z0-9_-]{1,128}$/
/** Account ids, and the service tenants `svc:relay` / `svc:api` whose `:` no account id can hold. */
const ACCOUNT = /^[A-Za-z0-9_:-]{1,128}$/

export const QUALITY = ["complete", "partial", "not_collected", "unsupported"] as const
export type StoredQuality = (typeof QUALITY)[number]
export const SEQUENCE = ["exact", "unverified", "not_collected", "not_computed"] as const
export type SequenceState = (typeof SEQUENCE)[number]
/** `service_scope`: Relay and API evidence, on the operator surface only and never attributed to an account (contract §10.4). */
/** `disabled` / `supported`: the incident reads' own coverage words (contract §11.4). */
export const COVERAGE_STATES = ["collected", "not_collected", "unsupported", "service_scope", "disabled", "supported"] as const
export type CoverageState = (typeof COVERAGE_STATES)[number]
export const GROUP_OUTCOMES = ["stall_detected", "slow", "recovered", "released_after_stall", "loss", "unsupported"] as const
export type GroupOutcome = (typeof GROUP_OUTCOMES)[number]
export const GROUP_REASONS = ["sequence_gap_within", "sequence_gap_after", "sequence_unverified"] as const
export const FRESHNESS = ["fresh", "stale"] as const
export const BOOT_ORDER = ["single_boot", "unverified"] as const
export const GAP_CAUSES = ["unexplained", "unverified"] as const
/** A service stage's own result (record/2 only, contract §10.2). `pending` means still active, never final. */
export const RECORD_OUTCOMES = ["ok", "error", "timeout", "refused", "zero_target", "threw", "pending"] as const
export type RecordOutcome = (typeof RECORD_OUTCOMES)[number]
/** The watchdog's state after a loss marker (contract §12): only on `state_after_loss`, record/1. */
export const WATCHDOG_STATES = ["slow", "blocked", "unverified_after_gap"] as const
export type WatchdogState = (typeof WATCHDOG_STATES)[number]
/** Producers the events read can be narrowed to (`source=`, contract §3). */
export const EVENT_SOURCES = ["daemon", "cloud_api", "relay", "viewer", "collector"] as const
export type EventSource = (typeof EVENT_SOURCES)[number]
/** A stored source id as the events read admits it (`source_id=`). */
export const SOURCE_ID = /^hs_[0-9a-f]{32}$/

export class StoredContractError extends Error {
  readonly code: "contract_schema" | "contract_shape" | "contract_bound"
  readonly path: string
  constructor(code: "contract_schema" | "contract_shape" | "contract_bound", path: string) {
    super(`${code} at ${path}`)
    this.code = code
    this.path = path
  }
}

type Obj = Record<string, unknown>

/* ---- the shapes ----------------------------------------------------------- */

export type SeqRange = { source_id: string; machine_id: string; boot_id: string; from: number; to: number }
export type SeqGap = SeqRange & { count: number | null; cause: (typeof GAP_CAUSES)[number] }

export type Coverage = {
  retention_floor: string | null
  truncated: boolean
  sequence: SequenceState
  sequence_reason: string | null
  /** null means not computed or past a cap: unknown, never "no ranges". */
  seq_ranges: SeqRange[] | null
  gaps: SeqGap[] | null
  gap_edges: string | null
  dropped: { transitions_lost: number | null; overflow: number | null } | null
}

export type Envelope = {
  generated_at: string
  as_of: string
  scope: { kind: "owner" } | { kind: "operator"; account: string | null }
  /** Always "unknown" in v1: there is no presence contract, so silence is never healthy. */
  outcome: "unknown"
  outcome_reason: string | null
  rule_version: string | null
  evidence_quality: StoredQuality
  partial_reason: string | null
  next_cursor: string | null
  clock_uncertainty_ms: number | null
  clock: { basis: string; order: string; producer_wall: string }
  owner_diagnostics: string
  coverage: Coverage
  source_coverage: Record<string, CoverageState>
  window: { from: string; to: string } | null
}

export type StoredEvent = {
  id: string
  account: string | null
  machine_id: string
  source_id: string
  producer_instance: string
  schema: string
  producer_seq: number
  event_id: string
  boot_id: string
  batch_id: string | null
  build_commit: string | null
  event: string
  /** Producer wall clock: display only, never subtracted across hosts. */
  wall: string
  mono_ns: number
  source: string | null
  stage: string | null
  class: string | null
  kind: string | null
  phase_id: number | null
  correlation: string | null
  count: number | null
  rule: string | null
  /** record/2 service rows only; null on a daemon row, which has no such field. */
  outcome: RecordOutcome | null
  /** The service's own elapsed time for that stage (the maximum, for an aggregate). Not end-to-end latency. */
  elapsed_ms: number | null
  /** `state_after_loss` only: what the watchdog could still say after a loss. Null on every other event. */
  state: WatchdogState | null
  received_at: string
  retain_until: string
}

export type StoredBoot = {
  boot_id: string
  server_generation: number
  build_commit: string | null
  first_received_at: string
  last_received_at: string
  last_wall_at: string | null
  last_seq: number
  events_accepted: number
  events_rejected: number
}

export type StoredSource = {
  source_id: string
  account: string | null
  machine_id: string
  producer_instance: string
  source: string | null
  boot_id: string
  boot_basis: string
  boot_order: (typeof BOOT_ORDER)[number]
  build_commit: string | null
  build_provenance: string
  last_received_at: string
  last_wall_at: string | null
  last_receipt_age_ms: number
  /** Evidence age, never machine presence: stale is not offline. */
  freshness: (typeof FRESHNESS)[number]
  expected_every_ms: number | null
  last_seq: number
  events_accepted: number
  events_rejected: number
  clock_offset_ms: number | null
  boots: StoredBoot[]
  boots_evicted: number
  retain_until: string
}

export type StoredGroup = {
  account: string | null
  machine_id: string
  source_id: string
  boot_id: string
  phase_id: number | null
  correlation: string | null
  group_outcome: GroupOutcome
  group_quality: "complete" | "partial"
  group_reason: (typeof GROUP_REASONS)[number] | null
  last_event: string
  last_stage: string | null
  class: string | null
  first_seq: number
  last_seq: number
  events: number
  dropped: number | null
  last_received_at: string
  evidence_age_ms: number
  last_wall: string
  rule_version: string
}

export type OverviewBody = Envelope & {
  events: { total: number | null; total_is_floor: boolean; by_event: Record<string, number> | null }
  sources: { listed: number; more: boolean; fresh: number | null; stale: number | null }
  incidents_coverage: string
}
export type EventsBody = Envelope & { events: StoredEvent[] }
export type SourcesBody = Envelope & { sources: StoredSource[] }
export type OutcomesBody = Envelope & { groups: StoredGroup[] | null; ungrouped: Record<string, number> | null }
export const INCIDENT_STATES = ["active", "recovering", "recovered_awaiting_review", "closed"] as const
export type IncidentState = (typeof INCIDENT_STATES)[number]
export const INCIDENT_TRIGGERS = ["stall", "slow", "loss"] as const
export const RECOVERY_REASONS = ["released_outcome_unknown", "ended_failed", "sequence_gap", "sequence_unverified", "not_recovery_evidence"] as const
export const CLOSURES = ["acknowledged_after_observed_recovery", "acknowledged_without_observed_recovery"] as const
/** `hi_` + 32 hex (contract §11.2). Anything else names no incident and is never sent. */
export const INCIDENT_ID = /^hi_[0-9a-f]{32}$/
export const CONTENT_HASH = /^sha256:[0-9a-f]{64}$/

export type IncidentGroupKey = { rule: string; kind: "operation" | "loss"; correlation: string | null; phase_id: number | null }

export type IncidentRow = {
  id: string
  account: string | null
  machine_id: string
  source: string
  source_id: string
  producer_instance: string
  boot_id: string
  group: IncidentGroupKey
  trigger: (typeof INCIDENT_TRIGGERS)[number]
  /** null only on an expired row: its evidence is gone, its state is not "nothing". */
  state: IncidentState | null
  expired: boolean
  revision: number
  content_hash: string
  opened_at: string
  updated_at: string
  acknowledged: boolean
  facts: number | null
  facts_refused: number
  bytes: number
  retain_until: string
}

export type IncidentListBody = Envelope & {
  state_rule: string
  aggregates: { minute: string; retention_days: number | null }
  incidents: IncidentRow[]
  refusals: { rows: { account: string | null; reason: string; window_start: string; count: number }[]; truncated: boolean }
}

export type BundleFact = {
  id: string; schema: string; event_id: string; producer_seq: number; boot_id: string; batch_id: string | null
  event: string; source: string | null; stage: string | null; class: string | null; kind: string | null; phase_id: number | null
  correlation: string | null; count: number | null; rule: string | null; outcome: RecordOutcome | null; elapsed_ms: number | null
  state: WatchdogState | null; build_commit: string | null; mono_ns: number; wall: string; received_at: string; digest: string
}

export type StageProof = { stage: string; event_key: string; basis: string } | null

export type Bundle = {
  manifest: {
    schema: string
    incident_id: string
    revision: number
    rule_versions: { grouping: string; state: string }
    scope: { machine_id: string; source_id: string; producer_instance: string; source: string; boot_id: string; group: IncidentGroupKey }
    window: { from: string | null; to: string | null; basis: string }
    build: { commits: string[]; provenance: string }
    sources: { source_id: string; source: string; producer_instance: string; boot_id: string; boot_order: string }[]
    sequence: { exactness: string; reason: string | null; present: { from: number; to: number }[]; gaps: { from: number; to: number; count: number | null; cause: string }[]; gap_edges: string }
    dropped: { transitions_lost: number | null; overflow: number | null; facts_refused_by_bound: number }
    retention: { raw_retain_until_earliest: string | null; bundle_retain_until: string; frozen: string }
    clock: { uncertainty_ms: number | null; basis: string; order: string; producer_wall: string }
    redaction: { class: string; owner_diagnostics: string; absent_fields: string[]; payloads_paths_names: string }
    provenance: { reason: "opened" | "new_evidence" | "acknowledged"; previous_revision: number | null; previous_content_hash: string | null; added: { id: string; producer_seq: number; received_at: string }[]; late: boolean; revised_at: string }
  }
  summary: {
    state: IncidentState
    trigger: (typeof INCIDENT_TRIGGERS)[number]
    opened_at: string
    recovery: { observed: boolean; event_key: string | null; event: string | null; received_at: string | null; unverified_reason: (typeof RECOVERY_REASONS)[number] | null }
    acknowledgement: { at: string; through_seq: number } | null
    closure: (typeof CLOSURES)[number] | null
    /** Cloud derives no cause: `root_cause` is always `not_derived`; anything else is refused as drift. */
    stages: { last_proven: StageProof; active: StageProof; next: "unknown"; root_cause: "not_derived"; hypotheses: string[] }
  }
  facts: BundleFact[]
  context: BundleFact[]
  next: { missing_evidence: string; owner_diagnostics: string; automatic_repair: string }
}

export type IncidentDetailBody = Envelope & {
  incident: IncidentRow & { revisions: number[] }
  revision: { number: number; created_at: string; content_hash: string; bytes: number }
  bundle: Bundle
  /** Agent investigations of this incident; `unsupported` when an older Cloud sent no field. */
  investigations: Investigations
}

/* ---- readers ---------------------------------------------------------------- */

const shape = (path: string): never => {
  throw new StoredContractError("contract_shape", path)
}
const obj = (v: unknown, path: string): Obj => (v !== null && typeof v === "object" && !Array.isArray(v) ? (v as Obj) : shape(path))
const str = (v: unknown, path: string): string => (typeof v === "string" && v.length > 0 && v.length <= 512 ? v : shape(path))
const strOrNull = (v: unknown, path: string): string | null => (v === null || v === undefined ? null : str(v, path))
const id = (v: unknown, path: string, pattern = STORED_ID): string => (typeof v === "string" && pattern.test(v) ? v : shape(path))
const idOrNull = (v: unknown, path: string, pattern = STORED_ID): string | null => (v === null || v === undefined ? null : id(v, path, pattern))
const instant = (v: unknown, path: string): string => (typeof v === "string" && v.length <= 40 && Number.isFinite(Date.parse(v)) ? v : shape(path))
const instantOrNull = (v: unknown, path: string): string | null => (v === null || v === undefined ? null : instant(v, path))
const int = (v: unknown, path: string): number => (typeof v === "number" && Number.isSafeInteger(v) ? v : shape(path))
const nat = (v: unknown, path: string): number => (int(v, path) >= 0 ? (v as number) : shape(path))
const natOrNull = (v: unknown, path: string): number | null => (v === null || v === undefined ? null : nat(v, path))
const intOrNull = (v: unknown, path: string): number | null => (v === null || v === undefined ? null : int(v, path))
const bool = (v: unknown, path: string): boolean => (typeof v === "boolean" ? v : shape(path))
function oneOf<T extends readonly string[]>(v: unknown, list: T, path: string): T[number] {
  return typeof v === "string" && (list as readonly string[]).includes(v) ? (v as T[number]) : shape(path)
}
function list<T>(v: unknown, path: string, bound: number, item: (x: unknown, p: string) => T): T[] {
  if (!Array.isArray(v)) return shape(path)
  if (v.length > bound) throw new StoredContractError("contract_bound", path)
  return v.map((x, i) => item(x, `${path}[${i}]`))
}
function counts(v: unknown, path: string, bound: number): Record<string, number> {
  const o = obj(v, path)
  const keys = Object.keys(o)
  if (keys.length > bound) throw new StoredContractError("contract_bound", path)
  const out: Record<string, number> = {}
  for (const k of keys) out[id(k, `${path}.key`)] = nat(o[k], `${path}.${k}`)
  return out
}

function range(v: unknown, path: string): SeqRange {
  const o = obj(v, path)
  return { source_id: id(o.source_id, `${path}.source_id`), machine_id: id(o.machine_id, `${path}.machine_id`), boot_id: id(o.boot_id, `${path}.boot_id`, OPAQUE), from: nat(o.from, `${path}.from`), to: nat(o.to, `${path}.to`) }
}

function coverage(v: unknown, path: string): Coverage {
  const o = obj(v, path)
  const dropped = o.dropped === null || o.dropped === undefined ? null : obj(o.dropped, `${path}.dropped`)
  return {
    retention_floor: instantOrNull(o.retention_floor, `${path}.retention_floor`),
    truncated: o.truncated === undefined ? false : bool(o.truncated, `${path}.truncated`),
    sequence: oneOf(o.sequence, SEQUENCE, `${path}.sequence`),
    sequence_reason: idOrNull(o.sequence_reason, `${path}.sequence_reason`),
    seq_ranges: o.seq_ranges === null || o.seq_ranges === undefined ? null : list(o.seq_ranges, `${path}.seq_ranges`, STORED_BOUNDS.ranges, range),
    gaps: o.gaps === null || o.gaps === undefined ? null : list(o.gaps, `${path}.gaps`, STORED_BOUNDS.gaps, (g, p) => {
      const go = obj(g, p)
      return { ...range(g, p), count: natOrNull(go.count, `${p}.count`), cause: oneOf(go.cause, GAP_CAUSES, `${p}.cause`) }
    }),
    gap_edges: idOrNull(o.gap_edges, `${path}.gap_edges`),
    dropped: dropped === null ? null : { transitions_lost: natOrNull(dropped.transitions_lost, `${path}.dropped.transitions_lost`), overflow: natOrNull(dropped.overflow, `${path}.dropped.overflow`) },
  }
}

/** The envelope every read carries. A version or an overall outcome v1 cannot have is refused. */
export function parseEnvelope(body: unknown): Envelope {
  const o = obj(body, "$")
  if (o.schema_version !== STORED_SCHEMA_VERSION) throw new StoredContractError("contract_schema", "$.schema_version")
  // v1 has no presence contract: an overall verdict other than unknown is not something it can say.
  if (o.outcome !== "unknown") throw new StoredContractError("contract_shape", "$.outcome")
  const scope = obj(o.scope, "$.scope")
  const kind = oneOf(scope.kind, ["owner", "operator"] as const, "$.scope.kind")
  const clock = obj(o.clock, "$.clock")
  const sc = obj(o.source_coverage ?? {}, "$.source_coverage")
  const scKeys = Object.keys(sc)
  if (scKeys.length > STORED_BOUNDS.coverageKeys) throw new StoredContractError("contract_bound", "$.source_coverage")
  const sourceCoverage: Record<string, CoverageState> = {}
  for (const k of scKeys) sourceCoverage[id(k, "$.source_coverage.key")] = oneOf(sc[k], COVERAGE_STATES, `$.source_coverage.${k}`)
  const win = o.window === undefined || o.window === null ? null : obj(o.window, "$.window")
  return {
    generated_at: instant(o.generated_at, "$.generated_at"),
    as_of: instant(o.as_of, "$.as_of"),
    scope: kind === "owner" ? { kind } : { kind, account: idOrNull(scope.account, "$.scope.account", ACCOUNT) },
    outcome: "unknown",
    outcome_reason: idOrNull(o.outcome_reason, "$.outcome_reason"),
    rule_version: strOrNull(o.rule_version, "$.rule_version"),
    evidence_quality: oneOf(o.evidence_quality, QUALITY, "$.evidence_quality"),
    partial_reason: idOrNull(o.partial_reason, "$.partial_reason"),
    next_cursor: o.next_cursor === null || o.next_cursor === undefined ? null : (typeof o.next_cursor === "string" && /^[A-Za-z0-9_=-]{1,1024}$/.test(o.next_cursor) ? o.next_cursor : shape("$.next_cursor")),
    clock_uncertainty_ms: natOrNull(o.clock_uncertainty_ms, "$.clock_uncertainty_ms"),
    clock: { basis: id(clock.basis, "$.clock.basis"), order: id(clock.order, "$.clock.order"), producer_wall: id(clock.producer_wall, "$.clock.producer_wall") },
    owner_diagnostics: id(o.owner_diagnostics, "$.owner_diagnostics"),
    coverage: coverage(o.coverage, "$.coverage"),
    source_coverage: sourceCoverage,
    window: win === null ? null : { from: instant(win.from, "$.window.from"), to: instant(win.to, "$.window.to") },
  }
}

export function parseOverview(body: unknown): OverviewBody {
  const env = parseEnvelope(body)
  const o = body as Obj
  const ev = obj(o.events, "$.events")
  const so = obj(o.sources, "$.sources")
  const inc = obj(o.incidents, "$.incidents")
  return {
    ...env,
    events: {
      total: natOrNull(ev.total, "$.events.total"),
      total_is_floor: bool(ev.total_is_floor, "$.events.total_is_floor"),
      by_event: ev.by_event === null || ev.by_event === undefined ? null : counts(ev.by_event, "$.events.by_event", STORED_BOUNDS.byEventKeys),
    },
    sources: { listed: nat(so.listed, "$.sources.listed"), more: bool(so.more, "$.sources.more"), fresh: natOrNull(so.fresh, "$.sources.fresh"), stale: natOrNull(so.stale, "$.sources.stale") },
    incidents_coverage: id(inc.coverage, "$.incidents.coverage"),
  }
}

function event(v: unknown, path: string): StoredEvent {
  const o = obj(v, path)
  return {
    id: id(o.id, `${path}.id`),
    account: idOrNull(o.account, `${path}.account`, ACCOUNT),
    machine_id: id(o.machine_id, `${path}.machine_id`),
    source_id: id(o.source_id, `${path}.source_id`),
    producer_instance: id(o.producer_instance, `${path}.producer_instance`, OPAQUE),
    schema: str(o.schema, `${path}.schema`),
    producer_seq: nat(o.producer_seq, `${path}.producer_seq`),
    event_id: id(o.event_id, `${path}.event_id`),
    boot_id: id(o.boot_id, `${path}.boot_id`, OPAQUE),
    batch_id: idOrNull(o.batch_id, `${path}.batch_id`, OPAQUE),
    build_commit: idOrNull(o.build_commit, `${path}.build_commit`, OPAQUE),
    event: id(o.event, `${path}.event`),
    wall: instant(o.wall, `${path}.wall`),
    mono_ns: nat(o.mono_ns, `${path}.mono_ns`),
    source: idOrNull(o.source, `${path}.source`),
    stage: idOrNull(o.stage, `${path}.stage`),
    class: idOrNull(o.class, `${path}.class`),
    kind: idOrNull(o.kind, `${path}.kind`),
    phase_id: intOrNull(o.phase_id, `${path}.phase_id`),
    correlation: idOrNull(o.correlation, `${path}.correlation`, OPAQUE),
    count: natOrNull(o.count, `${path}.count`),
    rule: strOrNull(o.rule, `${path}.rule`),
    outcome: o.outcome === null || o.outcome === undefined ? null : oneOf(o.outcome, RECORD_OUTCOMES, `${path}.outcome`),
    elapsed_ms: natOrNull(o.elapsed_ms, `${path}.elapsed_ms`),
    state: watchdogState(o, path),
    received_at: instant(o.received_at, `${path}.received_at`),
    retain_until: instant(o.retain_until, `${path}.retain_until`),
  }
}

/** `state` belongs to `state_after_loss` and nowhere else: a missing one there, or one anywhere
 *  else, is schema drift and refused, never read as "no state". */
function watchdogState(o: Obj, path: string): WatchdogState | null {
  if (o.event === "state_after_loss") return oneOf(o.state, WATCHDOG_STATES, `${path}.state`)
  if (o.state !== undefined && o.state !== null) return shape(`${path}.state`)
  return null
}

export function parseEvents(body: unknown): EventsBody {
  const env = parseEnvelope(body)
  return { ...env, events: list((body as Obj).events, "$.events", STORED_BOUNDS.pageRows, event) }
}

export function parseSources(body: unknown): SourcesBody {
  const env = parseEnvelope(body)
  const sources = list((body as Obj).sources, "$.sources", STORED_BOUNDS.sources, (v, p): StoredSource => {
    const o = obj(v, p)
    return {
      source_id: id(o.source_id, `${p}.source_id`),
      account: idOrNull(o.account, `${p}.account`, ACCOUNT),
      machine_id: id(o.machine_id, `${p}.machine_id`),
      producer_instance: id(o.producer_instance, `${p}.producer_instance`, OPAQUE),
      source: idOrNull(o.source, `${p}.source`),
      boot_id: id(o.boot_id, `${p}.boot_id`, OPAQUE),
      boot_basis: id(o.boot_basis, `${p}.boot_basis`),
      boot_order: oneOf(o.boot_order, BOOT_ORDER, `${p}.boot_order`),
      build_commit: idOrNull(o.build_commit, `${p}.build_commit`, OPAQUE),
      build_provenance: id(o.build_provenance, `${p}.build_provenance`),
      last_received_at: instant(o.last_received_at, `${p}.last_received_at`),
      last_wall_at: instantOrNull(o.last_wall_at, `${p}.last_wall_at`),
      last_receipt_age_ms: nat(o.last_receipt_age_ms, `${p}.last_receipt_age_ms`),
      freshness: oneOf(o.freshness, FRESHNESS, `${p}.freshness`),
      expected_every_ms: natOrNull(o.expected_every_ms, `${p}.expected_every_ms`),
      last_seq: nat(o.last_seq, `${p}.last_seq`),
      events_accepted: nat(o.events_accepted, `${p}.events_accepted`),
      events_rejected: nat(o.events_rejected, `${p}.events_rejected`),
      clock_offset_ms: intOrNull(o.clock_offset_ms, `${p}.clock_offset_ms`),
      boots: list(o.boots, `${p}.boots`, STORED_BOUNDS.bootsPerSource, (b, bp): StoredBoot => {
        const bo = obj(b, bp)
        return {
          boot_id: id(bo.boot_id, `${bp}.boot_id`, OPAQUE),
          server_generation: nat(bo.server_generation, `${bp}.server_generation`),
          build_commit: idOrNull(bo.build_commit, `${bp}.build_commit`, OPAQUE),
          first_received_at: instant(bo.first_received_at, `${bp}.first_received_at`),
          last_received_at: instant(bo.last_received_at, `${bp}.last_received_at`),
          last_wall_at: instantOrNull(bo.last_wall_at, `${bp}.last_wall_at`),
          last_seq: nat(bo.last_seq, `${bp}.last_seq`),
          events_accepted: nat(bo.events_accepted, `${bp}.events_accepted`),
          events_rejected: nat(bo.events_rejected, `${bp}.events_rejected`),
        }
      }),
      boots_evicted: nat(o.boots_evicted, `${p}.boots_evicted`),
      retain_until: instant(o.retain_until, `${p}.retain_until`),
    }
  })
  return { ...env, sources }
}

export function parseOutcomes(body: unknown): OutcomesBody {
  const env = parseEnvelope(body)
  const o = body as Obj
  const groups = o.groups === null || o.groups === undefined ? null : list(o.groups, "$.groups", STORED_BOUNDS.groups, (v, p): StoredGroup => {
    const g = obj(v, p)
    const basis = obj(g.basis, `${p}.basis`)
    return {
      account: idOrNull(g.account, `${p}.account`, ACCOUNT),
      machine_id: id(g.machine_id, `${p}.machine_id`),
      source_id: id(g.source_id, `${p}.source_id`),
      boot_id: id(g.boot_id, `${p}.boot_id`, OPAQUE),
      phase_id: intOrNull(g.phase_id, `${p}.phase_id`),
      correlation: idOrNull(g.correlation, `${p}.correlation`, OPAQUE),
      group_outcome: oneOf(g.group_outcome, GROUP_OUTCOMES, `${p}.group_outcome`),
      group_quality: oneOf(g.group_quality, ["complete", "partial"] as const, `${p}.group_quality`),
      group_reason: g.group_reason === null ? null : oneOf(g.group_reason, GROUP_REASONS, `${p}.group_reason`),
      last_event: id(g.last_event, `${p}.last_event`),
      last_stage: idOrNull(g.last_stage, `${p}.last_stage`),
      class: idOrNull(g.class, `${p}.class`),
      first_seq: nat(g.first_seq, `${p}.first_seq`),
      last_seq: nat(g.last_seq, `${p}.last_seq`),
      events: nat(g.events, `${p}.events`),
      dropped: natOrNull(g.dropped, `${p}.dropped`),
      last_received_at: instant(g.last_received_at, `${p}.last_received_at`),
      evidence_age_ms: nat(g.evidence_age_ms, `${p}.evidence_age_ms`),
      last_wall: instant(g.last_wall, `${p}.last_wall`),
      rule_version: str(basis.rule_version, `${p}.basis.rule_version`),
    }
  })
  return { ...env, groups, ungrouped: o.ungrouped === null || o.ungrouped === undefined ? null : counts(o.ungrouped, "$.ungrouped", STORED_BOUNDS.ungroupedKeys) }
}

/** The incident reads carry no sequence coverage of their own: it is not computed, never "exact". */
const UNCOMPUTED = { retention_floor: null, sequence: "not_computed", sequence_reason: null, seq_ranges: null, gaps: null, dropped: null }

function incidentEnvelope(body: unknown): Envelope {
  const o = obj(body, "$")
  return parseEnvelope(o.coverage === undefined ? { ...o, coverage: UNCOMPUTED } : o)
}

function groupKey(v: unknown, path: string): IncidentGroupKey {
  const g = obj(v, path)
  return {
    rule: id(g.rule, `${path}.rule`, /^[A-Za-z0-9_./-]{1,64}$/),
    kind: oneOf(g.kind, ["operation", "loss"] as const, `${path}.kind`),
    correlation: idOrNull(g.correlation, `${path}.correlation`, OPAQUE),
    phase_id: intOrNull(g.phase_id, `${path}.phase_id`),
  }
}

function incidentRow(v: unknown, path: string): IncidentRow {
  const o = obj(v, path)
  const expired = bool(o.expired, `${path}.expired`)
  const state = o.state === null ? null : oneOf(o.state, INCIDENT_STATES, `${path}.state`)
  // A live row always has a state; an expired one never shows one (contract §11.4).
  if (expired !== (state === null)) return shape(`${path}.state`)
  return {
    id: id(o.id, `${path}.id`, INCIDENT_ID),
    account: idOrNull(o.account, `${path}.account`, ACCOUNT),
    machine_id: id(o.machine_id, `${path}.machine_id`),
    source: id(o.source, `${path}.source`),
    source_id: id(o.source_id, `${path}.source_id`),
    producer_instance: id(o.producer_instance, `${path}.producer_instance`, OPAQUE),
    boot_id: id(o.boot_id, `${path}.boot_id`, OPAQUE),
    group: groupKey(o.group, `${path}.group`),
    trigger: oneOf(o.trigger, INCIDENT_TRIGGERS, `${path}.trigger`),
    state,
    expired,
    revision: revisionNumber(o.revision, `${path}.revision`),
    content_hash: hash(o.content_hash, `${path}.content_hash`),
    opened_at: instant(o.opened_at, `${path}.opened_at`),
    updated_at: instant(o.updated_at, `${path}.updated_at`),
    acknowledged: bool(o.acknowledged, `${path}.acknowledged`),
    facts: expired ? (o.facts === null ? null : shape(`${path}.facts`)) : nat(o.facts, `${path}.facts`),
    facts_refused: nat(o.facts_refused, `${path}.facts_refused`),
    bytes: nat(o.bytes, `${path}.bytes`),
    retain_until: instant(o.retain_until, `${path}.retain_until`),
  }
}

const hash = (v: unknown, path: string): string => (typeof v === "string" && CONTENT_HASH.test(v) ? v : shape(path))
const hashOrNull = (v: unknown, path: string): string | null => (v === null || v === undefined ? null : hash(v, path))
const revisionNumber = (v: unknown, path: string): number => {
  const n = nat(v, path)
  return n >= 1 && n <= STORED_BOUNDS.revisions ? n : shape(path)
}

export function parseIncidentList(body: unknown): IncidentListBody {
  const env = incidentEnvelope(body)
  const o = body as Obj
  const agg = obj(o.aggregates, "$.aggregates")
  const ref = obj(o.refusals, "$.refusals")
  return {
    ...env,
    state_rule: str(o.state_rule, "$.state_rule"),
    aggregates: { minute: id(agg.minute, "$.aggregates.minute"), retention_days: natOrNull(agg.retention_days, "$.aggregates.retention_days") },
    incidents: list(o.incidents, "$.incidents", STORED_BOUNDS.pageRows, incidentRow),
    refusals: {
      rows: list(ref.rows, "$.refusals.rows", STORED_BOUNDS.refusalRows, (r, p) => {
        const ro = obj(r, p)
        return { account: idOrNull(ro.account, `${p}.account`, ACCOUNT), reason: id(ro.reason, `${p}.reason`), window_start: instant(ro.window_start, `${p}.window_start`), count: nat(ro.count, `${p}.count`) }
      }),
      truncated: bool(ref.truncated, "$.refusals.truncated"),
    },
  }
}

function fact(v: unknown, path: string): BundleFact {
  const o = obj(v, path)
  return {
    id: id(o.id, `${path}.id`), schema: str(o.schema, `${path}.schema`), event_id: id(o.event_id, `${path}.event_id`),
    producer_seq: nat(o.producer_seq, `${path}.producer_seq`), boot_id: id(o.boot_id, `${path}.boot_id`, OPAQUE),
    batch_id: idOrNull(o.batch_id, `${path}.batch_id`, OPAQUE), event: id(o.event, `${path}.event`), source: idOrNull(o.source, `${path}.source`),
    stage: idOrNull(o.stage, `${path}.stage`), class: idOrNull(o.class, `${path}.class`), kind: idOrNull(o.kind, `${path}.kind`),
    phase_id: intOrNull(o.phase_id, `${path}.phase_id`), correlation: idOrNull(o.correlation, `${path}.correlation`, OPAQUE),
    count: natOrNull(o.count, `${path}.count`), rule: strOrNull(o.rule, `${path}.rule`),
    outcome: o.outcome === null || o.outcome === undefined ? null : oneOf(o.outcome, RECORD_OUTCOMES, `${path}.outcome`),
    elapsed_ms: natOrNull(o.elapsed_ms, `${path}.elapsed_ms`), state: watchdogState(o, path),
    build_commit: idOrNull(o.build_commit, `${path}.build_commit`, OPAQUE), mono_ns: nat(o.mono_ns, `${path}.mono_ns`),
    wall: instant(o.wall, `${path}.wall`), received_at: instant(o.received_at, `${path}.received_at`), digest: hash(o.digest, `${path}.digest`),
  }
}

function proof(v: unknown, path: string): StageProof {
  if (v === null) return null
  const o = obj(v, path)
  return { stage: id(o.stage, `${path}.stage`), event_key: id(o.event_key, `${path}.event_key`), basis: id(o.basis, `${path}.basis`) }
}

/** One frozen revision, read field by field. A cause, a next stage or a hypothesis Cloud does not
 *  derive is drift and refused, so the page can never show an invented root cause. */
export function parseBundle(v: unknown, path = "$.bundle"): Bundle {
  const b = obj(v, path)
  const m = obj(b.manifest, `${path}.manifest`)
  const scope = obj(m.scope, `${path}.manifest.scope`)
  const win = obj(m.window, `${path}.manifest.window`)
  const build = obj(m.build, `${path}.manifest.build`)
  const seq = obj(m.sequence, `${path}.manifest.sequence`)
  const dropped = obj(m.dropped, `${path}.manifest.dropped`)
  const ret = obj(m.retention, `${path}.manifest.retention`)
  const clock = obj(m.clock, `${path}.manifest.clock`)
  const red = obj(m.redaction, `${path}.manifest.redaction`)
  const prov = obj(m.provenance, `${path}.manifest.provenance`)
  const rules = obj(m.rule_versions, `${path}.manifest.rule_versions`)
  const s = obj(b.summary, `${path}.summary`)
  const rec = obj(s.recovery, `${path}.summary.recovery`)
  const st = obj(s.stages, `${path}.summary.stages`)
  const ack = s.acknowledgement === null || s.acknowledgement === undefined ? null : obj(s.acknowledgement, `${path}.summary.acknowledgement`)
  const nx = obj(b.next, `${path}.next`)
  const rule = /^[A-Za-z0-9_./-]{1,64}$/
  const p = `${path}.manifest`
  return {
    manifest: {
      schema: oneOf(m.schema, ["clawdline.health-incident-bundle/1"] as const, `${p}.schema`),
      incident_id: id(m.incident_id, `${p}.incident_id`, INCIDENT_ID),
      revision: revisionNumber(m.revision, `${p}.revision`),
      rule_versions: { grouping: id(rules.grouping, `${p}.rule_versions.grouping`, rule), state: id(rules.state, `${p}.rule_versions.state`, rule) },
      scope: {
        machine_id: id(scope.machine_id, `${p}.scope.machine_id`), source_id: id(scope.source_id, `${p}.scope.source_id`),
        producer_instance: id(scope.producer_instance, `${p}.scope.producer_instance`, OPAQUE), source: id(scope.source, `${p}.scope.source`),
        boot_id: id(scope.boot_id, `${p}.scope.boot_id`, OPAQUE), group: groupKey(scope.group, `${p}.scope.group`),
      },
      window: { from: instantOrNull(win.from, `${p}.window.from`), to: instantOrNull(win.to, `${p}.window.to`), basis: id(win.basis, `${p}.window.basis`) },
      build: { commits: list(build.commits, `${p}.build.commits`, STORED_BOUNDS.bundleFacts + STORED_BOUNDS.bundleContext, (c, cp) => id(c, cp, OPAQUE)), provenance: id(build.provenance, `${p}.build.provenance`) },
      sources: list(m.sources, `${p}.sources`, STORED_BOUNDS.bootsPerSource, (x, xp) => {
        const o = obj(x, xp)
        return { source_id: id(o.source_id, `${xp}.source_id`), source: id(o.source, `${xp}.source`), producer_instance: id(o.producer_instance, `${xp}.producer_instance`, OPAQUE), boot_id: id(o.boot_id, `${xp}.boot_id`, OPAQUE), boot_order: id(o.boot_order, `${xp}.boot_order`) }
      }),
      sequence: {
        exactness: oneOf(seq.exactness, ["exact", "unverified"] as const, `${p}.sequence.exactness`),
        reason: idOrNull(seq.reason, `${p}.sequence.reason`),
        present: list(seq.present, `${p}.sequence.present`, STORED_BOUNDS.ranges, (r, rp) => { const o = obj(r, rp); return { from: nat(o.from, `${rp}.from`), to: nat(o.to, `${rp}.to`) } }),
        gaps: list(seq.gaps, `${p}.sequence.gaps`, STORED_BOUNDS.gaps, (r, rp) => { const o = obj(r, rp); return { from: nat(o.from, `${rp}.from`), to: nat(o.to, `${rp}.to`), count: natOrNull(o.count, `${rp}.count`), cause: oneOf(o.cause, GAP_CAUSES, `${rp}.cause`) } }),
        gap_edges: id(seq.gap_edges, `${p}.sequence.gap_edges`),
      },
      dropped: { transitions_lost: natOrNull(dropped.transitions_lost, `${p}.dropped.transitions_lost`), overflow: natOrNull(dropped.overflow, `${p}.dropped.overflow`), facts_refused_by_bound: nat(dropped.facts_refused_by_bound, `${p}.dropped.facts_refused_by_bound`) },
      retention: { raw_retain_until_earliest: instantOrNull(ret.raw_retain_until_earliest, `${p}.retention.raw_retain_until_earliest`), bundle_retain_until: instant(ret.bundle_retain_until, `${p}.retention.bundle_retain_until`), frozen: id(ret.frozen, `${p}.retention.frozen`) },
      clock: { uncertainty_ms: natOrNull(clock.uncertainty_ms, `${p}.clock.uncertainty_ms`), basis: id(clock.basis, `${p}.clock.basis`), order: id(clock.order, `${p}.clock.order`), producer_wall: id(clock.producer_wall, `${p}.clock.producer_wall`) },
      redaction: { class: id(red.class, `${p}.redaction.class`), owner_diagnostics: id(red.owner_diagnostics, `${p}.redaction.owner_diagnostics`), absent_fields: list(red.absent_fields, `${p}.redaction.absent_fields`, STORED_BOUNDS.coverageKeys, (x, xp) => id(x, xp)), payloads_paths_names: id(red.payloads_paths_names, `${p}.redaction.payloads_paths_names`) },
      provenance: {
        reason: oneOf(prov.reason, ["opened", "new_evidence", "acknowledged"] as const, `${p}.provenance.reason`),
        previous_revision: prov.previous_revision === null ? null : revisionNumber(prov.previous_revision, `${p}.provenance.previous_revision`),
        previous_content_hash: hashOrNull(prov.previous_content_hash, `${p}.provenance.previous_content_hash`),
        added: list(prov.added, `${p}.provenance.added`, STORED_BOUNDS.bundleFacts, (x, xp) => { const o = obj(x, xp); return { id: id(o.id, `${xp}.id`), producer_seq: nat(o.producer_seq, `${xp}.producer_seq`), received_at: instant(o.received_at, `${xp}.received_at`) } }),
        late: bool(prov.late, `${p}.provenance.late`),
        revised_at: instant(prov.revised_at, `${p}.provenance.revised_at`),
      },
    },
    summary: {
      state: oneOf(s.state, INCIDENT_STATES, `${path}.summary.state`),
      trigger: oneOf(s.trigger, INCIDENT_TRIGGERS, `${path}.summary.trigger`),
      opened_at: instant(s.opened_at, `${path}.summary.opened_at`),
      recovery: {
        observed: bool(rec.observed, `${path}.summary.recovery.observed`), event_key: idOrNull(rec.event_key, `${path}.summary.recovery.event_key`),
        event: idOrNull(rec.event, `${path}.summary.recovery.event`), received_at: instantOrNull(rec.received_at, `${path}.summary.recovery.received_at`),
        unverified_reason: rec.unverified_reason === null ? null : oneOf(rec.unverified_reason, RECOVERY_REASONS, `${path}.summary.recovery.unverified_reason`),
      },
      acknowledgement: ack === null ? null : { at: instant(ack.at, `${path}.summary.acknowledgement.at`), through_seq: nat(ack.through_seq, `${path}.summary.acknowledgement.through_seq`) },
      closure: s.closure === null || s.closure === undefined ? null : oneOf(s.closure, CLOSURES, `${path}.summary.closure`),
      stages: {
        last_proven: proof(st.last_proven, `${path}.summary.stages.last_proven`),
        active: proof(st.active, `${path}.summary.stages.active`),
        next: oneOf(st.next, ["unknown"] as const, `${path}.summary.stages.next`),
        root_cause: oneOf(st.root_cause, ["not_derived"] as const, `${path}.summary.stages.root_cause`),
        hypotheses: list(st.hypotheses, `${path}.summary.stages.hypotheses`, 0, (x, xp) => str(x, xp)),
      },
    },
    facts: list(b.facts, `${path}.facts`, STORED_BOUNDS.bundleFacts, fact),
    context: list(b.context, `${path}.context`, STORED_BOUNDS.bundleContext, fact),
    next: { missing_evidence: id(nx.missing_evidence, `${path}.next.missing_evidence`), owner_diagnostics: id(nx.owner_diagnostics, `${path}.next.owner_diagnostics`), automatic_repair: oneOf(nx.automatic_repair, ["never"] as const, `${path}.next.automatic_repair`) },
  }
}

export function parseIncidentDetail(body: unknown): IncidentDetailBody {
  const env = incidentEnvelope(body)
  const o = body as Obj
  const inc = obj(o.incident, "$.incident")
  const row = incidentRow(inc, "$.incident")
  const revisions = list(inc.revisions, "$.incident.revisions", STORED_BOUNDS.revisions, (x, xp) => revisionNumber(x, xp))
  // Revisions are 1..n with n the row's latest, in order: anything else is not the immutable chain.
  if (revisions.length !== row.revision || revisions.some((n, i) => n !== i + 1)) return shape("$.incident.revisions")
  const rv = obj(o.revision, "$.revision")
  const revision = { number: revisionNumber(rv.number, "$.revision.number"), created_at: instant(rv.created_at, "$.revision.created_at"), content_hash: hash(rv.content_hash, "$.revision.content_hash"), bytes: nat(rv.bytes, "$.revision.bytes") }
  if (revision.number > row.revision) return shape("$.revision.number")
  const bundle = parseBundle(o.bundle)
  if (bundle.manifest.incident_id !== row.id) return shape("$.bundle.manifest.incident_id")
  if (bundle.manifest.revision !== revision.number) return shape("$.bundle.manifest.revision")
  return { ...env, incident: { ...row, revisions }, revision, bundle, investigations: parseInvestigations(o.investigations) }
}

/* ---- what a service stage means (contract §10.3) --------------------------- */

/** `downstream`: the service has handed work on and is waiting for it, so time here is not its own;
 *  `handoff`: a socket send that did not throw, never an endpoint receipt; `final`: where an
 *  operation ends; `own`: the service's own work. Unlisted stages (the daemon's) have no role. */
export type StageRole = "own" | "downstream" | "handoff" | "final"
const STAGE_ROLES: Record<string, StageRole> = {
  worker_admitted: "own",
  account_dispatch_started: "downstream",
  account_dispatch_returned: "own",
  outer_settled: "final",
  do_entered: "own",
  do_runtime_ready: "own",
  storage_started: "downstream",
  storage_completed: "own",
  account_pinned: "own",
  do_handler_returned: "final",
  fanout_sent: "handoff",
  api_admitted: "own",
  api_handler_started: "own",
  api_dependency_started: "downstream",
  api_dependency_completed: "own",
  api_handler_completed: "own",
  api_response_completed: "final",
}

/** Service stages in the contract's order (§10.3): worker, then Durable Object, then API. */
export const STAGE_ORDER: readonly string[] = Object.keys(STAGE_ROLES)

export function stageRole(stage: string | null): StageRole | null {
  return stage !== null && Object.hasOwn(STAGE_ROLES, stage) ? STAGE_ROLES[stage] : null
}

/** Service producers (contract §10.1): their rows are service scope, never an account's. */
export function isServiceSource(source: string | null): boolean {
  return source === "relay" || source === "cloud_api" || source === "external_probe"
}

/** The field readers, for the other adapters of this page (aggregates.ts): one strictness everywhere. */
export const storedReaders = { shape, obj, str, id, idOrNull, instant, instantOrNull, int, nat, natOrNull, bool, oneOf, list } as const
