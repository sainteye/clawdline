/** Production Chat + manager + SourceClient against the owned Go TCP fixture. */
import assert from "node:assert/strict"
import {createServer} from "node:http"
import {access,mkdir,mkdtemp,readFile,rm,writeFile} from "node:fs/promises"
import {spawn} from "node:child_process"
import {constants} from "node:fs"
import os from "node:os"
import {fileURLToPath} from "node:url"
import path from "node:path"
import {build} from "esbuild"

const {BOTS_ORIGIN:upstream,BOTS_TOKEN:token,BOTS_MACHINE:machine,BOTS_BOT:bot,BOTS_ARTIFACTS:artifacts}=process.env
if(!upstream||!token||!machine||!bot||!artifacts)throw Error("Requires the owned Go HTTP fixture")
await mkdir(artifacts,{recursive:true})
const consoleRoot=fileURLToPath(new URL("..",import.meta.url))
const bundle=await build({stdin:{contents:`
import React from "react";import {createRoot} from "react-dom/client";
import {Chat} from "./src/project-assistants/Chat";
import {ProjectAssistantSourceClient} from "./src/cloud/project-assistants";
import {createViewState} from "./src/project-assistants/view-state";
import {en,zhHant} from "./src/project-assistants/words";
import {activateCatalog} from "./src/catalog";
import taiwan from "./public/catalogs/zh-Hant.json";
import "./src/project-assistants/styles.css";
const language=new URL(location.href).searchParams.get("lang")??"en";
if(language==="zh-Hant")activateCatalog(taiwan,"zh-Hant");
const grant={accountKey:"synthetic-account",readerID:"synthetic-reader",machineID:${JSON.stringify(machine)},authorizationRevision:"synthetic-grant",apiLevel:95,paired:true,online:true,writable:true};
const source=new ProjectAssistantSourceClient(grant,(input,init)=>fetch(input,init),()=>grant);
await source.status(new AbortController().signal);
const list=await source.list({machine_id:grant.machineID,query:"",cursor:"",page_size:100},new AbortController().signal);
const summary=list.items.find(x=>x.bot.key.bot_id===${JSON.stringify(bot)});
const view=createViewState(sessionStorage);
if(new URL(location.href).searchParams.get("journal")==="broken") {
  const key="clawdline:routine-command:"+JSON.stringify([grant.accountKey,grant.readerID,summary.bot.key.machine_id,summary.bot.key.bot_id]);
  sessionStorage.setItem(key,'{"action":"test","request":');
}
createRoot(document.getElementById("app")).render(<main className="project-assistants" style={{height:"100vh"}}><Chat summary={summary} source={source} current={()=>[source]} accountKey={grant.accountKey} readerID={grant.readerID} view={view} w={language==="en"?en:zhHant} language={language} onBack={()=>{}} onBotChanged={()=>{}}/></main>);
`,resolveDir:consoleRoot,loader:"tsx"},bundle:true,write:false,outfile:"routine.js",format:"esm",target:"es2022",platform:"browser",jsx:"automatic",define:{"process.env.NODE_ENV":'"development"'}})
const files=new Map(bundle.outputFiles.map(f=>["/"+path.basename(f.path),f.contents])),requests=[]
let lostSave=false,pageLoads=0,holdEnable=0,enableArrived=0
const server=createServer(async(req,res)=>{
  try {
    if(req.url.startsWith("/v1/")) {
      const chunks=[];for await(const chunk of req)chunks.push(chunk)
      // A deterministic slow toggle: the commit itself is held, so a person
      // can try to open the editor while the owned enable is still pending.
      if(req.method==="POST"&&req.url.endsWith("/enabled")){enableArrived++;if(holdEnable){const ms=holdEnable;holdEnable=0;await new Promise(r=>setTimeout(r,ms))}}
      const headers={Authorization:`Bearer ${token}`};for(const k of ["content-type","idempotency-key"])if(req.headers[k])headers[k]=req.headers[k]
      const answer=await fetch(upstream+req.url,{method:req.method,headers,...(chunks.length?{body:Buffer.concat(chunks)}:{})})
      const body=Buffer.from(await answer.arrayBuffer());requests.push({method:req.method,path:req.url,status:answer.status,...(chunks.length?{body:JSON.parse(Buffer.concat(chunks))}:{})})
      if(!lostSave&&req.method==="POST"&&req.url.endsWith("/routines")&&answer.status===200) {
        lostSave=true;requests[requests.length-1].response_status=502
        res.writeHead(502,{"Content-Type":"application/json"});res.end('{"synthetic":"response lost after actual commit"}');return
      }
      res.writeHead(answer.status,{"Content-Type":answer.headers.get("Content-Type")??"application/json"});res.end(body);return
    }
    const url=new URL(req.url,"http://fixture")
    if(files.has(url.pathname)){res.setHeader("Content-Type",url.pathname.endsWith(".css")?"text/css":"text/javascript");res.end(files.get(url.pathname));return}
    if(url.pathname==="/")pageLoads++;res.setHeader("Content-Type","text/html");res.end('<!doctype html><html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Routine management TCP fixture</title><script>addEventListener("error",e=>{document.body.append("Fixture error: "+e.message)});addEventListener("unhandledrejection",e=>{document.body.append("Fixture rejection: "+JSON.stringify(e.reason))})</script><style>body{margin:0;background:#101010;color:#eee;font:15px system-ui}#app{height:100vh;max-width:1000px;margin:auto}button{cursor:pointer}.pa-chat{height:100%}</style><link rel="stylesheet" href="/routine.css"><div id="app"></div><script type="module" src="/routine.js"></script></html>')
  }catch(error){res.writeHead(500);res.end(String(error))}
})
await new Promise(resolve=>server.listen(0,"127.0.0.1",resolve))
const origin=`http://127.0.0.1:${server.address().port}`,operations=[]
// A task-owned Chrome with a new empty profile, driven over CDP with real
// mouse, keyboard and text-input events. It never touches the person's
// browser, the daemon on 7727 or a production Clawdline browser broker.
const chromePath=process.env.CHROME??"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
await access(chromePath,constants.X_OK)
const profile=await mkdtemp(path.join(process.env.BOTS_WORK??os.tmpdir(),"routine-profile-"))
const chrome=spawn(chromePath,["--headless=new","--remote-debugging-port=0","--user-data-dir="+profile,"--no-first-run","--use-mock-keychain","--no-default-browser-check","--disable-features=OmniboxEverywhere","--disable-extensions","--disable-sync","about:blank"],{stdio:["ignore","ignore","pipe"]})
const endpoint=await new Promise((resolve,reject)=>{let output="";const timer=setTimeout(()=>reject(Error("Chrome startup timeout: "+output)),20000);chrome.on("error",reject);chrome.stderr.on("data",chunk=>{output+=chunk;const match=/DevTools listening on (ws:\/\/\S+)/.exec(output);if(match){clearTimeout(timer);resolve(match[1])}})})
const ws=new WebSocket(endpoint);await new Promise((resolve,reject)=>{ws.addEventListener("open",resolve);ws.addEventListener("error",reject)})
let serial=0;const pending=new Map(),loadWaiters=[]
ws.addEventListener("message",event=>{const m=JSON.parse(String(event.data));if(!m.id){if(m.method==="Page.loadEventFired")loadWaiters.splice(0).forEach(r=>r());if(m.method==="Runtime.exceptionThrown")operations.push({op:"page-exception",detail:m.params.exceptionDetails?.exception?.description??m.params.exceptionDetails?.text});return}const job=pending.get(m.id);pending.delete(m.id);if(m.error)job?.reject(Error(m.error.message));else job?.resolve(m.result)})
const cdp=(method,params={},sessionId)=>new Promise((resolve,reject)=>{const id=++serial,timer=setTimeout(()=>reject(Error(method+" timed out")),15000);pending.set(id,{resolve:v=>{clearTimeout(timer);resolve(v)},reject:e=>{clearTimeout(timer);reject(e)}});ws.send(JSON.stringify({id,method,params,sessionId}))})
let tab,session
const evaluate=async expression=>{const r=await cdp("Runtime.evaluate",{expression,awaitPromise:true,returnByValue:true},session);if(r.exceptionDetails)throw Error(r.exceptionDetails.exception?.description??r.exceptionDetails.text);return r.result.value}
// The same accessible-name lookup a person uses: aria-label, a label's
// text, or a control's own visible text; visible, enabled elements only.
const locate=label=>evaluate(`(()=>{const want=${JSON.stringify(label)};const visible=e=>{const r=e.getBoundingClientRect();return r.width>0&&r.height>0&&getComputedStyle(e).visibility!=="hidden"};
const named=[...document.querySelectorAll("button,a,input,textarea,select,[role],label,summary")].filter(visible);
let el=named.find(e=>e.getAttribute("aria-label")===want)??named.find(e=>e.tagName!=="LABEL"&&(e.innerText??e.textContent).trim()===want)??named.find(e=>(e.innerText??"").trim().split("\\n")[0].trim()===want)??named.find(e=>e.placeholder===want);
if(!el)return null;if(el.tagName==="LABEL"){const c=el.control??el.querySelector("input,textarea,select");if(c&&visible(c))el=c}
el.scrollIntoView({block:"center",inline:"center"});const r=el.getBoundingClientRect();return {x:r.left+r.width/2,y:r.top+r.height/2,tag:el.tagName,disabled:!!el.disabled}})()`)
const pause=ms=>new Promise(resolve=>setTimeout(resolve,ms))
const keys={ArrowDown:{code:"ArrowDown",keyCode:40},End:{code:"End",keyCode:35},Escape:{code:"Escape",keyCode:27},Enter:{code:"Enter",keyCode:13,text:"\r"}}
async function browser(op,...args) {
  let value
  if(op==="open"){({targetId:tab}=await cdp("Target.createTarget",{url:"about:blank"}));({sessionId:session}=await cdp("Target.attachToTarget",{targetId:tab,flatten:true}));await cdp("Page.enable",{},session);await cdp("Runtime.enable",{},session);await browser("viewport","desktop");const loaded=new Promise(r=>loadWaiters.push(r));await cdp("Page.navigate",{url:args[0]},session);await loaded;value={tab}}
  else if(op==="reload"){const loaded=new Promise(r=>loadWaiters.push(r));await cdp("Page.reload",{ignoreCache:true},session);await loaded;value={reloaded:true}}
  else if(op==="read")value={text:await evaluate("document.body.innerText")}
  else if(op==="click"){let at;for(let i=0;i<30&&(!at||at.disabled);i++){at=await locate(args[0]);if(!at||at.disabled)await pause(100)}if(!at){const seen=await evaluate(`[...document.querySelectorAll("button,[role=button],input,textarea,label")].filter(e=>e.getClientRects().length).map(e=>({tag:e.tagName,aria:e.getAttribute("aria-label"),text:(e.innerText??"").trim().slice(0,80),disabled:!!e.disabled}))`);await writeFile(path.join(artifacts,"missing-control.json"),JSON.stringify({label:args[0],seen},null,2));const {data}=await cdp("Page.captureScreenshot",{format:"jpeg",quality:80},session);await writeFile(path.join(artifacts,"missing-control.jpg"),Buffer.from(data,"base64"));throw Error("No visible control named "+JSON.stringify(args[0]))}if(at.disabled){const {data}=await cdp("Page.captureScreenshot",{format:"jpeg",quality:80},session);await writeFile(path.join(artifacts,"disabled-control.jpg"),Buffer.from(data,"base64"));throw Error("Control stayed disabled for 3 s: "+args[0])}for(const type of ["mouseMoved","mousePressed","mouseReleased"])await cdp("Input.dispatchMouseEvent",{type,x:at.x,y:at.y,button:"left",clickCount:1},session);value=at}
  else if(op==="type"){await cdp("Input.insertText",{text:args[0]},session);value={typed:args[0].length}}
  else if(op==="key"){const k=keys[args[0]];if(!k)throw Error("Unknown key "+args[0]);for(const type of ["rawKeyDown","keyUp"])await cdp("Input.dispatchKeyEvent",{type:type==="rawKeyDown"&&k.text?"keyDown":type,key:args[0],code:k.code,windowsVirtualKeyCode:k.keyCode,...(type!=="keyUp"&&k.text?{text:k.text}:{})},session);value={key:args[0]}}
  else if(op==="viewport"){const mobile=args[0]==="mobile";await cdp("Emulation.setDeviceMetricsOverride",{width:mobile?390:1280,height:844,deviceScaleFactor:mobile?2:1,mobile},session);value={viewport:args[0]}}
  else if(op==="scroll"){await cdp("Input.dispatchMouseEvent",{type:"mouseWheel",x:200,y:400,deltaX:0,deltaY:args[0]==="up"?-600:600},session);value={scrolled:args[0]}}
  else if(op==="screenshot"){const out=args[args.indexOf("--out")+1];const {data}=await cdp("Page.captureScreenshot",{format:"jpeg",quality:80},session);await writeFile(out,Buffer.from(data,"base64"));value={out}}
  else if(op==="close"){await cdp("Target.closeTarget",{targetId:tab});tab=session=undefined;value={closed:true}}
  else throw Error("Unknown browser op "+op)
  operations.push({op,args,result:value});return value
}
const controls=async language=>{const catalog=JSON.parse(await readFile(path.join(consoleRoot,"public/catalogs",language+".json"),"utf8"));return name=>catalog["botRoutineControls."+name]}
async function waitText(text) { let last;for(let i=0;i<40;i++){last=await browser("read");if(last.text?.includes(text))return last;await pause(150)}throw Error(`Missing ${text}: ${JSON.stringify(last)}`) }
try {
  // A damaged durable record is still an uncertain action, never permission
  // to replace it with another test/save. Exercise the production component.
  {
    const {en:copy}=await import("../src/project-assistants/words.ts")
    const opened=await browser("open",origin+"/?lang=en&journal=broken");tab=opened.tab
    await waitText("Synthetic Bot");await browser("click","Synthetic Bot · "+copy.botInformation)
    await waitText(copy.routineJournalUnreadable??copy.unknown)
    const before=requests.filter(r=>r.method!=="GET").length
    try{await browser("click",(await controls("en"))("setUp"))}catch(error){operations.push({op:"blocked-new-routine",detail:String(error)})}
    const read=await browser("read")
    assert.ok(!read.text?.includes(copy.routineInstruction),"Unreadable original command must prevent a replacement form")
    assert.equal(requests.filter(r=>r.method!=="GET").length,before,"Unreadable journal must never send a new operation")
    await browser("screenshot","--out",path.join(artifacts,"unreadable-command-held.jpg"))
    await browser("click",copy.closeInformation);await browser("click","Synthetic Bot · "+copy.botInformation)
    await waitText(copy.routineJournalUnreadable??copy.unknown)
    assert.equal(requests.filter(r=>r.method!=="GET").length,before,"Panel remount must preserve the held state")
    await browser("close");tab=undefined
  }
  for(const language of ["en","zh-Hant"]) {
    const zh=language!=="en",{en,zhHant}=await import("../src/project-assistants/words.ts"),copy=zh?zhHant:en
    const control=await controls(language)
    const w={identity:"Synthetic Bot · "+copy.botInformation,create:control("setUp"),name:copy.name,instructions:copy.routineInstruction,save:copy.save,edit:copy.routineEdit,test:copy.routineTest,pause:copy.routinePause,enable:copy.routineEnable,paused:copy.routinePaused,remove:copy.routineDelete,confirm:copy.routineConfirmDelete,empty:control("emptyRoutines")}

    const opened=await browser("open",origin+"/?lang="+language);tab=opened.tab
    await browser("viewport",zh?"mobile":"desktop")
    await waitText("Synthetic Bot");await browser("click",w.identity);await waitText(w.empty)
    await browser("click",w.create)
    await browser("click",control("webhook"))
    await waitText(control("webhookSave"))
    await browser("click",control("runSettings"))
    await browser("click",control("advancedTriggers"))
    await waitText(control("webhookSave"))
    await browser("screenshot","--out",path.join(artifacts,`${language}-advanced-webhook.jpg`))
    await browser("click",control("schedule"));await browser("click",w.name);await browser("type","Browser interval "+language);await browser("click",w.instructions);await browser("type","Synthetic browser routine task "+language);await browser("screenshot","--out",path.join(artifacts,`${language}-routine-editor.jpg`));await browser("click",control("createRoutine"))
    if(!zh) {
      await waitText(copy.routineHeld)
      assert.equal(requests.filter(r=>r.method==="POST"&&r.path.endsWith("/routines")).length,1,"Unknown outcome never automatically sends a replacement")
      const beforeReload=pageLoads;await browser("reload");await waitText("Synthetic Bot");assert.equal(pageLoads,beforeReload+1,"Same-tab reload was served by the origin");await browser("click",w.identity);await waitText(copy.routineHeld)
      assert.equal(requests.filter(r=>r.method==="POST"&&r.path.endsWith("/routines")).length,1,"Reload preserves the command without sending it")
      await browser("screenshot","--out",path.join(artifacts,"en-routine-held-after-reload.jpg"));await browser("click",copy.routineReplay)
    }
    await waitText("Browser interval "+language)
    await browser("screenshot","--out",path.join(artifacts,`${language}-routine-list.jpg`))
    await browser("click",`${copy.routineEdit}: Browser interval ${language}`);await waitText(w.instructions)
    // Appending verifies an actual edit while keeping the trigger untouched.
    await browser("click",w.name);await browser("key","ArrowDown");await browser("type"," edited");await browser("click",w.save)
    await waitText("edited")
    // Test run, history, delete and the switch follow Codync: one row per routine, actions in its editor.
    const edited=`Browser interval ${language} edited`
    await browser("click",`${copy.routineEdit}: ${edited}`);await waitText(w.instructions);await browser("click",w.test)
    for(let i=0;i<8;i++){await pause(250);const read=await browser("read");if(read.text?.includes("Actual isolated routine result"))break;if(i%3===2){await browser("click",copy.routineHistory);await browser("click",copy.routineHistory)}}
    await waitText("Actual isolated routine result")
    await browser("scroll","down")
    await browser("screenshot","--out",path.join(artifacts,`${language}-routine-result.jpg`))
    await browser("key","Escape")
    await browser("click",`${copy.routineEnabled}: ${edited}`);await waitText(w.paused);await browser("screenshot","--out",path.join(artifacts,`${language}-routine-paused.jpg`));const toggles=requests.filter(r=>r.path.endsWith("/enabled")).length
    // The re-enable's commit is held for 1.5 s. The person clicks the row as
    // soon as the toggle has been sent: the editor must not open on the
    // revision that the pending enable is about to replace.
    const arrived=enableArrived;holdEnable=1500
    await browser("click",`${copy.routineEnabled}: ${edited}`)
    for(let i=0;i<40&&enableArrived===arrived;i++)await pause(25)
    assert.equal(enableArrived,arrived+1,"Re-enable reached the fixture")
    // While the source holds the commit the row says what is happening, not the
    // state it is leaving, and a polite live region announces it.
    const pendingRow=await evaluate(`(()=>{const li=[...document.querySelectorAll(".pa-routine-card")].find(e=>e.querySelector("strong")?.textContent===${JSON.stringify(edited)});const sw=li?.querySelector(".pa-routine-switch");return {line:li?.querySelector(".pa-routine-row small")?.textContent??"",checked:sw?.getAttribute("aria-checked"),busy:sw?.getAttribute("aria-busy"),status:document.querySelector(".pa-routines [role=status][aria-live=polite], [role=status][aria-live=polite]")?.textContent??""}})()`)
    operations.push({op:"pending-enable-row",...pendingRow})
    assert.equal(pendingRow.line,copy.routineEnabling,"Pending row shows Enabling, not the paused draft")
    assert.equal(pendingRow.checked,"false","Switch keeps the confirmed state until the source answers")
    assert.equal(pendingRow.busy,"true")
    assert.equal(pendingRow.status,`${copy.routineEnabling}: ${edited}`)
    await browser("screenshot","--out",path.join(artifacts,`${language}-routine-enable-pending.jpg`))
    const clickedAt=Date.now(),row=await browser("click",`${copy.routineEdit}: ${edited}`);await waitText(w.instructions)
    const answeredBeforeEditor=requests.filter(r=>r.path.endsWith("/enabled")).length===toggles+1
    const settledRow=await evaluate(`(()=>{const li=[...document.querySelectorAll(".pa-routine-card")].find(e=>e.querySelector("strong")?.textContent===${JSON.stringify(edited)});return {line:li?.querySelector(".pa-routine-row small")?.textContent??"",checked:li?.querySelector(".pa-routine-switch")?.getAttribute("aria-checked"),busy:li?.querySelector(".pa-routine-switch")?.getAttribute("aria-busy")}})()`)
    operations.push({op:"settled-enable-row",...settledRow})
    assert.equal(settledRow.checked,"true","After the answer the switch shows the actual enabled state");assert.equal(settledRow.busy,null)
    assert.ok(settledRow.line&&settledRow.line!==copy.routineEnabling&&!settledRow.line.startsWith(copy.routinePaused),"Settled row shows the source outcome: "+settledRow.line)
    operations.push({op:"edit-during-pending-enable",waited_ms:Date.now()-clickedAt,row,answeredBeforeEditor})
    // A second, stale client still loses compare-and-swap against the newer revision.
    const latest=[...requests].reverse().find(r=>r.path.endsWith("/enabled")&&r.body),staleKey=crypto.randomUUID()
    const stale={...latest.body,command:{...latest.body.command,idempotency_key:staleKey},enabled:false}
    const foreign=await fetch(upstream+latest.path,{method:"POST",headers:{Authorization:`Bearer ${token}`,"Content-Type":"application/json","Idempotency-Key":staleKey},body:JSON.stringify(stale)})
    const refusal=await foreign.text();operations.push({op:"stale-second-client",status:foreign.status,expected_revision:stale.expected_revision,refusal})
    assert.equal(foreign.status,409,"Stale second client must be refused: "+refusal);assert.match(refusal,/pa_stale_version/)
    await browser("click",w.remove);await waitText(w.confirm);await browser("click",w.confirm);await waitText(w.empty)
    assert.ok(answeredBeforeEditor,"Editor opened only after the owned enable answered")
    await browser("screenshot","--out",path.join(artifacts,`${language}-routine-deleted-history.jpg`))
    await browser("close");tab=undefined
  }
  const writes=requests.filter(r=>r.method!=="GET")
  assert.equal(writes.filter(r=>r.path.includes("/messages")).length,0,"Manager cannot send main chat text")
  assert.equal(writes.filter(r=>r.path.endsWith("/runs")).length,2,"Only explicit test buttons request runs")
  assert.equal(writes.filter(r=>r.path.endsWith("/routines")).length,5,"Create/edit per language plus one exact lost-response replay")
  assert.ok(writes.every(r=>r.status===200),JSON.stringify(writes))
  const saved=writes.filter(r=>r.path.endsWith("/routines"));assert.deepEqual(saved[0].body,saved[1].body,"Explicit lost-response replay preserves exact command/payload")
  await writeFile(path.join(artifacts,"browser-requests.json"),JSON.stringify(requests,null,2)+"\n")
  await writeFile(path.join(artifacts,"browser-operations.json"),JSON.stringify(operations,null,2)+"\n")
  console.log("Production Chat/SourceClient -> real Go TCP/Host/Store; desktop English and mobile Taiwan Chinese; synthetic create/edit/test/results/pause/enable/delete; no chat sends; external/native/model requests=0")
} finally {
  if(tab)await browser("close").catch(error=>console.error("Owned tab cleanup:",error.message))
  await writeFile(path.join(artifacts,"browser-operations.json"),JSON.stringify(operations,null,2)+"\n")
  await writeFile(path.join(artifacts,"browser-requests.json"),JSON.stringify(requests,null,2)+"\n")
  ws.close();chrome.kill();await new Promise(resolve=>chrome.exitCode!==null||chrome.signalCode!==null?resolve():chrome.once("exit",resolve))
  await rm(profile,{recursive:true,force:true,maxRetries:5})
  server.closeAllConnections();await new Promise(resolve=>server.close(resolve))
}
