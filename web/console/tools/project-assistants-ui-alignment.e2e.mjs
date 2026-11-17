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
const bundle=await build({entryPoints:[path.join(root,"tools/fixtures/project-assistants-ui-alignment.tsx")],bundle:true,write:false,outfile:"alignment.js",format:"esm",target:"es2022",platform:"browser",jsx:"automatic",define:{"process.env.NODE_ENV":'"development"',READ_MACHINE:JSON.stringify(machine),READ_BOT:JSON.stringify(bot),ALIGN_GROUP:process.env.ALIGN_GROUP,ALIGN_BOTS:process.env.ALIGN_BOTS}})
const files=new Map(bundle.outputFiles.map(f=>["/"+path.basename(f.path),f.contents]))
if(process.env.BOTS_BASELINE){
  const source=await readFile(path.join(root,"tools/fixtures/project-assistants-ui-alignment.tsx"),"utf8")
  const before=await build({nodePaths:[path.resolve(root,"../node_modules")],stdin:{contents:source.replaceAll('"../../src/','"'+path.join(process.env.BOTS_BASELINE,"web/console/src")+'/'),resolveDir:path.join(root,"tools/fixtures"),loader:"tsx"},bundle:true,write:false,outfile:"before.js",format:"esm",target:"es2022",platform:"browser",jsx:"automatic",define:{"process.env.NODE_ENV":'"development"',READ_MACHINE:JSON.stringify(machine),READ_BOT:JSON.stringify(bot),ALIGN_GROUP:process.env.ALIGN_GROUP,ALIGN_BOTS:process.env.ALIGN_BOTS}})
  for(const file of before.outputFiles)files.set("/"+path.basename(file.path),file.contents)
}
let tab,expiredDetail=false,staleDetail=false,failAnswer=false,unknownGroup=false
const server=createServer(async(req,res)=>{try{
  const url=new URL(req.url,"http://fixture")
  if(url.pathname.startsWith("/v1/")){
    const read=url.searchParams.has("fixture_read");url.searchParams.delete("fixture_read")
    const group=url.pathname.includes("/rooms/groups")||url.searchParams.has("fixture_group");url.searchParams.delete("fixture_group")
    const record={method:req.method,path:url.pathname,query:url.search};requests.push(record)
    const chunks=[];for await(const chunk of req)chunks.push(chunk);const raw=Buffer.concat(chunks)
    if(raw.length)record.body=JSON.parse(raw)
    if((failAnswer&&url.pathname.endsWith("/answer"))||(unknownGroup&&group&&req.method==="POST"&&url.pathname.endsWith("/messages"))){
      res.writeHead(503,{"Content-Type":"application/json"});res.end(JSON.stringify({error:{code:"pa_outcome_unknown"}}));record.status=503;return
    }
    const headers={Authorization:"Bearer "+(group?(read?"reader":"person"):(read?readToken:token)),"Content-Type":"application/json"};if(req.headers["idempotency-key"])headers["Idempotency-Key"]=req.headers["idempotency-key"]
    const answer=await fetch((group?process.env.GROUP_ORIGIN:upstream)+url.pathname+url.search,{method:req.method,headers,...raw.length?{body:raw}:{}})
    let body=Buffer.from(await answer.arrayBuffer());record.status=answer.status
    if(url.pathname.endsWith("/view")&&url.searchParams.get("mode")==="detail"){
      const value=JSON.parse(body)
      const permission=value.items?.find(row=>row.permission)?.permission;if(permission){if(expiredDetail)permission.expires_at="2000-01-01T00:00:00Z";if(staleDetail)permission.fence.generation+=1;body=Buffer.from(JSON.stringify(value))}
    }
    if(req.method!=="GET")record.response=JSON.parse(body)
    res.writeHead(answer.status,{"Content-Type":"application/json"});res.end(body);return
  }
  if(url.pathname.startsWith("/catalog/")){res.setHeader("Content-Type","application/json");res.end(await readFile(path.join(root,"public/catalogs",path.basename(url.pathname))));return}
  if(files.has(url.pathname)){res.setHeader("Content-Type",url.pathname.endsWith(".css")?"text/css":"text/javascript");res.end(files.get(url.pathname));return}
  res.setHeader("Content-Type","text/html");let html=await readFile(path.join(root,"tools/fixtures/project-assistants-ui-alignment.html"),"utf8");if(url.searchParams.has("baseline"))html=html.replaceAll("alignment.","before.");res.end(html)
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

async function hit(selector){
  const point=await evaluate(`(()=>{const el=document.querySelector(${JSON.stringify(selector)});if(!el)throw Error("Missing control: "+${JSON.stringify(selector)});el.scrollIntoView({block:"center"});const r=el.getBoundingClientRect();return {x:r.x+r.width/2,y:r.y+r.height/2,disabled:el.disabled}})()`)
  assert.equal(!!point.disabled,false,selector)
  await send("Input.dispatchMouseEvent",{type:"mousePressed",x:point.x,y:point.y,button:"left",clickCount:1},session)
  await send("Input.dispatchMouseEvent",{type:"mouseReleased",x:point.x,y:point.y,button:"left",clickCount:1},session)
  operations.push({op:"hit",selector})
}
async function fill(selector,value){await evaluate(`(()=>{const el=document.querySelector(${JSON.stringify(selector)});if(!el||el.disabled)throw Error("Missing enabled field");el.focus();Object.getOwnPropertyDescriptor(el.tagName==="TEXTAREA"?HTMLTextAreaElement.prototype:HTMLInputElement.prototype,"value").set.call(el,${JSON.stringify(value)});el.dispatchEvent(new Event("input",{bubbles:true}));el.dispatchEvent(new Event("change",{bubbles:true}))})()`);operations.push({op:"fill",selector,value})}
async function key(key,code=key){await send("Input.dispatchKeyEvent",{type:"keyDown",key,code},session);await send("Input.dispatchKeyEvent",{type:"keyUp",key,code},session);operations.push({op:"key",key})}
async function capture(name){await browser("screenshot","--out",path.join(artifacts,name+".jpg"));await writeFile(path.join(artifacts,name+".html"),await evaluate("document.documentElement.outerHTML"))}
async function open(query="",mobile=false){await browser("open",origin+"/?"+query);await browser("viewport",mobile?"mobile":"desktop");await waitFor(query.includes("room")?'!!document.querySelector(".pa-chat-footer textarea")':query.includes("read")?'!!document.querySelector(".pa-composer textarea")':'!!document.querySelector(".pa-composer textarea:not(:disabled)")')}
async function close(){await browser("close");tab=undefined}
const posts=()=>requests.filter(r=>r.method!=="GET")
const answers=()=>requests.filter(r=>r.path.endsWith("/answer"))
async function requestApproval(text){await fill(".pa-composer textarea",text);await hit(".pa-send");await waitFor('!!document.querySelector(".pa-permission")')}
async function expandApproval(){await waitFor('!!document.querySelector(".pa-permission button")');await hit(".pa-permission button");await waitFor('document.querySelectorAll(".pa-permission [data-option-kind]").length===4');await hit(".pa-permission-detail summary")}
try{
  await connect()
  if(process.env.BOTS_BASELINE){
    await open("baseline=1");await capture("before-chat");assert.equal(await evaluate('!!document.querySelector(".pa-info-toggle")'),false)
    await browser("click","More actions");await browser("click","Memory");await waitFor('!!document.querySelector(".pa-memory textarea")');await capture("before-memory");await close()
    await open("baseline=1&room=1");await waitText("Synthetic room question");await capture("before-room");await close()
    results.push({baselineMissingInspector:true})
  }
  await open();const initialPosts=posts().length
  await hit(".pa-info-toggle");await waitFor('document.activeElement?.getAttribute("role")==="tab"');await capture("after-details")
  await key("End");await waitFor('document.querySelector("[role=tab][aria-selected=true]")?.textContent==="Bot settings"')
  const name='.pa-info-panel [role="tabpanel"]:not([hidden]) label input:not([type="checkbox"])'
  await fill(name,"Synthetic draft name");await hit('.pa-info-tabs [role="tab"]:nth-child(2)');await waitText("No shared files or artifacts yet")
  await capture("after-library");await hit('.pa-info-tabs [role="tab"]:nth-child(4)');assert.equal(await evaluate(`document.querySelector(${JSON.stringify(name)}).value`),"Synthetic draft name")
  await fill(name,"Synthetic saved name");await browser("click","Save");await waitFor('document.querySelector(".pa-info-panel").hidden');assert.ok(posts().some(r=>r.method==="PATCH"&&r.status===200))
  await hit(".pa-info-toggle");await hit('.pa-info-tabs [role="tab"]:nth-child(4)');assert.equal(await evaluate(`document.querySelector(${JSON.stringify(name)}).value`),"Synthetic saved name");await capture("after-settings")
  await hit('.pa-info-tabs [role="tab"]:nth-child(3)');await waitFor('!!document.querySelector(".pa-memory textarea")');await fill(".pa-memory textarea","Synthetic durable memory");await hit(".pa-memory form button[type=submit]");await waitText("Synthetic durable memory");await waitFor('document.querySelector(".pa-memory textarea").value===""');await capture("after-memory")
  const memoryPosts=posts().filter(r=>r.path.endsWith("/memory"));assert.equal(memoryPosts.length,1);assert.equal(memoryPosts[0].status,200)
  await browser("viewport","mobile");await capture("after-phone-memory");assert.equal(await evaluate('document.documentElement.scrollWidth<=innerWidth'),true)
  await key("Escape");await waitFor('document.querySelector(".pa-info-panel").hidden');assert.equal(await evaluate('document.activeElement.classList.contains("pa-info-toggle")'),true)
  await browser("viewport","desktop")
  for(const [kind,id] of [["allow_once","native-once"],["allow_always","native-always"],["reject_once","native-deny"]]){
    await requestApproval("approval "+kind);
    if(kind==="allow_once"&&process.env.BOTS_BASELINE){await close();await open("baseline=1");await capture("before-permission-summary");await hit(".pa-permission button");await waitFor('document.querySelectorAll(".pa-permission button").length===4');await capture("before-permission-detail");await close();await open()}
    await capture("after-permission-summary-"+kind)
    assert.equal(await evaluate('document.querySelectorAll(".pa-permission [data-option-kind]").length'),0)
    await expandApproval();await waitText("// final detail line");const cardWidth=await evaluate('document.querySelector(".pa-permission").getBoundingClientRect().width');assert.ok(cardWidth<=360);results.push({approval:kind,width:cardWidth});await capture("after-permission-detail-"+kind)
    if(kind==="reject_once"){await browser("viewport","mobile");await evaluate('document.querySelector(".pa-permission [data-option-kind=reject_always]").scrollIntoView({block:"center"})');assert.equal(await evaluate('getComputedStyle(document.querySelector(".pa-jump")).display'),"none");await capture("after-permission-phone-last-option");await browser("viewport","desktop")}
    const count=answers().length;await hit('.pa-permission [data-option-kind="'+kind+'"]');await waitText("Synthetic native answer: "+id);await waitFor('!document.querySelector(".pa-permission")');assert.equal(answers().length,count+1);assert.equal(answers().at(-1).body.option_id,id)
  }
  await requestApproval("approval boundary");await expandApproval();const count=answers().length
  await close()
  // Detail transport faults exercise the production decoder and answer preflight.
  expiredDetail=true;await open();await expandApproval();assert.equal(await evaluate('[...document.querySelectorAll(".pa-permission [data-option-kind]")].every(b=>b.disabled)'),true);await capture("after-permission-expired");assert.equal(answers().length,count);await close();expiredDetail=false
  await open("read=1");await expandApproval();assert.equal(await evaluate('[...document.querySelectorAll(".pa-permission [data-option-kind]")].every(b=>b.disabled)'),true);await capture("after-permission-readonly");await close()
  staleDetail=true;await open();await waitFor('!!document.querySelector(".pa-permission button")');await hit(".pa-permission button");await waitText("pa_stale_generation");assert.equal(answers().length,count);await capture("after-permission-stale");await close();staleDetail=false
  await open();await expandApproval();failAnswer=true;await hit('.pa-permission [data-option-kind="allow_once"]');await waitText("The answer could not be confirmed");assert.equal(answers().length,count+1);await capture("after-permission-unknown");failAnswer=false
  assert.equal(await evaluate('[...document.querySelectorAll(".pa-permission [data-option-kind]")].every(b=>b.disabled)'),true);await close()
  await open("room=1");await evaluate(`window.fixtureEvents=[];document.addEventListener("click",event=>window.fixtureEvents.push({tag:event.target.tagName,text:event.target.textContent}),true);window.addEventListener("error",event=>window.fixtureEvents.push({error:event.message}))`);await waitText("Synthetic room question");await waitFor('!!document.querySelector(".pa-room-author")');await capture("after-room");const compactHeight=await evaluate('document.querySelector(".pa-room-composer").getBoundingClientRect().height');assert.ok(compactHeight<70);await fill(".pa-chat-footer textarea","Synthetic IME draft");const compositionPosts=posts().length;await evaluate('document.querySelector(".pa-chat-footer textarea").dispatchEvent(new CompositionEvent("compositionstart",{bubbles:true}))');await key("Enter");assert.equal(posts().length,compositionPosts);await evaluate('document.querySelector(".pa-chat-footer textarea").dispatchEvent(new CompositionEvent("compositionend",{bubbles:true}))');await fill(".pa-chat-footer textarea","Synthetic line one\nSynthetic line two\nSynthetic line three");await waitFor(`document.querySelector(".pa-room-composer").getBoundingClientRect().height>${compactHeight}`);await capture("after-room-grown-composer");await fill(".pa-chat-footer textarea","");
  const loaded=await evaluate('document.querySelectorAll("[data-room-seq]").length');await browser("click","Earlier messages");await waitFor(`document.querySelectorAll("[data-room-seq]").length>${loaded}`);assert.equal(await evaluate('Number(document.querySelector("[data-room-seq]").dataset.roomSeq)'),1);await capture("after-room-history")
  await browser("click","Members (2)");await capture("after-room-members");await hit(".pa-room-member");await waitFor('document.activeElement.tagName==="TEXTAREA"');await fill(".pa-chat-footer textarea","Synthetic draft retained by Escape");await key("Escape");await waitFor('document.querySelector(".pa-room-members").hidden');assert.equal(await evaluate('document.querySelector(".pa-chat-footer textarea").value'),"Synthetic draft retained by Escape");await browser("click","Members (2)");await hit(".pa-room-members .pa-toolbar button");await waitFor('document.querySelector(".pa-room-members").hidden');await waitFor('document.activeElement.getAttribute("aria-expanded")==="false"');await browser("click","Members (2)");await fill(".pa-chat-footer textarea","Synthetic targeted reply");const before=posts().length;await hit(".pa-room-composer .pa-send");operations.push({op:"group-send-observed",state:await evaluate("({events:window.fixtureEvents,text:document.body.innerText})")});await waitText("Synthetic targeted reply");await waitFor(`document.querySelector(".pa-chat-footer textarea").value===""`)
  const sent=posts().slice(before).find(r=>r.path.endsWith("/messages"));assert.equal(sent.body.reply_to_author.bot_id,"alice");assert.equal(sent.status,200);await capture("after-room-targeted")
  await browser("viewport","mobile");await capture("after-room-phone");assert.equal(await evaluate('document.documentElement.scrollWidth<=innerWidth'),true)
  assert.equal(await evaluate('getComputedStyle(document.querySelector(".pa-room-layout>.pa-chat")).visibility'),"hidden");await evaluate('document.querySelector(".pa-room-members").focus()');await key("Escape");await waitFor('document.querySelector(".pa-room-members").hidden');await waitFor('document.activeElement.getAttribute("aria-expanded")==="false"');await browser("click","Members (2)");await key("Escape");await waitFor('document.activeElement.getAttribute("aria-expanded")==="false"')
  await capture("after-room-phone-composer");await browser("click","More actions");await browser("click","Stop group");await waitFor('!!document.querySelector(".pa-chat-footer textarea:not(:disabled)")');assert.ok(posts().some(r=>r.path.endsWith("/stop")&&r.status===200));await browser("click","More actions");await browser("click","Delete group");await waitFor('!!document.querySelector(".pa-chat-footer [role=alert]")');await capture("after-room-delete-confirmation");await hit(".pa-chat-footer [role=alert] button");await waitFor('!document.querySelector(".pa-chat-footer [role=alert]")');assert.equal(posts().filter(r=>r.method==="DELETE").length,0)
  unknownGroup=true;await fill(".pa-chat-footer textarea","Synthetic uncertain group send");await browser("click","Send");await waitText("pa_outcome_unknown");assert.equal(await evaluate('document.querySelector(".pa-chat-footer textarea").disabled'),true);const parked=posts().length;await capture("after-room-unknown");await new Promise(r=>setTimeout(r,150));assert.equal(posts().length,parked);unknownGroup=false;await close()
  await open("lang=zh-Hant&read=1",true);await hit(".pa-info-toggle");await capture("after-zh-phone-details");for(const [index,label] of [[2,"檔案庫"],[3,"記憶"],[4,"Bot 設定"]]){await hit('.pa-info-tabs [role="tab"]:nth-child('+index+')');await waitText(label);await capture("after-zh-phone-tab-"+index)}assert.equal(await evaluate('[...document.querySelectorAll(".pa-info-panel [role=tabpanel]:not([hidden]) fieldset input")].every(el=>el.matches(":disabled"))'),true);assert.equal(await evaluate('document.documentElement.scrollWidth<=innerWidth'),true);await close()
  await open("room=1&read=1&lang=zh-Hant",true);assert.equal(await evaluate('document.querySelector(".pa-chat-footer textarea").disabled'),true);await capture("after-zh-room-readonly");await close()
  results.push({composerHeight:compactHeight,historyRowsBefore:loaded,historyStartsAt:1,settingsPersisted:true,memoryPersisted:true,tabDraftPreserved:true,keyboardFocus:true,nativeOptionIdentities:["native-once","native-always","native-deny"],summaryCannotAnswer:true,expiryRefused:true,readOnlyRefused:true,unknownParked:true,groupTargetIdentity:"alice",modelRequests:0})
  console.log("PASS: production components crossed authenticated HTTP/Host/SQLite; settings/memory persisted, group target retained, exact native approval choices, keyboard/phone and expiry/read-only/unknown fences; external model requests=0")
}catch(error){if(tab)await capture("failure").catch(()=>{});throw error}
finally{
  if(tab)await close().catch(()=>{})
  await writeFile(path.join(artifacts,"operations.json"),JSON.stringify({operations,requests,results},null,2))
  ws?.close();if(chrome.pid&&chrome.exitCode===null&&chrome.signalCode===null){const exited=new Promise(resolve=>chrome.once("exit",resolve));chrome.kill("SIGTERM");await exited;}await new Promise(resolve=>server.close(resolve));await rm(profile,{recursive:true,force:true})
}
