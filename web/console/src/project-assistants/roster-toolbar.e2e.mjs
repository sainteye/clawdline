/** The Bots list toolbar keeps collapse at the start and its other buttons together at the end, and Bots settings opens from its options menu. No daemon, provider or paid API. */
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
const built=await build({entryPoints:[fileURLToPath(new URL("./roster-mixed.fixture.tsx",import.meta.url))],bundle:true,write:false,outfile:"toolbar.js",format:"esm",platform:"browser",jsx:"automatic",define:{"process.env.NODE_ENV":'"development"'}})
const files=new Map(built.outputFiles.map(f=>["/"+path.basename(f.path),f.contents]))
const server=createServer((req,res)=>{
 const file=files.get(new URL(req.url,"http://fixture").pathname)
 if(file){res.setHeader("Content-Type",req.url.endsWith("css")?"text/css":"text/javascript");res.end(file);return}
 res.setHeader("Content-Type","text/html");res.end(`<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><style>body{margin:0;background:#111;color:#eee;font-family:system-ui}</style><link rel="stylesheet" href="/toolbar.css"><div id="app"></div><script type="module" src="/toolbar.js"></script>`)
})
await new Promise(resolve=>server.listen(0,"127.0.0.1",resolve))
const origin=`http://127.0.0.1:${server.address().port}`
const failures=[]
// The desktop tower HUD blinks forever, so waiting for every animation never ends.
// Reduced motion removes the CSS loops; only animations that can finish are awaited.
const settled=page=>page.evaluate(()=>Promise.all(document.getAnimations().filter(animation=>Number.isFinite(animation.effect?.getComputedTiming().iterations??Infinity)).map(animation=>animation.finished.catch(()=>{}))))
try{
 for(const engine of (process.env.BOTS_ENGINES??"chromium,webkit").split(",")){
  const browser=await pw[engine].launch({headless:true,...(engine==="chromium"?{args:["--disable-features=OmniboxEverywhere"]}:{})})
  try{for(const [width,height] of [[390,844],[1280,800]]){
   const context=await browser.newContext({viewport:{width,height},hasTouch:width===390,reducedMotion:"reduce"}),page=await context.newPage()
   await page.goto(`${origin}/?actions&lang=zh-Hant`)
   const toolbar=page.locator(".pa-roster .pa-toolbar")
   await toolbar.waitFor()
   await page.getByRole("button",{name:/大樓檢視|Tower view/}).first().waitFor()
   await settled(page)
   const box=await toolbar.evaluate(bar=>{
    const outer=bar.getBoundingClientRect(),style=getComputedStyle(bar)
    const buttons=[...bar.querySelectorAll("button")].filter(button=>!button.closest(".pa-menu-list")&&button.getClientRects().length).map(button=>{const r=button.getBoundingClientRect();return {name:button.getAttribute("aria-label")??button.textContent,left:r.left,right:r.right,width:r.width,height:r.height}})
    return {left:outer.left+parseFloat(style.paddingLeft),right:outer.right-parseFloat(style.paddingRight),buttons}
   })
   await page.screenshot({path:path.join(artifacts,`toolbar-${engine}-${width}.png`),clip:{x:0,y:0,width,height:Math.min(height,260)}})
   const [first,...rest]=box.buttons,label=`${engine} ${width}x${height}`
   const check=async(run)=>{try{await run()}catch(error){failures.push(error.message);console.log(`not ok ${error.message}`)}}
   await check(async()=>{assert.equal(box.buttons.length,4,`${label}: collapse, tower, create and options are shown`)})
   await check(async()=>{assert.ok(Math.abs(first.left-box.left)<=1,`${label}: collapse sits at the start (${first.left} vs ${box.left})`)})
   await check(async()=>{assert.ok(Math.abs(rest.at(-1).right-box.right)<=1,`${label}: the last button touches the end (${rest.at(-1).right} vs ${box.right})`)})
   const gaps=rest.slice(1).map((b,i)=>b.left-rest[i].right)
   await check(async()=>{assert.ok(gaps.every(gap=>Math.abs(gap-gaps[0])<=1&&gap>=4&&gap<=12),`${label}: the end buttons share one small gap (${gaps.join(", ")})`)})
   await check(async()=>{assert.ok(rest[0].left-first.right>gaps[0]+8,`${label}: the end buttons are grouped away from collapse`)})
   await check(async()=>{for(const b of box.buttons)assert.ok(b.width>=44&&b.height>=44,`${label}: ${b.name} keeps a 44px target (${b.width}x${b.height})`)})
   await check(async()=>{for(const [i,b] of box.buttons.entries())for(const other of box.buttons.slice(i+1))assert.ok(b.right<=other.left+0.5||other.right<=b.left+0.5,`${label}: ${b.name} and ${other.name} do not overlap`)})
   await check(async()=>{
    const options=toolbar.getByRole("button",{name:/^(清單選項|List options)$/})
    await options.click()
    const settings=page.getByRole("menuitem",{name:/^(Bots 設定|Bots settings)$/})
    await settings.waitFor({timeout:2000}).catch(()=>{throw Error(`${label}: Bots settings is an item in the options menu`)})
    assert.equal(await settings.getAttribute("aria-haspopup"),"dialog",`${label}: the settings item says it opens a dialog`)
    await settled(page)
    await page.screenshot({path:path.join(artifacts,`toolbar-menu-${engine}-${width}.png`),clip:{x:0,y:0,width,height:Math.min(height,400)}})
    await settings.click()
    const dialog=page.getByRole("dialog",{name:/Bots 設定|Bots settings/})
    await dialog.waitFor({timeout:2000})
    await page.waitForTimeout(150)
    await page.screenshot({path:path.join(artifacts,`toolbar-modal-${engine}-${width}.png`)})
    await page.keyboard.press("Escape")
    await dialog.waitFor({state:"detached",timeout:2000})
    assert.ok(await options.evaluate(button=>document.activeElement===button),`${label}: closing settings returns focus to the options button`)
    console.log(`ok ${label}: settings opens from the options menu`)
   })
   console.log(`${label}: gaps ${gaps.join(", ")}`)
   await context.close()
  }}finally{await browser.close()}
 }
}finally{server.close()}
if(failures.length)throw Error(failures.join("\n"))
