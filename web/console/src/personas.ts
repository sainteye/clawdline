import type { Persona, PersonaCatalog } from "@clawdline/contract"

/**
 * The machine's persona catalog (docs/personas.md), read once per page.
 *
 * A console shows one machine for its whole life (`cloud/install.ts`), so one
 * read is one read per machine. The catalog is compiled into the daemon and
 * does not change while it runs.
 *
 * A read that fails for any reason — an older daemon without the route, a
 * machine on Clawdline Cloud that does not list `personas` among its commands,
 * a dropped connection — is an empty catalog: no role chips and no bots, never
 * a sheet that breaks. Nothing here says a persona the machine did not name.
 */

let catalog: Persona[] | null = null
let reading: Promise<Persona[]> | null = null
const listeners = new Set<() => void>()

function wellFormed(p: unknown): p is Persona {
  const q = p as Persona | null
  return !!q && typeof q.id === "string" && !!q.id && !!q.name && typeof q.name.en === "string" && !!q.icon
}

/** Read the catalog, once; every later call answers from the first read. */
export function loadPersonas(read: typeof fetch = (...args) => fetch(...args)): Promise<Persona[]> {
  if (reading) return reading
  reading = (async () => {
    try {
      const res = await read("/v1/personas", { credentials: "same-origin" })
      if (!res.ok) return []
      const body = (await res.json()) as PersonaCatalog | null
      return Array.isArray(body?.personas) ? body.personas.filter(wellFormed).map(withTeams) : []
    } catch {
      // refusal-ok: a missing catalog is shown as no roles to pick, not as a failure.
      return []
    }
  })().then((list) => {
    catalog = list
    listeners.forEach((fn) => fn())
    return list
  })
  return reading
}

/** The catalog if it has been read, or null while it has not. */
export function personasNow(): Persona[] | null {
  return catalog
}

/** Called once the catalog arrives. */
export function onPersonas(fn: () => void): () => void {
  listeners.add(fn)
  return () => {
    listeners.delete(fn)
  }
}

/** The persona a session row or an assignment names, when the catalog has it. */
export function personaById(list: readonly Persona[] | null, id: string | null | undefined): Persona | null {
  if (!id || !list) return null
  return list.find((p) => p.id === id) ?? null
}

function language(): "en" | "zh-Hant" {
  const lang =
    (typeof document !== "undefined" && document.documentElement.lang) ||
    (typeof navigator !== "undefined" && navigator.language) ||
    "en"
  return lang.toLowerCase().startsWith("zh") ? "zh-Hant" : "en"
}

/** The persona's name in the page's language. */
export function personaName(p: Persona): string {
  return p.name[language()] || p.name.en || p.id
}

/** The persona's one-sentence summary in the page's language; "" when it has none. */
export function personaSummary(p: Persona): string {
  return p.summary?.[language()] || p.summary?.en || ""
}

/** Name and one-sentence summary, for a title attribute or an accessible label. */
export function personaTitle(p: Persona): string {
  const summary = personaSummary(p)
  return summary ? personaName(p) + " — " + summary : personaName(p)
}

/** What the Session detail header says about a row's persona. */
export interface HeadPersona {
  persona: Persona
  name: string
  summary: string
  title: string
}

/**
 * The persona the Session detail header shows, or null for none: a row with no
 * persona, a catalog not read yet or unreadable, and a name the catalog does
 * not have all show nothing. The header never guesses a role from an id.
 */
export function headPersona(list: readonly Persona[] | null, id: string | null | undefined): HeadPersona | null {
  const persona = personaById(list, id)
  if (!persona) return null
  return { persona, name: personaName(persona), summary: personaSummary(persona), title: personaTitle(persona) }
}

/**
 * What a session row shows for the role it was launched as: the bot at the
 * head of its second line, with name plus summary for its title and accessible
 * name. A row with no persona, or one whose id this machine's catalog does not
 * name, draws nothing there.
 */
export function rowPersonaLine(
  list: readonly Persona[] | null,
  id: string | null | undefined,
): { persona: Persona; name: string; title: string } | null {
  const persona = personaById(list, id)
  return persona ? { persona, name: personaName(persona), title: personaTitle(persona) } : null
}

/**
 * The persona a Board item of this kind is offered first: the first catalog
 * entry whose `suggested_kinds` names the kind, and only when exactly one does.
 * A kind two personas suggest (feature: backend and frontend) is the person's
 * to choose, so it gets none.
 */
export function suggestedPersona(list: readonly Persona[] | null, kind: string): Persona | null {
  const matches = (list ?? []).filter((p) => (p.suggested_kinds ?? []).includes(kind))
  return matches.length === 1 ? matches[0] : null
}

export interface PersonaSuggestion {
  persona: Persona
  /** The item words that explain a content suggestion, in matching priority. */
  signals: string[]
  source: "content" | "kind"
}

/**
 * Distinctive, user-facing words for the closed persona catalog. These are not
 * a classifier hidden on a service: the Board can explain every match from
 * the item text, works offline, and sends no work content away from the
 * machine. Broad words such as "feature", "fix" and "data" are deliberately
 * absent because they would turn a weak overlap into a confident-looking
 * guess.
 */
const PERSONA_SIGNALS: Readonly<Record<string, readonly string[]>> = {
  accessibility: ["wcag", "screen reader", "螢幕報讀器", "無障礙", "a11y"],
  "ai-search": ["ai search", "answer engine", "llm visibility", "ai 搜尋", "答案引擎", "結構化資料"],
  analytics: ["analytics", "funnel", "dashboard", "數據分析", "資料分析", "轉換率", "指標"],
  "api-tester": ["api test", "api contract", "postman", "契約測試", "端點測試"],
  architect: ["epic", "system architecture", "架構規劃", "系統架構", "技術選型"],
  backend: ["backend", "daemon", "api", "endpoint", "database", "migration", "後端", "端點", "資料庫", "遷移"],
  "brand-guardian": ["brand", "logo", "typography", "品牌", "標誌", "字體", "視覺識別"],
  "code-reviewer": ["code review", "review diff", "pull request review", "程式碼審查", "審查 diff"],
  "content-writer": ["article", "blog post", "content draft", "文章", "部落格", "內容草稿"],
  "customer-success": ["customer success", "onboarding", "renewal", "客戶成功", "客戶導入", "續約"],
  devops: ["ci/cd", "pipeline", "build automation", "deploy automation", "devops", "建置自動化", "部署自動化"],
  devrel: ["quickstart", "developer tutorial", "sample app", "changelog", "快速入門", "開發者教學", "更新紀錄"],
  email: ["newsletter", "email campaign", "email sequence", "電子報", "系列信", "退訂"],
  "evidence-collector": ["evidence", "screenshot proof", "驗收證據", "蒐集證據", "截圖證明"],
  "feedback-synthesizer": ["feedback synthesis", "feedback themes", "回饋整理", "意見彙整", "訪談整理"],
  finops: ["finops", "cloud cost", "cost allocation", "雲端成本", "成本歸屬", "帳單優化"],
  frontend: ["frontend", "react", "web console", "responsive", "phone width", "前端", "網頁主控台", "響應式", "手機版"],
  growth: ["growth experiment", "activation", "retention", "成長實驗", "啟用率", "留存率"],
  "image-prompt": ["image prompt", "image generation", "midjourney", "圖像提示", "圖片生成", "生圖"],
  "incident-commander": ["active incident", "incident commander", "outage", "事故指揮", "服務中斷", "重大事故"],
  instagram: ["instagram", "ig post", "carousel post", "ig 貼文", "輪播貼文"],
  "minimal-change": ["minimal change", "smallest fix", "最小改動", "最小修正"],
  performance: ["benchmark", "latency", "throughput", "load test", "效能量測", "延遲", "吞吐量", "負載測試"],
  pr: ["press release", "media list", "public announcement", "新聞稿", "媒體名單", "對外公告"],
  pricing: ["pricing", "packaging", "price plan", "定價", "價格方案", "方案組合"],
  privacy: ["privacy", "personal data", "consent", "retention policy", "隱私", "個人資料", "同意管理", "保存期限"],
  "product-manager": ["product requirement", "acceptance criteria", "scope alignment", "產品需求", "驗收條件", "範圍管理"],
  "reality-checker": ["verify claim", "reality check", "實際驗證", "查證說法", "尚未證實"],
  secrets: ["secret rotation", "credential", "api key", "密鑰", "憑證輪替", "金鑰外洩"],
  security: ["security", "authentication", "authorization", "threat model", "資安", "驗證授權", "威脅模型", "信任邊界"],
  seo: ["seo", "search engine", "schema.org", "搜尋引擎", "搜尋排名", "結構化標記"],
  "social-media": ["social media", "posting calendar", "社群媒體", "發文行事曆", "社群貼文"],
  "sprint-prioritizer": ["sprint planning", "backlog priority", "sprint 排序", "待辦排序", "優先順序"],
  sre: ["sre", "slo", "error budget", "reliability", "可靠性", "錯誤預算", "服務水準"],
  support: ["support reply", "help article", "customer ticket", "客服回覆", "說明文章", "客訴"],
  "technical-writer": ["documentation", "technical writing", "reference guide", "技術文件", "操作指南", "參考文件"],
  "test-automation": ["test automation", "e2e test", "flaky test", "自動化測試", "端對端測試", "不穩定測試"],
  "trend-researcher": ["trend research", "market trend", "technology trend", "趨勢研究", "市場趨勢", "技術趨勢"],
  "ui-designer": ["ui design", "design token", "visual design", "ui 設計", "設計 token", "視覺設計"],
  "ui-finish-gate": ["ui polish", "visual qa", "pixel perfect", "ui 上線把關", "畫面驗收", "像素級"],
  "ux-architect": ["ux flow", "information architecture", "wireframe", "user journey", "操作流程", "資訊架構", "使用者歷程", "看板流程"],
  "ux-researcher": ["ux research", "usability research", "user interview", "ux 研究", "可用性研究", "使用者訪談"],
}

function normalizedWords(value: string): string {
  return value.normalize("NFKC").toLocaleLowerCase()
}

function hasSignal(text: string, value: string): boolean {
  const signal = normalizedWords(value)
  const asciiWord = (character: string) => /[a-z0-9]/.test(character)
  for (let at = text.indexOf(signal); at >= 0; at = text.indexOf(signal, at + 1)) {
    const left = !asciiWord(signal[0] ?? "") || at === 0 || !asciiWord(text[at - 1] ?? "")
    const end = at + signal.length
    const right = !asciiWord(signal.at(-1) ?? "") || end === text.length || !asciiWord(text[end] ?? "")
    if (left && right) return true
  }
  return false
}

/**
 * Suggest a role from the item's own words, falling back to the old kind-only
 * rule when content has no distinctive signal. An exact score tie is
 * deliberately no content answer: ambiguity stays visible instead of being
 * resolved by catalog order.
 */
export function suggestedPersonaForItem(
  list: readonly Persona[] | null,
  item: { kind: string; title: string; description: string },
): PersonaSuggestion | null {
  const text = normalizedWords(`${item.title}\n${item.description}`)
  const scored = (list ?? []).map((persona) => {
    const signals = (PERSONA_SIGNALS[persona.id] ?? []).filter((signal) => hasSignal(text, signal))
    const score = signals.reduce((total, signal) => total + 10 + normalizedWords(signal).length, 0)
    return { persona, signals, score }
  }).filter((row) => row.score > 0).sort((a, b) => b.score - a.score)

  if (scored.length && (scored.length === 1 || scored[0].score > scored[1].score)) {
    return { persona: scored[0].persona, signals: scored[0].signals.slice(0, 3), source: "content" }
  }
  const byKind = suggestedPersona(list, item.kind)
  return byKind ? { persona: byKind, signals: [], source: "kind" } : null
}

const REMEMBERED = "clawdline.start.persona"

/** The start sheet's last choice in this browser; "" for none. */
export function rememberedPersona(): string {
  try {
    return localStorage.getItem(REMEMBERED) || ""
  } catch {
    // refusal-ok: a browser without storage just does not remember.
    return ""
  }
}

export function rememberPersona(id: string): void {
  try {
    if (id) localStorage.setItem(REMEMBERED, id)
    else localStorage.removeItem(REMEMBERED)
  } catch {
    // refusal-ok: a browser without storage just does not remember.
  }
}

/**
 * The teams a persona can belong to (`teams` in personas.schema.json), in the
 * order the role row's switcher lists them. The console names them
 * (`personaTeam*` in next-strings.ts); the catalog only says which.
 */
export const PERSONA_TEAMS = ["engineering", "marketing", "product", "quality", "operations", "design", "business"] as const
export type PersonaTeam = (typeof PERSONA_TEAMS)[number]

function knownTeam(team: unknown): team is PersonaTeam {
  return (PERSONA_TEAMS as readonly unknown[]).includes(team)
}

/**
 * A persona's teams, never empty. The daemon sends `teams`; one from before
 * that field sends a single `team` (read as a list of one) or, older still,
 * neither, which is engineering: every persona such a daemon has. Names this
 * console does not know are dropped, and a persona left with none is
 * engineering too.
 */
export function personaTeams(p: Persona): PersonaTeam[] {
  const raw = p as { teams?: unknown; team?: unknown }
  const listed = Array.isArray(raw.teams) ? raw.teams : raw.team !== undefined ? [raw.team] : []
  const teams = PERSONA_TEAMS.filter((t) => listed.includes(t))
  return teams.length ? teams : ["engineering"]
}

/** A catalog entry with `teams` filled in from whatever the daemon sent. */
export function withTeams(p: Persona): Persona {
  return { ...p, teams: personaTeams(p) }
}

function inTeam(p: Persona, team: string): boolean {
  return (personaTeams(p) as string[]).includes(team)
}

/** The teams that have at least one persona, in switcher order. One or none hides the switcher. */
export function teamsOffered(list: readonly Persona[] | null): PersonaTeam[] {
  const present = new Set((list ?? []).flatMap(personaTeams))
  return PERSONA_TEAMS.filter((t) => present.has(t))
}

/** The personas the chips show for one team, in catalog order. */
export function personasOfTeam(list: readonly Persona[] | null, team: PersonaTeam): Persona[] {
  return (list ?? []).filter((p) => inTeam(p, team))
}

/**
 * The team the role row shows. A chosen persona is never hidden behind a team
 * that does not hold it: the team last picked in this browser when it holds
 * the persona, otherwise the persona's first team in switcher order. With no
 * persona chosen, the remembered team when it still has personas; otherwise
 * engineering (or the first team there is).
 */
export function shownTeam(
  list: readonly Persona[] | null,
  chosen: string | null | undefined,
  preferred: string | null | undefined,
): PersonaTeam {
  const persona = personaById(list, chosen)
  if (persona) return preferred && knownTeam(preferred) && inTeam(persona, preferred) ? preferred : personaTeams(persona)[0]
  const offered = teamsOffered(list)
  if (preferred && (offered as string[]).includes(preferred)) return preferred as PersonaTeam
  return offered.includes("engineering") || !offered.length ? "engineering" : offered[0]
}

/**
 * Switching team: the chosen persona stays when the new team holds it too;
 * otherwise the choice becomes no role, so a start never sends a persona the
 * person cannot see.
 */
export function switchTeam(
  list: readonly Persona[] | null,
  chosen: string | null | undefined,
  team: PersonaTeam,
): { team: PersonaTeam; chosen: string } {
  const persona = personaById(list, chosen)
  return { team, chosen: persona && inTeam(persona, team) ? persona.id : "" }
}

const REMEMBERED_TEAM = "clawdline.persona.team"

/** The team last picked in this browser; "" for none. */
export function rememberedTeam(): string {
  try {
    return localStorage.getItem(REMEMBERED_TEAM) || ""
  } catch {
    // refusal-ok: a browser without storage just does not remember.
    return ""
  }
}

export function rememberTeam(team: PersonaTeam): void {
  try {
    localStorage.setItem(REMEMBERED_TEAM, team)
  } catch {
    // refusal-ok: a browser without storage just does not remember.
  }
}
