/** Synthetic ACP configuration and layered memory cross production UI, SourceClient, Host and SQLite. */
import assert from "node:assert/strict"
import {createServer} from "node:http"
import {mkdir,mkdtemp,readFile,writeFile,rm,access} from "node:fs/promises"
import {spawn} from "node:child_process"
import {constants} from "node:fs"
import {fileURLToPath} from "node:url"
import path from "node:path"
import {build} from "esbuild"

const {BOTS_ORIGIN:upstream,BOTS_TOKEN:token,BOTS_READ_TOKEN:readToken,BOTS_MACHINE:machine,BOTS_BOT:bot,BOTS_ARTIFACTS:artifacts,BOTS_MODEL_STATE:modelState}=process.env
assert.ok(upstream&&token&&readToken&&machine&&bot&&artifacts)
await mkdir(artifacts,{recursive:true})
const root=fileURLToPath(new URL("..",import.meta.url)),requests=[],operations=[]

const bundle=await build({stdin:{contents:`
import React,{useState} from "react";import {createRoot} from "react-dom/client";
import {BotSettings} from "./src/project-assistants/BotSettings";import {FallbackSourceScope} from "./src/project-assistants/FallbackSettings";
import {ProjectAssistantSourceClient} from "./src/cloud/project-assistants";import {command} from "./src/project-assistants/client";import {words} from "./src/project-assistants/words";import {bootCatalog} from "./src/catalog";import "./src/project-assistants/styles.css";
const q=new URL(location.href).searchParams,language=q.get("lang"),read=q.has("read");localStorage.setItem("ui_language",language);await bootCatalog(tag=>"/catalog/"+tag+".json");
let live={accountKey:"synthetic-account",readerID:read?"read-only":"synthetic-reader",machineID:${JSON.stringify(machine)},authorizationRevision:"synthetic-grant",apiLevel:${modelState?91:81},paired:true,online:true,writable:!read};
const source=new ProjectAssistantSourceClient(live,(input,init)=>fetch(input+(read?(input.includes("?")?"&":"?")+"fixture_read=1":""),init),()=>live);
const key={machine_id:live.machineID,bot_id:${JSON.stringify(bot)}},got=await source.get(key,new AbortController().signal),w=words(language);
window.fixtureRead=()=>source.loginProfiles(key,got.conversation.id,"codex",new AbortController().signal).then(()=>"unexpected publication",e=>e.error?.code);
window.fixtureProbe=()=>source.configuration(key,got.conversation.id,"codex","",new AbortController().signal).then(()=>true);
window.fixtureOnline=online=>{live={...live,online}};
window.fixtureRevoke=()=>{live={...live,authorizationRevision:"revoked-grant"}};
window.fixtureDowngrade=async()=>{live={...live,apiLevel:80};try{await source.edit({command:command(key,"edit"),settings:{name:got.bot.name,appearance:got.bot.appearance,instructions:got.bot.instructions.text,expected_version:got.bot.version,login_profile:{id:""}}},new AbortController().signal);return "unexpected write"}catch(e){return e.error?.code}finally{live={...live,apiLevel:81}}};
let sentTurn:{id:string,conversationID:string}|undefined;
async function sendScriptedInput(setMessage:(message:string)=>void){
  const signal=new AbortController().signal;
  const fresh=await source.get(key,signal),c=fresh.conversation;
  const {receipt}=await source.send({command:command(key,"send"),fence:{key,project:fresh.bot.project,conversation_id:c.id,binding_id:c.binding_id,generation:c.generation},text:"Synthetic selected account input",quote_message_id:""},signal);
  sentTurn={id:receipt.resource_id,conversationID:c.id};
  setMessage("Scripted input accepted");
}
window.fixtureTurnSucceeded=async()=>{
  if(!sentTurn)return false;
  const page=await source.view({key,conversation_id:sentTurn.conversationID,mode:"snapshot",source_epoch:"",cursor:"",after_seq:0,page_size:1},new AbortController().signal);
  const last=page.snapshot?.last_turn;
  return last?.id===sentTurn.id&&last.state==="succeeded";
};
function Fixture(){const [bot,setBot]=useState(got.bot),[screen,setScreen]=useState("home"),[message,setMessage]=useState("");return <FallbackSourceScope source={source}><main className="project-assistants" style={{maxWidth:900,margin:"0 auto"}}><nav><button onClick={()=>setScreen("settings")}>{w.settings}</button><button disabled={read} onClick={()=>void sendScriptedInput(setMessage)}>Send scripted input</button></nav>{screen==="settings"&&<BotSettings bot={bot} enabled={!read} reason={read?w.readOnlySource:""} teamAvailable={true} w={w} onClose={()=>setScreen("home")} onSave={async settings=>{const saved=await source.edit({command:command(key,"edit"),settings},new AbortController().signal);setBot(saved.bot);setScreen("home");setMessage("Settings saved")}}/>}<p role="status">{message}</p></main></FallbackSourceScope>};createRoot(document.getElementById("app")).render(<Fixture/>);
`,resolveDir:root,loader:"tsx"},bundle:true,write:false,outfile:"login-capabilities.js",format:"esm",target:"es2022",platform:"browser",jsx:"automatic",define:{"process.env.NODE_ENV":'"development"'}})
const files=new Map(bundle.outputFiles.map(f=>["/"+path.basename(f.path),f.contents]))
let tab,holdNext=false,releaseHeld,held
const server=createServer(async(req,res)=>{try{
  const url=new URL(req.url,"http://fixture")
  if(url.pathname.startsWith("/v1/")){
    const read=url.searchParams.has("fixture_read");url.searchParams.delete("fixture_read")
    const chunks=[];for await(const chunk of req)chunks.push(chunk)
    const headers={Authorization:"Bearer "+(read?readToken:token)};for(const key of ["content-type","idempotency-key"])if(req.headers[key])headers[key]=req.headers[key]
    const answer=await fetch(upstream+url.pathname+url.search,{method:req.method,headers,...(chunks.length?{body:Buffer.concat(chunks)}:{})}),body=Buffer.from(await answer.arrayBuffer())
    requests.push({method:req.method,path:url.pathname,status:answer.status,...(chunks.length?{body:JSON.parse(Buffer.concat(chunks))}:{})})
    if(holdNext&&url.pathname.endsWith("/login-profiles")){holdNext=false;held=true;await new Promise(resolve=>{releaseHeld=resolve})}
    res.writeHead(answer.status,{"Content-Type":"application/json"});res.end(body);return
  }
  if(url.pathname.startsWith("/catalog/")){res.setHeader("Content-Type","application/json");res.end(await readFile(path.join(root,"public/catalogs",path.basename(url.pathname))));return}
  if(files.has(url.pathname)){res.setHeader("Content-Type",url.pathname.endsWith(".css")?"text/css":"text/javascript");res.end(files.get(url.pathname));return}
  res.setHeader("Content-Type","text/html");res.end('<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="stylesheet" href="/login-capabilities.css"><div id="app"></div><script type="module" src="/login-capabilities.js"></script>')
}catch(error){res.writeHead(500);res.end(String(error))}})
await new Promise(resolve=>server.listen(0,"127.0.0.1",resolve));const origin=`http://127.0.0.1:${server.address().port}`
const executable=process.env.CHROME;assert.ok(executable,"CHROME must name an existing test browser; this harness installs nothing");await access(executable,constants.X_OK)
const profile=await mkdtemp(path.join(artifacts,"browser-profile-")),chrome=spawn(executable,["--headless=new","--remote-debugging-port=0","--use-mock-keychain","--user-data-dir="+profile,"--no-first-run","--no-default-browser-check","--disable-features=OmniboxEverywhere","about:blank"],{stdio:["ignore","ignore","pipe"]})
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
  else if(op==="click")result=modelState?await tap(buttonByLabel(args[0])):await evaluate(`(()=>{const label=${JSON.stringify(args[0])};const button=[...document.querySelectorAll("button")].find(b=>(b.getAttribute("aria-label")??b.textContent)===label);if(!button)throw Error("Missing button: "+label);const disabled=button.disabled;button.click();return {disabled}})()`)
  else if(op==="screenshot"){const {data}=await send("Page.captureScreenshot",{format:"jpeg"},session);await writeFile(args[1],Buffer.from(data,"base64"));result={captured:true}}
  else if(op==="close"){result=await send("Target.closeTarget",{targetId:tab});assert.equal(result.success,true)}
  else throw Error("Unsupported test operation: "+op)
  operations.push({op,args,result});return result
}
// The model note lives inside the collapsed Primary Agent picker; open it with a real pointer press.
async function openAgentPicker(zh){const prefix=JSON.stringify(zh?"主要 Agent:":"Primary Agent:"),find=`[...document.querySelectorAll("button.pa-agent-picker-summary")].find(b=>b.getAttribute("aria-label")?.startsWith(${prefix}))`;if(await evaluate(`${find}?.getAttribute("aria-expanded")==="true"`))return;await tap(find)}
async function waitText(text,present=true){let last;for(let n=0;n<30;n++){last=await browser("read");if(last.text?.includes(text)===present)return last;await new Promise(r=>setTimeout(r,100))}throw Error(`Text ${text} present=${present}: ${JSON.stringify(last)}`)}
async function select(label,value){await evaluate(`(()=>{const el=[...document.querySelectorAll("select")].find(e=>(e.getAttribute("aria-label")??e.closest("label")?.firstChild.textContent)===${JSON.stringify(label)});if(!el)throw Error("Missing select: "+${JSON.stringify(label)});el.value=${JSON.stringify(value)};el.dispatchEvent(new Event("change",{bubbles:true}))})()`)}
async function waitHeld(){for(let n=0;n<100&&!held;n++)await new Promise(r=>setTimeout(r,50));assert.equal(held,true)}
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
async function disclose(label){
  const summary=`[...document.querySelectorAll("summary")].find(el=>el.querySelector("strong")?.textContent===${JSON.stringify(label)})`
  if(!await evaluate(`(${summary})?.parentElement.open===true`))await tap(summary)
  await waitFor(`(${summary})?.parentElement.open===true`,"disclosure "+label)
}
async function waitFile(file){
  for(let n=0;n<100;n++){
    if(await access(file).then(()=>true,()=>false))return
    await new Promise(r=>setTimeout(r,30))
  }
  throw Error("Synthetic source did not reach "+path.basename(file))
}
// The Agent and model picker (AgentModelPicker since c4b9ad3ba): one summary button opening PaSelect drop-downs.
const picker=primary=>`[...document.querySelectorAll("button.pa-agent-picker-summary")].find(b=>b.getAttribute("aria-label")?.startsWith(${JSON.stringify(primary+": ")}))`
async function modelMatrix(){
  const hold=path.join(modelState,"synthetic-probe-hold"),ready=path.join(modelState,"synthetic-probe-ready")
  try{
    for(const [language,read] of [["en",true],["en",false],["zh-Hant",false]]){
      const zh=language==="zh-Hant",settings=zh?"Bot 設定":"Bot settings",primary=zh?"主要 Agent":"Primary Agent",model=zh?"模型":"Model",save=zh?"儲存":"Save",retry=zh?"重新讀取模型清單":"Retry model list"
      const open=async()=>{
        await browser("click",settings);await waitFor(`!!(${picker(primary)})`,"Agent picker")
        if(await evaluate(`(${picker(primary)}).matches(":disabled")`))return false
        if(await evaluate(`(${picker(primary)}).getAttribute("aria-expanded")!=="true"`))await tap(picker(primary))
        await waitFor(`(${picker(primary)}).getAttribute("aria-expanded")==="true"`,"picker open");return true
      }
      await browser("open",origin+"/?lang="+language+(read?"&read=1":""))
      await browser("viewport",zh?"mobile":"desktop")
      await waitText(settings)
      if(read){
        const before=requests.length
        assert.equal(await open(),false,"a read-only source cannot open the Agent picker")
        await waitText("this machine has operation from Clawdline Cloud turned off")
        assert.equal((await browser("click",save)).disabled,true)
        assert.equal(await evaluate(`(${picker(primary)}).matches(":disabled")`),true)
        assert.equal(requests.slice(before).filter(r=>r.method!=="GET"||r.path.endsWith("/configuration")).length,0)
      }else{
        if(!zh){
          await writeFile(hold,"")
          await evaluate("window.fixtureHeldProbe=window.fixtureProbe();undefined")
          await waitFile(ready)
          await open()
          await waitText("The source is already checking Agent models.")
          assert.ok(requests.some(r=>r.path.endsWith("/configuration")&&r.status===429))
          await rm(hold);assert.equal(await evaluate("window.fixtureHeldProbe"),true);await rm(ready)
          await evaluate("window.fixtureOnline(false)")
          await browser("click",retry)
          // Since 08e54d0e7 Bots leave machine connection wording to the main interface; the code stays visible.
          await waitText("(pa_offline)");await waitFor(`!!(${buttonByLabel(retry)})`,"retry after offline")
          await evaluate("window.fixtureOnline(true)")
          await browser("click",retry)
          await waitFor(`!!(${paSelect(model)})`,"model list")
          assert.equal(await selectedText(model),"Default · codex source default")
          // Change Agent while an occupied model probe is still returning.
          await writeFile(hold,"")
          await choose(primary,"Claude Code",{keyboard:true})
          await waitFile(ready)
          await waitText("Reading Agent configuration")
          await choose(primary,"Codex")
          await rm(hold);await rm(ready)
          await waitFor(`(${paSelect(model)})?.textContent==="Default · codex source default"`,"codex models")
          assert.equal(requests.filter(r=>r.path.endsWith("/configuration")&&r.status===429).length,1)
          await choose(primary,"Claude Code")
          await waitFor(`(${paSelect(model)})?.textContent==="Default · claude source default"`,"claude models")
          await writeFile(hold,"")
          await choose(model,"claude source explicit")
          await waitFile(ready)
          assert.equal((await browser("click",save)).disabled,true)
          await rm(hold);await rm(ready)
          await waitText("Saved Agent and model changes apply")
          await waitFor(`(${paSelect(model)})?.textContent==="claude source explicit"`,"explicit model after the probe")
          await disclose("Alternate Agents")
          await waitFor(`!!(${buttonByLabel("Add alternate Agent")})`,"alternate drop-down")
          await browser("click","Add alternate Agent")
          await waitFor(`[...document.querySelectorAll('[role=option]')].some(el=>el.textContent==="Codex · Default login profile")`,"alternate choice")
          await tap(`[...document.querySelectorAll('[role=option]')].find(el=>el.textContent==="Codex · Default login profile")`)
        }else{
          await open();await waitFor(`!!(${paSelect(model)})`,"model list")
          assert.equal(await selectedText(model),"claude source explicit")
          await choose(primary,"Codex",{keyboard:true});await waitFor(`(${paSelect(model)})?.textContent?.endsWith("codex source default")`,"codex models")
          await browser("click","取消")
          await open();await waitFor(`!!(${paSelect(model)})`,"model list")
          assert.equal(await selectedText(model),"claude source explicit")
          await disclose("替代 Agent");await waitText("Codex · 預設登入設定")
          await choose(model,0,{keyboard:true});await waitText("儲存的 Agent 與模型變更")
          assert.notEqual(await selectedText(model),"claude source explicit")
        }
        assert.equal(await evaluate("document.documentElement.scrollWidth<=innerWidth"),true)
        await browser("screenshot","--out",path.join(artifacts,language+"-models.jpg"))
        // Save stays disabled while the chosen model is still being confirmed by the source.
        await waitFor(`(${buttonByLabel(save)})?.disabled===false`,"Save enabled after the model check")
        assert.equal((await browser("click",save)).disabled,false)
        await waitText("Settings saved")
        await open();await waitFor(`!!(${paSelect(model)})`,"model list")
        if(zh){
          await tap(paSelect(model));await waitFor(`!!document.querySelector('.pa-select-list[role=listbox]')`,"listbox")
          assert.equal(await evaluate(`document.querySelectorAll('.pa-select-list [role=option]')[0].getAttribute("aria-selected")`),"true","the saved default reopens as the default entry")
          await key("Escape");await waitFor(`!document.querySelector('.pa-select-list[role=listbox]')`,"listbox closed")
        }else assert.equal(await selectedText(model),"claude source explicit")
        await browser("screenshot","--out",path.join(artifacts,language+"-models-reopened.jpg"))
        // A real pointer cannot reach the page behind the settings panel; close it as a person would.
        await browser("click",zh?"取消":"Cancel");await waitFor(`!document.querySelector(".pa-settings-editor")`,"settings closed")
        await browser("click","Send scripted input");await waitText("Scripted input accepted")
        // The next language opens a fresh settings fixture with one Bot snapshot.
        // Wait for this turn to finish advancing the source before capturing that baseline.
        await waitFor("window.fixtureTurnSucceeded()","the accepted scripted turn to succeed")
      }
      await browser("close");tab=undefined
    }
    const writes=requests.filter(r=>r.method!=="GET")
    assert.deepEqual(writes.map(r=>[r.method,r.status]),[["PATCH",200],["POST",200],["PATCH",200],["POST",200]])
    assert.deepEqual(writes[0].body.settings.fallback_steps,[{agent:"codex"}])
    assert.equal(writes[2].body.settings.fallback_steps,undefined)
    console.log("Production Bot settings and SourceClient crossed HTTP, SQLite and a scripted ACP peer at 1280px and 390px. Capacity retry, offline recovery, read-only, model default and explicit, alternate selection, cancel and reopen passed. External/model API requests=0.")
  }finally{
    await rm(hold,{force:true})
  }
}
try{
  await connect()
  if(modelState){
    await modelMatrix()
  }else{
  for(const [language,readonly] of [["en",true],["en",false],["zh-Hant",false]]){
    const zh=language!=="en",settings=zh?"Bot 設定":"Bot settings",save=zh?"儲存":"Save",label=zh?"登入設定":"Login profile",name=zh?"登入設定名稱":"Login profile name",create=zh?"新增獨立帳號登入":"Add account login profile",start=zh?"開啟原生登入":"Open native sign-in",refresh=zh?"檢查新訊息":"Check for updates";
    await browser("open",origin+"/?lang="+language+(readonly?"&read=1":""));await browser("viewport",zh?"mobile":"desktop");await waitText(settings);await browser("click",settings);await waitText(zh?"預設登入設定":"Default login profile");await evaluate('[...document.querySelectorAll(".pa-settings-disclosure>summary"),...document.querySelectorAll(".pa-login-management>summary")].forEach(el=>el.click())');
    if(readonly){assert.equal((await browser("click",save)).disabled,true);assert.equal(requests.filter(r=>r.method!=="GET").length,0)}
    else{
      const account=zh?"Synthetic second browser account":"Synthetic first browser account";
      await evaluate(`(()=>{const e=document.querySelector('input[aria-label="${name}"]');Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,"value").set.call(e,${JSON.stringify(account)});e.dispatchEvent(new Event("input",{bubbles:true}));})()`);
      await browser("click",create);await waitText(account);
      const id=await evaluate(`(()=>{const s=document.querySelector('select[aria-label="${label}"]');return [...s.options].find(o=>o.textContent===${JSON.stringify(account)}).value})()`);assert.match(id,/^[0-9a-f]{32}$/);await select(label,id);await browser("click",start);
      for(let i=0;i<20;i++){await browser("click",refresh);const view=await browser("read");if(view.text.includes(zh?"已登入":"Signed in"))break;await new Promise(r=>setTimeout(r,100))}
      await waitText(zh?"已登入":"Signed in");await openAgentPicker(zh);await waitText(zh?"這個 Agent 未提供模型選單。":"This Agent does not expose a model selector.");await waitText("Synthetic installed skill");await evaluate(`(()=>{for(const label of document.querySelectorAll("label")){if(label.textContent==="Synthetic installed skill"||label.textContent==="fixture-connector"){const checkbox=label.querySelector('input[type="checkbox"]');if(checkbox&&!checkbox.checked)checkbox.click()}}})()`);await browser("screenshot","--out",path.join(artifacts,language+"-login.jpg"));await writeFile(path.join(artifacts,language+"-login.html"),await evaluate("document.documentElement.outerHTML"));assert.equal(await evaluate("document.documentElement.scrollWidth<=innerWidth"),true);
      await evaluate(`document.querySelector('section[aria-label="${zh?"應用程式技能與連接器":"Application skills and connectors"}"]').scrollIntoView()`);await browser("screenshot","--out",path.join(artifacts,language+"-extensions.jpg"));await browser("click",save);await waitText("Settings saved");await browser("click","Send scripted input");await waitText("Scripted input accepted");
    }
    await browser("close");tab=undefined;
  }
  await browser("open",origin+"/?lang=en&read=1");await waitText("Bot settings");const count=requests.length;assert.equal(await evaluate("window.fixtureDowngrade()"),"pa_unsupported");assert.equal(requests.length,count);
  holdNext=true;held=false;await evaluate("window.fixtureAttempt=window.fixtureRead();undefined");await waitHeld();await evaluate("window.fixtureRevoke()");releaseHeld();assert.equal(await evaluate("window.fixtureAttempt"),"pa_revoked");await browser("close");tab=undefined;
  const writes=requests.filter(r=>r.method!=="GET");assert.equal(writes.filter(r=>r.method==="PATCH").length,2);assert.equal(writes.filter(r=>r.path.endsWith("/messages")).length,2);assert.equal(writes.filter(r=>r.path.endsWith("/login-profile")).length,4);assert.ok(writes.every(r=>r.status===200));
  console.log("Production bilingual UI and SourceClient crossed authenticated HTTP/Host/SQLite, created and signed in two managed native fixture accounts, saved each selection and sent actual scripted ACP turns. Read-only, API downgrade and late source rejection passed. External/model API requests=0.");
  }
}finally{
  releaseHeld?.()
  if(tab&&send)await browser("close").catch(error=>console.error("Owned tab cleanup:",error.message))
  ws?.close();chrome.kill();await new Promise(resolve=>chrome.exitCode!==null||chrome.signalCode!==null?resolve():chrome.once("exit",resolve));await rm(profile,{recursive:true,force:true,maxRetries:5})
  await writeFile(path.join(artifacts,"requests.json"),JSON.stringify(requests,null,2)+"\n");await writeFile(path.join(artifacts,"operations.json"),JSON.stringify(operations,null,2)+"\n");await new Promise(resolve=>server.close(resolve))
}
