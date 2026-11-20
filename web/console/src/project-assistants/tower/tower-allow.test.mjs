// Fault injection for the tower's "Allow once": each failure shows its own
// reason and nothing is sent twice. The synthetic source is the chat's own
// fixture; no HTTP, provider or real Session.
import assert from "node:assert/strict"
import { after, test } from "node:test"
import { createRequire } from "node:module"
import { fileURLToPath } from "node:url"
import { build } from "esbuild"

globalThis.document??={getElementById:()=>null,querySelector:()=>null,querySelectorAll:()=>[],addEventListener:()=>{},createElement:()=>({style:{},setAttribute(){},appendChild(){},addEventListener(){}}),documentElement:{style:{},setAttribute(){},lang:"en"},body:{appendChild(){},classList:{add(){},remove(){},toggle(){},contains:()=>false}}}
globalThis.location??=new URL("http://synthetic.invalid/")
globalThis.window??={document:globalThis.document,location:globalThis.location,navigator:globalThis.navigator,matchMedia:()=>({matches:false,addEventListener(){},removeEventListener(){}}),addEventListener(){},removeEventListener(){}}
const {outputFiles}=await build({stdin:{contents:'export {readWaiting,allowOnce,AllowError} from "./tower/tower-allow";export {fixtureSource} from "./fixtures"',resolveDir:fileURLToPath(new URL("..",import.meta.url)),loader:"ts"},bundle:true,platform:"node",format:"cjs",packages:"external",write:false,jsx:"automatic",loader:{".css":"empty",".webp":"empty"}})
const compiled={exports:{}}
const importedTimers=[],nativeTimers={setTimeout:globalThis.setTimeout,setInterval:globalThis.setInterval}
for(const kind of Object.keys(nativeTimers))globalThis[kind]=(...args)=>{const timer=nativeTimers[kind](...args);importedTimers.push(timer);return timer}
try{new Function("require","module","exports",outputFiles[0].text)(createRequire(import.meta.url),compiled,compiled.exports)}
finally{Object.assign(globalThis,nativeTimers)}
after(()=>{for(const timer of importedTimers)clearTimeout(timer)})
const {readWaiting,allowOnce,AllowError,fixtureSource}=compiled.exports

const at="2026-11-20T00:00:00Z"
function setup() {
  const log=[],source=fixtureSource("studio",log),bot=source.bots[1]
  const item={bot,source:{},activity:"",activity_at:at,latest_message:"",unread_count:0,pending_permissions:1,read_cursor:{},version:1}
  const live=()=>[source],answers=()=>log.filter(r=>r.method==="answer")
  let current
  const offer=(over={})=>({id:"permission-1",native_request_id:"request-1",turn_id:"turn-1",fence:current,state:"pending",expires_at:"2099-01-01T00:00:00Z",selected_option_id:"",options:[{id:"yes",kind:"allow_once",label:"Allow once",allow:true,read_only:true},{id:"no",kind:"reject_once",label:"Deny",allow:false,read_only:true}],...over})
  // A later event about the same offer replaces it, as the source would publish it.
  const publish=(kind,permission)=>source.push({id:crypto.randomUUID(),seq:0,at,version:"project-assistants.v1",kind,fence:current,permission})
  const signal=new AbortController().signal
  const load=async()=>{const waiting=await readWaiting(source,live,item,signal);current=waiting.offer.fence;return waiting}
  return {source,item,live,answers,offer,publish,load,signal}
}
const reason=async(promise)=>{try{await promise}catch(error){if(error instanceof AllowError)return error.reason;throw error}assert.fail("expected a refusal")}

test("the card reads the whole offer and its allow-once option", async () => {
  const {source,item,live,signal}=setup()
  const waiting=await readWaiting(source,live,item,signal)
  assert.equal(waiting.offer.id,"permission-1")
  assert.deepEqual(waiting.offer.options.map(o=>o.label),["Allow once","Deny","Always"])
  assert.equal(waiting.once.id,"yes")
  assert.equal(waiting.writable,true)
})

test("allow once rereads, then sends one answer with the allow-once option", async () => {
  const {source,item,live,answers,signal}=setup()
  const waiting=await readWaiting(source,live,item,signal)
  const receipt=await allowOnce(source,live,waiting,signal)
  assert.equal(receipt.state,"accepted")
  assert.equal(answers().length,1)
  assert.equal(answers()[0].request.option_id,"yes")
  assert.equal(answers()[0].request.offer_id,"permission-1")
})

test("an offer that expired after the card loaded is refused before sending", async () => {
  const {source,item,live,answers,offer,publish,load,signal}=setup()
  const waiting=await load()
  publish("permission_offered",offer({expires_at:"2020-01-01T00:00:00Z"}))
  assert.equal(await reason(allowOnce(source,live,waiting,signal)),"expired")
  assert.equal(answers().length,0)
})

test("the clock passing the deadline refuses without reading or sending", async () => {
  const {source,item,live,answers,signal}=setup()
  const waiting=await readWaiting(source,live,item,signal)
  assert.equal(await reason(allowOnce(source,live,waiting,signal,()=>Date.parse("2100-01-01T00:00:00Z"))),"expired")
  assert.equal(answers().length,0)
})

test("an offer answered on another device is refused before sending", async () => {
  const {source,item,live,answers,offer,publish,load,signal}=setup()
  const waiting=await load()
  // The fixture lists pending offers only; an answered one disappears from the detail read.
  publish("permission_answered",offer({state:"answered",selected_option_id:"yes"}))
  assert.ok(["answered","changed"].includes(await reason(allowOnce(source,live,waiting,signal))))
  assert.equal(answers().length,0)
  assert.equal(await reason(readWaiting(source,live,item,signal)),"none")
})

test("a source that lost its authorization is refused before sending", async () => {
  const {source,item,live,answers,signal}=setup()
  const waiting=await readWaiting(source,live,item,signal)
  source.setState("revoked")
  assert.equal(await reason(allowOnce(source,live,waiting,signal)),"revoked")
  assert.equal(await reason(allowOnce(source,()=>[],waiting,signal)),"revoked")
  assert.equal(answers().length,0)
})

test("a read-only source shows the card but cannot allow", async () => {
  const {source,item,live,answers,signal}=setup()
  source.setState("online","history_only")
  const waiting=await readWaiting(source,live,item,signal)
  assert.equal(waiting.writable,false)
  assert.equal(await reason(allowOnce(source,live,waiting,signal)),"read_only")
  assert.equal(answers().length,0)
})

test("a network failure before sending says offline and sends nothing", async () => {
  const {source,item,live,answers,signal}=setup()
  const waiting=await readWaiting(source,live,item,signal)
  source.setState("offline")
  assert.equal(await reason(allowOnce(source,live,waiting,signal)),"offline")
  assert.equal(answers().length,0)
})

test("a network failure while sending is unknown and is not retried", async () => {
  const {source,item,live,answers,signal}=setup()
  const waiting=await readWaiting(source,live,item,signal)
  const real=source.answer.bind(source)
  let calls=0
  source.answer=async(r,s)=>{calls++;await real(r,s);throw new TypeError("Failed to fetch")}
  assert.equal(await reason(allowOnce(source,live,waiting,signal)),"unknown")
  assert.equal(calls,1)
  assert.equal(answers().length,1)
})

test("losing the grant after the answer left is unknown, not revoked", async () => {
  const {source,item,live,signal}=setup()
  const waiting=await readWaiting(source,live,item,signal)
  let sources=[source]
  const real=source.answer.bind(source)
  source.answer=async(r,s)=>{const answer=await real(r,s);sources=[];return answer}
  assert.equal(await reason(allowOnce(source,()=>sources,waiting,signal)),"unknown")
})

test("an uncertain receipt from the source is unknown", async () => {
  const {source,item,live,signal}=setup()
  const waiting=await readWaiting(source,live,item,signal)
  const real=source.answer.bind(source)
  source.answer=async(r,s)=>{const answer=await real(r,s);return {...answer,receipt:{...answer.receipt,state:"unknown"}}}
  assert.equal(await reason(allowOnce(source,live,waiting,signal)),"unknown")
})

test("a new turn or generation since the card loaded is a change, not an answer", async () => {
  const {source,item,live,answers,signal}=setup()
  const waiting=await readWaiting(source,live,item,signal)
  source.configureViews({detailFenceChange:true})
  assert.equal(await reason(allowOnce(source,live,waiting,signal)),"changed")
  assert.equal(answers().length,0)
})

test("an offer without allow once is answered in the chat only", async () => {
  const {source,item,live,answers,offer,publish,load,signal}=setup()
  await load()
  publish("permission_offered",offer({options:[{id:"always",kind:"allow_always",label:"Always",allow:true,read_only:true},{id:"no",kind:"reject_once",label:"Deny",allow:false,read_only:true}]}))
  const waiting=await readWaiting(source,live,item,signal)
  assert.equal(waiting.once,undefined)
  assert.equal(await reason(allowOnce(source,live,waiting,signal)),"changed")
  assert.equal(answers().length,0)
})

test("a source that reports the offer answered is refused as answered", async () => {
  const {source,item,live,answers,signal}=setup()
  const waiting=await readWaiting(source,live,item,signal)
  const real=source.view.bind(source)
  source.view=async(r,s)=>{const page=await real(r,s);if(r.mode==="detail"&&page.items[0]?.permission)page.items[0].permission={...page.items[0].permission,state:"answered",selected_option_id:"no"};return page}
  assert.equal(await reason(allowOnce(source,live,waiting,signal)),"answered")
  assert.equal(answers().length,0)
})
