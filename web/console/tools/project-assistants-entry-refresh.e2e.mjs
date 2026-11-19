/** Manual footer refresh restores the actual Cloud-style Bots host entry. */
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
  else if(op==="click")result=await evaluate(`(()=>{const label=${JSON.stringify(args[0])};const button=[...document.querySelectorAll("button")].find(b=>(b.getAttribute("aria-label")??b.textContent)===label);if(!button)throw Error("Missing button: "+label);const disabled=button.disabled;button.click();return {disabled}})()`)
  else if(op==="screenshot"){const {data}=await send("Page.captureScreenshot",{format:"jpeg"},session);await writeFile(args[1],Buffer.from(data,"base64"));result={captured:true}}
  else if(op==="close"){result=await send("Target.closeTarget",{targetId:tab});assert.equal(result.success,true)}
  else throw Error("Unsupported test operation: "+op)
  operations.push({op,args,result});return result
}
async function waitText(text,present=true){let last;for(let n=0;n<30;n++){last=await browser("read");if(last.text?.includes(text)===present)return last;await new Promise(r=>setTimeout(r,100))}throw Error(`Text ${text} present=${present}: ${JSON.stringify(last)}`)}

async function until(expression,label){for(let n=0;n<30;n++){if(await evaluate(expression))return;await new Promise(r=>setTimeout(r,50))}throw Error(label)}
try{
 await connect()
 for(const language of ["en","zh-Hant"]) for(const listMode of ["ready","failed","hold"]){
  const zh=language==="zh-Hant",refresh=zh?"更新額度":"Refresh quotas"
  await browser("open",origin+"/?feature=off&lang="+language+"&list="+listMode)
  await browser("viewport",zh?"mobile":"desktop")
  await until('!!window.fixtureHost&&!!document.querySelector(".quota-refresh")',"real status footer did not load")
  await until('window.fixtureHost.log.filter(r=>r.method==="status").length>=2',"initial source authority was not read")
  assert.equal(await evaluate('!!document.querySelector(".pa-orb,.pa-dock")'),false)
  await browser("screenshot","--out",path.join(artifacts,language+"-"+listMode+"-before.jpg"))
  if(zh){
   await evaluate('window.fixtureHost.quota("hold")')
   await browser("click",refresh)
   await until('window.fixtureHost.requests.filter(r=>r.path==="/v1/provider-quotas").length>=2',"quota hold did not start")
  }
  const before=await evaluate('window.fixtureHost.log.filter(r=>r.method==="status").length')
  const quotas=await evaluate('window.fixtureHost.requests.filter(r=>r.path==="/v1/provider-quotas").length')
  await evaluate('window.fixtureHost.enableFeature()')
  const clicked=await browser("click",refresh)
  assert.equal(clicked.disabled,false,"quota loading blocked recovery of Bots")
  await until('!!document.querySelector(".pa-orb,.pa-dock")',"manual refresh did not restore Bots before the background timer")
  assert.ok(await evaluate('window.fixtureHost.log.filter(r=>r.method==="status").length')>before,"refresh did not recheck Bot authority")
  if(zh)assert.equal(await evaluate('window.fixtureHost.requests.filter(r=>r.path==="/v1/provider-quotas").length'),quotas,"pending quota read duplicated")
  await browser("screenshot","--out",path.join(artifacts,language+"-"+listMode+"-restored.jpg"))
  if(listMode!=="ready") {
   await until('window.fixtureHost.log.some(r=>r.method==="list-unavailable")',"list failure/hold was not exercised")
   assert.equal(await evaluate('!!document.querySelector(".pa-orb,.pa-dock")'),true,"readable authority was hidden by an unavailable list")
   await evaluate('window.fixtureHost.listMode("ready")')
  }
  await evaluate('document.querySelector(".pa-orb,.pa-dock").click()')
  await until('!!document.querySelector(".pa-window button[data-bot-key]")',"restored entry did not open the actual roster")
  assert.deepEqual(await evaluate('({media:window.fixtureHost.stats().mediaOpens,sends:window.fixtureHost.stats().sessionSends})'),{media:0,sends:0})
  await evaluate('window.fixtureHost.quota("ready");window.fixtureHost.disabled()')
  await until('!document.querySelector(".pa-window,.pa-orb,.pa-dock")',"disabled source did not remove access")
  const deniedReads=await evaluate('window.fixtureHost.log.filter(r=>r.method==="status").length')
  await browser("click",refresh)
  await until(`window.fixtureHost.log.filter(r=>r.method==="status").length>${deniedReads}`,"disabled source was not rechecked")
  assert.equal(await evaluate('!!document.querySelector(".pa-orb,.pa-dock")'),false,"refresh granted access to a disabled source")
  const listReads=await evaluate('window.fixtureHost.log.filter(r=>r.method==="list-unavailable").length')
  await evaluate('window.fixtureHost.listMode("denied");window.fixtureHost.enableFeature()')
  await browser("click",refresh)
  await until(`window.fixtureHost.log.filter(r=>r.method==="list-unavailable").length>${listReads}`,"explicit list denial did not cross the source client")
  await until('!document.querySelector(".pa-orb,.pa-dock")',"explicit source denial retained entry access")
  results.push({language,listMode,explicitDenialHidden:true,entryRestored:true,actualRosterOpened:true,quotaPending:zh,disabledRemainsHidden:true,modelRequests:0})
  await browser("close");tab=undefined
 }
 await browser("open",origin+"/?feature=off&lang=zh-Hant")
 await browser("viewport","mobile")
 await until('window.fixtureHost?.log.filter(r=>r.method==="status").length>=2',"initial hidden status missing")
 await evaluate('window.fixtureHost.offline();window.fixtureHost.holdStatus();window.fixtureHost.enableFeature()')
 await browser("click","更新額度")
 await until('window.fixtureHost.log.some(r=>r.method==="status-wait")',"online source did not start")
 const started=await evaluate('window.fixtureHost.log.filter(r=>r.method==="status-wait").length')
 await browser("click","更新額度")
 await evaluate('new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve)))')
 assert.equal(await evaluate('window.fixtureHost.log.filter(r=>r.method==="status-wait").length'),started,"refresh restarted an outstanding status read")
 await evaluate('window.fixtureHost.releaseStatus()')
 await until('!!document.querySelector(".pa-orb")',"delayed online source failed to restore entry")
 await evaluate('localStorage.setItem("bots_orb_place",JSON.stringify({right:750,bottom:980,docked:false}));location.replace("/?lang=zh-Hant")')
 await until('!!document.querySelector(".pa-orb")',"saved orb not mounted")
 assert.ok(await evaluate('(()=>{const b=document.querySelector(".pa-orb").getBoundingClientRect();return b.left>=0&&b.top>=0&&b.right<=innerWidth&&b.bottom<=innerHeight})()'),"restored orb outside phone viewport")
 await evaluate('document.querySelector(".pa-orb").click()')
 await until('!!document.querySelector(".pa-window button[data-bot-key]")',"phone entry did not open roster")
 await browser("close");tab=undefined
 console.log(JSON.stringify(results))
}finally{
 if(tab&&send)await browser("close").catch(e=>console.error(e.message))
 ws?.close();chrome.kill();await new Promise(resolve=>chrome.exitCode!==null||chrome.signalCode!==null?resolve():chrome.once("exit",resolve));await rm(profile,{recursive:true,force:true,maxRetries:5})
 await writeFile(path.join(artifacts,"results.json"),JSON.stringify(results,null,2)+"\n");await writeFile(path.join(artifacts,"operations.json"),JSON.stringify(operations,null,2)+"\n");await new Promise(resolve=>server.close(resolve))
}
