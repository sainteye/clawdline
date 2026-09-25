// How the Projects page reads a *different* machine's project settings.
//
// Everything else on this console reads the one machine it was opened on
// (`install.ts`). Project settings sync (docs/project-sync.md) is the one page
// that needs two: the mirror it is showing, and the source that owns the
// settings. The two machines never reach each other — each has its own content
// key, and the relay carries only ciphertext — so the browser, which is paired
// with both, is the courier. The gate installs this seam when it connects and
// takes it away when it goes; a page with no seam is a page on the machine's
// own network, and says the sync needs Clawdline Cloud.

/** One other machine this browser could read. */
export interface SyncMachine {
  id: string
  name: string
  /** Whether the machine says it answers `project-manifest`; null when its descriptor has not arrived. */
  offers: boolean | null
  selectable: boolean
}

export interface ProjectSyncSeam {
  /** The machine this console is showing. */
  here(): string | null
  /** Every machine on the account this browser has paired with. */
  machines(): Promise<SyncMachine[]>
  /** One read of `machine`'s own route, answered as that route's body. */
  read<T>(machine: string, word: "project-manifest" | "project-entry", body: Record<string, unknown>): Promise<T>
}

let seam: ProjectSyncSeam | null = null

/** The gate's half: install on connect, `null` when the line goes. */
export function setProjectSyncSeam(next: ProjectSyncSeam | null): void {
  seam = next
}

/** The page's half. */
export function projectSyncSeam(): ProjectSyncSeam | null {
  return seam
}

/** The client surface the gate hands over; typed loosely, as the copied client is. */
export interface SyncClient {
  machines(): Promise<{ machines: { id: string; name?: string; label: string; selectable: boolean }[] }>
  machineDescriptor?: (machine: string) => { machine?: { commands?: unknown } } | null
  _machineRequest?: (machine: string, word: string, body: Record<string, unknown>, kind: "read" | "action") => Promise<unknown>
}

/** A seam over whichever client is current at the moment of each call. */
export function syncSeamFor(current: () => SyncClient | null, here: () => string | null): ProjectSyncSeam {
  const client = (): SyncClient => {
    const c = current()
    if (!c) throw Object.assign(new Error("the cloud connection is not ready"), { code: "offline" })
    return c
  }
  return {
    here,
    async machines() {
      const c = client()
      const answer = await c.machines()
      return answer.machines.map((m) => {
        const commands = c.machineDescriptor?.(m.id)?.machine?.commands
        return {
          id: m.id,
          name: m.name || m.label || m.id,
          offers: Array.isArray(commands) ? commands.includes("project-manifest") : null,
          selectable: m.selectable,
        }
      })
    },
    async read<T>(machine: string, word: "project-manifest" | "project-entry", body: Record<string, unknown>): Promise<T> {
      const c = client()
      if (typeof c._machineRequest !== "function") {
        throw Object.assign(new Error("This console cannot read another machine."), { code: "cloud_not_carried" })
      }
      return (await c._machineRequest(machine, word, body, "read")) as T
    },
  }
}
