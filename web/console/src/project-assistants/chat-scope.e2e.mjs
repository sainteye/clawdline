/** Actual HTTP/SQLite projections in the production Bots UI; no model calls. */
import assert from "node:assert/strict"
import {createServer} from "node:http"
import {readFile,mkdir,writeFile} from "node:fs/promises"
import {fileURLToPath,pathToFileURL} from "node:url"
import path from "node:path"
import {build} from "esbuild"

const root=fileURLToPath(new URL("../..",import.meta.url))
const {BOTS_ORIGIN:upstream,BOTS_CLOUD_ORIGIN:cloud,BOTS_ARTIFACTS:artifacts,PLAYWRIGHT_MODULE:module}=process.env
assert.ok(upstream&&cloud&&artifacts&&module,"requires the synthetic HTTP fixture and installed Playwright")
await mkdir(artifacts,{recursive:true})
const bundle=await build({stdin:{resolveDir:root,loader:"tsx",contents:`
import React from "react";import {createRoot} from "react-dom/client";
import {Bots} from "./src/project-assistants/Bots";
import {ProjectAssistantSourceClient,capturedAssistantRelayFetch} from "./src/cloud/project-assistants";
import {bootCatalog} from "./src/catalog";
import "./src/legacy/tokens.css";import "./src/project-assistants/styles.css";
const q=new URL(location.href).searchParams,mode=q.get("mode"),language=q.get("lang");
localStorage.setItem("ui_language",language);await bootCatalog(tag=>"/catalog/"+tag+".json");
const words=["status","list","get","view","read","observe","export","groups","group-history","team-plans","team-settings"].map(w=>"project-assistants."+w);
const grant={accountKey:"synthetic-scope-"+mode,readerID:"person",machineID:"machine",authorizationRevision:"one",apiLevel:89,paired:true,online:true,writable:true};
const relay={machineDescriptor:()=>({machine:{commands:words}}),_machineRequest:async(_m,w,fields)=>await(await fetch("/relay/"+w,{method:"POST",body:JSON.stringify(fields)})).json(),_machineRequestAs:async(id,_m,w,fields)=>await(await fetch("/relay/"+w,{method:"POST",headers:{"Idempotency-Key":id},body:JSON.stringify(fields)})).json()};
const source=new ProjectAssistantSourceClient(grant,mode==="cloud"?capturedAssistantRelayFetch("machine",()=>relay,words):(input,init)=>fetch("/proxy"+input,init),()=>grant);source.canRoster=()=>false;
createRoot(document.getElementById("app")).render(<Bots sources={[source]} language={language} accountKey={grant.accountKey} readerID="person" onClose={()=>{}}/>);
`},bundle:true,write:false,outfile:"scope.js",format:"esm",platform:"browser",target:"es2022",jsx:"automatic",define:{"process.env.NODE_ENV":"\"development\""}})
const files=new Map(bundle.outputFiles.map(f=>["/"+path.basename(f.path),f.contents])),traffic=[]
const server=createServer(async(req,res)=>{try{
 const u=new URL(req.url,"http://fixture")
 if(u.pathname.startsWith("/proxy/")||u.pathname.startsWith("/relay/")){
  const chunks=[];for await(const chunk of req)chunks.push(chunk);const body=Buffer.concat(chunks),relay=u.pathname.startsWith("/relay/")
  const record={path:u.pathname+u.search,relay};traffic.push(record)
  const answer=relay?await fetch(cloud,{method:"POST",body:JSON.stringify({type:u.pathname.slice(7),session:"__clawdline_machine__",request:req.headers["idempotency-key"]??crypto.randomUUID(),...JSON.parse(body.toString()||"{}")})}):await fetch(upstream+u.pathname.slice(6)+u.search,{method:req.method,headers:{Authorization:"Bearer person",...(body.length?{"Content-Type":"application/json","Idempotency-Key":req.headers["idempotency-key"]??""}:{})},...(body.length?{body}:{})})
  const result=await answer.json();record.status=answer.status;record.result=relay?result.body??result:result
  res.writeHead(answer.status,{"Content-Type":"application/json"});res.end(JSON.stringify(record.result));return
 }
 if(u.pathname.startsWith("/catalog/")){res.setHeader("Content-Type","application/json");res.end(await readFile(path.join(root,"public/catalogs",path.basename(u.pathname))));return}
 if(files.has(u.pathname)){res.setHeader("Content-Type",u.pathname.endsWith(".css")?"text/css":"text/javascript");res.end(files.get(u.pathname));return}
 res.setHeader("Content-Type","text/html");res.end('<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><style>html,body,#app{height:100%;margin:0}</style><link rel="stylesheet" href="/scope.css"><div id="app"></div><script type="module" src="/scope.js"></script>')
}catch(e){res.writeHead(500);res.end(String(e))}})
await new Promise(r=>server.listen(0,"127.0.0.1",r))
const origin="http://127.0.0.1:"+server.address().port,pw=await import(pathToFileURL(module).href),evidence=[]
try{
 for(const engine of (process.env.BOTS_ENGINES??"chromium,webkit").split(",")){
  const executablePath=process.env[engine==="chromium"?"CHROME":"WEBKIT"]
  const browser=await pw[engine].launch({headless:true,...executablePath?{executablePath}:{},...engine==="chromium"?{args:["--disable-features=OmniboxEverywhere"]}:{}})
  try{for(const [mode,lang,width] of [["local","en",1440],["cloud","zh-Hant",390]]){
   const context=await browser.newContext({viewport:{width,height:800}}),page=await context.newPage(),errors=[]
   page.on("pageerror",e=>errors.push(String(e)))
   try{
    await page.goto(origin+"/?"+new URLSearchParams({mode,lang}))
    const choose=async(name)=>{await page.locator("[data-bot-key]").filter({has:page.locator("strong",{hasText:new RegExp("^"+name+"$")})}).click();await page.locator(".pa-main .pa-chat-header h2").filter({hasText:name}).waitFor();await page.waitForFunction(()=>!!document.querySelector('.pa-main textarea:not(:disabled)')&&!document.querySelector('.pa-main .pa-chat-skeleton'))}
    await choose("Bob")
    assert.equal(await page.locator(".pa-main [data-message-id]").count(),0,"room-only member has a DM transcript")
    assert.equal(await page.locator(".pa-main [data-trace-id]").count(),0,"room tool entered DM")
    await page.screenshot({path:path.join(artifacts,engine+"-"+mode+"-empty-dm.png")})
    if(width<700)await page.locator(".pa-main .pa-chat-back").click()
    await choose("Alice")
    await page.locator(".pa-main [data-message-id]").filter({hasText:"Private direct message"}).waitFor()
    assert.equal(await page.locator(".pa-main [data-message-id]").count(),2)
    const text=await page.locator(".pa-main").innerText();assert.ok(text.includes("Direct answer"));assert.ok(!text.includes("Room answer")&&!text.includes("Room A")&&!text.includes("Room B")&&!text.includes("It's your turn"))
    await page.reload();await choose("Alice");await page.locator(".pa-main [data-message-id]").filter({hasText:"Private direct message"}).waitFor();assert.equal(await page.locator(".pa-main [data-message-id]").count(),2)
    await page.screenshot({path:path.join(artifacts,engine+"-"+mode+"-real-dm.png")})
    assert.deepEqual(errors,[]);evidence.push({engine,mode,lang,width,roomOnlyDM:0,realDM:2,reload:true,modelCalls:0})
   }finally{await context.close()}
  }}finally{await browser.close()}
 }
 await writeFile(path.join(artifacts,"ui-scope.json"),JSON.stringify({evidence,traffic},null,2));console.log(JSON.stringify(evidence))
}finally{server.closeAllConnections();await new Promise(r=>server.close(r))}
