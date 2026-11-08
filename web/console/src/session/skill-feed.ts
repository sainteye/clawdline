/*
 * The one feed of Skill receipts every Session view shares. While any view
 * listens, it reads `/v1/squad/events` from the start once (static state),
 * then polls after its cursor the way the Squad page does. The head read at
 * start is the baseline: only later receipts play (skill-use.ts). The same
 * routes are carried to the phone by the relay reader (`squad-event-head`,
 * `squad-events`, `squad-session-snapshot` in cloud/relay-reader.ts).
 */
import { useEffect, useSyncExternalStore } from "react"
import type { Icon } from "@clawdline/contract"
import { client } from "../client.js"
import { builtinDisplay } from "../builtin-display.js"
import type { SquadReceipt } from "../pages/squad/model.js"
import type { WireNames } from "../pages/squad/wire.js"
import { SkillLedger, type SessionSkillSnapshot, type SkillMoment } from "./skill-use.js"

const POLL_MS = 3000
const MAX_BACKOFF_MS = 30000

let ledger = new SkillLedger()
let version = 0
const listeners = new Set<() => void>()
const momentListeners = new Set<(moment: SkillMoment) => void>()
let users = 0
let generation = 0
let timer: ReturnType<typeof setTimeout> | null = null

function changed(): void { version++; for (const listener of [...listeners]) listener() }

async function readJSON<T>(path: string): Promise<{ status: number; value: T | null }> {
  const response = await fetch(client.url(path), { credentials: "same-origin" })
  if (!response.ok) return { status: response.status, value: null }
  return { status: response.status, value: await response.json() as T }
}

interface WireEvent { seq: number; receipt_id: string; snapshot_id: string; conversation_id: string; definition_id: string; scope_id: string; skill_id: string; skill_version: string; status: SquadReceipt["status"]; at: number }

function receiptOf(entry: WireEvent): SquadReceipt {
  return { seq: entry.seq, receiptId: entry.receipt_id, snapshotId: entry.snapshot_id, conversationId: entry.conversation_id,
    definitionId: entry.definition_id, scopeId: entry.scope_id, skillId: entry.skill_id, skillVersion: entry.skill_version, status: entry.status, at: entry.at }
}

/** Sequences that arrived live in this page's life; a transcript card for one enters, a replayed one does not. */
const liveSeqs = new Set<number>()
export function arrivedLive(seq: number): boolean { return liveSeqs.has(seq) }

function start(): void {
  const mine = ++generation
  ledger = new SkillLedger()
  let live = true
  let delay = POLL_MS
  const later = (ms: number) => { if (mine === generation) timer = setTimeout(poll, ms) }
  async function poll(): Promise<void> {
    try {
      if (!ledger.baselined) {
        const head = await readJSON<{ seq: number }>("/v1/squad/events/head")
        if (mine !== generation) return
        // An older machine has no squad events: nothing to show, nothing to poll.
        if (!head.value) { if (head.status === 404 || head.status === 501) return; throw new Error("head") }
        ledger.baseline(head.value.seq)
      }
      const page = await readJSON<{ events: WireEvent[]; next_after: number; has_more: boolean }>(
        "/v1/squad/events?after=" + ledger.after + "&limit=50")
      if (mine !== generation) return
      if (!page.value) throw new Error("events")
      let moved = false
      for (const entry of page.value.events) {
        const moment = ledger.take(receiptOf(entry), live)
        moved = true
        if (moment) { liveSeqs.add(moment.receipt.seq); for (const listener of [...momentListeners]) { try { listener(moment) } catch (error) { console.error(error) } } }
      }
      ledger.advance(page.value.next_after)
      if (moved) changed()
      // Every page of a catch-up after a failure stays quiet, the last one too.
      if (!page.value.has_more) live = true
      delay = POLL_MS
      later(page.value.has_more ? 0 : delay)
    } catch {
      if (mine !== generation) return
      live = false
      delay = Math.min(delay * 2, MAX_BACKOFF_MS)
      later(delay)
    }
  }
  void poll()
}

function stop(): void {
  generation++
  if (timer) clearTimeout(timer)
  timer = null
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener)
  if (users++ === 0) start()
  return () => {
    listeners.delete(listener)
    if (--users === 0) stop()
  }
}

/** The shared ledger, re-rendering when a receipt arrives; `null` keeps the feed closed. */
const closed = () => () => {}
export function useSkillLedger(enabled: boolean): SkillLedger | null {
  useSyncExternalStore(enabled ? subscribe : closed, () => version)
  return enabled ? ledger : null
}

/** Calls `listener` for each live moment while mounted. */
export function useSkillMoments(enabled: boolean, listener: (moment: SkillMoment) => void): void {
  useEffect(() => {
    if (!enabled) return
    momentListeners.add(listener)
    return () => { momentListeners.delete(listener) }
  }, [enabled, listener])
}

/* ---------------- Launch snapshots, read once per conversation ---------------- */

interface WireSnapshot {
  snapshot_id: string
  snapshot: { definition_id: string; scope_id: string; skills?: { id: string; version: string; enabled: boolean; name: WireNames; purpose: WireNames; icon?: Icon }[] }
}

type SnapshotReading = { kind: "loading" } | { kind: "ready"; value: SessionSkillSnapshot } | { kind: "none" }
const snapshots = new Map<string, SnapshotReading>()
let snapshotVersion = 0
const snapshotListeners = new Set<() => void>()

function snapshotChanged(): void { snapshotVersion++; for (const listener of [...snapshotListeners]) listener() }

function readSnapshot(conversation: string): void {
  if (snapshots.has(conversation)) return
  snapshots.set(conversation, { kind: "loading" })
  void readJSON<WireSnapshot>("/v1/squad/session-snapshots/" + encodeURIComponent(conversation)).then((reply) => {
    const body = reply.value
    if (!body?.snapshot || typeof body.snapshot_id !== "string") {
      // A missing snapshot is an older Session; a failed read may be tried again on the next view.
      if (reply.status === 404 || reply.status === 501) snapshots.set(conversation, { kind: "none" })
      else snapshots.delete(conversation)
      snapshotChanged()
      return
    }
    const skills = (body.snapshot.skills ?? []).filter((skill) => skill.enabled).map((skill) => ({
      id: skill.id, version: skill.version, icon: skill.icon,
      name: builtinDisplay("skill", skill.id, skill.name ?? { en: skill.id, "zh-Hant": skill.id }, skill.purpose ?? { en: "", "zh-Hant": "" }).name,
    }))
    snapshots.set(conversation, { kind: "ready", value: { snapshotId: body.snapshot_id, conversationId: conversation,
      definitionId: body.snapshot.definition_id, scopeId: body.snapshot.scope_id, skills } })
    snapshotChanged()
  }, () => { snapshots.delete(conversation); snapshotChanged() })
}

function subscribeSnapshots(listener: () => void): () => void {
  snapshotListeners.add(listener)
  return () => { snapshotListeners.delete(listener) }
}

/** This conversation's launch snapshot, or null while unread, absent or with no conversation. */
export function useSessionSnapshot(conversation: string | null | undefined): SessionSkillSnapshot | null {
  useSyncExternalStore(subscribeSnapshots, () => snapshotVersion)
  useEffect(() => { if (conversation) readSnapshot(conversation) }, [conversation])
  const reading = conversation ? snapshots.get(conversation) : undefined
  return reading?.kind === "ready" ? reading.value : null
}

/* ---------------- The Squad motion setting, per Project scope ---------------- */

const motions = new Map<string, boolean | "loading">()
let motionVersion = 0
const motionListeners = new Set<() => void>()

function readMotion(scopeId: string): void {
  if (motions.has(scopeId)) return
  motions.set(scopeId, "loading")
  const done = (value: boolean) => { motions.set(scopeId, value); motionVersion++; for (const listener of [...motionListeners]) listener() }
  const global = () => readJSON<{ motion?: { value: boolean } }>("/v1/squad/settings")
  const scoped = scopeId && scopeId !== "global" ? readJSON<{ motion?: { value: boolean } }>("/v1/squad/settings?" + new URLSearchParams({ scope_id: scopeId })) : global()
  // A Project with no saved squad settings is not listed as a scope; it follows the global value.
  void scoped.then((reply) => reply.value ? reply : global()).then((reply) => done(reply.value?.motion?.value ?? true), () => done(true))
}

function subscribeMotion(listener: () => void): () => void {
  motionListeners.add(listener)
  return () => { motionListeners.delete(listener) }
}

function subscribeReduced(listener: () => void): () => void {
  if (typeof matchMedia !== "function") return () => {}
  const media = matchMedia("(prefers-reduced-motion: reduce)")
  media.addEventListener("change", listener)
  return () => media.removeEventListener("change", listener)
}
const reducedNow = () => typeof matchMedia === "function" && matchMedia("(prefers-reduced-motion: reduce)").matches

/**
 * Whether Skill use may move for this Project scope: the Squad motion setting
 * (on until read) and never under `prefers-reduced-motion`.
 */
export function useSkillMotion(scopeId: string | null | undefined): boolean {
  useSyncExternalStore(subscribeMotion, () => motionVersion)
  const reduced = useSyncExternalStore(subscribeReduced, reducedNow)
  useEffect(() => { if (scopeId) readMotion(scopeId) }, [scopeId])
  const value = scopeId ? motions.get(scopeId) : undefined
  return !reduced && value !== false
}
