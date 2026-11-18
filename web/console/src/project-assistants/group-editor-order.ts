import type * as W from "@clawdline/contract"
// @ts-expect-error Node executes source extensions.
import {botIdentity} from "./view-state.ts"

/** Retained members keep the room's order; new selections follow afterward. */
export function orderedGroupMembers(existing:readonly W.ProjectAssistantBotKey[],selected:readonly string[],eligible:readonly W.ProjectAssistantBotKey[]):W.ProjectAssistantBotKey[]{
  const selectedIDs=new Set(selected),existingIDs=new Set(existing.map(botIdentity)),available=new Map(eligible.map(key=>[botIdentity(key),key]))
  const order=[...existing.map(botIdentity).filter(id=>selectedIDs.has(id)),...selected.filter(id=>!existingIDs.has(id))]
  return [...new Set(order)].flatMap(id=>{const key=available.get(id);return key?[key]:[]})
}
