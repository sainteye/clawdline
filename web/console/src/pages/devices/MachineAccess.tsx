import { useCallback, useEffect, useRef, useState, type ReactNode } from "react"
import { nextWord, type NextWord } from "../../next-strings.js"
import { PairCard, codeOf, when, type InviteAsk } from "./BrowserInvites.js"
import { listViewerDevices, revokeViewerDevice } from "../../cloud/viewer-devices.js"
import type { MachineManager } from "../../cloud/machine-manager.js"
import "./account-machines.css"

/**
 * One machine's access, the top of the Devices page on both consoles: the
 * same card to pair a new browser and the same list of everything that may
 * use this machine, in three groups whose Revoke says how far it reaches.
 *
 * - Invited browsers (docs/browser-invite.md): this machine only.
 * - The account's browsers and phones: on Cloud a revoke ends that device for
 *   the whole account; on the machine's own console it refuses it here only,
 *   because a machine cannot revoke an account device.
 * - Devices signed in to this machine directly (password, pairing code,
 *   `clawdline open`).
 *
 * Where a list cannot be read here it says so instead of reading as empty.
 */

export interface AccessRow { id: string; name: string; detail: string; current?: boolean }

export interface AccessSource {
  key: string
  machineName: string
  ask: InviteAsk | null
  inviteAvailable: boolean
  unavailableWord: "invUnavailable" | "invInvitedHere" | "accessCloudOff" | "accessNotOwner"
  invited(): Promise<AccessRow[]>
  revokeInvited(id: string): Promise<void>
  account(): Promise<AccessRow[]>
  revokeAccount(id: string): Promise<void>
  accountNote: NextWord
  /** Null when this console shows the direct list in a block of its own (`directBlock`). */
  direct: (() => Promise<AccessRow[]>) | null
  revokeDirect(id: string): Promise<void>
  directBlock?: ReactNode
}

type Loaded = { rows: AccessRow[] } | { error: string } | null

function load(read: () => Promise<AccessRow[]>, set: (l: Loaded) => void, live: { current: boolean }) {
  read().then((rows) => live.current && set({ rows }), (e) => live.current && set({ error: codeOf(e) }))
}

export function MachineAccess({ source, picker }: { source: AccessSource; picker?: ReactNode }) {
  const [invited, setInvited] = useState<Loaded>(null)
  const [account, setAccount] = useState<Loaded>(null)
  const [direct, setDirect] = useState<Loaded>(null)
  const live = useRef(true)
  useEffect(() => { live.current = true; return () => { live.current = false } }, [])

  const reload = useCallback(() => {
    load(source.invited, setInvited, live)
    load(source.account, setAccount, live)
    if (source.direct) load(source.direct, setDirect, live)
  }, [source])
  useEffect(() => { setInvited(null); setAccount(null); setDirect(null); reload() }, [reload])

  return (
    <div className="am-stack machine-access">
      {picker}
      <PairCard machineKey={source.key} machineName={source.machineName} ask={source.ask}
        available={source.inviteAvailable} unavailableWord={source.unavailableWord} onChanged={reload} />
      <section aria-labelledby="access-list-title">
        <p className="am-label" id="access-list-title">{source.machineName
          ? nextWord("accessListTitle", { machine: source.machineName }) : nextWord("accessListTitleHere")}</p>
        <Group title="accessGroupInvited" note="accessGroupInvitedNote" loaded={invited}
          revoke={async (id) => { await source.revokeInvited(id); reload() }} />
        <Group title="accessGroupAccount" note={source.accountNote} loaded={account}
          revoke={async (id) => { await source.revokeAccount(id); reload() }} />
        {source.direct ? (
          <Group title="accessGroupDirect" note="accessGroupDirectNote" loaded={direct}
            revoke={async (id) => { await source.revokeDirect(id); reload() }} />
        ) : (
          <div className="access-group">
            <p className="access-group-title">{nextWord("accessGroupDirect")}</p>
            {source.directBlock}
          </div>
        )}
      </section>
    </div>
  )
}

function Group({ title, note, loaded, revoke }: { title: NextWord; note: NextWord; loaded: Loaded; revoke: (id: string) => Promise<void> }) {
  const [asking, setAsking] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [said, setSaid] = useState<string | null>(null)
  const go = async (row: AccessRow) => {
    setBusy(true)
    try { await revoke(row.id); setAsking(null); setSaid(null) } catch (e) { setSaid(nextWord("invRevokeFailed", { code: codeOf(e) })) } finally { setBusy(false) }
  }
  return (
    <div className="access-group">
      <p className="access-group-title">{nextWord(title)}</p>
      <p className="am-sub access-group-note">{nextWord(note)}</p>
      {loaded === null ? <p className="am-say" role="status">{nextWord("msDevicesLoading")}</p>
        : "error" in loaded ? <p className="am-say" role="status">{loaded.error === "forbidden" || loaded.error === "http_403"
          ? nextWord("accessNotOwnerList") : nextWord("accessGroupUnreadable", { code: loaded.error })}</p>
          : loaded.rows.length === 0 ? <p className="am-say">{nextWord("accessGroupEmpty")}</p>
            : (
              <ul className="am-card">
                {loaded.rows.map((row) => (
                  <li key={row.id} className="am-card-row">
                    {asking === row.id ? (
                      <div className="am-revoke-ask">
                        <p>{nextWord("accessRevokeAsk", { device: row.name })}</p>
                        <div className="am-pair-buttons">
                          <button type="button" className="am-ghost" onClick={() => setAsking(null)} disabled={busy}>{nextWord("msCancel")}</button>
                          <button type="button" className="am-danger-fill" onClick={() => void go(row)} disabled={busy}>
                            {busy ? nextWord("msRevoking") : nextWord("msRevoke")}</button>
                        </div>
                      </div>
                    ) : (
                      <>
                        <span className="am-card-text"><span>{row.name}</span><span className="am-sub">{row.detail}</span></span>
                        {row.current ? <span className="am-sub">{nextWord("msDeviceThis")}</span>
                          : <button type="button" className="am-ghost am-small" onClick={() => { setSaid(null); setAsking(row.id) }}>{nextWord("msRevoke")}</button>}
                      </>
                    )}
                  </li>
                ))}
              </ul>
            )}
      {said && <p className="am-say" role="status">{said}</p>}
    </div>
  )
}

// --- the two sources ---

interface InvitedBrowser { device_id: string; name?: string; expires_at: number; active: boolean }

function invitedRows(browsers: unknown): AccessRow[] {
  if (!Array.isArray(browsers)) throw new Error("unreadable_invited_list")
  return (browsers as InvitedBrowser[]).filter((b) => b.active).map((b) => ({
    id: b.device_id, name: b.name || "Browser",
    detail: b.expires_at ? nextWord("invUntil", { time: when(b.expires_at) }) : nextWord("invUntilRevoked"),
  }))
}

function shortDay(ms: number): string {
  try {
    return new Intl.DateTimeFormat(document.documentElement.lang || undefined, { month: "numeric", day: "numeric" }).format(new Date(ms))
  } catch { return new Date(ms).toLocaleDateString() }
}

function dayOf(seconds: number | undefined): string {
  return seconds ? nextWord("msDeviceLastUsed", { date: shortDay(seconds * 1000) }) : nextWord("msDeviceNeverUsed")
}

function directRows(list: unknown): AccessRow[] {
  const devices = (list as { devices?: unknown })?.devices
  if (!Array.isArray(devices)) throw new Error("unreadable_direct_list")
  return (devices as Array<{ id: string; name: string; last_seen?: number; created: number }>).map((d) => ({
    id: d.id, name: d.name || "Browser", detail: dayOf(d.last_seen ?? d.created),
  }))
}

/** Clawdline Cloud: invitations and the direct list are machine words; the account list is the account's. */
export function cloudAccessSource(manager: MachineManager, machineID: string, machineName: string): AccessSource {
  const invitedViewer = manager.invited?.() === true
  const ask: InviteAsk | null = manager.ask
    ? (word, body, kind) => manager.ask!(machineID, word, body, kind)
    : null
  const serves = (word: string) => manager.serves?.(machineID, word) === true
  return {
    key: machineID,
    machineName,
    ask,
    inviteAvailable: !invitedViewer && serves("browser-invite-start"),
    unavailableWord: invitedViewer ? "invInvitedHere" : "invUnavailable",
    invited: async () => {
      if (!ask || !serves("browser-invite-list")) throw new Error("machine_needs_update")
      return invitedRows((await ask("browser-invite-list", {}, "read")).browsers)
    },
    revokeInvited: async (id) => { await ask!("browser-invite-revoke", { device_id: id }, "action") },
    account: async () => {
      const list = await listViewerDevices(manager.apiOrigin)
      if (list.kind !== "listed") throw new Error(list.kind === "refused" ? "refused_" + list.status : "unreadable")
      const me = manager.deviceID()
      return list.devices.filter((d) => d.kind !== "invited_browser").map((d) => ({
        id: d.id, name: d.name || "Browser", current: d.id === me,
        detail: d.lastSeenAt && !Number.isNaN(Date.parse(d.lastSeenAt))
          ? nextWord("msDeviceLastUsed", { date: shortDay(Date.parse(d.lastSeenAt)) }) : nextWord("msDeviceNeverUsed"),
      }))
    },
    revokeAccount: async (id) => {
      const outcome = await revokeViewerDevice(manager.apiOrigin, id)
      if (outcome.kind !== "revoked") throw Object.assign(new Error(outcome.code), { code: outcome.code })
    },
    accountNote: "accessGroupAccountCloudNote",
    direct: async () => {
      if (!ask || !serves("direct-devices")) throw new Error("machine_needs_update")
      return directRows(await ask("direct-devices", {}, "read"))
    },
    revokeDirect: async (id) => { await ask!("direct-device-revoke", { device_id: id }, "action") },
  }
}

type Fetch = typeof fetch

async function localJSON(read: Fetch, path: string, init?: RequestInit): Promise<Record<string, unknown>> {
  const res = await read(path, { cache: "no-store", ...init, headers: { "Content-Type": "application/json", ...(init?.headers ?? {}) } })
  let body: Record<string, unknown> = {}
  try { body = await res.json() as Record<string, unknown> } catch { /* an empty answer */ }
  if (!res.ok) {
    const code = (body.error as { code?: unknown } | undefined)?.code
    throw Object.assign(new Error(typeof code === "string" ? code : "http_" + res.status), { code: typeof code === "string" ? code : "http_" + res.status, status: res.status })
  }
  return body
}

/** The invitation words as this machine's own routes (internal/transport/http/cloud_browser_invite.go). */
export function localInviteAsk(read: Fetch = fetch): InviteAsk {
  return async (word, body) => {
    const id = encodeURIComponent(String(body.invite_id ?? ""))
    switch (word) {
      case "browser-invite-start": return localJSON(read, "/v1/cloud/browser-invites", { method: "POST", body: JSON.stringify({ expiry: body.expiry }) })
      case "browser-invite-list": return localJSON(read, "/v1/cloud/browser-invites")
      case "browser-invite-status": return localJSON(read, "/v1/cloud/browser-invites/" + id)
      case "browser-invite-decide": return localJSON(read, `/v1/cloud/browser-invites/${id}/decision`, { method: "POST", body: JSON.stringify({ approve: body.approve }) })
      case "browser-invite-cancel": return localJSON(read, `/v1/cloud/browser-invites/${id}/cancel`, { method: "POST", body: "{}" })
      case "browser-invite-revoke": return localJSON(read, `/v1/cloud/browser-invites/devices/${encodeURIComponent(String(body.device_id))}/revoke`, { method: "POST", body: "{}" })
    }
    throw new Error("unknown_word")
  }
}

/** The machine's own console: everything is this machine's routes; the direct list keeps its own block. */
export function localAccessSource(machineName: string, cloudOn: boolean, directBlock: ReactNode, read: Fetch = fetch): AccessSource {
  const ask = localInviteAsk(read)
  return {
    key: "local",
    machineName,
    ask,
    inviteAvailable: cloudOn,
    unavailableWord: "accessCloudOff",
    invited: async () => invitedRows((await ask("browser-invite-list", {}, "read")).browsers),
    revokeInvited: async (id) => { await ask("browser-invite-revoke", { device_id: id }, "action") },
    account: async () => {
      const status = await localJSON(read, "/v1/cloud/status")
      const devices = status.devices
      if (!Array.isArray(devices)) return []
      return (devices as Array<{ id: string; kind?: string; name?: string; invited?: boolean; revoked?: boolean; paired_at?: number }>)
        .filter((d) => d.kind && d.kind !== "invited_browser" && !d.invited && !d.revoked)
        .map((d) => ({ id: d.id, name: d.name || "Browser", detail: d.paired_at ? nextWord("accessPairedOn", { date: shortDay(d.paired_at * 1000) }) : "" }))
    },
    revokeAccount: async (id) => { await localJSON(read, "/v1/cloud/devices/revoke", { method: "POST", body: JSON.stringify({ device: id }) }) },
    accountNote: "accessGroupAccountLocalNote",
    direct: null,
    revokeDirect: async () => {},
    directBlock,
  }
}
