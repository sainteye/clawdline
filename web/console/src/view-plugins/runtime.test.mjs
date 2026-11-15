import assert from "node:assert/strict"
import test from "node:test"
import { readFile, mkdtemp, rm } from "node:fs/promises"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { build } from "esbuild"
import { spawnSync } from "node:child_process"
import { fileURLToPath } from "node:url"
import { pathToFileURL } from "node:url"

const scratch = await mkdtemp(join(tmpdir(), "cl-view-unit-"))
const bundle = join(scratch,"runtime.mjs")
await build({stdin:{contents:'export * from "./package.js";export * from "./bridge.js";export * from "./sources.js"',resolveDir:new URL(".",import.meta.url).pathname,loader:"ts"},bundle:true,platform:"node",format:"esm",outfile:bundle})
const {parsePackage,pluginBridge,PluginError,cloudPluginSources,fleetPluginSource} = await import(pathToFileURL(bundle).href)
test.after(() => rm(scratch,{recursive:true,force:true}))
const packageURL = new URL("../../public/view-plugins/session-galaxy.zip",import.meta.url)
const context = {language:"en",theme:"dark",reducedMotion:true,visible:true,scopeLabel:"Studio",connection:"connected"}
const row = {key:"private-machine/execution",title:"<img src=x onerror=alert(1)>",project:{key:"/private/project",label:"Demo"},workState:"working",freshness:"fresh"}
const reading = {sessions:[row],connected:true}
const tick = () => new Promise(r => setTimeout(r,15))
function fixture(capabilities=["sessions.read","sessions.open"], initial=reading) {
  const channel = new MessageChannel(), answers=[], opened=[], faults=[]; let closes=0
  channel.port2.onmessage = e => answers.push(e.data)
  const bridge = pluginBridge({port:channel.port1,instanceId:"instance",capabilities,reading:initial,context,open:async key => {opened.push(key)},close:()=>closes++,fault:e=>faults.push(e)})
  const send = (method,params={},extra={}) => channel.port2.postMessage({v:1,instanceId:"instance",id:"r"+Math.random(),method,params,...extra})
  return {channel,bridge,answers,opened,faults,send,get closes(){return closes},dispose(){bridge.dispose();channel.port2.close()}}
}
test("The shipped ZIP parses as an independent self-contained package",async()=>{
  const pkg=await parsePackage(await readFile(packageURL))
  assert.equal(pkg.manifest.id,"com.clawdline.session-galaxy")
  assert.ok(pkg.html.includes("connectViewPlugin"));assert.ok(!pkg.html.includes("MockHost"));assert.ok(!pkg.html.includes("<!-- CLAWDLINE_SDK -->"))
  const list=await parsePackage(await readFile(new URL("../../public/view-plugins/session-list.zip",import.meta.url)))
  assert.notEqual(list.manifest.id,pkg.manifest.id)
})
test("ZIP corruption, compressed/encrypted archives and overflow are refused before execution",async()=>{
  const original=await readFile(packageURL)
  const bad=Buffer.from(original);bad[80]^=1
  await assert.rejects(parsePackage(bad),PluginError)
  const compression=Buffer.from(original),central=compression.indexOf(Buffer.from([0x50,0x4b,0x01,0x02]));compression.writeUInt16LE(8,central+10)
  await assert.rejects(parsePackage(compression),e=>e.code==="unsupported_zip")
  const encryption=Buffer.from(original);encryption.writeUInt16LE(1,central+8)
  await assert.rejects(parsePackage(encryption),e=>e.code==="unsupported_zip")
  await assert.rejects(parsePackage(new Uint8Array(262145)),e=>e.code==="package_too_large")
  await assert.rejects(parsePackage(original.subarray(0,-1)),PluginError)
})
test("Initial subscribe precedes updates, refs hide private identity, deleted refs cannot navigate",async()=>{
  const f=fixture();try{
    f.send("sessions.subscribe");await tick()
    const initial=f.answers[0].result, ref=initial.sessions[0].ref
    assert.equal(initial.sessions[0].title,row.title);assert.ok(!JSON.stringify(initial).includes("private"))
    f.bridge.update({...reading,sessions:[]},context);await tick()
    assert.equal(f.answers[1].event,"host.context")
    assert.equal(f.answers[2].event,"sessions.snapshot");assert.deepEqual(f.answers[2].data.sessions,[])
    f.send("sessions.open",{ref});await tick();assert.equal(f.answers.at(-1).error.code,"session_unavailable");assert.deepEqual(f.opened,[])
  }finally{f.dispose()}
})
test("Capabilities, exact request fields, instance fences and disconnection restrict navigation",async()=>{
  const f=fixture(["sessions.read"]);try{
    f.send("sessions.subscribe",{},{instanceId:"old"});await tick();assert.equal(f.answers.length,0)
    f.send("sessions.subscribe");await tick();const ref=f.answers[0].result.sessions[0].ref
    f.send("sessions.open",{ref});await tick();assert.equal(f.answers.at(-1).error.code,"permission_denied")
    f.send("sessions.subscribe",{},{rawAPI:"/v1/send"});await tick();assert.deepEqual(f.faults,["invalid_request"])
  }finally{f.dispose()}
  const g=fixture();try{
    g.send("sessions.subscribe");await tick();const ref=g.answers[0].result.sessions[0].ref
    g.bridge.update({...reading,connected:false},{...context,connection:"disconnected"});await tick()
    assert.equal(g.answers.at(-1).data.sessions[0].freshness,"stale")
    g.send("sessions.open",{ref});await tick();assert.equal(g.answers.at(-1).error.code,"host_unavailable");assert.deepEqual(g.opened,[])
  }finally{g.dispose()}
})
test("Updates coalesce, request flood stops only that frame, a new grant gets new refs",async()=>{
 const f=fixture(),g=fixture();try{
  f.send("sessions.subscribe");g.send("sessions.subscribe");await tick()
  assert.notEqual(f.answers[0].result.sessions[0].ref,g.answers[0].result.sessions[0].ref)
  for(let i=0;i<10;i++)f.bridge.update({...reading,sessions:[{...row,title:String(i)}]},context)
  await tick();assert.equal(f.answers.filter(x=>x.event==="sessions.snapshot").length,1);assert.equal(f.answers.at(-1).data.sessions[0].title,"9")
  for(let i=0;i<33;i++)f.send("sessions.subscribe")
  await tick();assert.deepEqual(f.faults,["busy"]);assert.deepEqual(g.faults,[])
 }finally{f.dispose();g.dispose()}
})
test("Opening acknowledges a validated navigation and asks the host to close",async()=>{
 const f=fixture();try{f.send("sessions.subscribe");await tick();f.send("sessions.open",{ref:f.answers[0].result.sessions[0].ref});await tick();assert.deepEqual(f.opened,[row.key]);assert.equal(f.closes,1);assert.deepEqual(f.answers.at(-1).result,{opened:true})}finally{f.dispose()}
})
test("Cloud sources refuse a revoked owner across a delayed authorized read",async()=>{
 let current=true,release;const wait=new Promise(r=>release=r)
 const [source]=cloudPluginSources({owner:"owner",machines:[{id:"studio",label:"Studio",pairing:"paired",selectable:true,operationAllowed:true}],current:()=>current,open(){throw Error("must not navigate")},source:{async readMachine(){await wait;return{kind:"ready",rows:[]}},async readMachinePresentations(){throw Error("must not read names")},subscribe(){return()=>{}}}})
 const pending=source.read(new AbortController().signal);current=false;release();await assert.rejects(pending,e=>e.code==="scope_changed")
 assert.deepEqual(cloudPluginSources({owner:"owner",machines:[{id:"studio",label:"Studio",pairing:"paired",selectable:true,operationAllowed:null}],source:{},current:()=>true,open(){}}),[])
})

test("A scope overflow fails before exposing a partial initial list",()=>{
 const channel=new MessageChannel()
 try{assert.throws(()=>pluginBridge({port:channel.port1,instanceId:"i",capabilities:["sessions.read"],reading:{sessions:Array.from({length:257},(_,n)=>({...row,key:String(n)})),connected:true},context,open:async()=>{},close(){},fault(){}}),e=>e.code==="too_many_sessions")}
 finally{channel.port1.close();channel.port2.close()}
})
test("A second navigation cannot override an in-flight destination",async()=>{
 const channel=new MessageChannel(),answers=[],opened=[];let release
 const waiting=new Promise(r=>release=r)
 channel.port2.onmessage=e=>answers.push(e.data)
 const bridge=pluginBridge({port:channel.port1,instanceId:"i",capabilities:["sessions.read","sessions.open"],reading,context,open:async key=>{opened.push(key);await waiting},close(){},fault(){}})
 const send=(id,method,params={})=>channel.port2.postMessage({v:1,instanceId:"i",id,method,params})
 try{send("a","sessions.subscribe");await tick();const ref=answers[0].result.sessions[0].ref
 send("b","sessions.open",{ref});send("c","sessions.open",{ref});await tick()
 assert.equal(answers.at(-1).error.code,"busy");assert.deepEqual(opened,[row.key]);release();await tick();assert.deepEqual(answers.at(-1).result,{opened:true})
 }finally{release();bridge.dispose();channel.port2.close()}
})

test("Manifest negotiation refuses added capabilities and missing required fields",async()=>{
 const {validateManifest}=await import(pathToFileURL(bundle).href)
 const good={manifestVersion:1,id:"org.example.map",version:"1.0.0",hostApi:1,name:{en:"Map","zh-Hant":"地圖"},entry:"index.html",capabilities:["sessions.read"]}
 assert.deepEqual(validateManifest(good),good)
 for(const bad of [{...good,capabilities:["sessions.read","sessions.send"]},{...good,capabilities:[]},{...good,entry:"../index.html"},{...good,name:{en:"Map"}},{...good,version:"tomorrow"},{...good,extra:"hidden"}])assert.throws(()=>validateManifest(bad),PluginError)
 assert.throws(()=>validateManifest({...good,hostApi:2}),e=>e.code==="protocol_mismatch")
})
test("The archive refuses traversal names, aliased entries and symlinks",async()=>{
 const original=await readFile(packageURL),signature=Buffer.from([0x50,0x4b,0x01,0x02]),central=original.indexOf(signature)
 const symlink=Buffer.from(original);symlink.writeUInt32LE(0xa1ff0000,central+38);await assert.rejects(parsePackage(symlink),PluginError)
 const traversal=Buffer.from(original),replacement=Buffer.from("../index.xxxx");assert.equal(replacement.length,13)
 replacement.copy(traversal,30);replacement.copy(traversal,central+46);await assert.rejects(parsePackage(traversal),PluginError)
 const duplicate=Buffer.from(original),second=duplicate.indexOf(signature,central+1);duplicate.writeUInt32LE(0,second+42)
 await assert.rejects(parsePackage(duplicate),PluginError)
})


test("The public packer reproduces both shipped ZIPs from author sources and the SDK",async()=>{
 const root=fileURLToPath(new URL("../../../../",import.meta.url))
 for(const id of ["session-galaxy","session-list"]){
  const out=join(scratch,id+".zip"),result=spawnSync("python3",["tools/pack-view-plugin.py","plugins/"+id,out],{cwd:root,encoding:"utf8"})
  assert.equal(result.status,0,result.stderr)
  assert.deepEqual(await readFile(out),await readFile(new URL("../../public/view-plugins/"+id+".zip",import.meta.url)))
 }
})


test("The fleet combines all machines and Projects without colliding local identities",async()=>{
 const opened=[],events=new Set(),controller=new AbortController()
 const make=(key,connected=true)=>({key,label:key,async read(signal){assert.equal(signal,controller.signal);return{sessions:[{...row,key:"same-session"}],connected}},subscribe(fn){events.add(fn);return()=>events.delete(fn)},async prepareOpen(key,signal){assert.equal(signal,controller.signal);return()=>opened.push([this.key,key])}})
 const one=make("machine-one"),two=make("machine-two"),fleet=fleetPluginSource([two,one],"All machines")
 assert.equal(fleet.key,fleetPluginSource([one,two],"Other language").key)
 const value=await fleet.read(controller.signal)
 assert.equal(value.sessions.length,2);assert.equal(value.connected,true);assert.equal(value.partial,false)
 assert.notEqual(value.sessions[0].key,value.sessions[1].key)
 assert.notEqual(value.sessions[0].project.key,value.sessions[1].project.key)
 assert.deepEqual(value.sessions.map(s=>s.project.label),["Demo","Demo"])
 const navigate=await fleet.prepareOpen(value.sessions[1].key,controller.signal);navigate()
 assert.deepEqual(opened,[["machine-two","same-session"]])
 let calls=0;const a=()=>calls++,b=()=>calls++;one.subscribe=fn=>{events.add(a);return()=>events.delete(a)};two.subscribe=fn=>{events.add(b);return()=>events.delete(b)}
 const stop=fleet.subscribe(()=>{});assert.equal(events.size,2);for(const event of events)event();assert.equal(calls,2);stop();assert.equal(events.size,0)
 const f=fixture(undefined,value)
 try{f.send("sessions.subscribe");await tick();const payload=f.answers[0].result
  assert.notEqual(payload.sessions[0].ref,payload.sessions[1].ref)
  assert.notEqual(payload.sessions[0].project.ref,payload.sessions[1].project.ref)
  assert.ok(!JSON.stringify(payload).includes("machine-one"));assert.ok(!JSON.stringify(payload).includes("private"))
 }finally{f.dispose()}
})

test("One offline machine stays stale while another can still open its own Session",async()=>{
 const make=(key,connected)=>({key,label:key,async read(){return{sessions:[{...row,key}],connected}},subscribe(){return()=>{}},async prepareOpen(){return()=>{}}})
 const fleet=fleetPluginSource([make("online",true),make("offline",false)],"All machines")
 const value=await fleet.read(new AbortController().signal)
 assert.equal(value.connected,true);assert.equal(value.partial,true)
 const f=fixture(undefined,value)
 try{f.send("sessions.subscribe");await tick();const items=f.answers[0].result.sessions
  const offline=items[value.sessions.findIndex(s=>!s.connected)],online=items[value.sessions.findIndex(s=>s.connected)]
  assert.equal(offline.freshness,"stale");assert.equal(online.freshness,"fresh")
  f.send("sessions.open",{ref:offline.ref});await tick();assert.equal(f.answers.at(-1).error.code,"host_unavailable");assert.deepEqual(f.opened,[])
  f.send("sessions.open",{ref:online.ref});await tick();assert.deepEqual(f.opened,[value.sessions.find(s=>s.connected).key])
 }finally{f.dispose()}
})

test("Fleet read failures are explicit and authorization failures never become partial success",async()=>{
 const controller=new AbortController(),good={key:"good",label:"Good",async read(){return reading},subscribe(){return()=>{}},async prepareOpen(){return()=>{}}}
 const broken=code=>({...good,key:code,async read(){throw new PluginError(code)}})
 const partial=await fleetPluginSource([good,broken("host_unavailable")],"All").read(controller.signal)
 assert.equal(partial.sessions.length,1);assert.equal(partial.connected,true);assert.equal(partial.partial,true)
 for(const code of ["permission_denied","scope_changed"])await assert.rejects(fleetPluginSource([good,broken(code)],"All").read(controller.signal),e=>e.code===code)
 await assert.rejects(fleetPluginSource([broken("host_unavailable")],"All").read(controller.signal),e=>e.code==="host_unavailable")
 await assert.rejects(fleetPluginSource([],"All").read(controller.signal),e=>e.code==="permission_denied")
 controller.abort();await assert.rejects(fleetPluginSource([good],"All").read(controller.signal),e=>e.code==="scope_changed")
 const many=key=>({...good,key,async read(){return{...reading,sessions:Array.from({length:129},(_,i)=>({...row,key:String(i)}))}}})
 const overflow=await fleetPluginSource([many("one"),many("two")],"All").read(new AbortController().signal),channel=new MessageChannel()
 try{assert.throws(()=>pluginBridge({port:channel.port1,instanceId:"i",capabilities:["sessions.read"],reading:overflow,context,open:async()=>{},close(){},fault(){}}),e=>e.code==="too_many_sessions")}
 finally{channel.port1.close();channel.port2.close()}
})

test("An offline row is refused before the coalesced fleet snapshot is delivered",async()=>{
 const replies=[],opened=[],port={start(){},close(){},postMessage(value){replies.push(value)}}
 const initial={sessions:[{...row,key:"offline-next",connected:true},{...row,key:"still-online",connected:true}],connected:true}
 const bridge=pluginBridge({port,instanceId:"fleet",capabilities:["sessions.read","sessions.open"],reading:initial,context,open:async key=>opened.push(key),close(){},fault(){}})
 const send=(id,method,params={})=>port.onmessage({data:{v:1,instanceId:"fleet",id,method,params}})
 try{
  await send("subscribe","sessions.subscribe");const ref=replies[0].result.sessions[0].ref
  bridge.update({...initial,partial:true,sessions:initial.sessions.map((row,index)=>({...row,connected:index!==0}))},context)
  await send("open","sessions.open",{ref})
  assert.equal(replies.at(-1).error?.code,"host_unavailable");assert.deepEqual(opened,[])
 }finally{bridge.dispose()}
})
