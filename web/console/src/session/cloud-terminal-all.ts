import type { Terminal } from "@clawdline/contract"
// @ts-expect-error -- Node's strip-types test runner loads the source path.
import { decodeCloudPlaceID, type CloudPlaceRow } from "../pages/terminal/cloud-project.ts"

export interface CloudTerminalMachine { id: string; name: string }
export interface CloudTerminalRow {
  machine: string
  machineName: string
  project: string
  projectName: string
  terminal: Terminal
  stale: boolean
  confirmedAt: number
}
export interface CloudTerminalError { machine: string; code: string }

/** A failed machine retains only its own prior rows, clearly marked stale. */
export async function collectCloudTerminals(
  machines: readonly CloudTerminalMachine[], places: readonly CloudPlaceRow[] | Promise<readonly CloudPlaceRow[]>, previous: readonly CloudTerminalRow[],
  read: (machine: string) => Promise<Terminal[]>,
  onProgress?: (result: { rows: CloudTerminalRow[]; errors: CloudTerminalError[] }) => void,
): Promise<{ rows: CloudTerminalRow[]; errors: CloudTerminalError[] }> {
  const rows: CloudTerminalRow[][] = Array.from({ length: machines.length }, () => [])
  const errors: CloudTerminalError[] = []
  let next = 0
  // Each read holds two relay subscriptions. Two concurrent reads leave half
  // the eight-channel socket available to other terminal activity.
  await Promise.all(Array.from({ length: Math.min(2, machines.length) }, async () => {
    for (;;) {
      const index = next++
      if (index >= machines.length) return
      const machine = machines[index]!
      try {
        const terminals = await read(machine.id)
        const namedPlaces = await places
        rows[index] = terminals.filter((terminal) => terminal.status === "running").map((terminal) => {
          const place = namedPlaces.find((candidate) => {
            const pair = decodeCloudPlaceID(candidate.id)
            return pair?.[0] === machine.id && pair[1] === terminal.project_id
          })
          return { machine: machine.id, machineName: machine.name, project: place?.id ?? "",
            projectName: place?.label ?? terminal.project_id, terminal, stale: false, confirmedAt: Date.now() }
        })
      } catch (error) {
        const code = (error as { code?: unknown })?.code
        errors.push({ machine: machine.id, code: typeof code === "string" ? code :
          error instanceof Error && error.message ? error.message : "terminal_list_failed" })
        rows[index] = previous.filter((row) => row.machine === machine.id).map((row) => ({ ...row, stale: true }))
      }
      onProgress?.({ rows: rows.flat(), errors: [...errors] })
    }
  }))
  return { rows: rows.flat(), errors }
}
