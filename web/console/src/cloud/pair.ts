/*
 * Pairing a machine with this browser, from the machine list.
 *
 * The cryptography is the copied console's and none of it is here:
 * `pairViewer` (`legacy/js/net/cloud-boot.js`) asks the control plane for a
 * pairing id and a one-time claim nonce, builds this browser's offer
 * (`createPairingOffer`, `cloud-pairing.js`) and then claims the one sealed
 * handover the machine leaves; `pairViewerFromInvitation` does the same after
 * answering the link a machine printed. What this file owns is the *order a
 * person sees*, and the words each ending gets.
 *
 * **Why a code the person carries, and not a button that pairs.** Signing in
 * to the account is not enough to read a machine, on purpose: the account's
 * master secret is handed over only by the machine, and only to an offer that
 * reached it by a hop the cloud does not carry. The machine's link is that hop
 * in one direction (a secret the machine shows, the browser opens). This is
 * the same hop in the other direction: the browser shows its offer, and the
 * person runs it on the machine — `clawdline cloud pair -offer <code>`, which
 * reaches `POST /v1/cloud/pairing/offer`, a route only the machine's own
 * credential may call (`internal/transport/http/cloud.go`). So pairing still
 * needs somebody who can run a command on that machine. A button that paired
 * from here alone would need the machine to trust what the cloud said, which
 * is the thing the whole design refuses.
 *
 * **The fingerprints are part of the path, not decoration.** The machine
 * checks the fingerprint the control plane recorded for this browser against
 * the one inside the offer before it seals anything, and it prints both keys
 * when it is done. The person compares: this browser's key, shown while the
 * code is up, against the machine's `browser` line; the machine's key, shown
 * here once the handover is open, against its `machine` line.
 *
 * Nothing is imported at run time, so `node --test` loads it as it is.
 */

/** Where the copied console kept a machine's link across the sign-in round trip (`input/cloud-pairing.js`). */
export const INVITATION_KEY = "clawdline.pairing.invitation.v1"

/** What `startPairing` hands `onOffer` (`cloud-boot.js`). */
export interface PendingOffer {
  pairingID: string
  expiresAt: number
  /** This browser's own key, as the machine will print it on its `browser` line. */
  fingerprint: string
  /** The offer, canonical JSON in base64url: what the machine is given. */
  fragment: string
}

/** What `claimPairing` answers once the handover is open (`openPairingHandover`). */
export interface OpenedPairing {
  machineID: string
  machineFingerprint: string
}

/** One pairing from a person's point of view. */
export type PairState =
  | { phase: "idle" }
  | { phase: "asking" }
  | { phase: "waiting"; fragment: string; fingerprint: string; expiresAt: number }
  | { phase: "paired"; machineID: string; machineFingerprint: string; fingerprint: string }
  | { phase: "stopped"; expiresAt: number | null }
  | { phase: "expired" }
  | { phase: "refused"; code: string }
  | { phase: "failed"; code: string }

/** `pairViewer` or `pairViewerFromInvitation`, bound to the session, with the two hooks it takes. */
export type PairStart = (hooks: {
  onOffer: (pending: PendingOffer) => void
  sleep: (ms: number) => Promise<void>
}) => Promise<OpenedPairing>

/** The line the machine is given. The offer is base64url, so nothing in it needs quoting. */
export function pairingCommand(fragment: string): string {
  return "clawdline cloud pair -offer " + fragment
}

/**
 * How a pairing that did not finish ended.
 *
 * Three words, because they send a person to three places: the code ran out
 * (make a new one), the thing opened was not this browser's to open (a link
 * for another account, a pairing already claimed — making a new one here does
 * not fix that), or anything else, said with its code, because the machine
 * printed the fuller reason.
 */
export function pairingEnding(error: unknown): PairState {
  const code = failureCode(error)
  if (code === "offer_expired" || code === "pairing_expired" || code === "invitation_expired") return { phase: "expired" }
  if (
    code === "wrong_invitation" ||
    code === "wrong_claimant" ||
    code === "pairing_gone" ||
    code === "wrong_account" ||
    code === "wrong_sender" ||
    code === "wrong_pairing"
  ) {
    return { phase: "refused", code }
  }
  return { phase: "failed", code: code || "pairing_failed" }
}

/**
 * The copied modules' typed code, or the name of what the browser threw —
 * a WebCrypto `NotSupportedError` carries its reason in `name`, and its
 * `code` is a number that says nothing. "pairing_failed" is only for a
 * failure that named nothing at all.
 */
function failureCode(error: unknown): string {
  if (!error || typeof error !== "object") return ""
  const { code, name } = error as { code?: unknown; name?: unknown }
  if (typeof code === "string" && code) return code
  if (typeof name === "string" && name && name !== "Error") return name
  return ""
}

/** What stopping looks like to the loop inside `pairViewer`: its `sleep` rejects. */
const STOPPED = Symbol("stopped")

/**
 * One pairing, started once and watched.
 *
 * It is started by a press and not by a render. A render can happen twice for
 * one intent (React's StrictMode does it on purpose), and for a machine's link
 * the second start would spend the one answer that link accepts.
 *
 * **Stopping stops the waiting and nothing else.** The code already shown
 * stays good until it expires — there is no route that withdraws one — so the
 * state says when it stops working rather than pretending it is dead. And a
 * handover that arrives in the moment between the press and the stop is kept
 * and reported: the keys are stored by then, and saying "stopped" over a
 * browser that is paired would be the lie in the other direction.
 */
export class PairingRun {
  private current: PairState = { phase: "idle" }
  private stopped = false
  private wake: (() => void) | null = null
  private readonly start: PairStart
  private readonly onChange: (state: PairState) => void
  private readonly timers: { setTimeout: typeof setTimeout; clearTimeout: typeof clearTimeout }

  constructor(
    start: PairStart,
    onChange: (state: PairState) => void,
    timers: { setTimeout: typeof setTimeout; clearTimeout: typeof clearTimeout } = { setTimeout, clearTimeout },
  ) {
    this.start = start
    this.onChange = onChange
    this.timers = timers
  }

  get state(): PairState {
    return this.current
  }

  /** Begin, once. A second call is the same run. */
  begin(): Promise<PairState> {
    if (this.current.phase !== "idle") return Promise.resolve(this.current)
    this.set({ phase: "asking" })
    let pending: PendingOffer | null = null
    return this.start({
      onOffer: (offer) => {
        pending = offer
        if (this.stopped) return
        this.set({ phase: "waiting", fragment: offer.fragment, fingerprint: offer.fingerprint, expiresAt: offer.expiresAt })
      },
      sleep: (ms) =>
        new Promise<void>((resolve, reject) => {
          if (this.stopped) {
            reject(STOPPED)
            return
          }
          // Called without a receiver: a browser's `setTimeout` refuses to run
          // with `this` set to anything but the window ("Illegal invocation"),
          // which is what calling it as `this.timers.setTimeout` would do.
          const { setTimeout: later, clearTimeout: cancel } = this.timers
          const timer = later(() => {
            this.wake = null
            resolve()
          }, ms)
          this.wake = () => {
            cancel(timer)
            this.wake = null
            reject(STOPPED)
          }
        }),
    }).then(
      (opened) => {
        this.set({
          phase: "paired",
          machineID: opened.machineID,
          machineFingerprint: opened.machineFingerprint,
          fingerprint: (pending as PendingOffer | null)?.fingerprint ?? "",
        })
        return this.current
      },
      (error: unknown) => {
        if (error === STOPPED || this.stopped) {
          this.set({ phase: "stopped", expiresAt: (pending as PendingOffer | null)?.expiresAt ?? null })
        } else {
          this.set(pairingEnding(error))
        }
        return this.current
      },
    )
  }

  /** Stop waiting for the machine. The code on the screen is not withdrawn; see above. */
  stop(): void {
    if (this.current.phase !== "asking" && this.current.phase !== "waiting") return
    const expiresAt = this.current.phase === "waiting" ? this.current.expiresAt : null
    this.stopped = true
    this.wake?.()
    this.set({ phase: "stopped", expiresAt })
  }

  private set(state: PairState): void {
    this.current = state
    this.onChange(state)
  }
}

/** The fragment a machine's link carries, when this address is one: `#pair=<invitation>`. */
export function invitationInHash(hash: string): string | null {
  if (typeof hash !== "string" || hash.indexOf("#pair=") !== 0) return null
  const raw = hash.slice(6)
  return raw ? raw : null
}

interface InvitationScope {
  location: { hash: string; pathname: string; search: string }
  history: { state: unknown; replaceState(state: unknown, unused: string, url: string): void }
  sessionStorage: { getItem(key: string): string | null; setItem(key: string, value: string): void }
}

/**
 * The machine's link this page was opened with, or the one it was opened with
 * before signing in sent it away and back.
 *
 * The fragment carries the link's one-time secret, so it is taken out of the
 * address as soon as it is read — out of the history entry a Back step would
 * return to, and out of whatever a person copies from the address bar next —
 * and kept only in this tab's session storage, as the copied console kept it.
 */
export function takeInvitation(scope: InvitationScope): string | null {
  let raw = invitationInHash(scope.location.hash)
  if (raw) {
    try {
      scope.sessionStorage.setItem(INVITATION_KEY, raw)
    } catch {
      /* a tab that cannot keep it still answers it now */
    }
    try {
      scope.history.replaceState(scope.history.state, "", scope.location.pathname + scope.location.search)
    } catch {
      /* the address keeps it; the pairing does not depend on that */
    }
    return raw
  }
  try {
    raw = scope.sessionStorage.getItem(INVITATION_KEY)
  } catch {
    raw = null
  }
  return raw || null
}

/** Forget a link once it has been answered, refused or given up on: it is good for one answer. */
export function dropInvitation(storage: { removeItem(key: string): void }): void {
  try {
    storage.removeItem(INVITATION_KEY)
  } catch {
    /* nothing kept */
  }
}
