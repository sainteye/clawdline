import type { Message, RequestType, Sources, SupportImage, SupportLocale } from "../support/model.js"
export type PublicAuthor = { display_name: string | null; avatar_url: string | null }
export const ISSUE_STAGES = ["triage", "needs_information", "confirmed", "in_progress", "ready_for_release", "released", "declined", "duplicate"] as const
export const IDEA_STAGES = ["discussion", "accepted", "planned", "in_progress", "released", "declined", "duplicate"] as const
export type ProgressStage = typeof ISSUE_STAGES[number] | typeof IDEA_STAGES[number]
export type Progress = { stage: ProgressStage; release_version?: string | null; release_url?: string | null }
export type PublicMessage = { id: string; kind: Message["kind"]; author: PublicAuthor; body: string; at: string; locale: SupportLocale; original_language?: string; sources: Sources; images: SupportImage[] }
export type CommunityTopic = { id: string; title: string; request_type: RequestType; topic: string; state: string; revision: number; source_language: string; reply_locale: SupportLocale;
  author: PublicAuthor; progress: Progress | null; reply_count: number; created_at: string; updated_at: string; messages?: PublicMessage[];
  viewer?: { can_reply: boolean; can_resolve: boolean; can_moderate: boolean } }
export type CommunityRoute = { page: "list" | "mine" | "new" | "detail"; requestType: RequestType; id: string | null }
export function communityRoute(address: string): CommunityRoute {
  const url = new URL(address, "https://app.clawdline.com")
  const parts = url.pathname.split("/").filter(Boolean)
  const requestType = parts[1] === "ideas" ? "feature_request" : parts[1] === "questions" ? "question" : "issue"
  if (parts[1] === "topics" && parts[2]) return { page: "detail", requestType, id: parts[2] }
  if (parts[1] === "mine") return { page: "mine", requestType, id: null }
  if (parts[1] === "new") {
    const value = url.searchParams.get("request_type")
    return { page: "new", requestType: value === "question" || value === "feature_request" ? value : "issue", id: null }
  }
  return { page: "list", requestType, id: null }
}
export function typePath(type: RequestType): string {
  return "/community/" + (type === "issue" ? "issues" : type === "feature_request" ? "ideas" : "questions")
}
/** The initial post supplies context; replies alone run newest first. Never mutate API order. */
export function publicConversation(messages: readonly PublicMessage[] = []) {
  return { original: messages[0] ?? null, replies: messages.slice(1).reverse(), latestReplyID: messages.length > 1 ? messages.at(-1)!.id : null }
}
export function progressStages(type: RequestType): readonly ProgressStage[] {
  return type === "issue" ? ISSUE_STAGES : type === "feature_request" ? IDEA_STAGES : []
}
export function topicClosed(topic: Pick<CommunityTopic, "request_type" | "state" | "progress">): boolean {
  return topic.request_type === "question" ? topic.state === "resolved" : ["released", "declined", "duplicate"].includes(topic.progress?.stage ?? "")
}

/** Only retired member entry points move; public question lists keep their own route. */
export function communityEntry(address: string): string | null {
  const path = address.split("?")[0]?.replace(/\/+$/, "")
  if (path === "/help/ask" || path?.startsWith("/help/ask/")) return "/community/new?request_type=question"
  if (path === "/help/questions") return "/community/mine"
  return path === "/community" || path?.startsWith("/community/") ? address : null
}

/** Structured report sections must not become the discussion's list title. */
export function communityTitle(body: string, limit: number): string | undefined {
  return body.trim().split(/\r?\n/, 1)[0]?.replace(/^#+\s+/, "").trim().slice(0, limit) || undefined
}
