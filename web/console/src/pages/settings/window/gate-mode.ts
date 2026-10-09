// @ts-expect-error -- Node's strip-types test runner needs the source extension.
import { catalogWord } from "../../../catalog.ts"
export interface GateModeOption {
  id: "nexus" | "planning" | "verification" | "standard"
  label: string
  description: string
  current: boolean
  default?: boolean
}

/** The complete mode set stays visible even though the two switches remain the controls. */
export function gateModeOptions(planning: boolean, verification: boolean): GateModeOption[] {
  const current = planning && verification ? "nexus" : planning ? "planning" : verification ? "verification" : "standard"
  return [
    { id: "nexus", label: "NEXUS", description: catalogWord("literal", "4f066af02920"), current: current === "nexus" },
    { id: "planning", label: catalogWord("literal", "5cddfd18b35f"), description: catalogWord("literal", "bfa73268a872"), current: current === "planning", default: true },
    { id: "verification", label: catalogWord("literal", "b9b69876ed00"), description: catalogWord("literal", "b5ecad888fc4"), current: current === "verification" },
    { id: "standard", label: catalogWord("literal", "9f758b115a39"), description: catalogWord("literal", "38759b4f9a8d"), current: current === "standard" },
  ]
}

/** One sentence for every settings combination; a cycle keeps the pair it captured on assignment. */
export function gateModeText(planning: boolean, verification: boolean): string {
  if (planning && verification) return catalogWord("literal", "3f3878d54702")
  if (planning) return catalogWord("literal", "5cea22b9e003")
  if (verification) return catalogWord("literal", "5a2c278484d7")
  return catalogWord("literal", "14337b315281")
}
