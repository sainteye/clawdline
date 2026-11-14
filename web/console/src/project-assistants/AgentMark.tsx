import type * as W from "@clawdline/contract"

/** A small mark for each agent, drawn here: a burst for Claude Code, a prompt for Codex. */
export function AgentMark({agent,size=22}:{agent:W.ProjectAssistantAgent;size?:number}) {
  if(agent==="claude")return <svg width={size} height={size} viewBox="0 0 24 24" aria-hidden="true" fill="none" stroke="#d97757" strokeWidth="2.2" strokeLinecap="round"><path d="M12 3v18M3 12h18M5.6 5.6l12.8 12.8M18.4 5.6 5.6 18.4"/></svg>
  return <svg width={size} height={size} viewBox="0 0 24 24" aria-hidden="true" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><rect x="2.5" y="4" width="19" height="16" rx="4"/><path d="m7 10 3 2.5L7 15M12.5 15.5h4.5"/></svg>
}

export const agentName = (agent: W.ProjectAssistantAgent) => agent === "claude" ? "Claude Code" : "Codex"
