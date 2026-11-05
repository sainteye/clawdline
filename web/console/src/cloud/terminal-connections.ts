import type { TerminalCloudClient, RelayEvent } from "./terminal-transport.js"
// @ts-expect-error -- Node's strip-types runner loads source paths.
import { RECEIPT_MS } from "./terminal-session.ts"

export interface TerminalConnectionRow {
  machine: string
  connection: string
  opened_at: number
  expires_at: number
  active: boolean
}
export interface TerminalConnections { limit: number; connections: TerminalConnectionRow[] }
const fail = (code: string) => Object.assign(new Error(code), { code })

/** Authenticated, content-free relay metadata; this never acquires a terminal slot or sends input. */
export function manageTerminalConnections(client: Pick<TerminalCloudClient, "ready" | "retired" | "events" | "_send">, action: "list" | "disconnect",
  target?: Pick<TerminalConnectionRow, "machine" | "connection">): Promise<TerminalConnections> {
  if (!client.ready || client.retired) return Promise.reject(fail("cloud_reconnecting"))
  const request_id = crypto.randomUUID()
  return new Promise((resolve, reject) => {
    let stop = () => {}
    const finish = (error?: Error, result?: TerminalConnections) => {
      clearTimeout(timer); stop()
      if (error) reject(error); else resolve(result!)
    }
    const timer = setTimeout(() => finish(fail(action === "disconnect" ? "terminal_connection_disconnect_unknown" : "terminal_receipt_timeout")), RECEIPT_MS)
    stop = client.events((event: RelayEvent) => {
      if (event.type !== "terminal_connections_result" || event.request_id !== request_id || event.action !== action) return
      if (event.code) { finish(fail(event.code)); return }
      const rows = event.connections
      if (!Number.isSafeInteger(event.limit) || event.limit! <= 0 || !Array.isArray(rows) || rows.length > event.limit! ||
        rows.some((row) => !row || typeof row.machine !== "string" || !/^[A-Za-z0-9_-]{1,128}$/.test(row.machine) ||
          typeof row.connection !== "string" || !/^[A-Za-z0-9_-]{1,128}$/.test(row.connection) ||
          !Number.isFinite(row.opened_at) || !Number.isFinite(row.expires_at) || typeof row.active !== "boolean")) {
        finish(fail("terminal_bad_receipt")); return
      }
      finish(undefined, { limit: event.limit!, connections: rows })
    })
    try { client._send({ type: "terminal_connections", request_id, action, ...(target ? { machine: target.machine, connection: target.connection } : {}) }) }
    catch (error) { finish(error instanceof Error ? error : fail("terminal_send_failed")) }
  })
}
