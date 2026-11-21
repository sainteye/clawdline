/** Inject hidden-on-entry into the real roster, Cloud decoder, HTTP Host and SQLite. */
import assert from "node:assert/strict"
import {createServer} from "node:http"
import {spawn} from "node:child_process"
import {access,mkdir,mkdtemp,readFile,readdir,rm,writeFile} from "node:fs/promises"
import {constants} from "node:fs"
import {userInfo,tmpdir} from "node:os"
import path from "node:path"
import {fileURLToPath} from "node:url"
import {build} from "esbuild"

const {BOTS_CLOUD_ORIGIN:cloud,BOTS_MACHINE:machine,BOTS_ARTIFACTS:artifacts,BOTS_BASELINE:baseline}=process.env
assert.ok(cloud&&machine&&artifacts,"requires the task-owned Go roster fixture")
await mkdir(artifacts,{recursive:true})
const root=fileURLToPath(new URL("..",import.meta.url)),fixture=path.join(root,"tools/fixtures/project-assistants-roster-entry.tsx")
async function testBrowser(){
  if(process.env.CHROME){await access(process.env.CHROME,constants.X_OK);return process.env.CHROME}
  const cache=process.env.PLAYWRIGHT_BROWSERS_PATH??(process.platform==="darwin"?path.join(userInfo().homedir,"Library/Caches/ms-playwright"):process.platform==="win32"?path.join(process.env.LOCALAPPDATA??path.join(userInfo().homedir,"AppData/Local"),"ms-playwright"):path.join(process.env.XDG_CACHE_HOME??path.join(userInfo().homedir,".cache"),"ms-playwright"))
  const shells=(await readdir(cache,{withFileTypes:true})).filter(e=>e.isDirectory()&&/^chromium_headless_shell-\d+$/.test(e.name)).sort((a,b)=>Number(b.name.split("-").at(-1))-Number(a.name.split("-").at(-1)))
  const suffix=process.platform==="darwin"?"chrome-mac/headless_shell":process.platform==="win32"?"chrome-win/headless_shell.exe":"chrome-linux/headless_shell"
  for(const shell of shells){const executable=path.join(cache,shell.name,suffix);try{await access(executable,constants.X_OK);return executable}catch{}}
  throw Error("No installed headless shell. This harness installs nothing.")
}
const files=new Map(),traffic=[],results=[]
for(const name of baseline?["current","baseline"]:["current"]){
  const contents=(await readFile(fixture,"utf8")).replaceAll('"../../src/',JSON.stringify(name==="baseline"?path.join(baseline,"web/console/src/"):path.join(root,"src/")).slice(0,-1))
  const bundle=await build({stdin:{contents,resolveDir:root,loader:"tsx"},bundle:true,write:false,outfile:name+".js",format:"esm",target:"es2022",platform:"browser",jsx:"automatic",nodePaths:[path.join(root,"node_modules"),path.join(root,"../node_modules")],define:{"process.env.NODE_ENV":'"development"',READ_MACHINE:JSON.stringify(machine)}})
  for(const file of bundle.outputFiles)files.set("/"+path.basename(file.path),file.contents)
}
const server=createServer(async(req,res)=>{
  try{
    const url=new URL(req.url,"http://fixture")
    if(url.pathname==="/relay"){
      const chunks=[];for await(const chunk of req)chunks.push(chunk)
      const raw=Buffer.concat(chunks).toString(),envelope=JSON.parse(raw)
      const record={scenario:url.searchParams.get("scenario"),word:envelope.type,acceptedAt:performance.now()};traffic.push(record)
      if(record.scenario==="offline"&&envelope.type==="project-assistants.list"){
        record.status=503;record.returnedAt=performance.now();res.writeHead(503,{"Content-Type":"application/json"});res.end(JSON.stringify({body:{contract_version:"project-assistants.v1",error:{code:"pa_offline"}}}));return
      }
      const answer=await fetch(cloud,{method:"POST",body:raw}),payload=await answer.text()
      record.status=answer.status;record.returnedAt=performance.now();res.writeHead(answer.status,{"Content-Type":"application/json"});res.end(payload);return
    }
    if(url.pathname.startsWith("/catalog/")){res.setHeader("Content-Type","application/json");res.end(await readFile(path.join(root,"public/catalogs",path.basename(url.pathname))));return}
    if(files.has(url.pathname)){res.setHeader("Content-Type",url.pathname.endsWith(".css")?"text/css":"text/javascript");res.end(files.get(url.pathname));return}
    const name=url.searchParams.get("tree")==="baseline"?"baseline":"current"
    res.setHeader("Content-Type","text/html");res.end(`<!doctype html><html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Synthetic roster entry proof</title><style>html,body,#app{height:100%;margin:0}</style><link rel="stylesheet" href="/${name}.css"><div id="app"></div><script type="module" src="/${name}.js"></script></html>`)
  }catch(error){res.writeHead(500);res.end(JSON.stringify({error:String(error)}))}
})
await new Promise(resolve=>server.listen(0,"127.0.0.1",resolve))
const origin=`http://127.0.0.1:${server.address().port}`,profile=await mkdtemp(path.join(tmpdir(),"clawdline-roster-entry-")),executable=await testBrowser()
const chrome=spawn(executable,["--headless=new","--remote-debugging-port=0","--use-mock-keychain","--user-data-dir="+profile,"--no-first-run","--no-default-browser-check","--disable-features=OmniboxEverywhere","about:blank"],{stdio:["ignore","ignore","pipe"]})
let ws
try{
  const endpoint=await new Promise((resolve,reject)=>{let output="";const timer=setTimeout(()=>reject(Error("Chrome startup: "+output)),20000);chrome.once("error",reject);chrome.once("exit",(code,signal)=>{clearTimeout(timer);reject(Error("Chrome exited: "+code+" "+signal+" "+output))});chrome.stderr.on("data",chunk=>{output+=chunk;const match=/DevTools listening on (ws:\/\/\S+)/.exec(output);if(match){clearTimeout(timer);resolve(match[1])}})})
  ws=new WebSocket(endpoint);await new Promise((resolve,reject)=>{ws.addEventListener("open",resolve);ws.addEventListener("error",reject)})
  let serial=0;const pending=new Map()
  ws.addEventListener("message",event=>{const message=JSON.parse(String(event.data)),job=pending.get(message.id);if(!job)return;pending.delete(message.id);message.error?job.reject(Error(message.error.message)):job.resolve(message.result)})
  const send=(method,params={},sessionId)=>new Promise((resolve,reject)=>{const id=++serial,timer=setTimeout(()=>{pending.delete(id);reject(Error(method+" timed out"))},15000);pending.set(id,{resolve:v=>{clearTimeout(timer);resolve(v)},reject:e=>{clearTimeout(timer);reject(e)}});ws.send(JSON.stringify({id,method,params,sessionId}))})
  const evaluate=async(sessionId,expression)=>{const reply=await send("Runtime.evaluate",{expression,awaitPromise:true,returnByValue:true},sessionId);if(reply.exceptionDetails)throw Error(reply.exceptionDetails.exception?.description??reply.exceptionDetails.text);return reply.result.value}
  const until=async(sessionId,expression)=>{const deadline=Date.now()+12000;while(Date.now()<deadline){if(await evaluate(sessionId,expression))return;await new Promise(r=>setTimeout(r,50))}throw Error("UI wait: "+expression+"\n"+await evaluate(sessionId,"document.body.innerText"))}
  const capture=async(sessionId,name)=>{
    const state=await evaluate(sessionId,"({visibility:document.visibilityState,rows:document.querySelectorAll('[data-bot-key]').length,loading:!!document.querySelector('[data-loading-id=\"bots-roster\"]'),phases:window.rosterEntry.phases,text:document.body.innerText,html:document.documentElement.outerHTML})")
    const shot=await send("Page.captureScreenshot",{format:"png"},sessionId)
    await writeFile(path.join(artifacts,name+".png"),Buffer.from(shot.data,"base64"));await writeFile(path.join(artifacts,name+".json"),JSON.stringify(state,null,2));return state
  }
  for(const [tree,scenario,lang,width,height]of [...(baseline?[["baseline","ready","en",1280,800]]:[]),["current","ready","en",1280,800],["current","ready","zh-Hant",1280,800],["current","ready","en",390,844],["current","ready","zh-Hant",390,844],["current","offline","en",1280,800],["current","offline","zh-Hant",390,844]]){
    const start=traffic.length,{targetId}=await send("Target.createTarget",{url:"about:blank"}),{sessionId}=await send("Target.attachToTarget",{targetId,flatten:true}),name=[tree,scenario,lang,width].join("-")
    await send("Page.enable",{},sessionId);await send("Runtime.enable",{},sessionId)
    await send("Emulation.setDeviceMetricsOverride",{width,height,deviceScaleFactor:1,mobile:width<600},sessionId)
    await send("Page.navigate",{url:origin+"/?"+new URLSearchParams({tree,scenario,lang})},sessionId)
    await until(sessionId,"!!document.querySelector('.pa-roster')&&!!window.rosterEntry")
    if(tree==="baseline"){
      await new Promise(r=>setTimeout(r,250))
      const state=await capture(sessionId,name);assert.equal(state.visibility,"hidden");assert.equal(state.loading,true);assert.equal(state.rows,0)
      assert.equal(state.phases.some(p=>p.phase==="emitted"&&/\.(status|list)$/.test(p.word)),false)
      results.push({tree,scenario,lang,width,hidden:true,loading:true,rosterRequests:0})
    }else{
      await until(sessionId,scenario==="ready"?"document.querySelectorAll('[data-bot-key]').length>0&&!document.querySelector('[data-loading-id=\"bots-roster\"]')":"document.querySelector('.pa-empty[role=status]')&&!document.querySelector('[data-loading-id=\"bots-roster\"]')")
      const state=await capture(sessionId,name),reads=traffic.slice(start).filter(p=>/\.(status|list)$/.test(p.word))
      assert.equal(state.visibility,"hidden");assert.equal(state.loading,false)
      if(scenario==="ready"){
        assert.deepEqual(reads.map(r=>[r.word,r.status]),[["project-assistants.list",200]])
        assert.ok(state.phases.find(p=>p.phase==="snapshot-received"));assert.ok(state.rows>0)
      }else{
        assert.deepEqual(reads.map(r=>[r.word,r.status]),[["project-assistants.list",503]])
        assert.equal(state.rows,0);assert.ok(await evaluate(sessionId,"!!document.querySelector('.pa-empty[role=status]')&&!document.querySelector('.pa-source-status,.pa-status-bar')"))
      }
      results.push({tree,scenario,lang,width,hidden:true,loading:false,rows:state.rows,reads,phases:state.phases})
    }
    await send("Target.closeTarget",{targetId})
  }
  console.log(JSON.stringify({cases:results.filter(r=>r.tree==="current").length,baseline:!!baseline,providerRequests:0,results,boundary:"controlled visibility fault; real roster, captured relay client, closed decoder, HTTP Host and SQLite; outer encrypted carrier simulated"}))
}finally{
  await writeFile(path.join(artifacts,"traffic.json"),JSON.stringify(traffic,null,2));await writeFile(path.join(artifacts,"results.json"),JSON.stringify(results,null,2))
  ws?.close();chrome.kill("SIGTERM");await new Promise(resolve=>{if(chrome.exitCode!==null)return resolve();const timer=setTimeout(()=>{chrome.kill("SIGKILL");resolve()},3000);chrome.once("exit",()=>{clearTimeout(timer);resolve()})});server.closeAllConnections();await new Promise(resolve=>server.close(resolve));await rm(profile,{recursive:true,force:true})
}
