/**
 * Where a signed-in Cloud tab lands, and which machine it reads there.
 *
 * The machine list used to be the only way in. It is the screen that pairs,
 * renames and forgets a machine, so it was also made the door, and an account
 * whose every machine this browser can already read still stopped on it and
 * asked which one — a question the console no longer needs answered, because
 * "所有機器" reads all of them and the opened Session says which machine it is
 * on. So the list is a destination now, one press away under the header's own
 * name, and the way in is the console.
 *
 * One machine is still chosen underneath the fleet: the original Session page,
 * its Projects, its schedules and its terminals are one machine's, and the
 * reader is repointed when a Session on another machine is opened
 * (`CloudGate.tsx` `pointAt`). This only decides which one it starts on.
 *
 * Nothing here reads `location`, `sessionStorage` or the client: the caller
 * hands in what they said, so the rules can be read and tested as rules.
 */

/** A machine as the gate's corrected list has it; `selectable` is "this browser can read it". */
export interface ListedMachine {
  id: string
  selectable: boolean
}

export type Opening =
  /** Nothing can be opened yet: stay on the list, and ask again when it changes. */
  | { at: "list" }
  | { at: "console"; machine: string; fleet: boolean }

export function openingFor(said: {
  machines: readonly ListedMachine[]
  /**
   * The machine this tab's address names, and whether that address is one the
   * fleet holds. A Session destination is the fleet's own address, so it opens
   * there; a document address is one machine's page, and opening the fleet
   * over it would take the person to the Session list instead of the document.
   */
  addressed?: { machine: string; inFleet?: boolean } | null
  /** The address is the fleet's own, `#all-machines`. */
  everyMachineAddressed?: boolean
  /** The machine this tab chose before, from its own storage. */
  remembered?: string | null
}): Opening {
  const readable = said.machines.filter((machine) => machine.selectable).map((machine) => machine.id)
  if (!readable.length) return { at: "list" }
  const fleet = readable.length >= 2

  // An address names its machine, and only that machine will do: a Session
  // this browser cannot read yet is waited for, not replaced with another.
  if (said.addressed) {
    return readable.includes(said.addressed.machine)
      ? { at: "console", machine: said.addressed.machine, fleet: fleet && said.addressed.inFleet === true }
      : { at: "list" }
  }

  const remembered = said.remembered && readable.includes(said.remembered) ? said.remembered : null
  if (said.everyMachineAddressed && fleet) {
    return { at: "console", machine: remembered ?? readable[0]!, fleet: true }
  }
  // A tab that chose one machine keeps it. The choice is this tab's own
  // (`sessionStorage`), so a new tab has made none and gets the fleet.
  if (remembered) return { at: "console", machine: remembered, fleet: false }
  // Nothing addressed, nothing chosen, and more than one machine to read:
  // the fleet, rather than a question.
  if (fleet) return { at: "console", machine: readable[0]!, fleet: true }
  // One machine and no choice recorded. The list is still the way in: its row
  // carries this machine's own name, its pairing and the way to forget it, and
  // a person who has never picked it has not yet seen any of that.
  return { at: "list" }
}
