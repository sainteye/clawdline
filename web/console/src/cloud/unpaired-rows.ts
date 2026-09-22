/*
 * What a row may say about a machine this browser cannot read.
 *
 * Two things the relay client's list gets wrong about such a machine, both for
 * the same reason, and both fixed here rather than in the byte-for-byte copy
 * that makes them (`cloud-client.js` `_machineRows`).
 *
 * **Its session count.** The count is the number of session snapshots this
 * browser decrypted for the machine. For a machine it is not paired with that
 * is always 0, whatever the machine is running — a number made out of not
 * being able to look. `sessionsFact` says which of "a count", "cannot be read
 * here" and "not known yet" a row is.
 *
 * **Its name.** Every row's name comes out of the machine's own `orch/`
 * snapshot, which is encrypted to the account key the machine hands over at
 * pairing. So a machine this browser is not paired with has no readable name
 * at all, and the copied presentation falls back to the last eight characters
 * of its id —
 * "Machine · 51463f04" — on exactly the row a person has to recognise before
 * they can decide to pair it.
 *
 * The account holds a name for it anyway. `GET /v1/machines` answers each
 * machine's `name` and `platform` to this browser's own account cookie
 * (`requireAccount`, the same credential `forget.ts` and `rename.ts` send); the
 * machine registered that name when it signed in (`MachineName`,
 * `internal/transport/cloud/name.go`) and a rename from this page writes it.
 * It is a label, not a key: nothing is trusted from it, and a machine whose
 * own snapshot *can* be read keeps the name it published.
 *
 * Asking is best-effort. An account that will not answer leaves the rows as
 * they were — a short id is less friendly than a name, and still true.
 *
 * Nothing is imported at run time, so `node --test` loads it as it is.
 */

/** One machine as the account names it. */
export interface AccountName {
  name: string
  platform: string
}

/** The account's authoritative machine roster, including registrations it revoked. */
export interface AccountMachineRoster {
  names: Map<string, AccountName>
  revoked: Set<string>
}

/** Facts every machine row can use to distinguish one registration from another. */
export interface MachineIdentityFacts {
  shortID: string
  platform: "macos" | "linux" | "linux_aws" | "unknown"
  seenAt: number | null
}

type Get = (url: string, init?: RequestInit) => Promise<{ status: number; json(): Promise<unknown> }>

/** The account's names for its machines, by id; empty when it would not say. */
export async function accountMachineRoster(apiOrigin: string, get: Get = fetch): Promise<AccountMachineRoster | null> {
  const names = new Map<string, AccountName>()
  const revoked = new Set<string>()
  try {
    const res = await get(apiOrigin + "/v1/machines", { credentials: "include" })
    if (res.status !== 200) return null
    const body = (await res.json()) as { machines?: unknown }
    if (!body || !Array.isArray(body.machines)) return null
    for (const row of body.machines as Array<Record<string, unknown>>) {
      if (!row || typeof row.id !== "string" || !row.id) continue
      if (row.revoked_at) {
        revoked.add(row.id)
        continue
      }
      const name = typeof row.name === "string" ? row.name.trim() : ""
      if (!name) continue
      names.set(row.id, { name, platform: typeof row.platform === "string" ? row.platform : "" })
    }
  } catch {
    return null
  }
  return { names, revoked }
}

/** Compatibility for callers that only need labels and can tolerate no account answer. */
export async function accountMachineNames(apiOrigin: string, get: Get = fetch): Promise<Map<string, AccountName>> {
  return (await accountMachineRoster(apiOrigin, get))?.names ?? new Map()
}

/** A row that can be relabelled: the fields the presentation writes. */
interface Row {
  id: string
  name?: string
  label: string
  kind?: string
}

/**
 * The rows, with the account's name on every one this browser could not name
 * itself. `described` says whether this browser has read a machine's own
 * snapshot; `present` is the copied presentation (`machinePresentation`), so a
 * renamed row reads exactly like a described one.
 */
export function withAccountNames<T extends Row>(
  rows: readonly T[],
  names: ReadonlyMap<string, AccountName>,
  described: (id: string) => boolean,
  present: (id: string, name: string, platform: string) => { name: string; label: string; kind: string },
): T[] {
  return rows.map((row) => {
    if (described(row.id)) return row
    const known = names.get(row.id)
    if (!known) return row
    const shown = present(row.id, known.name, known.platform)
    return { ...row, name: shown.name, label: shown.label, kind: shown.kind }
  })
}

/**
 * How many sessions a row may claim. A count is only a count when this
 * browser could read the machine's list; see above.
 */
export function sessionsFact(machine: { pairing: string; sessions?: number }): { count: number } | "unread" | "unknown" {
  if (Number.isSafeInteger(machine.sessions) && (machine.pairing === "paired" || machine.sessions! > 0)) {
    return { count: machine.sessions! }
  }
  return machine.pairing === "not_paired" ? "unread" : "unknown"
}

/**
 * The small, truthful identity line beside a machine's mutable account name.
 *
 * Names can repeat and one physical host can leave more than one registration.
 * The opaque id fragment is therefore always shown. `observedAt` is the last
 * authenticated envelope this browser saw; it is not invented from the
 * control-plane roster, whose machine records carry no last-seen field.
 */
export function machineIdentityFacts(machine: { id: string; kind?: string; observedAt?: number | null }): MachineIdentityFacts {
  const id = typeof machine.id === "string" ? machine.id.trim() : ""
  const kind =
    machine.kind === "mac"
      ? "macos"
      : machine.kind === "linux-aws"
        ? "linux_aws"
        : machine.kind === "linux"
          ? "linux"
          : "unknown"
  const observed = machine.observedAt
  return {
    shortID: Array.from(id || "unknown").slice(-8).join(""),
    platform: kind,
    seenAt: typeof observed === "number" && Number.isFinite(observed) && observed > 0 ? observed : null,
  }
}
