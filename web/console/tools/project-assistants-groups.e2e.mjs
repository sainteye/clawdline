/** Real roster/room components, source clients, decrypted Cloud decoder, HTTP and SQLite. */
import assert from "node:assert/strict"
import {createServer} from "node:http"
import {spawn,execFileSync} from "node:child_process"
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
const grant={accountKey:mode==="cloud"?"synthetic":"local",readerID:"person",machineID:"machine",authorizationRevision:"one",apiLevel:mode==="old"?75:88,paired:true,online:true,writable:mode!=="read"};
const carried=${JSON.stringify(words)};
const relay={machineDescriptor:()=>({machine:{commands:carried}}),_machineRequest:async(_,word,fields)=>await(await fetch("/relay/"+word,{method:"POST",body:JSON.stringify(fields)})).json(),_machineRequestAs:async(id,_,word,fields)=>await(await fetch("/relay/"+word,{method:"POST",body:JSON.stringify(fields),headers:{"Idempotency-Key":id}})).json()};
const source=new ProjectAssistantSourceClient(grant,mode==="cloud"?capturedAssistantRelayFetch("machine",()=>relay,carried):(input,init)=>fetch("/proxy/"+mode+input,init),()=>grant);
const readGroup=source.groupHistory.bind(source);window.fixtureReadAttempts=0;source.groupHistory=async(...args)=>{window.fixtureReadAttempts++;if(window.fixtureReadFailure)throw {error:{code:"pa_offline"}};return readGroup(...args)};
await source.status(new AbortController().signal);const page=await source.list({machine_id:"machine",cursor:"",query:"",page_size:100},new AbortController().signal);
createRoot(document.getElementById("app")).render(<div className="project-assistants pa-shell"><BotRoster sources={[source]} groupsItems={page.items} items={page.items} machines={["machine"]} selected={null} query="" onQuery={()=>{}} machine="" onMachine={()=>{}} onSelect={()=>{}} onClose={()=>{}} w={words(language)} language={language} loading={false} errors={[]} projectNames={new Map([[JSON.stringify(["machine","project"]),"Synthetic Project"],[JSON.stringify(["machine","other"]),"Other Project"]])}/><main className="pa-main"/></div>);
`,resolveDir:root,loader:"tsx"},bundle:true,write:false,outfile:"groups.js",format:"esm",target:"es2022",platform:"browser",jsx:"automatic",plugins:process.env.BOTS_ROOM_BASELINE?[{name:"recorded-room-baseline",setup(plugin){plugin.onLoad({filter:/project-assistants\/(GroupRoom\.tsx|styles\.css)$/},args=>({contents:execFileSync("git",["show",process.env.BOTS_ROOM_BASELINE+":"+path.relative(path.resolve(root,"../.."),args.path)],{cwd:path.resolve(root,"../.."),encoding:"utf8"}),loader:args.path.endsWith(".css")?"css":"tsx",resolveDir:path.dirname(args.path)}))}}]:[],define:{"process.env.NODE_ENV":'"development"'}})
const files=new Map(bundle.outputFiles.map(f=>["/"+path.basename(f.path),f.contents])),traffic=[]
const server=createServer(async(req,res)=>{
  try{
    const url=new URL(req.url,"http://fixture")
    if(url.pathname==="/fixture/hold"||url.pathname==="/fixture/release"){
      const response=await fetch(cloud+url.pathname.replace("/fixture", ""),{method:"POST"});res.writeHead(response.status,{"Content-Type":"application/json"});res.end(await response.text());return
    }
    if(url.pathname.startsWith("/proxy/")||url.pathname.startsWith("/relay/")){
      const chunks=[];for await(const chunk of req)chunks.push(chunk);const raw=Buffer.concat(chunks).toString(),relay=url.pathname.startsWith("/relay/")
      let answer,record={method:req.method,path:url.pathname,relay,body:raw?JSON.parse(raw):null};traffic.push(record)
      if(relay){const word=url.pathname.slice(7),fields=JSON.parse(raw||"{}"),envelope={type:word,session:"__clawdline_machine__",request:req.headers["idempotency-key"]??"read-"+crypto.randomUUID(),...fields};record.word=word
        answer=await fetch(cloud,{method:"POST",body:JSON.stringify(envelope)});const payload=await answer.json();record.status=answer.status;record.response=payload.body??{error:payload.error??{code:"pa_outcome_unknown"}};res.writeHead(answer.status,{"Content-Type":"application/json"});res.end(JSON.stringify(record.response));return
      }
      const mode=url.pathname.split("/")[2],pathname=url.pathname.replace("/proxy/"+mode,"")+url.search;record.path=pathname
      answer=await fetch(upstream+pathname,{method:req.method,headers:{Authorization:"Bearer "+(mode==="read"?"reader":"person"),...(raw?{"Content-Type":"application/json","Idempotency-Key":req.headers["idempotency-key"]??""}:{})},...(raw?{body:raw}:{})})
      record.status=answer.status;const bytes=Buffer.from(await answer.arrayBuffer());record.response=JSON.parse(bytes.toString());res.writeHead(answer.status,{"Content-Type":"application/json"});res.end(bytes);return
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
const chrome=spawn(executable,["--headless=new","--remote-debugging-port=0","--user-data-dir="+profile,"--no-first-run","--no-default-browser-check","--disable-features=OmniboxEverywhere","about:blank"],{stdio:["ignore","ignore","pipe"]})
let ws
const results=[],operations=[]
try{
  const endpoint=await new Promise((resolve,reject)=>{let output="";const timer=setTimeout(()=>reject(Error("Chrome startup: "+output)),20000);chrome.on("error",reject);chrome.on("exit",(code,signal)=>{clearTimeout(timer);reject(Error("Chrome exited: "+code+" "+signal+" "+output))});chrome.stderr.on("data",chunk=>{output+=chunk;const match=/DevTools listening on (ws:\/\/\S+)/.exec(output);if(match){clearTimeout(timer);resolve(match[1])}})})
  ws=new WebSocket(endpoint);await new Promise((resolve,reject)=>{ws.addEventListener("open",resolve);ws.addEventListener("error",reject)})
  let serial=0;const pending=new Map()
  ws.addEventListener("message",event=>{const message=JSON.parse(String(event.data)),job=pending.get(message.id);if(!job)return;pending.delete(message.id);message.error?job.reject(Error(message.error.message)):job.resolve(message.result)})
  const send=(method,params={},sessionId)=>new Promise((resolve,reject)=>{const id=++serial,timer=setTimeout(()=>reject(Error(method+" timed out")),15000);pending.set(id,{resolve:v=>{clearTimeout(timer);resolve(v)},reject:e=>{clearTimeout(timer);reject(e)}});ws.send(JSON.stringify({id,method,params,sessionId}))})
  const open=async(mode,lang,width,height)=>{const {targetId}=await send("Target.createTarget",{url:"about:blank"});const {sessionId}=await send("Target.attachToTarget",{targetId,flatten:true});await send("Page.enable",{},sessionId);await send("Runtime.enable",{},sessionId);await send("Page.bringToFront",{},sessionId);await send("Emulation.setDeviceMetricsOverride",{width,height,deviceScaleFactor:1,mobile:width<600},sessionId);await send("Emulation.setTouchEmulationEnabled",{enabled:width<600},sessionId);await send("Page.navigate",{url:origin+"/?mode="+mode+"&lang="+lang},sessionId);return{targetId,sessionId}}
  const evaluate=async(sessionId,expression)=>{const reply=await send("Runtime.evaluate",{expression,awaitPromise:true,returnByValue:true},sessionId);if(reply.exceptionDetails)throw Error(reply.exceptionDetails.text+": "+reply.exceptionDetails.exception?.description);return reply.result.value}
  const until=async(sessionId,expression)=>{const deadline=Date.now()+15000;while(Date.now()<deadline){if(await evaluate(sessionId,expression))return;await new Promise(r=>setTimeout(r,50))}throw Error("UI wait: "+expression+"\n"+await evaluate(sessionId,"document.body.innerText"))}
  const hit=async(sessionId,selector,touch=false)=>{
    const point=await evaluate(sessionId,`(()=>{const b=document.querySelector(${JSON.stringify(selector)});if(!b||b.disabled)throw Error('Missing enabled '+${JSON.stringify(selector)});b.scrollIntoView({block:'nearest'});const r=b.getBoundingClientRect();return {x:r.x+r.width/2,y:r.y+r.height/2}})()`)
    operations.push({action:touch?"touch":"click",selector,at:Date.now(),point})
    if(touch){await send("Input.dispatchTouchEvent",{type:"touchStart",touchPoints:[point]},sessionId);await send("Input.dispatchTouchEvent",{type:"touchEnd",touchPoints:[]},sessionId)}
    else{await send("Input.dispatchMouseEvent",{type:"mousePressed",...point,button:"left",clickCount:1},sessionId);await send("Input.dispatchMouseEvent",{type:"mouseReleased",...point,button:"left",clickCount:1},sessionId)}
  }
  const click=async(sessionId,text)=>{
    const selector=await evaluate(sessionId,`(()=>{const buttons=[...document.querySelectorAll('button')];const index=buttons.findIndex(b=>(b.getAttribute('aria-label')===${JSON.stringify(text)}||b.textContent.trim()===${JSON.stringify(text)})&&!b.disabled&&b.getBoundingClientRect().height>0);if(index<0)throw Error('Missing button '+${JSON.stringify(text)});buttons[index].setAttribute('data-fixture-hit','true');return 'button[data-fixture-hit=true]'})()`)
    try{await hit(sessionId,selector)}finally{await evaluate(sessionId,"document.querySelector('[data-fixture-hit]')?.removeAttribute('data-fixture-hit')")}
  }
  const openGroupCreator=async(sessionId,zh)=>{
    const add=zh?"新增":"Add",create=zh?"新增群組":"New group"
    await until(sessionId,`!!document.querySelector('.pa-toolbar button[aria-label="${add}"]')`)
    await click(sessionId,add)
    const choices=await evaluate(sessionId,`[...document.querySelectorAll('.pa-toolbar [role="menuitem"]')].map(item=>item.textContent.trim())`)
    assert.deepEqual(choices,[create],"the group-only roster exposed another creation action")
    await click(sessionId,create)
  }
  const key=async(sessionId,key,code=key)=>{operations.push({action:"key",key,at:Date.now()});await send("Input.dispatchKeyEvent",{type:"keyDown",key,code},sessionId);await send("Input.dispatchKeyEvent",{type:"keyUp",key,code},sessionId)}
  const fill=async(sessionId,selector,value)=>evaluate(sessionId,`(()=>{const e=document.querySelector(${JSON.stringify(selector)});if(!e)throw Error('Missing input');e.focus();Object.getOwnPropertyDescriptor(e.tagName==='TEXTAREA'?HTMLTextAreaElement.prototype:HTMLInputElement.prototype,'value').set.call(e,${JSON.stringify(value)});e.setSelectionRange?.(e.value.length,e.value.length);e.dispatchEvent(new Event('input',{bubbles:true}));e.dispatchEvent(new Event('select',{bubbles:true}));})()`)
  if(process.env.BOTS_GROUP_BACKGROUND==="1"){
    for(const [mode,lang,width,height,outcome] of [["local","en",1280,800,"complete"],["cloud","zh-Hant",390,844,"complete"],["cloud","en",1280,800,"refusal"]]){
      const start=traffic.length,{targetId,sessionId}=await open(mode,lang,width,height),zh=lang==="zh-Hant"
      await openGroupCreator(sessionId,zh);await until(sessionId,"!!document.querySelector('dialog[open] input:not([type=checkbox])')")
      await fill(sessionId,"dialog[open] input:not([type=checkbox])","Background observation "+mode)
      await evaluate(sessionId,"for(const e of document.querySelectorAll('dialog[open] input[type=checkbox]'))if(['Alice','Bob'].includes(e.parentElement.textContent.trim()))e.click()")
      await click(sessionId,zh?"儲存":"Save");await until(sessionId,"!!document.querySelector('[role=dialog] textarea:not(:disabled)')")
      const created=traffic.slice(start).find(row=>row.response?.group?.name==="Background observation "+mode).response.group
      // Remote controls can send to a hidden document. This fixture makes
      // that visibility boundary deterministic without another real viewer.
      await evaluate(sessionId,"Object.defineProperty(document,'visibilityState',{configurable:true,get:()=>window.fixtureVisibility??'visible'});window.fixtureVisibility='hidden';document.dispatchEvent(new Event('visibilitychange'))")
      await fetch(origin+"/fixture/hold",{method:"POST"})
      await fill(sessionId,"[role=dialog] textarea","One accepted background room question");await click(sessionId,zh?"傳送":"Send")
      await until(sessionId,"document.querySelectorAll('[data-room-seq]').length===1&&document.querySelectorAll('[data-working-member]').length===1")
      if(outcome==="refusal")await evaluate(sessionId,"window.fixtureReadFailure=true")
      await fetch(origin+"/fixture/release",{method:"POST"})
      let stored;const deadline=Date.now()+5000
      do {stored=await(await fetch(upstream+"/v1/project-assistants/rooms/groups/"+created.key.bot_id+"?machine_id=machine&before_seq=0&page_size=100",{headers:{Authorization:"Bearer person"}})).json();if(stored.lines?.length===3&&!stored.room.running)break;assert.ok(Date.now()<deadline);await new Promise(resolve=>setTimeout(resolve,20))}while(true)
      const proof={mode,lang,width,outcome,sourceLines:stored.lines,sourceRoom:stored.room,visibility:await evaluate(sessionId,"document.visibilityState"),prompts:1,externalModels:0}
      try{
        if(outcome==="refusal"){
          await until(sessionId,"document.querySelector('.pa-room-layout [role=alert]')?.textContent.includes('pa_offline')")
          const attempts=await evaluate(sessionId,"window.fixtureReadAttempts")
          await new Promise(resolve=>setTimeout(resolve,3500));assert.equal(await evaluate(sessionId,"window.fixtureReadAttempts"),attempts,'typed hidden read refusal kept polling')
          assert.equal(await evaluate(sessionId,"document.querySelectorAll('[data-room-seq]').length"),1,'read failure lost the accepted person message')
          proof.typedFailure="pa_offline";proof.stoppedAfterFailure=true
          await evaluate(sessionId,"window.fixtureReadFailure=false;window.fixtureVisibility='visible';document.dispatchEvent(new Event('visibilitychange'))")
        }
        await until(sessionId,"document.querySelectorAll('[data-room-seq]').length===3&&!document.querySelector('[data-working-member]')")
        if(outcome==="refusal")await evaluate(sessionId,"window.fixtureVisibility='hidden';document.dispatchEvent(new Event('visibilitychange'))")
      }
      finally{proof.uiText=await evaluate(sessionId,"document.querySelector('.pa-room-layout').innerText");proof.groupReads=traffic.slice(start).filter(row=>row.word==="project-assistants.group-history"||row.method==="GET"&&row.path.includes('/rooms/groups/')).length;await writeFile(path.join(artifacts,`background-${mode}.json`),JSON.stringify(proof,null,2));const shot=await send("Page.captureScreenshot",{format:"png"},sessionId);await writeFile(path.join(artifacts,`background-${mode}.png`),Buffer.from(shot.data,"base64"))}
      assert.equal(traffic.slice(start).filter(row=>row.word==="project-assistants.group-send"||row.method==="POST"&&row.path.endsWith('/messages')).length,1)
      const reads=proof.groupReads;await new Promise(resolve=>setTimeout(resolve,3500))
      assert.equal(traffic.slice(start).filter(row=>row.word==="project-assistants.group-history"||row.method==="GET"&&row.path.includes('/rooms/groups/')).length,reads,'idle hidden room kept polling after its accepted turn completed')
      results.push({...proof,observedWithoutForeground:outcome==="complete",foregroundRecovered:outcome==="refusal",noResend:true})
      const cleanup={command:{key:created.key,operation:"delete",idempotency_key:crypto.randomUUID(),content_hash:""},expected_version:created.version}
      const deleted=await fetch(upstream+"/v1/project-assistants/rooms/groups/"+created.key.bot_id,{method:"DELETE",headers:{Authorization:"Bearer person","Content-Type":"application/json","Idempotency-Key":cleanup.command.idempotency_key},body:JSON.stringify(cleanup)})
      assert.equal(deleted.status,200);await send("Target.closeTarget",{targetId})
    }
  }else{
  for(const [mode,lang,width,height]of [["local","en",1280,800],["local","zh-Hant",390,844],["cloud","en",1280,800],["cloud","zh-Hant",390,844]]){
    const {targetId,sessionId}=await open(mode,lang,width,height),zh=lang==="zh-Hant",name=`Review ${mode} ${lang}`
    const copy=zh?{save:"儲存",edit:"編輯群組",send:"傳送",stop:"停止群組",remove:"刪除群組",confirm:"確認刪除群組"}:{save:"Save",edit:"Edit group",send:"Send",stop:"Stop group",remove:"Delete group",confirm:"Confirm group deletion"}
    await openGroupCreator(sessionId,zh)
    await until(sessionId,"!!document.querySelector('dialog[open] input:not([type=checkbox])')");await fill(sessionId,"dialog[open] input:not([type=checkbox])",name)
    assert.equal(await evaluate(sessionId,"[...document.querySelectorAll('dialog[open] input[type=checkbox]')].some(e=>e.parentElement.textContent.includes('Stranger'))"),false,"another Project was offered as a room member")
    await evaluate(sessionId,"for(const e of document.querySelectorAll('dialog[open] input[type=checkbox]'))if(['Alice','Bob'].includes(e.parentElement.textContent.trim()))e.click()")
    await click(sessionId,copy.save);await until(sessionId,`!!document.querySelector('.pa-chat-header h2')&&document.querySelector('.pa-chat-header h2').textContent===${JSON.stringify(name)}`)
    const bounds=await evaluate(sessionId,`(()=>{
      const room=document.querySelector('.pa-room-layout'),shell=room.parentElement,roster=shell.querySelector('.pa-roster'),search=roster.querySelector('input[type=search]')
      const rect=node=>node.getBoundingClientRect().toJSON(),s=rect(search)
      return {room:rect(room),shell:rect(shell),roster:rect(roster),searchHit:search.contains(document.elementFromPoint(s.left+s.width/2,s.top+s.height/2))}
    })()`)
    await writeFile(path.join(artifacts,`room-bounds-${mode}-${lang}.json`),JSON.stringify(bounds,null,2))
    const positionShot=await send("Page.captureScreenshot",{format:"png"},sessionId);await writeFile(path.join(artifacts,`room-position-${mode}-${lang}.png`),Buffer.from(positionShot.data,"base64"))
    assert.ok(bounds.room.top>=0&&bounds.room.bottom<=height+2&&Math.abs(bounds.room.right-bounds.shell.right)<=1,JSON.stringify(bounds))
    assert.ok(Math.abs(bounds.room.left-(width>700?bounds.roster.right:bounds.shell.left))<=1,JSON.stringify(bounds))
    assert.equal(bounds.searchHit,width>700,"only the desktop room leaves the roster reachable")
    await fetch(origin+"/fixture/hold",{method:"POST"})
    await fill(sessionId,"[role=dialog] textarea","Synthetic UI room message");await click(sessionId,copy.send)
    await until(sessionId,"[...document.querySelectorAll('[data-working-member]')].some(row=>JSON.parse(row.dataset.workingMember)[1]==='alice'&&row.dataset.workingState==='running')")
    const workers=await evaluate(sessionId,"[...document.querySelectorAll('[data-working-member]')].map(row=>JSON.parse(row.dataset.workingMember)[1])")
    assert.deepEqual(workers,["alice"],"room activity guessed that every member was working")
    const workingShot=await send("Page.captureScreenshot",{format:"png"},sessionId);await writeFile(path.join(artifacts,`group-working-${mode}-${lang}.png`),Buffer.from(workingShot.data,"base64"))
    await fetch(origin+"/fixture/release",{method:"POST"})
    await until(sessionId,"document.querySelectorAll('[data-room-seq]').length===3")
    const authors=await evaluate(sessionId,"[...document.querySelectorAll('[data-room-seq] strong')].map(e=>e.textContent)");assert.ok(authors.includes("Alice")&&authors.includes("Bob"),authors)
    assert.equal(await evaluate(sessionId,"document.documentElement.scrollWidth>innerWidth"),false,"room widened the page")
    const shot=await send("Page.captureScreenshot",{format:"png"},sessionId);await writeFile(path.join(artifacts,`group-${mode}-${lang}.png`),Buffer.from(shot.data,"base64"))
    // Keyboard and real touch pick the same full member name. A pick does
    // not send; a subsequent Enter sends one command to the source.
    const sends=()=>traffic.filter(row=>row.method!=="GET"&&(row.word==="project-assistants.group-send"||row.path.endsWith("/messages")))
    const prior=sends().length
    await fill(sessionId,"[role=dialog] textarea","@")
    await until(sessionId,"document.querySelectorAll('.pa-room-mentions [role=option]').length===2")
    if(width<600)await hit(sessionId,".pa-room-mentions [role=option]:nth-child(2)",true)
    else{await key(sessionId,"ArrowDown");await key(sessionId,"Tab")}
    await until(sessionId,"document.querySelector('[role=dialog] textarea').value==='@Bob '")
    assert.equal(sends().length,prior,"picking a mention sent a message")
    await key(sessionId,"Enter")
    await until(sessionId,`document.querySelectorAll('[data-room-seq]').length===5`)
    assert.equal(sends().length,prior+1)
    const bobLine=await evaluate(sessionId,"[...document.querySelectorAll('[data-room-seq]')].filter(row=>row.querySelector('.pa-room-author strong')?.textContent==='Bob').at(-1).dataset.roomSeq")
    await hit(sessionId,`[data-room-seq="${bobLine}"] .pa-room-reply`)
    await until(sessionId,"!!document.querySelector('.pa-room-draft-quote')")
    await fill(sessionId,"[role=dialog] textarea","Synthetic exact quote reply")
    await click(sessionId,copy.send)
    await until(sessionId,`!!document.querySelector('[data-quote-seq="${bobLine}"]')`)
    const quoteSend=sends().at(-1)
    assert.equal(quoteSend.body.quote.seq,Number(bobLine))
    assert.equal(quoteSend.body.text,"Synthetic exact quote reply")
    const shown=await evaluate(sessionId,`document.querySelector('[data-quote-seq="${bobLine}"]').innerText`)
    assert.ok(shown.includes("Bob")&&shown.includes("Synthetic main result"),shown)
    await fill(sessionId,"[role=dialog] textarea","Synthetic draft retained in member details")
    await click(sessionId,zh?"成員 (2)":"Members (2)")
    await key(sessionId,"Escape")
    await until(sessionId,"document.querySelector('.pa-room-members').hidden")
    assert.equal(await evaluate(sessionId,"document.querySelector('[role=dialog] textarea').value"),"Synthetic draft retained in member details")
    await fill(sessionId,"[role=dialog] textarea","")
    await click(sessionId,zh?"更多動作":"More actions");await click(sessionId,copy.edit);await until(sessionId,"!!document.querySelector('dialog[open] input:not([type=checkbox])')");await fill(sessionId,"dialog[open] input:not([type=checkbox])",name+" changed")
    await evaluate(sessionId,"[...document.querySelectorAll('dialog[open] input[type=checkbox]')].find(e=>e.parentElement.textContent.trim()==='Bob').click()")
    await click(sessionId,copy.save);await until(sessionId,`document.querySelector('.pa-chat-header h2')?.textContent===${JSON.stringify(name+" changed")}`)
    await click(sessionId,zh?"更多動作":"More actions");await click(sessionId,copy.stop);await until(sessionId,"!!document.querySelector('[role=dialog] textarea:not(:disabled)')")
    await click(sessionId,zh?"更多動作":"More actions");await click(sessionId,copy.remove);await click(sessionId,copy.confirm);await until(sessionId,"!document.querySelector('[role=dialog]')")
    results.push({mode,lang,width,create:true,send:true,authors,edit:true,stop:true,delete:true,roomBounds:bounds,actualWorkers:workers,keyboardMention:width>=600,touchMention:width<600,quoteSeq:Number(bobLine),quoteAuthor:"Bob",memberPanelDraftRetained:true,modelRequests:0});await send("Target.closeTarget",{targetId})
  }
  // A real saved room can be read from a read-only source; no action leaves it.
  const request={command:{key:{machine_id:"machine",bot_id:"read-room"},operation:"create",idempotency_key:"seed-reader-room",content_hash:""},settings:{target:{machine_id:"machine",project_id:"project"},name:"Saved reader room",members:[{machine_id:"machine",bot_id:"alice"}]}}
  const seeded=await fetch(upstream+"/v1/project-assistants/rooms/groups",{method:"POST",headers:{Authorization:"Bearer person","Content-Type":"application/json","Idempotency-Key":"seed-reader-room"},body:JSON.stringify(request)});assert.equal(seeded.status,200,await seeded.text())
  for(const mode of ["read","old"]){const start=traffic.length,{targetId,sessionId}=await open(mode,"zh-Hant",390,844);await until(sessionId,"!!document.body?.innerText.includes('Alice')")
    if(mode==="read"){await until(sessionId,"[...document.querySelectorAll('button')].some(b=>b.textContent.includes('Saved reader room'))");await evaluate(sessionId,"[...document.querySelectorAll('button')].find(b=>b.textContent.includes('Saved reader room')).click()");await until(sessionId,"!!document.querySelector('[role=dialog] textarea')");assert.equal(await evaluate(sessionId,"document.querySelector('[role=dialog] textarea').disabled"),true);assert.equal(await evaluate(sessionId,"[...document.querySelectorAll('[role=dialog] button')].filter(b=>['傳送','編輯群組','停止群組','刪除群組'].includes(b.textContent.trim())).some(b=>!b.disabled)"),false)}else{assert.equal(await evaluate(sessionId,"document.body.innerText.includes('新增群組')"),false)}
    assert.equal(traffic.slice(start).filter(r=>r.method!=="GET").length,0);results.push({mode,lang:"zh-Hant",readOnly:mode==="read",actions:0});await send("Target.closeTarget",{targetId})
  }
  }
  console.log("GROUP_UI_RECEIPT "+JSON.stringify({browser:executable,results,providerRequests:0,cloudBoundary:"decrypted vocabulary -> actual HTTP -> SQLite"}))
}finally{
  await writeFile(path.join(artifacts,"group-ui-traffic.json"),JSON.stringify(traffic,null,2));await writeFile(path.join(artifacts,"group-ui-results.json"),JSON.stringify(results,null,2))
  await writeFile(path.join(artifacts,"group-ui-operations.json"),JSON.stringify(operations,null,2));ws?.close();chrome.kill("SIGTERM");await new Promise(resolve=>{if(chrome.exitCode!==null)return resolve();const timer=setTimeout(()=>{chrome.kill("SIGKILL");resolve()},3000);chrome.once("exit",()=>{clearTimeout(timer);resolve()})});server.closeAllConnections();await new Promise(resolve=>server.close(resolve));await rm(profile,{recursive:true,force:true})
}
