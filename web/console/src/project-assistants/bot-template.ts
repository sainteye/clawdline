import type * as W from "@clawdline/contract"
// @ts-expect-error Node's strip-types runner loads source extensions.
import { failure } from "./client.ts"
// @ts-expect-error Node's strip-types runner loads source extensions.
import { readBotMetadata, readSource, type ReadScope } from "./read-views.ts"
// @ts-expect-error Node's strip-types runner loads source extensions.
import { canRead, sameBot } from "./view-state.ts"

/** This renderer-owned format is a settings reference, not an import contract. */
export function encodeBotTemplate(bot: W.ProjectAssistantBot): string {
  const settings: Record<string, unknown> = {
    name: bot.name,
    appearance: {shape: bot.appearance.shape, color: bot.appearance.color},
    agent: bot.agent,
    instructions: bot.instructions.text,
    permission_policy: bot.permission_policy,
  }
  if (bot.mode != null) settings.mode = bot.mode
  if (bot.notify != null) settings.notify = bot.notify
  if (bot.runtime_settings != null) {
    const options: Record<string, string> = {}
    // Native option IDs outside these portable controls require source setup.
    for (const key of ["reasoning_effort", "reasoning-effort", "effort", "thought_level", "fast", "fast-mode"]) {
      const value = bot.runtime_settings.options?.[key]
      if (value != null) {
        if (typeof value !== "string") throw failure("pa_invalid")
        options[key] = value
      }
    }
    if (typeof bot.runtime_settings.model !== "string") throw failure("pa_invalid")
    settings.runtime_settings = {model: bot.runtime_settings.model, options}
  }
  return JSON.stringify(sorted({format: "clawdline.bot-template.v1", settings}), null, 2)
}

function sorted(value: unknown): unknown {
  if (typeof value !== "object" || value === null) return value
  return Object.fromEntries(Object.entries(value).filter(([,v]) => v != null)
    .sort(([a],[b]) => a < b ? -1 : a > b ? 1 : 0).map(([k,v]) => [k,sorted(v)]))
}

/** A list summary is never sufficient authority for a portable snapshot. */
export function readableTemplate(answer: W.ProjectAssistantGetResponse, scope: ReadScope): string {
  if (!answer || answer.contract_version !== "project-assistants.v1") throw failure("pa_invalid")
  const source = readSource(answer.source, scope.key.machine_id)
  if (!canRead(source)) throw failure("pa_scope_denied")
  if (!source.fresh || source.state !== "online") throw failure("pa_offline")
  readBotMetadata(answer.bot, scope)
  const c = answer.conversation
  if (!c || c.id !== scope.conversation_id || !c.key || !sameBot(c.key, scope.key)) throw failure("pa_scope_denied")
  if (answer.bot.mode != null && typeof answer.bot.mode !== "string" || answer.bot.notify != null && !["on","off"].includes(answer.bot.notify)) throw failure("pa_invalid")
  return encodeBotTemplate(answer.bot)
}
