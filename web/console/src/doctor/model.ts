import type { DoctorReport } from "@clawdline/contract"

export const DOCTOR_DESCRIPTION_CHARACTERS = 500
export const DOCTOR_REPORT_BYTES = 8192
const ids = ["claude", "codex", "gemini", "grok"]
const problems = ["", "no_record", "no_reading", "unreadable", "too_old", "not_signed_in", "reauth_required", "invalid_response", "unavailable", "rate_limited"]
const gaps = ["claude_auth_not_inspected", "upstream_claude_usage_not_observed", "history_only_when_quota_is_read", "browser_render_not_observed"]
const record = (v: unknown): v is Record<string, unknown> => !!v && typeof v === "object" && !Array.isArray(v)
const integer = (v: unknown) => Number.isSafeInteger(v) && Number(v) >= 0
const build = (v: unknown) => typeof v === "string" && /^[A-Za-z0-9][A-Za-z0-9.+_-]{0,95}$/.test(v)
function keys(v: Record<string, unknown>, names: string[]) { return Object.keys(v).every(k => names.includes(k)) }
function provider(v: unknown): boolean {
  if (!record(v) || !keys(v, ["id", "source", "state", "problem", "observed_at", "used_percent", "stale"]) || !ids.includes(String(v.id))) return false
  const source = v.id === "claude" ? "claude_statusline" : v.id === "codex" ? "codex_rollout" : "provider_api"
  return v.source === source && ["ok", "failed", "stale", "unknown"].includes(String(v.state)) && typeof v.stale === "boolean" &&
    (v.problem === undefined || problems.includes(String(v.problem))) && (v.observed_at === null || integer(v.observed_at)) &&
    (v.used_percent === null || typeof v.used_percent === "number" && Number.isFinite(v.used_percent) && v.used_percent >= 0 && v.used_percent <= 100)
}
/** Reject unknown fields before a downloaded report can enter the private support lane. */
export function decodeReport(text: string): DoctorReport {
  if (new TextEncoder().encode(text).length > DOCTOR_REPORT_BYTES) throw new Error("invalid_report")
  const r: unknown = JSON.parse(text)
  if (!record(r) || !keys(r, ["schema", "at", "version", "web_version", "os", "arch", "tools", "providers", "history", "coverage", "gaps"]) || r.schema !== "clawdline.doctor/1" || !integer(r.at) || !build(r.version) || (r.web_version !== undefined && !build(r.web_version)) ||
    !["darwin", "linux", "windows"].includes(String(r.os)) || !["arm64", "amd64", "386", "arm"].includes(String(r.arch)) ||
    !Array.isArray(r.tools) || r.tools.length !== 2 || !r.tools.every((t, i) => record(t) && keys(t, ["id", "state", "version"]) && t.id === ["claude", "codex"][i] &&
      ["ok", "failed", "timeout", "unrecognized", "not_found"].includes(String(t.state)) && (t.version === undefined || typeof t.version === "string" && /^\d+\.\d+\.\d+(?:[-+][A-Za-z0-9.-]+)?$/.test(t.version))) ||
    !Array.isArray(r.providers) || r.providers.length !== 4 || !r.providers.every(provider) || new Set(r.providers.map(p => p.id)).size !== 4 ||
    !Array.isArray(r.history) || r.history.length > 24 || !r.history.every(e => record(e) && keys(e, ["at", "provider"]) && integer(e.at) && provider(e.provider)) ||
    !record(r.coverage) || !keys(r.coverage, ["state", "dropped", "from", "through", "lost_unknown"]) || !["ok", "not_recorded", "unreadable", "write_failed"].includes(String(r.coverage.state)) || !integer(r.coverage.dropped) || typeof r.coverage.lost_unknown !== "boolean" ||
    (r.coverage.from !== undefined && !integer(r.coverage.from)) || (r.coverage.through !== undefined && !integer(r.coverage.through)) ||
    !Array.isArray(r.gaps) || !r.gaps.every(g => gaps.includes(String(g)))) throw new Error("invalid_report")
  return r as unknown as DoctorReport
}

export function supportBody(report: DoctorReport, description: string): string {
  if ([...description].length > DOCTOR_DESCRIPTION_CHARACTERS) throw new Error("invalid_description")
  // A second validation is intentional: edited/imported data never bypasses the whitelist.
  const safe = decodeReport(JSON.stringify(report))
  return `Clawdline Doctor\n\n${description.trim()}\n\n\`\`\`json\n${JSON.stringify(safe)}\n\`\`\``
}
