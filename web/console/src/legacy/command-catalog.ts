import type { AssistantSkill } from "@clawdline/contract"
import catalog from "./native-commands.json" with { type: "json" }

type Definition = { name: string; description: string; descriptionZhHant: string; aliases: string[] }
export type MenuCommand = AssistantSkill & { descriptionZhHant?: string; aliases?: string[] }

export function nativeDefinitions(assistant: string | undefined): readonly Definition[] {
  return assistant === "claude" || assistant === "codex" ? catalog[assistant].commands : []
}

/** Explicit invocations distinguish Codex's /commands, /prompts:name and $skills. */
export function commandInvocation(command: AssistantSkill, assistant: string | undefined): string {
  return command.invocation || (assistant === "codex" ? "$" : "/") + command.name
}

/** Custom commands replace a same-named native command; Codex skills stay separate. */
export function commandCatalog(assistant: string | undefined, skills: readonly AssistantSkill[]): MenuCommand[] {
  const effective = new Map<string, MenuCommand>()
  for (const definition of nativeDefinitions(assistant)) {
    effective.set("/" + definition.name, { ...definition, source: "system", invocation: "/" + definition.name })
  }
  for (const skill of skills) effective.set(commandInvocation(skill, assistant), skill)
  return [...effective.values()]
}

export function commandDescription(command: MenuCommand, language: string | undefined): string {
  return language === "zh-Hant" && command.descriptionZhHant ? command.descriptionZhHant : command.description
}
