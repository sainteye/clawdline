import type { CloudSession, PairingInvitation, PendingPairing } from "./copied.js"
import type { OpenedPairing, PairStart } from "./pair.js"

const DATABASE = "clawdline-viewer-state"
const STORE = "pending"
// This is the copied console's existing `pairViewer` cadence, not a new retry
// policy: one claim at once, then one every two seconds while the offer lives.
const CLAIM_INTERVAL_MS = 2_000

export interface PairingScope {
  account: string
  device: string
}

export interface PendingPairingStore {
  put(scope: PairingScope, pending: PendingPairing): Promise<void>
  get(scope: PairingScope, now: number): Promise<PendingPairing | null>
  remove(scope: PairingScope, pairingID: string): Promise<void>
}

interface PairHooks {
  onOffer?: (pending: PendingPairing) => void
  sleep?: (ms: number) => Promise<void>
}

function scopeFor(session: CloudSession): PairingScope {
  if (!session.account || !session.deviceID) {
    throw failure("pairing_session_unavailable", "the signed-in browser has no pairing identity")
  }
  return { account: session.account, device: session.deviceID }
}

function failure(code: string, message: string): Error & { code: string } {
  return Object.assign(new Error(message), { code })
}

function codeOf(error: unknown): string {
  if (!error || typeof error !== "object") return ""
  const code = (error as { code?: unknown }).code
  return typeof code === "string" ? code : ""
}

function wait(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms))
}

/**
 * Start a browser-first pairing only after its one private claim key is
 * durable. Closing the card stops that card's wait, not the pairing itself;
 * `resumePendingPairing` can claim it from this or a later page.
 */
export function durablePairViewer(session: CloudSession, store: PendingPairingStore): PairStart {
  return async (hooks) => {
    const pending = await session.startPairing()
    const scope = scopeFor(session)
    try {
      await store.put(scope, pending)
    } catch (error) {
      throw failure(
        "pairing_storage_unavailable",
        error instanceof Error ? error.message : "the pending pairing could not be stored",
      )
    }
    hooks.onOffer(pending)
    return claim(session, store, scope, pending, hooks.sleep)
  }
}

/** The machine-link path has the same durable claim boundary. */
export function durablePairViewerFromInvitation(
  session: CloudSession,
  invitation: PairingInvitation,
  store: PendingPairingStore,
): PairStart {
  return async (hooks) => {
    const pending = await session.startPairing()
    const scope = scopeFor(session)
    try {
      await store.put(scope, pending)
    } catch (error) {
      throw failure(
        "pairing_storage_unavailable",
        error instanceof Error ? error.message : "the pending pairing could not be stored",
      )
    }
    hooks.onOffer(pending)
    try {
      await session.acceptPairingInvitation(invitation, pending)
    } catch (error) {
      await removeTerminal(store, scope, pending, error, session.now())
      throw error
    }
    return claim(session, store, scope, pending, hooks.sleep)
  }
}

/** Claim the offer a previous card or page left, if it is still live. */
export async function resumePendingPairing(
  session: CloudSession,
  store: PendingPairingStore,
  hooks: Pick<PairHooks, "sleep"> = {},
): Promise<OpenedPairing | null> {
  const scope = scopeFor(session)
  const pending = await store.get(scope, session.now())
  if (!pending) return null
  return claim(session, store, scope, pending, hooks.sleep)
}

async function claim(
  session: CloudSession,
  store: PendingPairingStore,
  scope: PairingScope,
  pending: PendingPairing,
  sleep: ((ms: number) => Promise<void>) | undefined,
): Promise<OpenedPairing> {
  for (;;) {
    try {
      const opened = await session.claimPairing(pending)
      await store.remove(scope, pending.pairingID)
      return opened
    } catch (error) {
      if (codeOf(error) !== "pairing_unfinished") {
        await removeTerminal(store, scope, pending, error, session.now())
        throw error
      }
      if (session.now() >= pending.expiresAt) {
        await store.remove(scope, pending.pairingID)
        throw failure("offer_expired", "that pairing offer expired unanswered")
      }
      await (sleep ?? wait)(CLAIM_INTERVAL_MS)
    }
  }
}

/**
 * A typed pairing answer is final. An untyped fetch/storage interruption is
 * not: keep the private key so another page can settle the still-live offer.
 */
async function removeTerminal(
  store: PendingPairingStore,
  scope: PairingScope,
  pending: PendingPairing,
  error: unknown,
  now: number,
): Promise<void> {
  if (codeOf(error) || now >= pending.expiresAt) {
    await store.remove(scope, pending.pairingID)
  }
}

interface PendingRecord {
  v: 1
  account: string
  device: string
  pending: PendingPairing
}

function recordKey(scope: PairingScope): string {
  return encodeURIComponent(scope.account) + ":" + encodeURIComponent(scope.device)
}

function openDatabase(factory: IDBFactory): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const request = factory.open(DATABASE, 1)
    request.onupgradeneeded = () => {
      if (!request.result.objectStoreNames.contains(STORE)) request.result.createObjectStore(STORE)
    }
    request.onsuccess = () => resolve(request.result)
    request.onerror = () => reject(request.error ?? new Error("could not open pending pairing store"))
  })
}

function pendingIn(value: unknown, scope: PairingScope): PendingPairing | null {
  if (!value || typeof value !== "object") return null
  const record = value as Partial<PendingRecord>
  if (record.v !== 1 || record.account !== scope.account || record.device !== scope.device) return null
  const pending = record.pending as Partial<PendingPairing> | undefined
  const key = pending?.ephemeralPrivateKey as Partial<CryptoKey> | undefined
  if (
    !pending ||
    typeof pending.pairingID !== "string" ||
    !pending.pairingID ||
    typeof pending.claimNonce !== "string" ||
    !pending.claimNonce ||
    typeof pending.fragment !== "string" ||
    !pending.fragment ||
    typeof pending.fingerprint !== "string" ||
    !Number.isFinite(pending.expiresAt) ||
    !pending.offer ||
    typeof pending.offer !== "object" ||
    !key ||
    key.type !== "private" ||
    key.extractable !== false
  ) {
    return null
  }
  return pending as PendingPairing
}

/** IndexedDB can retain a non-extractable CryptoKey without exposing it. */
export function browserPendingPairings(factory: IDBFactory | undefined = globalThis.indexedDB): PendingPairingStore {
  async function transact<T>(
    mode: IDBTransactionMode,
    run: (store: IDBObjectStore, done: (value: T) => void, fail: (error: unknown) => void) => void,
  ): Promise<T> {
    if (!factory) throw new Error("IndexedDB is unavailable")
    const database = await openDatabase(factory)
    return new Promise<T>((resolve, reject) => {
      const transaction = database.transaction(STORE, mode)
      let answer: T
      let answered = false
      const done = (value: T) => {
        answer = value
        answered = true
      }
      const fail = (error: unknown) => {
        try {
          transaction.abort()
        } catch {
          /* it may already have failed */
        }
        reject(error)
      }
      run(transaction.objectStore(STORE), done, fail)
      transaction.oncomplete = () => {
        database.close()
        if (answered) resolve(answer)
        else reject(new Error("pending pairing transaction produced no answer"))
      }
      transaction.onerror = () => {
        database.close()
        reject(transaction.error ?? new Error("pending pairing transaction failed"))
      }
      transaction.onabort = transaction.onerror
    })
  }

  const remove = (scope: PairingScope, pairingID: string) =>
    transact<void>("readwrite", (store, done, fail) => {
      const request = store.get(recordKey(scope))
      request.onerror = () => fail(request.error)
      request.onsuccess = () => {
        const pending = pendingIn(request.result, scope)
        if (!pending || pending.pairingID !== pairingID) {
          done(undefined)
          return
        }
        const removed = store.delete(recordKey(scope))
        removed.onsuccess = () => done(undefined)
        removed.onerror = () => fail(removed.error)
      }
    })

  return {
    put(scope, pending) {
      const record: PendingRecord = { v: 1, account: scope.account, device: scope.device, pending }
      return transact<void>("readwrite", (store, done, fail) => {
        const request = store.put(record, recordKey(scope))
        request.onsuccess = () => done(undefined)
        request.onerror = () => fail(request.error)
      })
    },
    async get(scope, now) {
      const value = await transact<unknown>("readonly", (store, done, fail) => {
        const request = store.get(recordKey(scope))
        request.onsuccess = () => done(request.result)
        request.onerror = () => fail(request.error)
      })
      const pending = pendingIn(value, scope)
      if (!pending) return null
      if (pending.expiresAt <= now) {
        await remove(scope, pending.pairingID)
        return null
      }
      return pending
    },
    remove,
  }
}
