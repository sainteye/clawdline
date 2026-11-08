/*
 * The feed of Skill receipts every Session view shares, one per machine
 * (skill-feed-core.ts holds how it reads). `machine` null is this page's
 * machine; a row of the all-machines list names its own, and its reads carry
 * `?machine=` to that machine's relay (cloud/relay-reader.ts), so every row
 * of one machine shares that machine's one feed and never reads per row.
 * While any view listens a feed reads the still state of the conversations
 * on screen — the open Session and the list rows with a role — in one
 * filtered `/v1/squad/events?conversation=` read, then polls the whole feed
 * after the head the way the Squad page does. Only later receipts play
 * (skill-use.ts). The phone carries the same routes through the relay
 * reader (`squad-event-head`, `squad-events.conversations`, `squad-events`,
 * `squad-session-snapshots`, `squad-session-snapshot`).
 *
 * Launch snapshots: the list rows read theirs together, at most
 * `MAX_SQUAD_SNAPSHOT_CONVERSATIONS` in one
 * `/v1/squad/session-snapshots?conversation=` read, which carries only what a
 * row draws. The open Session reads its own full snapshot, whose Skill
 * content says when each applies (`skillWhen`).
 *
 * When the Cloud console starts reading another machine, `forgetSkillFeeds`
 * restarts every feed: a cursor or ledger kept for one machine is not
 * another's.
 */
import { useEffect, useSyncExternalStore } from "react"
import type { Icon } from "@clawdline/contract"
import { client } from "../client.js"
import { builtinDisplay } from "../builtin-display.js"
import type { WireNames } from "../pages/squad/wire.js"
import { MAX_SQUAD_SNAPSHOT_CONVERSATIONS, squadEventIDValid } from "./skill-event-bounds.js"
import { SkillFeed } from "./skill-feed-core.js"
import { skillWhen, type SessionSkillSnapshot, type SkillLedger, type SkillMoment } from "./skill-use.js"

/** The refusals that mean a machine does not have the route at all (relay-reader.ts `MACHINE_LACKS_WORD`). */
const LACKS_WORD = new Set(["unknown_command", "cloud_feature_unavailable", "cloud_machine_unsupported", "cloud_not_carried", "no_such_route"])

async function readJSON<T>(path: string): Promise<{ status: number; value: T | null }> {
  const response = await fetch(client.url(path), { credentials: "same-origin" })
  if (!response.ok) {
    let code = ""
    try { const body = await response.json() as { error?: unknown } | null; code = typeof body?.error === "string" ? body.error : (body?.error as { code?: unknown } | undefined)?.code as string ?? "" } catch { /* no body */ }
    // A machine without the route is answered as one, so its rows show nothing and are not polled.
    return { status: LACKS_WORD.has(code) ? 501 : response.status, value: null }
  }
  return { status: response.status, value: await response.json() as T }
}

/** `path` read from `machine`, or from this page's machine when null. */
function on(machine: string | null, path: string): string {
  return machine ? path + (path.includes("?") ? "&" : "?") + "machine=" + encodeURIComponent(machine) : path
}

interface WireSkill { id: string; version: string; enabled?: boolean; name: WireNames; purpose: WireNames; content?: string; icon?: Icon }
interface WireSnapshot {
  snapshot_id: string
  snapshot: { definition_id: string; scope_id: string; skills?: WireSkill[] }
}
interface WireRowSnapshot { conversation_id: string; snapshot_id: string; definition_id: string; scope_id: string; skills?: WireSkill[] }

type SnapshotReading = { kind: "loading" } | { kind: "ready"; value: SessionSkillSnapshot; full: boolean } | { kind: "none" }

function skillsOf(skills: WireSkill[], full: boolean): SessionSkillSnapshot["skills"] {
  return skills.filter((skill) => !full || skill.enabled).map((skill) => {
    const shown = builtinDisplay("skill", skill.id, skill.name ?? { en: skill.id, "zh-Hant": skill.id }, skill.purpose ?? { en: "", "zh-Hant": "" })
    return { id: skill.id, version: skill.version, icon: skill.icon, name: shown.name, when: skillWhen(skill.content) || shown.detail }
  })
}

/** One machine's receipts, launch snapshots and motion settings. */
class MachineSkills {
  readonly machine: string | null
  version = 0
  readonly listeners = new Set<() => void>()
  readonly momentListeners = new Set<(moment: SkillMoment) => void>()
  users = 0
  readonly feed: SkillFeed

  readonly snapshots = new Map<string, SnapshotReading>()
  private readonly fullAsked = new Set<string>()
  private readonly batch = new Set<string>()
  private batchTimer: ReturnType<typeof setTimeout> | null = null
  snapshotVersion = 0
  readonly snapshotListeners = new Set<() => void>()

  readonly motions = new Map<string, boolean | "loading">()
  motionVersion = 0
  readonly motionListeners = new Set<() => void>()

  constructor(machine: string | null) {
    this.machine = machine
    this.feed = new SkillFeed((path) => readJSON(on(machine, path)), () => this.changed(), (moment) => {
      for (const listener of [...this.momentListeners]) { try { listener(moment) } catch (error) { console.error(error) } }
    })
  }

  changed(): void { this.version++; for (const listener of [...this.listeners]) listener() }

  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener)
    if (this.users++ === 0) this.feed.start()
    return () => {
      this.listeners.delete(listener)
      if (--this.users === 0) this.feed.stop()
    }
  }

  /** Everything read so far is another machine's now: start again. */
  forget(): void {
    const running = this.users > 0
    this.feed.stop()
    this.snapshots.clear()
    this.fullAsked.clear()
    this.batch.clear()
    if (this.batchTimer !== null) clearTimeout(this.batchTimer)
    this.batchTimer = null
    this.motions.clear()
    if (running) this.feed.start()
    this.snapshotChanged()
    this.motionVersion++
    for (const listener of [...this.motionListeners]) listener()
    this.changed()
  }

  subscribeSnapshots = (listener: () => void): (() => void) => { this.snapshotListeners.add(listener); return () => { this.snapshotListeners.delete(listener) } }
  subscribeMotions = (listener: () => void): (() => void) => { this.motionListeners.add(listener); return () => { this.motionListeners.delete(listener) } }

  snapshotChanged(): void { this.snapshotVersion++; for (const listener of [...this.snapshotListeners]) listener() }

  /** A row's snapshot, read with the other rows on screen; `full` is the open Session's own read. */
  wantSnapshot(conversation: string, full: boolean): void {
    const held = this.snapshots.get(conversation)
    if (full) {
      if (this.fullAsked.has(conversation) || held?.kind === "none") return
      this.fullAsked.add(conversation)
      this.readFull(conversation)
      return
    }
    if (held) return
    if (!squadEventIDValid(conversation)) { this.snapshots.set(conversation, { kind: "none" }); return }
    this.snapshots.set(conversation, { kind: "loading" })
    this.batch.add(conversation)
    // The rows of one draw mount together; one read answers all of them.
    this.batchTimer ??= setTimeout(() => this.flush(), 0)
  }

  private flush(): void {
    this.batchTimer = null
    const all = [...this.batch]
    this.batch.clear()
    for (let at = 0; at < all.length; at += MAX_SQUAD_SNAPSHOT_CONVERSATIONS) this.readRows(all.slice(at, at + MAX_SQUAD_SNAPSHOT_CONVERSATIONS))
  }

  private readRows(conversations: string[]): void {
    const generation = this.snapshots
    void readJSON<{ snapshots?: WireRowSnapshot[] }>(on(this.machine, "/v1/squad/session-snapshots?conversation=" + conversations.map(encodeURIComponent).join(","))).then((reply) => {
      if (generation !== this.snapshots) return
      if (!Array.isArray(reply.value?.snapshots)) {
        // A machine without squad snapshots shows nothing; a failed read may be tried again on the next view.
        for (const conversation of conversations) {
          if (this.snapshots.get(conversation)?.kind !== "loading") continue
          if (reply.status === 501) this.snapshots.set(conversation, { kind: "none" })
          else this.snapshots.delete(conversation)
        }
        this.snapshotChanged()
        return
      }
      const found = new Map(reply.value.snapshots.filter((row) => row && typeof row.snapshot_id === "string").map((row) => [row.conversation_id, row]))
      for (const conversation of conversations) {
        const held = this.snapshots.get(conversation)
        if (held?.kind === "ready" && held.full) continue
        const row = found.get(conversation)
        this.snapshots.set(conversation, row ? { kind: "ready", full: false, value: { snapshotId: row.snapshot_id, conversationId: conversation,
          definitionId: row.definition_id, scopeId: row.scope_id, machine: this.machine, skills: skillsOf(row.skills ?? [], false) } } : { kind: "none" })
      }
      this.snapshotChanged()
    }, () => {
      for (const conversation of conversations) if (this.snapshots.get(conversation)?.kind === "loading") this.snapshots.delete(conversation)
      this.snapshotChanged()
    })
  }

  private readFull(conversation: string): void {
    const generation = this.snapshots
    if (!this.snapshots.has(conversation)) this.snapshots.set(conversation, { kind: "loading" })
    const retry = () => { this.fullAsked.delete(conversation); if (this.snapshots.get(conversation)?.kind === "loading") this.snapshots.delete(conversation) }
    void readJSON<WireSnapshot>(on(this.machine, "/v1/squad/session-snapshots/" + encodeURIComponent(conversation))).then((reply) => {
      if (generation !== this.snapshots) return
      const body = reply.value
      if (!body?.snapshot || typeof body.snapshot_id !== "string") {
        // A missing snapshot is an older Session; a failed read may be tried again on the next view.
        if (reply.status === 404 || reply.status === 501) this.snapshots.set(conversation, { kind: "none" })
        else retry()
        this.snapshotChanged()
        return
      }
      this.snapshots.set(conversation, { kind: "ready", full: true, value: { snapshotId: body.snapshot_id, conversationId: conversation,
        definitionId: body.snapshot.definition_id, scopeId: body.snapshot.scope_id, machine: this.machine, skills: skillsOf(body.snapshot.skills ?? [], true) } })
      this.snapshotChanged()
    }, () => { if (generation === this.snapshots) { retry(); this.snapshotChanged() } })
  }

  readMotion(scopeId: string): void {
    if (this.motions.has(scopeId)) return
    this.motions.set(scopeId, "loading")
    const motions = this.motions
    const done = (value: boolean) => {
      if (motions !== this.motions || this.motions.get(scopeId) !== "loading") return
      this.motions.set(scopeId, value); this.motionVersion++; for (const listener of [...this.motionListeners]) listener()
    }
    const global = () => readJSON<{ motion?: { value: boolean } }>(on(this.machine, "/v1/squad/settings"))
    const scoped = scopeId && scopeId !== "global" ? readJSON<{ motion?: { value: boolean } }>(on(this.machine, "/v1/squad/settings?" + new URLSearchParams({ scope_id: scopeId }))) : global()
    // A Project with no saved squad settings is not listed as a scope; it follows the global value.
    void scoped.then((reply) => reply.value ? reply : global()).then((reply) => done(reply.value?.motion?.value ?? true), () => done(true))
  }
}

const machines = new Map<string, MachineSkills>()
/** The machine this page reads (`forgetSkillFeeds`): a row of it shares this page's own feed. */
let readerMachine: string | null = null
let epoch = 0
const epochListeners = new Set<() => void>()

function skillsOn(machine: string | null | undefined): MachineSkills {
  const key = machine && machine !== readerMachine ? machine : ""
  let held = machines.get(key)
  if (!held) { held = new MachineSkills(key || null); machines.set(key, held) }
  return held
}

/**
 * The Cloud console now reads `machine`: every feed starts again, and a row
 * of that machine shares the page's own feed rather than opening a second.
 */
export function forgetSkillFeeds(machine: string | null = null): void {
  readerMachine = machine
  for (const held of machines.values()) held.forget()
  epoch++
  for (const listener of [...epochListeners]) listener()
}

function subscribeEpoch(listener: () => void): () => void {
  epochListeners.add(listener)
  return () => { epochListeners.delete(listener) }
}
/** Which feed a machine's views use changes only on `forgetSkillFeeds`. */
function useFeed(machine: string | null | undefined): MachineSkills {
  useSyncExternalStore(subscribeEpoch, () => epoch)
  return skillsOn(machine)
}

/** Whether a sequence arrived live in this page's life; a transcript card for one enters, a replayed one does not. */
export function arrivedLive(seq: number, machine: string | null = null): boolean { return skillsOn(machine).feed.liveSeqs.has(seq) }

/**
 * The shared ledger of `machine` (this page's when null), re-rendering when
 * a receipt arrives, with `conversation` counted as on screen while mounted;
 * `null` keeps the feed closed.
 */
const closed = () => () => {}
export function useSkillLedger(conversation: string | null | undefined, machine: string | null = null): SkillLedger | null {
  const held = useFeed(machine)
  const enabled = !!conversation
  useSyncExternalStore(enabled ? held.subscribe : closed, () => held.version)
  useEffect(() => conversation ? held.feed.show(conversation) : undefined, [conversation, held])
  return enabled ? held.feed.ledger : null
}

/** Calls `listener` for each live moment of `machine` while mounted. */
export function useSkillMoments(enabled: boolean, listener: (moment: SkillMoment) => void, machine: string | null = null): void {
  const held = useFeed(machine)
  useEffect(() => {
    if (!enabled) return
    held.momentListeners.add(listener)
    return () => { held.momentListeners.delete(listener) }
  }, [enabled, listener, held])
}

/**
 * This conversation's launch snapshot on `machine`, or null while unread,
 * absent or with no conversation. A row (`full` false) is read with the
 * other rows on screen; the open Session (`full`) reads its own, with each
 * Skill's line on when it applies.
 */
export function useSessionSnapshot(conversation: string | null | undefined, machine: string | null = null, full = true): SessionSkillSnapshot | null {
  const held = useFeed(machine)
  useSyncExternalStore(held.subscribeSnapshots, () => held.snapshotVersion)
  useEffect(() => { if (conversation) held.wantSnapshot(conversation, full) }, [conversation, held, full])
  const reading = conversation ? held.snapshots.get(conversation) : undefined
  return reading?.kind === "ready" ? reading.value : null
}

function subscribeReduced(listener: () => void): () => void {
  if (typeof matchMedia !== "function") return () => {}
  const media = matchMedia("(prefers-reduced-motion: reduce)")
  media.addEventListener("change", listener)
  return () => media.removeEventListener("change", listener)
}
const reducedNow = () => typeof matchMedia === "function" && matchMedia("(prefers-reduced-motion: reduce)").matches

/**
 * Whether Skill use may move for this Project scope on `machine`: that
 * machine's Squad motion setting (on until read) and never under
 * `prefers-reduced-motion`.
 */
export function useSkillMotion(scopeId: string | null | undefined, machine: string | null = null): boolean {
  const held = useFeed(machine)
  useSyncExternalStore(held.subscribeMotions, () => held.motionVersion)
  const reduced = useSyncExternalStore(subscribeReduced, reducedNow)
  useEffect(() => { if (scopeId) held.readMotion(scopeId) }, [scopeId, held])
  const value = scopeId ? held.motions.get(scopeId) : undefined
  return !reduced && value !== false
}
