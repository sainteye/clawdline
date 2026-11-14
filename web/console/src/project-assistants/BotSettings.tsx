import { useState } from "react"
import type * as W from "@clawdline/contract"
import { AppearancePicker } from "./BotAvatar"
import type { Words } from "./words"
import { errorCode } from "./client"
import { ApprovalSelect } from "./ApprovalSelect"
import { withApproval } from "./approvals"
export function BotSettings({bot,onSave,onClose,w,enabled,reason=""}:{bot:W.ProjectAssistantBot;onSave:(settings:W.ProjectAssistantEditSettings)=>Promise<void>;onClose:()=>void;w:Words;enabled:boolean;reason?:string}) {
  const [name,setName]=useState(bot.name),[appearance,setAppearance]=useState(bot.appearance),[instructions,setInstructions]=useState(bot.instructions.text),[busy,setBusy]=useState(false),[error,setError]=useState(""),[approval,setApproval]=useState<{permission_policy:W.ProjectAssistantPermissionPolicy;mode?:string}>(bot.mode?{permission_policy:bot.permission_policy,mode:bot.mode}:{permission_policy:bot.permission_policy})
  return <section className="pa-form-panel" role="dialog" aria-modal="true" aria-label={w.settings}><header className="pa-toolbar"><button onClick={onClose}>{w.cancel}</button><h2>{w.settings}</h2><button disabled={!enabled||busy||!name.trim()} onClick={async()=>{setBusy(true);setError("");try{await onSave({name:name.trim(),appearance,instructions,expected_version:bot.version,...approval})}catch(e){setError(errorCode(e))}finally{setBusy(false)}}}>{w.save}</button></header><div className="pa-form-body"><fieldset disabled={!enabled||busy}><AppearancePicker value={appearance} onChange={setAppearance} w={w}/><label>{w.name}<input value={name} onChange={e=>setName(e.target.value)}/></label><ApprovalSelect agent={bot.agent} policy={approval.permission_policy} mode={approval.mode} w={w} onChange={id=>setApproval(withApproval(id))}/><label>{w.instructions}<textarea value={instructions} onChange={e=>setInstructions(e.target.value)}/></label></fieldset>{!enabled&&reason&&<p role="status" className="pa-warning">{w.cannotEdit}: {reason}</p>}<p>{w.nextIdle}</p>{error&&<p role="alert">{w.unavailable} ({error})</p>}</div></section>
}
