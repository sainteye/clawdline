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
