/*
 * What a Session's role did with its Skills, from the machine's skill-event
 * receipts (docs/ai-squad-contract.md, "skill events"). One ledger holds
 * what the shared feed read for the Sessions on screen and keeps, per
 * conversation, the latest status of each Skill identity; a Session view asks
 * it for the Skills of its own launch snapshot. Pure: the feed and the screens live in skill-feed.ts and
 * SkillUse.tsx.
 */
import type { Icon } from "@clawdline/contract"
import type { SquadReceipt } from "../pages/squad/model.js"

export type SkillUseStatus = SquadReceipt["status"]

/** One Skill as this Session's launch snapshot recorded it. */
export interface SnapshotSkill {
  id: string
  version: string
  name: string
  icon?: Icon
}

/** The parts of `/v1/squad/session-snapshots/{conversation}` a Session view uses. */
export interface SessionSkillSnapshot {
  snapshotId: string
  conversationId: string
  definitionId: string
  scopeId: string
  skills: SnapshotSkill[]
}

/** What one Skill did in one Session, as far as its receipts say. */
export interface SkillUse {
  skillId: string
  status: SkillUseStatus
  /** Milliseconds of the first receipt, where the transcript places its card. */
  firstAt: number
  /** The sequence of the first receipt, which says whether the card arrived live. */
  firstSeq: number
  /** Milliseconds of the receipt that set `status`. */
  at: number
  seq: number
}

/** A receipt that arrived after the view's initial cursor, while the feed was live. */
export interface SkillMoment {
  receipt: SquadReceipt
}

const identity = (r: Pick<SquadReceipt, "snapshotId" | "definitionId" | "scopeId" | "skillId" | "skillVersion">) =>
  [r.snapshotId, r.definitionId, r.scopeId, r.skillId, r.skillVersion].join("\u0000")

/**
 * A receipt belongs to a Session view only when every part of its identity is
 * that Session's: conversation, launch snapshot, definition, Project scope and
 * a Skill id and version the snapshot carries. Anything else is another
 * Session's, or another launch's, and is ignored.
 */
export function receiptBelongs(receipt: SquadReceipt, snapshot: SessionSkillSnapshot | null | undefined): boolean {
  return !!snapshot && receipt.conversationId === snapshot.conversationId && receipt.snapshotId === snapshot.snapshotId &&
    receipt.definitionId === snapshot.definitionId && receipt.scopeId === snapshot.scopeId &&
    snapshot.skills.some((skill) => skill.id === receipt.skillId && skill.version === receipt.skillVersion)
}

/**
 * A later status replaces an earlier one, except that `applied` stays: a role
 * that applied a Skill and then reported a failed or a fresh read still used it.
 */
function merged(was: SkillUse | undefined, receipt: SquadReceipt): SkillUse {
  if (!was) return { skillId: receipt.skillId, status: receipt.status, firstAt: receipt.at, firstSeq: receipt.seq, at: receipt.at, seq: receipt.seq }
  // The still-state read of a Session shown late can bring older receipts
  // after the live poll brought newer ones: order by sequence, not arrival.
  let out = was
  if (receipt.seq < out.firstSeq) out = { ...out, firstAt: receipt.at, firstSeq: receipt.seq }
  if (out.status === "applied") return out
  if (receipt.status === "applied" || receipt.seq > out.seq) out = { ...out, status: receipt.status, at: receipt.at, seq: receipt.seq }
  return out
}

/**
 * The feed's memory. `baseline(head)` is the initial cursor: receipts at or
 * before it are recorded but never played, so a reload or a reconnect shows
 * the same state without replaying it. Afterwards `take` records each new
 * receipt once (by sequence and by receipt ID) and says whether it is a
 * moment to play: live, after the baseline, and `read` or `applied`. A
 * `failed` receipt changes the static state only.
 */
export class SkillLedger {
  private cursor = 0
  private liveAfter = Number.POSITIVE_INFINITY
  private seen = new Set<string>()
  private uses = new Map<string, Map<string, SkillUse>>()

  /** The head of the feed when this view started; nothing at or before it plays. */
  baseline(head: number): void {
    this.liveAfter = Number.isSafeInteger(head) && head >= 0 ? head : Number.POSITIVE_INFINITY
  }

  get after(): number { return this.cursor }
  get baselined(): boolean { return Number.isFinite(this.liveAfter) }

  advance(seq: number): void { if (Number.isSafeInteger(seq)) this.cursor = Math.max(this.cursor, seq) }

  take(receipt: SquadReceipt, live: boolean): SkillMoment | null {
    if (!Number.isSafeInteger(receipt.seq) || receipt.seq <= this.cursor) return null
    this.cursor = receipt.seq
    if (!this.record(receipt)) return null
    if (!live || receipt.seq <= this.liveAfter || receipt.status === "failed") return null
    return { receipt }
  }

  /**
   * Records a still-state receipt, read for one shown Session apart from the
   * live cursor: never a moment, and once by receipt ID whichever read
   * brought it first. False when it was already recorded.
   */
  record(receipt: SquadReceipt): boolean {
    if (!Number.isSafeInteger(receipt.seq) || this.seen.has(receipt.receiptId)) return false
    this.seen.add(receipt.receiptId)
    let session = this.uses.get(receipt.conversationId)
    if (!session) this.uses.set(receipt.conversationId, session = new Map())
    const key = identity(receipt)
    session.set(key, merged(session.get(key), receipt))
    return true
  }

  /** The Skills of one Session's own snapshot that its receipts name, by Skill ID. */
  usesFor(snapshot: SessionSkillSnapshot | null | undefined): Map<string, SkillUse> {
    const out = new Map<string, SkillUse>()
    if (!snapshot) return out
    const session = this.uses.get(snapshot.conversationId)
    if (!session) return out
    for (const skill of snapshot.skills) {
      const use = session.get(identity({ snapshotId: snapshot.snapshotId, definitionId: snapshot.definitionId,
        scopeId: snapshot.scopeId, skillId: skill.id, skillVersion: skill.version }))
      if (use) out.set(skill.id, use)
    }
    return out
  }
}

/** The summary: the snapshot's Skills split into used (read or applied) and not. */
export function skillSummary(snapshot: SessionSkillSnapshot, uses: ReadonlyMap<string, SkillUse>): { used: SnapshotSkill[]; unused: SnapshotSkill[] } {
  const used: SnapshotSkill[] = []
  const unused: SnapshotSkill[] = []
  for (const skill of snapshot.skills) {
    const status = uses.get(skill.id)?.status
    if (status === "read" || status === "applied") used.push(skill)
    else unused.push(skill)
  }
  return { used, unused }
}

/**
 * Plays moments one after another. A moment plays at once when nothing is
 * playing; one that arrives while another plays waits, and the one playing is
 * cut to `quick` milliseconds so a burst does not fall behind. The static state
 * never waits on this queue.
 */
export class MomentQueue<T> {
  private queue: T[] = []
  private timer: ReturnType<typeof setTimeout> | null = null
  private playing: T | null = null

  private readonly show: (moment: T | null) => void
  private readonly hold: number
  private readonly quick: number
  private readonly clock: { set: typeof setTimeout; clear: typeof clearTimeout }

  constructor(show: (moment: T | null) => void, hold = 1600, quick = 450,
    clock: { set: typeof setTimeout; clear: typeof clearTimeout } = { set: ((run: () => void, ms: number) => setTimeout(run, ms)) as typeof setTimeout, clear: ((id: ReturnType<typeof setTimeout>) => clearTimeout(id)) as typeof clearTimeout }) {
    this.show = show; this.hold = hold; this.quick = quick; this.clock = clock
  }

  push(moment: T): void {
    this.queue.push(moment)
    if (!this.playing) { this.next(); return }
    if (this.timer) this.clock.clear(this.timer)
    this.timer = this.clock.set(() => this.next(), this.quick)
  }

  private next(): void {
    this.playing = this.queue.shift() ?? null
    this.show(this.playing)
    this.timer = this.playing ? this.clock.set(() => this.next(), this.queue.length ? this.quick : this.hold) : null
  }

  stop(): void {
    if (this.timer) this.clock.clear(this.timer)
    this.timer = null; this.queue = []; this.playing = null
  }
}
