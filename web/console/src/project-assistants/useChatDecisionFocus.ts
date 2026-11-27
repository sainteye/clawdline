import { useEffect, useState, type RefObject } from "react"
import type * as W from "@clawdline/contract"
import { isMemoryProposal } from "./DecisionCard"
import { decisionFocus } from "./decision-focus"
import { revealInChat } from "./message-focus"
import { currentPermission, permissionState } from "./view-state"

export function useChatDecisionFocus({focusDecision,onDecisionFocused,entryActivity,cards,head,ready,exchangeDecisionIds,scroll,memoryProposals,followingRef,setFollowing,openMemory}:{
  focusDecision?:string
  onDecisionFocused?:()=>void
  entryActivity:string
  cards:readonly {summary:W.ProjectAssistantReadPermissionSummary;offer?:W.ProjectAssistantPermissionOffer}[]
  head:W.ProjectAssistantReadViewHead|null
  ready:boolean
  exchangeDecisionIds:ReadonlySet<string>
  scroll:RefObject<HTMLDivElement|null>
  memoryProposals:RefObject<HTMLElement|null>
  followingRef:RefObject<boolean>
  setFollowing:(value:boolean)=>void
  openMemory?:()=>void
}) {
  const [target,setTarget]=useState(focusDecision??"")
  const [focusedDecision,setFocusedDecision]=useState("")
  const [decisionHandled,setDecisionHandled]=useState(false)
  const [rosterQuestion,setRosterQuestion]=useState(()=>entryActivity.startsWith("Asks: ")?entryActivity.slice(6):"")
  useEffect(()=>{if(focusDecision)setTarget(focusDecision)},[focusDecision])

  const outcome=target?decisionFocus(target,cards,ready):"wait"
  const targetCard=cards.find(card=>card.summary.id===target)
  const targetIsMemory=!!targetCard&&isMemoryProposal(targetCard.summary,targetCard.offer)
  const rosterTarget=rosterQuestion?cards.find(({summary,offer})=>summary.tool_kind==="decision"&&!isMemoryProposal(summary,offer)
    &&currentPermission(summary,head)&&permissionState(summary)==="pending"&&summary.title?.split("\n",1)[0].trim()===rosterQuestion)?.summary.id:undefined
  useEffect(()=>{
    if(target){
      if(outcome==="wait")return
      setRosterQuestion("");setTarget("");onDecisionFocused?.()
      if(outcome==="handled")setDecisionHandled(true)
      else {
        if(targetIsMemory)openMemory?.()
        setFocusedDecision(target)
      }
    }else if(rosterTarget){
      setRosterQuestion("");setFocusedDecision(rosterTarget)
    }
  },[target,outcome,rosterTarget,targetIsMemory])
  const underExchange=exchangeDecisionIds.has(focusedDecision)
  const focusedCard=cards.find(card=>card.summary.id===focusedDecision)
  const detailLoaded=!!focusedCard?.offer
  const focusedMemory=!!focusedCard&&isMemoryProposal(focusedCard.summary,focusedCard.offer)
  useEffect(()=>{
    if(!focusedDecision)return
    const selector=`#pa-decision-${CSS.escape(focusedDecision)}`
    const frame=requestAnimationFrame(()=>{
      if(focusedMemory){
        const card=memoryProposals.current?.querySelector<HTMLElement>(selector)
        card?.scrollIntoView({block:"nearest"})
        card?.focus({preventScroll:true})
        return
      }
      const viewport=scroll.current
      const card=viewport?.querySelector<HTMLElement>(selector)
      if(!viewport||!card){setFocusedDecision("");setDecisionHandled(true);return}
      followingRef.current=false;setFollowing(false)
      revealInChat(viewport,card)
    })
    const clear=window.setTimeout(()=>setFocusedDecision(old=>old===focusedDecision?"":old),2400)
    return()=>{cancelAnimationFrame(frame);window.clearTimeout(clear)}
  },[focusedDecision,underExchange,detailLoaded,focusedMemory])

  return {focusedDecision,decisionHandled,dismissDecisionHandled:()=>setDecisionHandled(false)}
}
