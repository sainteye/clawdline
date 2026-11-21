/** Synthetic ACP configuration and layered memory cross production UI, SourceClient, Host and SQLite. */
import assert from "node:assert/strict"
import {createServer} from "node:http"
import {mkdir,mkdtemp,readFile,writeFile,rm,access} from "node:fs/promises"
import {spawn} from "node:child_process"
import {constants} from "node:fs"
import {fileURLToPath} from "node:url"
import path from "node:path"
import {build} from "esbuild"

const {BOTS_ORIGIN:upstream,BOTS_TOKEN:token,BOTS_READ_TOKEN:readToken,BOTS_MACHINE:machine,BOTS_BOT:bot,BOTS_ARTIFACTS:artifacts}=process.env
assert.ok(upstream&&token&&readToken&&machine&&bot&&artifacts)
await mkdir(artifacts,{recursive:true})
const root=fileURLToPath(new URL("..",import.meta.url)),requests=[],operations=[]

const bundle=await build({stdin:{contents:`
import React,{useState} from "react";import {createRoot} from "react-dom/client";
import {MemoryPanel,projectMemoryPageSize} from "./src/project-assistants/MemoryPanel";import {BotSettings} from "./src/project-assistants/BotSettings";
import {FallbackSourceScope} from "./src/project-assistants/FallbackSettings";
import {ProjectAssistantSourceClient} from "./src/cloud/project-assistants";import {command} from "./src/project-assistants/client";
import {words} from "./src/project-assistants/words";import {bootCatalog} from "./src/catalog";import "./src/project-assistants/styles.css";
const q=new URL(location.href).searchParams,language=q.get("lang"),read=q.has("read");localStorage.setItem("ui_language",language);await bootCatalog(tag=>"/catalog/"+tag+".json");
let live={accountKey:"synthetic-account",readerID:read?"read-only":"synthetic-reader",machineID:${JSON.stringify(machine)},authorizationRevision:"synthetic-grant",apiLevel:98,paired:true,online:true,writable:!read};
const source=new ProjectAssistantSourceClient(live,(input,init)=>fetch(input+(read?(input.includes("?")?"&":"?")+"fixture_read=1":""),init),()=>live);
const key={machine_id:live.machineID,bot_id:${JSON.stringify(bot)}},got=await source.get(key,new AbortController().signal),w=words(language);
window.fixtureRead=()=>source.memoryView(key,got.conversation.id,new AbortController().signal).then(()=>"unexpected publication",e=>e.error?.code);
window.fixtureDowngrade=async()=>{live={...live,apiLevel:78};try{await source.edit({command:command(key,"edit"),settings:{name:got.bot.name,appearance:got.bot.appearance,instructions:got.bot.instructions.text,expected_version:got.bot.version,runtime_settings:{model:"",options:{}}}},new AbortController().signal);return "unexpected write"}catch(e){return e.error?.code}finally{live={...live,apiLevel:98}}};
window.fixtureRevoke=()=>{live={...live,authorizationRevision:"revoked-grant"}};
function Fixture(){const [bot,setBot]=useState(got.bot),[memory,setMemory]=useState(null),[screen,setScreen]=useState("home"),[message,setMessage]=useState("");
const botPage={page:0,page_size:projectMemoryPageSize}
const refresh=async()=>setMemory(await source.memoryView(key,got.conversation.id,new AbortController().signal,undefined,botPage));
// The same requests Chat.tsx sends: a note keeps its id and expected revision; forgetting names sorted sources.
const save=async input=>{const note=typeof input==="string"?null:input,text=note?note.text:input,revision=note?.expected_revision??memory.view.memory.revision;await source.saveMemory({command:command(key,"memory_save"),expected_revision:revision,entry:{id:note?.entry_id??crypto.randomUUID(),key,project:memory.view.bot.project,text,revision,sources:[],forgotten:false,...(note?{kind:"note",title:note.title,summary:note.summary}:{})}},new AbortController().signal);await refresh()};
const forget=async ids=>{await source.forget({command:command(key,"forget"),project:memory.view.bot.project,expected_revision:memory.view.memory.revision,source_ids:[...new Set(ids)].sort()},new AbortController().signal);await refresh()};
return <FallbackSourceScope source={source}><main className="project-assistants" style={{maxWidth:900,margin:"0 auto"}}>
<nav><button onClick={()=>{setScreen("memory");void refresh()}}>{w.memory}</button><button onClick={()=>setScreen("settings")}>{w.settings}</button><button disabled={read} onClick={async()=>{const fresh=await source.get(key,new AbortController().signal);const c=fresh.conversation;await source.send({command:command(key,"send"),fence:{key,project:fresh.bot.project,conversation_id:c.id,binding_id:c.binding_id,generation:c.generation},text:"Synthetic configured input",quote_message_id:""},new AbortController().signal);setMessage("Scripted input accepted")}}>Send scripted input</button></nav>
{screen==="memory"&&<MemoryPanel value={memory} enabled={!read} w={w} onClose={()=>setScreen("home")} onRefresh={refresh} onSave={save} onSaveNote={source.canTopicMemory()?save:undefined} onForget={forget} readDetail={(detail,page)=>source.memoryView(key,got.conversation.id,new AbortController().signal,detail,page??botPage)}
readProjectPage={async(epoch,page,signal)=>(await source.memoryView(key,got.conversation.id,signal,{source_epoch:epoch,layer:"project",entry_id:"",revision:""},page)).view.layers[0]}/>}
{screen==="settings"&&<BotSettings bot={bot} enabled={!read} teamAvailable={true} w={w} onClose={()=>setScreen("home")} onSave={async settings=>{const saved=await source.edit({command:command(key,"edit"),settings},new AbortController().signal);setBot(saved.bot);setScreen("home");setMessage("Settings saved")}}/>}
<p role="status">{message}</p></main></FallbackSourceScope>}
createRoot(document.getElementById("app")).render(<Fixture/>);
`,resolveDir:root,loader:"tsx"},bundle:true,write:false,outfile:"memory-settings.js",format:"esm",target:"es2022",platform:"browser",jsx:"automatic",define:{"process.env.NODE_ENV":'"development"'}})
const files=new Map(bundle.outputFiles.map(f=>["/"+path.basename(f.path),f.contents]))
let tab,holdNext=false,releaseHeld,held
const server=createServer(async(req,res)=>{try{
  const url=new URL(req.url,"http://fixture")
  if(url.pathname.startsWith("/v1/")){
    const read=url.searchParams.has("fixture_read");url.searchParams.delete("fixture_read")
    const chunks=[];for await(const chunk of req)chunks.push(chunk)
    const headers={Authorization:"Bearer "+(read?readToken:token)};for(const key of ["content-type","idempotency-key"])if(req.headers[key])headers[key]=req.headers[key]
    const answer=await fetch(upstream+url.pathname+url.search,{method:req.method,headers,...(chunks.length?{body:Buffer.concat(chunks)}:{})}),body=Buffer.from(await answer.arrayBuffer())
    requests.push({method:req.method,path:url.pathname,page:url.searchParams.get("page"),status:answer.status,...(chunks.length?{body:JSON.parse(Buffer.concat(chunks))}:{})})
    if(holdNext&&url.pathname.endsWith("/memory-view")){holdNext=false;held=true;await new Promise(resolve=>{releaseHeld=resolve})}
    res.writeHead(answer.status,{"Content-Type":"application/json"});res.end(body);return
  }
  if(url.pathname.startsWith("/catalog/")){res.setHeader("Content-Type","application/json");res.end(await readFile(path.join(root,"public/catalogs",path.basename(url.pathname))));return}
  if(files.has(url.pathname)){res.setHeader("Content-Type",url.pathname.endsWith(".css")?"text/css":"text/javascript");res.end(files.get(url.pathname));return}
  res.setHeader("Content-Type","text/html");res.end('<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="stylesheet" href="/memory-settings.css"><div id="app"></div><script type="module" src="/memory-settings.js"></script>')
}catch(error){res.writeHead(500);res.end(String(error))}})
await new Promise(resolve=>server.listen(0,"127.0.0.1",resolve));const origin=`http://127.0.0.1:${server.address().port}`
const executable=process.env.CHROME;assert.ok(executable,"CHROME must name an existing test browser; this harness installs nothing");await access(executable,constants.X_OK)
const profile=await mkdtemp(path.join(artifacts,"browser-profile-")),chrome=spawn(executable,["--headless=new","--remote-debugging-port=0","--user-data-dir="+profile,"--no-first-run","--no-default-browser-check","--disable-features=OmniboxEverywhere","about:blank"],{stdio:["ignore","ignore","pipe"]})
let ws,send,session
async function connect(){
  const endpoint=await new Promise((resolve,reject)=>{let output="";const timer=setTimeout(()=>reject(Error("Test browser startup timeout: "+output)),20000);chrome.on("error",reject);chrome.on("exit",()=>{clearTimeout(timer);reject(Error("Test browser exited: "+output))});chrome.stderr.on("data",chunk=>{output+=chunk;const match=/DevTools listening on (ws:\/\/\S+)/.exec(output);if(match){clearTimeout(timer);resolve(match[1])}})})
  ws=new WebSocket(endpoint);await new Promise((resolve,reject)=>{ws.addEventListener("open",resolve);ws.addEventListener("error",reject)})
  let serial=0;const pending=new Map()
  ws.addEventListener("message",event=>{const message=JSON.parse(String(event.data));if(!message.id)return;const job=pending.get(message.id);pending.delete(message.id);if(message.error)job?.reject(Error(message.error.message));else job?.resolve(message.result)})
  send=(method,params={},sessionId)=>new Promise((resolve,reject)=>{const id=++serial,timer=setTimeout(()=>reject(Error(method+" timed out")),15000);pending.set(id,{resolve:value=>{clearTimeout(timer);resolve(value)},reject:error=>{clearTimeout(timer);reject(error)}});ws.send(JSON.stringify({id,method,params,sessionId}))})
}
async function evaluate(expression){const result=await send("Runtime.evaluate",{expression,awaitPromise:true,returnByValue:true},session);if(result.exceptionDetails)throw Error(result.exceptionDetails.exception?.description??result.exceptionDetails.text);return result.result.value}
async function browser(op,...args){
  let result
  if(op==="open"){const {targetId}=await send("Target.createTarget",{url:"about:blank"});tab=targetId;session=(await send("Target.attachToTarget",{targetId,flatten:true})).sessionId;await send("Page.enable",{},session);await send("Page.navigate",{url:args[0]},session);result={tab:targetId}}
  else if(op==="viewport")result=await send("Emulation.setDeviceMetricsOverride",{width:args[0]==="mobile"?390:1280,height:844,deviceScaleFactor:1,mobile:args[0]==="mobile"},session)
  else if(op==="read")result={text:await evaluate('document.body?.innerText??""')}
  else if(op==="click")result=await tap(buttonByLabel(args[0]))
  else if(op==="screenshot"){const {data}=await send("Page.captureScreenshot",{format:"jpeg"},session);await writeFile(args[1],Buffer.from(data,"base64"));result={captured:true}}
  else if(op==="close"){result=await send("Target.closeTarget",{targetId:tab});assert.equal(result.success,true)}
  else throw Error("Unsupported test operation: "+op)
  operations.push({op,args,result});return result
}
async function waitText(text,present=true){let last;for(let n=0;n<30;n++){last=await browser("read");if(last.text?.includes(text)===present)return last;await new Promise(r=>setTimeout(r,100))}throw Error(`Text ${text} present=${present}: ${JSON.stringify(last)}`)}
// Real Chrome input: mouse, keyboard and text arrive through CDP Input, so React sees trusted pointer, key and input events.
const buttonByLabel=label=>`[...document.querySelectorAll("button")].find(b=>(b.getAttribute("aria-label")??b.textContent)===${JSON.stringify(label)})`
const paSelect=label=>`[...document.querySelectorAll(".pa-select")].find(s=>s.querySelector(":scope>.pa-field-label")?.textContent===${JSON.stringify(label)})?.querySelector(".pa-select-button")`
async function tap(find){
  const at=await evaluate(`(()=>{const el=(${find});if(!el)return null;el.scrollIntoView({block:"center",inline:"nearest"});const b=el.getBoundingClientRect();return {x:b.left+b.width/2,y:b.top+b.height/2,disabled:el.matches(":disabled")}})()`)
  if(!at)throw Error("Missing element: "+find)
  for(const type of ["mouseMoved","mousePressed","mouseReleased"])await send("Input.dispatchMouseEvent",{type,x:at.x,y:at.y,button:"left",buttons:type==="mousePressed"?1:0,clickCount:type==="mouseMoved"?0:1,pointerType:"mouse"},session)
  return {disabled:at.disabled}
}
const keyCodes={Enter:13,Escape:27,ArrowDown:40,ArrowUp:38,Home:36,End:35,Tab:9}
async function key(name,commands){
  const code=keyCodes[name]
  await send("Input.dispatchKeyEvent",{type:"rawKeyDown",key:name,code:name,windowsVirtualKeyCode:code,nativeVirtualKeyCode:code,...(commands?{commands}:{})},session)
  if(name==="Enter")await send("Input.dispatchKeyEvent",{type:"char",key:"Enter",text:"\r",unmodifiedText:"\r",windowsVirtualKeyCode:13},session)
  await send("Input.dispatchKeyEvent",{type:"keyUp",key:name,code:name,windowsVirtualKeyCode:code,nativeVirtualKeyCode:code},session)
  operations.push({op:"key",args:[name]})
}
async function typeInto(find,text,replace=false){
  await tap(find)
  if(replace)await send("Input.dispatchKeyEvent",{type:"rawKeyDown",key:"a",code:"KeyA",windowsVirtualKeyCode:65,modifiers:process.platform==="darwin"?4:2,commands:["selectAll"]},session),await send("Input.dispatchKeyEvent",{type:"keyUp",key:"a",code:"KeyA",windowsVirtualKeyCode:65},session)
  await send("Input.insertText",{text},session)
  operations.push({op:"type",args:[find.length>80?find.slice(0,80)+"…":find,text.length]})
}
async function waitFor(expression,what){for(let n=0;n<60;n++){if(await evaluate(expression))return;await new Promise(r=>setTimeout(r,100))}throw Error("Timed out waiting for "+(what??expression))}
const selectedText=label=>evaluate(`(${paSelect(label)})?.querySelector(".pa-select-value")?.textContent??null`)
// Opens a PaSelect with a real click and chooses an option by mouse, or by arrow keys and Enter.
async function choose(label,option,{keyboard=false}={}){
  await waitFor(`!!(${paSelect(label)})`,"select "+label)
  await tap(paSelect(label))
  await waitFor(`!!document.querySelector('.pa-select-list[role=listbox]')`,"listbox "+label)
  const labels=await evaluate(`[...document.querySelectorAll('.pa-select-list [role=option] .pa-select-text>span:first-child')].map(e=>e.textContent)`)
  const index=typeof option==="number"?option:labels.indexOf(option)
  if(index<0)throw Error(`Option ${option} missing from ${label}: ${JSON.stringify(labels)}`)
  if(keyboard){
    const active=await evaluate(`[...document.querySelectorAll('.pa-select-list [role=option]')].findIndex(e=>e.hasAttribute("data-active"))`)
    for(let n=active;n<index;n++)await key("ArrowDown")
    for(let n=active;n>index;n--)await key("ArrowUp")
    await waitFor(`document.querySelectorAll('.pa-select-list [role=option]')[${index}]?.hasAttribute("data-active")`,"active option")
    await key("Enter")
  }else await tap(`document.querySelectorAll('.pa-select-list [role=option]')[${index}]`)
  await waitFor(`!document.querySelector('.pa-select-list[role=listbox]')`,"listbox closed")
  operations.push({op:"choose",args:[label,labels[index],keyboard?"keyboard":"mouse"]})
}
// Writable personal memory, crossed with real pointer, keyboard and text input: a topic note is created, read back after a
// reload, edited, read back again and forgotten; a short fact is saved and read back. Kept apart from the read-only and
// settings matrix so a failure names the memory write path alone.
const editor=label=>`[...document.querySelectorAll(".pa-memory-note-editor label")].find(l=>l.firstChild?.textContent===${JSON.stringify(label)})?.querySelector("input,textarea")`
const indexButton=name=>`[...document.querySelectorAll("button.pa-memory-index")].find(b=>b.getAttribute("aria-label")===${JSON.stringify(name)})`
async function openMemory(language){
  const zh=language!=="en"
  await browser("open",origin+"/?lang="+language);await browser("viewport",zh?"mobile":"desktop");await waitText(zh?"Bot 設定":"Bot settings")
  await browser("click",zh?"記憶":"Memory");await waitText(zh?"這個 Bot 的記憶":"This Bot’s memory")
}
async function memoryWrites(){
  const title="Synthetic browser note",first="First synthetic body typed in the browser.",second="Edited synthetic body after reload."
  await openMemory("en")
  await browser("click","New topic note");await waitFor(`!!(${editor("Note name")})`,"note editor")
  await typeInto(editor("Note name"),title);await typeInto(editor("Index summary"),"Synthetic index summary");await typeInto(editor("Note body"),first)
  await browser("screenshot","--out",path.join(artifacts,"en-note-create.jpg"))
  await tap(`document.querySelector(".pa-memory-note-editor button[type=submit]")`)
  await waitFor(`!document.querySelector(".pa-memory-note-editor")&&!!(${indexButton("Read note: "+title)})`,"saved note in the index")
  await browser("close");tab=undefined
  // A fresh page reads the note back from the source.
  await openMemory("en");await waitFor(`!!(${indexButton("Read note: "+title)})`,"note after reload")
  await tap(indexButton("Read note: "+title));await waitText(first)
  await browser("click","Edit topic note");await waitFor(`!!(${editor("Note body")})`,"edit form")
  assert.equal(await evaluate(`(${editor("Note name")}).value`),title)
  await typeInto(editor("Note body"),second,true)
  await tap(`document.querySelector(".pa-memory-note-editor button[type=submit]")`)
  await waitFor(`!document.querySelector(".pa-memory-note-editor")`,"edit saved")
  await browser("close");tab=undefined
  await openMemory("en");await waitFor(`!!(${indexButton("Read note: "+title)})`,"edited note after reload")
  await tap(indexButton("Read note: "+title));await waitText(second);await waitText(first,false)
  await browser("screenshot","--out",path.join(artifacts,"en-note-edited.jpg"))
  await tap(`[...document.querySelectorAll(".pa-memory-detail button")].find(b=>b.textContent==="Forget selected sources")`)
  await waitFor(`!(${indexButton("Read note: "+title)})`,"forgotten note leaves the index")
  await browser("close");tab=undefined
  await openMemory("en");await waitText("Synthetic local fact");await waitFor(`!(${indexButton("Read note: "+title)})`,"forgotten note stays gone after reload")
  await browser("close");tab=undefined
  // zh-Hant at 390px: a short fact typed with the keyboard and saved.
  const fact="Synthetic short fact typed on a phone."
  await openMemory("zh-Hant")
  await tap(`document.querySelector(".pa-memory-short-facts>summary")`)
  await typeInto(`document.querySelector(".pa-memory-short-facts textarea")`,fact)
  await browser("click","儲存記憶");await waitText(fact)
  assert.equal(await evaluate("document.documentElement.scrollWidth<=innerWidth"),true,"Memory stays within the phone viewport")
  await browser("screenshot","--out",path.join(artifacts,"zh-Hant-short-fact.jpg"))
  await browser("close");tab=undefined
  await openMemory("zh-Hant");await waitText(fact)
  await browser("close");tab=undefined
  const writes=requests.filter(r=>r.method!=="GET")
  assert.deepEqual(writes.map(r=>[r.method,r.path.split("/").pop(),r.status]),[["POST","memory",200],["POST","memory",200],["POST","forget",200],["POST","memory",200]])
  assert.equal(writes[0].body.entry.id,writes[1].body.entry.id,"editing keeps the note's identity")
  console.log("Writable memory through real Chrome input, production MemoryPanel/SourceClient and authenticated Host/SQLite: topic note created, reloaded, edited, reloaded, forgotten and confirmed gone; zh-Hant short fact saved and reloaded at 390px. External model/API requests=0.")
}
async function waitHeld(){for(let n=0;n<100&&!held;n++)await new Promise(r=>setTimeout(r,50));assert.equal(held,true)}
try{
  await connect()
  if(process.env.BOTS_MEMORY_WRITE)await memoryWrites()
  else{
  for(const [language,readonly] of [["en",true],["en",false],["zh-Hant",false]]){
    const zh=language!=="en",memory=zh?"記憶":"Memory",settings=zh?"Bot 設定":"Bot settings",project=zh?"Project 共用記憶":"Project shared memory",local=zh?"這個 Bot 的記憶":"This Bot’s memory",read=zh?"閱讀筆記":"Read note",save=zh?"儲存":"Save"
    await browser("open",origin+"/?lang="+language+(readonly?"&read=1":""));await browser("viewport",zh?"mobile":"desktop");await waitText(settings)
    const beforeMemory=requests.length
    await browser("click",memory);await waitText(local)
    assert.deepEqual(requests.slice(beforeMemory).filter(r=>r.path.endsWith("/memory-view")).map(r=>r.page),["0"])
    await browser("click",project);await waitText("sample-0000")
    await waitText("sample-0025",false)
    assert.equal(await evaluate('document.querySelectorAll(".pa-memory-collection .pa-memory-index").length'),25)
    await waitText(zh?"第 1 頁，共 2 頁":"Page 1 of 2")
    await browser("click",zh?"下一頁":"Next page");await waitText("Synthetic shared long note")
    await waitText("sample-0000",false)
    await waitText(zh?"第 2 頁，共 2 頁":"Page 2 of 2")
    await browser("click",zh?"上一頁":"Previous page");await waitText("sample-0000")
    await browser("click",zh?"下一頁":"Next page");await waitText("Synthetic shared long note")
    assert.deepEqual(requests.slice(beforeMemory).filter(r=>r.path.endsWith("/memory-view")).map(r=>r.page),["0","1","2","1","2"])
    await browser("click",read+": shared-note");await waitText("Synthetic authorized Project note.")
    await browser("screenshot","--out",path.join(artifacts,language+(readonly?"-readonly":"")+"-memory.jpg"));await writeFile(path.join(artifacts,language+(readonly?"-readonly":"")+"-memory.html"),await evaluate("document.documentElement.outerHTML"))
    assert.equal(await evaluate("document.documentElement.scrollWidth<=innerWidth"),true,"Memory stays within viewport")
    // A detail finishing after switching layers cannot publish into the new layer.
    holdNext=true;held=false;await browser("click",read+": shared-note");await waitHeld();await browser("click",local);releaseHeld();await waitText("Synthetic local fact");await waitText("Synthetic authorized Project note.",false)
    const picker=`[...document.querySelectorAll("button.pa-agent-picker-summary")].find(b=>b.getAttribute("aria-label")?.startsWith(${JSON.stringify((zh?"主要 Agent":"Primary Agent")+": ")}))`
    await browser("click",settings);await waitFor(`!!(${picker})`,"Agent picker")
    if(readonly){assert.equal(await evaluate(`(${picker}).matches(":disabled")`),true);assert.equal((await browser("click",save)).disabled,true);assert.equal(requests.filter(r=>r.method!=="GET").length,0)}
    else{
      await tap(picker);await waitFor(`!!(${paSelect("Reasoning effort")})`,"native settings")
      await choose(zh?"模型":"Model","Synthetic large");await waitFor(`!!(${paSelect("Reasoning effort")})`,"settings for the chosen model")
      await choose("Reasoning effort","High",{keyboard:true});await choose("Fast mode",zh?"開啟":"On")
      await waitFor(`(${buttonByLabel(save)})?.disabled===false`,"Save enabled")
      await browser("screenshot","--out",path.join(artifacts,language+"-settings.jpg"));await writeFile(path.join(artifacts,language+"-settings.html"),await evaluate("document.documentElement.outerHTML"))
      await browser("click",save);await waitText("Settings saved")
      if(zh){await waitFor(`!document.querySelector(".pa-settings-editor")`,"settings closed");await browser("click","Send scripted input");await waitText("Scripted input accepted")}
    }
    await browser("close");tab=undefined
  }
  await browser("open",origin+"/?lang=en&read=1");await waitText("Bot settings")
  const beforeDowngrade=requests.length;assert.equal(await evaluate("window.fixtureDowngrade()"),"pa_unsupported");assert.equal(requests.length,beforeDowngrade);
  holdNext=true;held=false;await evaluate("window.fixtureAttempt=window.fixtureRead();undefined");await waitHeld();await evaluate("window.fixtureRevoke()");releaseHeld();assert.equal(await evaluate("window.fixtureAttempt"),"pa_revoked");await browser("close");tab=undefined
  const writes=requests.filter(r=>r.method!=="GET");assert.equal(writes.filter(r=>r.method==="PATCH").length,2);assert.equal(writes.filter(r=>r.path.endsWith("/messages")).length,1);assert.ok(writes.every(r=>r.status===200))
  console.log("Real bilingual browser + production SourceClient + authenticated Host/SQLite: shared long detail, read-only controls, delayed layer/source rejection, two saved settings and one actual scripted ACP prompt. External model/API requests=0.")
  }
}finally{
  releaseHeld?.()
  if(tab&&send)await browser("close").catch(error=>console.error("Owned tab cleanup:",error.message))
  ws?.close();chrome.kill();await new Promise(resolve=>chrome.exitCode!==null||chrome.signalCode!==null?resolve():chrome.once("exit",resolve));await rm(profile,{recursive:true,force:true,maxRetries:5})
  await writeFile(path.join(artifacts,"requests.json"),JSON.stringify(requests,null,2)+"\n");await writeFile(path.join(artifacts,"operations.json"),JSON.stringify(operations,null,2)+"\n");await new Promise(resolve=>server.close(resolve))
}
