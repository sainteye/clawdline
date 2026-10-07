/*
 * Freeing a viewer-device slot from the screen that says there is none.
 *
 * `POST /v1/auth/session` answers `409 device_limit_reached` when the account
 * already has as many viewer devices as its plan allows. Before this, the card
 * said so and stopped: no list, no button, and no other page a signed-out
 * browser could reach to remove an old device (docs/records/first-run-audit.md B12).
 *
 * The control plane already has the way out (PROTOCOL.md, "Viewer-capacity
 * recovery"): while the account is at its ceiling, the same short-lived OAuth
 * login ticket that was refused a session may list the active viewer devices
 * and revoke one. Once a slot is free the ticket may ask for the session again.
 * The copied session has both calls (`cloud-boot.js` `recoveryDevices`,
 * `revokeRecoveryDevice`); this is what drives them, one state at a time, so
 * the card only draws.
 *
 * Asking again after a revoke is not done here. It is the gate's own start —
 * the same `keepConnected` that met the limit — so the browser carries on
 * signing in exactly as an ordinary first login does, and meets the limit
 * again honestly if another device took the slot in between.
 *
 * Nothing is imported at run time, so `node --test` loads it as it is.
 */

/** One active viewer device, as the recovery list gives it: no key, no account. */
export interface RecoveryDevice {
  id: string
  name: string
  kind: string
  created_at: string | null
  last_seen_at: string | null
}

/** The two recovery calls of the copied session (`cloud-boot.js`). */
export interface RecoverySession {
  recoveryDevices(): Promise<{ tier: string; limit: number | null; active: number; devices: RecoveryDevice[] }>
  revokeRecoveryDevice(id: string): Promise<unknown>
}

/**
 * The word `cloud-boot.js` puts in `tier` when the control plane named none.
 * It is that file's English stand-in, not a plan, so it is never shown.
 */
export const COPIED_TIER_FALLBACK = "current plan"

/** The plan's own name, or null when the answer did not carry one. */
export function knownTier(tier: unknown): string | null {
  if (typeof tier !== "string") return null
  const trimmed = tier.trim()
  return trimmed && trimmed !== COPIED_TIER_FALLBACK ? trimmed : null
}

/** What one failed call came to, kept apart so the card says which. */
export interface DeviceLimitFailure {
  step: "list" | "revoke"
  /** The control plane's own code, or one that says where it came from. */
  code: string
  /**
   * The login ticket is gone: only signing in again helps. A retry here would
   * be refused the same way.
   */
  expired: boolean
}

export type DeviceLimitState =
  | { phase: "loading" }
  | {
      phase: "listed"
      tier: string | null
      limit: number | null
      devices: RecoveryDevice[]
      /** The device whose removal is being asked about, before anything is sent. */
      asking: string | null
      /** The device whose removal is in flight. */
      revoking: string | null
      /** The last revoke that failed, shown above the list it left in place. */
      failure: DeviceLimitFailure | null
    }
  | { phase: "list_failed"; failure: DeviceLimitFailure }
  | { phase: "continuing"; removed: RecoveryDevice }

/** Ticket codes the control plane answers when the fresh login is gone (`routes/auth.ts`). */
const EXPIRED_CODES = new Set(["no_login_ticket", "login_ticket_expired", "no_session"])

/** A failure the copied session threw, read without trusting its shape. */
export function readFailure(step: DeviceLimitFailure["step"], error: unknown): DeviceLimitFailure {
  const said = error && typeof error === "object" ? (error as { code?: unknown; status?: unknown }) : {}
  const status = typeof said.status === "number" ? said.status : null
  const code =
    typeof said.code === "string" && said.code
      ? said.code
      : status !== null
        ? "http_" + status
        : error instanceof TypeError
          ? "network_unreachable"
          : "device_recovery_failed"
  return { step, code, expired: status === 401 || status === 403 || EXPIRED_CODES.has(code) }
}

export interface DeviceLimitOptions {
  onState: (state: DeviceLimitState) => void
  continueSignIn: () => void
  /** What the refused session already said, until the list says it again. */
  tier?: string | null
  limit?: number | null
}

/**
 * One visit to the device-limit card: read the list, ask, revoke, carry on.
 *
 * `continueSignIn` is called once, after a revoke the control plane confirmed,
 * and is the caller's way of asking for the session again.
 */
export class DeviceLimitRun {
  private state: DeviceLimitState = { phase: "loading" }
  private stopped = false

  private readonly session: RecoverySession
  private readonly options: DeviceLimitOptions

  constructor(session: RecoverySession, options: DeviceLimitOptions) {
    this.session = session
    this.options = options
  }

  get current(): DeviceLimitState {
    return this.state
  }

  private set(next: DeviceLimitState): void {
    if (this.stopped) return
    this.state = next
    this.options.onState(next)
  }

  /** Nothing more is drawn: the card was left. Calls in flight finish unseen. */
  stop(): void {
    this.stopped = true
  }

  async load(): Promise<void> {
    this.set({ phase: "loading" })
    try {
      const listed = await this.session.recoveryDevices()
      this.set({
        phase: "listed",
        tier: knownTier(listed.tier) ?? knownTier(this.options.tier),
        limit: listed.limit ?? this.options.limit ?? null,
        devices: listed.devices,
        asking: null,
        revoking: null,
        failure: null,
      })
    } catch (error) {
      this.set({ phase: "list_failed", failure: readFailure("list", error) })
    }
  }

  /** Open, or with null close, the in-card question for one device. */
  ask(id: string | null): void {
    const held = this.state
    if (held.phase !== "listed" || held.revoking) return
    if (id !== null && !held.devices.some((device) => device.id === id)) return
    this.set({ ...held, asking: id, failure: id === null ? held.failure : null })
  }

  /** Remove the device being asked about; on success, ask for the session again. */
  async revoke(id: string): Promise<void> {
    const held = this.state
    if (held.phase !== "listed" || held.revoking || held.asking !== id) return
    const device = held.devices.find((row) => row.id === id)
    if (!device) return
    this.set({ ...held, asking: null, revoking: id, failure: null })
    try {
      await this.session.revokeRecoveryDevice(id)
    } catch (error) {
      this.set({ ...held, asking: null, revoking: null, failure: readFailure("revoke", error) })
      return
    }
    if (this.stopped) return
    this.set({ phase: "continuing", removed: device })
    this.options.continueSignIn()
  }
}
