/* The human reading of one incident revision (M05), derived only from its typed stored fields.
 *
 * It says what was observed at the time, what the frozen evidence can and cannot establish now,
 * what is missing and where to look next. It never reads another endpoint, never names a cause,
 * never says the operation is stuck now, and never subtracts a producer's clock from Cloud's: the
 * only age it gives is the read's own `as_of` minus the newest receive time, both Cloud's clock.
 * Durations are the producer's own `elapsed_ms`, quoted as measured. Raw names stay in the
 * technical record; an unknown stage or event gets a generic sentence, never a guess.
 * Nothing is imported at run time except stored.ts and words.ts, so node's strip-types tests load it. */
import type { Bundle, BundleFact } from "./stored.ts"
import type { HealthSay, HealthWord } from "./words.ts"
// @ts-expect-error -- a `.ts` path, so node's strip-types tests can import this module.
import { isServiceSource, stageRole } from "./stored.ts"
// @ts-expect-error -- a `.ts` path, so node's strip-types tests can import this module.
import { HEALTH_WORD_TABLES, healthDuration, healthMs } from "./words.ts"

export type IncidentInput = {
  summary: Bundle["summary"]
  manifest: Pick<Bundle["manifest"], "scope" | "sequence" | "dropped">
  facts: BundleFact[]
  /** The read's own time (Cloud's clock); null when unknown, and then no age is given. */
  asOf: string | null
}

/** Where a next step points: another screen of this page, or a panel further down this one. */
export type NextTarget = "trace" | "machine" | "monitoring" | "review" | "export" | "none"

export type IncidentReading = {
  headline: string
  observed: string[]
  now: string[]
  missing: string[]
  next: { text: string; to: NextTarget }[]
}

const known = (key: string): key is HealthWord => Object.hasOwn(HEALTH_WORD_TABLES.en, key)

/** A stage in plain words; one this page does not describe gets a generic phrase. */
export function stageWords(say: HealthSay, stage: string): string {
  const key = `inc_stg_${stage}`
  return say(known(key) ? key : "inc_stg_other")
}

/** An event in plain words; one this page does not describe gets a generic sentence. */
export function eventWords(say: HealthSay, event: string): string {
  const key = `inc_evt_${event}`
  return say(known(key) ? key : "inc_evt_other")
}

/** "2026-10-21 02:46:45 UTC": seconds are enough for a person; the exact instant is in the record. */
export function utcSeconds(iso: string): string {
  return `${iso.slice(0, 10)} ${iso.slice(11, 19)} UTC`
}

const SIGNAL: Record<string, "stall" | "slow" | "loss"> = { stall_detected: "stall", slow_detected: "slow", transitions_lost: "loss" }

/** Whether a record shows the operation reaching a stage where it ends. */
export function finalObserved(facts: BundleFact[]): boolean {
  return facts.some((f) => stageRole(f.stage) === "final" && f.event === "stage_completed" && f.outcome !== null && f.outcome !== "pending")
}

function bySeq(facts: BundleFact[]): BundleFact[] {
  return [...facts].sort((a, b) => a.producer_seq - b.producer_seq)
}

export function readIncident(input: IncidentInput, say: HealthSay): IncidentReading {
  const { summary: s, manifest: m } = input
  const facts = bySeq(input.facts)
  const signals = facts.filter((f) => Object.hasOwn(SIGNAL, f.event))
  const received = facts.map((f) => f.received_at).sort()
  const first = received[0] ?? null
  const last = received[received.length - 1] ?? null
  const ended = finalObserved(facts) || s.recovery.observed

  const source = say(known(`inc_srcw_${m.scope.source}`) ? (`inc_srcw_${m.scope.source}` as HealthWord) : "inc_srcw_other")
  const headline = say(`inc_head_${s.trigger}` as HealthWord, { source })

  // The stage the warning was about: the newest signal's, else what Cloud proved, else any record's.
  const latestSignal = [...signals].reverse().find((f) => f.stage !== null) ?? null
  const stage = latestSignal?.stage ?? s.stages.active?.stage ?? s.stages.last_proven?.stage ?? [...facts].reverse().find((f) => f.stage !== null)?.stage ?? null

  const observed: string[] = []
  if (first !== null && last !== null) observed.push(first === last ? say("inc_sum_whenOne", { first: utcSeconds(first) }) : say("inc_sum_when", { first: utcSeconds(first), last: utcSeconds(last) }))
  const counts = { stall: 0, slow: 0, loss: 0 }
  for (const f of signals) counts[SIGNAL[f.event]] += 1
  const list = (["stall", "slow", "loss"] as const).filter((k) => counts[k] > 0).map((k) => say(`inc_sig_${k}`, { n: counts[k] }))
  if (list.length > 0) observed.push(say("inc_sum_signals", { list: list.join(", ") }))
  if (stage !== null) {
    observed.push(say("inc_sum_stage", { stage: stageWords(say, stage) }))
    const measured = [...signals].reverse().find((f) => f.elapsed_ms !== null)
    if (measured) observed.push(say("inc_sum_waited", { time: healthMs(say, measured.elapsed_ms) }))
  } else if (s.trigger !== "loss") observed.push(say("inc_sum_noStage"))

  const now: string[] = []
  switch (s.state) {
    case "active": now.push(say("inc_now_active")); break
    case "recovering":
      now.push(say("inc_now_recovering"))
      if (s.recovery.unverified_reason) now.push(say(`inc_why_${s.recovery.unverified_reason}` as HealthWord))
      break
    case "recovered_awaiting_review": now.push(say("inc_now_recovered", { time: s.recovery.received_at ? utcSeconds(s.recovery.received_at) : say("unknown") })); break
    case "closed":
      now.push(say("inc_now_closed"))
      if (s.closure) now.push(say(`inc_closure_${s.closure}` as HealthWord))
      break
  }
  if (last !== null && input.asOf !== null) {
    const seconds = (Date.parse(input.asOf) - Date.parse(last)) / 1000
    if (Number.isFinite(seconds) && seconds >= 0) now.push(say("inc_now_age", { age: healthDuration(say, seconds), asOf: utcSeconds(input.asOf) }))
  }
  now.push(last !== null ? say("inc_now_limits", { last: utcSeconds(last) }) : say("inc_now_limitsNoTime"))

  const missing: string[] = []
  if (!ended && s.trigger !== "loss") missing.push(say("inc_miss_noEnd"))
  if (m.sequence.gaps.length > 0) missing.push(say("inc_miss_gaps", { ranges: m.sequence.gaps.map((g) => (g.from === g.to ? `#${g.from}` : `#${g.from}–#${g.to}`)).join(", ") }))
  if (m.sequence.exactness !== "exact") missing.push(say("inc_miss_unverified"))
  if (m.dropped.transitions_lost !== null && m.dropped.transitions_lost > 0) missing.push(say("inc_miss_lost", { n: m.dropped.transitions_lost }))
  if (m.dropped.overflow !== null && m.dropped.overflow > 0) missing.push(say("inc_miss_overflow", { n: m.dropped.overflow }))
  if (m.dropped.facts_refused_by_bound > 0) missing.push(say("inc_miss_refused", { n: m.dropped.facts_refused_by_bound }))
  if (missing.length === 0) missing.push(say("inc_miss_none"))

  const next: { text: string; to: NextTarget }[] = []
  if (s.state === "recovered_awaiting_review") next.push({ text: say("inc_next_review"), to: "review" })
  if (m.scope.group.correlation) next.push({ text: say("inc_next_trace"), to: "trace" })
  const role = stageRole(stage)
  if (!ended && last !== null && isServiceSource(m.scope.source) && (role === "own" || role === "downstream")) next.push({ text: say("inc_next_logs", { last: utcSeconds(last) }), to: "none" })
  const lossy = s.trigger === "loss" || m.sequence.gaps.length > 0 || m.sequence.exactness !== "exact" || (m.dropped.transitions_lost ?? 0) > 0 || (m.dropped.overflow ?? 0) > 0
  if (lossy) next.push({ text: say("inc_next_monitoring"), to: "monitoring" })
  if (next.length === 0) next.push({ text: say("inc_next_machine"), to: "machine" })
  next.push({ text: say("inc_next_export"), to: "export" })

  return { headline, observed, now, missing, next }
}
