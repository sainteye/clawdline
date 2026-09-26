// Which machines a schedule may live on, as the hosted gate knows them.
//
// A schedule lives on the machine that runs it: its row is in that machine's
// own store and that machine's clock fires it, so it fires while every other
// machine is offline (docs/schedules.md, "Which machine runs it"). The console
// is one machine's by construction (`relay-reader.ts`), but the schedule list
// is the one place it shows every machine's rows — and every action on a row
// goes to that row's machine, not to the header's.
//
// This file is the seam between the two. The hosted gate publishes the machines
// the header switcher offers (`CloudGate.tsx`); the schedule page reads them
// here. The console a daemon serves on its own network publishes nothing, so
// `scheduleFleet()` is null there and the page draws exactly what it drew
// before this file existed: no machine column, no machine field.
//
// Nothing here is imported at run time, so `node --test` loads it as it is.

/** One machine a schedule can be on, in the header switcher's words. */
export interface ScheduleMachine {
  id: string
  /** The machine's own name, as the switcher shows it. */
  name: string
  /** The switcher's platform word: macOS, Linux, … */
  platform: string
  /** When the account last heard from it, milliseconds; null when not known here. */
  seenAt: number | null
  /** Whether it reported in recently. Online and offline are the only two states. */
  online: boolean
}

export interface ScheduleFleet {
  /** The header's machine: the default for a new schedule. */
  current: string
  machines: ScheduleMachine[]
}

let fleet: ScheduleFleet | null = null
const listeners = new Set<() => void>()

/** Set by the hosted gate whenever its machine list changes; null takes it away. */
export function publishScheduleFleet(next: ScheduleFleet | null): void {
  fleet = next
  for (const listener of listeners) listener()
}

/** The machines, or null on a console with no Cloud account behind it. */
export function scheduleFleet(): ScheduleFleet | null {
  return fleet
}

export function onScheduleFleet(listener: () => void): () => void {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

/** One machine's presence as the client computes it now. */
export interface ScheduleMachinePresence {
  id: string
  online: boolean
  seenAt: number | null
}

// The published fleet is recomputed only when the gate lists machines again,
// and it stops listing once a console is on screen: a machine that was
// restarting when the page opened stays offline in it. The gate answers this
// with the client's own computation, made now.
let presenceNow: (() => Promise<readonly ScheduleMachinePresence[]>) | null = null

/** Set by the hosted gate while it has a client; null takes it away. */
export function answerSchedulePresence(ask: (() => Promise<readonly ScheduleMachinePresence[]>) | null): void {
  presenceNow = ask
}

/**
 * Which machines report in now, by id, with the published fleet brought up to
 * it; null when there is no one to ask. A machine the answer does not list
 * keeps its row as it was.
 */
export async function recheckSchedulePresence(): Promise<ReadonlyMap<string, boolean> | null> {
  if (!presenceNow) return null
  const rows = await presenceNow()
  const now = new Map(rows.map((row) => [row.id, row] as const))
  if (fleet) {
    const changed = fleet.machines.some((m) => {
      const row = now.get(m.id)
      return !!row && (row.online !== m.online || row.seenAt !== m.seenAt)
    })
    if (changed) {
      publishScheduleFleet({
        ...fleet,
        machines: fleet.machines.map((m) => {
          const row = now.get(m.id)
          return row ? { ...m, online: row.online, seenAt: row.seenAt } : m
        }),
      })
    }
  }
  return new Map(rows.map((row) => [row.id, row.online] as const))
}

/**
 * A machine that just answered a read is online: its row says so, so the
 * page stops calling it offline from an older reading.
 */
export function noteScheduleMachineAnswered(id: string): void {
  if (!fleet || !fleet.machines.some((m) => m.id === id && !m.online)) return
  publishScheduleFleet({
    ...fleet,
    machines: fleet.machines.map((m) => (m.id === id ? { ...m, online: true } : m)),
  })
}

/**
 * Whether the page says which machine at all. A console without Cloud has one
 * machine by definition, and an account with one selectable machine has
 * nothing to choose between: both draw what the local console draws.
 */
export function choosesMachine(f: ScheduleFleet | null = fleet): f is ScheduleFleet {
  return !!f && f.machines.length > 1
}

/** "Studio · macOS": the name and the platform word the switcher shows. */
export function machineWords(machine: ScheduleMachine): string {
  return machine.platform ? machine.name + " · " + machine.platform : machine.name
}

export function machineNamed(id: string, f: ScheduleFleet | null = fleet): ScheduleMachine | null {
  return f?.machines.find((m) => m.id === id) ?? null
}

// Which machine each schedule id was last listed on. Written when the list is
// drawn, read by every action on a row — the history sheet, Edit, Run now,
// Delete and the webhook panel — so each goes to the machine the row came
// from rather than to the header's.
const owners = new Map<string, string>()

export function rememberScheduleOwners(rows: readonly { id?: unknown; machine?: unknown }[]): void {
  owners.clear()
  for (const row of rows) {
    if (typeof row.id === "string" && row.id && typeof row.machine === "string" && row.machine) {
      owners.set(row.id, row.machine)
    }
  }
}

/** The machine a schedule id was listed on, or undefined on a console that lists one machine. */
export function scheduleOwner(id: string | null | undefined): string | undefined {
  return id ? owners.get(id) : undefined
}

/** One machine's part of the list. */
export interface ScheduleGroup<Row> {
  machine: ScheduleMachine
  rows: Row[]
  /**
   * The code a machine that could have answered did not answer with. Such a
   * machine is drawn as a named group saying it is offline or unreadable,
   * never as a machine with no schedules.
   */
  unanswered: string | null
}

/**
 * The list, one group per machine: the header's machine first, then the rest
 * in the switcher's order. A machine that answered with no schedules draws no
 * group; a machine that did not answer draws one, named, with no rows. A row
 * from a machine the switcher does not offer — unpaired, forgotten — is not
 * drawn: nothing on this page could open it.
 */
export function groupSchedules<Row extends { machine?: unknown }>(
  rows: readonly Row[],
  unanswered: readonly { machine?: unknown; code?: unknown }[],
  f: ScheduleFleet,
): ScheduleGroup<Row>[] {
  const order = [...f.machines].sort((a, b) =>
    a.id === f.current ? -1 : b.id === f.current ? 1 : 0,
  )
  const silent = new Map<string, string>()
  for (const row of unanswered) {
    if (typeof row.machine === "string" && row.machine) {
      silent.set(row.machine, typeof row.code === "string" && row.code ? row.code : "unanswered")
    }
  }
  const groups: ScheduleGroup<Row>[] = []
  for (const machine of order) {
    const mine = rows.filter((row) => row.machine === machine.id)
    const code = silent.get(machine.id) ?? null
    if (!mine.length && code === null) continue
    groups.push({ machine, rows: mine, unanswered: code })
  }
  return groups
}
