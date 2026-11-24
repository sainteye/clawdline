// Bots settings modal in a real browser: opens, keeps focus inside, closes on
// Esc and on the backdrop, saves, and the dropdown chevrons are aligned.
// PLAYWRIGHT_MODULE=<playwright index.mjs> BOTS_ARTIFACTS=<dir> node bots-settings.e2e.mjs
import assert from "node:assert/strict"
import {createServer} from "node:http"
import {mkdir,readFile} from "node:fs/promises"
import path from "node:path"
import {pathToFileURL} from "node:url"
import {build} from "esbuild"

const {PLAYWRIGHT_MODULE,BOTS_ARTIFACTS}=process.env
assert.ok(PLAYWRIGHT_MODULE&&BOTS_ARTIFACTS,"Use an existing Playwright installation and a task-owned evidence directory")
await mkdir(BOTS_ARTIFACTS,{recursive:true})
const browsers=await import(pathToFileURL(PLAYWRIGHT_MODULE).href)
const bundle=await build({entryPoints:[new URL("bots-settings.fixture.tsx",import.meta.url).pathname],outfile:"project-assistants-fixture.js",write:false,bundle:true,format:"esm",platform:"browser",jsx:"automatic",define:{"process.env.NODE_ENV":'"development"'}})
const files=new Map(bundle.outputFiles.map(file=>["/"+path.basename(file.path),file.contents]))
const html=await readFile(new URL("../../tools/fixtures/project-assistants.html",import.meta.url))
const catalogs=new URL("../../public/catalogs/",import.meta.url)
const server=createServer(async(req,res)=>{
  const pathname=new URL(req.url,"http://fixture").pathname
  if(pathname==="/"){res.setHeader("Content-Type","text/html");res.end(html);return}
  if(pathname.startsWith("/catalog/")){try{res.setHeader("Content-Type","application/json");res.end(await readFile(new URL(path.basename(pathname),catalogs)))}catch{res.writeHead(404).end()}return}
  const file=files.get(pathname)
  if(!file){res.writeHead(404).end();return}
  res.setHeader("Content-Type",pathname.endsWith(".css")?"text/css":"text/javascript");res.end(file)
})
await new Promise(resolve=>server.listen(0,"127.0.0.1",resolve))
const origin="http://127.0.0.1:"+server.address().port
const browser=await browsers.chromium.launch({args:["--disable-features=OmniboxEverywhere"]})
try{
  for(const [name,viewport] of [["desktop",{width:1280,height:900}],["390",{width:390,height:844}]]){
    const touch=name==="390",page=await browser.newPage({viewport,deviceScaleFactor:2,locale:"zh-TW",isMobile:touch,hasTouch:touch})
    await page.goto(origin+"/")
    await page.click("#opener")
    const dialog=page.getByRole("dialog")
    await dialog.waitFor()
    await page.locator(".pa-bs-rule").first().waitFor()
    assert.equal(await page.evaluate(()=>document.activeElement?.classList.contains("pa-bs-close")),true,"focus starts on close")
    // Focus trap: Shift+Tab from the first control wraps to the last one, Tab wraps back.
    await page.keyboard.press("Shift+Tab")
    assert.equal(await page.evaluate(()=>document.activeElement?.closest("[role=dialog]")!==null),true,"Shift+Tab stays in the dialog")
    for(let i=0;i<60;i++){await page.keyboard.press("Tab");assert.equal(await page.evaluate(()=>document.activeElement?.closest("[role=dialog]")!==null),true,"Tab stays in the dialog")}
    const box=await dialog.boundingBox()
    if(name==="390")assert.ok(box.width>=389&&box.x<=1,"full screen at 390px")
    await page.locator(".pa-bs-body").evaluate(e=>e.scrollTop=0)
    await page.screenshot({path:path.join(BOTS_ARTIFACTS,`bots-settings-${name}.png`)})
    // Each fallback agent's remove button stays on the line of its own order number,
    // inside its row, and on touch it answers a 44px square around its centre.
    const agentRows=page.locator(".pa-bs-agent"),problems=[]
    assert.ok(await agentRows.count()>=4,"the fixture shows a rule with several fallback agents")
    for(let i=0;i<await agentRows.count();i++){
      const row=agentRows.nth(i)
      await row.scrollIntoViewIfNeeded()
      const seen=await row.evaluate((row,touch)=>{
        const remove=row.querySelector("button[aria-label]:not(.pa-select-button)"),order=row.firstElementChild
        const r=remove.getBoundingClientRect(),o=order.getBoundingClientRect(),box=row.getBoundingClientRect()
        const centre={x:r.left+r.width/2,y:r.top+r.height/2}
        const misses=touch?[[-21,-21],[21,-21],[-21,21],[21,21],[0,-21],[0,21],[-21,0],[21,0]].filter(([dx,dy])=>document.elementFromPoint(centre.x+dx,centre.y+dy)?.closest("button")!==remove):[]
        return {line:Math.abs(centre.y-(o.top+o.height/2)),inside:r.right<=box.right+0.5&&r.left>=o.right,misses,label:remove.getAttribute("aria-label")}
      },touch)
      if(seen.line>2)problems.push(`${name}: row ${i+1}'s ${seen.label} is on the same line as its order number (${seen.line}px apart)`)
      if(!seen.inside)problems.push(`${name}: row ${i+1}'s ${seen.label} stays inside its row`)
      if(seen.misses.length)problems.push(`${name}: row ${i+1}'s ${seen.label} answers a 44px touch target (missed at ${JSON.stringify(seen.misses)})`)
    }
    assert.deepEqual(problems,[],"every fallback agent row keeps its remove button")
    await page.locator(".pa-bs-rule").nth(1).screenshot({path:path.join(BOTS_ARTIFACTS,`bots-settings-fallbacks-${name}.png`)})
    // Scope picker open.
    await page.locator(".pa-bs-scope-button").nth(1).click()
    await page.locator(".pa-bs-scope-pop").waitFor();await page.waitForTimeout(250)
    await page.screenshot({path:path.join(BOTS_ARTIFACTS,`bots-settings-scope-${name}.png`)})
    // Chevron alignment: every indicator is vertically centred in its button.
    const offsets=await page.evaluate(()=>[...document.querySelectorAll(".pa-bs-chevron,.pa-select-button svg")].map(svg=>{const b=svg.closest("button").getBoundingClientRect(),s=svg.getBoundingClientRect();return Math.abs((s.top+s.height/2)-(b.top+b.height/2))}))
    assert.ok(offsets.length>0&&offsets.every(o=>o<=1),"chevrons centred: "+offsets.join(","))
    const chip=page.locator(".pa-bs-scope-button").nth(1)
    await chip.screenshot({path:path.join(BOTS_ARTIFACTS,`bots-settings-chevron-${name}.png`),scale:"device"})
    await page.keyboard.press("Escape")
    await page.locator(".pa-bs-scope-pop").waitFor({state:"detached"})
    assert.equal(await dialog.isVisible(),true,"Esc closes the popover first")
    // Edit and save the whole document.
    await page.getByRole("button",{name:"新增記憶"}).click()
    await page.locator(".pa-bs-memory textarea").last().fill("設計相關先給兩個方向")
    await page.getByRole("button",{name:"儲存",exact:true}).click()
    await page.waitForFunction(()=>window.saved?.memory?.length===2)
    await page.keyboard.press("Escape")
    await dialog.waitFor({state:"detached"})
    assert.equal(await page.evaluate(()=>document.activeElement?.id),"opener","focus returns to the opener")
    await page.click("#opener");await dialog.waitFor()
    await page.mouse.click(5,viewport.height-5)
    if(name==="desktop")await dialog.waitFor({state:"detached"})
    await page.close()
    console.log("ok",name)
  }
}finally{await browser.close();server.close()}
