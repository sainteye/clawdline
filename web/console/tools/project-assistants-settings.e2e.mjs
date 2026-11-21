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
const bundle=await build({entryPoints:[path.join(root,"tools/fixtures/project-assistants-settings.tsx")],bundle:true,write:false,outfile:"alignment.js",format:"esm",target:"es2022",platform:"browser",jsx:"automatic",define:{"process.env.NODE_ENV":'"development"',READ_MACHINE:JSON.stringify(machine),READ_BOT:JSON.stringify(bot)}})
const files=new Map(bundle.outputFiles.map(f=>["/"+path.basename(f.path),f.contents]))
const cut=(res,record,status)=>{record.status=status;res.writeHead(200,{"Content-Type":"application/json","Content-Length":"64"});res.write("{\"bot\":");setTimeout(()=>res.socket.destroy(),20)}
let tab,saveFault="",readFault=false,touch=false
const server=createServer(async(req,res)=>{try{
  const url=new URL(req.url,"http://fixture")
  if(url.pathname.startsWith("/v1/")){
    const read=url.searchParams.has("fixture_read");url.searchParams.delete("fixture_read")
    const record={method:req.method,path:url.pathname,query:url.search};requests.push(record)
    const chunks=[];for await(const chunk of req)chunks.push(chunk);const raw=Buffer.concat(chunks)
    if(raw.length)record.body=JSON.parse(raw)
    // Transport loss: "drop" lets the source commit, then cuts the reply; "lost" never reaches the source.
    if(readFault&&req.method==="GET"&&url.pathname.endsWith("/view")&&url.searchParams.get("mode")==="snapshot"){cut(res,record,"dropped");return}
    if(saveFault==="lost"&&req.method==="PATCH"){cut(res,record,"lost");return}
    if(saveFault&&saveFault!=="drop"&&req.method==="PATCH"){
      const code=saveFault==="unknown"?"pa_outcome_unknown":"pa_stale_version"
      record.status=409;res.writeHead(409,{"Content-Type":"application/json"});res.end(JSON.stringify({error:{code}}));return
    }
    const headers={Authorization:"Bearer "+(read?readToken:token),"Content-Type":"application/json"};if(req.headers["idempotency-key"])headers["Idempotency-Key"]=req.headers["idempotency-key"]
    const answer=await fetch(upstream+url.pathname+url.search,{method:req.method,headers,...raw.length?{body:raw}:{}})
    let body=Buffer.from(await answer.arrayBuffer());record.status=answer.status
    if(req.method!=="GET")record.response=JSON.parse(body)
    if(saveFault==="drop"&&req.method==="PATCH"){record.dropped=true;cut(res,record,answer.status);return}
    res.writeHead(answer.status,{"Content-Type":"application/json"});res.end(body);return
  }
  if(url.pathname.startsWith("/catalog/")){res.setHeader("Content-Type","application/json");res.end(await readFile(path.join(root,"public/catalogs",path.basename(url.pathname))));return}
  if(files.has(url.pathname)){res.setHeader("Content-Type",url.pathname.endsWith(".css")?"text/css":"text/javascript");res.end(files.get(url.pathname));return}
  res.setHeader("Content-Type","text/html");res.end('<!doctype html>'+ '<meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="stylesheet" href="/alignment.css"><style>html{--status-h:0px;--ink:#eee;--line:#333;--ease:ease}html,body,#app{height:100%;margin:0;background:#080808;color:#eee}#app>.project-assistants{height:100dvh;position:relative}.pa-chat{height:100%}</style><div id="app"></div><div id="toast" class="toast" role="status" hidden></div><script type="module" src="/alignment.js"></script>')
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
  else if(op==="viewport")result=await send("Emulation.setDeviceMetricsOverride",{width:args[0]==="mobile"?390:1280,height:args[0]==="mobile"?720:900,deviceScaleFactor:1,mobile:args[0]==="mobile"},session)
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
async function waitText(text,present=true){let last;for(let n=0;n<100;n++){last=await browser("read");if(last.text?.includes(text)===present)return last;await new Promise(r=>setTimeout(r,100))}throw Error(`Text ${text} present=${present}: ${JSON.stringify(last)}`)}

async function waitFor(expression){for(let n=0;n<100;n++){if(await evaluate(expression))return;await new Promise(r=>setTimeout(r,50))}throw Error("Condition did not become true: "+expression+"\n"+(await browser("read")).text)}

async function hit(selector){
  const point=await evaluate(`(()=>{const el=document.querySelector(${JSON.stringify(selector)});if(!el)throw Error("Missing control: "+${JSON.stringify(selector)});el.scrollIntoView({block:"center"});const r=el.getBoundingClientRect();return {x:r.x+r.width/2,y:r.y+r.height/2,disabled:el.disabled}})()`)
  assert.equal(!!point.disabled,false,selector)
  if(touch){await send("Emulation.setTouchEmulationEnabled",{enabled:true},session);await send("Input.dispatchTouchEvent",{type:"touchStart",touchPoints:[{x:point.x,y:point.y}]},session);await send("Input.dispatchTouchEvent",{type:"touchEnd",touchPoints:[]},session)}
  else{await send("Input.dispatchMouseEvent",{type:"mousePressed",x:point.x,y:point.y,button:"left",clickCount:1},session);await send("Input.dispatchMouseEvent",{type:"mouseReleased",x:point.x,y:point.y,button:"left",clickCount:1},session)}
  operations.push({op:"hit",selector})
}
async function fill(selector,value){await evaluate(`(()=>{const el=document.querySelector(${JSON.stringify(selector)});if(!el||el.disabled)throw Error("Missing enabled field");el.focus();Object.getOwnPropertyDescriptor(el.tagName==="TEXTAREA"?HTMLTextAreaElement.prototype:HTMLInputElement.prototype,"value").set.call(el,${JSON.stringify(value)});el.dispatchEvent(new Event("input",{bubbles:true}));el.dispatchEvent(new Event("change",{bubbles:true}))})()`);operations.push({op:"fill",selector,value})}
async function key(key,code=key){const windowsVirtualKeyCode=({Enter:13,Escape:27,Tab:9," ":32})[key]??0;const event={key,code,windowsVirtualKeyCode};await send("Input.dispatchKeyEvent",{type:"keyDown",...event,...(key==="Enter"?{text:"\r",unmodifiedText:"\r"}:{})},session);await send("Input.dispatchKeyEvent",{type:"keyUp",...event},session);operations.push({op:"key",...event})}
async function capture(name){await browser("screenshot","--out",path.join(artifacts,name+".jpg"));await writeFile(path.join(artifacts,name+".html"),await evaluate("document.documentElement.outerHTML"))}
async function open(query="",mobile=false){touch=mobile;await browser("open",origin+"/?"+query);await browser("viewport",mobile?"mobile":"desktop");await waitFor(query.includes("read")?'!!document.querySelector(".pa-composer textarea")':'!!document.querySelector(".pa-composer textarea:not(:disabled)")')}
async function close(){await browser("close");tab=undefined}
const posts=()=>requests.filter(r=>r.method!=="GET")
const field=".pa-settings-instructions textarea"
const editorShown='document.querySelector(".pa-settings-instructions textarea")?.getClientRects().length>0'
// The identity opens Details; the settings editor lives on the Settings tab, as in Codync.
async function openDetails(){
  await hit(".pa-chat-identity")
  await waitFor('document.querySelector(".pa-info-panel:not([hidden])")?.getAnimations().every(animation=>animation.playState!=="running")')
  await tabAt(4);await waitFor(editorShown)
}
async function tabAt(n){await hit('.pa-info-tabs [role="tab"]:nth-child('+n+')')}
async function save(){await hit('.pa-settings-savebar .pa-settings-save')}
async function waitToast(message,bad=false){
  await waitFor(`document.querySelector("#toast:not([hidden])")?.textContent===${JSON.stringify(message)}`)
  assert.equal(await evaluate('document.querySelector("#toast").classList.contains("err")'),bad)
  assert.equal(await evaluate(`(()=>{
    const rect=document.querySelector("#toast").getBoundingClientRect()
    return rect.width>0&&rect.left>=0&&rect.right<=innerWidth&&rect.top>=0&&rect.bottom<=innerHeight
  })()`),true)
}
async function cancel(){
  await hit('.pa-settings-savebar button:not(.pa-settings-save):not(.pa-settings-check-saved)')
  await waitFor('document.querySelector(".pa-info-panel").hidden')
}
async function chooseAgentSetting(label,choice){
  const menu=`[...document.querySelectorAll(".pa-agent-picker-panel .pa-select")].find(el=>el.querySelector(".pa-field-label")?.textContent===${JSON.stringify(label)})`
  await waitFor(`!!(${menu})?.querySelector(".pa-select-button:not([aria-busy=true])")`)
  await evaluate(`(${menu}).querySelector("button").focus()`)
  await key("Enter")
  const option=`[...document.querySelectorAll(".pa-agent-picker-panel [role=option]")].find(el=>el.querySelector(".pa-select-text>span")?.textContent===${JSON.stringify(choice)})`
  await waitFor(`!!(${option})`)
  const id=await evaluate(`(${option}).id`)
  await hit(`[id="${id}"]`)
}
async function editFromAnotherClient(suffix,operation,body,method){
  const id=crypto.randomUUID()
  const response=await fetch(upstream+"/v1/project-assistants/"+encodeURIComponent(bot)+suffix,{
    method,headers:{Authorization:"Bearer "+token,"Content-Type":"application/json","Idempotency-Key":id},
    body:JSON.stringify({command:{key:{machine_id:machine,bot_id:bot},operation,idempotency_key:id,content_hash:""},...body}),
  })
  const result=await response.json()
  assert.equal(response.status,200,JSON.stringify(result))
  return result.bot
}
try{
 await connect()
 for(const [lang,mobile] of [["en",false],["zh-Hant",true]]){
  const zh=lang!=="en",initial="Synthetic saved instructions "+lang
  await open("lang="+lang,mobile);await openDetails()
  assert.equal(await evaluate('document.querySelector(".pa-settings-savebar").hidden && document.querySelector(".pa-settings-savebar").getClientRects().length===0'),true,"clean Settings starts at Instructions without a save bar")
  assert.equal(await evaluate('document.querySelectorAll(".pa-settings-instructions textarea").length'),1)
  assert.equal(await evaluate('document.querySelectorAll(".pa-settings-disclosure[open]").length'),0)
  await capture(lang+"-settings")
  await fill(field,initial)
  assert.equal(await evaluate('document.querySelector(".pa-settings-savebar").hidden'),false,"dirty drafts expose explicit save and cancel")
  // Details holds work and routines only; the hidden editor keeps the draft.
  await tabAt(1);await waitFor('document.querySelector(".pa-info-editor").hidden')
  assert.equal(await evaluate('[...document.querySelectorAll(".pa-settings-instructions textarea")].filter(el=>el.getClientRects().length).length'),0)
  await capture(lang+"-details")
  await tabAt(4);await waitFor(editorShown);assert.equal(await evaluate(`document.querySelector(${JSON.stringify(field)}).value`),initial)
  await tabAt(2);await tabAt(1);assert.equal(await evaluate(`document.querySelector(${JSON.stringify(field)}).value`),initial)
  await hit('.pa-info-close');await openDetails();assert.equal(await evaluate(`document.querySelector(${JSON.stringify(field)}).value`),initial)
  // Source activity can advance the Bot version without changing any setting in the draft.
  const latest=await editFromAnotherClient("/visibility","visibility",{hidden:false},"POST")
  await save();await waitFor('document.querySelector(".pa-info-panel").hidden')
  const edit=posts().filter(r=>r.method==="PATCH").at(-1);assert.equal(edit.status,200);assert.equal(edit.body.settings.instructions,initial)
  assert.equal(edit.body.settings.expected_version,latest.version)
  await waitToast(zh?"已儲存 Bot 設定。":"Bot settings saved.")
  await close();await open("lang="+lang,mobile);await openDetails();assert.equal(await evaluate(`document.querySelector(${JSON.stringify(field)}).value`),initial)
  const writesBeforeCancel=posts().length;await fill(field,"Synthetic discarded draft");await cancel();await openDetails();assert.equal(await evaluate(`document.querySelector(${JSON.stringify(field)}).value`),initial);assert.equal(posts().length,writesBeforeCancel,"cancel does not write settings")
  // A second client edits the same settings while a draft is open.
  const competingName="Synthetic renamed Bot "+lang
  await fill(field,"Synthetic conflicting draft")
  await editFromAnotherClient("","edit",{settings:{expected_version:edit.response.bot.version,name:competingName,
    appearance:edit.response.bot.appearance,instructions:initial}},"PATCH")
  const writesBeforeConflict=posts().length
  await save();await waitToast(zh?"無法儲存 Bot 設定。":"Couldn’t save Bot settings.",true)
  assert.equal(posts().length,writesBeforeConflict,"a settings conflict cannot overwrite the other client")
  assert.equal(await evaluate(`document.querySelector(${JSON.stringify(field)}).value`),"Synthetic conflicting draft")
  assert.equal(await evaluate('!!document.querySelector(".pa-settings-savebar [role=alert]")'),true)
  await cancel();await openDetails()
  assert.equal(await evaluate('document.querySelector(".pa-settings-field input").value'),competingName)
  assert.equal(await evaluate(`document.querySelector(${JSON.stringify(field)}).value`),initial)
  // Source refusals and unknown outcomes keep the exact draft without replay.
  for(const fault of ["stale","unknown"]){
   await fill(field,"Synthetic protected "+fault);saveFault=fault;const before=posts().length;await save();await waitFor('!!document.querySelector(".pa-settings-editor [role=alert]")');assert.equal(posts().length,before+1)
   assert.equal(await evaluate(`document.querySelector(${JSON.stringify(field)}).value`),"Synthetic protected "+fault)
   await tabAt(1);await tabAt(4);await waitFor(editorShown);assert.equal(await evaluate(`document.querySelector(${JSON.stringify(field)}).value`),"Synthetic protected "+fault)
   if(fault==="unknown"){await waitToast(zh?"無法確認 Bot 設定是否已儲存。":"Bot settings save could not be confirmed.",true);assert.equal(await evaluate('document.querySelector(".pa-settings-save").disabled'),true);await new Promise(r=>setTimeout(r,150));assert.equal(posts().length,before+1)}
   await capture(lang+"-"+fault);saveFault="";await cancel();await openDetails()
  }
  assert.equal(await evaluate('document.documentElement.scrollWidth<=innerWidth'),true)
  assert.equal(await evaluate('(()=>{const panel=document.querySelector(".pa-info-panel").getBoundingClientRect();return [...document.querySelectorAll(".pa-settings-editor input,.pa-settings-editor select,.pa-settings-editor textarea,.pa-settings-disclosure>summary")].filter(el=>el.getClientRects().length).every(el=>{const r=el.getBoundingClientRect();return r.left>=panel.left-1&&r.right<=panel.right+1})})()'),true)
  await capture(lang+"-saved-reopened");await close()
 }
 // An unconfirmed save is reconciled by an authorized read, never by a replayed write.
 const patches=()=>requests.filter(r=>r.method==="PATCH").length,check=".pa-settings-savebar .pa-settings-check-saved"
 const locked='document.querySelector(".pa-settings-save").disabled&&document.querySelector(".pa-settings-editor fieldset").disabled'
 for(const [lang,mobile] of [["en",false],["zh-Hant",true]]){
  const zh=lang!=="en",copy=await (await fetch(origin+"/catalog/"+lang+".json")).json(),t=k=>copy["projectAssistants."+k]
  await open("lang="+lang,mobile);await openDetails()
  const committed="Synthetic committed reply lost "+lang
  await fill(field,committed);saveFault="drop";let before=patches();await save()
  for(const k of ["settingsSaveUnknownCheck","settingsCheckSaved","settingsCheckConfirmed","settingsCheckUnchanged","settingsCheckDiverged","settingsCheckFailed"])assert.ok(t(k),k)
  await waitFor(`!!document.querySelector(".pa-settings-savebar [role=alert]")`);saveFault=""
  assert.equal(await evaluate('document.querySelector(".pa-settings-savebar [role=alert]").textContent'),t("settingsSaveUnknownCheck"))
  const dropped=requests.filter(r=>r.method==="PATCH").at(-1);assert.equal(dropped.dropped,true);assert.equal(dropped.response.bot.instructions.text,committed,"the source committed before the reply was cut")
  assert.equal(patches(),before+1);assert.equal(await evaluate(locked),true,"unknown keeps Save and fields locked")
  assert.equal(await evaluate(`document.querySelector(${JSON.stringify(check)}).textContent`),t("settingsCheckSaved"))
  // A failed read leaves every protection in place.
  readFault=true;await hit(check);await waitText(t("settingsCheckFailed"));readFault=false
  assert.equal(await evaluate(locked),true,"a failed check keeps the save locked");assert.equal(patches(),before+1)
  await capture(lang+"-check-failed")
  for(let n=0;n<40&&!(await browser("read")).text.includes(t("settingsCheckConfirmed"));n++){if(await evaluate(`!!document.querySelector(${JSON.stringify(check)}+":not(:disabled)")`))await hit(check);await new Promise(r=>setTimeout(r,250))}
  await waitText(t("settingsCheckConfirmed"))
  assert.equal(await evaluate(`document.querySelector(${JSON.stringify(field)}).value`),committed)
  assert.equal(await evaluate('document.querySelector(".pa-settings-editor fieldset").disabled'),false,"confirmed settings unlock in place")
  assert.equal(await evaluate(`!!document.querySelector(${JSON.stringify(check)})`),false)
  assert.equal(patches(),before+1,"checking never writes");assert.equal(await evaluate('document.documentElement.scrollWidth<=innerWidth'),true)
  assert.equal(await evaluate('(()=>{const r=document.querySelector(".pa-settings-savebar [role=status]").getBoundingClientRect();return r.width>0&&r.top>=0&&r.bottom<=innerHeight})()'),true,"the result stays visible")
  await capture(lang+"-check-confirmed")
  // The write never reached the source: the draft stays and Save works again without reopening.
  const lost="Synthetic never arrived "+lang
  await fill(field,lost);saveFault="lost";before=patches();await save()
  await waitFor(`!!document.querySelector(${JSON.stringify(check)})`);saveFault="";assert.equal(await evaluate(locked),true)
  await hit(check);await waitText(t("settingsCheckUnchanged"))
  assert.equal(await evaluate(`document.querySelector(${JSON.stringify(field)}).value`),lost);assert.equal(patches(),before+1,"no automatic replay")
  assert.equal(await evaluate('document.querySelector(".pa-settings-save").disabled'),false);await capture(lang+"-check-unchanged")
  await save();await waitFor('document.querySelector(".pa-info-panel").hidden');assert.equal(patches(),before+2)
  const resent=requests.filter(r=>r.method==="PATCH").at(-1);assert.equal(resent.status,200);assert.equal(resent.response.bot.instructions.text,lost)
  // The reply is cut and another client then saves different settings: nothing overwrites either.
  await openDetails();const draft="Synthetic diverged draft "+lang,other="Synthetic other writer "+lang
  await fill(field,draft);saveFault="drop";before=patches();await save()
  await waitFor(`!!document.querySelector(${JSON.stringify(check)})`);saveFault=""
  const cut=requests.filter(r=>r.method==="PATCH").at(-1).response.bot
  await editFromAnotherClient("","edit",{settings:{expected_version:cut.version,name:cut.name,appearance:cut.appearance,instructions:other}},"PATCH")
  await hit(check);await waitText(t("settingsCheckDiverged"))
  assert.equal(await evaluate(`document.querySelector(${JSON.stringify(field)}).value`),draft,"the draft is preserved")
  assert.equal(await evaluate('document.querySelector(".pa-settings-save").disabled'),true,"a diverged source refuses an overwrite")
  assert.equal(patches(),before+1,"only the cut save crossed this browser; the other client wrote directly")
  await capture(lang+"-check-diverged")
  await cancel();await openDetails();assert.equal(await evaluate(`document.querySelector(${JSON.stringify(field)}).value`),other)
  assert.equal(zh?mobile:!mobile,true);await close()
 }
 await open("lang=en",false);await openDetails()
 await hit(".pa-agent-picker-summary")
 await chooseAgentSetting("Model","Synthetic large")
 await chooseAgentSetting("Reasoning effort","High")
 await chooseAgentSetting("Fast mode","On")
 await save();await waitFor('document.querySelector(".pa-info-panel").hidden')
 await fill('.pa-composer textarea','Synthetic settings next-turn check');await hit('.pa-send');await waitText('Synthetic runtime selected synthetic-large / high / true')
 await capture('next-turn-selected-runtime');await close()
 // Real pointer/key actions on the same persisted scripted reply. No model request is made here.
 for(const mobile of [false,true]){
  await open("lang=en",mobile);await waitFor('!!document.querySelector(".pa-message:not(.pa-mine) .pa-reaction-add")')
  const trigger='.pa-message:not(.pa-mine) .pa-reaction-add'
  await evaluate(`document.querySelector(${JSON.stringify(trigger)}).focus()`)
  await key("Enter");await waitFor('!!document.querySelector(".pa-reaction-palette")')
  await key("Escape");await waitFor('!document.querySelector(".pa-reaction-palette")')
  assert.equal(await evaluate(`document.activeElement===document.querySelector(${JSON.stringify(trigger)})`),true,"Escape restores the reaction trigger")
  await key("Enter");await waitFor('!!document.querySelector(".pa-reaction-palette")');await openDetails()
  await waitFor('!document.querySelector(".pa-reaction-palette")');await hit('.pa-info-close')
  await evaluate('document.querySelector(".pa-chat-identity").focus()');await key("Enter");await waitFor('!document.querySelector(".pa-info-panel").hidden');await key("Escape");await waitFor('document.querySelector(".pa-info-panel").hidden');assert.equal(await evaluate('document.activeElement===document.querySelector(".pa-chat-identity")'),true)
  assert.equal(await evaluate('!!document.querySelector(".pa-reaction-palette")'),false,"Details does not retain a stale palette")
  await evaluate('document.querySelector(".pa-mine").focus()');await key("Enter");await waitFor('!!document.querySelector(".pa-message-actions")')
  await key("Escape");await waitFor('!document.querySelector(".pa-message-actions")')
  assert.equal(await evaluate('document.activeElement===document.querySelector(".pa-mine .pa-message-more")'),true)
  const reply='.pa-message:not(.pa-mine)'
  const point=await evaluate(`(()=>{const r=document.querySelector(${JSON.stringify(reply)}).getBoundingClientRect();return {x:r.x+5,y:r.y+5}})()`)
  await send("Input.dispatchMouseEvent",{type:"mouseMoved",...point},session)
  assert.equal(await evaluate(`getComputedStyle(document.querySelector(${JSON.stringify(reply+" .pa-message-more")})).opacity`),"1","pointer reveals reply actions")
  await evaluate(`document.querySelector(${JSON.stringify(reply+" .pa-message-more")}).focus()`);assert.equal(await evaluate(`getComputedStyle(document.querySelector(${JSON.stringify(reply+" .pa-message-more")})).opacity`),"1","keyboard focus reveals reply actions")
  await key("Enter")
  const quote='[...document.querySelectorAll(".pa-message-menu [role=menuitem]")].find(el=>el.textContent==="Reply")'
  await waitFor(`!!(${quote})`);await evaluate(`(${quote}).focus()`);await key("Enter")
  await waitFor('!!document.querySelector(".pa-reply")')
  await capture(mobile?'mobile-actions':'desktop-actions');await close()
 }

 await open("lang=en&read=1",true);await openDetails();assert.equal(await evaluate('document.querySelector(".pa-settings-save").disabled'),true);await capture('readonly');await close()
 assert.equal(posts().filter(r=>r.path.endsWith("/react")).length,0,"opening and dismissing actions does not write reactions")
 results.push({settingsPersisted:true,backgroundVersionRebased:true,conflictKept:true,saveToasts:true,tabDraftPreserved:true,closeDraftPreserved:true,cancelDiscards:true,staleKept:true,unknownLockedNoReplay:true,reconciledCommitted:true,reconciledUncommitted:true,reconciledDiverged:true,checkReadFailureLocked:true,desktop:[1280,900],mobile:[390,720],externalModelRequests:0})
 console.log("PASS: production Chat settings crossed authenticated HTTP/Host/SQLite; unique editor, drafts, reopen, conflict/unknown fences and next scripted ACP turn; external model requests=0")
}catch(error){if(tab)await capture("failure").catch(()=>{});throw error}
finally{
 if(tab)await close().catch(()=>{})
 await writeFile(path.join(artifacts,"operations.json"),JSON.stringify({operations,requests,results},null,2))
 ws?.close();if(chrome.pid&&chrome.exitCode===null&&chrome.signalCode===null){const exited=new Promise(resolve=>chrome.once("exit",resolve));chrome.kill("SIGTERM");await exited}await new Promise(resolve=>server.close(resolve));await rm(profile,{recursive:true,force:true})
}
