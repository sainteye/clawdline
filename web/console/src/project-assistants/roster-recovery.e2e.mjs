/** Roster reads recover on both transports without rendering machine status. */
import assert from "node:assert/strict"
import {createServer} from "node:http"
import {mkdir,readFile,writeFile} from "node:fs/promises"
import {fileURLToPath,pathToFileURL} from "node:url"
import path from "node:path"
import {build} from "esbuild"
const artifacts=process.env.BOTS_ARTIFACTS
if(!artifacts||!process.env.PLAYWRIGHT_MODULE)throw Error("Set BOTS_ARTIFACTS and an installed PLAYWRIGHT_MODULE")
await mkdir(artifacts,{recursive:true})
const {chromium,webkit}=await import(pathToFileURL(process.env.PLAYWRIGHT_MODULE).href)
const built=await build({entryPoints:[fileURLToPath(new URL("./roster-recovery.fixture.tsx",import.meta.url))],bundle:true,write:false,outfile:"roster.js",format:"esm",platform:"browser",jsx:"automatic",define:{"process.env.NODE_ENV":'"development"'}})
const files=new Map(built.outputFiles.map(f=>["/"+path.basename(f.path),f.contents]))
const pictures=new Map(await Promise.all(["ramen","skyline","rain","platform","building"].map(async name=>[
  `/${name}.webp`,await readFile(new URL(`./tower/${name}.webp`,import.meta.url)),
])))
let forbidden=0
const server=createServer((req,res)=>{
  const url=new URL(req.url,"http://fixture")
  if(url.pathname.startsWith("/v1/")){forbidden++;res.writeHead(500);res.end("No live API");return}
  if(pictures.has(url.pathname)){res.setHeader("Content-Type","image/webp");res.end(pictures.get(url.pathname));return}
  if(files.has(url.pathname)){res.setHeader("Content-Type",url.pathname.endsWith(".css")?"text/css":"text/javascript");res.end(files.get(url.pathname));return}
  res.setHeader("Content-Type","text/html");res.end('<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><style>body{margin:0;font:15px system-ui;background:#080808;color:#fafafa}#app{height:100dvh}</style><link rel="stylesheet" href="/roster.css"><div id="app"></div><script type="module" src="/roster.js"></script>')
})
await new Promise(r=>server.listen(0,"127.0.0.1",r))
const origin=`http://127.0.0.1:${server.address().port}`,results=[]
async function checkIndependentSnapshots(page, language) {
  await page.goto(origin+"/?snapshots&lang="+language)
  await page.evaluate(()=>{
    window.rosterRecovery.studio.setCode(null)
    window.rosterRecovery.notebook.setCode(null)
  })
  await page.waitForFunction(()=>document.querySelectorAll(".pa-bot-row[data-bot-key]").length===6,undefined,{timeout:8000})
  const scene=page.locator(".pa-tower-scene")
  const toggle=page.getByRole("button",{name:language==="en"?"Tower view":"大樓檢視",exact:true})
  if(await toggle.getAttribute("aria-pressed")!=="true")await toggle.click()
  await page.waitForFunction(()=>{
    const img=document.querySelector(".pa-tower-scene img")
    return img?.complete&&img.naturalWidth>0
  },undefined,{timeout:8000})
  await page.evaluate(()=>window.rosterRecovery.notebook.setCode("pa_offline"))
  const stale=page.locator(".pa-hud-chip[data-tone=stale]")
  await stale.waitFor({timeout:6000})
  const partlyStale=await scene.locator("img").evaluate(img=>getComputedStyle(img).filter)
  await page.evaluate(()=>window.rosterRecovery.notebook.hold())
  await page.waitForFunction(()=>window.rosterRecovery.notebook.snapshot().active===1,undefined,{timeout:6000})
  const reads=await page.evaluate(()=>window.rosterRecovery.studio.snapshot().reads)
  await page.waitForFunction(n=>window.rosterRecovery.studio.snapshot().reads>n,reads,{timeout:5000})
    .catch(()=>assert.fail("a stalled snapshot source blocks healthy roster refreshes"))
  assert.equal(partlyStale,"none","one stale source dims the whole scene")
  await page.evaluate(()=>{
    window.rosterRecovery.notebook.setCode(null)
    window.rosterRecovery.notebook.release()
  })
  await stale.waitFor({state:"hidden",timeout:6000})
  const proof=await page.evaluate(()=>({studio:window.rosterRecovery.studio.snapshot(),notebook:window.rosterRecovery.notebook.snapshot()}))
  assert.equal(proof.studio.maxActive,1)
  assert.equal(proof.notebook.maxActive,1)
  return proof
}
async function checkCloudRosterBeforeVersionReplay(page) {
  await page.goto(origin+"/?cloud&lang=zh-Hant")
  const error=page.getByText("暫時無法載入 Bot 清單。",{exact:true})
  await error.waitFor()
  assert.deepEqual(await page.evaluate(()=>["studio","notebook"].map(id=>window.cloudRoster.reads(id))),[1,1])
  const rows=page.locator(".pa-bot-row[data-bot-key]")
  await page.evaluate(()=>window.cloudRoster.restore("studio"))
  await page.waitForFunction(()=>document.querySelectorAll(".pa-bot-row[data-bot-key]").length===3,undefined,{timeout:7000})
  await error.waitFor({state:"hidden"})
  await page.evaluate(()=>window.cloudRoster.restore("notebook"))
  await page.waitForFunction(()=>document.querySelectorAll(".pa-bot-row[data-bot-key]").length===6,undefined,{timeout:7000})
  assert.equal(await rows.count(),6)
}

async function checkMobileToolbarAndRetry(page,language,width) {
  await page.goto(origin+"/?snapshots&retry&hold&lang="+language)
  const zh=language!=="en"
  const tower=page.locator(".pa-view-toggle")
  if(await tower.getAttribute("aria-pressed")==="true")await tower.click()
  const error=page.getByText(zh?"暫時無法載入 Bot 清單。":"Could not load the Bot list right now.",{exact:true})
  const stored=()=>page.evaluate(()=>JSON.parse(localStorage.getItem("roster-recovery-diagnostic")??"null")?.rows??[])
  const release=()=>page.evaluate(()=>{
    window.rosterRecovery.studio.release()
    window.rosterRecovery.notebook.release()
  })
  assert.deepEqual(await stored(),[])
  await page.context().setOffline(true)
  try {
    await release()
    await error.waitFor()
    const rows=await stored()
    assert.equal(rows.length,1)
    assert.equal(rows[0].event,"cloud.bots.roster.unavailable")
    assert.deepEqual(rows[0].data,{
      machine:null,source_count:2,searching:false,order:"default",online:false,
      sources:["studio|pa_invalid|||||","notebook|pa_invalid|||||"],
    })
  } finally {
    await page.context().setOffline(false)
  }
  await page.reload()
  assert.equal((await stored()).length,1,"the loss of the old page keeps the displayed failure")
  await release()
  await error.waitFor()
  if(width<701){
    const controls=await page.locator(".pa-roster .pa-toolbar .pa-circle").evaluateAll(buttons=>buttons.map(button=>{
      const {left,right}=button.getBoundingClientRect()
      return {left,right}
    }))
    // An unreadable source offers no create action.
    assert.equal(controls.length,3)
    assert.ok(controls[2].left-controls[1].right<=12,"the toolbar actions are still spread across the phone")
    assert.ok(controls[0].right-controls[0].left>=44)
    assert.doesNotMatch(await page.locator(".pa-roster-filter").innerText(),/Bots\s+…/)
  }
  await page.screenshot({path:path.join(artifacts,`${language}-${width}-toolbar-retry.png`)})
  await page.evaluate(()=>{
    window.rosterRecovery.studio.setCode(null)
    window.rosterRecovery.notebook.setCode(null)
  })
  await page.getByRole("button",{name:zh?"重試":"Try again",exact:true}).click()
  await page.waitForFunction(()=>document.querySelectorAll(".pa-bot-row[data-bot-key]").length===6)
}
try {
  for(const engine of [chromium,webkit])for(const [language,width] of [["zh-Hant",390],["en",1280]]) {
    const browser=await engine.launch({headless:true,...(engine===chromium?{args:["--disable-features=OmniboxEverywhere"]}:{})}),page=await browser.newPage({viewport:{width,height:844}})
    const errors=[];page.on("pageerror",error=>errors.push(error.message))
    const name=`${engine.name()}-${language}-${width}`
    try {
      await page.goto(origin+"/?lang="+language)
      const rows=page.locator(".pa-bot-row[data-bot-key]")
      const waitForRows=count=>page.waitForFunction(n=>document.querySelectorAll(".pa-bot-row[data-bot-key]").length===n,count)
      const diagnostic=page.locator(".pa-status-bar,.pa-source-status,.pa-connection-notice")
      const noConnectionCopy=async()=>{
        assert.equal(await diagnostic.count(),0)
        assert.doesNotMatch(await page.locator(".pa-roster").innerText(),/pa_offline|pa_revoked|暫時連不上|已連線|快取資料|connection details|cached copy/i)
      }
      await page.getByText(language==="en"?"Could not load the Bot list right now.":"暫時無法載入 Bot 清單。",{exact:true}).waitFor()
      await noConnectionCopy()
      assert.doesNotMatch(await page.locator(".pa-roster-filter").innerText(),/Bots\s+0/)
      await page.screenshot({path:path.join(artifacts,`${name}-first-read-unavailable.png`)})
      await page.evaluate(()=>{window.rosterRecovery.studio.setCode(null)})
      await waitForRows(3)
      await page.evaluate(()=>{window.rosterRecovery.notebook.setCode(null)})
      await waitForRows(6)
      await noConnectionCopy()
      await rows.first().click({button:"right"})
      const sidebarMenu=page.getByRole("menu")
      const zh=language!=="en"
      await sidebarMenu.getByRole("menuitem",{name:zh?"設為主助理":"Set as lead assistant",exact:true}).waitFor()
      const remove=sidebarMenu.getByRole("menuitem",{name:zh?"刪除 Bot":"Delete Bot",exact:true})
      assert.equal(await remove.isEnabled(),true)
      await remove.click()
      await page.getByRole("dialog",{name:zh?"刪除 Bot":"Delete Bot",exact:true}).waitFor()
      await page.keyboard.press("Escape")
      const calls=await page.evaluate(()=>window.rosterRecovery.notebook.snapshot().reads)
      await page.evaluate(()=>{window.rosterRecovery.studio.setCode("pa_offline");window.rosterRecovery.notebook.setCode("pa_offline")})
      await page.waitForFunction(n=>window.rosterRecovery.notebook.snapshot().reads>n,calls)
      assert.equal(await rows.count(),6,"temporary failures retain the last visible roster")
      await noConnectionCopy()
      await page.screenshot({path:path.join(artifacts,`${name}-retained.png`)})
      const streams=await page.evaluate(()=>window.rosterRecovery.studio.snapshot().cursors.length)
      await page.evaluate(()=>{window.rosterRecovery.studio.setCode(null);window.rosterRecovery.notebook.setCode(null)})
      await page.waitForFunction(n=>window.rosterRecovery.studio.snapshot().cursors.length>n,streams)
      await page.evaluate(()=>window.rosterRecovery.studio.setCode("pa_revoked"))
      await waitForRows(3)
      await noConnectionCopy()
      const proof=await page.evaluate(()=>({studio:window.rosterRecovery.studio.snapshot(),notebook:window.rosterRecovery.notebook.snapshot()}))
      assert.equal(proof.studio.maxActive,1)
      assert.equal(proof.studio.maxStreams,1)
      assert.equal(proof.notebook.maxActive,1)
      assert.equal(proof.studio.cursors[1],"fixture-cursor","automatic recovery preserves the applied cursor")
      assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true)
      const snapshots=await checkIndependentSnapshots(page,language)
      await checkMobileToolbarAndRetry(page,language,width)
      if(width<701)await checkCloudRosterBeforeVersionReplay(page)
      assert.deepEqual(errors,[])
      results.push({name,...proof,snapshots})
    }catch(error){await page.screenshot({path:path.join(artifacts,`${name}-FAIL.png`)});throw error}
    finally{await browser.close()}
  }
  assert.equal(forbidden,0)
}finally{server.closeAllConnections();await new Promise(r=>server.close(r));await writeFile(path.join(artifacts,"roster-recovery-results.json"),JSON.stringify({results,forbidden},null,2)+"\n")}
console.log(JSON.stringify({results,forbidden}))
