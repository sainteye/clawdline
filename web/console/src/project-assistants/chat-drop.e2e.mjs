/** A file dragged over a Bot chat, and a run of exchange capsules, in a real browser. No daemon, provider or paid API. */
import assert from "node:assert/strict"
import {createServer} from "node:http"
import {mkdir} from "node:fs/promises"
import {fileURLToPath,pathToFileURL} from "node:url"
import path from "node:path"
import {build} from "esbuild"
const artifacts=process.env.BOTS_ARTIFACTS
if(!artifacts||!process.env.PLAYWRIGHT_MODULE)throw Error("Set BOTS_ARTIFACTS and an existing PLAYWRIGHT_MODULE")
await mkdir(artifacts,{recursive:true})
const pw=await import(pathToFileURL(process.env.PLAYWRIGHT_MODULE).href)
const built=await build({entryPoints:[fileURLToPath(new URL("./chat-drop.fixture.tsx",import.meta.url))],bundle:true,write:false,outfile:"drop.js",format:"esm",platform:"browser",target:"es2022",jsx:"automatic",loader:{".webp":"dataurl",".png":"dataurl",".svg":"dataurl",".mp3":"dataurl",".wav":"dataurl"},define:{"process.env.NODE_ENV":'"development"',"import.meta.env":"{}"}})
const files=new Map(built.outputFiles.map(f=>["/"+path.basename(f.path),f.contents]))
const server=createServer((req,res)=>{
 const file=files.get(new URL(req.url,"http://fixture").pathname)
 if(file){res.setHeader("Content-Type",req.url.endsWith("css")?"text/css":"text/javascript");res.end(file);return}
 res.setHeader("Content-Type","text/html");res.end(`<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><style>body{margin:0;background:#080808;color:#eee;font-family:system-ui}</style><link rel="stylesheet" href="/drop.css"><div id="app"></div><script type="module" src="/drop.js"></script>`)
})
await new Promise(resolve=>server.listen(0,"127.0.0.1",resolve))
const origin=`http://127.0.0.1:${server.address().port}`
const failures=[]
async function check(label,run){try{await run();console.log(`ok ${label}`)}catch(error){failures.push(`${label}: ${error.message}`);console.log(`not ok ${label}: ${error.message.split("\n")[0]}`)}}
// One gesture keeps one DataTransfer, as a browser does; each event targets the element the pointer is over.
const gesture=(page,steps,payload={files:[{name:"photo.png",type:"image/png"}]})=>page.evaluate(({steps,payload})=>{
 const dt=new DataTransfer()
 if(payload.text)dt.setData("text/plain",payload.text)
 for(const f of payload.files??[])dt.items.add(new File(["fixture"],f.name,{type:f.type}))
 for(const [type,selector,related] of steps){
  const target=document.querySelector(selector)
  if(!target)throw Error(`no ${selector}`)
  target.dispatchEvent(new DragEvent(type,{bubbles:true,cancelable:true,dataTransfer:dt,relatedTarget:related?document.querySelector(related):null}))
 }
 // Drag events render at continuous priority; read the screen after it has painted.
 return new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve)))
},{steps,payload})
const overlay=page=>page.evaluate(()=>{const el=document.querySelector(".pa-drop-target");return el?{refused:el.hasAttribute("data-refused"),text:el.textContent}:null})
const browser=await pw.chromium.launch({headless:true,args:["--disable-features=OmniboxEverywhere"]})
const open=async({query="",phone=false}={})=>{
 const context=await browser.newContext({viewport:phone?{width:390,height:844}:{width:1280,height:800},deviceScaleFactor:2})
 const page=await context.newPage()
 await page.goto(`${origin}/${query}`)
 await page.locator(".pa-exchange-entry").first().waitFor({timeout:10000}).catch(()=>{})
 await page.locator(".pa-composer textarea").waitFor()
 return {context,page}
}
const scroll=".pa-chat-scroll",field=".pa-composer textarea"
try{
 for(const phone of [false,true]){
  const width=phone?"390":"1280",{context,page}=await open({phone})
  await check(`${width}: consecutive exchange capsules are spaced evenly`,async()=>{
   const boxes=await page.locator(".pa-exchange-attribution").evaluateAll(els=>els.map(el=>{const r=el.getBoundingClientRect();return {top:r.top,bottom:r.bottom}}))
   assert.equal(boxes.length,5,"five capsules")
   const gaps=boxes.slice(1).map((box,i)=>Math.round(box.top-boxes[i].bottom))
   assert.ok(gaps.every(gap=>gap>0),`gaps ${gaps}`)
   assert.equal(new Set(gaps).size,1,`gaps ${gaps}`)
  })
  await page.screenshot({path:path.join(artifacts,`capsules-${width}.png`)})
  await check(`${width}: dragging an image shows where it lands`,async()=>{
   await gesture(page,[["dragenter",scroll],["dragover",scroll]])
   const shown=await overlay(page)
   assert.ok(shown&&!shown.refused,"ready overlay")
   assert.match(shown.text,/Drop images to attach/)
  })
  await page.screenshot({path:path.join(artifacts,`drop-ready-${width}.png`)})
  await check(`${width}: moving between children keeps the overlay without a flicker`,async()=>{
   await page.evaluate(()=>{window.__removed=0;new MutationObserver(list=>{for(const m of list)for(const n of m.removedNodes)if(n.classList?.contains("pa-drop-target"))window.__removed++}).observe(document.body,{childList:true,subtree:true})})
   await gesture(page,[["dragenter",field,scroll],["dragleave",scroll,field],["dragover",field],["dragenter",scroll,field],["dragleave",field,scroll]])
   assert.ok(await overlay(page),"still shown")
   assert.equal(await page.evaluate(()=>window.__removed),0)
  })
  await check(`${width}: leaving the chat takes the overlay away`,async()=>{
   await gesture(page,[["dragleave",scroll]])
   assert.equal(await overlay(page),null)
  })
  await check(`${width}: Esc during a drag takes the overlay away`,async()=>{
   await gesture(page,[["dragenter",scroll]])
   assert.ok(await overlay(page))
   await page.keyboard.press("Escape");await page.evaluate(()=>new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve))))
   assert.equal(await overlay(page),null)
  })
  await check(`${width}: dragging text is not a file drop`,async()=>{
   await gesture(page,[["dragenter",scroll],["dragover",scroll]],{text:"just words"})
   assert.equal(await overlay(page),null)
  })
  await check(`${width}: a non-image on an images-only Bot is refused with the reason`,async()=>{
   await gesture(page,[["dragenter",scroll],["dragover",scroll]],{files:[{name:"notes.pdf",type:"application/pdf"}]})
   const shown=await overlay(page)
   assert.ok(shown?.refused,"refused overlay");assert.match(shown.text,/takes images only/)
  })
  await page.screenshot({path:path.join(artifacts,`drop-refused-${width}.png`)})
  await gesture(page,[["dragleave",scroll]],{files:[{name:"notes.pdf",type:"application/pdf"}]})
  await check(`${width}: dropping an image attaches it and clears the overlay`,async()=>{
   await gesture(page,[["dragenter",scroll],["dragover",scroll],["drop",scroll]])
   assert.equal(await overlay(page),null)
   await page.locator(".pa-pictures .pa-picture").first().waitFor({timeout:5000})
  })
  await check(`${width}: no horizontal page scroll`,async()=>{
   assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),"scrollWidth")
  })
  await context.close()
 }
 {
  const {context,page}=await open({query:"?accept=files"})
  await check("files Bot: a dropped file becomes an attachment chip",async()=>{
   await gesture(page,[["dragenter",scroll],["dragover",scroll]],{files:[{name:"notes.pdf",type:"application/pdf"}]})
   const shown=await overlay(page);assert.ok(shown&&!shown.refused);assert.match(shown.text,/Drop to attach/)
   await gesture(page,[["drop",scroll]],{files:[{name:"notes.pdf",type:"application/pdf"}]})
   await page.locator(".pa-input-chips",{hasText:"notes.pdf"}).waitFor({timeout:5000})
  })
  await context.close()
 }
 for(const [state,reason] of [["readonly",/read-only/],["offline",/offline/]]){
  const context=await browser.newContext({viewport:{width:1280,height:800},deviceScaleFactor:2}),page=await context.newPage()
  await page.goto(`${origin}/?state=${state}`)
  await page.locator(scroll).waitFor()
  await page.waitForTimeout(800)
  await check(`${state}: the drop is refused with the reason`,async()=>{
   await gesture(page,[["dragenter",scroll],["dragover",scroll]])
   const shown=await overlay(page);assert.ok(shown?.refused,"refused overlay");assert.match(shown.text,reason)
  })
  await page.screenshot({path:path.join(artifacts,`drop-${state}-1280.png`)})
  await context.close()
 }
}finally{await browser.close();server.close()}
if(failures.length){console.log(failures.join("\n"));process.exit(1)}
