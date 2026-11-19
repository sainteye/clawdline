import {test} from "node:test"
import assert from "node:assert/strict"
import type * as W from "@clawdline/contract"
// @ts-expect-error Node executes source extensions.
import {conversations,conversationSections,groupConversationKey,groupLastSpeaker,groupPreviewMembers,type GroupRosterEntry,type RosterRoom} from "./roster-conversations.ts"
// @ts-expect-error Node executes source extensions.
import {emptyRosterState,pinRosterBot,moveRosterBot} from "./roster-state.ts"
const key=(bot_id:string,machine_id="m")=>({machine_id,bot_id})
const bot=(id:string,at="2026-11-18T01:00:00Z")=>({bot:{key:key(id),name:id,project:{project_id:"p"}},activity_at:at} as W.ProjectAssistantBotSummary)
const room=(id:string,last_entry?:RosterRoom["last_entry"])=>({source:{machineID:"m"},status:{},group:{key:key(id),name:id,project:{project_id:"p"},members:[{key:key("One"),joined_revision:1},{key:key("Two"),joined_revision:1}],last_entry,updated_at:"2099-01-01T00:00:00Z"}} as GroupRosterEntry)
test("a group and Bot with the same IDs mix in their real Project and keep separate pins",()=>{
  const entries=conversations([bot("same")],[room("same")]),state=pinRosterBot(emptyRosterState(),groupConversationKey(key("same")),true)
  const groups=conversationSections(entries,state)
  assert.deepEqual(groups.map(g=>[g.kind,g.items.map(row=>row.kind)]),[["pinned",["group"]],["project",["bot"]]])
  assert.notEqual(entries[0].key,entries[1].key)
  const ordered=moveRosterBot(state,entries[1].key,entries[0].key,false,entries.map(row=>row.key))
  assert.deepEqual(conversationSections(entries,ordered)[0].items.map(row=>row.kind),["group","bot"])
})
test("only real message timestamps sort rooms; old entries keep their place",()=>{
  const rooms=[room("new",{seq:1,kind:"member",author:key("Two"),text:"New reply",at:"2026-11-18T02:00:00Z"}),room("old")]
  const rows=conversations([bot("One")],rooms)
  assert.deepEqual(rows.map(row=>row.kind==="group"?row.entry.group.name:row.item.bot.name),["new","One","old"])
  const old=room("no-time",{seq:9999,kind:"member",author:key("One"),text:"Old reply"})
  assert.equal(conversations([bot("One")],[old]).at(-1)?.kind,"group")
})
test("last speaker uses the returned author, supports the person and never invents a missing summary",()=>{
  const bots=[bot("One").bot,{...bot("Two").bot,name:"Second Bot"}]
  const group=room("r",{seq:4,kind:"member",author:key("Two"),text:"Actual last reply"}).group
  assert.deepEqual(groupLastSpeaker(group,bots,"You"),{speaker:"Second Bot",text:"Actual last reply"})
  assert.deepEqual(groupLastSpeaker({...group,last_entry:{seq:5,kind:"person",text:"Question"}},bots,"You"),{speaker:"You",text:"Question"})
  assert.equal(groupLastSpeaker({...group,last_entry:undefined},bots,"You"),null)
  assert.equal(groupLastSpeaker({...group,last_entry:{seq:6,kind:"system",text:"Not a member reply"}},bots,"You"),null)
  assert.equal(groupLastSpeaker({...group,last_entry:{...group.last_entry!,author:key("Two","other")}},bots,"You")?.speaker,"Two")
})
test("the avatar preview counts all remaining members without losing membership or unknown identities",()=>{
  const group=room("many").group
  group.members=Array.from({length:64},(_,i)=>({key:key(String(i)),joined_revision:1}))
  const preview=groupPreviewMembers(group.members)
  assert.equal(preview.members.length,4);assert.equal(preview.more,60);assert.equal(group.members.length,64)
  assert.equal(groupPreviewMembers([]).more,0)
})
test("same Project on another source, removed rooms and hidden saved identities stay isolated",()=>{
  const one=room("r"),other=room("r");other.group={...other.group,key:key("r","other")}
  const gone=room("gone");gone.group.deleted=true
  const state={...emptyRosterState(),pinnedBotKeys:["hidden"],botSortOrder:["foreign"]}
  const sections=conversationSections(conversations([bot("One")],[one,other,gone]),state)
  assert.deepEqual(sections.map(group=>group.machineID),["m","other"])
  assert.equal(sections.flatMap(group=>group.items).length,3)
  assert.deepEqual(state.pinnedBotKeys,["hidden"])
})
