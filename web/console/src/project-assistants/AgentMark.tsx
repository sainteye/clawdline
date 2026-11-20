import type * as W from "@clawdline/contract"
import { assistantLogo } from "../legacy/js/core/assistant-logos.js"

/** The same agent logos the Session list shows. */
export function AgentMark({agent,size=22}:{agent:W.ProjectAssistantAgent;size?:number}) {
  return <span className="pa-agent-logo" style={{width:size,height:size}} aria-hidden="true" dangerouslySetInnerHTML={{__html:assistantLogo(agent) as string}}/>
}

export {agentName} from "./fallback-settings"
