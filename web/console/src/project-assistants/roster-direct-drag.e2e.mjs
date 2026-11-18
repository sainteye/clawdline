/** Real roster with identical synthetic records; never opens a live viewer or sends a model prompt. */
import assert from "node:assert/strict"
import {createServer} from "node:http"
import {mkdir,writeFile} from "node:fs/promises"
import {execFileSync} from "node:child_process"
import {fileURLToPath,pathToFileURL} from "node:url"
import path from "node:path"
import {build} from "esbuild"
const artifacts=process.env.BOTS_ARTIFACTS,module=process.env.PLAYWRIGHT_MODULE,base=process.env.BOTS_BASELINE
if(!artifacts||!module)throw Error("Set BOTS_ARTIFACTS and an existing PLAYWRIGHT_MODULE")
await mkdir(artifacts,{recursive:true})
const pw=await import(pathToFileURL(module).href)
const built=await build({entryPoints:[fileURLToPath(new URL("./roster-mixed.fixture.tsx",import.meta.url))],bundle:true,write:false,outfile:"roster.js",format:"esm",platform:"browser",jsx:"automatic",define:{"process.env.NODE_ENV":'"development"'},plugins:base?[{name:"baseline",setup(b){b.onLoad({filter:/project-assistants\/(?:BotRoster\.tsx|RosterHandle\.tsx|roster-controls\.css)$/},args=>({contents:execFileSync("git",["show",`${base}:web/console/src/project-assistants/${path.basename(args.path)}`],{encoding:"utf8"}),loader:args.path.endsWith("css")?"css":"tsx",resolveDir:path.dirname(args.path)}))}}]:[]})
const files=new Map(built.outputFiles.map(f=>["/"+path.basename(f.path),f.contents]))
const server=createServer((req,res)=>{const file=files.get(new URL(req.url,"http://fixture").pathname);if(file){res.setHeader("Content-Type",req.url.endsWith("css")?"text/css":"text/javascript");res.end(file)}else {res.setHeader("Content-Type","text/html");res.end('<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><style>body{margin:0;background:#111;color:#eee;font-family:system-ui}</style><link rel="stylesheet" href="/roster.css"><div id="app"></div><script type="module" src="/roster.js"></script>')}})
await new Promise(resolve=>server.listen(0,"127.0.0.1",resolve))
const origin=`http://127.0.0.1:${server.address().port}`,evidence=[]
try{
 for(const engine of (process.env.BOTS_ENGINES??"chromium,webkit").split(",")){
  const browser=await pw[engine].launch({headless:true,...(engine==="chromium"?{args:["--disable-features=OmniboxEverywhere"]}:{})})
  try{for(const [language,width,height] of [["en",1440,900],["zh-Hant",390,720]]){
   const context=await browser.newContext({viewport:{width,height},hasTouch:width===390,recordVideo:{dir:artifacts,size:{width,height}}}),page=await context.newPage(),errors=[]
   page.on("pageerror",error=>errors.push(String(error)))
   const row=name=>page.locator(".pa-roster-entry").filter({has:page.locator("strong",{hasText:new RegExp(`^${name}$`)})})
   const order=()=>page.locator(".pa-roster-entry .pa-bot-row strong").allTextContents()
   const prefs=()=>page.evaluate(()=>localStorage.getItem(JSON.stringify(["project-assistants","roster","mixed-fixture"])))
   const point=async name=>{await row(name).scrollIntoViewIfNeeded();const b=await row(name).boundingBox();return {x:b.x+b.width/2,y:b.y+b.height/2}}
   await page.goto(origin+"/?lang="+language);await row("One").waitFor()
   const video=page.video(),initial=await order(),initialStorage=await prefs()
   const beforeButtons=await page.locator(".pa-roster-entry .pa-menu").count()
   await page.screenshot({path:path.join(artifacts,`${engine}-${language}-default.png`)})
   // Same hold reproduces the old requirement to enter a separate mode first.
   const a=await point("One")
   await page.mouse.move(a.x,a.y);await page.mouse.down();await page.waitForTimeout(420)
   const lifted=await page.locator(".pa-roster-lift").count()
   if(base){await page.mouse.up();await writeFile(path.join(artifacts,"red.json"),JSON.stringify({base,beforeButtons,lifted,initial},null,2));assert.equal(lifted,1,"default whole-row hold must lift without entering reorder mode")}
   assert.equal(beforeButtons,0,"default rows must not reserve a menu column")
   assert.equal(lifted,1)
   // Move a Bot through the group row. Preview changes, durable order waits for release.
   const b=await point("Two");await page.mouse.move(b.x,b.y,{steps:12});await page.waitForTimeout(230)
   const preview=await order();assert.ok(preview.indexOf("One")>preview.indexOf("Two"));assert.equal(await prefs(),initialStorage)
   await page.screenshot({path:path.join(artifacts,`${engine}-${language}-lifted.png`)})
   await page.mouse.up();await page.waitForTimeout(230)
   assert.deepEqual(await order(),preview);assert.notEqual(await prefs(),initialStorage)
   assert.equal(await page.locator('[role="dialog"]').count(),0);assert.equal(await page.locator(".pa-main output").innerText(),"")
   await page.reload();await row("One").waitFor();assert.deepEqual(await order(),preview)
   await page.screenshot({path:path.join(artifacts,`${engine}-${language}-reopened.png`)})
   // Escape rolls back a changed preview, leaves storage byte-identical and swallows the release click.
   const saved=await prefs(),c=await point("Two");await page.mouse.move(c.x,c.y);await page.mouse.down();await page.waitForTimeout(420)
   const d=await point("One");await page.mouse.move(d.x,d.y,{steps:12});await page.waitForTimeout(230)
   assert.notDeepEqual(await order(),preview);await page.keyboard.press("Escape");await page.mouse.up();await page.waitForTimeout(30)
   assert.deepEqual(await order(),preview);assert.equal(await prefs(),saved);assert.equal(await page.locator(".pa-roster-lift").count(),0)
   // Bot and group keyboard operations use their actual typed identities.
   await row("One").locator(".pa-bot-row").focus();await page.keyboard.press("Alt+ArrowUp")
   const keyboardOrder=await order();assert.notDeepEqual(keyboardOrder,preview)
   await page.reload();await row("One").waitFor();assert.deepEqual(await order(),keyboardOrder)
   const options=page.getByRole("button",{name:language==="en"?"List options":"清單選項",exact:true})
   await options.click();await page.getByRole("menuitem",{name:language==="en"?"Reorder conversations":"調整對話順序",exact:true}).click()
   await row("Release discussion").locator(".pa-roster-pin").click();await page.getByRole("button",{name:language==="en"?"Done":"完成",exact:true}).click()
   await page.reload();await row("Release discussion").waitFor();assert.equal((await order())[0],"Release discussion")
   // Short click still opens exactly that room, and Back returns to its row.
   await row("Release discussion").locator(".pa-bot-row").click();await page.getByRole("dialog",{name:"Release discussion",exact:true}).waitFor()
   await page.getByRole("button",{name:language==="en"?"Back":"返回",exact:true}).click()
   await row("Release discussion").waitFor()
   let touch=null
   if(engine==="chromium"&&width===390){
    const cdp=await context.newCDPSession(page),scroll=page.locator(".pa-roster-scroll")
    await scroll.evaluate(el=>el.scrollTop=0)
    let p=await point("Two"),q=await point("One"),beforeTouch=await order()
    await cdp.send("Input.dispatchTouchEvent",{type:"touchStart",touchPoints:[p]});await page.waitForTimeout(420)
    assert.equal(await page.locator(".pa-roster-lift").count(),1)
    for(let i=1;i<=12;i++){await cdp.send("Input.dispatchTouchEvent",{type:"touchMove",touchPoints:[{x:p.x+(q.x-p.x)*i/12,y:p.y+(q.y-p.y)*i/12}]});await page.waitForTimeout(20)}
    await cdp.send("Input.dispatchTouchEvent",{type:"touchEnd",touchPoints:[]});await page.waitForTimeout(230)
    const touchOrder=await order();assert.notDeepEqual(touchOrder,beforeTouch);await page.reload();await row("Two").waitFor();assert.deepEqual(await order(),touchOrder)
    // A touch moving before the hold pans the actual scroll container, not a synthetic scroll assignment.
    await scroll.evaluate(el=>el.scrollTop=0);const box=await scroll.boundingBox(),start={x:box.x+box.width/2,y:box.y+box.height-40},top=await scroll.evaluate(el=>el.scrollTop)
    await cdp.send("Input.dispatchTouchEvent",{type:"touchStart",touchPoints:[start]})
    for(let i=1;i<=8;i++){await cdp.send("Input.dispatchTouchEvent",{type:"touchMove",touchPoints:[{x:start.x,y:start.y-i*25}]});await page.waitForTimeout(16)}
    await cdp.send("Input.dispatchTouchEvent",{type:"touchEnd",touchPoints:[]});await page.waitForTimeout(300)
    assert.ok(await scroll.evaluate(el=>el.scrollTop)>top);assert.equal(await page.locator(".pa-roster-lift").count(),0)
    // True touch cancellation restores the order, and a following tap remains usable.
    await scroll.evaluate(el=>el.scrollTop=0);p=await point("Two");q=await point("One");const cancelOrder=await order(),cancelSaved=await prefs()
    await cdp.send("Input.dispatchTouchEvent",{type:"touchStart",touchPoints:[p]});await page.waitForTimeout(420)
    await cdp.send("Input.dispatchTouchEvent",{type:"touchMove",touchPoints:[q]});await page.waitForTimeout(230)
    await cdp.send("Input.dispatchTouchEvent",{type:"touchCancel",touchPoints:[]});await page.waitForTimeout(50)
    assert.deepEqual(await order(),cancelOrder);assert.equal(await prefs(),cancelSaved)
    await row("One").locator(".pa-bot-row").tap();assert.match(await page.locator(".pa-main output").innerText(),/One/)
    touch={nativeDrag:true,persisted:true,nativePan:true,touchCancel:true,shortTap:true};await cdp.detach()
   }
   await page.emulateMedia({reducedMotion:"reduce"});const p=await point("One");await page.mouse.move(p.x,p.y);await page.mouse.down();await page.waitForTimeout(420)
   const q=await point("Three");await page.mouse.move(q.x,q.y,{steps:12});await page.waitForTimeout(20)
   assert.equal(await page.locator(".pa-roster-entry").evaluateAll(rows=>rows.flatMap(row=>row.getAnimations()).length),0)
   await page.keyboard.press("Escape");await page.mouse.up()
   assert.deepEqual(errors,[]);assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false)
   await page.emulateMedia({reducedMotion:"no-preference"})
   const groupBefore=await order(),groupStart=await point("Six member room")
   await page.mouse.move(groupStart.x,groupStart.y);await page.mouse.down();await page.waitForTimeout(420)
   const groupTarget=await point("Three");await page.mouse.move(groupTarget.x,groupTarget.y)
   await page.waitForFunction(()=>document.querySelector(".pa-roster-entry[data-dragging]")?.querySelector("strong")?.textContent==="Six member room")
   const animations=await page.locator(".pa-roster-entry").evaluateAll(rows=>rows.flatMap(row=>row.getAnimations()).length)
   assert.ok(animations>0,"neighbours must animate into their preview slots")
   await page.waitForTimeout(230);const groupPreview=await order();assert.notDeepEqual(groupPreview,groupBefore)
   await page.screenshot({path:path.join(artifacts,`${engine}-${language}-group-lifted.png`)})
   await page.mouse.up();await page.reload();await row("Six member room").waitFor();assert.deepEqual(await order(),groupPreview)
   // Leaving the roster cancels rather than committing a changed preview.
   const outsideBefore=await order(),outsideSaved=await prefs(),outsideStart=await point("One")
   await page.mouse.move(outsideStart.x,outsideStart.y);await page.mouse.down();await page.waitForTimeout(420)
   const outsideTarget=await point("Two");await page.mouse.move(outsideTarget.x,outsideTarget.y);await page.waitForTimeout(230)
   await page.mouse.move(-10,20);await page.mouse.up();assert.deepEqual(await order(),outsideBefore);assert.equal(await prefs(),outsideSaved)
   const result={engine,language,width,height,groupDragAndReload:true,neighbourAnimation:true,outsideRollback:true,initial,reopened:preview,defaultRowActions:beforeButtons,holdLift:true,livePreview:true,saveOnlyOnDrop:true,escapeRollback:true,keyboardAndPin:true,shortGroupClick:true,reducedMotion:true,touch,pageErrors:errors}
   evidence.push(result)
   await context.close();await video.saveAs(path.join(artifacts,`${engine}-${language}.webm`))
  }}finally{await browser.close()}
 }
 await writeFile(path.join(artifacts,"result.json"),JSON.stringify({modelRequests:0,evidence},null,2)+"\n")
 console.log(JSON.stringify(evidence))
}finally{server.closeAllConnections();await new Promise(resolve=>server.close(resolve))}
