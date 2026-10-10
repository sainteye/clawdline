/**
 * What a fleet toolbar press means once a machine has been named.
 *
 * The hosted console can show every machine's Sessions at once, and four of
 * the toolbar's presses need one machine before they can do anything. Asking
 * which was right; taking the person to that machine was not, because the
 * question "where shall I start this?" has an answer that is a destination,
 * not a move — and the move used to reload the page.
 *
 * So starting is answered where the person is standing: the sheet reads the
 * named machine's Projects and roles and starts there, and the Session that
 * arrives appears in the fleet list under its machine's own heading
 * (`session/Start.tsx`, `CloudSessionSheets.tsx`). The other three are that
 * machine's own console features, read through the one seam this page has, so
 * they still move the console onto it — in place now, with nothing thrown away
 * to get there.
 *
 * Nothing here reads a clock or a client; the caller says what it knows.
 */
import type { MachineToolbarAction } from "./FleetSessionList.js"

/** Whether answering this press means the console moves onto the named machine. */
export function toolMovesConsole(action: MachineToolbarAction): boolean {
  return action !== "start"
}

export interface PressedTool {
  machineID: string
  action: MachineToolbarAction
  /** When the press happened, by the same clock as `now`. */
  at: number
}

export type ToolPlan =
  /** Nothing to do: the press is too old to still be what the person meant. */
  | { do: "forget" }
  /** The console reads this machine; open the sheet as the single-machine page does. */
  | { do: "here"; action: MachineToolbarAction }
  /** Start on a machine the console is not reading, without moving it. */
  | { do: "elsewhere"; action: MachineToolbarAction; machine: string }
  /** The console is still moving onto that machine; ask again when it arrives. */
  | { do: "wait" }

/**
 * A press is honoured for `HOLD`; past that the console has been somewhere
 * else for long enough that opening a sheet would be a surprise rather than
 * the answer to a press.
 */
export const HOLD_MS = 30_000

export function toolPlan(said: {
  pressed: PressedTool
  /** The machine the console is reading now. */
  reading: string
  now: number
}): ToolPlan {
  const { pressed, reading, now } = said
  if (now - pressed.at > HOLD_MS) return { do: "forget" }
  if (pressed.machineID === reading) return { do: "here", action: pressed.action }
  if (toolMovesConsole(pressed.action)) return { do: "wait" }
  return { do: "elsewhere", action: pressed.action, machine: pressed.machineID }
}
