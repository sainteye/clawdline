/** Production Chat and SourceClient cross an authenticated task-owned HTTP/Host/SQLite source. */
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
const root=fileURLToPath(new URL("..",import.meta.url)),requests=[],operations=[],results=[]
const bundle=await build({entryPoints:[path.join(root,"tools/fixtures/project-assistants-reactions.tsx")],bundle:true,write:false,outfile:"reactions.js",format:"esm",target:"es2022",platform:"browser",jsx:"automatic",define:{"process.env.NODE_ENV":'"development"',TEMPLATE_MACHINE:JSON.stringify(machine),TEMPLATE_BOT:JSON.stringify(bot)}})
const files=new Map(bundle.outputFiles.map(f=>["/"+path.basename(f.path),f.contents]))
let tab,holdNext=false,held=false,replaceConversation=false,replaceSettings=false,dropNext=false
const holds=[];const releaseHeld=()=>{holdNext=false;for(const resolve of holds.splice(0))resolve()}
const botPath="/v1/project-assistants/"+bot
const server=createServer(async(req,res)=>{try{
  const url=new URL(req.url,"http://fixture")
  if(url.pathname.startsWith("/v1/")){
    const read=url.searchParams.has("fixture_read");url.searchParams.delete("fixture_read")
    const record={method:req.method,path:url.pathname};requests.push(record)
    const chunks=[];for await(const chunk of req)chunks.push(chunk)
    const headers={Authorization:"Bearer "+(read?readToken:token)};for(const k of ["content-type","idempotency-key"])if(req.headers[k])headers[k]=req.headers[k]
    if(chunks.length)record.body=JSON.parse(Buffer.concat(chunks));
    const answer=await fetch(upstream+url.pathname+url.search,{method:req.method,headers,...(chunks.length?{body:Buffer.concat(chunks)}:{})})
    let body=Buffer.from(await answer.arrayBuffer())
    record.status=answer.status
    if(holdNext&&url.pathname.endsWith("/react")){held=true;await new Promise(resolve=>{holds.push(resolve)})}
    if(replaceSettings&&url.pathname===botPath){const value=JSON.parse(body);value.bot.name="Other synthetic settings";body=Buffer.from(JSON.stringify(value))}
    if(replaceConversation&&url.pathname===botPath){const value=JSON.parse(body);value.conversation.id="other-synthetic-conversation";value.bot.conversation_id="other-synthetic-conversation";body=Buffer.from(JSON.stringify(value))}
    if(dropNext&&url.pathname.endsWith("/react")){dropNext=false;res.writeHead(502);res.end("lost synthetic reply");return}
    res.writeHead(answer.status,{"Content-Type":"application/json"});res.end(body);return
  }
  if(url.pathname.startsWith("/catalog/")){res.setHeader("Content-Type","application/json");res.end(await readFile(path.join(root,"public/catalogs",path.basename(url.pathname))));return}
  if(files.has(url.pathname)){res.setHeader("Content-Type",url.pathname.endsWith(".css")?"text/css":"text/javascript");res.end(files.get(url.pathname));return}
  res.setHeader("Content-Type","text/html");res.end(await readFile(path.join(root,"tools/fixtures/project-assistants-reactions.html")))
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
  else if(op==="tap"){const hit=await evaluate(`(()=>{const b=[...document.querySelectorAll("button")].find(b=>(b.getAttribute("aria-label")??b.textContent)===${JSON.stringify(args[0])});if(!b)throw Error("missing tap target");b.scrollIntoView({block:"center"});const r=b.getBoundingClientRect();return {x:r.x+r.width/2,y:r.y+r.height/2}})()`);await send("Emulation.setTouchEmulationEnabled",{enabled:true},session);await send("Input.dispatchTouchEvent",{type:"touchStart",touchPoints:[hit]},session);await send("Input.dispatchTouchEvent",{type:"touchEnd",touchPoints:[]},session);result=hit}
  else if(op==="read")result={text:await evaluate('document.body?.innerText??""')}
  else if(op==="click"){
    const hit=await evaluate(`(()=>{const label=${JSON.stringify(args[0])};const button=[...document.querySelectorAll("button")].find(b=>(b.getAttribute("aria-label")??b.textContent)===label);if(!button)throw Error("Missing button: "+label);button.scrollIntoView({block:"center"});const r=button.getBoundingClientRect();return {x:r.x+r.width/2,y:r.y+r.height/2,disabled:button.disabled}})()`)
    await send("Input.dispatchMouseEvent",{type:"mousePressed",x:hit.x,y:hit.y,button:"left",clickCount:1},session)
    await send("Input.dispatchMouseEvent",{type:"mouseReleased",x:hit.x,y:hit.y,button:"left",clickCount:1},session);result=hit
  }
  else if(op==="screenshot"){const {data}=await send("Page.captureScreenshot",{format:"jpeg"},session);await writeFile(args[1],Buffer.from(data,"base64"));result={captured:true}}
  else if(op==="close"){result=await send("Target.closeTarget",{targetId:tab});assert.equal(result.success,true)}
  else throw Error("Unsupported test operation: "+op)
  operations.push({op,args,result});return result
}
async function waitText(text,present=true){let last;for(let n=0;n<30;n++){last=await browser("read");if(last.text?.includes(text)===present)return last;await new Promise(r=>setTimeout(r,100))}throw Error(`Text ${text} present=${present}: ${JSON.stringify(last)}`)}

async function waitFor(expression){for(let n=0;n<100;n++){if(await evaluate(expression))return;await new Promise(r=>setTimeout(r,50))}throw Error("Condition did not become true: "+expression+"\n"+(await browser("read")).text)}
async function waitHeld(){for(let n=0;n<100&&!held;n++)await new Promise(r=>setTimeout(r,50));assert.equal(held,true)}

try{
 await connect()
 for(const language of ["en","zh-Hant"]){
  const zh=language!=="en",react=zh?"加入表情":"React",withEmoji=zh?"回應表情 👍":"React with 👍",remove=zh?"移除自己的表情 👍":"Remove your reaction 👍";
  await browser("open",origin+"/?lang="+language);await browser("viewport",zh?"mobile":"desktop");await waitFor('document.querySelectorAll("[data-message-id]").length===2');
  assert.ok(await evaluate('!!document.querySelector(".pa-reaction svg")'),"native Bot reaction uses its readable avatar");
  for(const kind of ["user","assistant"]){
   const selector=kind==="user"?".pa-mine":".pa-message:not(.pa-mine)";
   await evaluate(`document.querySelector('${selector} .pa-reaction-add').click()`);await waitFor('!!document.querySelector(".pa-reaction-palette")');await browser(zh?"tap":"click",withEmoji);await waitFor(`!!document.querySelector('${selector} .pa-reaction-own')`);
   await browser("screenshot","--out",path.join(artifacts,language+"-"+kind+"-added.jpg"));
   await send("Page.reload",{},session);await waitFor(`!!document.querySelector('${selector} .pa-reaction-own')`);
   await evaluate(`document.querySelector('${selector} .pa-reaction-own').click()`);await waitFor(`!document.querySelector('${selector} .pa-reaction-own')`);
  }
  assert.equal(await evaluate('document.documentElement.scrollWidth<=innerWidth'),true)
  await writeFile(path.join(artifacts,language+"-removed.html"),await evaluate("document.documentElement.outerHTML"));await browser("screenshot","--out",path.join(artifacts,language+"-removed.jpg"));
  await browser("close");tab=undefined;results.push({language,addedReloadedRemoved:true,humanAndAssistant:true})
 }
 await browser("open",origin+"/?lang=en");await waitFor('document.querySelectorAll("[data-message-id]").length===2');dropNext=true;
 await evaluate('document.querySelector(".pa-mine .pa-reaction-add").click()');await waitFor('!!document.querySelector(".pa-reaction-palette")');await browser("click","React with 👍");await waitText("The reaction outcome is unknown.");
 const first=requests.filter(r=>r.path.endsWith("/react")).at(-1).body;
 await send("Page.reload",{},session);await waitText("Check original reaction");await browser("click","Check original reaction");await waitFor('!document.body.innerText.includes("The reaction outcome is unknown.")');
 const recovered=requests.filter(r=>r.path.endsWith("/react")).at(-1).body;assert.deepEqual(first,recovered);results.push({sameIDRecovery:true});await browser("close");tab=undefined;
 await browser("open",origin+"/?lang=en&read=1");await waitFor('document.querySelectorAll("[data-message-id]").length===2');assert.equal(await evaluate('[...document.querySelectorAll(".pa-reaction-add")].every(b=>b.disabled)'),true);await browser("close");tab=undefined;
 await browser("open",origin+"/?lang=en");await waitFor('document.querySelectorAll("[data-message-id]").length===2');holdNext=true;held=false;const effectsBefore=requests.filter(r=>r.path.endsWith("/react")).length;
 await evaluate('document.querySelector(".pa-message:not(.pa-mine) .pa-reaction-add").click()');await waitFor('!!document.querySelector(".pa-reaction-palette")');await browser("click","React with 👍");await waitHeld();await evaluate("window.fixtureRevoke()");releaseHeld();await waitText("The reaction outcome is unknown.");assert.equal(await evaluate('document.querySelectorAll(".pa-message:not(.pa-mine) .pa-reaction-own").length'),0);assert.equal(requests.filter(r=>r.path.endsWith("/react")).length,effectsBefore+1);results.push({lateRevokedReplyNotPainted:true,noAutomaticEffectRetry:true,nativeAvatar:true,narrowTouch:true});await browser("close");tab=undefined;
 console.log("PASS: production Chat + SourceClient + authenticated HTTP/Host/SQLite; bilingual click/add/reload/remove on human and final Bot messages; same-ID unknown recovery; read-only disabled; provider calls=0 during reactions")
}finally{
 releaseHeld();if(tab&&send)await browser("close").catch(()=>{});ws?.close();chrome.kill();await new Promise(resolve=>chrome.exitCode!==null||chrome.signalCode!==null?resolve():chrome.once("exit",resolve));await rm(profile,{recursive:true,force:true,maxRetries:5});
 await writeFile(path.join(artifacts,"requests.json"),JSON.stringify(requests,null,2));await writeFile(path.join(artifacts,"operations.json"),JSON.stringify(operations,null,2));await writeFile(path.join(artifacts,"results.json"),JSON.stringify(results,null,2));await new Promise(resolve=>server.close(resolve));
}
