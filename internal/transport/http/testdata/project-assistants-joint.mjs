// Test-only framed transport. View responses always come from the real Go
// Bridge/Router/Server/Host/Store; only original16 compatibility capture echoes.
import assert from "node:assert/strict"
import {readFileSync} from "node:fs"
import {createInterface} from "node:readline"
import {pathToFileURL} from "node:url"

const root=process.argv[2]
const {ProjectAssistantSourceClient,capturedAssistantRelayFetch}=await import(pathToFileURL(root+"/web/console/src/cloud/project-assistants.ts"))
const {readView,mergeTimeline,eventTimeline}=await import(pathToFileURL(root+"/web/console/src/project-assistants/read-views.ts"))
const {CARRIED}=await import(pathToFileURL(root+"/web/console/src/cloud/carry-generated.ts"))
const {withApproval}=await import(pathToFileURL(root+"/web/console/src/project-assistants/approvals.ts"))
const {command,errorCode}=await import(pathToFileURL(root+"/web/console/src/project-assistants/client.ts"))
const lines=createInterface({input:process.stdin}),pending=new Map()
let sequence=0
lines.on("line",line=>{const message=JSON.parse(line),resolve=pending.get(message.id);assert.ok(resolve,"unmatched Go frame");pending.delete(message.id);resolve(message)})
const exchange=frame=>new Promise(resolve=>{const id=++sequence;pending.set(id,resolve);process.stdout.write(JSON.stringify({id,...frame})+"\n")})
const boot=await exchange({kind:"init"}),trace=[],checks=[],mutants=[]
const words=Object.keys(CARRIED)
assert.deepEqual(words.filter(word=>word.startsWith("project-assistants.")).sort(),["dev-servers","dev-server-logs","dev-server-start","dev-server-restart","dev-server-stop","dev-server-remove","handoff-card","handoff-card-write","network-settings","network-settings-save","connector-credential","connector-secret","memory-view","configuration","configuration-profile","extensions","login-profiles","login-profile","login-secret","rename-artifact","groups","group-history","group-create","group-edit","group-delete","group-send","group-react","group-stop","team-plans","team-answer","team-settings","tower-floors","tower-floor-save","input","upload","send-files","output","attachment","send-pictures","status","projects","list","get","history","events","export","create","edit","send","stop","answer","read","react","observed","forget","memory","view","delete","visibility","rules","send-now","rule-remove","routines","routine-preview","routine-runs","routine-save","routine-enabled","routine-delete","routine-test","routine-hook"].map(word=>"project-assistants."+word).sort())
const signal=()=>new AbortController().signal
const request=(name,mode="snapshot",extra={})=>({key:boot.fixtures[name].key,conversation_id:boot.fixtures[name].conversation_id,mode,source_epoch:"",cursor:"",after_seq:0,page_size:100,...extra})
const scope=name=>({...boot.fixtures[name],conversation_id:boot.fixtures[name].conversation_id})

function sourceFor(name,sender="owner",changes={},advertised=words){
 const meta=boot.fixtures[name]
 let live={accountKey:"synthetic-account",readerID:sender,machineID:meta.key.machine_id,authorizationRevision:"revision-1",apiLevel:61,paired:true,online:true,writable:sender==="owner",...changes}
 async function call(ref,machine,word,fields){
  assert.equal(machine,meta.key.machine_id)
  const answer=await exchange({kind:"bridge",fixture:name,sender,word,fields,ref})
  assert.match(answer.sha256,/^[0-9a-f]{64}$/)
  trace.push({fixture:name,sender,word,fields,...answer})
  return answer.body
 }
 const connection={machineDescriptor:()=>({machine:{commands:advertised}}),
  _machineRequest:(machine,word,fields)=>call(word==="project-assistants.view"&&fields.mode==="snapshot"?"<".repeat(128):"joint-"+(sequence+1),machine,word,fields),
  _machineRequestAs:call}
 const relay=capturedAssistantRelayFetch(meta.key.machine_id,()=>connection,words)
 let hold=null,started=null
 const source=new ProjectAssistantSourceClient(live,async(input,init)=>{const response=await relay(input,init);if(hold){started();await hold}return response},()=>live)
 return {source,setGrant(value){live=value},grant:()=>live,hold(){let release,notify;hold=new Promise(r=>{release=r});const ready=new Promise(r=>{notify=r});started=notify;return{ready,release}}}
}
const actual=sourceFor("main"),q=request("main","snapshot",{page_size:3})
await actual.source.status(signal())
assert.equal(actual.source.canReactions(),false,"an additive advertised word must not enable reactions on API61")
const modernReaction=sourceFor("main","owner",{apiLevel:85})
assert.equal(modernReaction.source.canReactions(),false,"reaction authority requires a fresh source status")
await modernReaction.source.status(signal())
assert.equal(modernReaction.source.canReactions(),true)
const readonlyReaction=sourceFor("main","reader",{apiLevel:85})
await readonlyReaction.source.status(signal())
assert.equal(readonlyReaction.source.canReactions(),false,"read-only authority cannot react")
const snapshot=await actual.source.view(q,signal())
readView(snapshot,q,scope("main"))
assert.equal(snapshot.head.current_fence.generation,2)
assert.equal(snapshot.head.conversation.last_message_seq,120)
assert.equal(snapshot.head.event_frontier,603,"another Bot owns the machine frontier")
assert.equal(snapshot.head.unknown_turn_count,1)
assert.equal(snapshot.snapshot.pending_turns.length,1)
assert.equal(snapshot.snapshot.queued_messages[0].turn_id,"current-turn")
assert.equal(snapshot.items[0].message.seq,120)
assert.equal(snapshot.has_more,true)
// Additive handoff words still use the captured source and paired-person
// authority. Read the real source without opening an Agent or mutating a card.
const modernHandoff=sourceFor("main","owner",{apiLevel:93})
await modernHandoff.source.status(signal())
const handoff=await modernHandoff.source.handoff(boot.fixtures.main.key,boot.fixtures.main.conversation_id,signal())
assert.deepEqual(handoff.fence,snapshot.head.current_fence)
assert.equal(handoff.can_prepare,false,"an active or unknown turn cannot prepare a handoff")
assert.equal(handoff.card,undefined)
assert.equal(trace.at(-1).word,"project-assistants.handoff-card")
assert.equal(trace.at(-1).status,200)
const readonlyHandoff=sourceFor("main","reader",{apiLevel:93})
await readonlyHandoff.source.status(signal())
const beforeHandoffRead=trace.length
await assert.rejects(readonlyHandoff.source.handoff(boot.fixtures.main.key,boot.fixtures.main.conversation_id,signal()),error=>errorCode(error)==="pa_read_only")
assert.equal(trace.length,beforeHandoffRead,"read-only handoff requests are refused before transport")
const epoch=snapshot.source_epoch,history=[]
let cursor=snapshot.next_cursor
while(cursor){const page=await actual.source.view(request("main","history",{source_epoch:epoch,cursor}),signal());assert.equal(page.next_seq,0);history.push(...page.items);cursor=page.next_cursor}
const oldWork=history.find(row=>row.work?.id==="old-work")
assert.ok(oldWork)
assert.equal(oldWork.work.fence.generation,1)
assert.equal(oldWork.work.state,"unknown")
const emptyRequest=request("main","events",{source_epoch:epoch,after_seq:snapshot.head.event_frontier,page_size:256})
const empty=await actual.source.view(emptyRequest,signal())
assert.deepEqual(empty.items,[])
assert.equal(empty.next_seq,snapshot.head.event_frontier,"initial600 events are never replayed")
await exchange({kind:"control",fixture:"main",operation:"late_work"})
const eventRequest={...emptyRequest,page_size:1},events=await actual.source.view(eventRequest,signal())
assert.equal(events.items.length,1)
assert.equal(events.items[0].event.work.fence.generation,1)
assert.equal(events.next_seq,604)
assert.equal(events.head.event_frontier,605,"later machine head cannot skip the included source cursor")
const merged=mergeTimeline([eventTimeline(history,events.items),history])
assert.equal(merged.find(row=>row.work?.id==="old-work").work.state,"completed","an older history page must not downgrade the late event")
checks.push("JRV-02")

const permissions=await actual.source.view(request("main","permissions",{source_epoch:epoch,page_size:2}),signal())
assert.equal(permissions.head.pending_permission_count,3)
assert.equal(permissions.items.length,2)
assert.equal(permissions.has_more,true)
assert.equal(permissions.items[0].permission,undefined,"summary contains no answer options")
const permissionTail=await actual.source.view(request("main","permissions",{source_epoch:epoch,cursor:permissions.next_cursor,page_size:2}),signal())
assert.equal(permissionTail.items.length,1)
assert.equal(permissionTail.has_more,false)
const offerCursor=permissions.items[0].permission_summary.detail_cursor
const offer=await actual.source.view(request("main","detail",{source_epoch:epoch,cursor:offerCursor,page_size:1}),signal())
assert.equal(offer.items[0].permission.options.length,2)
assert.equal(offer.items[0].permission.fence.generation,2)
const traces=await actual.source.view(request("main","traces",{source_epoch:epoch}),signal())
assert.equal(traces.head.trace_category_count,1)
assert.equal(traces.items.length,1)
const detail=await actual.source.view(request("main","detail",{source_epoch:epoch,cursor:traces.items[0].trace_summary.detail_cursor,page_size:1}),signal())
assert.equal(detail.items[0].trace.text,"complete stored trace")
assert.equal(detail.items[0].trace.complete,true)
checks.push("JRV-01","JRV-03")

// Every mutant begins with an untouched successful Go body and changes exactly
// the named fact. The actual strict helper remains the only response validator.
for(const [path,change]of[
 ["head",body=>{delete body.head}],
 ["snapshot",body=>{delete body.snapshot}],
 ["items",body=>{body.items=null}],
 ["head.key.bot_id",body=>{body.head.key.bot_id="foreign"}],
 ["head.project.canonical_id",body=>{body.head.project.canonical_id="foreign"}],
 ["head.conversation.id",body=>{body.head.conversation.id="foreign"}],
 ["head.bot_version",body=>{body.head.bot_version=0}],
 ["snapshot.bot.agent",body=>{body.snapshot.bot.agent="unknown"}],
 ["items[0].kind",body=>{body.items[0].kind="work"}],
 ["items[0].work",body=>{body.items[0].work=oldWork.work}],
]){
 const body=structuredClone(snapshot);change(body)
 assert.throws(()=>readView(body,q,scope("main")),error=>["pa_invalid","pa_scope_denied"].includes(errorCode(error)),path)
 mutants.push({positive:"snapshot",changed_field:path})
}
readView(snapshot,q,scope("main"))
for(const [path,change]of[["next_seq",body=>{body.next_seq++}],["snapshot",body=>{body.snapshot=snapshot.snapshot}]]){
 const body=structuredClone(events);change(body)
 assert.throws(()=>readView(body,eventRequest,scope("main")),error=>errorCode(error)==="pa_invalid",path)
 mutants.push({positive:"events",changed_field:path})
}
checks.push("JRV-04")

const refusal=async(client,query,expected)=>{
 const before=trace.length
 await assert.rejects(client.view(query,signal()),error=>{
  assert.equal(trace.length,before+1)
  const raw=trace.at(-1).body
  assert.deepEqual(error,raw,"actual source code, field and retry data must cross unchanged")
  assert.equal(errorCode(error),expected)
  assert.equal(raw.head,undefined);assert.equal(raw.items,undefined);assert.equal(raw.snapshot,undefined)
  return true
 })
}
await refusal(actual.source,{...emptyRequest,source_epoch:"old-epoch"},"pa_cursor_expired")
await refusal(actual.source,{...emptyRequest,after_seq:events.head.event_frontier+1},"pa_cursor_expired")
await refusal(actual.source,request("main","history",{conversation_id:"foreign",source_epoch:epoch,cursor:snapshot.next_cursor}),"pa_not_found")
await refusal(actual.source,request("main","history",{source_epoch:epoch,cursor:offerCursor}),"pa_cursor_expired")
const capacity=sourceFor("capacity"),prefix=await capacity.source.view(request("capacity"),signal())
assert.equal(prefix.items.length,1)
assert.equal(prefix.items[0].id,"latest")
assert.equal(prefix.has_more,true)
await refusal(capacity.source,request("capacity","history",{source_epoch:prefix.source_epoch,cursor:prefix.next_cursor}),"pa_capacity_full")
const badScope=sourceFor("scope")
await exchange({kind:"control",fixture:"scope",operation:"break_scope"})
await refusal(badScope.source,request("scope"),"pa_scope_denied")
checks.push("JRV-05")

for(const changed of["accountKey","readerID","machineID","authorizationRevision","apiLevel"]){
 const client=sourceFor("main"),hold=client.hold(),waiting=client.source.view(request("main"),signal())
 await hold.ready
 client.setGrant({...client.grant(),[changed]:changed==="apiLevel"?60:"replacement"})
 hold.release()
 await assert.rejects(waiting,error=>errorCode(error)===(changed==="apiLevel"?"pa_unsupported":"pa_revoked"))
}
const revoked=sourceFor("revoked")
await exchange({kind:"control",fixture:"revoked",operation:"revoke_after_pack"})
await refusal(revoked.source,request("revoked"),"pa_revoked")
const reader=sourceFor("main","reader"),readonly=await reader.source.view(request("main"),signal())
assert.equal(readonly.source.writable,false)
await exchange({kind:"control",fixture:"main",operation:"disable"})
const disabled=await actual.source.view(request("main"),signal())
assert.equal(disabled.source.access,"history_only")
assert.equal(disabled.source.writable,false)
checks.push("JRV-06")

for(const apiLevel of[60,undefined]){
 const old=sourceFor("main","owner",{apiLevel}),before=trace.length
 if(apiLevel===60){await old.source.status(signal());assert.equal(trace.length,before+1)}
 const admitted=trace.length
 await assert.rejects(old.source.view(request("main"),signal()),error=>errorCode(error)==="pa_unsupported")
 assert.equal(trace.length,admitted,"API58 or unknown View must refuse before transport")
}
const missing=sourceFor("main","owner",{},words.filter(word=>word!=="project-assistants.view")),beforeMissing=trace.length
await assert.rejects(missing.source.view(request("main"),signal()),error=>errorCode(error)==="pa_unsupported")
assert.equal(trace.length,beforeMissing,"unadvertised View must refuse before transport")
checks.push("JRV-07")

const createSource=sourceFor("create","owner",{apiLevel:101}).source
await createSource.status(signal())
const target={machine_id:boot.fixtures.create.key.machine_id,project_id:boot.fixtures.create.project.project_id}
for(const [index,agent,choice] of [
 [1,"codex","approve"],
 [2,"codex","mode:read-only"],
 [3,"claude","mode:acceptEdits"],
 [4,"codex","mode:auto"],
 [5,"grok","mode:auto"],
]){
 const key={machine_id:target.machine_id,bot_id:"created-through-cloud-"+index}
 const approval=withApproval(choice)
 const input={command:command(key,"create","joint-create-"+index),settings:{
  target:agent==="grok"?{...target,project_id:""}:target,name:"Synthetic Bot "+index,appearance:{shape:"blob",color:"#e39567"},agent,
  instructions:"Synthetic standing instructions.",role_snapshot_ref:"",...approval
 }}
 const created=await createSource.create(input,signal()).catch(error=>{
  const reply=trace.at(-1)
  throw Error(agent+" "+choice+" create failed at the bridge: "+JSON.stringify({status:reply.status,body:reply.body}),{cause:error})
 })
 assert.equal(trace.at(-1).status,200,JSON.stringify(trace.at(-1).body))
 assert.deepEqual(created.bot.key,key)
 assert.equal(created.bot.permission_policy,approval.permission_policy)
 assert.equal(created.bot.mode??"",approval.mode??"")
 assert.equal(created.bot.agent,agent)
 if(index===1||agent==="grok"){
  const replay=await createSource.create(input,signal())
  assert.equal(replay.bot.conversation_id,created.bot.conversation_id)
  assert.deepEqual(replay.receipt,created.receipt)
 }
 const latest=await createSource.get(key,signal())
 assert.equal(latest.bot.conversation_id,created.bot.conversation_id)
 assert.equal(latest.bot.agent,agent)
 assert.equal(latest.bot.permission_policy,approval.permission_policy)
 assert.equal(latest.bot.mode??"",approval.mode??"")
 if(agent==="grok"){
  const settings={expected_version:latest.bot.version,name:"Renamed Grok Bot",appearance:latest.bot.appearance,
   instructions:latest.bot.instructions.text,agent,...approval}
  const edited=await createSource.edit({command:command(key,"edit","joint-edit-grok"),settings},signal())
  assert.equal(edited.bot.name,settings.name)
  const team=await createSource.edit({command:command(key,"edit","joint-team-grok"),settings:{...settings,expected_version:edited.bot.version,manage_team:true}},signal())
  assert.equal(team.bot.agent,"grok")
  assert.equal(team.bot.manage_team,true)
 }
}
checks.push("Cloud create approval settings")

const legacySuffixes=["status","projects","list","get","history","events","export","create","edit","send","stop","answer","read","observed","forget","memory"]
const legacyWords=new Set(legacySuffixes.map(word=>"project-assistants."+word))
const rows=JSON.parse(readFileSync(root+"/internal/domain/cloudvocab/vocabulary.json","utf8")).operations.filter(row=>legacyWords.has(row.op))
assert.equal(rows.length,16)
let http
const captured=[]
const connection={machineDescriptor:()=>({machine:{commands:words}}),
 _machineRequest:async(machine,word,fields,kind)=>receive("legacy-"+(sequence+1),machine,word,fields,kind),
 _machineRequestAs:async(ref,machine,word,fields,kind)=>receive(ref,machine,word,fields,kind)}
async function receive(ref,machine,word,fields,kind){
 assert.equal(machine,"source-one");assert.ok(legacyWords.has(word));assert.notEqual(word,"project-assistants.view")
 const reply=await exchange({kind:"capture",word,fields,ref,http})
 assert.equal(reply.status,200)
 captured.push({word,kind,http,sha256:reply.sha256})
 return reply.body
}
const legacyGrant={accountKey:"capture-account",readerID:"trusted-reader",machineID:"source-one",authorizationRevision:"capture",apiLevel:60,paired:true,online:true,writable:true}
const legacyRelay=capturedAssistantRelayFetch("source-one",()=>connection,words)
const legacySource=new ProjectAssistantSourceClient(legacyGrant,async(input,init)=>{http={input,method:init.method,body:init.body??null,idempotency:new Headers(init.headers).get("Idempotency-Key")};return legacyRelay(input,init)},()=>legacyGrant)
await legacySource.status(signal())
for(const row of rows){
 const suffix=row.op.split(".").at(-1);if(suffix==="status")continue
 const {request:_request,session:_session,...fields}=row.example
 if(suffix==="projects")fields.machine_id="source-one"
 // Current examples include an optional API93 handoff. API60 must refuse it
 // before transport, then keep accepting the original unversioned send shape.
 if(suffix==="send"){
  assert.ok(fields.handoff)
  const before=captured.length
  await assert.rejects(legacySource.send(fields,signal()),error=>errorCode(error)==="pa_unsupported")
  assert.equal(captured.length,before)
  delete fields.handoff
 }
 const method={read:"markRead",observed:"observe",memory:"saveMemory"}[suffix]??suffix
 await legacySource[method](fields,signal())
 if(suffix==="send")assert.equal("handoff" in JSON.parse(http.body),false)
}
assert.equal(captured.length,16)
assert.equal(new Set(captured.map(row=>row.word)).size,16)
const allMethods=new Set([...captured.map(row=>row.word),...trace.map(row=>row.word)])
assert.equal(allMethods.size,19)
assert.equal(trace.filter(row=>row.word==="project-assistants.export").length,0,"View scenarios must not use archive fallback")
assert.deepEqual(new Set(trace.filter(row=>row.word==="project-assistants.view"&&row.status===200).map(row=>row.fields.mode)),new Set(["snapshot","history","events","permissions","traces","detail"]))
checks.push("JRV-08")
assert.equal(new Set(checks).size,9)
await exchange({kind:"report",report:{checks:checks.sort(),successful_modes:6,strict_single_field_mutants:mutants,actual_methods:allMethods.size,legacy_api58_methods:captured.length,carried_words:words.length,handoff_read_methods:1,handoff_mutation_calls:0,real_source_responses:trace.length,archive_fallback_calls:0,native_model_calls:0}})
lines.close()
process.stdin.destroy()
