/** Real library UI/source clients against a task-owned Go HTTP/SQLite fixture. */
import assert from "node:assert/strict"
import {createServer} from "node:http"
import {spawn} from "node:child_process"
import {createHash} from "node:crypto"
import {mkdir,mkdtemp,readFile,writeFile,rm,readdir,access} from "node:fs/promises"
import {constants} from "node:fs"
import {homedir} from "node:os"
import path from "node:path"
import {fileURLToPath} from "node:url"
import {build} from "esbuild"
const {BOTS_ORIGIN:upstream,BOTS_TOKEN:token,BOTS_READ_TOKEN:readToken,BOTS_MACHINE:machine,BOTS_BOT:bot,BOTS_ARTIFACTS:artifacts}=process.env
assert.ok(upstream&&token&&readToken&&machine&&bot&&artifacts)
await mkdir(artifacts,{recursive:true})
const root=fileURLToPath(new URL("..",import.meta.url)),work=path.join(path.dirname(artifacts),"work")
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
import React,{useState} from "react";import {createRoot} from "react-dom/client";
import {OutputLibrary} from "./src/project-assistants/OutputLibrary";
import {ProjectAssistantSourceClient,capturedAssistantRelayFetch} from "./src/cloud/project-assistants";
import {words} from "./src/project-assistants/words";import {bootCatalog} from "./src/catalog";
import "./src/project-assistants/styles.css";
const q=new URL(location.href).searchParams,mode=q.get("mode"),lang=q.get("lang");localStorage.setItem("ui_language",lang);await bootCatalog(tag=>"/catalog/"+tag+".json");
const grant={accountKey:mode==="cloud"?"synthetic-account":"local",readerID:"synthetic-reader",machineID:${JSON.stringify(machine)},authorizationRevision:"grant",apiLevel:78,paired:true,online:true,writable:mode!=="read"};
const verbs=["project-assistants.get","project-assistants.view","project-assistants.output","project-assistants.input","project-assistants.attachment","project-assistants.rename-artifact"];
const relay={_machineRequestAs:async(id,_,word,fields)=>await(await fetch("/relay/"+word,{method:"POST",body:JSON.stringify(fields)})).json(),machineDescriptor:()=>({machine:{commands:verbs}}),_machineRequest:async(_,word,fields)=>await(await fetch("/relay/"+word,{method:"POST",body:JSON.stringify(fields)})).json()};
const source=new ProjectAssistantSourceClient(grant,mode==="cloud"?capturedAssistantRelayFetch(grant.machineID,()=>relay,verbs):(input,init)=>fetch("/proxy/"+mode+input,init),()=>grant);
const got=await source.get({machine_id:grant.machineID,bot_id:${JSON.stringify(bot)}},new AbortController().signal);
function Fixture(){const [messages,setMessages]=useState([]);window.addLibraryItem=()=>setMessages([{id:"live-change",seq:999,at:new Date().toISOString(),fence:{key:got.bot.key,project:got.bot.project,conversation_id:got.bot.conversation_id,binding_id:got.conversation.binding_id,generation:got.conversation.generation},kind:"user",text:"",turn_id:"",quote_message_id:"",files:[{id:"22222222-2222-4222-8222-222222222222",name:"Live attachment",bytes:0,sha256:"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"}]}]);return <main className="project-assistants" style={{height:"100vh",background:"#080808"}}><OutputLibrary source={source} bot={got.bot} messages={messages} w={words(lang)} onClose={()=>{}}/></main>}
createRoot(document.getElementById("app")).render(<Fixture/>);
`,resolveDir:root,loader:"tsx"},bundle:true,write:false,outfile:"library.js",format:"esm",platform:"browser",jsx:"automatic",define:{"process.env.NODE_ENV":'"development"'}})
const files=new Map(bundle.outputFiles.map(f=>["/"+path.basename(f.path),f.contents])),requests=[],protocolEvents=[]
let fault=""
const server=createServer(async(req,res)=>{try{
 const url=new URL(req.url,"http://fixture")
 if(url.pathname==="/fault"){fault=url.searchParams.get("kind");res.end("ok");return}
 if(url.pathname.startsWith("/proxy/")||url.pathname.startsWith("/relay/")){
  const relay=url.pathname.startsWith("/relay/"),mode=relay?"cloud":url.pathname.split("/")[2];let route=url.pathname.replace("/proxy/"+mode,"")+url.search,method=req.method,bodyToSend,headersToSend={Authorization:"Bearer "+(mode==="read"?readToken:token)}
  if(!relay&&method!=="GET"){const chunks=[];for await(const chunk of req)chunks.push(chunk);bodyToSend=Buffer.concat(chunks);headersToSend["Content-Type"]="application/json";headersToSend["Idempotency-Key"]=req.headers["idempotency-key"]}
  if(relay){const chunks=[];for await(const chunk of req)chunks.push(chunk);const fields=JSON.parse(Buffer.concat(chunks).toString()),word=url.pathname.slice(7).split(".").at(-1),key=fields.key??fields.command?.key??fields;const {key:_,machine_id:__,bot_id:___,...rest}=fields;
    if(word==="rename-artifact"){route="/v1/project-assistants/"+key.bot_id+"/artifacts/"+fields.artifact_id+"/rename";method="POST";bodyToSend=JSON.stringify(fields);headersToSend["Content-Type"]="application/json";headersToSend["Idempotency-Key"]=fields.command.idempotency_key}
    else {method="GET";route="/v1/project-assistants/"+key.bot_id+(word==="get"?"":word==="attachment"?"/attachments/"+fields.id:"/"+word)+"?"+new URLSearchParams({machine_id:key.machine_id,...(["get","attachment"].includes(word)?{}:rest)})}}

  if(relay&&route.includes("/rename-artifact?")){throw Error("rename routing failed")}
  const record={mode,path:route,idempotency:headersToSend["Idempotency-Key"]};requests.push(record)
  const unknownRename=route.endsWith("/rename")&&fault==="unknown";if(unknownRename)fault=""
  if(route.includes("/output?")&&fault){const kind=fault;fault="";if(kind==="fail"){record.status=503;res.writeHead(503,{"Content-Type":"application/json"});res.end(JSON.stringify({contract_version:"project-assistants.v1",error:{code:"pa_not_found",field:"snapshot"}}));return}await new Promise(resolve=>setTimeout(resolve,1200))}
  const answer=await fetch(upstream+route,{method,body:bodyToSend,headers:headersToSend}),body=Buffer.from(await answer.arrayBuffer()),mediaType=answer.headers.get("content-type")??"application/json";record.status=answer.status;record.mediaType=mediaType;record.bytes=body.length
  if(unknownRename&&answer.ok){res.writeHead(503,{"Content-Type":"application/json"});res.end(JSON.stringify({contract_version:"project-assistants.v1",error:{code:"pa_outcome_unknown",field:"lost_ack"}}));return}
  if(answer.ok&&route.includes("/output?")){const value=JSON.parse(body);record.output=value.output}
  // The production Cloud attachment operation wraps local image bytes in JSON.
  // Local/read-only HTTP retains the original image response.
  if(relay&&answer.ok&&route.includes("/attachments/")){
    const id=new URL(upstream+route).pathname.split("/").at(-1)
    assert.equal(createHash("sha256").update(body).digest("hex"),id)
    record.encoding="cloud-image-envelope";res.writeHead(answer.status,{"Content-Type":"application/json"});res.end(JSON.stringify({contract_version:"project-assistants.v1",id,media_type:mediaType,byte_count:body.length,data:body.toString("base64")}));return
  }
  res.writeHead(answer.status,{"Content-Type":mediaType});res.end(body);return
 }
 if(url.pathname.startsWith("/catalog/")){res.setHeader("Content-Type","application/json");res.end(await readFile(path.join(root,"public/catalogs",path.basename(url.pathname))));return}
 if(files.has(url.pathname)){res.setHeader("Content-Type",url.pathname.endsWith(".css")?"text/css":"text/javascript");res.end(files.get(url.pathname));return}
 res.setHeader("Content-Type","text/html");res.end('<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="stylesheet" href="/library.css"><div id="app"></div><script type="module" src="/library.js"></script>')
 }catch(e){res.writeHead(500);res.end(String(e))}})
await new Promise(resolve=>server.listen(0,"127.0.0.1",resolve));const origin=`http://127.0.0.1:${server.address().port}`
const profile=await mkdtemp(path.join(work,"library-profile-")),executable=await testBrowser(),chrome=spawn(executable,["--headless=new","--remote-debugging-port=0","--use-mock-keychain","--user-data-dir="+profile,"--no-first-run","--no-default-browser-check","--disable-features=OmniboxEverywhere","about:blank"],{stdio:["ignore","ignore","pipe"]})
let ws
try {
  const endpoint=await new Promise((resolve,reject)=>{let output="";const timer=setTimeout(()=>reject(Error("Chrome startup timeout: "+output)),20000);chrome.on("error",reject);chrome.on("exit",(code,signal)=>{clearTimeout(timer);reject(Error("Chrome exited: "+code+" "+signal+" "+output))});chrome.stderr.on("data",chunk=>{output+=chunk;const match=/DevTools listening on (ws:\/\/\S+)/.exec(output);if(match){clearTimeout(timer);resolve(match[1])}})})
  ws=new WebSocket(endpoint);await new Promise((resolve,reject)=>{ws.addEventListener("open",resolve);ws.addEventListener("error",reject)})
  let serial=0;const pending=new Map()
  ws.addEventListener("message",event=>{const message=JSON.parse(String(event.data));if(!message.id){if(message.method?.startsWith("Page.")||message.method?.includes("crashed")||["Runtime.exceptionThrown","Runtime.consoleAPICalled","Log.entryAdded"].includes(message.method))protocolEvents.push(message);return};const job=pending.get(message.id);pending.delete(message.id);if(message.error)job?.reject(Error(message.error.message));else job?.resolve(message.result)})
  const send=(method,params={},sessionId)=>new Promise((resolve,reject)=>{const id=++serial,timer=setTimeout(()=>reject(Error(method+" timed out")),15000);pending.set(id,{resolve:v=>{clearTimeout(timer);resolve(v)},reject:e=>{clearTimeout(timer);reject(e)}});ws.send(JSON.stringify({id,method,params,sessionId}))})
  const attach=async targetId=>{
    const {sessionId}=await send("Target.attachToTarget",{targetId,flatten:true})
    await send("Page.enable",{},sessionId)
    await send("Runtime.enable",{},sessionId)
    await send("Log.enable",{},sessionId)
    await send("Page.setLifecycleEventsEnabled",{enabled:true},sessionId)
    return sessionId
  }
  const results=[]
  await send("Browser.setDownloadBehavior",{behavior:"allow",downloadPath:artifacts})
  // local/en carries the lost-acknowledgement rename; it runs last so a
  // failure there cannot hide the other modes' rename/reopen evidence.
  for(const [mode,lang,width] of [["local","zh-Hant",390],["cloud","en",1280],["cloud","zh-Hant",390],["read","en",390],["local","en",1280]]){
    const {targetId}=await send("Target.createTarget",{url:"about:blank"}),sessionId=await attach(targetId)
    let lastExpression=""
    const run=async(expression)=>{lastExpression=expression;const r=await send("Runtime.evaluate",{expression,awaitPromise:true,returnByValue:true,userGesture:true},sessionId);if(r.exceptionDetails)throw Error(r.exceptionDetails.exception?.description??r.exceptionDetails.text);return r.result.value}
    const until=async(expression)=>{const deadline=Date.now()+10000;while(!await run(expression)){if(Date.now()>deadline)throw Error("Timed out waiting for: "+expression);await new Promise(r=>setTimeout(r,50))}}
    const click=async(label)=>run(`(()=>{const button=[...document.querySelectorAll("button")].find(b=>b.textContent===${JSON.stringify(label)});if(!button)throw Error("Missing button: "+${JSON.stringify(label)});button.click()})()`)
    const zh=lang!=="en",close=zh?"關閉":"Close",earlier=zh?"較早訊息":"Earlier messages"
    // Saved HTML artifacts open in the workspace dialog: its top bar holds the
    // close button and the title button (aria-label "Rename artifact"). File
    // viewers keep the separate .pa-output-overlay selectors above and below.
    const renameTitle=async()=>{const label=zh?"重新命名作品":"Rename artifact";assert.equal(await run('document.querySelector(".pa-artifact-workspace[open] .pa-artifact-bar .pa-artifact-rename > button")?.getAttribute("aria-label")??null'),label,"title button carries the rename name");await run('document.querySelector(".pa-artifact-workspace[open] .pa-artifact-bar .pa-artifact-rename > button").click()');await until('!!document.querySelector(".pa-artifact-rename form input")')}
    const closeArtifact=async()=>{await run('document.querySelector(".pa-artifact-workspace[open] .pa-artifact-bar > button.pa-circle").click()');await until('!document.querySelector(".pa-artifact-workspace[open]:not([data-closing])")')}
    try {
    await send("Emulation.setDeviceMetricsOverride",{width,height:844,deviceScaleFactor:1,mobile:width<700},sessionId)
    const historyBefore=requests.filter(r=>r.mode===mode&&r.path.includes("mode=history")).length
    await send("Page.navigate",{url:origin+"/?mode="+mode+"&lang="+lang},sessionId)
    await until('!!document.querySelector(".pa-library")')
    await until('[...document.querySelectorAll("button")].some(b=>/Earlier|較早/.test(b.textContent)&&!b.disabled)')
    assert.equal(requests.filter(r=>r.mode===mode&&r.path.includes("mode=history")).length,historyBefore,"history is on demand before the first click")
    await run('[...document.querySelectorAll("button")].find(b=>/Earlier|較早/.test(b.textContent)).click()')
    await until('document.querySelectorAll("[data-library-kind]").length===4')
    assert.equal(await run('document.querySelectorAll(".pa-library-group").length'),2)
    assert.deepEqual(await run('[...document.querySelectorAll("time")].map(t=>t.dateTime)'),await run('[...document.querySelectorAll("time")].map(t=>t.dateTime).sort().reverse()'))
    await run('document.querySelector("[data-library-kind=input] button").click()')
    await until('[...document.querySelectorAll(".pa-output-overlay button")].some(b=>/Download file|下載檔案/.test(b.textContent))')
    await click(zh?"下載檔案":"Download file")
    await until('!!document.querySelector(".pa-output-overlay")');await run('document.querySelector(".pa-output-overlay header button").click()')
    await run('document.querySelector("[data-library-kind=picture] button").click()')
    await until('!!document.querySelector(".pa-output-overlay")')
    await until('!!document.querySelector(".pa-output-overlay [role=alert]")||document.querySelector(".pa-library-picture")?.complete')
    assert.equal(await run('document.querySelector(".pa-output-overlay [role=alert]")?.textContent??null'),null,"picture transfer must succeed")
    assert.equal(await run('document.querySelector(".pa-library-picture")?.naturalWidth'),1,"stored PNG decodes")
    await run('document.querySelector(".pa-output-overlay header button").click()')
    await run('(()=>{const card=[...document.querySelectorAll(".pa-output-card button")].find(b=>b.textContent.includes("Interactive report"));card.focus();card.click()})()')
    await until('!!document.querySelector(".pa-artifact-workspace[open] .pa-artifact-frame")')
    if(mode!=="read") {
      const artifactBefore=requests.findLast(row=>row.output?.artifact_id)?.output
      assert.ok(artifactBefore?.artifact_id,"capture the actual opened artifact identity")
      const cards=()=>run('[...document.querySelectorAll(".pa-output-card strong")].map(row=>row.textContent)')
      const cardBefore=await cards()
      assert.ok(cardBefore.includes("Interactive report"),JSON.stringify(cardBefore))
      await renameTitle()
      await run(`(()=>{const field=document.querySelector(".pa-artifact-rename input");const set=Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,"value").set;set.call(field,${JSON.stringify(zh?"手機改名作品":"Renamed library artifact")});field.dispatchEvent(new Event("input",{bubbles:true}))})()`)
      const unknown=mode==="local"&&!zh,renameStart=requests.filter(r=>r.path.endsWith("/rename")).length
      if(unknown)await fetch(origin+"/fault?kind=unknown")
      await run('document.querySelector(".pa-artifact-rename form").requestSubmit()')
      if(unknown){
        await until('document.querySelector(".pa-artifact-rename [role=alert]")?.textContent.includes("pa_outcome_unknown")')
        assert.equal(await run('document.querySelector(".pa-artifact-rename input").disabled'),true)
        assert.equal(requests.filter(r=>r.path.endsWith("/rename")).length,renameStart+1,"unknown command does not replay automatically")
        await closeArtifact()
        await until('!document.querySelector(".pa-artifact-workspace")')
        // The lost acknowledgement hid a committed rename, so the refreshed
        // card may already show the new title; the person reopens that card.
        protocolEvents.push({unknownReopenCards:await run('[...document.querySelectorAll(".pa-output-card strong")].map(row=>row.textContent)')})
        await run('(()=>{const card=[...document.querySelectorAll(".pa-output-card button")].find(b=>/Interactive report|Renamed library artifact/.test(b.textContent));card.focus();card.click()})()')
        await until('!!document.querySelector(".pa-artifact-workspace[open]")')
        // A reopened viewer reads the stored output once; a refetch loop is a
        // product defect, reported with its measured rate instead of a timeout.
        const outputsAtReopen=requests.filter(r=>r.path.includes("/output?")).length;await new Promise(r=>setTimeout(r,1500))
        const storm=requests.filter(r=>r.path.includes("/output?")).length-outputsAtReopen
        if(storm>5){const {data}=await send("Page.captureScreenshot",{format:"png"},sessionId);await writeFile(path.join(artifacts,"library-output-storm.png"),Buffer.from(data,"base64"));await writeFile(path.join(artifacts,"library-output-storm.json"),JSON.stringify({action:"lost-ack rename committed, close workspace, reopen artifact card",outputRequestsIn1500ms:storm,heldFormVisible:await run('!!document.querySelector(".pa-artifact-rename form")'),title:await run('document.querySelector(".pa-artifact-workspace[open] .pa-artifact-heading > span:last-child")?.textContent??null')},null,2));throw Error(`Reopening the artifact after a lost-acknowledgement rename refetched /output ${storm} times in 1.5 s`)}
        await until('document.querySelector(".pa-artifact-rename input")?.disabled===true')
        await run('document.querySelector(".pa-artifact-rename form").requestSubmit()')
      }
      await until('document.querySelector(".pa-artifact-workspace[open] .pa-artifact-heading > span:last-child")?.textContent==='+JSON.stringify(zh?"手機改名作品":"Renamed library artifact"))
      await until('document.querySelector(".pa-artifact-bar .pa-artifact-rename > button")?.getAttribute("aria-label")==='+JSON.stringify(zh?"重新命名作品":"Rename artifact")+'&&!document.querySelector(".pa-artifact-rename [role=alert]")')
      if(unknown){const commands=requests.filter(r=>r.path.endsWith("/rename")).slice(renameStart);assert.equal(commands.length,2);assert.equal(commands[0].idempotency,commands[1].idempotency,"explicit retry keeps original intent key")}

      const renamed=zh?"手機改名作品":"Renamed library artifact"
      const proof={artifactBefore,cardBefore,renamed}
      try{await until('[...document.querySelectorAll(".pa-output-card strong")].some(row=>row.textContent==='+JSON.stringify(renamed)+')')}
      finally{proof.cardAfter=await cards();proof.artifactAfter=requests.findLast(row=>row.output?.artifact_id)?.output;await writeFile(path.join(artifacts,`rename-card-${mode}-${lang}-${width}.json`),JSON.stringify(proof,null,2))}
      assert.equal(proof.artifactAfter.artifact_id,artifactBefore.artifact_id)
      await closeArtifact()
      assert.ok((await cards()).includes(renamed),"closing the viewer lost the renamed old-history card")
      // Focus return is still a gate; it is judged after the reopen/bytes proof
      // below so one failure does not hide the other evidence.
      const focusOK=await until('document.activeElement?.closest(".pa-output-card")?.querySelector("strong")?.textContent==='+JSON.stringify(renamed)).then(()=>true,()=>false)
      proof.focusAfterClose=await run('document.activeElement?.closest(".pa-output-card")?.querySelector("strong")?.textContent??("not a card: "+(document.activeElement?.tagName??"none")+" "+(document.activeElement?.className??""))')
      proof.outputRequestsSoFar=requests.filter(r=>r.mode===mode&&r.path.includes("/output?")).length
      await run('(()=>{const card=[...document.querySelectorAll(".pa-output-card button")].find(row=>row.querySelector("strong")?.textContent==='+JSON.stringify(renamed)+');card.focus();card.click()})()')
      await until('document.querySelector(".pa-artifact-workspace[open] .pa-artifact-heading > span:last-child")?.textContent==='+JSON.stringify(renamed))
      await until('!!document.querySelector(".pa-artifact-frame")')
      const reopened=requests.findLast(row=>row.output?.artifact_id)?.output
      assert.equal(reopened.artifact_id,artifactBefore.artifact_id)
      assert.equal(reopened.sha256,artifactBefore.sha256,"renaming changed the stored artifact bytes")
      proof.cardAfterReopen=await cards();proof.reopened=reopened
      await writeFile(path.join(artifacts,`rename-card-${mode}-${lang}-${width}.json`),JSON.stringify(proof,null,2))
      assert.ok(focusOK,"closing the artifact returns focus to its renamed card: "+proof.focusAfterClose)
      const {data:renameShot}=await send("Page.captureScreenshot",{format:"png"},sessionId);await writeFile(path.join(artifacts,`rename-${mode}-${lang}-${width}.png`),Buffer.from(renameShot,"base64"))
      await renameTitle()
      await run('(()=>{const field=document.querySelector(".pa-artifact-rename input");Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,"value").set.call(field,"Interactive report");field.dispatchEvent(new Event("input",{bubbles:true}))})()')
      await run('document.querySelector(".pa-artifact-rename form").requestSubmit()')
      await until('document.querySelector(".pa-artifact-workspace[open] .pa-artifact-heading > span:last-child")?.textContent==="Interactive report"')
    } else assert.equal(await run('document.querySelector(".pa-artifact-rename")'),null,"read-only has no rename control")
    const before=await send("Target.getTargets")
    await click(zh?"在瀏覽器開啟":"Open in browser")
    let opened;const deadline=Date.now()+5000
    while(!opened){opened=(await send("Target.getTargets")).targetInfos.find(t=>t.type==="page"&&!before.targetInfos.some(b=>b.targetId===t.targetId));assert.ok(Date.now()<deadline);await new Promise(r=>setTimeout(r,50))}
    const tabSession=await attach(opened.targetId)
    const evaluate=async(expression)=>{const r=await send("Runtime.evaluate",{expression,returnByValue:true},tabSession);return r.result.value}
    assert.equal(await evaluate('window.opener===null'),true)
    assert.equal(await evaluate('document.querySelector("iframe").getAttribute("sandbox")'),"allow-scripts")
    let tree;const frameDeadline=Date.now()+5000
    do {tree=await send("Page.getFrameTree",{},tabSession);if(tree.frameTree.childFrames?.[0]?.childFrames?.[0])break;assert.ok(Date.now()<frameDeadline,"new tab keeps two nested frames");await new Promise(r=>setTimeout(r,50))}while(true)
    const innerFrame=tree.frameTree.childFrames[0].childFrames[0].frame.id
    const {executionContextId}=await send("Page.createIsolatedWorld",{frameId:innerFrame,worldName:"library-acceptance"},tabSession)
    const inner=async(expression)=>{const r=await send("Runtime.evaluate",{expression,contextId:executionContextId,awaitPromise:true,returnByValue:true},tabSession);if(r.exceptionDetails)throw Error(r.exceptionDetails.text);return r.result.value}
    const ready=Date.now()+5000;while(await inner('document.querySelector("#isolation")?.textContent')!=="Isolated 2"){assert.ok(Date.now()<ready,"artifact script runs in isolation");await new Promise(r=>setTimeout(r,50))}
    assert.equal(await inner('(()=>{try{return !!parent.parent.document}catch{return false}})()'),false)
    assert.equal(await inner('(()=>{try{localStorage.getItem("ui_language");return true}catch{return false}})()'),false)
    await inner('[...document.querySelectorAll("button")].find(b=>b.textContent==="Try interaction").click()')
    assert.equal(await inner('document.querySelector("button").textContent'),"Clicked")
    assert.equal(await inner('fetch("https://escape.invalid/test").then(()=>false,()=>true)'),true)
    assert.equal(await inner('window.open("https://escape.invalid/test")===null'),true)
    const targetsAfter=(await send("Target.getTargets")).targetInfos.filter(t=>t.type==="page").map(t=>t.targetId).sort()
    assert.deepEqual(targetsAfter,[...before.targetInfos.filter(t=>t.type==="page").map(t=>t.targetId),opened.targetId].sort(),"artifact cannot create popups")
    assert.equal(await evaluate('document.body.dataset.compromised'),undefined)
    await send("Target.closeTarget",{targetId:opened.targetId})
    await closeArtifact()
    if(mode==="local"&&lang==="en"){
      await fetch(origin+"/fault?kind=delay");await run('[...document.querySelectorAll(".pa-output-card button")].find(b=>b.textContent.includes("result.bin")).click()')
      await until('!!document.querySelector(".pa-output-overlay [role=status]")');await click("Cancel download");await new Promise(r=>setTimeout(r,1400));assert.ok(await run('document.querySelector(".pa-output-overlay").textContent.includes("Download cancelled")'));assert.equal(await run('document.querySelector(".pa-output-overlay .pa-library-actions")'),null)
      await run('document.querySelector(".pa-output-overlay header button").click()')
      await fetch(origin+"/fault?kind=fail")
      await run('[...document.querySelectorAll(".pa-output-card button")].find(b=>b.textContent.includes("result.bin")).click()')
      await until('!!document.querySelector(".pa-output-overlay [role=alert]")')
      assert.ok(await run('document.querySelector(".pa-output-overlay [role=alert]").textContent.includes("pa_not_found")'))
      await click("Try again")
      await until('!!document.querySelector(".pa-library-actions")')
      await run('document.querySelector(".pa-output-overlay header button").click()')
    }
    const snapshots=requests.filter(r=>r.path.includes("mode=snapshot")).length
    await run('window.addLibraryItem()');await until('document.querySelectorAll("[data-library-kind]").length===5');const refreshDeadline=Date.now()+5000;while(requests.filter(r=>r.path.includes("mode=snapshot")).length===snapshots){assert.ok(Date.now()<refreshDeadline);await new Promise(r=>setTimeout(r,50))}
    assert.ok(await run('document.querySelector(".pa-library").textContent.includes("library-notes.txt")'),"refresh retains earlier pages")
    const {data}=await send("Page.captureScreenshot",{format:"png"},sessionId);await writeFile(path.join(artifacts,`library-${mode}-${lang}-${width}.png`),Buffer.from(data,"base64"))
    await send("Target.closeTarget",{targetId});results.push({mode,lang,width})
    }catch(error){
      const failure={mode,lang,width,targetId,expression:lastExpression,error:error.stack??String(error)}
      // Preserve the actual failed surface before the browser/profile is removed.
      try {
        const snapshot=await send("Runtime.evaluate",{expression:`(()=>({url:location.href,bodyText:document.body.innerText,library:document.querySelector(".pa-library")?.outerHTML??null,overlay:document.querySelector(".pa-output-overlay")?.outerHTML??null,cards:[...document.querySelectorAll(".pa-output-card")].map(card=>({kind:card.dataset.libraryKind??null,text:card.textContent,html:card.outerHTML})),buttons:[...document.querySelectorAll("button")].map(button=>({text:button.textContent,disabled:button.disabled,visible:!!button.getClientRects().length,card:!!button.closest(".pa-output-card"),overlay:!!button.closest(".pa-output-overlay")}))}))()`,returnByValue:true},sessionId)
        failure.page=snapshot.result?.value??snapshot
      }catch(captureError){failure.captureError=String(captureError)}
      try {const {data}=await send("Page.captureScreenshot",{format:"png"},sessionId);await writeFile(path.join(artifacts,"library-failure.png"),Buffer.from(data,"base64"))}catch(captureError){failure.screenshotError=String(captureError)}
      await writeFile(path.join(artifacts,"library-failure.json"),JSON.stringify(failure,null,2))
      throw error
    }
  }
  assert.deepEqual(await readFile(path.join(artifacts,"library-notes.txt")),Buffer.from("Explicit library input.\n"))
  await writeFile(path.join(artifacts,"library-results.json"),JSON.stringify({results,providerRequests:0},null,2));console.log(JSON.stringify(results))
}finally{
  await writeFile(path.join(artifacts,"library-requests.json"),JSON.stringify(requests,null,2));await writeFile(path.join(artifacts,"library-protocol.json"),JSON.stringify(protocolEvents,null,2));ws?.close();chrome.kill();await new Promise(resolve=>chrome.exitCode!==null||chrome.signalCode!==null?resolve():chrome.once("exit",resolve));server.closeAllConnections();await new Promise(resolve=>server.close(resolve));await rm(profile,{recursive:true,force:true,maxRetries:5})
}
