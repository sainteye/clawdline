/** Bot request cards on the real Board page and beside a real work result card, at 1280 and 390 (docs/bot-request-cards.md §9). */
// Run: BOT_REQUEST_ARTIFACTS=<dir> node tools/bot-request-cards.e2e.mjs   (CHROME names the browser binary)
// The fixture serves a fixed set of /v1 routes and refuses every other one with 404; the Board must not need more.
import assert from "node:assert/strict"
import {spawn} from "node:child_process"
import {createServer} from "node:http"
import {mkdir,mkdtemp,readFile,rm,writeFile} from "node:fs/promises"
import {tmpdir} from "node:os"
import {fileURLToPath} from "node:url"
import path from "node:path"
import {build} from "esbuild"

const artifacts=process.env.BOT_REQUEST_ARTIFACTS
assert.ok(artifacts,"Set BOT_REQUEST_ARTIFACTS")
const chrome=process.env.CHROME||"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
await mkdir(artifacts,{recursive:true})
const root=fileURLToPath(new URL("..",import.meta.url))
const styles=(await readFile(path.join(root,"src/main.tsx"),"utf8")).split("\n").filter(line=>/^import "\.\/.*\.css"$/.test(line)).map(line=>line.replace('"./','"./src/')).join("\n")

const bundle=await build({stdin:{contents:`
import React,{useState} from "react";import {createRoot} from "react-dom/client";
${styles}
import "./src/pages/work/work.css";import "./src/project-assistants/styles.css";import "./src/project-assistants/chat-presentation.css";
import {WorkV2Page} from "./src/pages/work/WorkV2";
import {BotRequestDetail,BotRequestLine,useBotRequests} from "./src/project-assistants/BotRequestCard";
import {placeBotRequests} from "./src/project-assistants/bot-request";
import {WorkResultCard} from "./src/project-assistants/WorkResultCard";
import {OPEN_BOT,publishBotDirectory} from "./src/project-assistants/open-bot";
import {words} from "./src/project-assistants/words";
import {bootCatalog} from "./src/catalog";
window.fixtureErrors=[];window.addEventListener("error",e=>window.fixtureErrors.push(e.message));window.addEventListener("unhandledrejection",e=>window.fixtureErrors.push(String(e.reason)));
const q=new URL(location.href).searchParams;localStorage.setItem("ui_language",q.get("lang")||"zh-Hant");await bootCatalog(tag=>"/catalog/"+tag+".json");
const bot=(id,name,shape,color)=>({bot:{key:{machine_id:"m",bot_id:id},name,appearance:{shape,color},project:{kind:"personal"}}});
const roster=[bot("atlas","Atlas","blob","#5b8def"),bot("nova","Nova","hex","#e0a458"),bot("orion","Orion 很長很長的名字的整合小幫手","pebble","#5f9e73")];
publishBotDirectory(roster);
window.openedBots=[];window.addEventListener(OPEN_BOT,e=>window.openedBots.push(e.detail.key.bot_id));
const links=[{id:"l1",task_id:"task-1",title:"整理十一月的數據",state:"completed",delivery_commit:"1a2b3c4d5e6f7a8b",board_item_id:"",bot_request_id:"card-progress",landing:"pending"},
  {id:"l2",task_id:"task-2",title:"沒有交辦卡的工作",state:"executing",board_item_id:""}];
function ChatHarness(){const w=words(q.get("lang")||"zh-Hant"),{reading,refresh}=useBotRequests({enabled:true,bot:"atlas"}),[open,setOpen]=useState();
  const placed=placeBotRequests(reading.state==="ready"?reading.cards:[],links);
  return <section className="project-assistants" style={{maxWidth:720,margin:"0 auto",padding:16}} data-chat-harness={reading.state}><div className="pa-chat-surface">
    {links.map(link=><React.Fragment key={link.id}>{placed.above.get(link.id)&&<BotRequestLine card={placed.above.get(link.id)} w={w} onOpen={setOpen}/>}<WorkResultCard work={link} w={w}/></React.Fragment>)}
    {placed.loose.length>0&&<div className="pa-request-loose">{placed.loose.map(card=><BotRequestLine key={card.id} card={card} w={w} onOpen={setOpen}/>)}</div>}
    {open&&<BotRequestDetail card={open} bots={roster.map(item=>item.bot)} links={placed.links.get(open.id)} w={w} language="zh-Hant" onClose={()=>{setOpen(undefined);refresh()}} onChanged={refresh}/>}
  </div></section>}
createRoot(document.getElementById("app")).render(q.get("view")==="chat"?<ChatHarness/>:<WorkV2Page shown/>);
document.documentElement.dataset.ready="1";
`,resolveDir:root,loader:"tsx"},bundle:true,external:["/*.webp","/*.jpg"],write:false,outfile:"bot-request-cards.js",format:"esm",target:"es2022",platform:"browser",jsx:"automatic",
  loader:{".svg":"dataurl",".png":"dataurl",".woff2":"dataurl",".woff":"dataurl",".ttf":"dataurl"},define:{"process.env.NODE_ENV":'"development"',"import.meta.env":"{}"}})
const files=new Map(bundle.outputFiles.map(f=>["/"+path.basename(f.path),f.contents]))

const minutes=n=>new Date(Date.now()-n*60_000).toISOString()
const base={project_id:"p1",project_path:"/fixture/p1",created_by:"atlas",authority:[],cycle:1,version:3,state:"open",checklist:[],done:0,total:0}
const freshCards=()=>[
  {...base,id:"card-progress",title:"發布十一月的營運報告，並把連結貼到團隊的公告頻道裡讓大家都看得到",progress_state:"in_progress",baton:{bot:"nova",role:"reviewer"},chain:["atlas","nova"],
    created_at:minutes(300),updated_at:minutes(5),done:2,total:3,board_item_id:"item-1",description:"月底前發出去。",
    checklist:[{key:"draft",label:"寫好草稿",state:"done",note:"commit 1a2b3c4d https://example.com/reports/2026-11",reported_by:"atlas",reported_at:minutes(120)},
      {key:"review",label:"請 Nova 檢查數字",state:"open"},
      {key:"translate",label:"翻成英文",state:"not_needed",note:"這次只發中文版",reported_by:"nova",reported_at:minutes(60)}],
    verdicts:[{id:"v1",verdict:"conditional_pass",reviewer_bot:"nova",at:minutes(30),commit:"1a2b3c4d5e6f",text:"第三節的數字要再對一次"}]},
  {...base,id:"card-waiting",title:"訂下週三的會議室",progress_state:"waiting_for_you",waiting:"q-1",baton:{bot:"atlas",role:"maker"},chain:["atlas"],created_at:minutes(200),updated_at:minutes(90)},
  {...base,id:"card-done",title:"整理客服常見問題",state:"done",progress_state:"done",baton:{bot:"orion",role:"integrator"},chain:["atlas","orion"],created_at:minutes(3000),updated_at:minutes(1500),closed_at:minutes(1500),done:1,total:1,
    checklist:[{key:"faq",label:"FAQ 上線",state:"done",note:"https://example.com/faq",reported_by:"orion",reported_at:minutes(1500)}]},
  {...base,id:"card-cancelled",title:"不做了的實驗",state:"cancelled",progress_state:"cancelled",baton:{bot:"gone",role:"maker"},chain:["gone"],created_at:minutes(9000),updated_at:minutes(4400),closed_at:minutes(4400)},
]
const eventsOf=card=>[{id:1,kind:"card.created",actor:"bot:"+card.chain[0],at:card.created_at,version:1},
  ...card.chain.slice(1).map((_,i)=>({id:2+i,kind:"baton.handed",actor:"bot:"+card.chain[i],at:minutes(100-i),version:2+i,detail:"請接手，草稿在 https://example.com/draft"}))]
const item=(id,title,area,phase)=>({id,project:{id:"p1",label:"fixture",path:"/fixture/p1",icon:null,available:true},kind:"feature",title,description:"",acceptance_criteria:"",acceptance_version:1,acceptance_digest:"",
  gate_snapshot_cycle:0,gate_snapshot_at:null,planning_gate:false,verify_gate:false,phase,condition:null,user_action:"",area,deployment_policy:"not_required",review_required:false,owner_session:null,
  created_at:1795000000,updated_at:1795000000,closed_at:null,cycle:1,version:1})
const boardItems=[item("item-1","十一月營運報告","implementing","implementing"),item("item-2","修正登入頁","unassigned","created"),item("item-3","新的儀表板","verifying","verifying")]
let cards=freshCards(),routes=true,conflictOnce=true
const refused=[],writes=[]
const server=createServer(async(req,res)=>{try{
  const url=new URL(req.url,"http://fixture"),chunks=[];for await(const chunk of req)chunks.push(chunk)
  const json=(status,body)=>{res.writeHead(status,{"Content-Type":"application/json"});res.end(JSON.stringify(body))}
  if(url.pathname==="/fixture/reset"){cards=freshCards();routes=url.searchParams.get("routes")!=="off";conflictOnce=true;refused.length=0;writes.length=0;return json(200,{})}
  if(url.pathname.startsWith("/v1/bot-request/cards")&&routes){
    const [id,action]=url.pathname.slice("/v1/bot-request/cards".length).split("/").filter(Boolean),card=cards.find(c=>c.id===id)
    if(!id)return json(200,{cards:cards.filter(c=>!url.searchParams.get("bot")||c.chain.includes(url.searchParams.get("bot")))})
    if(!card)return json(404,{error:"bot_request_not_found",detail:"id"})
    if(req.method==="GET")return json(200,{...card,events:eventsOf(card)})
    const body=JSON.parse(Buffer.concat(chunks));writes.push({id,action,body,key:req.headers["idempotency-key"]})
    if(action==="complete"&&conflictOnce){conflictOnce=false;card.version++;return json(409,{error:"bot_request_version_conflict",detail:"expected_version",version:card.version})}
    if(body.expected_version!==card.version)return json(409,{error:"bot_request_version_conflict",detail:"expected_version",version:card.version})
    card.version++;card.updated_at=new Date().toISOString()
    if(action==="complete"){card.state="done";card.progress_state="done"}
    if(action==="cancel"){card.state="cancelled";card.progress_state="cancelled"}
    if(action==="handoff"){card.baton={bot:body.to_bot,role:body.role};if(!card.chain.includes(body.to_bot))card.chain.push(body.to_bot)}
    return json(200,{card:{...card,events:eventsOf(card)},changed:true})
  }
  if(url.pathname==="/v1/work/v2/items")return json(200,{ok:true,rows:boardItems,counts:{open:boardItems.length},next_cursor:null,page_size:100,truncated:false})
  if(url.pathname==="/v1/work/v2/proposals")return json(200,{ok:true,rows:[]})
  if(url.pathname==="/v1/work/decisions")return json(200,{counts:{},rows:[],next_cursor:null})
  if(url.pathname==="/v1/places")return json(200,{places:[{id:"p1",label:"fixture",path:"/fixture/p1"}]})
  if(url.pathname.startsWith("/v1/")){refused.push(req.method+" "+url.pathname);return json(404,{error:{code:"not_found",message:"The fixture does not serve this route."}})}
  if(url.pathname.startsWith("/catalog/")){res.setHeader("Content-Type","application/json");res.end(await readFile(path.join(root,"public/catalogs",path.basename(url.pathname))));return}
  if(files.has(url.pathname)){res.setHeader("Content-Type",url.pathname.endsWith(".css")?"text/css":"text/javascript");res.end(files.get(url.pathname));return}
  res.setHeader("Content-Type","text/html");res.end('<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="stylesheet" href="/bot-request-cards.css"><body><div id="app"></div><script type="module" src="/bot-request-cards.js"></script>')
}catch(error){res.writeHead(500);res.end(String(error))}})
await new Promise(resolve=>server.listen(0,"127.0.0.1",resolve))
const origin=`http://127.0.0.1:${server.address().port}`
const profile=await mkdtemp(path.join(tmpdir(),"bot-request-cards-"))
const browser=spawn(chrome,["--headless=new","--disable-features=OmniboxEverywhere","--disable-gpu","--hide-scrollbars","--no-first-run","--use-mock-keychain","--remote-debugging-port=0",`--user-data-dir=${profile}`,"about:blank"],{stdio:"ignore"})
const report=[]
try{
  let port=""
  for(let i=0;i<200&&!port;i++){port=(await readFile(path.join(profile,"DevToolsActivePort"),"utf8").catch(()=>"")).split("\n")[0];if(!port)await new Promise(r=>setTimeout(r,50))}
  assert.ok(port,"the browser did not open its DevTools port")
  for(const [view,width,height] of [["desktop",1280,800],["phone",390,844]]){
    const target=await(await fetch(`http://127.0.0.1:${port}/json/new?about:blank`,{method:"PUT"})).json()
    const socket=new WebSocket(target.webSocketDebuggerUrl),waiting=new Map();let serial=0
    await new Promise((resolve,reject)=>{socket.onopen=resolve;socket.onerror=reject})
    socket.onmessage=event=>{const message=JSON.parse(event.data),done=waiting.get(message.id);if(done){waiting.delete(message.id);done(message)}}
    const send=(method,params={})=>new Promise((resolve,reject)=>{const id=++serial;waiting.set(id,message=>message.error?reject(Error(method+": "+message.error.message)):resolve(message.result));socket.send(JSON.stringify({id,method,params}))})
    const run=async expression=>{const got=await send("Runtime.evaluate",{expression,awaitPromise:true,returnByValue:true});if(got.exceptionDetails)throw Error(got.exceptionDetails.exception?.description||got.exceptionDetails.text);return got.result.value}
    const until=async(expression,what)=>{for(let i=0;i<200;i++){if(await run(`!!(${expression})`))return;await new Promise(r=>setTimeout(r,50))}throw Error(`${view}: ${what} did not happen`)}
    const shot=async name=>{const {data}=await send("Page.captureScreenshot",{format:"png"});await writeFile(path.join(artifacts,`bot-request-${name}-${width}.png`),Buffer.from(data,"base64"))}
    const fits=async what=>assert.ok(await run("document.documentElement.scrollWidth<=window.innerWidth"),`${view}: ${what} overflows sideways`)
    const inside=async(selector,what)=>assert.ok(await run(`(()=>{const r=document.querySelector(${JSON.stringify(selector)}).getBoundingClientRect();return r.left>=0&&r.right<=window.innerWidth+0.5&&r.top>=0&&r.bottom<=window.innerHeight+0.5})()`),`${view}: ${what} is cut off`)
    const click=selector=>run(`document.querySelector(${JSON.stringify(selector)}).click()`)
    const type=(selector,value)=>run(`(()=>{const el=document.querySelector(${JSON.stringify(selector)});Object.getOwnPropertyDescriptor(Object.getPrototypeOf(el),"value").set.call(el,${JSON.stringify(value)});el.dispatchEvent(new Event(el.tagName==="SELECT"?"change":"input",{bubbles:true}))})()`)
    const counts=()=>run(`JSON.stringify({counts:[...document.querySelectorAll(".work-count")].map(n=>n.textContent),lanes:[...document.querySelectorAll(".work-desktop-lane,.work-section:not(.bot-work)")].map(n=>n.querySelectorAll(".work-card,.work-brief-card").length),attention:document.querySelectorAll(".work-attention,[data-lane=attention] .work-card").length})`)
    await send("Runtime.enable");await send("Page.enable")
    await send("Emulation.setDeviceMetricsOverride",{width,height,deviceScaleFactor:2,mobile:view==="phone"})
    const open=async query=>{await send("Page.navigate",{url:`${origin}/?lang=zh-Hant${query}`});await until("document.documentElement?.dataset.ready==='1'","the fixture")}

    // An origin without the card routes: the Board renders and the section is absent.
    await fetch(origin+"/fixture/reset?routes=off")
    await open("")
    await until("document.querySelectorAll('.work-count').length>0&&!document.querySelector('#work[aria-busy]')","the Board without card routes")
    assert.equal(await run("document.querySelectorAll('.bot-work').length"),0,`${view}: the section is hidden without the routes`)
    const without=await counts()

    await fetch(origin+"/fixture/reset")
    await open("")
    await until("document.querySelectorAll('.bot-work-rows .pa-request-row').length===4","the Bot work rows")
    await until("!document.querySelector('#work[aria-busy]')","the Board")
    assert.deepEqual(JSON.parse(await run("JSON.stringify([...document.querySelectorAll('.pa-request-row')].map(n=>n.dataset.requestId))")),["card-waiting","card-progress","card-done","card-cancelled"],`${view}: waiting first`)
    const first=await run("document.querySelector('.pa-request-row').innerText")
    assert.match(first,/Atlas/);assert.match(first,/等你/);assert.match(first,/最後更新 1 小時前/)
    assert.match(await run("document.querySelector('[data-request-id=card-progress]').innerText"),/Nova[^]*進行中[^]*2\/3 ✓[^]*最後更新 5 分鐘前/)
    await fits("the Board with Bot work")
    const shownCounts=await counts()
    assert.equal(shownCounts,without,`${view}: Board counts are the same with and without card routes`)
    await shot("board")
    await click(".bot-work-switch input")
    await until("!document.querySelector('.bot-work-rows')","hiding the section")
    assert.equal(await counts(),shownCounts,`${view}: Board counts are the same with the section off`)
    await open("")
    await until("document.querySelector('.bot-work-switch input')&&!document.querySelector('#work[aria-busy]')","the Board after a reload")
    assert.equal(await run("document.querySelector('.bot-work-switch input').checked"),false,`${view}: the switch is remembered`)
    assert.equal(await run("document.querySelectorAll('.bot-work-rows').length"),0)
    await click(".bot-work-switch input")
    await until("document.querySelectorAll('.pa-request-row').length===4","showing the section again")
    assert.equal(await counts(),shownCounts,`${view}: Board counts are the same with the section back on`)

    // The detail: history, checklist, verdict, Board item; complete asks for a reason, meets a version conflict, then succeeds.
    await click("[data-request-id=card-progress].pa-request-row")
    await until("document.querySelector('dialog.pa-request-detail[open] .pa-request-history li')","the card detail")
    const detail=await run("document.querySelector('dialog.pa-request-detail').innerText")
    assert.match(detail,/Atlas → Nova/);assert.match(detail,/由 Atlas 於[^]*回報/);assert.match(detail,/不需要[^]*這次只發中文版/);assert.match(detail,/有條件通過/);assert.match(detail,/十一月營運報告/)
    assert.doesNotMatch(detail,/核准|拒絕/)
    assert.equal(await run("document.querySelector('.pa-request-checklist a').href"),"https://example.com/reports/2026-11")
    await inside("dialog.pa-request-detail","the card detail");await fits("the card detail")
    await shot("detail")
    await run("[...document.querySelectorAll('.pa-request-actions button')].find(b=>b.textContent==='完成').click()")
    await until("document.querySelector('.pa-request-form textarea')","the reason form")
    assert.equal(await run("document.querySelector('.pa-request-actions button[type=submit]').disabled"),true,`${view}: completing with open entries needs a reason`)
    await type(".pa-request-form textarea","數字已經口頭確認")
    await click(".pa-request-actions button[type=submit]")
    await until("document.querySelector('.pa-request-failure')","the version conflict")
    assert.match(await run("document.querySelector('.pa-request-failure').textContent"),/這張卡剛剛有變動，已經重新載入/)
    await inside("dialog.pa-request-detail","the refusal")
    await shot("refusal")
    await until("!document.querySelector('.pa-request-actions button[type=submit]').disabled","the reread after the conflict")
    await click(".pa-request-actions button[type=submit]")
    await until("document.querySelector('dialog.pa-request-detail .pa-request-summary [data-progress=done]')","the completion")
    assert.equal(await run("[...document.querySelectorAll('.pa-request-actions button')].some(b=>b.textContent==='取消交辦')"),false,`${view}: a done card offers no write`)
    assert.deepEqual(writes.map(w=>[w.action,w.body.expected_version,w.body.reason]),[["complete",3,"數字已經口頭確認"],["complete",4,"數字已經口頭確認"]])
    assert.notEqual(writes[0].key,writes[1].key,"a refused write does not reuse its key")
    await click(".pa-request-close")
    await until("!document.querySelector('dialog.pa-request-detail')","closing the detail")
    await until("document.querySelector('[data-request-id=card-progress].pa-request-row').dataset.progress==='done'","the row after the completion")

    // A waiting card has one extra action, and it opens the holder's chat.
    await click("[data-request-id=card-waiting].pa-request-row")
    await until("document.querySelector('dialog.pa-request-detail[open] .pa-request-primary')","the waiting card")
    assert.equal(await run("document.querySelector('.pa-request-actions .pa-request-primary').textContent"),"到 Atlas 的聊天回覆")
    await shot("waiting")
    await click(".pa-request-actions .pa-request-primary")
    await until("window.openedBots.length===1&&!document.querySelector('dialog.pa-request-detail')","opening the chat")
    assert.equal(await run("window.openedBots[0]"),"atlas")
    // Hand a card on, then cancel it: each is one write with the version the person saw.
    writes.length=0
    await click("[data-request-id=card-waiting].pa-request-row")
    await until("document.querySelector('dialog.pa-request-detail[open]')","the card again")
    await run("[...document.querySelectorAll('.pa-request-actions button')].find(b=>b.textContent==='交給其他 Bot').click()")
    await until("document.querySelector('.pa-request-form select')","the handoff form")
    await type(".pa-request-form select","nova");await type(".pa-request-form textarea","請幫忙訂")
    await inside("dialog.pa-request-detail","the handoff form")
    await shot("handoff")
    await click(".pa-request-actions button[type=submit]")
    await until("document.querySelector('dialog.pa-request-detail .pa-request-holder-name').textContent==='Nova'","the handoff")
    await run("[...document.querySelectorAll('.pa-request-actions button')].find(b=>b.textContent==='取消交辦').click()")
    await until("document.querySelector('.pa-request-form p')","the cancel confirmation")
    assert.match(await run("document.querySelector('.pa-request-form p').textContent"),/要取消這張交辦卡嗎/)
    await click(".pa-request-actions button[type=submit]")
    await until("document.querySelector('dialog.pa-request-detail .pa-request-summary [data-progress=cancelled]')","the cancellation")
    assert.deepEqual(writes.map(w=>[w.action,w.body.expected_version,w.body.to_bot??""]),[["handoff",3,"nova"],["cancel",4,""]])
    assert.deepEqual(JSON.parse(await run("JSON.stringify(window.fixtureErrors)")),[],`${view}: page errors`)
    const boardRefused=[...new Set(refused)]

    // Beside a real work result card: the card's line sits above its work, and a chat without cards is unchanged.
    await fetch(origin+"/fixture/reset")
    await open("&view=chat")
    await until("document.querySelector('[data-chat-harness=ready] .pa-request-line')","the chat line")
    assert.ok(await run("(()=>{const line=document.querySelector('.pa-request-line[data-request-id=card-progress]'),work=document.querySelectorAll('.pa-work-task');return line.getBoundingClientRect().bottom<=work[0].getBoundingClientRect().top&&line.nextElementSibling===work[0]})()"),`${view}: the line is above its work result card`)
    assert.equal(await run("document.querySelectorAll('.pa-work-task').length"),2)
    assert.equal(await run("document.querySelectorAll('.pa-work-task')[1].previousElementSibling.classList.contains('pa-request-line')"),false,`${view}: work without a card has no line`)
    assert.equal(await run("document.querySelector('.pa-request-loose .pa-request-line').dataset.requestId"),"card-waiting")
    const withCards=await run("document.querySelectorAll('.pa-work-task')[1].outerHTML")
    await fits("the chat");await shot("chat")
    await click(".pa-request-line[data-request-id=card-progress]")
    await until("document.querySelector('dialog.pa-request-detail[open] .pa-request-sessions')","the detail from the chat")
    assert.match(await run("document.querySelector('.pa-request-sessions').innerText"),/整理十一月的數據/)
    await inside("dialog.pa-request-detail","the detail from the chat");await shot("chat-detail")
    await fetch(origin+"/fixture/reset?routes=off")
    await open("&view=chat")
    await until("document.querySelector('[data-chat-harness=hidden] .pa-work-task')","the chat without cards")
    assert.equal(await run("document.querySelectorAll('.pa-request-line').length"),0)
    assert.equal(await run("document.querySelectorAll('.pa-work-task')[1].outerHTML"),withCards,`${view}: a work result card renders the same with and without cards`)
    report.push({view,width,counts:JSON.parse(shownCounts),refused:boardRefused})
    socket.close()
  }
  console.log(JSON.stringify(report,null,1))
}finally{
  browser.kill();server.close()
  await new Promise(r=>setTimeout(r,300));await rm(profile,{recursive:true,force:true}).catch(()=>{})
}
