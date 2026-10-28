export const SUPPORT_LOCALES = ["en", "zh-Hant", "ja", "zh-Hans", "ko", "es", "pt-BR", "fr", "de"] as const
export type SupportLocale = typeof SUPPORT_LOCALES[number]
export const LANGUAGE_NAMES: Record<SupportLocale, string> = { en: "English", "zh-Hant": "繁體中文", ja: "日本語", "zh-Hans": "简体中文", ko: "한국어", es: "Español", "pt-BR": "Português", fr: "Français", de: "Deutsch" }
export const TOPICS = ["getting_started", "cloud", "agents", "account", "other"] as const
export const REQUEST_TYPES = ["question", "issue", "feature_request"] as const
export type RequestType = typeof REQUEST_TYPES[number]
export type Sources = { url: string; revision: string }[]
export interface SupportImage { id:string; mime:"image/jpeg"; bytes:number; width:number; height:number; sha256:string }
export interface Message { id: string; kind: "member" | "agent" | "operator"; body: string; locale: SupportLocale; original_language: string; at: string; sources: Sources; images?: SupportImage[] }
export interface Question { community_published?: boolean; request_type?: RequestType; reporter?: {id:string;name:string|null;avatar_url?:string|null}; id: string; title: string; topic: string; source_language: string; reply_locale: SupportLocale; state: string; revision: number; input_revision: number;
  created_at: string; updated_at: string; unread: boolean; messages?: Message[];
  work?: { state: string; attempts: number; lease_until: string | null; error: string | null };
  delivery?: { state: string; attempts: number; delivery_id: string | null; error: string | null } }
export interface Faq { id: string; title: string; body: string; topic: string; slug: string; revision: number; locale: SupportLocale; sources: Sources; updated_at: string; available_locales: SupportLocale[] }
/** Only the canonical, account-bound GitHub CDN projection may become an image request. */
export function githubAvatarSource(value: unknown): string | null {
  if (typeof value !== "string" || value.trim() !== value) return null
  const id = /^https:\/\/avatars\.githubusercontent\.com\/u\/([1-9][0-9]*)\?s=80$/.exec(value)?.[1]
  return id && Number.isSafeInteger(Number(id)) ? value : null
}
export interface FaqText { title: string; body: string; slug: string; revision: number; source_revision: number; sources: Sources; updated_at: string }
export interface Article { _id: string; revision: number; source_locale: SupportLocale; source_revision: number; topic: string; source_question_id: string | null;
  translations: Partial<Record<SupportLocale, { draft: FaqText; published: FaqText | null }>> }
export type HelpRoute = { page: "faq" | "ask" | "mine" | "thread" | "article" | "inbox" | "operator" | "editor" | "drafts" | "worker"; id: string | null }
export function helpRoute(path: string): HelpRoute {
  const parts = path.split("/").filter(Boolean)
  if (parts[1] === "ask") return { page: "ask", id: null }
  if (parts[1] === "questions") return { page: parts[2] ? "thread" : "mine", id: parts[2] ?? null }
  if (parts[1] === "faq" && parts[2]) return { page: "article", id: parts[2] }
  if (parts[1] === "admin") {
    if (parts[2] === "worker") return { page: "worker", id: null }
    if (parts[2] === "faq") return { page: parts[3] ? "editor" : "drafts", id: parts[3] ?? null }
    return { page: parts[2] === "questions" && parts[3] ? "operator" : "inbox", id: parts[3] ?? null }
  }
  return { page: "faq", id: null }
}
export function helpAccess(route: HelpRoute): "public" | "member" | "admin" {
  if (route.page === "faq" || route.page === "article") return "public"
  if (route.page === "ask" || route.page === "mine" || route.page === "thread") return "member"
  return "admin"
}
export function translationState(article: Article, locale: SupportLocale): "missing" | "stale" | "draft" | "published" {
  const row = article.translations[locale]
  if (!row) return "missing"
  if (row.draft.source_revision !== article.source_revision) return "stale"
  if (row.published?.source_revision === article.source_revision && row.published.revision === row.draft.revision) return "published"
  return "draft"
}

/** The API retains chronological order for read receipts; only the view is reversed. */
export function conversationView(messages: readonly Message[] = []): { messages: Message[]; latestReplyID: string | null } {
  const newestFirst = [...messages].reverse()
  return { messages: newestFirst, latestReplyID: newestFirst.find(message => message.kind !== "member")?.id ?? null }
}
