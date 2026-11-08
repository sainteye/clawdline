/*
 * The one feed of Skill receipts every Session view shares (skill-feed-core.ts
 * holds how it reads). While any view listens it reads the still state of the
 * conversations on screen — the open Session and the list rows with a role —
 * in one filtered `/v1/squad/events?conversation=` read, then polls the whole
 * feed after the head the way the Squad page does. Only later receipts play
 * (skill-use.ts). The same routes are carried to the phone by the relay
 * reader (`squad-event-head`, `squad-events.conversations`, `squad-events`,
 * `squad-session-snapshot` in cloud/relay-reader.ts).
 */
import { useEffect, useSyncExternalStore } from "react"
import type { Icon } from "@clawdline/contract"
import { client } from "../client.js"
import { builtinDisplay } from "../builtin-display.js"
import type { WireNames } from "../pages/squad/wire.js"
import { SkillFeed } from "./skill-feed-core.js"
import type { SessionSkillSnapshot, SkillLedger, SkillMoment } from "./skill-use.js"

let version = 0
const listeners = new Set<() => void>()
const momentListeners = new Set<(moment: SkillMoment) => void>()
let users = 0

async function readJSON<T>(path: string): Promise<{ status: number; value: T | null }> {
  const response = await fetch(client.url(path), { credentials: "same-origin" })
  if (!response.ok) return { status: response.status, value: null }
  return { status: response.status, value: await response.json() as T }
}

function changed(): void { version++; for (const listener of [...listeners]) listener() }

const feed = new SkillFeed(readJSON, changed, (moment) => {
  for (const listener of [...momentListeners]) { try { listener(moment) } catch (error) { console.error(error) } }
})

/** Whether a sequence arrived live in this page's life; a transcript card for one enters, a replayed one does not. */
export function arrivedLive(seq: number): boolean { return feed.liveSeqs.has(seq) }

function subscribe(listener: () => void): () => void {
  listeners.add(listener)
  if (users++ === 0) feed.start()
  return () => {
    listeners.delete(listener)
    if (--users === 0) feed.stop()
  }
}

/**
 * The shared ledger, re-rendering when a receipt arrives, with `conversation`
 * counted as on screen while mounted; `null` keeps the feed closed.
 */
const closed = () => () => {}
export function useSkillLedger(conversation: string | null | undefined): SkillLedger | null {
  const enabled = !!conversation
  useSyncExternalStore(enabled ? subscribe : closed, () => version)
  useEffect(() => conversation ? feed.show(conversation) : undefined, [conversation])
  return enabled ? feed.ledger : null
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
