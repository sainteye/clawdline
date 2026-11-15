import assert from "node:assert/strict"
import test from "node:test"
import { readFile, writeFile, readdir, mkdtemp, rm } from "node:fs/promises"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { build } from "esbuild"
import { spawnSync } from "node:child_process"
import { fileURLToPath } from "node:url"
import { pathToFileURL } from "node:url"
import { deflateSync, crc32 as pngCRC } from "node:zlib"

const scratch = await mkdtemp(join(tmpdir(), "cl-view-unit-"))
const bundle = join(scratch,"runtime.mjs")
await build({stdin:{contents:'export * from "./package.js";export * from "./bridge.js";export * from "./sources.js";export * from "./store.js";export {packageCover,exampleCover,curatedPackageIDs,hasCuratedUpdate} from "./Artwork.js"',resolveDir:new URL(".",import.meta.url).pathname,loader:"ts"},bundle:true,platform:"node",format:"esm",outfile:bundle})
const {parsePackage,pluginBridge,PluginError,cloudPluginSources,fleetPluginSource,installPackage,packageCover,exampleCover,curatedPackageIDs,hasCuratedUpdate} = await import(pathToFileURL(bundle).href)
test.after(() => rm(scratch,{recursive:true,force:true}))
const packageURL = new URL("../../public/view-plugins/session-galaxy.zip",import.meta.url)
const context = {language:"en",theme:"dark",reducedMotion:true,visible:true,scopeLabel:"Studio",connection:"connected"}
const row = {key:"private-machine/execution",title:"<img src=x onerror=alert(1)>",project:{key:"/private/project",label:"Demo"},workState:"working",freshness:"fresh"}
const reading = {sessions:[row],connected:true}
const tick = () => new Promise(r => setTimeout(r,15))
function changedManifest(archive, patch) {
 const script=`import sys,json,io,base64,zipfile
p=json.load(sys.stdin)
with zipfile.ZipFile(io.BytesIO(base64.b64decode(p['zip']))) as z:
 files={n:z.read(n) for n in z.namelist()}
m=json.loads(files['manifest.json'])
for k,v in p['patch'].items():
 if v is None: m.pop(k,None)
 else: m[k]=v
files['manifest.json']=json.dumps(m).encode()
out=io.BytesIO()
with zipfile.ZipFile(out,'w',compression=zipfile.ZIP_STORED) as z:
 for n,b in files.items(): z.writestr(n,b)
sys.stdout.buffer.write(out.getvalue())`
 const run=spawnSync("python3",["-c",script],{input:JSON.stringify({zip:Buffer.from(archive).toString("base64"),patch})})
 assert.equal(run.status,0,run.stderr.toString());return run.stdout
}
test("Retired built-in views cannot be imported or executed from old archives",async()=>{
 const original=await readFile(packageURL)
 for(const id of ["com.clawdline.session-list","com.clawdline.session-pop-art","com.clawdline.session-absurd"]){
  const archive=changedManifest(original,{id})
  await assert.rejects(parsePackage(archive),e=>e.code==="retired_package")
  await assert.rejects(parsePackage(archive,{allowLegacy:true}),e=>e.code==="retired_package")
  await assert.rejects(installPackage(archive,null),e=>e.code==="retired_package")
 }
 const custom=await parsePackage(changedManifest(original,{id:"org.example.community"}))
 assert.equal(custom.manifest.id,"org.example.community")
})
function coverPNG(side=2, height=side, filter=0, options={}) {
 const chunk=(kind,body)=>{const type=Buffer.from(kind),head=Buffer.alloc(4),tail=Buffer.alloc(4);head.writeUInt32BE(body.length);tail.writeUInt32BE(pngCRC(Buffer.concat([type,body])));return Buffer.concat([head,type,body,tail])}
 const head=Buffer.alloc(13);head.writeUInt32BE(side);head.writeUInt32BE(height,4);head[8]=8;head[9]=6
 const scan=Buffer.alloc(height*(side*4+1)+(options.extraBytes||0),255);for(let n=0;n<height;n++)scan[n*(side*4+1)]=filter
 const compressed=options.badDeflate?Buffer.from("not deflate"):deflateSync(scan)
 const bytes=Buffer.concat([Buffer.from([137,80,78,71,13,10,26,10]),chunk("IHDR",head),...(options.animated?[chunk("acTL",Buffer.alloc(8))]:[]),chunk("IDAT",compressed),chunk("IEND",Buffer.alloc(0))])
 if(options.badCRC)bytes[29]^=1
 return "data:image/png;base64,"+bytes.toString("base64")
}
test("New packages require a valid embedded cover before they can be installed",async()=>{
 const original=await readFile(packageURL),missing=changedManifest(original,{cover:null})
 await assert.rejects(parsePackage(missing),e=>e.code==="missing_cover")
 await assert.rejects(installPackage(missing,null),e=>e.code==="missing_cover","Installation refuses before opening browser storage")
 for(const cover of ["", "https://example.invalid/cover.png", "data:image/svg+xml,<svg/>", "data:image/png;base64,bm90LWFuLWltYWdl",coverPNG(257),coverPNG(2,3),coverPNG(2,2,5),coverPNG(2,2,0,{badCRC:true}),coverPNG(2,2,0,{badDeflate:true}),coverPNG(2,2,0,{extraBytes:1}),coverPNG(2,2,0,{extraBytes:-1}),coverPNG(2,2,0,{animated:true})]) {
  await assert.rejects(parsePackage(changedManifest(original,{cover})),e=>e.code==="invalid_cover")
 }
 const valid=await parsePackage(changedManifest(original,{cover:coverPNG()}))
 assert.ok(valid.manifest.cover.startsWith("data:image/png;base64,"))
 const legacy=await parsePackage(missing,{allowLegacy:true})
 assert.equal(legacy.manifest.cover,undefined,"Reading an existing coverless archive remains possible without permitting new installation")
})
test("The packer refuses missing covers and does not overwrite an existing archive",async()=>{
 const source=await mkdtemp(join(scratch,"author-")),output=join(source,"view.zip")
 const original=await parsePackage(await readFile(packageURL)),manifest={...original.manifest}
 delete manifest.cover
 await writeFile(join(source,"manifest.json"),JSON.stringify(manifest));await writeFile(join(source,manifest.entry),"<!doctype html><title>Example</title>")
 await writeFile(output,"keep existing archive")
 const packer=fileURLToPath(new URL("../../../../tools/pack-view-plugin.py",import.meta.url))
 const rejected=spawnSync("python3",[packer,source,output])
 assert.notEqual(rejected.status,0);assert.match(rejected.stderr.toString(),/cover/i)
 assert.equal(await readFile(output,"utf8"),"keep existing archive")
 for(const cover of ["https://example.invalid/art.png",coverPNG(257),coverPNG(2,2,0,{badCRC:true}),coverPNG(2,2,0,{badDeflate:true}),coverPNG(2,2,0,{extraBytes:1}),coverPNG(2,2,0,{animated:true})]) {
  manifest.cover=cover;await writeFile(join(source,"manifest.json"),JSON.stringify(manifest))
  const invalid=spawnSync("python3",[packer,source,output]);assert.notEqual(invalid.status,0);assert.match(invalid.stderr.toString(),/cover/i)
  assert.equal(await readFile(output,"utf8"),"keep existing archive")
 }
 await writeFile(join(source,"manifest.json"),JSON.stringify({...manifest,cover:coverPNG(),name:{en:"x".repeat(8193),"zh-Hant":"Example"}}))
 const oversized=spawnSync("python3",[packer,source,output]);assert.notEqual(oversized.status,0);assert.match(oversized.stderr.toString(),/manifest/i)
 assert.equal(await readFile(output,"utf8"),"keep existing archive")
 manifest.cover=coverPNG();await writeFile(join(source,"manifest.json"),JSON.stringify(manifest))
 const accepted=spawnSync("python3",[packer,source,output]);assert.equal(accepted.status,0,accepted.stderr.toString())
 assert.equal((await parsePackage(await readFile(output))).manifest.cover,manifest.cover)
})
test("Published examples have real covers and exact legacy archives gain artwork without replacing code",async()=>{
 const sources=new URL("../../../../plugins/",import.meta.url)
 for(const entry of await readdir(sources,{withFileTypes:true})) {
  if(!entry.isDirectory())continue
  const name=entry.name
  let manifest
  try {manifest=JSON.parse(await readFile(new URL(name+"/manifest.json",sources),"utf8"))}catch(e){if(e.code==="ENOENT")continue;throw e}
  const pkg=await parsePackage(await readFile(new URL("../../public/view-plugins/"+name+".zip",import.meta.url)))
  assert.equal(pkg.manifest.cover,manifest.cover)
  if (curatedPackageIDs.includes(manifest.id)) assert.equal(exampleCover(manifest.id),manifest.cover,"A curated view must not disappear behind an absent cover")
 }
 const legacy=await parsePackage(await readFile(new URL("./testdata/legacy-session-galaxy.zip",import.meta.url)),{allowLegacy:true})
 assert.equal(legacy.manifest.cover,undefined);assert.ok(packageCover(legacy))
 assert.equal(packageCover({...legacy,digest:"unrecognized archive"}),undefined,"An unknown archive cannot impersonate a built-in's legacy cover")
 assert.equal(packageCover({...legacy,manifest:{...legacy.manifest,id:"org.example.legacy"}}),undefined)
})
test("Curated views have downloadable packages with their own validated cover",async()=>{
 assert.ok(curatedPackageIDs.length>0,"The supplied Galaxy view must remain discoverable")
 for(const id of curatedPackageIDs){
  const pkg=await parsePackage(await readFile(new URL("../../public/view-plugins/"+id.slice("com.clawdline.".length)+".zip",import.meta.url)))
  assert.equal(pkg.manifest.id,id)
  assert.ok(pkg.manifest.cover)
  assert.equal(exampleCover(id),pkg.manifest.cover)
 }
})
test("Curated updates require a newer stable release and never downgrade an installed package",async()=>{
 const pkg=await parsePackage(await readFile(packageURL))
 const atVersion=version=>({...pkg,manifest:{...pkg.manifest,version}})
 assert.equal(hasCuratedUpdate(atVersion("0.0.0")),true)
 assert.equal(hasCuratedUpdate(pkg),false)
 assert.equal(hasCuratedUpdate(atVersion("99.0.0")),false)
 assert.equal(hasCuratedUpdate(atVersion(pkg.manifest.version+"-preview.1")),true)
 assert.equal(hasCuratedUpdate(atVersion(pkg.manifest.version+"+local")),false)
 assert.equal(hasCuratedUpdate({...pkg,manifest:{...pkg.manifest,id:"org.example.private"}}),false)
})
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
 const good={manifestVersion:1,id:"org.example.map",version:"1.0.0",hostApi:1,name:{en:"Map","zh-Hant":"地圖"},entry:"index.html",capabilities:["sessions.read"],cover:coverPNG()}
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


test("The public packer reproduces all supplied ZIPs from author sources and the SDK",async()=>{
 const root=fileURLToPath(new URL("../../../../",import.meta.url))
 for(const id of ["session-galaxy"]){
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
