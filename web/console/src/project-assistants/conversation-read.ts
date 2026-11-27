import type * as W from "@clawdline/contract"
import {authorized, command, failure, type SourceClient} from "./client"
import {readView, type ReadScope} from "./read-views"
import {canRead, sameBot} from "./view-state"

export function canMarkConversationRead(source: W.ProjectAssistantSourceStatus) {
  return canRead(source) && source.state === "online"
}

export async function markConversationRead(source: SourceClient, current: () => readonly SourceClient[],
  scope: ReadScope, readerID: string, signal: AbortSignal, observedSeq?: number) {
  let seq = observedSeq
  if (seq === undefined) {
    // An explicit roster action covers messages beyond any loaded chat window.
    const request: W.ProjectAssistantReadViewRequest = {
      key: scope.key, conversation_id: scope.conversation_id, mode: "snapshot",
      source_epoch: "", cursor: "", after_seq: 0, page_size: 1,
    }
    const answer = await authorized(source, current, s => source.view(request, s), signal)
    const page = readView(answer, request, scope)
    if (!canRead(page.source)) throw failure("pa_revoked")
    if (!canMarkConversationRead(page.source)) throw failure("pa_offline")
    seq = page.head.conversation.last_message_seq
  }
  const request: W.ProjectAssistantReadRequest = {
    command: command(scope.key, "read"),
    cursor: {key: scope.key, conversation_id: scope.conversation_id, message_seq: seq, reader_id: readerID},
  }
  const {cursor} = await authorized(source, current, s => source.markRead(request, s), signal)
  // The transport binds the reader. A source may return its own opaque reader ID.
  if (!cursor?.key || !sameBot(cursor.key, scope.key) || cursor.conversation_id !== scope.conversation_id
    || !Number.isSafeInteger(cursor.message_seq) || cursor.message_seq < seq) throw failure("pa_scope_denied")
  return cursor
}

/** Whether reading the chat would clear anything: a count or the grey dot. */
export function hasUnreadNews(item:W.ProjectAssistantBotSummary):boolean {
  return item.unread_count>0||item.has_activity===true
}
