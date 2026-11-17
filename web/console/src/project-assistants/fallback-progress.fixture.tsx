// Server rendering of the production component from authenticated stored facts.
// This proves rendered copy/data flow, not browser layout or native acceptance.
import React from "react"
import {renderToStaticMarkup} from "react-dom/server"
import type * as W from "@clawdline/contract"
import {FallbackProgress} from "./FallbackProgress"
import {readView} from "./read-views"
import {words} from "./words"

export function renderStoredProgress(page:W.ProjectAssistantReadViewResponse,language:string) {
  const q:W.ProjectAssistantReadViewRequest={key:page.head.key,conversation_id:page.head.conversation.id,mode:"snapshot",source_epoch:"",cursor:"",after_seq:0,page_size:100}
  const scope={key:page.head.key,project:page.head.project,conversation_id:page.head.conversation.id}
  const data=readView(page,q,scope),snapshot=data.snapshot!
  const turn=snapshot.pending_turns.find(t=>t.id===data.head.conversation.active_turn_id)??snapshot.last_turn
  return renderToStaticMarkup(<FallbackProgress primary={snapshot.bot.agent} turn={turn} w={words(language)}/>)
}
