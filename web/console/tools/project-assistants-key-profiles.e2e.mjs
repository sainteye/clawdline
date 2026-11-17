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
import {BotSettings} from "./src/project-assistants/BotSettings";import {FallbackSourceScope} from "./src/project-assistants/FallbackSettings";
import {ProjectAssistantSourceClient} from "./src/cloud/project-assistants";import {command} from "./src/project-assistants/client";import {words} from "./src/project-assistants/words";import {bootCatalog} from "./src/catalog";import "./src/project-assistants/styles.css";
const q=new URL(location.href).searchParams,language=q.get("lang"),read=q.has("read");localStorage.setItem("ui_language",language);await bootCatalog(tag=>"/catalog/"+tag+".json");
let live={accountKey:"synthetic-account",readerID:read?"read-only":"synthetic-reader",machineID:${JSON.stringify(machine)},authorizationRevision:"synthetic-grant",apiLevel:82,paired:true,online:true,writable:!read};
const source=new ProjectAssistantSourceClient(live,(input,init)=>fetch(input+(read?(input.includes("?")?"&":"?")+"fixture_read=1":""),init),()=>live);
const key={machine_id:live.machineID,bot_id:${JSON.stringify(bot)}},got=await source.get(key,new AbortController().signal),w=words(language);
window.fixtureRead=()=>source.loginProfiles(key,got.conversation.id,"codex",new AbortController().signal).then(()=>"unexpected publication",e=>e.error?.code);
window.fixtureRevoke=()=>{live={...live,authorizationRevision:"revoked-grant"}};
window.fixtureDowngrade=async()=>{live={...live,apiLevel:80};try{await source.edit({command:command(key,"edit"),settings:{name:got.bot.name,appearance:got.bot.appearance,instructions:got.bot.instructions.text,expected_version:got.bot.version,login_profile:{id:""}}},new AbortController().signal);return "unexpected write"}catch(e){return e.error?.code}finally{live={...live,apiLevel:82}}};
function Fixture(){const [bot,setBot]=useState(got.bot),[screen,setScreen]=useState("home"),[message,setMessage]=useState("");return <FallbackSourceScope source={source}><main className="project-assistants" style={{maxWidth:900,margin:"0 auto"}}><nav><button onClick={()=>setScreen("settings")}>{w.settings}</button><button disabled={read} onClick={async()=>{const fresh=await source.get(key,new AbortController().signal);const c=fresh.conversation;await source.send({command:command(key,"send"),fence:{key,project:fresh.bot.project,conversation_id:c.id,binding_id:c.binding_id,generation:c.generation},text:"Synthetic selected account input",quote_message_id:""},new AbortController().signal);setMessage("Scripted input accepted")}}>Send scripted input</button></nav>{screen==="settings"&&<BotSettings bot={bot} enabled={!read} teamAvailable={true} w={w} onClose={()=>setScreen("home")} onSave={async settings=>{const saved=await source.edit({command:command(key,"edit"),settings},new AbortController().signal);setBot(saved.bot);setScreen("home");setMessage("Settings saved")}}/>}<p role="status">{message}</p></main></FallbackSourceScope>};createRoot(document.getElementById("app")).render(<Fixture/>);
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
    const payload=chunks.length?JSON.parse(Buffer.concat(chunks)):undefined;if(payload&&url.pathname.endsWith("/login-secret"))delete payload.value;requests.push({method:req.method,path:url.pathname,status:answer.status,...(payload?{body:payload}:{})})
    if(holdNext&&url.pathname.endsWith("/login-profiles")){holdNext=false;held=true;await new Promise(resolve=>{releaseHeld=resolve})}
    res.writeHead(answer.status,{"Content-Type":"application/json"});res.end(body);return
  }
  if(url.pathname.startsWith("/catalog/")){res.setHeader("Content-Type","application/json");res.end(await readFile(path.join(root,"public/catalogs",path.basename(url.pathname))));return}
  if(files.has(url.pathname)){res.setHeader("Content-Type",url.pathname.endsWith(".css")?"text/css":"text/javascript");res.end(files.get(url.pathname));return}
  res.setHeader("Content-Type","text/html");res.end('<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="stylesheet" href="/login-capabilities.css"><div id="app"></div><script type="module" src="/login-capabilities.js"></script>')
}catch(error){res.writeHead(500);res.end(String(error))}})
await new Promise(resolve=>server.listen(0,"127.0.0.1",resolve));const origin=`http://127.0.0.1:${server.address().port}`
const executable=process.env.CHROME;assert.ok(executable,"CHROME must name an existing test browser; this harness installs nothing");await access(executable,constants.X_OK)
const profile=await mkdtemp(path.join(artifacts,"browser-profile-")),chrome=spawn(executable,["--headless=new","--remote-debugging-port=0","--user-data-dir="+profile,"--no-first-run","--no-default-browser-check","about:blank"],{stdio:["ignore","ignore","pipe"]})
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
  else if(op==="click")result=await evaluate(`(()=>{const label=${JSON.stringify(args[0])};const button=[...document.querySelectorAll("button")].find(b=>(b.getAttribute("aria-label")??b.textContent)===label);if(!button)throw Error("Missing button: "+label);const disabled=button.disabled;button.click();return {disabled}})()`)
  else if(op==="screenshot"){const {data}=await send("Page.captureScreenshot",{format:"jpeg"},session);await writeFile(args[1],Buffer.from(data,"base64"));result={captured:true}}
  else if(op==="close"){result=await send("Target.closeTarget",{targetId:tab});assert.equal(result.success,true)}
  else throw Error("Unsupported test operation: "+op)
  operations.push({op,args,result});return result
}
async function waitText(text,present=true){let last;for(let n=0;n<30;n++){last=await browser("read");if(last.text?.includes(text)===present)return last;await new Promise(r=>setTimeout(r,100))}throw Error(`Text ${text} present=${present}: ${JSON.stringify(last)}`)}
async function select(label,value){await evaluate(`(()=>{const el=[...document.querySelectorAll("select")].find(e=>(e.getAttribute("aria-label")??e.closest("label")?.firstChild.textContent)===${JSON.stringify(label)});if(!el)throw Error("Missing select: "+${JSON.stringify(label)});el.value=${JSON.stringify(value)};el.dispatchEvent(new Event("change",{bubbles:true}))})()`)}
async function waitHeld(){for(let n=0;n<100&&!held;n++)await new Promise(r=>setTimeout(r,50));assert.equal(held,true)}
try{
  await connect()
  for(const [language,readonly] of [["en",true],["en",false],["zh-Hant",false]]){
    const zh=language!=="en",settings=zh?"Bot 設定":"Bot settings",save=zh?"儲存":"Save",label=zh?"登入設定":"Login profile",name=zh?"登入設定名稱":"Login profile name",create=zh?"新增獨立帳號登入":"Add account login profile",start=zh?"開啟原生登入":"Open native sign-in",refresh=zh?"檢查新訊息":"Check for updates";
    await browser("open",origin+"/?lang="+language+(readonly?"&read=1":""));await browser("viewport",zh?"mobile":"desktop");await waitText(settings);await browser("click",settings);await waitText(zh?"預設登入設定":"Default login profile");
    if(readonly){assert.equal((await browser("click",save)).disabled,true);assert.equal(requests.filter(r=>r.method!=="GET").length,0)}
    else{
      const account=zh?"Synthetic second browser key":"Synthetic first browser key";
      await evaluate(`(()=>{const e=document.querySelector('input[aria-label="${name}"]');Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,"value").set.call(e,${JSON.stringify(account)});e.dispatchEvent(new Event("input",{bubbles:true}));})()`);
      await browser("click",zh?"新增 API 金鑰登入":"Add API-key login profile");await waitText(account);
      const id=await evaluate(`(()=>{const s=document.querySelector('select[aria-label="${label}"]');return [...s.options].find(o=>o.textContent===${JSON.stringify(account)}).value})()`);assert.match(id,/^[0-9a-f]{32}$/);await select(label,id);await browser("click",zh?"輸入或更換 API 金鑰":"Enter or replace API key");await waitText("CODEX_API_KEY");
      await evaluate(`document.querySelector('textarea[aria-label="${zh?"API 金鑰":"API key"}"]').scrollIntoView({block:"center"})`);await browser("screenshot","--out",path.join(artifacts,language+"-key-form.jpg"));await writeFile(path.join(artifacts,language+"-key-form.html"),await evaluate("document.documentElement.outerHTML"));
      await evaluate(`(()=>{document.querySelector('textarea[aria-label="${zh?"API 金鑰":"API key"}"]').value="SYNTHETIC_PROFILE_KEY_ONLY";})()`);
      await browser("click",zh?"安全交付金鑰":"Deliver key securely");
      for(let i=0;i<20;i++){await browser("click",refresh);const view=await browser("read");if(view.text.includes(zh?"金鑰已保存（尚未驗證供應商）":"Key saved (provider not verified)"))break;await new Promise(r=>setTimeout(r,100))}
      await waitText(zh?"金鑰已保存（尚未驗證供應商）":"Key saved (provider not verified)");await waitText(zh?"這個 Agent 未提供模型選單。":"This Agent does not expose a model selector.");
      await browser("click",zh?"新增替代 Agent":"Add alternate Agent");
      const alternate=zh?"Codex · Synthetic first browser key":"Codex · Default login profile";
      await waitText(alternate);await evaluate(`(()=>{const option=[...document.querySelectorAll('[role="option"]')].find(o=>o.textContent===${JSON.stringify(alternate)});if(!option)throw Error("Missing selected-login fallback");option.click()})()`);
      await waitText(alternate);await browser("screenshot","--out",path.join(artifacts,language+"-fallbacks.jpg"));
      await waitText("Synthetic installed skill");await evaluate(`(()=>{for(const label of document.querySelectorAll("label")){if(label.textContent==="Synthetic installed skill"||label.textContent==="fixture-connector"){const checkbox=label.querySelector('input[type="checkbox"]');if(checkbox&&!checkbox.checked)checkbox.click()}}})()`);await browser("screenshot","--out",path.join(artifacts,language+"-login.jpg"));await writeFile(path.join(artifacts,language+"-login.html"),await evaluate("document.documentElement.outerHTML"));assert.equal(await evaluate("document.documentElement.scrollWidth<=innerWidth"),true);
      await evaluate(`document.querySelector('section[aria-label="${zh?"應用程式技能與連接器":"Application skills and connectors"}"]').scrollIntoView()`);await browser("screenshot","--out",path.join(artifacts,language+"-extensions.jpg"));await browser("click",save);await waitText("Settings saved");await browser("click","Send scripted input");await waitText("Scripted input accepted");
      await browser("click",settings);await waitText(zh?"輸入或更換 API 金鑰":"Enter or replace API key");await browser("click",zh?"輸入或更換 API 金鑰":"Enter or replace API key");await waitText("pa_scope_denied");
      assert.equal(await evaluate(`Object.keys(localStorage).filter(k=>k.startsWith("clawdline.bot-login-intent:")).length`),0,"a refused referenced-key replacement must not trap the settings journal");
    }
    await browser("close");tab=undefined;
  }
  await browser("open",origin+"/?lang=en&read=1");await waitText("Bot settings");const count=requests.length;assert.equal(await evaluate("window.fixtureDowngrade()"),"pa_unsupported");assert.equal(requests.length,count);
  holdNext=true;held=false;await evaluate("window.fixtureAttempt=window.fixtureRead();undefined");await waitHeld();await evaluate("window.fixtureRevoke()");releaseHeld();assert.equal(await evaluate("window.fixtureAttempt"),"pa_revoked");await browser("close");tab=undefined;
  const writes=requests.filter(r=>r.method!=="GET");assert.equal(writes.filter(r=>r.method==="PATCH").length,2);assert.equal(writes.filter(r=>r.path.endsWith("/messages")).length,2);assert.equal(writes.filter(r=>r.path.endsWith("/login-profile")).length,6);assert.equal(writes.filter(r=>r.path.endsWith("/login-secret")).length,2);assert.ok(writes.every(r=>r.status===200||r.path.endsWith("/login-profile")&&r.body.action==="prepare-key"&&r.status===403));
  assert.ok(writes.filter(r=>r.method==="PATCH").every(r=>r.body.settings.fallback_steps?.some(s=>s.agent==="codex")));assert.ok(writes.filter(r=>r.method==="PATCH")[1].body.settings.fallback_steps.some(s=>s.login_profile));
  console.log("Production bilingual UI and SourceClient crossed authenticated HTTP/Host/SQLite, securely stored two synthetic API-key login profiles, saved each selection and sent actual scripted ACP turns. Read-only, API downgrade and late source rejection passed. External/model API requests=0.");
}finally{
  releaseHeld?.()
  if(tab&&send)await browser("close").catch(error=>console.error("Owned tab cleanup:",error.message))
  ws?.close();chrome.kill();await new Promise(resolve=>chrome.exitCode!==null||chrome.signalCode!==null?resolve():chrome.once("exit",resolve));await rm(profile,{recursive:true,force:true,maxRetries:5})
  await writeFile(path.join(artifacts,"requests.json"),JSON.stringify(requests,null,2)+"\n");await writeFile(path.join(artifacts,"operations.json"),JSON.stringify(operations,null,2)+"\n");await new Promise(resolve=>server.close(resolve))
}
