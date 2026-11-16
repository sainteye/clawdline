import assert from "node:assert/strict"
import {readFileSync,writeFileSync} from "node:fs"
import {pathToFileURL} from "node:url"

const root=process.argv[2],boot=JSON.parse(readFileSync(0,"utf8"))
const {ProjectAssistantSourceClient}=await import(pathToFileURL(root+"/web/console/src/cloud/project-assistants.ts"))
const {readView,eventTimeline}=await import(pathToFileURL(root+"/web/console/src/project-assistants/read-views.ts"))
const {errorCode}=await import(pathToFileURL(root+"/web/console/src/project-assistants/client.ts"))
const grant={accountKey:"local",readerID:"local",machineID:boot.key.machine_id,authorizationRevision:"fixture",apiLevel:73,paired:true,online:true,writable:true}
const evidence={responses:[],checks:[],mutants:[]}
const transport=async(path,init)=>{
 const response=await fetch(boot.url+path,{...init,headers:{...init?.headers,Authorization:"Bearer "+boot.token}})
 evidence.responses.push({path,status:response.status,body:await response.clone().json()})
 return response
}
const source=new ProjectAssistantSourceClient(grant,transport,()=>grant)
const signal=()=>new AbortController().signal
const command=(operation,id)=>({key:boot.key,operation,idempotency_key:id,content_hash:"not-authority"})
try {
 await source.status(signal())
 const get=await source.get(boot.key,signal())
 const scope={key:boot.key,project:get.bot.project,conversation_id:get.conversation.id}
 const request=(mode,extra={})=>({key:boot.key,conversation_id:scope.conversation_id,mode,source_epoch:"",cursor:"",after_seq:0,page_size:100,...extra})
 const snapshot=await source.view(request("snapshot"),signal())
 let bot=snapshot.snapshot.bot
 const edited=await source.edit({command:command("edit","metadata-edit"),settings:{expected_version:bot.version,name:"Changed metadata",appearance:bot.appearance,instructions:bot.instructions.text}},signal())
 assert.equal(edited.bot.name,"Changed metadata")
 await source.setHidden({command:command("visibility","hide"),hidden:true},signal())
 await source.setHidden({command:command("visibility","show"),hidden:false},signal())
 const q=request("events",{source_epoch:snapshot.source_epoch})
 const page=await source.view(q,signal())
 readView(page,q,scope)
 const events=page.items.map(row=>row.event).filter(e=>e.bot)
 assert.deepEqual(events.map(e=>e.kind),["bot_created","bot_updated","bot_updated","bot_updated"])
 assert.equal(events[1].bot.bot.name,"Changed metadata")
 assert.equal(events[1].bot.activity_at,"0001-01-01T00:00:00Z")
 assert.equal(events[2].bot.bot.hidden,true)
 assert.equal(events[3].bot.bot.hidden,undefined)
 for(const event of events)assert.deepEqual(event.bot.read_cursor,{key:boot.key,conversation_id:scope.conversation_id,reader_id:"",message_seq:0})
 assert.deepEqual(eventTimeline([],page.items),[],"metadata never invents a chat message")
 evidence.checks.push("create/edit/hide/show zero observations accepted by production SourceClient and strict readView")
 const cursorAt=body=>body.items.find(row=>row.event?.kind==="bot_updated").event.bot.read_cursor
 for(const [field,value]of[["key.machine_id","foreign"],["key.bot_id","foreign"],["conversation_id","foreign"],["message_seq",-1],["reader_id",1]]){
  const body=structuredClone(page),cursor=cursorAt(body)
  if(field.startsWith("key."))cursor.key[field.slice(4)]=value
  else cursor[field]=value
  let code
  assert.throws(()=>readView(body,q,scope),error=>{code=errorCode(error);return code==="pa_invalid"||code==="pa_scope_denied"},field)
  evidence.mutants.push({changed_field:field,code})
 }
 // Replace only the cursor fact with each actual Go projection. These partial
 // nonzero observations must never be repaired into successful source bodies.
 for(const cursor of boot.malformed){
  const body=structuredClone(page)
  body.items.find(row=>row.event?.kind==="bot_updated").event.bot.read_cursor=cursor
  assert.throws(()=>readView(body,q,scope),e=>errorCode(e)==="pa_invalid")
  evidence.mutants.push({changed_field:"read_cursor",go_projection:cursor,code:"pa_invalid"})
 }
 await source.markRead({command:command("read","read-stored"),cursor:{key:boot.key,conversation_id:scope.conversation_id,reader_id:"ignored-body-reader",message_seq:1}},signal())
 const listed=await source.list({machine_id:boot.key.machine_id,query:"",cursor:"",page_size:100},signal())
 const observed=listed.items.find(row=>row.bot.key.bot_id===boot.key.bot_id).read_cursor
 assert.equal(observed.message_seq,1)
 assert.ok(observed.reader_id)
 assert.notEqual(observed.reader_id,"ignored-body-reader")
 assert.deepEqual(observed.key,boot.key)
 assert.equal(observed.conversation_id,scope.conversation_id)
 const nextQ=request("events",{source_epoch:page.source_epoch,after_seq:page.next_seq})
 const next=await source.view(nextQ,signal())
 readView(next,nextQ,scope)
 assert.deepEqual(next.items.find(row=>row.event?.read).event.read,observed)
 evidence.checks.push("authenticated positive read cursor remains source-issued and unchanged in list/read_advanced")
 console.log(JSON.stringify({checks:evidence.checks,mutants:evidence.mutants}))
} catch(error) {
 evidence.failure={code:errorCode(error),message:String(error),stack:error?.stack}
 throw error
} finally {
 if(process.env.CLAWDLINE_PA_EVENT_CURSOR_ARTIFACTS)writeFileSync(process.env.CLAWDLINE_PA_EVENT_CURSOR_ARTIFACTS+"/consumer.json",JSON.stringify(evidence,null,2)+"\n")
}
