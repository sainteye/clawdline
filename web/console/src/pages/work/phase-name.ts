import { catalogWord } from "../../catalog.js"

const phaseKeys: Record<string, string> = {
  created: "phaseCreated",
  assigning: "phaseAssigning",
  assigned: "phaseAssigned",
  implementing: "phaseImplementing",
  verifying: "phaseVerifying",
  merging: "phaseMerging",
  deploying: "phaseDeploying",
  done: "phaseDone",
  cancelled: "phaseCancelled",
}

export function phaseName(phase: string): string {
  const key = phaseKeys[phase]
  return key ? catalogWord("work", key) : phase
}
