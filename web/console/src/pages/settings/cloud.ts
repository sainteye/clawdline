import { client } from "../../client.js"

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
  queue_dropped: number
  devices?: CloudViewer[]
  roster_readable: boolean
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
