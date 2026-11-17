/** Real Bot chat and source clients, a task-owned Go daemon and scripted native host. */
import assert from "node:assert/strict"
import {createServer} from "node:http"
import {spawn} from "node:child_process"
import {mkdir,mkdtemp,readFile,writeFile,rm,readdir,access} from "node:fs/promises"
import {constants} from "node:fs"
import {homedir} from "node:os"
import path from "node:path"
import {fileURLToPath} from "node:url"
import {build} from "esbuild"

const {BOTS_ORIGIN:upstream,BOTS_TOKEN:token,BOTS_READ_TOKEN:readToken,BOTS_MACHINE:machine,BOTS_BOT:bot,BOTS_CONVERSATION:conversation,BOTS_EMPTY_BOT:emptyBot,BOTS_EMPTY_CONVERSATION:emptyConversation,BOTS_ARTIFACTS:artifacts}=process.env
assert.ok(upstream&&token&&readToken&&machine&&bot&&conversation&&artifacts,"requires the task-owned Go fixture and artifact directory")
await mkdir(artifacts,{recursive:true})
const root=fileURLToPath(new URL("..",import.meta.url))
const artifactParts=artifacts.split(path.sep),artifactIndex=artifactParts.lastIndexOf("artifacts")
const work=process.env.BOTS_WORK??path.join(artifactIndex<0?path.dirname(artifacts):artifactParts.slice(0,artifactIndex).join(path.sep),"work")
await mkdir(work,{recursive:true})
async function testBrowser() {
  if(process.env.CHROME){await access(process.env.CHROME,constants.X_OK);return process.env.CHROME}
  // Discover only the existing Playwright browser cache; never install a browser.
  const cache=process.env.PLAYWRIGHT_BROWSERS_PATH??(process.platform==="darwin"?path.join(homedir(),"Library/Caches/ms-playwright"):process.platform==="win32"?path.join(process.env.LOCALAPPDATA??path.join(homedir(),"AppData/Local"),"ms-playwright"):path.join(process.env.XDG_CACHE_HOME??path.join(homedir(),".cache"),"ms-playwright"))
  const entries=await readdir(cache,{withFileTypes:true})
  const shells=entries.filter(entry=>entry.isDirectory()&&/^chromium_headless_shell-\d+$/.test(entry.name)).sort((a,b)=>Number(b.name.split("-").at(-1))-Number(a.name.split("-").at(-1)))
  const suffix=process.platform==="darwin"?"chrome-mac/headless_shell":process.platform==="win32"?"chrome-win/headless_shell.exe":"chrome-linux/headless_shell"
  for(const shell of shells){const executable=path.join(cache,shell.name,suffix);try{await access(executable,constants.X_OK);return executable}catch{}}
  throw Error("No installed Playwright Chromium headless shell. Set PLAYWRIGHT_BROWSERS_PATH or CHROME to the existing test browser; this harness installs nothing.")
}
const bundle=await build({stdin:{contents:`
import React from "react";import {createRoot} from "react-dom/client";
import {Bots} from "./src/project-assistants/Bots";
import {ProjectAssistantSourceClient,capturedAssistantRelayFetch} from "./src/cloud/project-assistants";
import {bootCatalog} from "./src/catalog";
import "./src/legacy/tokens.css";import "./src/legacy/base.css";import "./src/legacy/detail.css";
const q=new URL(location.href).searchParams,mode=q.get("mode")??"local",language=q.get("lang")??"en";
localStorage.setItem("ui_language",language);await bootCatalog(tag=>"/catalog/"+tag+".json");
const grant={accountKey:mode==="cloud"?"synthetic-account":"local",readerID:"synthetic-reader",machineID:${JSON.stringify(machine)},authorizationRevision:"synthetic-grant",apiLevel:mode==="old"?73:74,paired:mode!=="unpaired",online:true,writable:mode!=="read"};
const transport=(input,init)=>fetch("/proxy/"+mode+input,init);
const words=["chrome-tab-status","chrome-tab-activity","chrome-audit","chrome-tab-screenshot","chrome-tab-control","chrome-tab-input","chrome-tab-open","chrome-tab-mute","project-assistants.status","project-assistants.list","project-assistants.get","project-assistants.view","project-assistants.projects","project-assistants.read","project-assistants.observed"];
const relay={machineDescriptor:()=>({machine:{commands:words}}),_machineRequest:async(_,word,fields)=>await(await fetch("/relay/"+word,{method:"POST",body:JSON.stringify(fields),headers:{"Content-Type":"application/json"}})).json(),_machineRequestAs:async(id,_,word,fields)=>await(await fetch("/relay/"+word,{method:"POST",body:JSON.stringify(fields),headers:{"Content-Type":"application/json","Idempotency-Key":id}})).json()};
const source=new ProjectAssistantSourceClient(grant,mode==="cloud"?capturedAssistantRelayFetch(grant.machineID,()=>relay,words):transport,()=>grant);
source.canRoster=()=>false;
createRoot(document.getElementById("app")).render(<Bots sources={[source]} language={language} accountKey={grant.accountKey} readerID={grant.readerID} onClose={()=>{}}/>);
`,resolveDir:root,loader:"tsx"},bundle:true,write:false,outfile:"browser.js",format:"esm",target:"es2022",platform:"browser",jsx:"automatic",define:{"process.env.NODE_ENV":'"development"'}})
const files=new Map(bundle.outputFiles.map(f=>["/"+path.basename(f.path),f.contents]))
const requests=[],traffic=[],protocolEvents=[],browserWords={"chrome-tab-status":"status","chrome-tab-activity":"activity","chrome-audit":"audit","chrome-tab-screenshot":"screenshot","chrome-tab-control":"control","chrome-tab-input":"person","chrome-tab-open":"open","chrome-tab-mute":"mute"}
let injectStale=false,injectLost=false
function relayRoute(word,fields) {
  if(browserWords[word]) {
    const method=["status","activity","audit","screenshot"].includes(browserWords[word])?"GET":"POST"
    return {method,path:"/v1/browser/"+browserWords[word],fields}
  }
  const key=fields.key??fields.command?.key??fields
  const base="/v1/project-assistants"
  if(word==="project-assistants.status")return {method:"GET",path:base+"/status",fields:{}}
  if(word==="project-assistants.list")return {method:"GET",path:base,fields}
  if(word==="project-assistants.projects")return {method:"GET",path:"/v1/project-assistant-targets",fields}
  if(word==="project-assistants.get")return {method:"GET",path:base+"/"+encodeURIComponent(key.bot_id),fields:{machine_id:key.machine_id}}
  if(word==="project-assistants.view"){const {key,...rest}=fields;return {method:"GET",path:base+"/"+encodeURIComponent(key.bot_id)+"/view",fields:{machine_id:key.machine_id,...rest}}}
  if(word==="project-assistants.read"||word==="project-assistants.observed")return {method:"POST",path:base+"/"+encodeURIComponent(key.bot_id)+(word.endsWith(".read")?"/read":"/observed"),fields}
  throw Error("Unexpected relay word: "+word)
}
const server=createServer(async(req,res)=>{
  const request={method:req.method,path:req.url,at:Date.now()};traffic.push(request)
  res.once("finish",()=>{request.status=res.statusCode;request.finished=Date.now()})
  try {
    const url=new URL(req.url,"http://fixture")
    if(url.pathname.startsWith("/proxy/")||url.pathname.startsWith("/relay/")) {
      const chunks=[];for await(const chunk of req)chunks.push(chunk)
      const raw=Buffer.concat(chunks).toString(),relay=url.pathname.startsWith("/relay/")
      const mode=relay?"cloud":url.pathname.split("/")[2]
      const route=relay?relayRoute(url.pathname.slice(7),JSON.parse(raw||"{}")):{method:req.method,path:url.pathname.replace("/proxy/"+mode,"")+url.search,fields:raw?JSON.parse(raw):null}
      if(relay&&route.method==="GET")route.path+=(Object.keys(route.fields).length?"?"+new URLSearchParams(route.fields):"")
      const headers={Authorization:"Bearer "+(mode==="read"?readToken:token)}
      if(route.method!=="GET"){headers["Content-Type"]="application/json";if(req.headers["idempotency-key"])headers["Idempotency-Key"]=req.headers["idempotency-key"]}
      const record={mode,method:route.method,path:route.path,body:route.method!=="GET"?route.fields:undefined,word:relay?url.pathname.slice(7):undefined};requests.push(record)
      if(route.path==="/v1/browser/control"&&injectStale){injectStale=false;record.status=409;res.writeHead(409,{"Content-Type":"application/json"});res.end(JSON.stringify({contract_version:"project-assistants.v1",error:{code:"pa_stale_version",field:"conversation"}}));return}
      const answer=await fetch(upstream+route.path,{method:route.method,headers,...(route.method!=="GET"?{body:JSON.stringify(route.fields)}:{})})
      const body=Buffer.from(await answer.arrayBuffer());record.status=answer.status
      if(route.path==="/v1/browser/control"&&injectLost){injectLost=false;record.committed_status=answer.status;record.status=503;res.writeHead(503,{"Content-Type":"application/json"});res.end('{"error":"browser_timeout","detail":"Synthetic result lost after execution"}');return}
      res.writeHead(answer.status,{"Content-Type":answer.headers.get("Content-Type")??"application/json"});res.end(body);return
    }
    if(url.pathname.startsWith("/catalog/")){res.setHeader("Content-Type","application/json");res.end(await readFile(path.join(root,"public/catalogs",path.basename(url.pathname))));return}
    if(files.has(url.pathname)){res.setHeader("Content-Type",url.pathname.endsWith(".css")?"text/css":"text/javascript");res.end(files.get(url.pathname));return}
    res.setHeader("Content-Type","text/html");res.end('<!doctype html><html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><style>html,body,#app{height:100%;margin:0}body{background:#080808}</style><link rel="stylesheet" href="/browser.css"><div id="app"></div><script type="module" src="/browser.js"></script></html>')
  }catch(error){res.writeHead(500);res.end(JSON.stringify({error:String(error)}))}
})
await new Promise(resolve=>server.listen(0,"127.0.0.1",resolve))
const origin=`http://127.0.0.1:${server.address().port}`
const profile=await mkdtemp(path.join(work,"browser-profile-"))
const executable=await testBrowser()
console.log("BROWSER_UI_BROWSER "+JSON.stringify({executable,profile}))
const chrome=spawn(executable,["--headless=new","--remote-debugging-port=0","--user-data-dir="+profile,"--no-first-run","--no-default-browser-check","--disable-features=OmniboxEverywhere","about:blank"],{stdio:["ignore","ignore","pipe"]})
let ws
try {
  const endpoint=await new Promise((resolve,reject)=>{let output="";const timer=setTimeout(()=>reject(Error("Chrome startup timeout: "+output)),20000);chrome.on("error",reject);chrome.on("exit",(code,signal)=>{clearTimeout(timer);reject(Error("Chrome exited: "+code+" "+signal+" "+output))});chrome.stderr.on("data",chunk=>{output+=chunk;const match=/DevTools listening on (ws:\/\/\S+)/.exec(output);if(match){clearTimeout(timer);resolve(match[1])}})})
  ws=new WebSocket(endpoint);await new Promise((resolve,reject)=>{ws.addEventListener("open",resolve);ws.addEventListener("error",reject)})
  let serial=0;const pending=new Map()
  ws.addEventListener("message",event=>{const message=JSON.parse(String(event.data));if(!message.id){if(message.method?.startsWith("Page.")||message.method?.includes("crashed")||message.method==="Runtime.exceptionThrown")protocolEvents.push(message);return};const job=pending.get(message.id);pending.delete(message.id);if(message.error)job?.reject(Error(message.error.message));else job?.resolve(message.result)})
  const send=(method,params={},sessionId)=>new Promise((resolve,reject)=>{const id=++serial,timer=setTimeout(()=>reject(Error(method+" timed out")),15000);pending.set(id,{resolve:v=>{clearTimeout(timer);resolve(v)},reject:e=>{clearTimeout(timer);reject(e)}});ws.send(JSON.stringify({id,method,params,sessionId}))})
  const attach=async targetId=>{
    const {sessionId}=await send("Target.attachToTarget",{targetId,flatten:true})
    await send("Page.enable",{},sessionId)
    await send("Runtime.enable",{},sessionId)
    await send("Page.setLifecycleEventsEnabled",{enabled:true},sessionId)
    return sessionId
  }
  const results=[]
  for(const [mode,lang,width,height] of [["local","en",1280,800],["local","zh-Hant",390,844],["cloud","en",1280,800],["cloud","zh-Hant",390,844],["read","en",390,844],["old","zh-Hant",390,844]]) {
    const {targetId}=await send("Target.createTarget",{url:"about:blank"})
    const sessionId=await attach(targetId)
    const run=async(expression)=>{const r=await send("Runtime.evaluate",{expression,awaitPromise:true,returnByValue:true},sessionId);if(r.exceptionDetails)throw Error(r.exceptionDetails.exception?.description??r.exceptionDetails.text);return r.result.value}
    const until=async(expression)=>{const deadline=Date.now()+15000;while(!await run(expression)){if(Date.now()>deadline)throw Error("UI did not reach "+expression+"\n"+await run("document.body.innerText"));await new Promise(r=>setTimeout(r,50))}}
    const start=requests.length
    console.log("BROWSER_UI_STAGE "+JSON.stringify({stage:"navigate",mode,lang,width}))
    await send("Emulation.setDeviceMetricsOverride",{width,height,deviceScaleFactor:1,mobile:width<700},sessionId)
    await send("Page.navigate",{url:origin+"/?mode="+mode+"&lang="+lang+"#bot="+encodeURIComponent(bot)+"&conversation="+encodeURIComponent(conversation)+"&machine="+encodeURIComponent(machine)},sessionId)
    await until('document.querySelector(".pa-chat-header h2")?.textContent==="Synthetic Bot"')
    if(mode==="old") {
      assert.equal(await run('!!document.querySelector(".pa-chat-header .browser-chip")'),false)
      assert.equal(await run('!!document.querySelector(".pa-reply-browser")'),false)
      await run('document.querySelector(".pa-chat-header button[aria-haspopup=menu]").click()')
      await until('!!document.querySelector("[role=menuitem]")')
      await run('document.querySelector("[role=menuitem]").click()')
    } else {
      await until('!!document.querySelector(".pa-chat-header .browser-chip")')
      assert.equal(await run('document.querySelector(".pa-chat-header").scrollWidth>document.querySelector(".pa-chat-header").clientWidth'),false,"chip fits the phone header")
      await run('document.querySelector(".pa-chat-header .browser-chip").click()')
    }
    await until('!!document.querySelector("#browser-panel")')
    if(mode==="old") {
      await until('document.querySelector("#browser-panel").innerText.includes("API 74")')
      assert.equal(requests.slice(start).filter(r=>r.path.startsWith("/v1/browser/")).length,0)
    } else {
      await until('!!document.querySelector("#browser-panel img")?.naturalWidth')
      const frameDeadline=Date.now()+5000
      while(requests.slice(start).filter(r=>r.path.startsWith("/v1/browser/screenshot")).length<2){assert.ok(Date.now()<frameDeadline,"the open panel samples another screenshot");await new Promise(r=>setTimeout(r,50))}
      if(mode==="read") {
        assert.equal(await run('document.querySelector(".browser-take").disabled'),true)
        await until('document.querySelector("#browser-panel").innerText.includes("write access")')
        assert.equal(requests.slice(start).filter(r=>r.method==="POST"&&r.path.startsWith("/v1/browser/")).length,0)
      } else {
        await run('document.querySelector(".browser-take").click()')
        await until('document.querySelector(".browser-who").dataset.person==="true"')
        await run('document.querySelector(".browser-take").click()')
        await until('!document.querySelector(".browser-who").dataset.person')
        await run('document.querySelector(".browser-tools-toggle").click()')
        await run('document.querySelector(".browser-notify").click()')
        await until('document.querySelector(".browser-notify").getAttribute("aria-pressed")==="false"')
        await run('document.querySelector(".browser-notify").click()')
        await until('document.querySelector(".browser-notify").getAttribute("aria-pressed")==="true"')
        await run('document.querySelectorAll(".browser-activity").forEach(el=>el.open=true)')
        await until('document.querySelectorAll(".browser-activity li").length>0')
      }
    }
    if(mode!=="old") {
      // The reply entry uses durable tool details, without opening the trace first.
      await run('document.querySelector("#browser-panel .browser-close").click()')
      await until('!document.querySelector("#browser-panel")')
      await until('!!document.querySelector("[data-message-id=browser-ui-reply] .pa-reply-browser")')
      assert.equal(await run('document.querySelectorAll(".pa-reply-browser").length'),1,"the user message mentioning a tool has no browser action")
      assert.equal(await run('document.querySelector(".pa-reply-browser").textContent'),lang==="en"?"Open browser":"打開瀏覽器")
      await run('document.querySelector(".pa-reply-browser").click()')
      await until('!!document.querySelector("#browser-panel img")?.naturalWidth')
      assert.equal(await run('document.querySelector(".browser-take").disabled'),mode==="read")
    }
    const {data}=await send("Page.captureScreenshot",{format:"png"},sessionId)
    await writeFile(path.join(artifacts,`bot-browser-${mode}-${lang}-${width}.png`),Buffer.from(data,"base64"))
    for(const request of requests.slice(start).filter(r=>r.path.startsWith("/v1/browser/"))) {
      const fields=request.body??Object.fromEntries(new URL(request.path,origin).searchParams)
      assert.equal(fields.bot_id,bot);assert.equal(fields.conversation,conversation);assert.equal("owner" in fields,false);assert.equal("machine_id" in fields,false)
      assert.equal(request.status,200,JSON.stringify(request))
    }
    results.push({mode,lang,width,chip:mode!=="old",replyEntry:mode!=="old",requests:requests.length-start})
    await send("Target.closeTarget",{targetId})
  }
  // A Bot with no tab can be given one, already taken over, through the same UI.
  {
    assert.ok(emptyBot&&emptyConversation)
    const {targetId}=await send("Target.createTarget",{url:"about:blank"}),sessionId=await attach(targetId)
    const run=async(expression)=>{const r=await send("Runtime.evaluate",{expression,awaitPromise:true,returnByValue:true},sessionId);if(r.exceptionDetails)throw Error(r.exceptionDetails.exception?.description??r.exceptionDetails.text);return r.result.value}
    const until=async(expression)=>{const deadline=Date.now()+10000;while(!await run(expression)){if(Date.now()>deadline)throw Error("open fixture did not reach "+expression+" "+await run("document.body.innerText"));await new Promise(r=>setTimeout(r,50))}}
    await send("Page.navigate",{url:origin+"/?mode=local&lang=en#bot="+emptyBot+"&conversation="+emptyConversation},sessionId)
    await until('document.querySelector(".pa-chat-header h2")?.textContent==="Empty Bot"')
    await until('!!document.querySelector(".pa-reply-browser")')
    assert.equal(await run('!!document.querySelector(".pa-chat-header .browser-chip")'),false,"no chip without an owned tab")
    await run('document.querySelector(".pa-reply-browser").click()')
    await until('!!document.querySelector("#browser-open-url")&&!document.querySelector("#browser-open-url").disabled')
    if(await run('document.querySelector(".browser-card h3")?.textContent')!=="This Bot has no tab yet")throw Error("Bot empty state names a Session: "+await run('document.querySelector(".browser-card h3")?.textContent'))
    await run('const input=document.querySelector("#browser-open-url");Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,"value").set.call(input,"https://synthetic.example/new");input.dispatchEvent(new Event("input",{bubbles:true}))')
    await run('document.querySelector("#browser-open-url").form.requestSubmit()')
    await until('document.querySelector(".browser-who")?.dataset.person==="true"')
    const opened=requests.filter(r=>r.path==="/v1/browser/open").at(-1)
    assert.equal(opened.status,200);assert.equal(opened.body.bot_id,emptyBot);assert.equal(opened.body.conversation,emptyConversation)
    await run('document.querySelector(".browser-take").click()')
    await until('!document.querySelector(".browser-who").dataset.person')
    await send("Target.closeTarget",{targetId});results.push({openedBot:emptyBot,personInitially:true,handedBack:true})
  }
  // A stale write freezes the old panel; reloading identity cannot replay it.
  for(const fault of ["stale","lost"]) {
    const {targetId}=await send("Target.createTarget",{url:"about:blank"}),sessionId=await attach(targetId)
    const run=async(expression)=>{const r=await send("Runtime.evaluate",{expression,awaitPromise:true,returnByValue:true},sessionId);if(r.exceptionDetails)throw Error(r.exceptionDetails.exception?.description??r.exceptionDetails.text);return r.result.value}
    const until=async(expression)=>{const deadline=Date.now()+10000;while(!await run(expression)){if(Date.now()>deadline)throw Error("fault fixture did not reach "+expression+" "+await run("document.body.innerText"));await new Promise(r=>setTimeout(r,50))}}
    await send("Page.navigate",{url:origin+"/?mode=local&lang=en#bot="+bot+"&conversation="+conversation},sessionId)
    await until('!!document.querySelector(".pa-chat-header h2")')
    await run('document.querySelector(".pa-chat-header button[aria-haspopup=menu]").click()')
    await until('!!document.querySelector(".pa-chat-header [role=menuitem]")')
    await run('[...document.querySelectorAll(".pa-chat-header [role=menuitem]")].find(button=>button.textContent==="Browser").click()')
    await until('!!document.querySelector(".browser-take")')
    const before=requests.filter(r=>r.path==="/v1/browser/control").length,beforeReads=requests.filter(r=>r.path.startsWith("/v1/project-assistants/"+bot+"?")).length
    assert.equal(await run('!!document.querySelector("#browser-panel [role=alert]")'),false)
    if(fault==="stale")injectStale=true;else injectLost=true
    await run('document.querySelector(".browser-take").click()')
    await until(fault==="stale"?'document.querySelector("#browser-panel").innerText.includes("conversation changed")':'!!document.querySelector("#browser-panel [role=alert]")?.innerText.trim()')
    await new Promise(r=>setTimeout(r,3300))
    assert.equal(requests.filter(r=>r.path==="/v1/browser/control").length,before+1)
    if(fault==="stale"){
      assert.equal(await run('document.querySelector(".browser-take").disabled'),true)
      assert.ok(requests.filter(r=>r.path.startsWith("/v1/project-assistants/"+bot+"?")).length>beforeReads,"stale identity is reloaded")
    }
    await send("Target.closeTarget",{targetId});results.push({fault,actions:1,replayed:false})
  }
  await writeFile(path.join(artifacts,"browser-ui-requests.json"),JSON.stringify(requests,null,2))
  await writeFile(path.join(artifacts,"browser-ui-results.json"),JSON.stringify(results,null,2))
  console.log(JSON.stringify({results,providerRequests:0,realChromeAutomation:0,headlessUI:true}))
} finally {
  await writeFile(path.join(artifacts,"browser-ui-requests.json"),JSON.stringify(requests,null,2))
  await writeFile(path.join(artifacts,"browser-ui-traffic.json"),JSON.stringify(traffic,null,2))
  await writeFile(path.join(artifacts,"browser-ui-protocol-events.json"),JSON.stringify(protocolEvents,null,2))
  ws?.close();chrome.kill()
  await new Promise(resolve=>chrome.exitCode!==null||chrome.signalCode!==null?resolve():chrome.once("exit",resolve))
  server.closeAllConnections();await new Promise(resolve=>server.close(resolve))
  await rm(profile,{recursive:true,force:true,maxRetries:5})
}
