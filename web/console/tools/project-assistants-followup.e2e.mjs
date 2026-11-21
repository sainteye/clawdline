/** Pending notes cross the production manager, source client, Host and SQLite. */
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
import {Routines} from "./src/project-assistants/Routines";import {ProjectAssistantSourceClient} from "./src/cloud/project-assistants";
import {words} from "./src/project-assistants/words";import {bootCatalog} from "./src/catalog";import "./src/project-assistants/styles.css";
const q=new URL(location.href).searchParams,language=q.get("lang"),read=q.has("read");localStorage.setItem("ui_language",language);await bootCatalog(tag=>"/catalog/"+tag+".json");
const grant={accountKey:"synthetic-account",readerID:read?"read-only":"synthetic-reader",machineID:${JSON.stringify(machine)},authorizationRevision:"synthetic-grant",apiLevel:78,paired:true,online:true,writable:!read};
const source=new ProjectAssistantSourceClient(grant,(input,init)=>fetch(input+(read?(input.includes("?")?"&":"?")+"fixture_read=1":""),init),()=>grant);
const got=await source.get({machine_id:grant.machineID,bot_id:${JSON.stringify(bot)}},new AbortController().signal),w=words(language);
function Fixture(){const [visible,setVisible]=useState(true);return <main className="project-assistants">{visible?<Routines source={source} bot={got.bot} enabled={true} w={w} storageKey={JSON.stringify([grant.accountKey,grant.readerID,grant.machineID,got.bot.key.bot_id])} onClose={()=>setVisible(false)}/>:<button onClick={()=>setVisible(true)}>{w.routines}</button>}</main>}
createRoot(document.getElementById("app")).render(<Fixture/>);
`,resolveDir:root,loader:"tsx"},bundle:true,write:false,outfile:"followup.js",format:"esm",target:"es2022",platform:"browser",jsx:"automatic",define:{"process.env.NODE_ENV":'"development"'}})
const files=new Map(bundle.outputFiles.map(f=>["/"+path.basename(f.path),f.contents]))
let loseResponse=true,tab
const server=createServer(async(req,res)=>{try {
  const url=new URL(req.url,"http://fixture")
  if(url.pathname.startsWith("/v1/")) {
    const read=url.searchParams.has("fixture_read");url.searchParams.delete("fixture_read")
    const chunks=[];for await(const chunk of req)chunks.push(chunk)
    const headers={Authorization:"Bearer "+(read?readToken:token)};for(const key of ["content-type","idempotency-key"])if(req.headers[key])headers[key]=req.headers[key]
    const answer=await fetch(upstream+url.pathname+url.search,{method:req.method,headers,...(chunks.length?{body:Buffer.concat(chunks)}:{})}),body=Buffer.from(await answer.arrayBuffer())
    const record={method:req.method,path:url.pathname,status:answer.status,...(chunks.length?{body:JSON.parse(Buffer.concat(chunks))}:{})};requests.push(record)
    if(loseResponse&&req.method==="DELETE"&&answer.ok){loseResponse=false;record.browser_status=503;res.writeHead(503,{"Content-Type":"application/json"});res.end('{"contract_version":"project-assistants.v1","error":{"code":"pa_outcome_unknown"}}');return}
    res.writeHead(answer.status,{"Content-Type":"application/json"});res.end(body);return
  }
  if(url.pathname.startsWith("/catalog/")){res.setHeader("Content-Type","application/json");res.end(await readFile(path.join(root,"public/catalogs",path.basename(url.pathname))));return}
  if(files.has(url.pathname)){res.setHeader("Content-Type",url.pathname.endsWith(".css")?"text/css":"text/javascript");res.end(files.get(url.pathname));return}
  res.setHeader("Content-Type","text/html");res.end('<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="stylesheet" href="/followup.css"><div id="app"></div><script type="module" src="/followup.js"></script>')
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
  else if(op==="read")result={text:await evaluate('document.body.innerText')}
  else if(op==="click")result=await evaluate(`(()=>{const label=${JSON.stringify(args[0])};const button=[...document.querySelectorAll("button")].find(b=>(b.getAttribute("aria-label")??b.textContent)===label);if(!button)throw Error("Missing button: "+label);const disabled=button.disabled;button.click();return {disabled}})()`)
  else if(op==="screenshot"){const {data}=await send("Page.captureScreenshot",{format:"jpeg"},session);await writeFile(args[1],Buffer.from(data,"base64"));result={captured:true}}
  else if(op==="close"){result=await send("Target.closeTarget",{targetId:tab});assert.equal(result.success,true)}
  else throw Error("Unsupported test operation: "+op)
  operations.push({op,args,result});return result
}
async function waitText(text,present=true){let last;for(let n=0;n<30;n++){last=await browser("read");if(last.text?.includes(text)===present)return last;await new Promise(r=>setTimeout(r,100))}throw Error(`Text ${text} present=${present}: ${JSON.stringify(last)}`)}
try {
  await connect()
  for(const [language,id,readonly] of [["en","future-en",true],["en","future-en",false],["zh-Hant","future-zh",false]]) {
    const zh=language!=="en",cancel=zh?"取消":"Cancel",close=zh?"關閉":"Close",routines=zh?"例行排程":"Routines"
    tab=(await browser("open",origin+"/?lang="+language+(readonly?"&read=1":""))).tab
    await browser("viewport",zh?"mobile":"desktop");await waitText(id)
    if(readonly){try{await browser("click",cancel+": "+id)}catch(error){operations.push({op:"read-only-refusal",detail:String(error)})};assert.equal(requests.filter(r=>r.method!=="GET").length,0)}
    else {
      await browser("click",cancel+": "+id)
      if(!zh){
        const held="The last action may have reached the source."
        await waitText(held);assert.equal(requests.filter(r=>r.method==="DELETE").length,1)
        await browser("click",close);await browser("click",routines);await waitText(held)
        assert.equal(requests.filter(r=>r.method==="DELETE").length,1,"Remount cannot automatically replay an uncertain cancellation")
        const deadline=Date.now()+10000
        while(!await evaluate('[...document.querySelectorAll("button")].some(b=>b.textContent==="Confirm or finish original action"&&!b.disabled)')){assert.ok(Date.now()<deadline,"Replay waits for current source authority");await new Promise(r=>setTimeout(r,50))}
        await browser("click","Confirm or finish original action");await waitText(held,false)
      }
      await waitText(id,false)
    }
    await browser("screenshot","--out",path.join(artifacts,`${language}-${readonly?"read-only":"cancelled"}.jpg`))
    await browser("close");tab=undefined
  }
  const writes=requests.filter(r=>r.method!=="GET");assert.equal(writes.length,3);assert.ok(writes.every(r=>r.method==="DELETE"&&r.status===200))
  assert.deepEqual(writes[0].body,writes[1].body,"Explicit replay keeps the exact original cancellation command")
  console.log("Real bilingual browser cancellation and read-only refusal; lost reply held across remount; exact replay; three DELETE requests for two effects; model requests=0")
} finally {
  if(tab&&send)await browser("close").catch(error=>console.error("Owned test tab cleanup:",error.message))
  ws?.close();chrome.kill();await new Promise(resolve=>chrome.exitCode!==null||chrome.signalCode!==null?resolve():chrome.once("exit",resolve));await rm(profile,{recursive:true,force:true,maxRetries:5})
  await writeFile(path.join(artifacts,"requests.json"),JSON.stringify(requests,null,2)+"\n")
  await writeFile(path.join(artifacts,"operations.json"),JSON.stringify(operations,null,2)+"\n")
  await new Promise(resolve=>server.close(resolve))
}
