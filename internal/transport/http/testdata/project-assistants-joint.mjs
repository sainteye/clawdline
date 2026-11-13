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
const {errorCode}=await import(pathToFileURL(root+"/web/console/src/project-assistants/client.ts"))
const lines=createInterface({input:process.stdin}),pending=new Map()
let sequence=0
lines.on("line",line=>{const message=JSON.parse(line),resolve=pending.get(message.id);assert.ok(resolve,"unmatched Go frame");pending.delete(message.id);resolve(message)})
const exchange=frame=>new Promise(resolve=>{const id=++sequence;pending.set(id,resolve);process.stdout.write(JSON.stringify({id,...frame})+"\n")})
const boot=await exchange({kind:"init"}),trace=[],checks=[],mutants=[]
const words=Object.keys(CARRIED)
assert.equal(words.length,253)
assert.deepEqual(words.filter(word=>word.startsWith("project-assistants.")).sort(),["status","projects","list","get","history","events","export","create","edit","send","stop","answer","read","observed","forget","memory","view"].map(word=>"project-assistants."+word).sort())
const signal=()=>new AbortController().signal
const request=(name,mode="snapshot",extra={})=>({key:boot.fixtures[name].key,conversation_id:boot.fixtures[name].conversation_id,mode,source_epoch:"",cursor:"",after_seq:0,page_size:100,...extra})
const scope=name=>({...boot.fixtures[name],conversation_id:boot.fixtures[name].conversation_id})

function sourceFor(name,sender="owner",changes={},advertised=words){
 const meta=boot.fixtures[name]
 let live={accountKey:"synthetic-account",readerID:sender,machineID:meta.key.machine_id,authorizationRevision:"revision-1",apiLevel:59,paired:true,online:true,writable:sender==="owner",...changes}
 const connection={machineDescriptor:()=>({machine:{commands:advertised}}),async _machineRequest(machine,word,fields){
  assert.equal(machine,meta.key.machine_id)
  const ref=word==="project-assistants.view"&&fields.mode==="snapshot"?"<".repeat(128):"joint-"+(sequence+1)
  const answer=await exchange({kind:"bridge",fixture:name,sender,word,fields,ref})
  assert.match(answer.sha256,/^[0-9a-f]{64}$/)
  trace.push({fixture:name,sender,word,fields,...answer})
  return answer.body
 }}
 const relay=capturedAssistantRelayFetch(meta.key.machine_id,()=>connection,words)
 let hold=null,started=null
 const source=new ProjectAssistantSourceClient(live,async(input,init)=>{const response=await relay(input,init);if(hold){started();await hold}return response},()=>live)
 return {source,setGrant(value){live=value},grant:()=>live,hold(){let release,notify;hold=new Promise(r=>{release=r});const ready=new Promise(r=>{notify=r});started=notify;return{ready,release}}}
}
const actual=sourceFor("main"),q=request("main","snapshot",{page_size:3})
await actual.source.status(signal())
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
 client.setGrant({...client.grant(),[changed]:changed==="apiLevel"?58:"replacement"})
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

for(const apiLevel of[58,undefined]){
 const old=sourceFor("main","owner",{apiLevel}),before=trace.length
 if(apiLevel===58){await old.source.status(signal());assert.equal(trace.length,before+1)}
 const admitted=trace.length
 await assert.rejects(old.source.view(request("main"),signal()),error=>errorCode(error)==="pa_unsupported")
 assert.equal(trace.length,admitted,"API58 or unknown View must refuse before transport")
}
const missing=sourceFor("main","owner",{},words.filter(word=>word!=="project-assistants.view")),beforeMissing=trace.length
await assert.rejects(missing.source.view(request("main"),signal()),error=>errorCode(error)==="pa_unsupported")
assert.equal(trace.length,beforeMissing,"unadvertised View must refuse before transport")
checks.push("JRV-07")

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
const legacyGrant={accountKey:"capture-account",readerID:"trusted-reader",machineID:"source-one",authorizationRevision:"capture",apiLevel:58,paired:true,online:true,writable:true}
const legacyRelay=capturedAssistantRelayFetch("source-one",()=>connection,words)
const legacySource=new ProjectAssistantSourceClient(legacyGrant,async(input,init)=>{http={input,method:init.method,body:init.body??null,idempotency:new Headers(init.headers).get("Idempotency-Key")};return legacyRelay(input,init)},()=>legacyGrant)
await legacySource.status(signal())
for(const row of rows){
 const suffix=row.op.split(".").at(-1);if(suffix==="status")continue
 const {request:_request,session:_session,...fields}=row.example
 if(suffix==="projects")fields.machine_id="source-one"
 const method={read:"markRead",observed:"observe",memory:"saveMemory"}[suffix]??suffix
 await legacySource[method](fields,signal())
}
assert.equal(captured.length,16)
assert.equal(new Set(captured.map(row=>row.word)).size,16)
const allMethods=new Set([...captured.map(row=>row.word),...trace.map(row=>row.word)])
assert.equal(allMethods.size,17)
assert.equal(trace.filter(row=>row.word==="project-assistants.export").length,0,"View scenarios must not use archive fallback")
assert.deepEqual(new Set(trace.filter(row=>row.word==="project-assistants.view"&&row.status===200).map(row=>row.fields.mode)),new Set(["snapshot","history","events","permissions","traces","detail"]))
checks.push("JRV-08")
assert.equal(new Set(checks).size,8)
await exchange({kind:"report",report:{checks:checks.sort(),successful_modes:6,strict_single_field_mutants:mutants,actual_methods:allMethods.size,legacy_api58_methods:captured.length,carried_words:words.length,real_source_responses:trace.length,archive_fallback_calls:0,native_model_calls:0}})
lines.close()
process.stdin.destroy()
