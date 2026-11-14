import type * as W from "@clawdline/contract"
import { approvalChoices } from "./approvals"
import type { Words } from "./words"

/** Codync's Permissions setting: the agent's modes, Auto first, then Approve automatically. */
export function ApprovalSelect({agent,policy,mode,onChange,w}:{agent:W.ProjectAssistantAgent;policy:W.ProjectAssistantPermissionPolicy;mode:string|undefined;onChange:(id:string)=>void;w:Words}) {
  const {choices,selected}=approvalChoices(agent,policy,mode,w.approveAutomatically,w.auto_read)
  const current=choices.find(c=>c.id===selected)
  const detail=current?.detail==="auto"?w.approvalAuto:current?.detail==="approve"?w.approvalApprove:current?.detail==="auto_read"?w.auto_read:w.approvalMode
  return <label>{w.policy}<select value={selected} onChange={e=>onChange(e.target.value)}>{choices.map(c=><option key={c.id} value={c.id}>{c.label}</option>)}</select><small className="pa-approval-detail">{detail}</small></label>
}
