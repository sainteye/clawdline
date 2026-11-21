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
const bundle=await build({entryPoints:[path.join(root,"tools/fixtures/project-assistants-template.tsx")],bundle:true,write:false,outfile:"template.js",format:"esm",target:"es2022",platform:"browser",jsx:"automatic",define:{"process.env.NODE_ENV":'"development"',TEMPLATE_MACHINE:JSON.stringify(machine),TEMPLATE_BOT:JSON.stringify(bot)}})
const files=new Map(bundle.outputFiles.map(f=>["/"+path.basename(f.path),f.contents]))
let tab,holdNext=false,held=false,replaceConversation=false,replaceSettings=false
const holds=[];const releaseHeld=()=>{holdNext=false;for(const resolve of holds.splice(0))resolve()}
const botPath="/v1/project-assistants/"+bot
const server=createServer(async(req,res)=>{try{
  const url=new URL(req.url,"http://fixture")
  if(url.pathname.startsWith("/v1/")){
    const read=url.searchParams.has("fixture_read");url.searchParams.delete("fixture_read")
    const record={method:req.method,path:url.pathname};requests.push(record)
    assert.equal(req.method,"GET","Template fixture forbids every mutation")
    const answer=await fetch(upstream+url.pathname+url.search,{headers:{Authorization:"Bearer "+(read?readToken:token)}})
    let body=Buffer.from(await answer.arrayBuffer())
    record.status=answer.status
    if(holdNext&&url.pathname===botPath){held=true;await new Promise(resolve=>{holds.push(resolve)})}
    if(replaceSettings&&url.pathname===botPath){const value=JSON.parse(body);value.bot.name="Other synthetic settings";body=Buffer.from(JSON.stringify(value))}
    if(replaceConversation&&url.pathname===botPath){const value=JSON.parse(body);value.conversation.id="other-synthetic-conversation";value.bot.conversation_id="other-synthetic-conversation";body=Buffer.from(JSON.stringify(value))}
    res.writeHead(answer.status,{"Content-Type":"application/json"});res.end(body);return
  }
  if(url.pathname.startsWith("/catalog/")){res.setHeader("Content-Type","application/json");res.end(await readFile(path.join(root,"public/catalogs",path.basename(url.pathname))));return}
  if(files.has(url.pathname)){res.setHeader("Content-Type",url.pathname.endsWith(".css")?"text/css":"text/javascript");res.end(files.get(url.pathname));return}
  res.setHeader("Content-Type","text/html");res.end(await readFile(path.join(root,"tools/fixtures/project-assistants-template.html")))
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
const expected=JSON.stringify({format:"clawdline.bot-template.v1",settings:{agent:"codex",appearance:{color:"#e39567",shape:"blob"},instructions:'Synthetic configured instructions.\n</textarea><script>window.templateExecuted=true</script>',name:"Synthetic template Bot",notify:"off",permission_policy:"ask",runtime_settings:{model:"synthetic-model",options:{effort:"high",fast:"false"}}}},null,2)
async function readyTemplate(zh=false){
  if(await evaluate('document.querySelector(".pa-info-panel").hidden'))await browser("click",zh?"Synthetic template Bot · Bot 資訊":"Synthetic template Bot · Bot information")
  if(await evaluate('document.querySelector(".pa-info-editor").hidden'))await browser("click",zh?"Bot 設定":"Bot settings")
  await waitFor(`(()=>{const e=[...document.querySelectorAll('.pa-settings-link')].find(b=>b.textContent===${JSON.stringify(zh?"建立範本":"Create template")});return e&&!e.disabled})()`)
}
async function openPreview(zh=false){await readyTemplate(zh);await browser("click",zh?"建立範本":"Create template");await waitFor('!!document.querySelector("dialog textarea")')}
async function closePreview(){await send("Input.dispatchKeyEvent",{type:"keyDown",key:"Escape",code:"Escape",windowsVirtualKeyCode:27},session);await send("Input.dispatchKeyEvent",{type:"keyUp",key:"Escape",code:"Escape",windowsVirtualKeyCode:27},session);await waitFor('!document.querySelector("dialog")')}
try{
  await connect()
  await send("Browser.grantPermissions",{origin,permissions:["clipboardReadWrite","clipboardSanitizedWrite"]})
  for(const [language,readonly] of [["en",false],["zh-Hant",true]]){
    const zh=language!=="en",copy=zh?"複製範本":"Copy template",copied=zh?"已複製範本":"Template copied"
    await browser("open",origin+"/?lang="+language+(readonly?"&read=1":""));await browser("viewport",zh?"mobile":"desktop");await waitText("Synthetic template Bot");await waitFor('!!document.querySelector(".pa-composer textarea")')
    if(!readonly)await evaluate(`(()=>{const e=document.querySelector('.pa-composer textarea');Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype,"value").set.call(e,"Preserved synthetic draft");e.dispatchEvent(new Event("input",{bubbles:true}));})()`)
    const historyBefore=await evaluate("history.length");await openPreview(zh);assert.equal(await evaluate("history.length"),historyBefore+1,"StrictMode adds exactly one modal Back entry")
    const actual=await evaluate('document.querySelector("dialog textarea").value');assert.equal(actual,expected)
    assert.equal(await evaluate('window.templateExecuted===true'),false)
    assert.equal(await evaluate('document.documentElement.scrollWidth<=innerWidth'),true)
    await browser("screenshot","--out",path.join(artifacts,language+"-preview.jpg"));await writeFile(path.join(artifacts,language+"-preview.html"),await evaluate("document.documentElement.outerHTML"))
    // IME Enter on a focused button must not copy or send; plain Enter in preview has no global shortcut.
    await evaluate(`(()=>{const b=[...document.querySelectorAll("dialog button")].at(-1);b.focus();b.dispatchEvent(new KeyboardEvent("keydown",{key:"Enter",isComposing:true,bubbles:true,cancelable:true}));document.querySelector("dialog textarea").focus()})()`)
    await send("Input.dispatchKeyEvent",{type:"keyDown",key:"Enter",code:"Enter",windowsVirtualKeyCode:13},session);await send("Input.dispatchKeyEvent",{type:"keyUp",key:"Enter",code:"Enter",windowsVirtualKeyCode:13},session)
    assert.equal((await browser("read")).text.includes(copied),false)
    await browser("click",copy);await waitText(copied)
    const clipboard=await evaluate('navigator.clipboard.readText()');assert.equal(clipboard,expected)
    await writeFile(path.join(artifacts,language+"-clipboard.json"),clipboard)
    await browser("screenshot","--out",path.join(artifacts,language+"-copied.jpg"))
    await closePreview()

    await waitFor('document.activeElement?.textContent==='+JSON.stringify(zh?"建立範本":"Create template"))
    assert.equal(await evaluate('document.activeElement?.textContent'),zh?"建立範本":"Create template")
    if(!readonly)assert.equal(await evaluate('document.querySelector(".pa-composer textarea").value'),"Preserved synthetic draft")
    await openPreview(zh);await evaluate("history.back()");await waitFor('!document.querySelector("dialog")')
    assert.ok(await evaluate('!!document.querySelector(".pa-chat")'))
    // Clipboard rejection keeps exactly the same selectable preview and never reports copied.
    await openPreview(zh)
    await send("Browser.setPermission",{origin,permission:{name:"clipboard-write"},setting:"denied"})
    await browser("click",copy);await waitText(zh?"請選取預覽文字並手動複製":"Select the preview text and copy it manually")
    assert.equal(await evaluate('document.querySelector("dialog textarea").value'),expected)
    assert.equal(await evaluate('(()=>{const e=document.querySelector("dialog textarea");e.focus();e.select();return e.selectionEnd-e.selectionStart})()'),expected.length)
    assert.equal((await browser("read")).text.includes(copied),false)
    await browser("screenshot","--out",path.join(artifacts,language+"-denied.jpg"))
    await send("Browser.grantPermissions",{origin,permissions:["clipboardReadWrite","clipboardSanitizedWrite"]})
    await closePreview()
    await openPreview(zh);await evaluate('Object.defineProperty(navigator,"clipboard",{value:undefined,configurable:true})');await browser("click",copy);await waitText(zh?"請選取預覽文字並手動複製":"Select the preview text and copy it manually");assert.equal(await evaluate('document.querySelector("dialog textarea").value'),expected)
    await closePreview();await browser("close");tab=undefined
    results.push({language,readonly,viewport:zh?390:1280,previewExact:true,clipboardExact:true,denialHonest:true,escapeFocus:true,backPreservesChat:true,imeNoCopy:true})
  }
  // Revoke while the preview's authorized read is in flight.
  await browser("open",origin+"/?lang=en&read=1");await waitText("Synthetic template Bot");await waitFor('!!document.querySelector(".pa-composer textarea")');holdNext=true;held=false
  await readyTemplate();await browser("click","Create template");await waitHeld();await evaluate("window.fixtureRevoke()");releaseHeld();await waitText("Nothing was copied.");assert.equal(await evaluate('!!document.querySelector("dialog textarea")'),false)
  await browser("close");tab=undefined
  // Revoke while copy revalidates; a late response cannot reach clipboard.
  await browser("open",origin+"/?lang=en&read=1");await waitText("Synthetic template Bot");await openPreview();await evaluate('navigator.clipboard.writeText("synthetic clipboard sentinel")');holdNext=true;held=false
  await browser("click","Copy template");await waitHeld();await evaluate("window.fixtureRevoke()");releaseHeld();await waitText("Nothing was copied.");assert.equal(await evaluate('navigator.clipboard.readText()'),"synthetic clipboard sentinel")
  await browser("close");tab=undefined
  // A different conversation returned by the same endpoint is never a template source.
  await browser("open",origin+"/?lang=en&read=1");await waitText("Synthetic template Bot");await openPreview();replaceConversation=true;await browser("click","Copy template");await waitText("Nothing was copied.");assert.equal(await evaluate('navigator.clipboard.readText()'),"synthetic clipboard sentinel");replaceConversation=false
  await browser("close");tab=undefined
  await browser("open",origin+"/?lang=en&read=1");await waitText("Synthetic template Bot");await openPreview();replaceSettings=true;await browser("click","Copy template");await waitText("Nothing was copied.");assert.equal(await evaluate('navigator.clipboard.readText()'),"synthetic clipboard sentinel");replaceSettings=false
  await browser("close");tab=undefined
  await browser("open",origin+"/?lang=en&read=1");await waitText("Synthetic template Bot");await openPreview();holdNext=true;held=false;await browser("click","Copy template");await waitHeld();await closePreview();releaseHeld();assert.equal(await evaluate('navigator.clipboard.readText()'),"synthetic clipboard sentinel")
  await browser("close");tab=undefined
  assert.ok(requests.some(r=>r.path===botPath&&r.status===200));assert.ok(requests.every(r=>r.method==="GET"))
  results.push({latePreviewRevocationRejected:true,lateCopyRevocationRejected:true,changedConversationRejected:true,changedSettingsRejected:true,unmountedCopyRejected:true,unsupportedClipboardHonest:true,mutations:0,modelCalls:0})
  console.log("PASS: bilingual production Chat menu -> authenticated source read -> exact preview -> real browser clipboard; denial, Escape/back/focus, draft/IME, delayed source and changed-conversation fences; mutations/model calls=0")
}finally{
  releaseHeld?.()
  if(tab&&send)await browser("close").catch(error=>console.error("Owned tab cleanup:",error.message))
  ws?.close();chrome.kill();await new Promise(resolve=>chrome.exitCode!==null||chrome.signalCode!==null?resolve():chrome.once("exit",resolve));await rm(profile,{recursive:true,force:true,maxRetries:5})
  await writeFile(path.join(artifacts,"requests.json"),JSON.stringify(requests,null,2)+"\n");await writeFile(path.join(artifacts,"operations.json"),JSON.stringify(operations,null,2)+"\n");await writeFile(path.join(artifacts,"results.json"),JSON.stringify(results,null,2)+"\n");await new Promise(resolve=>server.close(resolve))
}
