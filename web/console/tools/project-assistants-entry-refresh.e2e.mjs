/** The Cloud-style App host opens Bots independently of source readiness. */
import assert from "node:assert/strict"
import {createServer} from "node:http"
import {mkdir,mkdtemp,readFile,writeFile,rm,access} from "node:fs/promises"
import {spawn} from "node:child_process"
import {constants} from "node:fs"
import path from "node:path"
import {hostBundle} from "./project-assistants-host.e2e.mjs"
const artifacts=process.env.BOTS_ARTIFACTS
assert.ok(artifacts,"BOTS_ARTIFACTS must name an owned evidence directory")
await mkdir(artifacts,{recursive:true})
const {html,files}=await hostBundle(),operations=[],results=[]
const server=createServer(async(req,res)=>{try{
 const url=new URL(req.url,"http://fixture")
 let body=url.pathname==="/"?html:files.get(url.pathname)
 if(/^\/catalogs\/(en|zh-Hant)\.json$/.test(url.pathname))body=await readFile(new URL("../public/catalogs/"+path.basename(url.pathname),import.meta.url))
 if(body===undefined){res.writeHead(404);res.end();return}
 res.setHeader("Content-Type",url.pathname.endsWith(".js")?"text/javascript":url.pathname.endsWith(".css")?"text/css":url.pathname.endsWith(".json")?"application/json":"text/html");res.end(body)
}catch(e){res.writeHead(500);res.end(String(e))}})
await new Promise(resolve=>server.listen(0,"127.0.0.1",resolve))
const origin=`http://127.0.0.1:${server.address().port}`
let tab
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
  else if(op==="click")result=await evaluate(`(()=>{const label=${JSON.stringify(args[0])};const button=[...document.querySelectorAll("button")].find(b=>(b.getAttribute("aria-label")??b.textContent)===label);if(!button)throw Error("Missing button: "+label);const disabled=button.disabled;button.click();return {disabled}})()`)
  else if(op==="screenshot"){const {data}=await send("Page.captureScreenshot",{format:"jpeg"},session);await writeFile(args[1],Buffer.from(data,"base64"));result={captured:true}}
  else if(op==="close"){result=await send("Target.closeTarget",{targetId:tab});assert.equal(result.success,true)}
  else throw Error("Unsupported test operation: "+op)
  operations.push({op,args,result});return result
}
async function until(expression,label){for(let n=0;n<90;n++){if(await evaluate(expression))return;await new Promise(r=>setTimeout(r,50))}throw Error(label)}
try{
 await connect()
 const hasWindow = '!!document.querySelector(".pa-window")'
 const hasRows = '!!document.querySelector(".pa-window button[data-bot-key]")'
 const unavailable = '!!document.querySelector(".pa-window .pa-roster-unavailable [role=status]")'
 for(const language of ["en","zh-Hant"]){
  const zh=language==="zh-Hant",orb=zh?"所有 Bots":"All Bots",collapse=zh?"收起 Bots":"Collapse Bots"
  for(const scenario of ["disabled","offline","unsupported","unpaired","held-list","failed-list","denied-list","revoked"]){
   const params=new URLSearchParams({lang:language})
   if(scenario==="disabled")params.set("feature","off")
   if(["offline","unsupported","unpaired"].includes(scenario))params.set("source",scenario)
   if(scenario.endsWith("-list"))params.set("list",scenario.split("-")[0]==="held"?"hold":scenario.split("-")[0])
   if(scenario==="revoked")params.set("hold-status","1")
   await browser("open",origin+"/?"+params)
   await browser("viewport",zh?"mobile":"desktop")
   await until('!!window.fixtureHost&&!!document.querySelector(".pa-dock")',"real status footer did not load")
   const clicked=await evaluate('(()=>{const button=document.querySelector(".pa-dock");button.click();return !button.disabled})()')
   assert.equal(clicked,true,scenario+" disabled the Bots dock")
   await until(hasWindow,scenario+" did not open the Bots window")
   if(scenario==="held-list"){
    await until('window.fixtureHost.log.some(r=>r.method==="list-unavailable")',"held list did not start")
    assert.equal(await evaluate(hasRows),false)
    await evaluate('window.fixtureHost.listMode("ready")')
    await until(hasRows,"held list did not populate the open Bots window")
   }else if(scenario==="revoked"){
    await until('window.fixtureHost.log.some(r=>r.method==="status-wait")',"status was not held")
    await evaluate('window.fixtureHost.releaseStatus()')
    await until(hasRows,"roster did not populate")
    await evaluate('document.querySelector(".pa-window button[data-bot-key]").click()')
    await until('!!document.querySelector(".pa-window .pa-chat-header")',"chat did not open")
    await evaluate('window.fixtureHost.registry.revoke()')
    await until(unavailable,"revocation did not remove the roster")
    assert.equal(await evaluate('!!document.querySelector(".pa-window .pa-chat-header")'),false,"revocation kept the chat")
   }else{
    await until(unavailable,scenario+" did not show the unavailable roster")
    assert.equal(await evaluate(hasRows),false)
   }
   if(scenario==="failed-list"){
    await evaluate('window.fixtureHost.listMode("ready")')
    await until(hasRows,"the open roster did not recover after a transient failure")
   }
   assert.equal(await evaluate(hasWindow),true,scenario+" closed the Bots window")
   await browser("screenshot","--out",path.join(artifacts,language+"-"+scenario+".jpg"))
   await browser("click",collapse)
   await until('!document.querySelector(".pa-window")',"the Bots window did not collapse")
   await until('document.activeElement?.matches(".pa-dock:enabled")',"focus was not restored to the usable Bots dock")
   assert.deepEqual(await evaluate('({media:window.fixtureHost.stats().mediaOpens,sends:window.fixtureHost.stats().sessionSends})'),{media:0,sends:0})
   results.push({language,scenario,passed:true})
   await browser("close");tab=undefined
  }
  await browser("open",origin+"/?lang="+language+"&feature=off#bot=bot-0&conversation=chat-0&machine=studio")
  await browser("viewport",zh?"mobile":"desktop")
  await until(hasWindow,"the deep link did not open Bots before source authority")
  await until(unavailable,"the unauthorized deep link did not show the unavailable roster")
  assert.equal(await evaluate(hasRows),false)
  await browser("click",collapse)
  await until('!document.querySelector(".pa-window")',"the deep-link window did not collapse")
  assert.equal((await browser("click",orb)).disabled,false)
  await until(hasWindow,"the dock did not reopen Bots after closing a deep link")
  results.push({language,scenario:"deep-link",passed:true})
  await browser("close");tab=undefined
 }
 // The footer can still recheck badge authority while quota and status reads are held.
 await browser("open",origin+"/?feature=off&lang=zh-Hant")
 await browser("viewport","mobile")
 await until('window.fixtureHost?.log.filter(r=>r.method==="status").length>=2',"initial source status was not read")
 await evaluate('window.fixtureHost.quota("hold")')
 await browser("click","更新額度")
 await until('window.fixtureHost.requests.filter(r=>r.path==="/v1/provider-quotas").length>=2',"quota read was not held")
 const quotas=await evaluate('window.fixtureHost.requests.filter(r=>r.path==="/v1/provider-quotas").length')
 await evaluate('window.fixtureHost.holdStatus();window.fixtureHost.enableFeature()')
 assert.equal((await browser("click","更新額度")).disabled,false)
 await until('window.fixtureHost.log.filter(r=>r.method==="status-wait").length===2',"refresh did not recheck both sources")
 await browser("click","更新額度")
 await evaluate('new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve)))')
 assert.equal(await evaluate('window.fixtureHost.log.filter(r=>r.method==="status-wait").length'),2,"refresh restarted an outstanding status read")
 assert.equal(await evaluate('window.fixtureHost.requests.filter(r=>r.path==="/v1/provider-quotas").length'),quotas,"refresh duplicated a pending quota read")
 await evaluate('window.fixtureHost.releaseStatus()')
 await until('window.fixtureHost.log.some(r=>r.method==="list")',"badge list did not resume after source status")
 await evaluate('localStorage.setItem("bots_orb_place",JSON.stringify({right:750,bottom:980,docked:false}));location.replace("/?lang=zh-Hant")')
 await until('!!document.querySelector(".pa-dock:enabled")',"old preference prevented the dock from opening")
 assert.ok(await evaluate('(()=>{const b=document.querySelector(".pa-dock").getBoundingClientRect();return b.left>=0&&b.top>=0&&b.right<=innerWidth&&b.bottom<=innerHeight})()'),"dock outside phone viewport")
 await browser("close");tab=undefined
 results.push({language:"zh-Hant",scenario:"footer-refresh",passed:true})
 await browser("open",origin+"/?lang=zh-Hant")
 await browser("viewport","mobile")
 await send("Emulation.setTouchEmulationEnabled",{enabled:true,maxTouchPoints:1},session)
 await until('!!document.querySelector(".pa-dock:enabled")',"Bots dock did not load for gesture")
 await browser("click","所有 Bots")
 const rows = '[...document.querySelectorAll(".pa-roster-scroll [data-bot-key]")].map(row=>row.dataset.botKey).sort()'
 await until(`new Set((${rows}).map(key=>JSON.parse(key)[0])).size===2`,"both fixture machines must have visible Bots")
 const originalRows=await evaluate(rows)
 await evaluate('window.fixtureHost.holdStatus();window.fixtureHost.listMode("hold")')
 const start=await evaluate('(()=>{const rect=document.querySelector(".pa-roster-scroll").getBoundingClientRect();return {x:Math.round(rect.left+rect.width/2),y:Math.round(rect.top+16)}})()')
 const touch=(type,touchPoints=[])=>send("Input.dispatchTouchEvent",{type,touchPoints},session)
 const pull=async()=>{
  await touch("touchStart",[start])
  await touch("touchMove",[{...start,y:start.y+45}])
  await touch("touchMove",[{...start,y:start.y+190}])
  await touch("touchEnd")
 }
 await pull()
 await until('document.querySelector(".pa-roster-scroll .ptr")?.style.height === "34px"',"releasing a pull did not show the refresh indicator")
 await until('new Set(window.fixtureHost.log.filter(row=>row.method==="list-unavailable").map(row=>row.machine)).size===2',"pull did not begin a roster read for each machine")
 assert.deepEqual(await evaluate(rows),originalRows,"existing lists vanished while refreshing")
 const pending=await evaluate('window.fixtureHost.log.filter(row=>row.method==="list-unavailable").length')
 await pull()
 assert.equal(await evaluate('window.fixtureHost.log.filter(row=>row.method==="list-unavailable").length'),pending,"a second pull duplicated the pending reads")
 await evaluate('window.fixtureHost.listMode("ready");window.fixtureHost.releaseStatus()')
 await until('document.querySelector(".pa-roster-scroll .ptr")?.style.height === "0px"',"the refresh indicator outlived the roster reads")
 assert.deepEqual(await evaluate(rows),originalRows)
 assert.equal(await evaluate(hasWindow),true,"the gesture closed the Bots window")
 await browser("close");tab=undefined
 results.push({language:"zh-Hant",scenario:"bots-pull-to-refresh",passed:true})
 console.log(JSON.stringify(results))
}finally{
 if(tab&&send)await browser("close").catch(e=>console.error(e.message))
 ws?.close();chrome.kill();await new Promise(resolve=>chrome.exitCode!==null||chrome.signalCode!==null?resolve():chrome.once("exit",resolve));await rm(profile,{recursive:true,force:true,maxRetries:5})
 await writeFile(path.join(artifacts,"results.json"),JSON.stringify(results,null,2)+"\n");await writeFile(path.join(artifacts,"operations.json"),JSON.stringify(operations,null,2)+"\n");await new Promise(resolve=>server.close(resolve))
}
