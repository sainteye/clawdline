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
const bundle=await build({entryPoints:[path.join(root,"tools/fixtures/project-assistants-mark-read.tsx")],bundle:true,write:false,outfile:"mark-read.js",format:"esm",target:"es2022",platform:"browser",jsx:"automatic",define:{"process.env.NODE_ENV":'"development"',READ_MACHINE:JSON.stringify(machine),READ_BOT:JSON.stringify(bot)}})
const files=new Map(bundle.outputFiles.map(f=>["/"+path.basename(f.path),f.contents]))
let tab,holdNext=false,held=false,replaceConversation=false
const holds=[];const releaseHeld=()=>{holdNext=false;for(const resolve of holds.splice(0))resolve()}
const botPath="/v1/project-assistants/"+bot
const server=createServer(async(req,res)=>{try{
  const url=new URL(req.url,"http://fixture")
  if(url.pathname.startsWith("/v1/")){
    const read=url.searchParams.has("fixture_read"),forceUnread=url.searchParams.has("fixture_force_unread")
    url.searchParams.delete("fixture_read");url.searchParams.delete("fixture_force_unread")
    const record={method:req.method,path:url.pathname};requests.push(record)
    assert.ok(req.method==="GET"||(req.method==="POST"&&url.pathname===botPath+"/read"),"Only read cursor observation is allowed")
    const chunks=[];for await(const chunk of req)chunks.push(chunk);const raw=Buffer.concat(chunks)
    if(raw.length)record.body=JSON.parse(raw)
    const headers={Authorization:"Bearer "+(read?readToken:token),"Content-Type":"application/json"};if(req.headers["idempotency-key"])headers["Idempotency-Key"]=req.headers["idempotency-key"]
    const answer=await fetch(upstream+url.pathname+url.search,{method:req.method,headers,...raw.length?{body:raw}:{}})
    let body=Buffer.from(await answer.arrayBuffer())
    record.status=answer.status
    if(holdNext&&url.pathname===botPath+"/view"&&url.searchParams.get("mode")==="snapshot"){held=true;await new Promise(resolve=>{holds.push(resolve)})}
    if(replaceConversation&&url.pathname===botPath+"/view"){const value=JSON.parse(body);value.head.conversation.id="other-synthetic-conversation";body=Buffer.from(JSON.stringify(value))}
    if(forceUnread&&record.status===200&&req.method==="GET"&&url.pathname==="/v1/project-assistants"){
      const value=JSON.parse(body)
      for(const row of value.items)if(row.bot.key.bot_id===bot)row.unread_count=1
      body=Buffer.from(JSON.stringify(value))
    }
    if(req.method==="POST")record.response=JSON.parse(body)
    res.writeHead(answer.status,{"Content-Type":"application/json"});res.end(body);return
  }
  if(url.pathname.startsWith("/catalog/")){res.setHeader("Content-Type","application/json");res.end(await readFile(path.join(root,"public/catalogs",path.basename(url.pathname))));return}
  if(files.has(url.pathname)){res.setHeader("Content-Type",url.pathname.endsWith(".css")?"text/css":"text/javascript");res.end(files.get(url.pathname));return}
  res.setHeader("Content-Type","text/html");res.end(await readFile(path.join(root,"tools/fixtures/project-assistants-mark-read.html")))
}catch(error){res.writeHead(500);res.end(String(error))}})
await new Promise(resolve=>server.listen(0,"127.0.0.1",resolve));const origin=`http://127.0.0.1:${server.address().port}`
const executable=process.env.CHROME;assert.ok(executable,"CHROME must name an existing test browser; this harness installs nothing");await access(executable,constants.X_OK)
const profile=await mkdtemp(path.join(artifacts,"browser-profile-")),chrome=spawn(executable,["--disable-features=OmniboxEverywhere","--headless=new","--remote-debugging-port=0","--use-mock-keychain","--user-data-dir="+profile,"--no-first-run","--no-default-browser-check","about:blank"],{stdio:["ignore","ignore","pipe"]})
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
const action=zh=>zh?"全部標為已讀":"Mark all as read"
async function openRoster(language="en",readonly=false,forceUnread=false){
  await browser("open",origin+"/?lang="+language+(readonly?"&read=1":"")+(forceUnread?"&force_unread=1":""))
  await browser("viewport",language==="en"?"desktop":"mobile")
  await waitFor('!!document.querySelector(".pa-roster-read:not(:disabled)")')
  return evaluate('document.querySelector(".pa-roster-read").getAttribute("aria-label")')
}
const readPosts=()=>requests.filter(r=>r.method==="POST")
try{
  await connect()
  for(const [language,readonly] of [["en",false],["zh-Hant",true]]){
    const zh=language!=="en",label=await openRoster(language,readonly);assert.ok(label.startsWith(action(zh)))
    const rowSize=()=>evaluate(`(() => {
      const entry=document.querySelector(".pa-roster-entry:has(.pa-bot-row)")
      return [entry,entry.querySelector(".pa-bot-row")].map(el=>el.getBoundingClientRect().width)
    })()`)
    const [entryWidth,unreadWidth]=await rowSize()
    assert.ok(Math.abs(entryWidth-unreadWidth)<1,`Unread row width ${unreadWidth} of ${entryWidth}`)
    const before=readPosts().length
    await browser("click",label);await waitFor('!document.querySelector(".pa-roster-read")')
    assert.ok(Math.abs((await rowSize())[1]-unreadWidth)<1,"Marking read keeps the row width")
    assert.equal(readPosts().length,before+1)
    const record=readPosts().at(-1);assert.equal(record.status,200);assert.equal(record.body.cursor.message_seq,record.response.cursor.message_seq);assert.ok(record.body.cursor.message_seq>0)
    assert.equal(record.body.cursor.conversation_id,record.response.cursor.conversation_id)
    assert.equal(await evaluate('document.documentElement.dataset.readAcknowledged'),"true")
    assert.equal(await evaluate('document.documentElement.scrollWidth<=innerWidth'),true)
    await browser("screenshot","--out",path.join(artifacts,language+"-marked.jpg"));await writeFile(path.join(artifacts,language+"-marked.html"),await evaluate("document.documentElement.outerHTML"))
    await browser("close");tab=undefined;results.push({language,readonly,sequence:record.body.cursor.message_seq,acknowledged:true})
  }
  // A delayed snapshot cannot authorize a read after the captured grant changes.
  const revokeButton=await openRoster("en",false,true);holdNext=true;held=false;const beforeRevoke=readPosts().length
  await browser("click",revokeButton);await waitHeld();await evaluate("window.fixtureRevoke()");releaseHeld();await waitText("pa_revoked");assert.equal(readPosts().length,beforeRevoke)
  assert.equal(await evaluate('document.documentElement.dataset.readAcknowledged'),undefined)
  await browser("close");tab=undefined
  // A source snapshot for another conversation must not produce a cursor write.
  const mismatchButton=await openRoster("en",false,true);replaceConversation=true;const beforeMismatch=readPosts().length;await browser("click",mismatchButton);await waitText("pa_scope_denied");assert.equal(readPosts().length,beforeMismatch);replaceConversation=false
  await browser("close");tab=undefined
  // Leaving the chat cancels a pending observation without marking a replacement view.
  const leaveButton=await openRoster("en",false,true);holdNext=true;held=false;const beforeLeave=readPosts().length;await browser("click",leaveButton);await waitHeld();await browser("close");tab=undefined;releaseHeld();assert.equal(readPosts().length,beforeLeave)
  assert.equal(readPosts().length,2);assert.ok(readPosts().every(r=>r.path===botPath+"/read"))
  results.push({revokedGrantRefused:true,foreignConversationRefused:true,unmountedObservationCancelled:true,executionMutations:0,providerCalls:0})
  console.log("PASS: bilingual production unread roster row -> fresh source sequence -> authenticated read cursor acknowledgement; read-only observation, late revocation/conversation/leave fences; execution/model mutations=0")
}finally{
  releaseHeld?.()
  if(tab&&send)await browser("close").catch(error=>console.error("Owned tab cleanup:",error.message))
  ws?.close();chrome.kill();await new Promise(resolve=>chrome.exitCode!==null||chrome.signalCode!==null?resolve():chrome.once("exit",resolve));await rm(profile,{recursive:true,force:true,maxRetries:5})
  await writeFile(path.join(artifacts,"requests.json"),JSON.stringify(requests,null,2)+"\n");await writeFile(path.join(artifacts,"operations.json"),JSON.stringify(operations,null,2)+"\n");await writeFile(path.join(artifacts,"results.json"),JSON.stringify(results,null,2)+"\n");await new Promise(resolve=>server.close(resolve))
}
