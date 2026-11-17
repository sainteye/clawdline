/** Same Store → HTTP SSE → actual SourceClient → Bots, without a provider. */
import assert from "node:assert/strict"
import {createServer} from "node:http"
import {mkdir,writeFile} from "node:fs/promises"
import {fileURLToPath,pathToFileURL} from "node:url"
import path from "node:path"
import {build} from "esbuild"

const {BOTS_ORIGIN:upstream,BOTS_TOKEN:token,BOTS_MACHINE:machine,BOTS_ARTIFACTS:artifacts}=process.env
if(!upstream||!token||!machine||!artifacts||!process.env.PLAYWRIGHT_MODULE)throw new Error("Requires the owned live HTTP fixture and installed Playwright")
await mkdir(artifacts,{recursive:true})
const {chromium}=await import(pathToFileURL(process.env.PLAYWRIGHT_MODULE).href)
const built=await build({entryPoints:[fileURLToPath(new URL("./roster-live.fixture.tsx",import.meta.url))],bundle:true,write:false,outfile:"roster.js",format:"esm",platform:"browser",jsx:"automatic",define:{"process.env.NODE_ENV":'"development"'}})
const files=new Map(built.outputFiles.map(f=>["/"+path.basename(f.path),f.contents]))
const active=new Map(),blocked=new Set(),connections=[],frames=[],evidence=[],pageErrors=[]
let listRequests=0,maxPrimaryConnections=0,primaryConnections=0
const server=createServer(async(req,res)=>{
  const url=new URL(req.url,"http://fixture")
  if(url.pathname.startsWith("/v1/")||url.pathname==="/fixture/phase"){
    const controller=new AbortController(),reader=req.headers["x-fixture-reader"]??"reader"
    const stream=url.pathname==="/v1/project-assistants/roster-events"
    if(url.pathname==="/v1/project-assistants")listRequests++
    req.on("aborted",()=>controller.abort());res.on("close",()=>controller.abort())
    let tracked=false
    try{
      if(stream){
        connections.push({reader,cursor:url.searchParams.get("cursor")??"",blocked:blocked.has(reader)})
        if(blocked.has(reader)){res.writeHead(503,{"Content-Type":"application/json"});res.end('{"offline":true}');return}
      }
      const headers={Authorization:`Bearer ${token}`}
      for(const key of ["accept","content-type","idempotency-key","x-fixture-reader"])if(typeof req.headers[key]==="string")headers[key]=req.headers[key]
      const chunks=[]
      if(req.method!=="GET"&&req.method!=="HEAD")for await(const chunk of req)chunks.push(chunk)
      const answer=await fetch(upstream+req.url,{method:req.method,headers,signal:controller.signal,...(chunks.length?{body:Buffer.concat(chunks)}:{})})
      res.writeHead(answer.status,{"Content-Type":answer.headers.get("Content-Type")??"application/json"})
      if(!stream||!answer.ok){res.end(Buffer.from(await answer.arrayBuffer()));return}
      const streams=active.get(reader)??new Set();streams.add(res);active.set(reader,streams);tracked=true
      if(reader==="reader"){primaryConnections++;maxPrimaryConnections=Math.max(maxPrimaryConnections,primaryConnections)}
      const decoder=new TextDecoder();let pending=""
      for await(const chunk of answer.body){
        pending+=decoder.decode(chunk,{stream:true})
        for(let end;(end=pending.indexOf("\n\n"))>=0;){
          const raw=pending.slice(0,end+2);pending=pending.slice(end+2)
          const data=raw.split("\n").filter(s=>s.startsWith("data:")).map(s=>s.slice(5).trimStart()).join("\n")
          if(data)frames.push({reader,raw,frame:JSON.parse(data)})
        }
        if(res.destroyed)break
        res.write(chunk)
      }
      res.end()
    }catch(error){
      if(!controller.signal.aborted&&!res.destroyed){if(!res.headersSent)res.writeHead(500);res.end(String(error))}
    }finally{
      controller.abort()
      if(tracked){active.get(reader)?.delete(res);if(reader==="reader")primaryConnections--}
    }
    return
  }
  if(url.pathname==="/fixture.json"){res.setHeader("Content-Type","application/json");res.end(JSON.stringify({machine}));return}
  if(files.has(url.pathname)){res.setHeader("Content-Type",url.pathname.endsWith(".css")?"text/css":"text/javascript");res.end(files.get(url.pathname));return}
  res.setHeader("Content-Type","text/html");res.end('<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><style>body{margin:0;font-family:system-ui;background:#080808;color:#f2f2f2}</style><link rel="stylesheet" href="/roster.css"><div id="app"></div><script type="module" src="/roster.js"></script>')
})
await new Promise(resolve=>server.listen(0,"127.0.0.1",resolve))
const origin=`http://127.0.0.1:${server.address().port}`,browser=await chromium.launch({headless:true})
const phase=async(name,bot="reading")=>{
  const response=await fetch(upstream+"/fixture/phase?"+new URLSearchParams({phase:name,bot}),{method:"POST"})
  assert.equal(response.status,204,await response.text())
}
const state=async(page,id,wanted)=>page.waitForFunction(({key,wanted})=>document.querySelector(`[data-bot-key='${key}']`)?.dataset.status===wanted,{key:JSON.stringify([machine,id]),wanted},{timeout:15000})
const unread=async row=>Number(await row.locator(".pa-unread").textContent().catch(()=>"0"))
const waitUnread=async(page,id,count)=>page.waitForFunction(({key,count})=>Number(document.querySelector(`[data-bot-key='${key}'] .pa-unread`)?.textContent??0)===count,{key:JSON.stringify([machine,id]),count},{timeout:15000})
try{
  for(const [language,width,height] of [["en",1280,900],["zh-Hant",390,844]]){
    const page=await browser.newPage({viewport:{width,height}}),errors=[]
    page.on("pageerror",e=>errors.push(String(e)))
    let navigations=0;page.on("framenavigated",frame=>{if(frame===page.mainFrame())navigations++})
    if(language!=="en")await phase("reset")
    await page.goto(origin+"/?lang="+language)
    const row=id=>page.locator(`[data-bot-key='${JSON.stringify([machine,id])}']`)
    await row("reading").getByText("Read notes.md",{exact:true}).waitFor({timeout:15000})
    await state(page,"reading","working");await state(page,"approval","needsInput");await state(page,"failed","error");await state(page,"finished","idle");await state(page,"unknown","error")
    assert.equal(await row("approval").locator(".pa-attention").count(),1)
    assert.equal(await row("approval").locator(".pa-unread-dot").count(),0)
    assert.ok(await unread(row("approval"))>0)
    await page.screenshot({path:path.join(artifacts,`${language}-live-baseline.png`),fullPage:true})

    await phase("writing")
    await row("reading").getByText(language==="en"?"Writing a reply…":"正在回覆…",{exact:true}).waitFor({timeout:15000})
    const avatar=row("reading").locator("svg.pa-avatar"),first=await avatar.screenshot();let animated=false
    for(let sample=0;sample<6;sample++){await page.waitForTimeout(200);if(!first.equals(await avatar.screenshot())){animated=true;break}}
    assert.ok(animated,"Original line avatar moves on an actual live working update")
    await page.emulateMedia({reducedMotion:"reduce"});await page.evaluate(()=>new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve))))
    const still=await avatar.screenshot();await page.waitForTimeout(150)
    assert.ok(still.equals(await avatar.screenshot()),"Reduced motion keeps the line drawing still")
    await page.emulateMedia({reducedMotion:"no-preference"})
    await phase("waiting");await state(page,"reading","needsInput")
    await row("reading").getByText(language==="en"?"Needs approval: Run unit tests":"需要允許：Run unit tests",{exact:true}).waitFor({timeout:15000})
    await page.screenshot({path:path.join(artifacts,`${language}-live-approval.png`),fullPage:true})
    await phase("resume");await state(page,"reading","working")
    await phase("done");await state(page,"reading","idle")
    await row("reading").getByText("Fresh reply",{exact:true}).waitFor({timeout:15000})
    const count=await unread(row("reading"));assert.ok(count>0)
    const duplicate=frames.findLast(x=>x.reader==="reader"&&x.frame.changes?.some(c=>c.key.bot_id==="reading"&&c.summary?.latest_message==="Fresh reply"))
    assert.ok(duplicate)
    for(const response of active.get("reader")??[])response.write(duplicate.raw)
    await page.waitForTimeout(100);assert.equal(await unread(row("reading")),count,"Exact wire replay cannot accumulate unread")

    const other=await browser.newPage({viewport:{width,height}});other.on("pageerror",e=>errors.push(String(e)))
    await other.goto(origin+"/?"+new URLSearchParams({lang:language,reader:"second-reader",readonly:"1"}))
    const otherRow=other.locator(`[data-bot-key='${JSON.stringify([machine,"reading"])}']`)
    await waitUnread(other,"reading",count)
    await phase("read");await waitUnread(page,"reading",0)
    assert.equal(await unread(otherRow),count,"Default-reader observation leaves the other reader unread")
    await other.close()

    await phase("start");await state(page,"reading","working")
    const applied=frames.findLast(x=>x.reader==="reader"&&x.frame.cursor)?.frame.cursor
    assert.ok(applied)
    const reconnectStart=connections.length
    blocked.add("reader")
    for(const response of active.get("reader")??[])response.end()
    await page.locator(".pa-connection-notice").waitFor({timeout:15000});await state(page,"reading","idle")
    assert.equal(await row("reading").count(),1,"Offline rows remain available as stale observations")
    await phase("done")
    blocked.delete("reader")
    await row("reading").getByText("Fresh reply",{exact:true}).waitFor({timeout:15000})
    await state(page,"reading","idle")
    await page.waitForFunction(()=>!document.querySelector(".pa-source-error"))
    const resumed=connections.slice(reconnectStart).find(x=>x.reader==="reader"&&!x.blocked)
    assert.ok(resumed,"A dropped live connection reconnects")
    assert.equal(resumed.cursor,applied,"Reconnect starts at the last applied durable cursor")
    await page.screenshot({path:path.join(artifacts,`${language}-live-reconnected.png`),fullPage:true})

    await phase("hide");await row("reading").waitFor({state:"detached",timeout:15000})
    const hidden=page.locator(".pa-hidden-bots")
    await hidden.locator(".pa-hidden-toggle").click()
    await hidden.getByText("Reading helper",{exact:true}).waitFor()
    await hidden.locator("li button").click()
    await row("reading").getByText("Fresh reply",{exact:true}).waitFor({timeout:15000})
    await phase("delete");await row("reading").waitFor({state:"detached",timeout:15000})
    assert.equal(await page.locator(".pa-hidden-bots li").count(),0)
    assert.equal(listRequests,0,"The API71 list never falls back to polling List")
    assert.equal(navigations,1,"All changes arrive without page navigation or reload")
    assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false)
    await phase("revoked")
    await page.locator(".pa-bot-row").first().waitFor({state:"detached",timeout:15000})
    assert.equal(await page.locator(".pa-bot-row").count(),0,"Revocation clears retained rows")
    assert.deepEqual(errors,[]);pageErrors.push(...errors)
    evidence.push({language,width,height,states:5,eventUpdates:true,navigations,animatedLine: animated,reducedMotion:true,permissionWinsUnreadDot:true,absoluteReplayUnread:true,readerIsolation:true,reconnectCursor:true,staleRowsRetained:true,hiddenManagement:true,deletion:true,revocationClears:true,listRequests,pageErrors:errors.length})
    await page.close()
    const closedBy=Date.now()+5000
    while(primaryConnections&&Date.now()<closedBy)await new Promise(resolve=>setTimeout(resolve,10))
    assert.equal(primaryConnections,0,"Closing the page releases its live connection")
  }
  assert.equal(maxPrimaryConnections,1,"Each source maintains at most one live connection")
  await writeFile(path.join(artifacts,"browser-evidence.json"),JSON.stringify({modelRequests:0,providerRequests:0,localOnly:true,listRequests,maxPrimaryConnections,evidence},null,2)+"\n")
}finally{
  await browser.close();for(const streams of active.values())for(const response of streams)response.end()
  server.closeAllConnections();await new Promise(resolve=>server.close(resolve))
}
