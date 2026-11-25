// Compact now answers on a 390px phone: compacting with a spinner, the
// summarized sizes, and a plain reason with Try again when the turn fails.
import assert from "node:assert/strict"
import {createServer} from "node:http"
import {mkdir,readFile} from "node:fs/promises"
import path from "node:path"
import {pathToFileURL} from "node:url"
import {build} from "esbuild"

const {PLAYWRIGHT_MODULE,BOTS_ARTIFACTS,CHROME}=process.env
assert.ok(PLAYWRIGHT_MODULE&&BOTS_ARTIFACTS,"Set PLAYWRIGHT_MODULE and BOTS_ARTIFACTS")
const {chromium}=await import(pathToFileURL(PLAYWRIGHT_MODULE).href)
await mkdir(BOTS_ARTIFACTS,{recursive:true})
const bundle=await build({
  entryPoints:[new URL("fixtures.ts",import.meta.url).pathname],
  outfile:"project-assistants-fixture.js",write:false,bundle:true,format:"esm",platform:"browser",
  jsx:"automatic",define:{"process.env.NODE_ENV":'"development"'}
})
const files=new Map(bundle.outputFiles.map(file=>["/"+path.basename(file.path),file.contents]))
const html=await readFile(new URL("../../tools/fixtures/project-assistants.html",import.meta.url))
const catalogs=new Map(await Promise.all(["en","zh-Hant"].map(async tag=>[
  `/catalog/${tag}.json`,await readFile(new URL(`../../public/catalogs/${tag}.json`,import.meta.url))
])))
const server=createServer((req,res)=>{
  const {pathname}=new URL(req.url,"http://fixture")
  const file=files.get(pathname)
  if(file){res.setHeader("Content-Type",pathname.endsWith(".css")?"text/css":"text/javascript");res.end(file);return}
  if(catalogs.has(pathname)){res.setHeader("Content-Type","application/json");res.end(catalogs.get(pathname));return}
  if(pathname==="/"){res.setHeader("Content-Type","text/html");res.end(html);return}
  res.writeHead(404).end()
})
await new Promise((resolve,reject)=>server.once("error",reject).listen(0,"127.0.0.1",resolve))
const origin=`http://127.0.0.1:${server.address().port}`
let browser
try{
  browser=await chromium.launch({headless:true,executablePath:CHROME,args:["--disable-features=OmniboxEverywhere"]})
  for(const language of ["zh-Hant","en"]){
    const context=await browser.newContext({viewport:{width:390,height:844},deviceScaleFactor:2,reducedMotion:"reduce",hasTouch:true,isMobile:true})
    try{
      const page=await context.newPage()
      page.setDefaultTimeout(10000)
      const errors=[]
      page.on("pageerror",error=>errors.push(error.message))
      await page.addInitScript(tag=>localStorage.setItem("ui_language",tag),language)
      await page.goto(origin+"/?lang="+language+"&header=1")
      await page.waitForFunction(()=>!!window.botsFixture)
      await page.evaluate(()=>{window.botsFixture.compactionFixture();window.botsFixture.open()})
      await page.locator('.pa-bot-row[data-bot-key=\'["studio","bot-2"]\']').click()
      const zh=language==="zh-Hant",row=page.locator(".pa-compact-status")
      const press=async()=>{
        await page.locator(".pa-context-button").click()
        await page.getByRole("menuitem",{name:zh?"立即壓縮":"Compact now"}).click()
      }
      const shot=name=>page.screenshot({path:path.join(BOTS_ARTIFACTS,`compact-${name}-${language}-390.png`)})
      await press()
      await page.locator('.pa-compact-status[data-state="running"]').waitFor()
      assert.match(await row.innerText(),zh?/壓縮中/:/Compacting/)
      assert.equal(await row.getAttribute("aria-busy"),"true")
      const box=await row.boundingBox()
      assert.ok(box&&box.x>=0&&box.x+box.width<=390&&box.y+box.height<=844,"the status row is on screen")
      await shot("running")
      await page.evaluate(()=>window.botsFixture.finishCompaction(true))
      await page.locator('.pa-compact-status[data-state="done"]').waitFor()
      assert.match(await row.innerText(),/694\.2K → 41K/)
      await shot("done")
      await row.getByRole("button",{name:zh?"關閉":"Dismiss"}).click()
      await press()
      await page.locator('.pa-compact-status[data-state="running"]').waitFor()
      await page.evaluate(()=>window.botsFixture.finishCompaction(false))
      await page.locator('.pa-compact-status[data-state="failed"]').waitFor()
      const failed=await row.innerText()
      assert.match(failed,zh?/Agent 沒有回報壓縮結果/:/did not report a compaction/)
      assert.ok(await row.getByRole("button",{name:zh?"重試":"Try again"}).isVisible())
      await shot("failed")
      await row.getByRole("button",{name:zh?"關閉":"Dismiss"}).click()
      await page.locator(".pa-context-button").click()
      const newSessionItem=page.getByRole("menuitem",{name:zh?"開新 session":"New session"})
      assert.doesNotMatch(await newSessionItem.innerText(),/附交接|handoff/i)
      await newSessionItem.click()
      const dialog=page.locator("dialog.pa-roster-confirm")
      await dialog.waitFor()
      await dialog.getByRole("button",{name:zh?"開新 session":"New session"}).click()
      const fresh=page.locator("[data-new-session-status]")
      await fresh.waitFor()
      assert.match(await fresh.innerText(),zh?/已開新 session.*全新上下文/:/New session ready.*fresh context/)
      assert.equal(await page.locator(".pa-roster-notice").count(),0,"in-chat new session does not float a roster notice over the composer")
      assert.equal(await page.locator(".pa-context-button").count(),0,"stale Last CTX clears after new session")
      const freshBox=await fresh.boundingBox(),composerBox=await page.locator(".pa-composer").boundingBox()
      assert.ok(freshBox&&composerBox&&freshBox.y+freshBox.height<=composerBox.y+1,"status row sits above the composer")
      await shot("new-session")
      assert.deepEqual(errors,[])
      console.log(language,"running/done/failed",JSON.stringify(failed))
    }finally{await context.close()}
  }
}finally{await browser?.close();server.close()}
