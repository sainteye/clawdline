/** The production Board edit dialog reaches its two conflict choices against a real work route and store. */
// Run by TestBoardEditConflictBrowser with BOARD_* set. CHROME names the browser binary.
import assert from "node:assert/strict"
import {spawn} from "node:child_process"
import {createServer} from "node:http"
import {mkdir,mkdtemp,readFile,rm,writeFile} from "node:fs/promises"
import {tmpdir} from "node:os"
import {fileURLToPath} from "node:url"
import path from "node:path"
import {build} from "esbuild"

const {BOARD_ORIGIN:upstream,BOARD_ITEM:item,BOARD_ARTIFACTS:artifacts}=process.env
assert.ok(upstream&&item&&artifacts)
const chrome=process.env.CHROME||"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
await mkdir(artifacts,{recursive:true})
const root=fileURLToPath(new URL("..",import.meta.url))
// The console's own stylesheets, in the order main.tsx loads them.
const styles=(await readFile(path.join(root,"src/main.tsx"),"utf8")).split("\n").filter(line=>/^import "\.\/.*\.css"$/.test(line)).map(line=>line.replace('"./','"./src/')).join("\n")

const bundle=await build({stdin:{contents:`
import React,{useState} from "react";import {createRoot} from "react-dom/client";
${styles}
import "./src/pages/work/work.css";
import {RefusalError} from "@clawdline/core";
import {EditWorkModal} from "./src/pages/work/WorkV2";import {editWorkV2,readWorkV2Item} from "./src/pages/work/api";
import {bootCatalog} from "./src/catalog";
window.addEventListener("error",e=>{window.fixtureError=e.message});window.addEventListener("unhandledrejection",e=>{window.fixtureError=String(e.reason)});
const q=new URL(location.href).searchParams;localStorage.setItem("ui_language",q.get("lang"));await bootCatalog(tag=>"/catalog/"+tag+".json");
const id=${JSON.stringify(item)},first=(await readWorkV2Item(id)).item;
function Fixture(){const [item,setItem]=useState(first),[busy,setBusy]=useState(false),[failure,setFailure]=useState("");
  // Another device saves a different title while this dialog is open.
  window.fixtureElsewhere=async title=>{const live=(await readWorkV2Item(id)).item;setItem((await editWorkV2(live,title,live.description)).item)};
  return <EditWorkModal item={item} busy={busy} failure={failure} onClose={()=>{}} onSave={(title,description,base,overwrite,conflicted)=>{setBusy(true);
    editWorkV2(item,title,description,undefined,{base,overwrite,apiLevel:117}).then(saved=>setItem(saved.item))
      .catch(e=>{if(e instanceof RefusalError&&e.code==="field_conflict")conflicted();else setFailure(String(e))}).finally(()=>setBusy(false))}}/>}
createRoot(document.getElementById("app")).render(<Fixture/>);
const until=async find=>{for(let i=0;i<200;i++){const got=find();if(got)return got;await new Promise(r=>setTimeout(r,25))}throw Error("the dialog did not get there")};
const input=await until(()=>document.querySelector(".work-edit-modal input.work-input"));
Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,"value").set.call(input,q.get("draft"));input.dispatchEvent(new Event("input",{bubbles:true}));
await window.fixtureElsewhere(q.get("elsewhere"));
(await until(()=>document.querySelector(".work-edit-modal button[type=submit]:not([disabled])"))).click();
const choices=await until(()=>document.querySelector(".work-edit-conflict"));
await fetch("/fixture-state",{method:"POST",body:JSON.stringify({view:q.get("view"),alert:document.querySelector(".work-edit-modal [role=alert]").textContent,choices:[...choices.querySelectorAll("button")].map(b=>b.textContent)})});
document.documentElement.dataset.ready="1";
`,resolveDir:root,loader:"tsx"},bundle:true,external:["/*.webp","/*.jpg"],write:false,outfile:"board-edit-conflict.js",format:"esm",target:"es2022",platform:"browser",jsx:"automatic",
  loader:{".svg":"dataurl",".png":"dataurl",".woff2":"dataurl",".woff":"dataurl",".ttf":"dataurl"},define:{"process.env.NODE_ENV":'"development"',"import.meta.env":"{}"}})
const files=new Map(bundle.outputFiles.map(f=>["/"+path.basename(f.path),f.contents]))
const requests=[],states=[]
const server=createServer(async(req,res)=>{try{
  const url=new URL(req.url,"http://fixture"),chunks=[];for await(const chunk of req)chunks.push(chunk)
  if(url.pathname==="/fixture-state"){states.push(JSON.parse(Buffer.concat(chunks)));res.end("{}");return}
  if(url.pathname.startsWith("/v1/")){
    const headers={};for(const key of ["content-type","idempotency-key"])if(req.headers[key])headers[key]=req.headers[key]
    const answer=await fetch(upstream+url.pathname+url.search,{method:req.method,headers,...(chunks.length?{body:Buffer.concat(chunks)}:{})}),body=Buffer.from(await answer.arrayBuffer())
    if(req.method==="PATCH")requests.push({status:answer.status,body:JSON.parse(Buffer.concat(chunks)),answer:JSON.parse(body)})
    res.writeHead(answer.status,{"Content-Type":"application/json"});res.end(body);return
  }
  if(url.pathname.startsWith("/catalog/")){res.setHeader("Content-Type","application/json");res.end(await readFile(path.join(root,"public/catalogs",path.basename(url.pathname))));return}
  if(files.has(url.pathname)){res.setHeader("Content-Type",url.pathname.endsWith(".css")?"text/css":"text/javascript");res.end(files.get(url.pathname));return}
  res.setHeader("Content-Type","text/html");res.end('<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="stylesheet" href="/board-edit-conflict.css"><body><div id="app"></div><script type="module" src="/board-edit-conflict.js"></script>')
}catch(error){res.writeHead(500);res.end(String(error))}})
await new Promise(resolve=>server.listen(0,"127.0.0.1",resolve))
const origin=`http://127.0.0.1:${server.address().port}`
const profile=await mkdtemp(path.join(tmpdir(),"board-edit-conflict-"))
const browser=spawn(chrome,["--headless=new","--disable-features=OmniboxEverywhere","--disable-gpu","--hide-scrollbars","--no-first-run","--use-mock-keychain","--remote-debugging-port=0",`--user-data-dir=${profile}`,"about:blank"],{stdio:"ignore"})
try{
  let port=""
  for(let i=0;i<200&&!port;i++){port=(await readFile(path.join(profile,"DevToolsActivePort"),"utf8").catch(()=>"")).split("\n")[0];if(!port)await new Promise(r=>setTimeout(r,50))}
  assert.ok(port,"the browser did not open its DevTools port")
  for(const [view,width,height] of [["mobile",390,844],["desktop",1280,800]]){
    const target=await(await fetch(`http://127.0.0.1:${port}/json/new?about:blank`,{method:"PUT"})).json()
    const socket=new WebSocket(target.webSocketDebuggerUrl),waiting=new Map(),problems=[];let serial=0
    await new Promise((resolve,reject)=>{socket.onopen=resolve;socket.onerror=reject})
    socket.onmessage=event=>{const message=JSON.parse(event.data),done=waiting.get(message.id);if(done){waiting.delete(message.id);done(message)}
      else if(message.method==="Runtime.exceptionThrown")problems.push(message.params.exceptionDetails.exception?.description||message.params.exceptionDetails.text)
      else if(message.method==="Log.entryAdded"&&message.params.entry.level==="error")problems.push(message.params.entry.text+" "+(message.params.entry.url||""))}
    const send=(method,params={})=>new Promise((resolve,reject)=>{const id=++serial;waiting.set(id,message=>message.error?reject(Error(method+": "+message.error.message)):resolve(message.result));socket.send(JSON.stringify({id,method,params}))})
    await send("Runtime.enable");await send("Log.enable")
    await send("Emulation.setDeviceMetricsOverride",{width,height,deviceScaleFactor:2,mobile:view==="mobile"})
    await send("Page.navigate",{url:`${origin}/?lang=zh-Hant&view=${view}&draft=${encodeURIComponent("我在這裡改的標題（"+view+"）")}&elsewhere=${encodeURIComponent("另一台裝置存的標題（"+view+"）")}`})
    let ready=false
    for(let i=0;i<200&&!ready;i++){ready=(await send("Runtime.evaluate",{expression:"document.documentElement.dataset.ready==='1'"})).result.value===true;if(!ready)await new Promise(r=>setTimeout(r,100))}
    const seen=(await send("Runtime.evaluate",{expression:"document.body.innerText.slice(0,600)+' | '+(window.fixtureError||'')"})).result.value+" | "+problems.join(" | ")
    assert.ok(ready,`the ${view} dialog never showed its conflict choices: ${seen}`)
    const state=states.find(s=>s.view===view)
    assert.equal(state.choices.length,2)
    const shot=await send("Page.captureScreenshot",{format:"png"})
    await writeFile(path.join(artifacts,`board-edit-conflict-${view}-${width}x${height}.png`),Buffer.from(shot.data,"base64"))
    socket.close()
  }
  await writeFile(path.join(artifacts,"board-edit-conflict.json"),JSON.stringify({states,requests},null,2))
  // Both viewports edit the same item in turn, so each ends in one refused save.
  assert.equal(requests.filter(r=>r.status>=400&&JSON.stringify(r.answer).includes("field_conflict")).length,2)
  console.log(JSON.stringify({states}))
}finally{browser.kill();server.close();await new Promise(r=>setTimeout(r,300));await rm(profile,{recursive:true,force:true,maxRetries:5})}
