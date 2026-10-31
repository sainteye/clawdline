export type Names = Record<string, string>
export interface Category { id: string; name: Names }
export interface Skill { id: string; native_name: string; name: Names; purpose: Names; path: string; mode: string; requirements: Names[]; source_url: string; sha256?: string }
export interface Evidence { url: string; kind: string; rank?: number | null; scope?: string; snapshot_at?: string; observed_at?: string; note?: Names }
export interface Project {
  id: string; repo_url: string; owner: string; repo: string; name: Names; summary: Names; category_id: string; category: Names
  recommendation?: Names; recommendation_tier?: string; icon: { url: string; kind: string; source_url: string; asset?: string }
  source_commit: string; license: string | null; skills: Skill[]; evidence?: Evidence[]; integration_kind?: string; limitations?: Names[]
}
export interface Library { version: number; saved: Project[]; catalog: { categories: Category[]; projects: Project[]; researched_at: string } }
export interface Prepared { project: Project; skill: Skill | null; task: string; prompt: string; mode: string }
export interface ToolAttachment { id: string; target: string; title: string; prompt: string; revision: string; mode: string }
export interface ToolDraft { attachment: ToolAttachment; text: string }

export function localized(value: Names | undefined, locale: string): string {
  return value?.[locale.startsWith("zh-Hant") || locale === "zh-TW" ? "zh-Hant" : locale] || value?.en || ""
}
export function filterProjects(projects: readonly Project[], category: string, query: string, locale: string): Project[] {
  const q = query.trim().toLocaleLowerCase()
  return projects.filter(p => (category === "all" || p.category_id === category) && (!q ||
    [p.owner, p.repo, p.repo_url, ...Object.values(p.name), ...Object.values(p.summary), localized(p.category, locale),
      ...p.skills.flatMap(s => [s.native_name, s.path, ...Object.values(s.name), ...Object.values(s.purpose)])].join("\n").toLocaleLowerCase().includes(q)))
}
export function repositoryURL(raw: string): { owner: string; repo: string; url: string } | null {
  try {
    const u = new URL(raw.trim())
    if (u.protocol !== "https:" || u.host !== "github.com" || u.username || u.password || u.search || u.hash) return null
    const parts = u.pathname.replace(/\/$/, "").split("/")
    if (parts.length !== 3 || !parts.slice(1).every(x => /^[A-Za-z0-9][A-Za-z0-9_.-]*$/.test(x))) return null
    const repo = parts[2].replace(/\.git$/, "")
    if (!repo) return null
    return { owner: parts[1], repo, url: `https://github.com/${parts[1]}/${repo}` }
  } catch { return null }
}
export function toolTarget(row: { id: string; sessionId?: string; execution_generation?: string; machine?: string } | null, machine = ""): string {
  return row?.sessionId ? JSON.stringify([machine || row.machine || "", row.id, row.sessionId, row.execution_generation || ""]) : ""
}

// A draft keeps its instruction attachment in the existing durable text store.
// Plain drafts are byte-for-byte unchanged. A reused execution keeps the text
// and attachment visible, but submission checks the exact target again.
const prefix = "CLAWDLINE_TOOL_DRAFT_V1\n"
export function encodeToolDraft(text: string, attachment: ToolAttachment | null): string {
  return attachment ? prefix + JSON.stringify({ text, attachment }) : text
}
export function decodeToolDraft(raw: string): { text: string; attachment: ToolAttachment | null } {
  if (raw.startsWith(prefix)) {
    try {
      const d = JSON.parse(raw.slice(prefix.length)) as ToolDraft
      if (typeof d.text === "string" && d.attachment && ["id", "target", "title", "prompt", "revision", "mode"].every(k => typeof (d.attachment as unknown as Record<string, unknown>)[k] === "string"))
        return { text: d.text, attachment: d.attachment }
    } catch { /* A person's literal text is never discarded as broken metadata. */ }
  }
  return { text: raw, attachment: null }
}
export function toolMessage(text: string, attachment: ToolAttachment | null, target: string): string {
  if (!attachment) return text
  if (!target || attachment.target !== target) throw new Error("stale_tool_target")
  return attachment.prompt + "\n\nTask requested by the user / 使用者的任務：\n" + text
}

// Repository metadata is data; never turn a stored arbitrary scheme into a link.
export function sourceLink(value: string | undefined): string | undefined {
  try { const url = new URL(value || ""); return url.protocol === "https:" && !url.username && !url.password ? url.href : undefined } catch { return undefined }
}
