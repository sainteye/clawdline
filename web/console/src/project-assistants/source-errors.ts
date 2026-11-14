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
  if (source.state !== "online") return w[source.state] ?? source.state
  if (!source.fresh) return w.stale ?? ""
  if (source.access !== "interactive") return w[source.access] ?? source.access
  if (!source.writable) return w.readOnlySource ?? ""
  return ""
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
