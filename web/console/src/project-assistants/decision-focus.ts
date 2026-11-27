import type * as W from "@clawdline/contract"
import {botFromFragment} from "./client"
import {permissionState} from "./view-state"

/** The question a push about a Bot's decision points at; "" for any other link. */
export function decisionFromFragment(hash:string):string {
  if(!botFromFragment(hash))return ""
  const ids=new URLSearchParams(hash.replace(/^#/,"")).getAll("decision")
  return ids.length===1?ids[0]:""
}

/** The Bot message a push about it points at; "" for any other link. */
export function messageFromFragment(hash:string):string {
  if(!botFromFragment(hash))return ""
  const ids=new URLSearchParams(hash.replace(/^#/,"")).getAll("message")
  return ids.length===1?ids[0]:""
}

export type DecisionFocus="wait"|"scroll"|"handled"

/**
 * What the chat does with the decision a push opened it at: wait until the
 * permissions have been read, scroll to a card still waiting for the person,
 * or say the question was already handled rather than scroll to nothing.
 */
export function decisionFocus(id:string,cards:readonly {summary:W.ProjectAssistantReadPermissionSummary}[],loaded:boolean,now=Date.now()):DecisionFocus {
  const card=cards.find(c=>c.summary.id===id)
  if(card)return permissionState(card.summary,now)==="pending"?"scroll":"handled"
  return loaded?"handled":"wait"
}

export type DecisionFollowUp="working"|"done"

/**
 * The Bot's progress on the turn a reply from this page started. A turn the
 * page has not observed yet keeps what was last known (working right after
 * the reply was accepted); a failed or cancelled turn claims nothing here,
 * because the chat already shows the interruption.
 */
export function decisionFollowUp(turnID:string,turns:readonly W.ProjectAssistantReadTurnSummary[],previous:DecisionFollowUp|undefined):DecisionFollowUp|undefined {
  if(!turnID)return undefined
  const turn=turns.find(t=>t.id===turnID)
  if(!turn)return previous??"working"
  switch(turn.state){
    case "accepted":case "queued":case "running":case "waiting_permission":return "working"
    case "succeeded":return "done"
    default:return undefined
  }
}
