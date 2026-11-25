// Bots settings modal in a real browser: the side navigation, the default agent
// tiles, the work-kind table and a kind's page; every drop-down list stays inside
// the dialog, model lists come from the machine's model catalog and show its
// loading, still-asking and failure states, focus stays
// inside, Esc and the backdrop close it, and saving writes the whole document.
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
const icons=new URL("../../public/icons/",import.meta.url)
const types={".css":"text/css",".png":"image/png",".svg":"image/svg+xml",".json":"application/json"}
const server=createServer(async(req,res)=>{
  const pathname=new URL(req.url,"http://fixture").pathname
  if(pathname==="/"){res.setHeader("Content-Type","text/html");res.end(html);return}
  const from=pathname.startsWith("/catalog/")?catalogs:pathname.startsWith("/icons/")?icons:null
  if(from){try{res.setHeader("Content-Type",types[path.extname(pathname)]??"application/octet-stream");res.end(await readFile(new URL(path.basename(pathname),from)))}catch{res.writeHead(404).end()}return}
  const file=files.get(pathname)
  if(!file){res.writeHead(404).end();return}
  res.setHeader("Content-Type",types[path.extname(pathname)]??"text/javascript");res.end(file)
})
await new Promise(resolve=>server.listen(0,"127.0.0.1",resolve))
const origin="http://127.0.0.1:"+server.address().port
const browser=await browsers.chromium.launch({args:["--disable-features=OmniboxEverywhere"]})

// The open list lies wholly inside the dialog's own box.
async function listInsideDialog(page,label){
  const seen=await page.evaluate(()=>{
    const list=document.querySelector(".pa-select-list"),dialog=document.querySelector("[role=dialog]")
    if(!list||!dialog)return null
    const l=list.getBoundingClientRect(),d=dialog.getBoundingClientRect()
    return {l:{top:l.top,bottom:l.bottom,left:l.left,right:l.right},d:{top:d.top,bottom:d.bottom,left:d.left,right:d.right}}
  })
  assert.ok(seen,`${label}: a list is open`)
  const {l,d}=seen
  assert.ok(l.left>=d.left-0.5&&l.right<=d.right+0.5&&l.top>=d.top-0.5&&l.bottom<=d.bottom+0.5,`${label}: list ${JSON.stringify(l)} inside dialog ${JSON.stringify(d)}`)
}

try{
  for(const [name,viewport] of [["desktop",{width:1280,height:900}],["390",{width:390,height:844}]]){
    const touch=name==="390",page=await browser.newPage({viewport,deviceScaleFactor:2,locale:"zh-TW",isMobile:touch,hasTouch:touch})
    await page.goto(origin+"/?fails")
    await page.click("#opener")
    const dialog=page.getByRole("dialog")
    await dialog.waitFor()
    await page.locator(".pa-bs-tile").first().waitFor()
    assert.equal(await page.evaluate(()=>document.activeElement?.classList.contains("pa-bs-close")),true,"focus starts on close")
    await page.keyboard.press("Shift+Tab")
    for(let i=0;i<40;i++){await page.keyboard.press("Tab");assert.equal(await page.evaluate(()=>document.activeElement?.closest("[role=dialog]")!==null),true,"Tab stays in the dialog")}
    const box=await dialog.boundingBox()
    if(touch)assert.ok(box.width>=389&&box.x<=1,"full screen at 390px")

    // All four agents are shown; the one this computer lacks is marked and cannot be chosen.
    const tiles=page.locator(".pa-bs-tile")
    assert.equal(await tiles.count(),4,"four agent tiles")
    assert.deepEqual(await tiles.locator(".pa-bs-tile-name").allTextContents(),["Claude Code","Codex","Gemini","Grok Build"])
    assert.equal(await tiles.nth(3).isDisabled(),true,"Grok is not on this computer")
    assert.match(await tiles.nth(3).textContent(),/這台未安裝/)
    assert.equal(await page.locator(".pa-bs-tile .assistant-logo").count(),4,"each tile has the agent's own logo")
    await page.locator(".pa-bs-body").evaluate(e=>e.scrollTop=0)
    await page.screenshot({path:path.join(BOTS_ARTIFACTS,`bots-settings-${name}.png`)})

    // The default's model list: loading first, inside the list; the first read fails
    // with a retry inside the list; the retry reads the machine's catalog.
    const modelButton=page.locator(".pa-bs-group").first().locator(".pa-select-button").first()
    await modelButton.click()
    await page.locator(".pa-select-status[data-kind=loading]").waitFor()
    await listInsideDialog(page,`${name} default model loading`)
    await page.screenshot({path:path.join(BOTS_ARTIFACTS,`bots-settings-model-loading-${name}.png`)})
    await page.locator(".pa-select-status[data-kind=error]").waitFor()
    await listInsideDialog(page,`${name} default model failed`)
    await page.screenshot({path:path.join(BOTS_ARTIFACTS,`bots-settings-model-failed-${name}.png`)})
    await page.locator(".pa-select-retry").click()
    await page.locator(".pa-select-status").waitFor({state:"detached"})
    const claudeModels=await page.locator(".pa-select-list [role=option]").allTextContents()
    assert.deepEqual(claudeModels,["Agent 預設（Opus）","Opus","Sonnet","Haiku"],"Claude's default names what it runs and is not listed twice")
    await page.screenshot({path:path.join(BOTS_ARTIFACTS,`bots-settings-model-${name}.png`)})
    await page.getByRole("option",{name:"Sonnet"}).click()

    // Choosing Codex as the default resets its model, and its per-effort list collapses.
    await tiles.nth(1).click()
    assert.equal(await tiles.nth(1).getAttribute("aria-checked"),"true")
    // The daemon is still asking Codex: the list waits inside itself, then fills in by itself.
    await modelButton.click()
    await page.locator(".pa-select-status[data-kind=loading]").waitFor()
    await listInsideDialog(page,`${name} codex model refreshing`)
    await page.screenshot({path:path.join(BOTS_ARTIFACTS,`bots-settings-model-refreshing-${name}.png`)})
    await page.locator(".pa-select-status").waitFor({state:"detached",timeout:15000})
    assert.deepEqual(await page.locator(".pa-select-list [role=option]").allTextContents(),["Agent 預設（model-a）","model-a","model-b-mini"])
    await listInsideDialog(page,`${name} codex model`)
    await page.keyboard.press("Escape")
    await page.locator(".pa-select-list").waitFor({state:"detached"})
    assert.equal(await dialog.isVisible(),true,"Esc closes the list first")

    // A kind's page: the four agents, order by selection, each with the catalog's list.
    await page.locator(".pa-bs-kind-row").nth(1).click()
    await page.locator(".pa-bs-detail").waitFor()
    assert.equal(await page.locator(".pa-bs-order-item").count(),4,"all four agents are listed on a kind's page")
    const orders=await page.locator(".pa-bs-order-item .pa-bs-order").allTextContents()
    assert.deepEqual(orders,["","1","2","3"],"Codex, Gemini, Grok keep their saved order")
    const geminiModel=page.locator(".pa-bs-order-item").nth(2).locator(".pa-select-button").first()
    await geminiModel.scrollIntoViewIfNeeded()
    await geminiModel.click()
    assert.deepEqual(await page.locator(".pa-select-list [role=option]").allTextContents(),["Agent 預設（Pro）","Flash"],"Gemini reads the same catalog")
    await listInsideDialog(page,`${name} gemini model`)
    await page.keyboard.press("Escape")
    // The last agent's lists sit near the bottom of the pane and must still stay inside.
    const codexEffort=page.locator(".pa-bs-order-item").nth(1).locator(".pa-select-button").nth(1)
    await page.locator(".pa-bs-body").evaluate(e=>e.scrollTop=e.scrollHeight)
    await codexEffort.click()
    await listInsideDialog(page,`${name} codex effort`)
    await page.screenshot({path:path.join(BOTS_ARTIFACTS,`bots-settings-kind-${name}.png`)})
    await page.keyboard.press("Escape")
    // Adding Claude makes it fourth; removing Codex renumbers the rest.
    await page.locator(".pa-bs-order-toggle").nth(0).click()
    await page.locator(".pa-bs-order-toggle").nth(1).click()
    assert.deepEqual(await page.locator(".pa-bs-order-item .pa-bs-order").allTextContents(),["3","","1","2"])
    await page.getByRole("button",{name:"返回"}).click()
    await page.locator(".pa-bs-kinds").waitFor()

    // Save the whole document, then the memory tab.
    await page.getByRole("button",{name:"儲存",exact:true}).click()
    await page.waitForFunction(()=>window.saved?.default?.agent==="codex")
    const saved=await page.evaluate(()=>window.saved)
    assert.deepEqual(saved.rules[1].agents.map(a=>a.agent),["antigravity","grok","claude"])
    await page.getByRole("button",{name:"共用記憶"}).click()
    await page.getByRole("button",{name:"新增記憶"}).click()
    await page.locator(".pa-bs-memory textarea").last().fill("設計相關先給兩個方向")
    await page.screenshot({path:path.join(BOTS_ARTIFACTS,`bots-settings-memory-${name}.png`)})
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
