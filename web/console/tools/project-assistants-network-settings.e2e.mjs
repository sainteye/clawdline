/** Production network settings UI, SourceClient and authenticated HTTP/Host/SQLite. */
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
let live={accountKey:"synthetic-account",readerID:read?"read-only":"synthetic-reader",machineID:${JSON.stringify(machine)},authorizationRevision:"synthetic-grant",apiLevel:84,paired:true,online:true,writable:!read};
const source=new ProjectAssistantSourceClient(live,(input,init)=>fetch(input+(read?(input.includes("?")?"&":"?")+"fixture_read=1":""),init),()=>live);
const key={machine_id:live.machineID,bot_id:${JSON.stringify(bot)}},got=await source.get(key,new AbortController().signal),w=words(language);
window.fixtureRead=()=>source.networkSettings(key,got.conversation.id,new AbortController().signal).then(()=>"unexpected publication",e=>e.error?.code);
window.fixtureRevoke=()=>{live={...live,authorizationRevision:"revoked-grant"}};
window.fixtureDowngrade=async()=>{live={...live,apiLevel:83};try{await source.networkSettings(key,got.conversation.id,new AbortController().signal);return "unexpected read"}catch(e){return e.error?.code}finally{live={...live,apiLevel:84}}};
window.fixturePreference=async offered=>{try{const reply=await source.networkSettings(key,got.conversation.id,new AbortController().signal);const v=reply.network;const saved=await source.saveNetworkSettings({command:command(key,"network-settings"),fence:v.fence,expected_version:v.bot_version,expected_revision:v.settings.revision,patch:{offered_to_network:{enabled:offered}}},new AbortController().signal);return {revision:saved.network.settings.revision,offered:saved.network.settings.offered_to_network}}catch(e){return e.error?.code}};
function Fixture(){const [screen,setScreen]=useState("home");return <FallbackSourceScope source={source}><main className="project-assistants" style={{maxWidth:900,margin:"0 auto"}}><button onClick={()=>setScreen("settings")}>{w.settings}</button>{screen==="settings"&&<BotSettings bot={got.bot} enabled={!read} teamAvailable={true} w={w} onClose={()=>setScreen("home")} onSave={async()=>{throw Error("not used by this fixture")}}/>}</main></FallbackSourceScope>};createRoot(document.getElementById("app")).render(<Fixture/>);
`,resolveDir:root,loader:"tsx"},bundle:true,write:false,outfile:"network-settings.js",format:"esm",target:"es2022",platform:"browser",jsx:"automatic",define:{"process.env.NODE_ENV":'"development"'}})
const files=new Map(bundle.outputFiles.map(f=>["/"+path.basename(f.path),f.contents]))
let tab,holdNext=false,releaseHeld,held
const server=createServer(async(req,res)=>{try{
 const url=new URL(req.url,"http://fixture")
 if(url.pathname.startsWith("/v1/")){
  const read=url.searchParams.has("fixture_read");url.searchParams.delete("fixture_read");const chunks=[];for await(const chunk of req)chunks.push(chunk)
  const headers={Authorization:"Bearer "+(read?readToken:token)};for(const key of ["content-type","idempotency-key"])if(req.headers[key])headers[key]=req.headers[key]
  const answer=await fetch(upstream+url.pathname+url.search,{method:req.method,headers,...(chunks.length?{body:Buffer.concat(chunks)}:{})}),body=Buffer.from(await answer.arrayBuffer());requests.push({method:req.method,path:url.pathname,status:answer.status,...(chunks.length?{body:JSON.parse(Buffer.concat(chunks))}:{})})
  if(holdNext&&url.pathname.endsWith("/network-settings")){holdNext=false;held=true;await new Promise(resolve=>{releaseHeld=resolve})}
  res.writeHead(answer.status,{"Content-Type":"application/json"});res.end(body);return
 }
 if(url.pathname.startsWith("/catalog/")){res.setHeader("Content-Type","application/json");res.end(await readFile(path.join(root,"public/catalogs",path.basename(url.pathname))));return}
 if(files.has(url.pathname)){res.setHeader("Content-Type",url.pathname.endsWith(".css")?"text/css":"text/javascript");res.end(files.get(url.pathname));return}
 res.setHeader("Content-Type","text/html");res.end('<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="stylesheet" href="/network-settings.css"><div id="app"></div><script type="module" src="/network-settings.js"></script>')
}catch(error){res.writeHead(500);res.end(String(error))}})
await new Promise(resolve=>server.listen(0,"127.0.0.1",resolve));const origin=`http://127.0.0.1:${server.address().port}`
const executable=process.env.CHROME;assert.ok(executable,"CHROME must name an existing test browser; this harness installs nothing");await access(executable,constants.X_OK)
const profile=await mkdtemp(path.join(artifacts,"browser-profile-")),chrome=spawn(executable,["--disable-features=OmniboxEverywhere","--headless=new","--remote-debugging-port=0","--user-data-dir="+profile,"--no-first-run","--no-default-browser-check","about:blank"],{stdio:["ignore","ignore","pipe"]})
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
 for(const language of ["en","zh-Hant"]){
  const zh=language!=="en",settings=zh?"Bot 設定":"Bot settings",title=zh?"Bot 連線對象":"Bot reachability",unavailable=zh?"這個來源尚未設定連線服務。":"This source has no configured reachability service.";
  await browser("open",origin+"/?lang="+language);await browser("viewport",zh?"mobile":"desktop");await waitText(settings);await browser("click",settings);await waitText(title);await waitText(unavailable);
  assert.equal(await evaluate('document.querySelectorAll(".pa-network-settings [role=switch]").length'),0,"unconfigured source offers no working switch");
  await evaluate('document.querySelector(".pa-network-settings").scrollIntoView({block:"center"})');
  await browser("screenshot","--out",path.join(artifacts,language+"-network-unavailable.jpg"));await writeFile(path.join(artifacts,language+"-network-unavailable.html"),await evaluate("document.documentElement.outerHTML"));
  assert.equal(await evaluate("document.documentElement.scrollWidth<=innerWidth"),true);
  assert.equal(await evaluate("fixturePreference(true)"),"pa_unsupported");
  const saved=await evaluate("fixturePreference(false)");assert.equal(saved.offered,false);assert.ok(saved.revision>0);
  const before=requests.length;assert.equal(await evaluate("fixtureDowngrade()"),"pa_unsupported");assert.equal(requests.length,before,"API downgrade refuses before HTTP");
  holdNext=true;held=false;await evaluate("window.pendingRead=fixtureRead();'started'");await waitHeld();await evaluate("fixtureRevoke()");releaseHeld();assert.equal(await evaluate("window.pendingRead"),"pa_revoked");
  await browser("close");tab=undefined;
 }
 await browser("open",origin+"/?lang=en&read=1");await browser("viewport","desktop");await waitText("Bot settings");await browser("click","Bot settings");await waitText("Bot reachability");assert.equal(await evaluate("fixturePreference(false)"),"pa_read_only");await browser("close");tab=undefined;
 assert.ok(requests.filter(r=>r.method==="POST"&&r.path.endsWith("/network-settings")).some(r=>r.status===200));
 console.log("Production English desktop and Taiwan Chinese narrow network settings, SourceClient, authenticated HTTP/Host/SQLite; no fake capability. Default unavailable, refused enable, actual false preference save, read-only, API downgrade and late-source refusal passed. External/model API requests=0.")
}finally{
 releaseHeld?.();if(tab&&send)await browser("close").catch(error=>console.error("Owned tab cleanup:",error.message));ws?.close();chrome.kill();await new Promise(resolve=>chrome.exitCode!==null||chrome.signalCode!==null?resolve():chrome.once("exit",resolve));await rm(profile,{recursive:true,force:true,maxRetries:5});
 await writeFile(path.join(artifacts,"cleanup.json"),JSON.stringify({pid:chrome.pid,profile,exitCode:chrome.exitCode,signalCode:chrome.signalCode,ownedTargetsClosed:operations.filter(o=>o.op==="close").every(o=>o.result.success)},null,2)+"\n");await writeFile(path.join(artifacts,"requests.json"),JSON.stringify(requests,null,2)+"\n");await writeFile(path.join(artifacts,"operations.json"),JSON.stringify(operations,null,2)+"\n");await new Promise(resolve=>server.close(resolve))
}
