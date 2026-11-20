// @ts-expect-error -- Node's strip-types runner needs the source extension.
import { catalogWord, currentCatalogTag } from "./catalog.ts"
import source from "./builtin-display-source.json" with { type: "json" }

export interface DisplayNames { en: string; "zh-Hant"?: string }

/** Unrecognized and edited metadata keeps the machine's own words. */
export function wireDisplay(words: DisplayNames): string {
  return (currentCatalogTag() === "zh-Hant" ? words["zh-Hant"] : words.en) || words.en || words["zh-Hant"] || ""
}

/** Match bundled metadata against the known English pair. Older Bot extension
 * catalogs omit the skill purpose, so the exact built-in ID and name are used
 * in that case. Explicit or edited metadata keeps its source text.
 */
export function builtinDisplay(kind: "persona" | "skill", id: string, name: DisplayNames,
  detail?: DisplayNames): { name: string; detail: string } {
  const prefix = `clawdline.${kind}.`
  const short = kind === "persona" && !id.includes(".") ? id : id.startsWith(prefix) ? id.slice(prefix.length) : ""
  const domain = kind === "persona" ? "builtinPersona" : "builtinSkill"
  const field = kind === "persona" ? "summary" : "purpose"
  const key = `${domain}.${short}`
  const en = source.en as Record<string, string>
  const detailMatches = detail ? detail.en === en[`${key}.${field}`] : kind === "skill"
  if (short && Object.hasOwn(en, `${key}.name`) && name.en === en[`${key}.name`] && detailMatches) {
    return { name: catalogWord(domain, `${short}.name`), detail: catalogWord(domain, `${short}.${field}`) }
  }
  return { name: wireDisplay(name), detail: detail ? wireDisplay(detail) : "" }
}

const teamKeys: Record<string, string> = {
  engineering: "personaTeamEngineering", marketing: "personaTeamMarketing", product: "personaTeamProduct",
  quality: "personaTeamQuality", operations: "personaTeamOperations", design: "personaTeamDesign",
  business: "personaTeamBusiness", custom: "personaTeamCustom",
}

export function builtinTeamDisplay(id: string, words: DisplayNames): string {
  const short = id.startsWith("clawdline.team.") ? id.slice("clawdline.team.".length) : ""
  return Object.hasOwn(teamKeys, short) && words.en === short && words["zh-Hant"] === short
    ? catalogWord("next", teamKeys[short]) : wireDisplay(words)
}
