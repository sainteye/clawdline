/*
 * The identity handover as the hosted page runs it: a machine's `#pairid=` link opens this page,
 * the person compares six digits with the machine's own screen, and only a confirm the machine
 * signed leaves keys behind (`identity-pair.ts`). This file decides where those keys live and
 * when a run must stop.
 *
 * - Kept: one IndexedDB record per account and machine, written in one put after the confirm
 *   verified: the master secret as non-extractable HKDF and AES-GCM keys, and its content-key
 *   epoch. Nothing is extracted, nothing is guessed: a machine with no record answers
 *   `{ key: null, contentKeyEpoch: null }`, and the reader says the browser must be paired again.
 * - A browser paired before this handover existed holds the master secret only as an AES-GCM
 *   key with no epoch. That key is never read here; such a browser needs this handover once.
 * - Stopped: a sign-out, an account switch or a revocation calls `stop()`. A run in flight then
 *   fails `stale` at its next await, an unanswered code comparison is answered "no", and a record
 *   written after the stop is deleted again.
 */
// @ts-expect-error -- Node's strip-types tests import this module directly.
import { IdentityPairingError, pairThisViewer, type IdentityViewerResult } from "./identity-pair.ts"
import { deleteHealthHandovers, loadHealthHandover, storeHealthHandover } from "../../legacy/js/net/cloud-crypto.js"
import type { HeldMasterKey } from "./source.js"

export const IDENTITY_FRAGMENT = "#pairid="
const PENDING_KEY = "clawdline.identity-pairing"
const RECORD_PREFIX = "clawdline.health-handover:"

export function handoverPrefix(account: string): string {
  return RECORD_PREFIX + account + ":"
}

export function handoverName(account: string, machine: string): string {
  return handoverPrefix(account) + encodeURIComponent(machine)
}

/** What one machine's handover left in this browser. */
export interface HandoverRecord {
  v: 1
  accountID: string
  machineID: string
  machineFingerprint: string
  contentKeyEpoch: number
  identityEpoch: number
  contentKeyID?: string
  machineSigningKey?: string
  pairedAtMs: number
  hkdfKey: CryptoKey
  masterKey: CryptoKey
}

function validRecord(value: unknown, account: string, machine: string): value is HandoverRecord {
  const r = value as HandoverRecord | null
  return !!r && r.v === 1 && r.accountID === account && r.machineID === machine &&
    Number.isSafeInteger(r.contentKeyEpoch) && r.contentKeyEpoch >= 1 &&
    Number.isSafeInteger(r.identityEpoch) && r.identityEpoch >= 0 &&
    typeof CryptoKey !== "undefined" && r.hkdfKey instanceof CryptoKey && r.hkdfKey.algorithm?.name === "HKDF" &&
    r.hkdfKey.extractable === false && r.masterKey instanceof CryptoKey && r.masterKey.extractable === false
}

export async function saveHandover(result: IdentityViewerResult, nowMs: number, idb?: IDBFactory): Promise<HandoverRecord> {
  const record: HandoverRecord = {
    v: 1, accountID: result.accountID, machineID: result.machineID, machineFingerprint: result.machineFingerprint,
    contentKeyEpoch: result.contentKeyEpoch, identityEpoch: result.identityEpoch, pairedAtMs: nowMs,
    contentKeyID: result.contentKeyID, machineSigningKey: result.machineSigningKey,
    hkdfKey: result.hkdfKey, masterKey: result.masterKey,
  }
  if (!validRecord(record, result.accountID, result.machineID)) throw new IdentityPairingError("bad_result", "the handover result is not storable")
  await storeHealthHandover(handoverName(result.accountID, result.machineID), record, idb)
  return record
}

/** The record for one machine, or null. A record that does not validate is unknown, not a key. */
export async function loadHandover(account: string, machine: string, idb?: IDBFactory): Promise<HandoverRecord | null> {
  const value = await loadHealthHandover(handoverName(account, machine), idb)
  return validRecord(value, account, machine) ? value : null
}

export function forgetHandovers(account: string, idb?: IDBFactory): Promise<void> {
  return deleteHealthHandovers(handoverPrefix(account), idb)
}

/** `heldMasterKey` for the owner-diagnostics source. A read failure throws; it is not "no key". */
export async function heldMasterKeyFor(account: string | null, machine: string, idb?: IDBFactory): Promise<HeldMasterKey> {
  if (!account) return { key: null, contentKeyEpoch: null }
  const record = await loadHandover(account, machine, idb)
  return record ? { key: record.hkdfKey, contentKeyEpoch: record.contentKeyEpoch } : { key: null, contentKeyEpoch: null }
}

// MARK: the link

interface FragmentScope {
  location: { hash: string; pathname: string; search: string }
  history: { state: unknown; replaceState(state: unknown, unused: string, url: string): void }
  sessionStorage: { getItem(key: string): string | null; setItem(key: string, value: string): void; removeItem(key: string): void }
}

export function identityPairingInHash(hash: string): string | null {
  if (typeof hash !== "string" || hash.indexOf(IDENTITY_FRAGMENT) !== 0) return null
  const raw = hash.slice(IDENTITY_FRAGMENT.length)
  return raw && raw.length <= 4096 ? raw : null
}

/**
 * The `#pairid=` this page was opened with, taken out of the address at once and kept only in
 * this tab's session storage until a run takes it, as `takeInvitation` keeps `#pair=`.
 */
export function takeIdentityPairing(scope: FragmentScope): string | null {
  const raw = identityPairingInHash(scope.location.hash)
  if (raw) {
    try { scope.sessionStorage.setItem(PENDING_KEY, raw) } catch { /* answered now regardless */ }
    try { scope.history.replaceState(scope.history.state, "", scope.location.pathname + scope.location.search) } catch { /* kept in the address */ }
    return raw
  }
  try { return scope.sessionStorage.getItem(PENDING_KEY) || null } catch { return null }
}

export function dropPendingIdentityPairing(scope: Pick<FragmentScope, "sessionStorage">): void {
  try { scope.sessionStorage.removeItem(PENDING_KEY) } catch { /* nothing kept */ }
}

// MARK: who may run it

/** The little of the Cloud session a handover signs with. */
export interface HandoverSession {
  account?: string | null
  deviceID?: string | null
  devicePublicKey?: Uint8Array | null
}

/**
 * Whether this browser can read the machine's Sessions over the relay, said apart from the
 * diagnostics key: `connected` is a page past the pairing screen, `pairing_required` one that is
 * signed in and registered as a device but holds no relay pairing. The handover never changes it.
 */
export type RelayAccess = "connected" | "pairing_required"

/**
 * The words a finished handover says. The diagnostics key and the relay pairing are two
 * capabilities: a browser still on the pairing screen is told the key opens diagnostics only and
 * what reading Sessions still needs, never that it is now paired.
 */
export function handoverDoneWord(relay: RelayAccess): "handoverDone" | "handoverDoneRelayNeeded" {
  return relay === "pairing_required" ? "handoverDoneRelayNeeded" : "handoverDone"
}

/**
 * The device a `#pairid=` run may sign as, or null. Signing in registers the device and its key
 * before any relay pairing exists (`cloud-boot.js` `ensureSession`), so a browser that never took
 * a `#pair=` link qualifies on the pairing screen too. The session must be the same account the
 * page is signed in to; a different or missing one runs nothing.
 */
export function handoverSigner(session: HandoverSession | null, account: string | null):
  { deviceID: string; signingPublicKey: Uint8Array } | null {
  if (!account || !session || session.account !== account) return null
  if (typeof session.deviceID !== "string" || !session.deviceID) return null
  if (!(session.devicePublicKey instanceof Uint8Array) || session.devicePublicKey.length !== 32) return null
  return { deviceID: session.deviceID, signingPublicKey: session.devicePublicKey }
}

interface WatchScope extends FragmentScope {
  addEventListener(type: "hashchange", listener: () => void): void
  removeEventListener(type: "hashchange", listener: () => void): void
}

export interface WatchContext {
  apiOrigin: string
  /** The account the page is signed in to, connected or on the pairing screen. */
  account: string
  session: () => HandoverSession | null
  fetch: HandoverContext["fetch"]
  controller?: HandoverController
  indexedDB?: IDBFactory
  pair?: HandoverContext["pair"]
}

/**
 * Takes a `#pairid=` link from the address now and on every `hashchange`, and runs it as this
 * page's signed-in device. The link stays in session storage until a signer exists, so it waits
 * through a sign-in. The returned function stops a run in flight (sign-out, another account).
 */
export function watchIdentityPairing(scope: WatchScope, ctx: WatchContext): () => void {
  const controller = ctx.controller ?? handover
  const take = () => {
    const signer = handoverSigner(ctx.session(), ctx.account)
    if (!signer) return
    const fragment = takeIdentityPairing(scope)
    if (!fragment) return
    dropPendingIdentityPairing(scope)
    void controller.run(fragment, {
      apiOrigin: ctx.apiOrigin, fetch: ctx.fetch, accountID: ctx.account, deviceID: signer.deviceID,
      signingPublicKey: signer.signingPublicKey, currentAccount: () => ctx.session()?.account ?? null,
      indexedDB: ctx.indexedDB, pair: ctx.pair,
    })
  }
  take()
  scope.addEventListener("hashchange", take)
  return () => {
    scope.removeEventListener("hashchange", take)
    controller.stop()
  }
}

// MARK: the run

export type HandoverState =
  | { phase: "idle" }
  | { phase: "comparing"; code: string; machineFingerprint: string }
  | { phase: "working" }
  | { phase: "done"; machineID: string; contentKeyEpoch: number }
  | { phase: "failed"; code: string }

export interface HandoverContext {
  apiOrigin: string
  fetch: (input: string, init: RequestInit) => Promise<Response>
  accountID: string
  deviceID: string
  signingPublicKey: Uint8Array
  /** The account the page is signed in to now; a different answer stops the run. */
  currentAccount(): string | null
  indexedDB?: IDBFactory
  now?: () => number
  sleep?: (ms: number) => Promise<void>
  /** The pairing run; tests replace it to drive the persistence seam without a machine. */
  pair?: typeof pairThisViewer
}

export class HandoverController {
  private current: HandoverState = { phase: "idle" }
  private generation = 0
  private answer: ((ok: boolean) => void) | null = null
  private readonly listeners = new Set<() => void>()

  state(): HandoverState { return this.current }

  watch(listener: () => void): () => void {
    this.listeners.add(listener)
    return () => { this.listeners.delete(listener) }
  }

  /** The person's answer to the code comparison. */
  confirm(ok: boolean): void {
    const answer = this.answer
    this.answer = null
    if (ok && answer) this.set({ phase: "working" })
    answer?.(ok)
  }

  /** Sign-out, account switch, revocation, or the person closing the dialog. */
  stop(): void {
    this.generation++
    const answer = this.answer
    this.answer = null
    answer?.(false)
    if (this.current.phase !== "idle") this.set({ phase: "idle" })
  }

  /** Run one handover from a `#pairid=` fragment. Resolves to the final state; never throws. */
  async run(fragment: string, ctx: HandoverContext): Promise<HandoverState> {
    this.stop()
    const generation = ++this.generation
    const isCurrent = () => generation === this.generation && ctx.currentAccount() === ctx.accountID
    const now = ctx.now ?? (() => Date.now())
    this.set({ phase: "working" })
    try {
      await (ctx.pair ?? pairThisViewer)(fragment, {
        apiOrigin: ctx.apiOrigin, fetch: ctx.fetch, accountID: ctx.accountID, deviceID: ctx.deviceID,
        signingPublicKey: ctx.signingPublicKey, isCurrent, now,
        sleep: ctx.sleep ?? ((ms: number) => new Promise((resolve) => setTimeout(resolve, ms))),
        confirmCode: (code: string, machineFingerprint: string) => new Promise<boolean>((resolve) => {
          if (!isCurrent()) return resolve(false)
          this.answer = resolve
          this.set({ phase: "comparing", code, machineFingerprint })
        }),
        persist: async (result: IdentityViewerResult) => {
          await saveHandover(result, now(), ctx.indexedDB)
          if (!isCurrent()) {
            // Stopped while the write ran: the keys must not outlive the stop.
            await forgetHandovers(result.accountID, ctx.indexedDB).catch(() => undefined)
            throw new IdentityPairingError("stale", "the account changed while the keys were stored")
          }
          if (generation === this.generation) this.set({ phase: "done", machineID: result.machineID, contentKeyEpoch: result.contentKeyEpoch })
        },
      })
    } catch (error) {
      const code = error instanceof IdentityPairingError ? error.code : "failed"
      if (generation === this.generation) this.set(code === "stale" ? { phase: "idle" } : { phase: "failed", code })
    }
    return this.current
  }

  private set(state: HandoverState): void {
    this.current = state
    for (const listener of [...this.listeners]) listener()
  }
}

export const handover = new HandoverController()
