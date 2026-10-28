// @ts-expect-error -- The Node test runner imports the same source as Vite.
import { supportCall, SupportFailure } from "../support/api.ts"
// @ts-expect-error -- The Node test runner imports the same source as Vite.
import { SUPPORT_LOCALES, REQUEST_TYPES } from "../support/model.ts"
// @ts-expect-error -- The Node test runner imports the same source as Vite.
import { progressStages, type CommunityTopic, type PublicAuthor } from "./model.ts"
type RecordValue = Record<string, unknown>
const object = (v:unknown):v is RecordValue => !!v && typeof v === "object" && !Array.isArray(v)
function author(v:unknown):v is PublicAuthor {
  return object(v) && (v.display_name === null || typeof v.display_name === "string") && (v.avatar_url === null || typeof v.avatar_url === "string") &&
    Object.keys(v).every(k=>k === "display_name" || k === "avatar_url")
}
function topic(v:unknown):v is CommunityTopic {
  if (!object(v) || typeof v.id !== "string" || typeof v.title !== "string" || !REQUEST_TYPES.includes(v.request_type as never) || !author(v.author) ||
      typeof v.state !== "string" || !Number.isSafeInteger(v.revision) || typeof v.created_at !== "string" || typeof v.updated_at !== "string" ||
      !SUPPORT_LOCALES.includes(v.reply_locale as never) || typeof v.reply_count !== "number") return false
  if (v.progress !== null && (!object(v.progress) || !progressStages(v.request_type as CommunityTopic["request_type"]).includes(v.progress.stage as never))) return false
  if (v.messages !== undefined && (!Array.isArray(v.messages) || !v.messages.every(m=>object(m) && typeof m.id === "string" && ["member","agent","operator"].includes(String(m.kind)) && author(m.author) && typeof m.body === "string" && typeof m.at === "string" && Array.isArray(m.sources) && Array.isArray(m.images)))) return false
  return true
}
/** Validate public representations separately; the private support projection is never reused. */
export async function communityCall<T>(origin:string,path:string,method="GET",body?:unknown,key?:string,get:typeof fetch=fetch):Promise<T> {
  const result = await supportCall<unknown>(origin,path,method,body,key,get)
  if (!object(result) || ("topic" in result && !topic(result.topic)) || ("topics" in result && (!Array.isArray(result.topics) || !result.topics.every(topic))) ||
      ("next_cursor" in result && result.next_cursor !== null && typeof result.next_cursor !== "string")) throw new SupportFailure("support_response_unreadable",false)
  const endpoint = new URL(path,"https://schema.invalid").pathname
  if (/^\/(su\/)?v1\/community\/topics(?:\/|$)/.test(endpoint)) {
    const listing = method === "GET" && endpoint === "/v1/community/topics"
    if (listing ? !Array.isArray(result.topics) || !(result.next_cursor === null || typeof result.next_cursor === "string") : !topic(result.topic)) throw new SupportFailure("support_response_unreadable",false)
  }
  return result as T
}
