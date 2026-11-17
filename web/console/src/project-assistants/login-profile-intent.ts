import type * as W from "@clawdline/contract"

/** Local recovery data identifies an existing operation; it cannot select a
 * different Bot, Project, Agent or source when a sheet is reopened. */
export function validLoginIntent(value:unknown,bot:W.ProjectAssistantBot):value is W.ProjectAssistantLoginProfileAction{
 if(!value||typeof value!=="object")return false
 const q=value as W.ProjectAssistantLoginProfileAction,c=q.command,f=q.fence
 if(!c||!f||c.operation!=="login-profile"||typeof c.idempotency_key!=="string"||!c.idempotency_key||typeof c.content_hash!=="string"||!c.key||c.key.machine_id!==bot.key.machine_id||c.key.bot_id!==bot.key.bot_id)return false
 if(!f.key||f.key.machine_id!==bot.key.machine_id||f.key.bot_id!==bot.key.bot_id||f.conversation_id!==bot.conversation_id||typeof f.binding_id!=="string"||!Number.isSafeInteger(f.generation)||f.generation<0||!f.project)return false
 for(const key of ["machine_id","project_id","canonical_id","kind"] as const)if(f.project[key]!==bot.project[key])return false
 if(!["claude","codex"].includes(q.agent)||!["create","rename","start","cancel","check","remove"].includes(q.action)||typeof q.profile_id!=="string"||q.profile_id!==""&&!/^[0-9a-f]{32}$/.test(q.profile_id)||typeof q.name!=="string"||!Number.isSafeInteger(q.revision)||q.revision<0||!Number.isSafeInteger(q.expected_version)||q.expected_version<1)return false
 return Object.keys(q).sort().join(",")==="action,agent,command,expected_version,fence,name,profile_id,revision"
}
