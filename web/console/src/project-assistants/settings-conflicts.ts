import type * as W from "@clawdline/contract"

/** The settings an editor started from, so the source can tell its changes from other writers'. */
export function settingsBase(bot:W.ProjectAssistantBot):W.ProjectAssistantEditBase {
  return {name:bot.name,appearance:bot.appearance,instructions:bot.instructions.text,agent:bot.agent,permission_policy:bot.permission_policy,
    ...(bot.mode?{mode:bot.mode}:{}),...(bot.runtime_settings?{runtime_settings:bot.runtime_settings}:{}),...(bot.login_profile?{login_profile:bot.login_profile}:{}),
    ...(bot.extensions?{extensions:bot.extensions}:{}),...(bot.fallback_agents?{fallback_agents:bot.fallback_agents}:{}),...(bot.fallback_steps?{fallback_steps:bot.fallback_steps}:{}),
    ...(bot.notify?{notify:bot.notify}:{}),...(bot.background_threads?{background_threads:bot.background_threads}:{}),...(bot.manage_team?{manage_team:true}:{})}
}

/** The fields a pa_stale_version refusal names as changed both here and elsewhere; empty when it names none. */
export function settingsConflicts(error:{code:string;field?:string}|undefined):string[] {
  if(error?.code!=="pa_stale_version"||!error.field?.startsWith("settings."))return []
  return error.field.slice("settings.".length).split(",").filter(Boolean)
}
