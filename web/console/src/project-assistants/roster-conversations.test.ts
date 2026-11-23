import {test} from "node:test"
import assert from "node:assert/strict"
import type * as W from "@clawdline/contract"
// @ts-expect-error Node executes source extensions.
import {conversations,conversationSections,orderedConversationSections,firstCallableBot,groupConversationKey,groupLastSpeaker,groupPreviewMembers,type GroupRosterEntry,type RosterRoom} from "./roster-conversations.ts"
// @ts-expect-error Node executes source extensions.
import {emptyRosterState,pinRosterBot,moveRosterBot} from "./roster-state.ts"
// @ts-expect-error Node executes source extensions.
import {rosterShownState} from "./working.ts"
const key=(bot_id:string,machine_id="m")=>({machine_id,bot_id})
const live={fresh:true,state:"online",readable:true,access:"interactive"}
const bot=(id:string,at="2026-11-18T01:00:00Z")=>({bot:{key:key(id),name:id,project:{project_id:"p"}},activity_at:at,latest_message:"",activity:"",pending_permissions:0,unread_count:0,source:live} as unknown as W.ProjectAssistantBotSummary)
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

test("unread first prepends unread Bots and preserves every default section",()=>{
  const lead={...bot("Lead"),bot:{...bot("Lead").bot,lead:true}}
  const unread={...bot("Unread"),unread_count:2}
  const remote={...unread,bot:{...unread.bot,key:key("remote","other"),name:"Remote"}}
  const sections=conversationSections(conversations([lead,unread,remote],[room("Group")]),emptyRosterState())
  const name=(row:ReturnType<typeof conversations>[number])=>row.kind==="bot"?row.item.bot.name:row.entry.group.name
  const [unreadSection,...remaining]=orderedConversationSections(sections,"unread")
  assert.equal(unreadSection.kind,"unread")
  assert.deepEqual(unreadSection.items.map(name),["Unread","Remote"])
  assert.deepEqual(remaining,sections)
  const allRead=conversationSections(conversations([lead],[room("Group")]),emptyRosterState())
  assert.deepEqual(orderedConversationSections(allRead,"unread"),allRead)
  assert.deepEqual(orderedConversationSections(sections,"machine").map(section=>[section.machineID,section.items.map(name)]),
    [["m",["Lead","Unread","Group"]],["other",["Remote"]]])
  assert.equal(orderedConversationSections(sections,"default"),sections)
})

test("unread first lists unread Bots, then working Bots, then every default section",()=>{
  const name=(row:ReturnType<typeof conversations>[number])=>row.kind==="bot"?row.item.bot.name:row.entry.group.name
  const working={...bot("Working"),status:"working"} as W.ProjectAssistantBotSummary
  const both={...bot("Both"),status:"working",unread_count:1} as W.ProjectAssistantBotSummary
  const unread={...bot("Unread"),unread_count:3}
  const idle={...bot("Idle"),status:"idle"} as W.ProjectAssistantBotSummary
  const sections=conversationSections(conversations([idle,working,both,unread],[room("Group")]),emptyRosterState())
  const ordered=orderedConversationSections(sections,"unread")
  assert.deepEqual(ordered.slice(0,2).map(section=>[section.kind,section.items.map(name)]),[["unread",["Both","Unread"]],["working",["Working"]]])
  assert.deepEqual(ordered.slice(2),sections)
  // Without unread Bots the working section still comes first.
  const noUnread=conversationSections(conversations([idle,working],[room("Group")]),emptyRosterState())
  assert.deepEqual(orderedConversationSections(noUnread,"unread").map(section=>[section.kind,section.items.map(name)])[0],["working",["Working"]])
  // A Bot that stops working returns to its default place only.
  const stopped=conversationSections(conversations([idle,{...working,status:"idle"} as W.ProjectAssistantBotSummary,unread],[room("Group")]),emptyRosterState())
  assert.deepEqual(orderedConversationSections(stopped,"unread").map(section=>section.kind),["unread","project"])
  assert.equal(orderedConversationSections(sections,"default"),sections)
  assert.deepEqual(orderedConversationSections(sections,"machine").map(section=>[section.kind,section.items.map(name)]),[["machine",sections[0].items.map(name)]])
})

test("the desktop call fallback follows sidebar order and skips hidden or read-only Bots",()=>{
  const source={state:"online",access:"interactive",fresh:true,readable:true,writable:true} as W.ProjectAssistantSourceStatus
  const recent={...bot("recent","2026-11-18T03:00:00Z"),source}
  const pinned={...bot("pinned"),source}
  const unavailable={...bot("unavailable"),source:{...source,writable:false}}
  const hidden={...bot("hidden"),source,bot:{...bot("hidden").bot,hidden:true}}
  const state={...emptyRosterState(),pinnedBotKeys:["unavailable","hidden","pinned"].map(id=>JSON.stringify(["m",id]))}
  assert.equal(firstCallableBot([recent,hidden,unavailable,pinned],state)?.bot.name,"pinned")
  assert.equal(firstCallableBot([recent,hidden,unavailable],state)?.bot.name,"recent")
  assert.equal(firstCallableBot([hidden,unavailable],state),undefined)
})
test("each machine's lead stays above pinned Bots and every Project, whatever its activity",()=>{
  const lead={...bot("Lead","2026-11-01T00:00:00Z"),bot:{...bot("Lead").bot,lead:true}} as W.ProjectAssistantBotSummary
  const other={...bot("Other","2026-11-01T00:00:00Z"),bot:{...bot("Other").bot,key:key("Other","n"),lead:true}} as W.ProjectAssistantBotSummary
  const rows=conversations([bot("Fresh","2026-11-19T00:00:00Z"),lead,bot("Pinned"),other],[])
  const state=pinRosterBot(emptyRosterState(),rows.find(r=>r.kind==="bot"&&r.item.bot.name==="Pinned")!.key,true)
  const groups=conversationSections(rows,state)
  assert.deepEqual(groups.map(g=>[g.kind,g.items.map(r=>r.kind==="bot"?r.item.bot.name:"")]),[["lead",["Lead","Other"]],["pinned",["Pinned"]],["project",["Fresh"]]])
  // Pinning the lead too does not move it out of its own section.
  const both=pinRosterBot(state,groups[0].items[0].key,true)
  assert.equal(conversationSections(rows,both)[0].kind,"lead")
  assert.deepEqual(conversationSections(rows,both)[0].items.map(r=>r.kind==="bot"?r.item.bot.name:""),["Lead","Other"])
})

test("unread first lifts the state each row shows: background work, then Bots waiting on dispatched Sessions",()=>{
  const name=(row:ReturnType<typeof conversations>[number])=>row.kind==="bot"?row.item.bot.name:row.entry.group.name
  const own={...bot("Own"),status:"working"} as W.ProjectAssistantBotSummary
  // The row shows this Bot as working (background threads); the order must agree.
  const background={...bot("Background"),status:"idle",background_work:{working:1,waiting:0,needs_input:0}} as W.ProjectAssistantBotSummary
  const waiting={...bot("Waiting"),status:"idle",dispatched_work:{running:2,needs_you:0,delivered:0}} as W.ProjectAssistantBotSummary
  const idle={...bot("Idle"),status:"idle"} as W.ProjectAssistantBotSummary
  const sections=conversationSections(conversations([idle,waiting,background,own],[]),emptyRosterState())
  const lifted=(ordered:ReturnType<typeof orderedConversationSections>)=>ordered.filter((section:{kind:string})=>section.kind==="working").map((section:{items:ReturnType<typeof conversations>})=>section.items.map(name))
  assert.deepEqual(lifted(orderedConversationSections(sections,"unread")),[["Background","Own","Waiting"]])
  // With dispatched work hidden, a waiting Bot stays where it was.
  assert.deepEqual(lifted(orderedConversationSections(sections,"unread",(item:W.ProjectAssistantBotSummary)=>rosterShownState(item,false))),[["Background","Own"]])
})
