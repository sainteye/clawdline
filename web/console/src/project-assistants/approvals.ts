import type * as W from "@clawdline/contract"

/** Codync's id for an agent's Auto mode (apps/desktop/src/shared/approvals.ts). */
export const AUTO_MODE = "auto"

/** The modes of the supplied adapters, Auto first; the daemon's AgentModes holds the same list and refuses others. */
export const AGENT_MODES: Record<W.ProjectAssistantAgent, readonly {id:string;name:string}[]> = {
  claude: [{id:AUTO_MODE,name:"Auto"},{id:"default",name:"Manual"},{id:"acceptEdits",name:"Accept edits"},{id:"plan",name:"Plan"}],
  codex: [{id:AUTO_MODE,name:"Auto review"},{id:"read-only",name:"Read-only"},{id:"workspace-write",name:"Workspace access"},{id:"agent-full-access",name:"Full access"}],
}

export interface ApprovalChoice { id: string; label: string; detail: "auto" | "mode" | "approve" | "auto_read" }

/**
 * Codync's Approvals choices: the agent's modes, Auto first, then Approve
 * automatically. A Bot on the older "allow verified reads" setting keeps it
 * as its own choice, so the setting never shows as more careful than it is.
 */
export function approvalChoices(agent: W.ProjectAssistantAgent, policy: W.ProjectAssistantPermissionPolicy, mode: string | undefined, approveLabel: string, readsLabel: string) {
  const choices: ApprovalChoice[] = AGENT_MODES[agent].map(m => ({id:`mode:${m.id}`,label:m.name,detail:m.id===AUTO_MODE?"auto":"mode"}))
  if (policy === "auto_read") choices.push({id:"auto_read",label:readsLabel,detail:"auto_read"})
  choices.push({id:"approve",label:approveLabel,detail:"approve"})
  const selected = policy === "approve" ? "approve" : policy === "auto_read" ? "auto_read" : `mode:${mode || AUTO_MODE}`
  if (!choices.some(c => c.id === selected)) choices.splice(choices.length - 1, 0, {id:selected,label:mode ?? "",detail:"mode"})
  return { choices, selected }
}

/** The settings an Approvals choice stands for. Auto has no mode field at
 *  all: the daemon compares a request's keys with its shape, and an empty
 *  optional mode is not part of it. */
export function withApproval(id: string): { permission_policy: W.ProjectAssistantPermissionPolicy; mode?: string } {
  if (id === "approve") return { permission_policy: "approve" }
  if (id === "auto_read") return { permission_policy: "auto_read" }
  const mode = id.slice("mode:".length)
  return mode === AUTO_MODE ? { permission_policy: "ask" } : { permission_policy: "ask", mode }
}
