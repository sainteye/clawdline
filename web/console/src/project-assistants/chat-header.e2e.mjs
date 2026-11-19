import assert from "node:assert/strict"
import {createServer} from "node:http"
import {mkdir,readFile,writeFile} from "node:fs/promises"
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
const evidence=[]
try{
  browser=await chromium.launch({headless:true,executablePath:CHROME,args:["--disable-features=OmniboxEverywhere"]})
  for(const [language,width,height] of [["en",1440,900],["zh-Hant",390,844]]){
    const context=await browser.newContext({viewport:{width,height},reducedMotion:"reduce",acceptDownloads:true})
    try {
      const page=await context.newPage()
      page.setDefaultTimeout(10000)
      const errors=[],forbidden=[]
      page.on("pageerror",error=>errors.push(error.message))
      await page.route("**/*",route=>{
        const url=new URL(route.request().url())
        if(url.origin!==origin||url.pathname.startsWith("/v1")){forbidden.push(url.pathname);return route.abort()}
        return route.continue()
      })
      const screenshot=name=>page.screenshot({path:path.join(BOTS_ARTIFACTS,`${name}-${width}.png`)})
      const zh=language!=="en"
      await page.addInitScript(tag=>localStorage.setItem("ui_language",tag),language)
      const load=async({baseline=false,owned=false,readOnly=false}={})=>{
        await page.goto(origin+"/?lang="+language+(baseline?"":"&header=1"))
        await page.waitForFunction(()=>!!window.botsFixture)
        if(!baseline)await page.evaluate(({owned,readOnly})=>window.botsFixture.headerFixture(owned,readOnly),{owned,readOnly})
        await page.evaluate(()=>window.botsFixture.open())
      }
      const row=page.locator('.pa-bot-row[data-bot-key=\'["studio","bot-0"]\']')
      const readButton=row.locator("..").getByRole("button",{name:zh?"全部標為已讀 · Project assistant":"Mark all as read · Project assistant"})
      const openChat=async()=>{await row.click();await page.getByText("Help me plan the next tasks.",{exact:true}).waitFor()}
      const baseline=!!process.env.BOTS_HEADER_BASELINE
      await load({baseline})
      const focused=control=>control.evaluate(node=>node===document.activeElement)
      if(!baseline){
        // Read-only grants may observe, and the command takes its cursor from a fresh
        // scoped snapshot, even if messages arrive between roster render and the click.
        await readButton.waitFor()
        await page.evaluate(()=>window.botsFixture.a.emitMessage("Synthetic unread after roster render"))
        await screenshot("after-unread-roster")
        await readButton.press("Enter")
        await readButton.waitFor({state:"detached"})
        assert.equal(await focused(row),true)
        assert.equal(await page.locator(".pa-chat-header").count(),0)
        const reads=await page.evaluate(()=>window.botsFixture.log.filter(r=>r.method==="markRead"))
        assert.deepEqual(reads.map(({machine,request})=>[machine,request.cursor]),[["studio",{
          key:{machine_id:"studio",bot_id:"bot-0"},conversation_id:"chat-0",reader_id:"fixture-reader",message_seq:3
        }]])
        assert.equal(await page.locator('.pa-bot-row[data-bot-key=\'["notebook","bot-0"]\']').getAttribute("data-unread"),"true")
        await load()
      }
      await openChat()
      const header=page.locator(".pa-chat-header"),menu=header.getByRole("button",{name:zh?"更多動作":"More actions"})
      if(baseline){
        await screenshot("before-chat")
        await menu.click()
        assert.equal(await page.getByRole("menuitem").count(),9)
        await screenshot("before-menu")
        await page.keyboard.press("Escape")
        continue
      }
      assert.equal(await menu.count(),0)
      assert.equal(await header.locator(".browser-chip").count(),0)
      await screenshot("after-chat")
      const identity=header.locator(".pa-chat-identity"),panel=page.locator(".pa-info-panel")
      const details=panel.getByRole("tab",{name:zh?"詳情":"Details",exact:true})
      const browserButton=panel.getByRole("button",{name:zh?"瀏覽器":"Browser",exact:true})
      const browserDialog=page.locator(".pa-browser")
      const template=panel.getByRole("button",{name:zh?"建立範本":"Create template",exact:true})
      const exportButton=panel.getByRole("button",{name:zh?"匯出 Bot 資料":"Export Bot data",exact:true})
      const refreshName=zh?"檢查新訊息":"Check for updates"
      const refresh=panel.getByRole("button",{name:refreshName,exact:true})
      const openDetails=async()=>{await identity.press("Enter");await details.waitFor();assert.equal(await focused(details),true)}
      const closeDetails=async()=>{await page.keyboard.press("Escape");await panel.waitFor({state:"hidden"});assert.equal(await focused(identity),true)}
      await openDetails()
      await screenshot("after-details-keyboard")
      await browserButton.press("Enter")
      await browserDialog.locator(".browser-close").waitFor()
      await page.keyboard.press("Escape")
      await browserDialog.waitFor({state:"detached"})
      assert.equal(await focused(browserButton),true)
      await template.press("Enter")
      const templateDialog=page.getByRole("dialog",{name:zh?"建立範本":"Create template"})
      await templateDialog.getByRole("textbox").waitFor()
      assert.equal(JSON.parse(await templateDialog.getByRole("textbox").inputValue()).settings.name,"Project assistant")
      await page.keyboard.press("Escape")
      await templateDialog.waitFor({state:"detached"})
      assert.equal(await focused(template),true)
      const pendingDownload=page.waitForEvent("download")
      await exportButton.click()
      const archive=await pendingDownload
      assert.equal(archive.suggestedFilename(),"bot-export.json")
      assert.equal(JSON.parse(await readFile(await archive.path())).bot.conversation_id,"chat-0")
      await screenshot("after-details-management")

      const library=panel.getByRole("tab",{name:zh?"檔案庫":"Library"})
      await details.press("ArrowRight")
      assert.equal(await library.getAttribute("aria-selected"),"true")
      assert.equal(await focused(library),true)
      await panel.getByRole("tabpanel",{name:zh?"檔案庫":"Library"}).waitFor()
      const memory=panel.getByRole("tab",{name:zh?"記憶":"Memory"})
      await library.press("ArrowRight")
      assert.equal(await memory.getAttribute("aria-selected"),"true")
      await panel.locator(".pa-memory").waitFor()
      const settings=panel.getByRole("tab",{name:zh?"Bot 設定":"Bot settings"})
      await settings.click()
      assert.equal(await settings.getAttribute("aria-selected"),"true")
      await panel.locator(".pa-settings-editor").waitFor()
      await details.click()
      const routineName=zh?"設定例行任務":"Set up a routine"
      const addRoutine=panel.getByRole("button",{name:routineName,exact:true})
      await panel.getByText(zh?/還沒有例行任務/:/No routines yet/).waitFor()
      await addRoutine.click()
      const routineDialog=page.getByRole("dialog",{name:routineName,exact:true})
      await routineDialog.waitFor()
      await page.keyboard.press("Escape")
      await routineDialog.waitFor({state:"detached"})
      assert.equal(await focused(addRoutine),true)

      // A trapped snapshot fails, then retry is discoverable inside the visible error.
      await page.evaluate(()=>window.botsFixture.a.configureViews({failure:"pa_offline"}))
      await refresh.click()
      await page.waitForFunction(()=>document.querySelector(".pa-info-actions button")?.disabled)
      assert.equal(await browserButton.isDisabled(),true)
      assert.equal(await template.isDisabled(),true)
      assert.equal(await exportButton.isDisabled(),true)
      await closeDetails()
      const alert=page.locator(".pa-chat").getByRole("alert").filter({hasText:"pa_offline"})
      await alert.waitFor()
      await screenshot("after-error-retry")
      await page.evaluate(()=>window.botsFixture.a.configureViews({failure:null}))
      await alert.getByRole("button",{name:refreshName}).press("Enter")
      await alert.waitFor({state:"detached"})
      assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false)

      await load({readOnly:true})
      await readButton.waitFor()
      await page.evaluate(()=>window.botsFixture.a.configureViews({invalid:"scope"}))
      await readButton.click()
      await row.locator("..").getByRole("alert").filter({hasText:"pa_scope_denied"}).waitFor()
      assert.equal(await page.evaluate(()=>window.botsFixture.log.filter(row=>row.method==="markRead").length),0)
      await page.evaluate(()=>window.botsFixture.a.configureViews({invalid:null}))
      await openChat()
      await openDetails()
      await browserButton.click()
      await browserDialog.getByRole("status").filter({hasText:zh?"這個裝置可以查看瀏覽器；操作需要寫入權限。":"This device can view the browser. Browser controls require write access."}).waitFor()
      assert.equal(await browserDialog.locator(".browser-card-primary").isDisabled(),true)
      await browserDialog.locator(".browser-close").click()
      await browserDialog.waitFor({state:"detached"})
      assert.equal(await focused(browserButton),true)

      await load({owned:true,readOnly:true})
      await readButton.waitFor()
      await readButton.click()
      await readButton.waitFor({state:"detached"})
      await openChat()
      const chip=header.locator(".browser-chip")
      await chip.waitFor()
      await screenshot("after-owned-chip")
      await chip.press("Enter")
      await browserDialog.locator(".browser-close").click()
      await browserDialog.waitFor({state:"detached"})
      assert.equal(await focused(chip),true)
      const log=await page.evaluate(()=>window.botsFixture.log)
      assert.equal(log.filter(row=>row.method==="markRead").length,1,"history-only reader can advance its own cursor")
      assert.ok(log.every(row=>!["send","stop","answer","edit","saveRoutine"].includes(row.method)))
      assert.deepEqual(errors,[])
      assert.deepEqual(forbidden,[])
      evidence.push({language,width,height,entries:9,ownedChip:true,errorRetry:true,focusRestored:true,externalRequests:0,modelInvocations:0})
    } finally {await context.close()}
  }
  if(evidence.length)await writeFile(path.join(BOTS_ARTIFACTS,"chat-header-results.json"),JSON.stringify(evidence,null,2)+"\n")
} finally {
  await browser?.close()
  server.closeAllConnections()
  await new Promise(resolve=>server.close(resolve))
}
