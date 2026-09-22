// The Swift app's hosted-console transport, given types and one way in.
//
// `legacy/js/net/cloud-*.js` are that console's Cloud modules, copied byte for
// byte (`legacy/MANIFEST.json`, `tools/check-legacy-css.sh`). They are the code
// app.clawdline.com runs today against the relay that is deployed today, and
// the places they look over-careful are the places they were taught something
// (docs/cloud-wire.md). So they are used as they are, and what this console
// adds sits outside them: this file types the little of them it calls, the
// way `legacy/bridge.ts` does for the rest of the copied page.
import {
  CloudViewerSession as CloudViewerSessionOriginal,
  chooseTransport as chooseTransportOriginal,
  keepConnected as keepConnectedOriginal,
  pairViewer as pairViewerOriginal,
  pairViewerFromInvitation as pairViewerFromInvitationOriginal,
  readCloudConfig as readCloudConfigOriginal,
} from "../legacy/js/net/cloud-boot.js"
import { decodePairingInvitation as decodePairingInvitationOriginal } from "../legacy/js/net/cloud-pairing.js"
import {
  cloudOnboardingMode as cloudOnboardingModeOriginal,
  cloudViewerDeviceMetadata as cloudViewerDeviceMetadataOriginal,
} from "../legacy/js/net/cloud-onboarding.js"
import type { CloudReadClient } from "./relay-reader.js"
import type { OpenedPairing, PendingOffer } from "./pair.js"

/** A build's Cloud declaration, checked (`readCloudConfig`). */
export interface CloudConfig {
  appOrigin: string
  apiOrigin: string
  relayURL: string
  build: string
  strings: Record<string, string>
}

/** One machine as `CloudClient.machines()` lists it (`_machineRows`). */
export interface CloudMachine {
  id: string
  /** The machine's own name, e.g. "Studio Mac". */
  name?: string
  /** The name with its kind in front, e.g. "Mac 電腦 · Studio Mac". */
  label: string
  kind?: string
  observedAt: number | null
  freshness: "current" | "stale" | "unknown"
  pairing: "paired" | "not_paired" | "unknown"
  /** Absent when this browser has no readable session inventory for the machine. */
  sessions?: number
  selectable: boolean
  autoSelectable: boolean
}

/** The connected client, as the gate and the seam use it. */
export interface CloudClientHandle extends CloudReadClient {
  account: string | null
  deviceID: string | null
  /** The bounded receive log; an error receipt points to the row that names its machine. */
  readonly viewerEvents?: {
    snapshot(): {
      rows?: { n?: number; data?: { machine?: unknown; routed_machine?: unknown } }[]
    }
  }
  /**
   * Machines for which this client has verified and decrypted an authenticated
   * envelope. The copied client keeps this proof across its token renewals.
   */
  readonly viewerVerified?: ReadonlyMap<string, unknown>
  /** Clear an older negative pairing lookup after stronger capability evidence. */
  forgetMachinePairingAnswer?(machine: string): void
  machines(): Promise<{ machines: CloudMachine[]; syncing: boolean; retryAfterMs: number }>
  /**
   * What the last authenticated `orch/` snapshot of `machine` said it is, or
   * null when this browser has opened none — which is every machine it is not
   * paired with (`cloud-client.js`).
   */
  machineDescriptor?(machine: string): { machine?: { name?: string; platform?: string } } | null
  /** Low-level encrypted command seam used by the original webhook binder. */
  _publishCommand(
    machine: string,
    type: string,
    body: Record<string, unknown>,
    envelopeClass: "ctl",
  ): Promise<unknown>
}

/** A typed failure from the copied modules (`cloud-failure.js`, `bootError`). */
export interface CloudFailure extends Error {
  code?: string
  status?: number | null
  terminal?: boolean
}

/** What `keepConnected` reports, one state at a time (`cloud-boot.js`). */
export type CloudUpdate =
  | { state: "connected"; client: CloudClientHandle }
  | { state: "sign_in"; url: string }
  | { state: "device_limit_reached"; tier: string; limit: number | null; message: string }
  | { state: "pairing_required"; accountID: string }
  | { state: "retrying"; error?: CloudFailure; afterMs: number }
  | { state: "terminal_error"; error?: CloudFailure; reason?: string; attempts?: number }
  | { state: "revoked"; error?: CloudFailure }
  | { state: "reconnecting" }
  | { state: "paused" }

export interface CloudSession {
  readonly account: string | null
  readonly deviceID: string | null
  signInURL(): string
  now(): number
  startPairing(): Promise<PendingPairing>
  acceptPairingInvitation(invitation: PairingInvitation, pending: PendingPairing): Promise<PendingPairing>
  claimPairing(pending: PendingPairing): Promise<OpenedPairing>
}

export interface CloudConnection {
  stop(): void
  done: Promise<void>
}

/** The build's declaration, checked, or a throw that says what is wrong with it. */
export const readCloudConfig = readCloudConfigOriginal as (scope: { __clawdlineCloud?: unknown }) => CloudConfig | null

/** `cloud` when this page is served from the origin the build was made for, else `blocked`. */
export const chooseTransport = chooseTransportOriginal as (input: {
  mock?: boolean
  origin: string
  config: CloudConfig | null
}) => "mock" | "local" | "cloud" | "blocked"

/** `install` on an iPhone or iPad not yet running from the Home Screen; `pwa` or `browser` otherwise. */
export const cloudOnboardingMode = cloudOnboardingModeOriginal as (scope: Window) => "install" | "pwa" | "browser"

/** The coarse name and kind this browser registers itself under; never a user-agent string. */
export const cloudViewerDeviceMetadata = cloudViewerDeviceMetadataOriginal as (scope: Window) => {
  kind: "browser" | "ios" | "android"
  name: string
}

export function newCloudSession(options: { config: CloudConfig; deviceKind: string; deviceName: string }): CloudSession {
  // `handlers` is the Swift page's render seam (`net/handlers.js`, not copied):
  // this console draws from the client's events instead, so none is given.
  return new CloudViewerSessionOriginal({ ...options, handlers: null }) as unknown as CloudSession
}

export const keepConnected = keepConnectedOriginal as (
  session: CloudSession,
  options: { onState: (update: CloudUpdate) => void },
) => CloudConnection

/** A machine's link, decoded and checked (`decodePairingInvitation`); throws `invitation_expired` or `bad_*`. */
export interface PairingInvitation {
  invitation_id: string
  expires_at: number
}

/**
 * The browser half needed to claim a handover. In particular the private
 * X25519 key must survive leaving the waiting card; it is non-extractable but
 * can be structured-cloned by IndexedDB.
 */
export interface PendingPairing extends PendingOffer {
  claimNonce: string
  offer: Record<string, unknown>
  ephemeralPrivateKey: CryptoKey
}

export const decodePairingInvitation = decodePairingInvitationOriginal as (fragment: string, nowMilliseconds: number) => PairingInvitation

interface PairingHooks {
  onOffer?: (pending: PendingOffer) => void
  sleep?: (ms: number) => Promise<void>
  intervalMs?: number
}

/**
 * Show this browser's offer and claim what the machine seals for it
 * (`pairViewer`). The session must be past `ensureSession`: signed in, with a
 * device key — which `connected` and `pairing_required` both are.
 */
export const pairViewer = pairViewerOriginal as (session: CloudSession, options: PairingHooks) => Promise<OpenedPairing>

/** The same, answering the link a machine printed (`pairViewerFromInvitation`). */
export const pairViewerFromInvitation = pairViewerFromInvitationOriginal as (
  session: CloudSession,
  invitation: PairingInvitation,
  options: PairingHooks,
) => Promise<OpenedPairing>
