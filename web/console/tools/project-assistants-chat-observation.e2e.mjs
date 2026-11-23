/** Follow an accepted send through the real Chat, Cloud decoder, HTTP Host and SQLite. */
import assert from "node:assert/strict"
import {createServer} from "node:http"
import {spawn} from "node:child_process"
import {access,mkdir,mkdtemp,readFile,readdir,rm,writeFile} from "node:fs/promises"
import {constants} from "node:fs"
import {userInfo,tmpdir} from "node:os"
import path from "node:path"
import {fileURLToPath} from "node:url"
import {build} from "esbuild"

const {BOTS_CLOUD_ORIGIN:cloud,BOTS_MACHINE:machine,BOTS_ARTIFACTS:artifacts,BOTS_CASE:scenario}=process.env
assert.ok(cloud&&machine&&artifacts,"requires the task-owned Go chat fixture")
await mkdir(artifacts,{recursive:true})
const root=fileURLToPath(new URL("..",import.meta.url)),fixture=path.join(root,"tools/fixtures/project-assistants-chat-observation.tsx")
async function testBrowser(){
  if(process.env.CHROME){await access(process.env.CHROME,constants.X_OK);return process.env.CHROME}
  const cache=process.env.PLAYWRIGHT_BROWSERS_PATH??(process.platform==="darwin"?path.join(userInfo().homedir,"Library/Caches/ms-playwright"):process.platform==="win32"?path.join(process.env.LOCALAPPDATA??path.join(userInfo().homedir,"AppData/Local"),"ms-playwright"):path.join(process.env.XDG_CACHE_HOME??path.join(userInfo().homedir,".cache"),"ms-playwright"))
  const shells=(await readdir(cache,{withFileTypes:true})).filter(e=>e.isDirectory()&&/^chromium_headless_shell-\d+$/.test(e.name)).sort((a,b)=>Number(b.name.split("-").at(-1))-Number(a.name.split("-").at(-1)))
  const suffix=process.platform==="darwin"?"chrome-mac/headless_shell":process.platform==="win32"?"chrome-win/headless_shell.exe":"chrome-linux/headless_shell"
  for(const shell of shells){const executable=path.join(cache,shell.name,suffix);try{await access(executable,constants.X_OK);return executable}catch{}}
  throw Error("No installed headless shell. This harness installs nothing.")
}
const files=new Map(),traffic=[],results=[]
let sent=false,started=false,failReads=scenario==="read-failure",holdNextSnapshot=false,releaseHeld,held=false
for(const name of ["current"]){
  const contents=(await readFile(fixture,"utf8")).replaceAll('"../../src/',JSON.stringify(path.join(root,"src/")).slice(0,-1))
  const bundle=await build({stdin:{contents,resolveDir:root,loader:"tsx"},bundle:true,write:false,outfile:name+".js",format:"esm",target:"es2022",platform:"browser",jsx:"automatic",nodePaths:[path.join(root,"node_modules"),path.join(root,"../node_modules")],define:{"process.env.NODE_ENV":'"development"',READ_MACHINE:JSON.stringify(machine)}})
  for(const file of bundle.outputFiles)files.set("/"+path.basename(file.path),file.contents)
}
const server=createServer(async(req,res)=>{
  try{
    const url=new URL(req.url,"http://fixture")
    if(url.pathname==="/finish"){await fetch(cloud+"/finish",{method:"POST"});res.writeHead(204);res.end();return}
    if(url.pathname==="/control"){
      if(url.searchParams.has("hold"))holdNextSnapshot=true
      if(url.searchParams.has("release")){releaseHeld?.();releaseHeld=undefined}
      if(url.searchParams.has("recover"))failReads=false
      if(url.searchParams.has("fail"))failReads=true
      res.setHeader("Content-Type","application/json");res.end(JSON.stringify({held}));return
    }
    if(url.pathname==="/relay"){
      const chunks=[];for await(const chunk of req)chunks.push(chunk)
      const raw=Buffer.concat(chunks).toString(),envelope=JSON.parse(raw)
      const record={word:envelope.type,mode:envelope.mode,acceptedAt:performance.now()};traffic.push(record)
      if(sent&&failReads&&envelope.type==="project-assistants.view"){
        record.status=503;record.returnedAt=performance.now();res.writeHead(503,{"Content-Type":"application/json"});res.end(JSON.stringify({body:{contract_version:"project-assistants.v1",error:{code:"pa_offline",field:"fixture_read",retry_after_seconds:0}}}));return
      }
      const answer=await fetch(cloud,{method:"POST",body:raw});let payload=await answer.text()
      // Capture the first post-send read against the admission fence, then
      // release the provider so later reads see the running fence.
      if(sent&&!started&&envelope.type==="project-assistants.view"&&["events","snapshot"].includes(envelope.mode)){
        started=true
        const transition=await fetch(cloud+"/start",{method:"POST"})
        assert.equal(transition.status,204,"the local provider must reach the new running binding")
        record.bindingTransition=true
      }
      if(envelope.type==="project-assistants.send"){
        sent=true
        if(scenario==="unknown"||scenario==="inspect")payload=JSON.stringify({body:{contract_version:"project-assistants.v1",error:{code:"pa_outcome_unknown",field:"fixture_write",retry_after_seconds:0}}})
        if(scenario==="malformed")payload=JSON.stringify({body:{contract_version:"project-assistants.v1"}})
      }
      if(envelope.type==="project-assistants.view"&&envelope.mode==="snapshot"&&holdNextSnapshot){
        holdNextSnapshot=false;held=true
        await new Promise(resolve=>releaseHeld=resolve);held=false
      }
      if(scenario==="late-message"&&envelope.mode==="events"){
        const value=JSON.parse(payload),body=value.body
        if(body?.items){const previous=body.items.length;body.items=body.items.filter(row=>row.event?.message?.kind!=="assistant");record.delayedAssistantRows=previous-body.items.length;record.terminal=body.items.some(row=>row.event?.turn?.state==="succeeded");payload=JSON.stringify(value)}
      }
      record.status=answer.status;record.returnedAt=performance.now();res.writeHead(answer.status,{"Content-Type":"application/json"});res.end(payload);return
    }
    if(url.pathname.startsWith("/catalog/")){res.setHeader("Content-Type","application/json");res.end(await readFile(path.join(root,"public/catalogs",path.basename(url.pathname))));return}
    if(files.has(url.pathname)){res.setHeader("Content-Type",url.pathname.endsWith(".css")?"text/css":"text/javascript");res.end(files.get(url.pathname));return}
    res.setHeader("Content-Type","text/html");res.end('<!doctype html><html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Synthetic chat observation proof</title><style>html,body,#app{height:100%;margin:0}</style><link rel="stylesheet" href="/current.css"><div id="app"></div><script type="module" src="/current.js"></script></html>')
  }catch(error){res.writeHead(500);res.end(JSON.stringify({error:String(error)}))}
})
await new Promise(resolve=>server.listen(0,"127.0.0.1",resolve))
const origin=`http://127.0.0.1:${server.address().port}`,profile=await mkdtemp(path.join(tmpdir(),"clawdline-chat-observation-")),executable=await testBrowser()
const chrome=spawn(executable,["--headless=new","--remote-debugging-port=0","--use-mock-keychain","--user-data-dir="+profile,"--no-first-run","--no-default-browser-check","--disable-features=OmniboxEverywhere","about:blank"],{stdio:["ignore","ignore","pipe"]})
let ws
try{
  const endpoint=await new Promise((resolve,reject)=>{let output="";const timer=setTimeout(()=>reject(Error("Chrome startup: "+output)),20000);chrome.once("error",reject);chrome.once("exit",(code,signal)=>{clearTimeout(timer);reject(Error("Chrome exited: "+code+" "+signal+" "+output))});chrome.stderr.on("data",chunk=>{output+=chunk;const match=/DevTools listening on (ws:\/\/\S+)/.exec(output);if(match){clearTimeout(timer);resolve(match[1])}})})
  ws=new WebSocket(endpoint);await new Promise((resolve,reject)=>{ws.addEventListener("open",resolve);ws.addEventListener("error",reject)})
  let serial=0;const pending=new Map()
  ws.addEventListener("message",event=>{const message=JSON.parse(String(event.data)),job=pending.get(message.id);if(!job)return;pending.delete(message.id);message.error?job.reject(Error(message.error.message)):job.resolve(message.result)})
  const send=(method,params={},sessionId)=>new Promise((resolve,reject)=>{const id=++serial,timer=setTimeout(()=>{pending.delete(id);reject(Error(method+" timed out"))},15000);pending.set(id,{resolve:v=>{clearTimeout(timer);resolve(v)},reject:e=>{clearTimeout(timer);reject(e)}});ws.send(JSON.stringify({id,method,params,sessionId}))})
  const evaluate=async(sessionId,expression)=>{const reply=await send("Runtime.evaluate",{expression,awaitPromise:true,returnByValue:true},sessionId);if(reply.exceptionDetails)throw Error(reply.exceptionDetails.exception?.description??reply.exceptionDetails.text);return reply.result.value}
  const until=async(sessionId,expression)=>{const deadline=Date.now()+12000;while(Date.now()<deadline){if(await evaluate(sessionId,expression))return;await new Promise(r=>setTimeout(r,50))}await capture(sessionId,"failed-wait");throw Error("UI wait: "+expression+"\n"+await evaluate(sessionId,"document.body.innerText"))}
  const capture=async(sessionId,name)=>{
    const state=await evaluate(sessionId,"({visibility:document.visibilityState,rows:document.querySelectorAll('[data-bot-key]').length,loading:!!document.querySelector('[data-loading-id=\"bots-roster\"]'),phases:window.chatObservation.phases,text:document.body.innerText,html:document.documentElement.outerHTML})")
    const shot=await send("Page.captureScreenshot",{format:"png"},sessionId)
    await writeFile(path.join(artifacts,name+".png"),Buffer.from(shot.data,"base64"));await writeFile(path.join(artifacts,name+".json"),JSON.stringify(state,null,2));return state
  }
  const {targetId}=await send("Target.createTarget",{url:"about:blank"}),{sessionId}=await send("Target.attachToTarget",{targetId,flatten:true})
  const text="Synthetic observation message"
  await send("Page.enable",{},sessionId);await send("Runtime.enable",{},sessionId)
  const mobile=["hidden-zh-mobile","read-failure","cleanup-timeout","quota-card"].includes(scenario),lang=mobile?"zh-Hant":"en"
  await send("Emulation.setDeviceMetricsOverride",{width:mobile?390:1280,height:mobile?844:800,deviceScaleFactor:1,mobile},sessionId)
  await send("Page.navigate",{url:origin+"/?lang="+lang},sessionId)
  await until(sessionId,"!!document.querySelector('[data-bot-key]')")
  await evaluate(sessionId,"document.querySelector('[data-bot-key]').click()")
  await until(sessionId,"!!document.querySelector('.pa-composer textarea:not(:disabled)')")
  if(scenario==="busy"){
    await fetch(origin+"/control?hold=1")
    await evaluate(sessionId,"window.chatObservation.visibility('visible')")
    const deadline=Date.now()+5000
    while(!held&&Date.now()<deadline)await new Promise(resolve=>setTimeout(resolve,20))
    assert.equal(held,true,"the foreground read must be held before sending")
  }
  await evaluate(sessionId,"document.querySelector('.pa-composer textarea').focus()")
  await send("Input.insertText",{text},sessionId)
  await until(sessionId,"!!document.querySelector('.pa-composer .pa-send:not(:disabled)')")
  await evaluate(sessionId,"document.querySelector('.pa-composer .pa-send').click()")
  await until(sessionId,"window.chatObservation.phases.some(p=>p.phase==='returned'&&p.word==='project-assistants.send')")
  await new Promise(r=>setTimeout(r,200))
  const accepted=await capture(sessionId,"accepted-before-result")
  if(scenario.startsWith("hidden-")){
    const acknowledged=traffic.find(p=>p.word==="project-assistants.send")
    const next=traffic.find(p=>p.word==="project-assistants.view"&&p.acceptedAt>acknowledged.returnedAt)
    assert.equal(next?.mode,"events","a saved message must first reconcile with incremental events")
  }
  const uncertain=scenario==="unknown"||scenario==="malformed"||scenario==="inspect"
  assert.ok(accepted.text.includes(text)||uncertain&&await evaluate(sessionId,`document.querySelector('.pa-composer textarea').value===${JSON.stringify(text)}`),"the submitted message disappeared before its canonical row was observed")
  if(uncertain&&scenario!=="inspect"){
    assert.ok(await evaluate(sessionId,"document.querySelector('.pa-composer textarea').disabled"))
    await send("Page.reload",{},sessionId)
    await until(sessionId,"!!document.querySelector('[data-bot-key]')")
    await evaluate(sessionId,"document.querySelector('[data-bot-key]').click()")
    await until(sessionId,"!!document.querySelector('.pa-composer textarea:disabled')")
    await new Promise(resolve=>setTimeout(resolve,3300))
    assert.equal(traffic.filter(p=>p.word==='project-assistants.send').length,1,"unknown sends must never be replayed")
    await capture(sessionId,"unknown-after-reopen");results.push({scenario,sends:1,unknownRetained:true});await fetch(origin+"/finish",{method:"POST"})
  }else{
  if(scenario==="inspect"){
    await fetch(origin+"/control?fail=1")
    await evaluate(sessionId,"document.querySelector('.pa-held-send button').click()")
    await until(sessionId,"!!document.querySelector('.pa-chat-error[data-reconnecting][role=status]')")
    const checked=await capture(sessionId,"confirmed-admission-read-failed")
    assert.ok(checked.text.includes(text),"an explicit confirmed admission must keep its text until the canonical row is observed")
    assert.equal(traffic.filter(p=>p.word==='project-assistants.export').length,1)
    await fetch(origin+"/control?recover=1")
    await evaluate(sessionId,"document.querySelector('.pa-chat-error[data-reconnecting] button').click()")
  }
  if(scenario==="busy"){
    await evaluate(sessionId,"window.chatObservation.visibility('visible')")
    await fetch(origin+"/control?release=1")
  }
  if(scenario==="read-failure"){
    await until(sessionId,"!!document.querySelector('.pa-chat-error[data-reconnecting][role=status]')")
    await send("Page.reload",{},sessionId)
    await until(sessionId,"!!document.querySelector('[data-bot-key]')")
    await evaluate(sessionId,"document.querySelector('[data-bot-key]').click()")
    await until(sessionId,"!!document.querySelector('.pa-chat-error[data-reconnecting][role=status]')")
    const failed=await capture(sessionId,"accepted-read-failed-after-reopen")
    assert.ok(failed.text.includes(text),"accepted text must survive a failed read and reload")
    assert.equal(traffic.filter(p=>p.word==='project-assistants.send').length,1)
    await fetch(origin+"/control?recover=1")
    await evaluate(sessionId,"document.querySelector('.pa-chat-error[data-reconnecting] button').click()")
  }
  await until(sessionId,"window.chatObservation.phases.some(p=>p.phase==='returned'&&p.head?.current_fence.generation>=2)")
  if(scenario==="busy")await evaluate(sessionId,"window.chatObservation.visibility('hidden')")
  if(scenario==="leave"||scenario==="revoke"){
    if(scenario==="leave"){await evaluate(sessionId,"document.querySelector('.pa-chat-back').click()");await until(sessionId,"!document.querySelector('.pa-chat-layout')")}
    else await evaluate(sessionId,"window.chatObservation.revoke()")
    const reads=traffic.filter(p=>p.word==='project-assistants.view').length
    await new Promise(resolve=>setTimeout(resolve,3300))
    assert.equal(traffic.filter(p=>p.word==='project-assistants.view').length,reads,"leaving or revoking must stop owned reads")
    assert.equal(traffic.filter(p=>p.word==='project-assistants.send').length,1)
    await capture(sessionId,"observation-stopped");results.push({scenario,stopped:true,sends:1});await fetch(origin+"/finish",{method:"POST"})
  }else{
  await fetch(origin+"/finish",{method:"POST"})
  if(scenario==="cleanup-timeout"){
    await until(sessionId,"document.body.innerText.includes('Clawdline 在確認這一輪的 Agent 是否已關閉時逾時。')")
    const stopped=await capture(sessionId,"cleanup-timeout")
    assert.equal(stopped.text.includes("execution_effect_unknown"),false,"the technical code is collapsed")
    assert.equal(stopped.text.includes("先前回合的結果無法確認"),false,"one interruption should have one explanation")
    const followup="Synthetic follow-up retained in the composer"
    await evaluate(sessionId,"document.querySelector('.pa-composer textarea').focus()")
    await send("Input.insertText",{text:followup},sessionId)
    await until(sessionId,"!!document.querySelector('.pa-composer .pa-send:not(:disabled)')")
    await evaluate(sessionId,"document.querySelector('.pa-composer .pa-send').click()")
    await until(sessionId,"document.body.innerText.includes('這則訊息保留在輸入框。')")
    assert.equal(await evaluate(sessionId,"document.querySelector('.pa-composer textarea').value"),followup)
    assert.equal(traffic.filter(p=>p.word==="project-assistants.send").length,2)
    await capture(sessionId,"cleanup-refusal-preserves-draft")
    results.push({scenario,draftRetained:true,sends:2})
  }else if(scenario==="quota-card"){
    await until(sessionId,"document.querySelector('.pa-interruption strong')?.innerText==='Agent 可用額度已用完。'")
    const card=".pa-interruption"
    const outcome=await capture(sessionId,"quota-card-mobile")
    assert.match(outcome.text,/已顯示的回覆會保留。/)
    assert.equal(outcome.text.includes("execution_effect_unknown"),false)
    assert.equal(await evaluate(sessionId,`document.querySelector('${card}').getBoundingClientRect().right<=innerWidth`),true)
    await evaluate(sessionId,`document.querySelector('${card} summary').click()`)
    const expanded=await capture(sessionId,"quota-card-details")
    assert.match(expanded.text,/execution_effect_unknown · provider_usage_limit/)
    const beforeRefresh=traffic.length
    await evaluate(sessionId,`document.querySelector('${card} button').click()`)
    await until(sessionId,`!!document.querySelector('${card} button:not(:disabled)')`)
    assert.ok(traffic.slice(beforeRefresh).some(p=>p.word==="project-assistants.view"&&p.mode==="snapshot"&&p.status===200),"refresh fetches a new snapshot")
    assert.equal(traffic.filter(p=>p.word==="project-assistants.send").length,1,"a refresh reads without resending the prompt")
    results.push({scenario,closedDetails:true,refreshDoesNotResend:true})
  }else{
  await until(sessionId,"document.body.innerText.includes('Synthetic observation final answer')")
  const complete=await capture(sessionId,"completed")
  assert.equal(await evaluate(sessionId,`[...document.querySelectorAll('.pa-message')].filter(e=>e.textContent.includes(${JSON.stringify(text)})).length`),1,"the optimistic and canonical message must not duplicate")
  assert.equal(await evaluate(sessionId,"document.querySelectorAll('.pa-sending').length"),0)
  assert.equal(traffic.filter(p=>p.word==='project-assistants.send').length,1)
  if(scenario==="late-message"){
    const terminal=traffic.find(p=>p.terminal&&p.delayedAssistantRows>0)
    assert.ok(terminal,"the terminal event must arrive with its answer omitted by the fault")
    assert.ok(traffic.some(p=>p.mode==='snapshot'&&p.acceptedAt>terminal.returnedAt),"a final snapshot must recover the omitted answer")
  }
  const readCount=traffic.filter(p=>p.word==='project-assistants.view').length
  await new Promise(r=>setTimeout(r,3300))
  assert.equal(traffic.filter(p=>p.word==='project-assistants.view').length,readCount,"hidden periodic reads continued after the owned turn completed")
  results.push({scenario,accepted:true,complete:true,hidden:complete.visibility==="hidden",sends:1,duplicateMessages:0,phases:complete.phases})
  }
  }
  }
  await send("Target.closeTarget",{targetId})
  console.log(JSON.stringify({cases:1,modelRequests:0,results:results.map(({phases,...result})=>result),boundary:"controlled visibility; real Chat, captured source, closed decoder, HTTP Host, SQLite; outer encrypted carrier and provider are local doubles"}))
}finally{
  releaseHeld?.()
  await writeFile(path.join(artifacts,"traffic.json"),JSON.stringify(traffic,null,2));await writeFile(path.join(artifacts,"results.json"),JSON.stringify(results,null,2))
  ws?.close();chrome.kill("SIGTERM");await new Promise(resolve=>{if(chrome.exitCode!==null)return resolve();const timer=setTimeout(()=>{chrome.kill("SIGKILL");resolve()},3000);chrome.once("exit",()=>{clearTimeout(timer);resolve()})});server.closeAllConnections();await new Promise(resolve=>server.close(resolve));await rm(profile,{recursive:true,force:true})
}
