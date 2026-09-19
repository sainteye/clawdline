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
  readCloudConfig as readCloudConfigOriginal,
} from "../legacy/js/net/cloud-boot.js"
import {
  cloudOnboardingMode as cloudOnboardingModeOriginal,
  cloudViewerDeviceMetadata as cloudViewerDeviceMetadataOriginal,
} from "../legacy/js/net/cloud-onboarding.js"
import type { CloudReadClient } from "./relay-reader.js"

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
  sessions: number
  selectable: boolean
  autoSelectable: boolean
}

/** The connected client, as the gate and the seam use it. */
export interface CloudClientHandle extends CloudReadClient {
  account: string | null
  deviceID: string | null
  machines(): Promise<{ machines: CloudMachine[]; syncing: boolean; retryAfterMs: number }>
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
