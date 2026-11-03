/** A native resume invocation opens Clawdline's picker without a terminal send. */
export function resumeCommandQuery(text: string, assistant: string | undefined, pictures = 0): string | null {
  if (pictures || (assistant !== "claude" && assistant !== "codex")) return null
  const match = /^\/resume(?:[ \t]+([^\r\n]*))?$/.exec(text.trim())
  return match ? (match[1] || "").trim() : null
}

/** Prefer the most specific registered Project containing the current directory. */
export function resumeCommandPlace<T extends { path?: string }>(places: readonly T[], cwd: string | undefined): T | null {
  if (!cwd) return null
  const clean = (path: string) => path.replaceAll("\\", "/").replace(/\/+$/, "")
  const work = clean(cwd)
  return places.filter(place => {
    if (!place.path) return false
    const path = clean(place.path)
    return work === path || work.startsWith(path + "/")
  }).sort((a, b) => clean(b.path!).length - clean(a.path!).length)[0] ?? null
}
