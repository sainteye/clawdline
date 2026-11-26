/**
 * A machine whose daemon has no Bots (an older version answers unsupported)
 * or has them turned off is not a failure to read it: the roster leaves it
 * out instead of showing a line with an error code. Any other refusal stays.
 */
export function shownSourceError(code: string | undefined): boolean {
  return !!code && code !== "pa_unsupported" && code !== "unsupported" && code !== "pa_disabled"
}

/**
 * Why a Bot cannot be changed from this screen, in the person's words, or ""
 * when it can: a settings form that is only dimmed says nothing.
 */
export function readOnlyReason(source: { state: string; fresh: boolean; writable: boolean; access: string } | undefined, w: Record<string, string>): string {
  if (!source) return w.loading ?? ""
  if (source.state === "offline") return w.sourceOffline ?? w.unavailable ?? ""
  if (source.state !== "online" || !source.fresh) return w.unavailable ?? ""
  if (source.access !== "interactive") return w[source.access] ?? source.access
  if (!source.writable) return w.readOnlySource ?? ""
  return ""
}

/**
 * The workspace's readable name. Cloud may not have listed the source's
 * Projects yet; its raw ID is never a name, so the fallback word stands in.
 */
export function botWorkspaceName(name: string | undefined, w: { unnamedProject: string }): string {
  return name?.trim() ? name : w.unnamedProject
}

/** Why a source has not offered a capability (see SourceClient.capabilityGap). */
export type CapabilityGap = "pending" | "offline" | "outdated"

/**
 * The hint beside a setting the source cannot offer. Only a source known to
 * be older is asked to update; one that is offline or has not yet reported
 * its capabilities says so.
 */
export function capabilityHint(source: { capabilityGap?(): CapabilityGap } | undefined, update: string, w: { capabilitiesPending: string; sourceOffline: string }): string {
  const gap = source?.capabilityGap?.()
  if (gap === "pending") return w.capabilitiesPending
  if (gap === "offline") return w.sourceOffline
  return update
}

/**
 * Why the Bot settings form is read-only, or "" while it can be edited. A
 * source still describing itself, or offline, says that before the generic
 * reasons.
 */
export function settingsReadOnlyReason(enabled: boolean, source: { capabilityGap?(): CapabilityGap } | undefined, recoveryRequired: boolean,
  status: Parameters<typeof readOnlyReason>[0], w: Record<string, string>): string {
  if (enabled) return ""
  return capabilityHint(source, "", { capabilitiesPending: w.capabilitiesPending ?? "", sourceOffline: w.sourceOffline ?? "" }) ||
    (recoveryRequired ? w.unavailable ?? "" : readOnlyReason(status, w))
}

/**
 * What went wrong with a Bot action and what to do, in the person's words;
 * the code itself is shown beside it in small print.
 */
export function botFailureText(code: string, w: Record<string, string>): string {
  if (code === "pa_stale_version" || code === "pa_stale_generation" || code === "pa_revoked") return w.failRetry
  if (code === "pa_offline" || code === "unavailable") return w.failOffline
  if (code === "pa_read_only") return w.failCloudOff
  if (code === "pa_disabled") return w.failBotsOff
  if (code === "pa_invalid") return w.failInvalid
  if (code === "pa_capacity_full") return w.failFull
  if (code === "pa_scope_denied" || code === "pa_not_found") return w.failProject
  if (code === "pa_outcome_unknown" || code === "execution_effect_unknown") return w.failUnknown
  // refusal-ok: an unrecognised code still shows beside this sentence in small print.
  return w.failGeneric
}

export function providerSetupFailure(field: string | undefined, w: Record<string,string>): string | undefined {
  switch (field) {
    case "gemini_cli": return w.geminiInstall
    case "gemini_login": return w.geminiLogin
    case "gemini_settings": return w.geminiSettings
    case "grok_cli": return w.grokInstall
    case "grok_login":
    case "grok_auth_mode_unsupported": return w.grokLogin
    case "grok_account_restricted": return w.grokAccountRestricted
  }
}

export function botTurnReason(field:string|undefined,w:Record<string,string>):string|undefined {
  const fields:Record<string,string>={
    prompt_timeout:"failTimeout",prompt_stopped:"failStopped",acp_eof:"failConnection",
    native_update:"failNativeUpdate",prompt_result:"failResponse",prompt_max_tokens:"failOutputLimit",prompt_max_turn_requests:"failStepLimit",
    owned_cleanup:"failCleanup",owned_cleanup_timeout:"failCleanupTimeout",restart:"failRestart",restart_confirmed:"failRestart",acknowledged:"failAcknowledged",
    provider_usage_limit:"failUsage",provider_out_of_credits:"failCredits",provider_rate_limited:"failRate",
    provider_overloaded:"failOverloaded",provider_signed_out:"failSignedOut",provider_model_missing:"failModel",
  }
  return providerSetupFailure(field,w)??w[fields[field??""]]
}

/** A terminal turn explains the interruption even when its effects are unknown. */
export function botTurnOutcome(turn:{state:string;failure_code:string;failure_field?:string},w:Record<string,string>):string {
  if(!turn.failure_code)return w[turn.state]??w.failed
  const reason=botTurnReason(turn.failure_field,w)
  if(turn.state==="unknown")return [reason,w.turnUnknownNext].filter(Boolean).join(" ")
  return reason||botFailureText(turn.failure_code,w)
}

/** Permanent authorization and protocol refusals need intervention, not reconnect loops. */
export function retryableSourceError(code:string):boolean {
  return !!code&&!(["pa_revoked","pa_scope_denied","pa_unpaired","pa_invalid","pa_unsupported","unsupported","pa_disabled"]).includes(code)
}
