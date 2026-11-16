/** Real roster/room components, source clients, decrypted Cloud decoder, HTTP and SQLite. */
import assert from "node:assert/strict"
import {createServer} from "node:http"
import {spawn} from "node:child_process"
import {access,mkdir,mkdtemp,readFile,readdir,rm,writeFile} from "node:fs/promises"
import {constants} from "node:fs"
import {userInfo} from "node:os"
import path from "node:path"
import {fileURLToPath} from "node:url"
import {build} from "esbuild"

const {BOTS_ORIGIN:upstream,BOTS_CLOUD_ORIGIN:cloud,BOTS_ARTIFACTS:artifacts,BOTS_WORK:work}=process.env
assert.ok(upstream&&cloud&&artifacts&&work,"requires the task-owned Go group fixture")
await mkdir(artifacts,{recursive:true});await mkdir(work,{recursive:true})
const root=fileURLToPath(new URL("..",import.meta.url))
async function testBrowser(){
  if(process.env.CHROME){await access(process.env.CHROME,constants.X_OK);return process.env.CHROME}
  const cache=process.env.PLAYWRIGHT_BROWSERS_PATH??(process.platform==="darwin"?path.join(userInfo().homedir,"Library/Caches/ms-playwright"):process.platform==="win32"?path.join(process.env.LOCALAPPDATA??path.join(userInfo().homedir,"AppData/Local"),"ms-playwright"):path.join(process.env.XDG_CACHE_HOME??path.join(userInfo().homedir,".cache"),"ms-playwright"))
  const entries=await readdir(cache,{withFileTypes:true}),shells=entries.filter(e=>e.isDirectory()&&/^chromium_headless_shell-\d+$/.test(e.name)).sort((a,b)=>Number(b.name.split("-").at(-1))-Number(a.name.split("-").at(-1)))
  const suffix=process.platform==="darwin"?"chrome-mac/headless_shell":process.platform==="win32"?"chrome-win/headless_shell.exe":"chrome-linux/headless_shell"
  for(const shell of shells){const executable=path.join(cache,shell.name,suffix);try{await access(executable,constants.X_OK);return executable}catch{}}
  throw Error("No installed Playwright headless shell. This harness installs nothing.")
}
const words=["status","list","get","groups","group-history","group-create","group-edit","group-delete","group-send","group-stop","team-plans","team-answer","team-settings"].map(w=>"project-assistants."+w)
const bundle=await build({stdin:{contents:`
import React from "react";import {createRoot} from "react-dom/client";
import {BotRoster} from "./src/project-assistants/BotRoster";
import {ProjectAssistantSourceClient,capturedAssistantRelayFetch} from "./src/cloud/project-assistants";
import {words} from "./src/project-assistants/words";import {bootCatalog} from "./src/catalog";
import "./src/legacy/tokens.css";import "./src/project-assistants/styles.css";
const q=new URL(location.href).searchParams,mode=q.get("mode")??"local",language=q.get("lang")??"en";
localStorage.setItem("ui_language",language);await bootCatalog(tag=>"/catalog/"+tag+".json");
const grant={accountKey:mode==="cloud"?"synthetic":"local",readerID:"person",machineID:"machine",authorizationRevision:"one",apiLevel:mode==="old"?75:76,paired:true,online:true,writable:mode!=="read"};
const carried=${JSON.stringify(words)};
const relay={machineDescriptor:()=>({machine:{commands:carried}}),_machineRequest:async(_,word,fields)=>await(await fetch("/relay/"+word,{method:"POST",body:JSON.stringify(fields)})).json(),_machineRequestAs:async(id,_,word,fields)=>await(await fetch("/relay/"+word,{method:"POST",body:JSON.stringify(fields),headers:{"Idempotency-Key":id}})).json()};
const source=new ProjectAssistantSourceClient(grant,mode==="cloud"?capturedAssistantRelayFetch("machine",()=>relay,carried):(input,init)=>fetch("/proxy/"+mode+input,init),()=>grant);
await source.status(new AbortController().signal);const page=await source.list({machine_id:"machine",cursor:"",query:"",page_size:100},new AbortController().signal);
createRoot(document.getElementById("app")).render(<div className="project-assistants pa-shell"><BotRoster sources={[source]} groupsItems={page.items} items={page.items} machines={["machine"]} selected={null} query="" onQuery={()=>{}} machine="" onMachine={()=>{}} onSelect={()=>{}} onClose={()=>{}} w={words(language)} language={language} loading={false} errors={[]} projectNames={new Map([[JSON.stringify(["machine","project"]),"Synthetic Project"],[JSON.stringify(["machine","other"]),"Other Project"]])}/><main className="pa-main"/></div>);
`,resolveDir:root,loader:"tsx"},bundle:true,write:false,outfile:"groups.js",format:"esm",target:"es2022",platform:"browser",jsx:"automatic",define:{"process.env.NODE_ENV":'"development"'}})
const files=new Map(bundle.outputFiles.map(f=>["/"+path.basename(f.path),f.contents])),traffic=[]
const server=createServer(async(req,res)=>{
  try{
    const url=new URL(req.url,"http://fixture")
    if(url.pathname.startsWith("/proxy/")||url.pathname.startsWith("/relay/")){
      const chunks=[];for await(const chunk of req)chunks.push(chunk);const raw=Buffer.concat(chunks).toString(),relay=url.pathname.startsWith("/relay/")
      let answer,record={method:req.method,path:url.pathname,relay,body:raw?JSON.parse(raw):null};traffic.push(record)
      if(relay){const word=url.pathname.slice(7),fields=JSON.parse(raw||"{}"),envelope={type:word,session:"__clawdline_machine__",request:req.headers["idempotency-key"]??"read-"+crypto.randomUUID(),...fields};record.word=word
        answer=await fetch(cloud,{method:"POST",body:JSON.stringify(envelope)});const payload=await answer.json();record.status=answer.status;res.writeHead(answer.status,{"Content-Type":"application/json"});res.end(JSON.stringify(payload.body??{error:payload.error??{code:"pa_outcome_unknown"}}));return
      }
      const mode=url.pathname.split("/")[2],pathname=url.pathname.replace("/proxy/"+mode,"")+url.search;record.path=pathname
      answer=await fetch(upstream+pathname,{method:req.method,headers:{Authorization:"Bearer "+(mode==="read"?"reader":"person"),...(raw?{"Content-Type":"application/json","Idempotency-Key":req.headers["idempotency-key"]??""}:{})},...(raw?{body:raw}:{})})
      record.status=answer.status;res.writeHead(answer.status,{"Content-Type":"application/json"});res.end(Buffer.from(await answer.arrayBuffer()));return
    }
    if(url.pathname.startsWith("/catalog/")){res.setHeader("Content-Type","application/json");res.end(await readFile(path.join(root,"public/catalogs",path.basename(url.pathname))));return}
    if(files.has(url.pathname)){res.setHeader("Content-Type",url.pathname.endsWith(".css")?"text/css":"text/javascript");res.end(files.get(url.pathname));return}
    res.setHeader("Content-Type","text/html");res.end('<!doctype html><html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><style>html,body,#app{height:100%;margin:0}</style><link rel="stylesheet" href="/groups.css"><div id="app"></div><script type="module" src="/groups.js"></script></html>')
  }catch(error){res.writeHead(500);res.end(JSON.stringify({error:String(error)}))}
})
await new Promise(resolve=>server.listen(0,"127.0.0.1",resolve));const origin=`http://127.0.0.1:${server.address().port}`
if(process.env.BOTS_PREVIEW==="1"){
  const stop=path.join(artifacts,"group-ui-preview-stop")
  await rm(stop,{force:true});await writeFile(path.join(artifacts,"group-ui-preview.json"),JSON.stringify({origin,scope:"task-only scripted HTTP/SQLite fixture"}))
  await new Promise(resolve=>{const timer=setInterval(async()=>{try{await access(stop);clearInterval(timer);resolve()}catch{}},250)})
  server.closeAllConnections();await new Promise(resolve=>server.close(resolve));process.exit(0)
}
const profile=await mkdtemp(path.join(work,"groups-browser-")),executable=await testBrowser()
const chrome=spawn(executable,["--headless=new","--remote-debugging-port=0","--user-data-dir="+profile,"--no-first-run","--no-default-browser-check","about:blank"],{stdio:["ignore","ignore","pipe"]})
let ws
const results=[]
try{
  const endpoint=await new Promise((resolve,reject)=>{let output="";const timer=setTimeout(()=>reject(Error("Chrome startup: "+output)),20000);chrome.on("error",reject);chrome.on("exit",(code,signal)=>{clearTimeout(timer);reject(Error("Chrome exited: "+code+" "+signal+" "+output))});chrome.stderr.on("data",chunk=>{output+=chunk;const match=/DevTools listening on (ws:\/\/\S+)/.exec(output);if(match){clearTimeout(timer);resolve(match[1])}})})
  ws=new WebSocket(endpoint);await new Promise((resolve,reject)=>{ws.addEventListener("open",resolve);ws.addEventListener("error",reject)})
  let serial=0;const pending=new Map()
  ws.addEventListener("message",event=>{const message=JSON.parse(String(event.data)),job=pending.get(message.id);if(!job)return;pending.delete(message.id);message.error?job.reject(Error(message.error.message)):job.resolve(message.result)})
  const send=(method,params={},sessionId)=>new Promise((resolve,reject)=>{const id=++serial,timer=setTimeout(()=>reject(Error(method+" timed out")),15000);pending.set(id,{resolve:v=>{clearTimeout(timer);resolve(v)},reject:e=>{clearTimeout(timer);reject(e)}});ws.send(JSON.stringify({id,method,params,sessionId}))})
  const open=async(mode,lang,width,height)=>{const {targetId}=await send("Target.createTarget",{url:"about:blank"});const {sessionId}=await send("Target.attachToTarget",{targetId,flatten:true});await send("Page.enable",{},sessionId);await send("Runtime.enable",{},sessionId);await send("Emulation.setDeviceMetricsOverride",{width,height,deviceScaleFactor:1,mobile:width<600},sessionId);await send("Page.navigate",{url:origin+"/?mode="+mode+"&lang="+lang},sessionId);return{targetId,sessionId}}
  const evaluate=async(sessionId,expression)=>{const reply=await send("Runtime.evaluate",{expression,awaitPromise:true,returnByValue:true},sessionId);if(reply.exceptionDetails)throw Error(reply.exceptionDetails.text+": "+reply.exceptionDetails.exception?.description);return reply.result.value}
  const until=async(sessionId,expression)=>{const deadline=Date.now()+15000;while(Date.now()<deadline){if(await evaluate(sessionId,expression))return;await new Promise(r=>setTimeout(r,50))}throw Error("UI wait: "+expression+"\n"+await evaluate(sessionId,"document.body.innerText"))}
  const click=async(sessionId,text)=>{assert.ok(await evaluate(sessionId,`(()=>{const b=[...document.querySelectorAll('button')].find(b=>b.textContent.trim()===${JSON.stringify(text)}&&!b.disabled);if(!b)return false;b.click();return true})()`),"click "+text)}
  const fill=async(sessionId,selector,value)=>evaluate(sessionId,`(()=>{const e=document.querySelector(${JSON.stringify(selector)});if(!e)throw Error('Missing input');Object.getOwnPropertyDescriptor(e.tagName==='TEXTAREA'?HTMLTextAreaElement.prototype:HTMLInputElement.prototype,'value').set.call(e,${JSON.stringify(value)});e.dispatchEvent(new Event('input',{bubbles:true}));})()`)
  for(const [mode,lang,width,height]of [["local","en",1280,800],["local","zh-Hant",390,844],["cloud","en",1280,800],["cloud","zh-Hant",390,844]]){
    const {targetId,sessionId}=await open(mode,lang,width,height),zh=lang==="zh-Hant",name=`Review ${mode} ${lang}`
    const copy=zh?{create:"新增群組",save:"儲存",edit:"編輯群組",send:"傳送",stop:"停止群組",remove:"刪除群組",confirm:"確認刪除群組"}:{create:"New group",save:"Save",edit:"Edit group",send:"Send",stop:"Stop group",remove:"Delete group",confirm:"Confirm group deletion"}
    await until(sessionId,`[...document.querySelectorAll('button')].some(b=>b.textContent.trim()===${JSON.stringify(copy.create)})`);await click(sessionId,copy.create)
    await until(sessionId,"!!document.querySelector('[role=dialog] input:not([type=checkbox])')");await fill(sessionId,"[role=dialog] input:not([type=checkbox])",name)
    assert.equal(await evaluate(sessionId,"[...document.querySelectorAll('[role=dialog] input[type=checkbox]')].some(e=>e.parentElement.textContent.includes('Stranger'))"),false,"another Project was offered as a room member")
    await evaluate(sessionId,"for(const e of document.querySelectorAll('[role=dialog] input[type=checkbox]'))if(['Alice','Bob'].includes(e.parentElement.textContent.trim()))e.click()")
    await click(sessionId,copy.save);await until(sessionId,`!!document.querySelector('.pa-chat-header h2')&&document.querySelector('.pa-chat-header h2').textContent===${JSON.stringify(name)}`)
    await fill(sessionId,"[role=dialog] textarea","Synthetic UI room message");await click(sessionId,copy.send);await until(sessionId,"document.querySelectorAll('[data-room-seq]').length===3")
    const authors=await evaluate(sessionId,"[...document.querySelectorAll('[data-room-seq] strong')].map(e=>e.textContent)");assert.ok(authors.includes("Alice")&&authors.includes("Bob"),authors)
    assert.equal(await evaluate(sessionId,"document.documentElement.scrollWidth>innerWidth"),false,"room widened the page")
    const shot=await send("Page.captureScreenshot",{format:"png"},sessionId);await writeFile(path.join(artifacts,`group-${mode}-${lang}.png`),Buffer.from(shot.data,"base64"))
    await click(sessionId,copy.edit);await until(sessionId,"!!document.querySelector('[role=dialog] [role=dialog] input:not([type=checkbox])')");await fill(sessionId,"[role=dialog] [role=dialog] input:not([type=checkbox])",name+" changed")
    await evaluate(sessionId,"[...document.querySelectorAll('[role=dialog] [role=dialog] input[type=checkbox]')].find(e=>e.parentElement.textContent.trim()==='Bob').click()")
    await click(sessionId,copy.save);await until(sessionId,`document.querySelector('.pa-chat-header h2')?.textContent===${JSON.stringify(name+" changed")}`)
    await click(sessionId,copy.stop);await until(sessionId,`[...document.querySelectorAll('button')].some(b=>b.textContent.trim()===${JSON.stringify(copy.remove)}&&!b.disabled)`)
    await click(sessionId,copy.remove);await click(sessionId,copy.confirm);await until(sessionId,"!document.querySelector('[role=dialog]')")
    results.push({mode,lang,width,create:true,send:true,authors,edit:true,stop:true,delete:true});await send("Target.closeTarget",{targetId})
  }
  // A real saved room can be read from a read-only source; no action leaves it.
  const request={command:{key:{machine_id:"machine",bot_id:"read-room"},operation:"create",idempotency_key:"seed-reader-room",content_hash:""},settings:{target:{machine_id:"machine",project_id:"project"},name:"Saved reader room",members:[{machine_id:"machine",bot_id:"alice"}]}}
  const seeded=await fetch(upstream+"/v1/project-assistants/rooms/groups",{method:"POST",headers:{Authorization:"Bearer person","Content-Type":"application/json","Idempotency-Key":"seed-reader-room"},body:JSON.stringify(request)});assert.equal(seeded.status,200,await seeded.text())
  for(const mode of ["read","old"]){const start=traffic.length,{targetId,sessionId}=await open(mode,"zh-Hant",390,844);await until(sessionId,"document.body.innerText.includes('Alice')")
    if(mode==="read"){await until(sessionId,"[...document.querySelectorAll('button')].some(b=>b.textContent.includes('Saved reader room'))");await evaluate(sessionId,"[...document.querySelectorAll('button')].find(b=>b.textContent.includes('Saved reader room')).click()");await until(sessionId,"!!document.querySelector('[role=dialog] textarea')");assert.equal(await evaluate(sessionId,"document.querySelector('[role=dialog] textarea').disabled"),true);assert.equal(await evaluate(sessionId,"[...document.querySelectorAll('[role=dialog] button')].filter(b=>['傳送','編輯群組','停止群組','刪除群組'].includes(b.textContent.trim())).some(b=>!b.disabled)"),false)}else{assert.equal(await evaluate(sessionId,"document.body.innerText.includes('新增群組')"),false)}
    assert.equal(traffic.slice(start).filter(r=>r.method!=="GET").length,0);results.push({mode,lang:"zh-Hant",readOnly:mode==="read",actions:0});await send("Target.closeTarget",{targetId})
  }
  console.log("GROUP_UI_RECEIPT "+JSON.stringify({browser:executable,results,providerRequests:0,cloudBoundary:"decrypted vocabulary -> actual HTTP -> SQLite"}))
}finally{
  await writeFile(path.join(artifacts,"group-ui-traffic.json"),JSON.stringify(traffic,null,2));await writeFile(path.join(artifacts,"group-ui-results.json"),JSON.stringify(results,null,2))
  ws?.close();chrome.kill("SIGTERM");await new Promise(resolve=>{if(chrome.exitCode!==null)return resolve();const timer=setTimeout(()=>{chrome.kill("SIGKILL");resolve()},3000);chrome.once("exit",()=>{clearTimeout(timer);resolve()})});server.closeAllConnections();await new Promise(resolve=>server.close(resolve));await rm(profile,{recursive:true,force:true})
}
