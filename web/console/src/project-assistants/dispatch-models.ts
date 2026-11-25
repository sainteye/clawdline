import type * as W from "@clawdline/contract"

export type DispatchModelChoice = { value: string; label: string; hint?: string }
export type DispatchModels = { choices: DispatchModelChoice[]; defaultName: string }

// The launcher's own rule (projects.ModelName): lowercase letters, digits, "." "_" "-",
// 1–64 characters, never opening with "-". A name it would refuse is never offered.
const launchable = (name: string) => /^[a-z0-9._][a-z0-9._-]{0,63}$/.test(name)

// Codex's ACP adapter lists each model once per effort ("gpt-5.5/high"). A dispatch
// names the model and the effort separately, so only the part before "/" is a model.
const modelPart = (value: string) => value.split("/")[0]

/**
 * The models a Bot dispatch can name for one agent, read from the agent's own
 * catalog. The agent's "Default" entry is left out: it is the same as naming no
 * model, which the drop-down offers first as "Agent default (<what it runs>)".
 */
export function dispatchModels(catalog: W.ProjectAssistantConfigurationCatalog | undefined): DispatchModels {
  const model = catalog?.options.find(o => o.category === "model")
  if (!model) return { choices: [], defaultName: "" }
  // The agent's own "Default" entry, found as agent-model-summary's nativeDefault
  // finds it (this module imports no runtime code so Node can test it directly).
  const own = model.choices.find(c => c.value === "default" || c.name.toLowerCase().startsWith("default"))
  const seen = new Set<string>()
  const choices: DispatchModelChoice[] = []
  for (const choice of model.choices) {
    if (choice.value === own?.value) continue
    const value = modelPart(choice.value)
    if (!launchable(value) || seen.has(value)) continue
    seen.add(value)
    const split = value !== choice.value
    choices.push({ value, label: split ? value : choice.name || value, hint: split ? undefined : choice.description || undefined })
  }
  const current = model.choices.find(c => c.value === model.current)
  // What an unset model runs: the default entry's note (Claude names the model there),
  // else the agent's current concrete choice.
  const defaultName = own ? own.description ?? "" : current ? (current.value !== modelPart(current.value) ? modelPart(current.value) : current.description || current.name) : ""
  return { choices, defaultName }
}
