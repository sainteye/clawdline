import type * as W from "@clawdline/contract"
import { approvalChoices } from "./approvals"
import type { Words } from "./words"
import { PaSelect } from "./PaSelect"

/** Codync's Permissions setting: the agent's modes, Auto first, then Approve automatically. */
export function ApprovalSelect({agent,policy,mode,onChange,w}:{agent:W.ProjectAssistantAgent;policy:W.ProjectAssistantPermissionPolicy;mode:string|undefined;onChange:(id:string)=>void;w:Words}) {
  const {choices,selected}=approvalChoices(agent,policy,mode,w.approveAutomatically,w.auto_read)
  const current=choices.find(c=>c.id===selected)
  const detail=current?.detail==="auto"?w.approvalAuto:current?.detail==="approve"?w.approvalApprove:current?.detail==="auto_read"?w.auto_read:w.approvalMode
  return <PaSelect label={w.policy} value={selected} options={choices.map(c=>({value:c.id,label:c.label}))} onChange={onChange}><small className="pa-approval-detail">{detail}</small></PaSelect>
}
