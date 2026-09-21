import { client } from "../../client.js"
import { RefusalError, isRefusal } from "@clawdline/core"

/**
 * `/v1/cloud/status`, the line to app.clawdline.com.
 *
 * Read here rather than through the shared client for the same reason
 * `api.ts` reads `/v1/settings` here: the route belongs to this page and to
 * nothing else. It is behind **this machine's own token** — it names the relay,
 * the account, the machine's key fingerprint and every enrolled viewer — so a
 * paired phone reading this window over a tunnel gets a refusal, and this card
 * says so rather than drawing an empty one.
 *
 * The shape is hand-written rather than generated from `api/v1/`. That is a
 * deliberate, recorded gap: regenerating the contract rewrites two files that
 * another task is holding open, and a card that reads six fields is not worth
 * a merge conflict across 218 types. `docs/cloud-wire.md` §16.3 carries it, and
 * the schema is the follow-up.
 */
export type CloudViewer = {
  id: string
  kind?: string
  name?: string
  caps?: string[]
  fingerprint?: string
  /**
   * Whether this Mac itself handed that browser the account key, or whether it
   * is only on the account's list. Both are admitted today, and they are
   * different amounts of evidence, so the card says which.
   */
  pinned?: boolean
  paired_at?: number
  revoked?: boolean
  revoked_at?: number
}

/** The handover in progress: idle | waiting | sealing | paired | failed. */
export type CloudPairing = {
  phase: string
  invitation_id?: string
  /** The link to open in the browser being paired. Its fragment is a secret. */
  link?: string
  expires_at?: number
  machine_fingerprint?: string
  viewer_device_id?: string
  viewer_fingerprint?: string
  paired_at?: number
  error?: string
  error_at?: number
}

export type CloudStatus = {
  enabled: boolean
  commands: boolean
  configured: boolean
  /** off | idle | connected | reconnecting | stopped */
  state: string
  relay_url?: string
  api_base?: string
  account?: string
  machine_id?: string
  machine_name?: string
  fingerprint?: string
  last_error?: string
  last_error_at?: number
  last_close?: string
  connected_since?: number
  token_expires_at?: number
  connects: number
  reconnects: number
  published: number
  acked: number
  publish_errors: number
  inbound_total: number
  inbound_dropped?: Record<string, number>
  answered: number
  refused: number
  /** Requests turned away at a full queue; each sender was answered `cloud_ingress_busy`. */
  queue_refused: number
  /** Of those, the ones whose refusal could not be sent: the only requests that reached nobody. */
  queue_unanswered: number
  devices?: CloudViewer[]
  roster_readable: boolean
  pinned_readable?: boolean
  pinned_error?: string
  pairing?: CloudPairing
  commandset?: string[]
}

/**
 * Answers the status, or null when this daemon will not tell this caller.
 *
 * A refusal is null rather than a throw: "the line is off" and "you may not ask
 * from here" are both answered by drawing the card's one honest sentence, and a
 * settings window that throws on a tab nobody has opened yet is worse than one
 * that says nothing about Cloud.
 */
export async function readCloudStatus(): Promise<CloudStatus | null> {
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), 10_000)
  try {
    const res = await fetch(client.url("/v1/cloud/status"), { signal: controller.signal })
    if (!res.ok) return null
    return (await res.json()) as CloudStatus
  } catch {
    return null
  } finally {
    clearTimeout(timer)
  }
}

/**
 * The pairing routes, which unlike the status route **change** something.
 *
 * They are behind this machine's own token for a reason worth restating where
 * the calls are: `POST /v1/cloud/pairing` answers a link whose fragment hands
 * the account's master secret to whoever opens it. A refusal is thrown rather
 * than swallowed here — the card has to be able to say why, because "nothing
 * happened" is the one answer a person cannot act on.
 */
async function cloudCall<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(client.url(path), {
    method,
    headers: body === undefined ? undefined : { "content-type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const text = await res.text()
  let parsed: unknown = null
  try {
    parsed = text ? JSON.parse(text) : null
  } catch {
    parsed = null
  }
  if (!res.ok) {
    if (isRefusal(parsed)) throw new RefusalError(res.status, parsed, path)
    throw new Error(`${path} 回答 ${res.status}`)
  }
  return parsed as T
}

/** Show a fresh one-time link. Whatever was waiting is replaced. */
export function beginCloudPairing(): Promise<CloudPairing> {
  return cloudCall<CloudPairing>("POST", "/v1/cloud/pairing")
}

/** Stop waiting. The link is left to expire on its own; nothing withdraws one. */
export function cancelCloudPairing(): Promise<CloudPairing> {
  return cloudCall<CloudPairing>("DELETE", "/v1/cloud/pairing")
}

/** Finish from the code a desktop browser is showing, which has no camera to point. */
export function offerCloudPairing(code: string): Promise<CloudPairing> {
  return cloudCall<CloudPairing>("POST", "/v1/cloud/pairing/offer", { offer: code })
}

/** Throw one browser out of this Mac. Local, immediate, and not the account's list. */
export function revokeCloudViewer(device: string): Promise<{ device: string; revoked: boolean }> {
  return cloudCall("POST", "/v1/cloud/devices/revoke", { device })
}
