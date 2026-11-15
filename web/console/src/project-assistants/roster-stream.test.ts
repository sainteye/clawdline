import { test } from "node:test"
import assert from "node:assert/strict"
import type * as W from "@clawdline/contract"
// @ts-expect-error -- Node executes source extensions.
import { validateRosterFrame, RosterSSEDecoder, RosterStreamFailure } from "./roster-stream.ts"
// @ts-expect-error -- Node executes source extensions.
import { errorCode } from "./client.ts"

const q: W.ProjectAssistantRosterRequest = { machine_id:"one", project_id:"project" }
const encode = (s: string) => new TextEncoder().encode(s)
const summary = (): W.ProjectAssistantBotSummary => ({
  bot:{key:{machine_id:"one",bot_id:"bot"},project:{machine_id:"one",project_id:"project",canonical_id:"canonical",kind:"repository"},conversation_id:"chat",name:"Synthetic Bot",agent:"codex",permission_policy:"ask",appearance:{shape:"blob",color:"#123456"},instructions:{text:"",revision:1,role_snapshot_ref:""},version:1,created_at:"2026-11-15T00:00:00Z",updated_at:"2026-11-15T00:00:00Z"},
  source:{machine_id:"one",access:"history_only",state:"online",fresh:true,readable:true,writable:false,observed_at:"",capabilities:[]},
  activity:"Writing 合成測試 🥔",activity_at:"0001-01-01T00:00:00Z",latest_message:"synthetic",unread_count:3,pending_permissions:0,version:1,status:"working",
  read_cursor:{key:{machine_id:"one",bot_id:"bot"},conversation_id:"chat",reader_id:"reader",message_seq:0},
})
const frame = (): W.ProjectAssistantRosterFrame => ({type:"snapshot-complete",source_epoch:"epoch",cursor:"signed-cursor",changes:[{key:{machine_id:"one",bot_id:"bot"},revision:0,removed:false,summary:summary()}]})
const wire = (f: W.ProjectAssistantRosterFrame) => `${f.cursor ? `id: ${f.cursor}\n` : ""}event: ${f.type}\ndata: ${JSON.stringify(f)}\n\n`
const failure = {code:"pa_cursor_expired" as const,field:"roster_stream",retry_after_seconds:0}

test("roster frame validator accepts optional tombstone summaries and repeated durable replacements", () => {
  const value=frame(), row=value.changes![0]
  value.changes!.push({...row}, {...row,removed:true}, {...row,removed:true,summary:undefined})
  assert.deepEqual(validateRosterFrame(value,q),value)
  for(const type of ["hello","resync","closure"] as const) {
    const value=type==="hello"?{type,source_epoch:"epoch"}:{type,failure}
    assert.deepEqual(validateRosterFrame(value,q),{...value,changes:[]})
  }
  for(const type of ["snapshot","snapshot-complete","changes"] as const) assert.deepEqual(validateRosterFrame({type,source_epoch:"epoch",cursor:"next"},q).changes,[])
})

test("roster validator refuses malformed metadata, foreign scope and unsafe counts atomically", () => {
  const cases: [(f: W.ProjectAssistantRosterFrame)=>void,string][] = [
    [f=>{f.changes![0].key.machine_id="foreign"},"pa_scope_denied"],
    [f=>{f.changes![0].summary!.bot.key.bot_id="foreign"},"pa_scope_denied"],
    [f=>{f.changes![0].summary!.bot.project.project_id="foreign"},"pa_scope_denied"],
    [f=>{f.changes![0].summary!.bot.project.machine_id="foreign"},"pa_scope_denied"],
    [f=>{f.changes![0].summary!.read_cursor.conversation_id="foreign"},"pa_scope_denied"],
    [f=>{f.changes![0].summary!.source.machine_id="foreign"},"pa_scope_denied"],
    [f=>{f.changes![0].revision=Number.MAX_SAFE_INTEGER+1},"pa_invalid"],
    [f=>{f.changes![0].summary!.unread_count=-1},"pa_invalid"],
    [f=>{f.changes![0].summary!.pending_permissions=1.1},"pa_invalid"],
    [f=>{f.changes![0].summary!.bot.agent="other" as W.ProjectAssistantAgent},"pa_invalid"],
    [f=>{f.changes![0].summary!.bot.appearance.shape="other" as W.ProjectAssistantShape},"pa_invalid"],
    [f=>{f.changes![0].summary!.bot.permission_policy="other" as W.ProjectAssistantPermissionPolicy},"pa_invalid"],
    [f=>{f.changes![0].summary!.bot.project.kind="other" as W.ProjectAssistantProjectKind},"pa_invalid"],
    [f=>{f.changes![0].summary!.source.access="other" as W.ProjectAssistantAccess},"pa_invalid"],
    [f=>{f.changes![0].summary!.read_cursor.message_seq=Number.MAX_SAFE_INTEGER+1},"pa_invalid"],
    [f=>{f.changes![0].summary!.source.writable="false" as unknown as boolean},"pa_invalid"],
    [f=>{f.changes![0].summary!.source.state="revoked"},"pa_revoked"],
    [f=>{f.changes![0].summary!.source.fresh=false},"pa_offline"],
    [f=>{f.changes![0].summary!.source.readable=false},"pa_scope_denied"],
    [f=>{f.changes![0].summary!.failure={...failure,code:"foreign" as W.ProjectAssistantErrorCode}},"pa_invalid"],
    [f=>{f.changes![0].summary=undefined},"pa_invalid"],
    [f=>{f.cursor="界".repeat(1366)},"pa_capacity_full"],
    [f=>{f.changes=Array(101).fill(f.changes![0])},"pa_invalid"],
  ]
  for(const [change,code] of cases) {
    const value=frame();change(value)
    // A valid first row must not escape before the malformed second row.
    value.changes!.unshift(frame().changes![0])
    const decoder=new RosterSSEDecoder(q),seen:unknown[]=[]
    assert.throws(()=>decoder.push(encode(wire(value)),f=>seen.push(f)),e=>errorCode(e)===code)
    assert.equal(seen.length,0)
  }
  const value=frame();value.type="changes";value.changes=Array(256).fill(value.changes![0]);assert.equal(validateRosterFrame(value,q).changes!.length,256)
  value.changes.push(value.changes[0]);assert.throws(()=>validateRosterFrame(value,q))
})

test("roster control frames cannot advance cursors or carry rows and data frames require both epoch and cursor", () => {
  const invalid: unknown[] = [
    {type:"hello",source_epoch:"epoch",cursor:"next"}, {type:"hello"}, {type:"hello",source_epoch:"epoch",failure},
    {type:"resync",failure,cursor:"next"}, {type:"closure",failure,source_epoch:"epoch"}, {type:"closure"},
    {type:"resync",failure,changes:frame().changes}, {type:"snapshot",source_epoch:"epoch"}, {type:"changes",cursor:"next"},
    {type:"changes",cursor:"next",source_epoch:"epoch",failure}, {type:"snapshot-complete",cursor:"next",source_epoch:"epoch",changes:null},
  ]
  for(const value of invalid) assert.throws(()=>validateRosterFrame(value,q),e=>errorCode(e)==="pa_invalid")
})

test("SSE decodes split UTF-8, CRLF and multiline JSON with no inherited event IDs", () => {
  const value=frame(), payload=JSON.stringify(value).replace(',"changes"',',\n"changes"')
  const input=encode(": comment\r\nunknown: ignored\r\nid: signed-cursor\r\nevent: snapshot-complete\r\n"+payload.split("\n").map(s=>"data: "+s+"\r\n").join("")+"\r\n")
  const decoder=new RosterSSEDecoder(q),seen:W.ProjectAssistantRosterFrame[]=[]
  for(const byte of input) decoder.push(Uint8Array.of(byte),f=>seen.push(f))
  assert.deepEqual(seen,[value])
  assert.throws(()=>decoder.push(encode('event: changes\ndata: {"type":"changes","source_epoch":"epoch","cursor":"signed-cursor"}\n\n'),f=>seen.push(f)),e=>e instanceof RosterStreamFailure&&e.kind==="invalid")
  assert.equal(seen.length,1)
})

test("SSE rejects mismatched or duplicate event/id fields, malformed JSON and invalid UTF-8", () => {
  const good=wire(frame())
  for(const input of [good.replace("id: signed-cursor","id: other"),good.replace("event: snapshot-complete","event: changes"),good.replace("id: signed-cursor\n",""),"id: next\n"+good,"event: changes\n"+good,'event: hello\ndata: {broken\n\n', 'event: hello\n\n']) {
    const seen:unknown[]=[]
    assert.throws(()=>new RosterSSEDecoder(q).push(encode(input),f=>seen.push(f)))
    assert.equal(seen.length,0)
  }
  assert.throws(()=>new RosterSSEDecoder(q).push(Uint8Array.of(255),()=>{}),e=>errorCode(e)==="pa_invalid")
})

test("SSE retains bounded partial data while discarding unlimited comment/unknown fragments", () => {
  for(const prefix of [":", "unknown:"]) {
    const decoder=new RosterSSEDecoder(q),seen:unknown[]=[]
    decoder.push(encode(prefix),f=>seen.push(f))
    const block=encode("x".repeat(1024*1024))
    for(let i=0;i<10;i++)decoder.push(block,f=>seen.push(f))
    decoder.push(encode("\n\n"+wire({type:"hello",source_epoch:"epoch"})),f=>seen.push(f))
    assert.equal(seen.length,1)
  }
  for(const input of ["data: "+"x".repeat(8*1024*1024+1),"id: "+"界".repeat(1366)]) assert.throws(()=>new RosterSSEDecoder(q).push(encode(input),()=>{}),e=>e instanceof RosterStreamFailure&&e.kind==="capacity")
  const decoder=new RosterSSEDecoder(q)
  decoder.push(encode("data: "+"x".repeat(8*1024*1024-1)+"\n"),()=>{})
  assert.throws(()=>decoder.push(encode("data: x\n"),()=>{}),e=>errorCode(e)==="pa_capacity_full")
})

test("SSE admits the full 8 MiB JSON budget plus framing and exactly 4096 cursor bytes", () => {
  const value=frame();value.cursor="界".repeat(1365)+"x"
  value.changes![0].summary!.latest_message=""
  const room=8*1024*1024-encode(JSON.stringify(value)).length
  value.changes![0].summary!.latest_message="x".repeat(room)
  const seen:unknown[]=[]
  new RosterSSEDecoder(q).push(encode(wire(value)),f=>seen.push(f))
  assert.equal(seen.length,1)
  value.changes![0].summary!.latest_message+="x"
  assert.throws(()=>validateRosterFrame(value,q),e=>errorCode(e)==="pa_capacity_full")
})

test("EOF and explicit resync/closure retain distinct terminal errors without inventing application", () => {
  for(const partial of ["", "data: {",wire(frame())]) {
    const decoder=new RosterSSEDecoder(q);decoder.push(encode(partial),()=>{})
    assert.throws(()=>decoder.end(),e=>e instanceof RosterStreamFailure&&e.kind==="eof"&&errorCode(e)==="pa_offline")
  }
  for(const type of ["resync","closure"] as const) {
    const seen:unknown[]=[]
    assert.throws(()=>new RosterSSEDecoder(q).push(encode(wire({type,failure})),f=>seen.push(f)),e=>e instanceof RosterStreamFailure&&e.kind===type&&e.error.code===failure.code)
    assert.deepEqual(seen,[{type,failure,changes:[]}])
  }
})
