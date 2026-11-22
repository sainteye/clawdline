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
const bundle=await build({
  entryPoints:[new URL("fixtures.ts",import.meta.url).pathname],
  outfile:"project-assistants-fixture.js",write:false,bundle:true,format:"esm",platform:"browser",
  jsx:"automatic",define:{"process.env.NODE_ENV":'"development"'}
})
const files=new Map(bundle.outputFiles.map(file=>["/"+path.basename(file.path),file.contents]))
const html=await readFile(new URL("../../tools/fixtures/project-assistants.html",import.meta.url))
const server=createServer((req,res)=>{
  const pathname=new URL(req.url,"http://fixture").pathname
  if(pathname==="/"){res.setHeader("Content-Type","text/html");res.end(html);return}
  const file=files.get(pathname)
  if(!file){res.writeHead(404).end();return}
  res.setHeader("Content-Type",pathname.endsWith(".css")?"text/css":"text/javascript")
  res.end(file)
})
await new Promise(resolve=>server.listen(0,"127.0.0.1",resolve))
const origin="http://127.0.0.1:"+server.address().port

async function verify(page,language,width){
  const zh=language!=="en"
  const settingsLabel=zh?"Bot 設定":"Bot settings"
  const removeLabel=zh?"刪除 Bot":"Delete Bot"
  const cancelLabel=zh?"取消":"Cancel"
  const extensionLabel=zh?"應用程式技能與連接器":"Application skills and connectors"
  const rulesLabel=zh?"已儲存的權限規則":"Saved permission rules"
  const panel=page.locator(".pa-info-panel")
  const row=page.locator('.pa-bot-row[data-bot-key=\'["studio","bot-0"]\']')
  const settings=panel.getByRole("tab",{name:settingsLabel,exact:true})
  const name=panel.getByRole("textbox",{name:zh?"名稱":"Name",exact:true})
  const skill=panel.getByRole("switch",{name:"Fixture skill 1",exact:true})
  const revoke=panel.getByRole("button",{name:zh?"撤銷":"Revoke",exact:true})
  const remove=panel.getByRole("button",{name:removeLabel,exact:true})
  await page.goto(origin+"/?lang="+language)
  await page.evaluate(()=>{
    const {a,open}=window.botsFixture
    Object.assign(a,{
      canLoginProfiles:()=>true,
      extensions:async()=>({catalog:{connectors_available:true,items:Array.from({length:20},(_,index)=>({
        kind:"skill",id:"skill:fixture-"+index,name:"Fixture skill "+(index+1),fingerprint:"fixture-"+index
      }))}}),
      canRules:()=>true,
      rules:async()=>({rules:Array.from({length:8},(_,index)=>({rule:"Fixture long permission rule "+index,behavior:"allow"}))}),
      removeRule:()=>{throw Error("Unexpected rule revocation")},
      canManage:()=>true,
      setHidden:()=>{throw Error("Unexpected hide")},
      deleteBot:()=>{throw Error("Unexpected deletion")}
    })
    open()
  })
  await row.click()
  await page.locator(".pa-chat-identity").click()
  await settings.click()
  await remove.waitFor()
  const initialOffset=await remove.evaluate(node=>{
    const inspector=node.closest(".pa-info-panel")
    return Math.round(node.getBoundingClientRect().top-inspector.getBoundingClientRect().top+inspector.scrollTop)
  })
  console.log(JSON.stringify({language,width,initialOffset}))
  const initiallyExpanded=await skill.isVisible()||await revoke.first().isVisible()
  await page.screenshot({path:path.join(BOTS_ARTIFACTS,"settings-"+width+".png")})
  assert.equal(initiallyExpanded,false,"long extension and rule lists start collapsed")
  const extensions=panel.getByText(extensionLabel,{exact:true})
  const rules=panel.getByText(rulesLabel,{exact:true})
  await extensions.click()
  await skill.waitFor()
  await skill.click()
  assert.equal(await skill.getAttribute("aria-checked"),"true")
  await extensions.click()
  assert.equal(await skill.isVisible(),false)
  await rules.click()
  await revoke.last().waitFor()
  assert.equal(await revoke.count(),8)
  await rules.click()
  assert.equal(await revoke.first().isVisible(),false)
  await extensions.click()
  assert.equal(await skill.getAttribute("aria-checked"),"true","collapsed catalog keeps its draft")
  await extensions.click()
  await remove.click()
  const confirmation=panel.getByRole("alertdialog",{name:removeLabel})
  await confirmation.getByRole("button",{name:cancelLabel,exact:true}).click()
  assert.equal(await confirmation.count(),0)
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true)

  // The roster shortcut opens the profile directly without opening the chat first.
  await page.reload()
  await page.evaluate(()=>window.botsFixture.open())
  await row.click({button:"right"})
  await page.getByRole("menuitem",{name:zh?"編輯資料⋯":"Edit profile…",exact:true}).click()
  await page.waitForFunction(()=>document.activeElement?.tagName==="INPUT"&&document.activeElement?.closest(".pa-settings-editor"))
  assert.equal(await settings.getAttribute("aria-selected"),"true")
  assert.equal(await name.inputValue(),"Project assistant")
  assert.ok(await name.evaluate(node=>{
    const bounds=node.getBoundingClientRect()
    return node===document.activeElement&&bounds.top>=0&&bounds.bottom<=innerHeight
  }),"the shortcut brings the editable name into view and focuses it")
  await page.screenshot({path:path.join(BOTS_ARTIFACTS,"profile-"+width+".png")})
  await name.fill("Renamed fixture Bot")
  await panel.getByRole("button",{name:zh?"儲存":"Save",exact:true}).click()
  await page.locator(".pa-chat-identity").getByText("Renamed fixture Bot",{exact:true}).waitFor()
  assert.equal(await page.evaluate(()=>window.botsFixture.log.filter(record=>record.method==="edit").length),1)
  await page.goBack()
  await row.click()
  await page.locator(".pa-chat-identity").getByText("Renamed fixture Bot",{exact:true}).waitFor()
  assert.equal(await panel.isVisible(),false,"opening the same chat again does not replay Edit profile")
  console.log(JSON.stringify({language,width,collapsedLists:true,keptExtensionDraft:true,deleteConfirmationCancelled:true,profileNameSaved:true}))
}

try{
  const checks=[]
  for(const [engine,language,width,height] of [["chromium","en",1440,900],["webkit","zh-Hant",390,844]]){
    const browser=await browsers[engine].launch({headless:true,...(engine==="chromium"?{args:["--disable-features=OmniboxEverywhere"]}:{})})
    try{
      const page=await browser.newPage({viewport:{width,height},isMobile:width===390,hasTouch:width===390,reducedMotion:"reduce"})
      page.setDefaultTimeout(2500)
      const errors=[]
      page.on("pageerror",error=>errors.push(error.message))
      await page.route("**/*",route=>route.request().url().startsWith(origin+"/")?route.continue():route.abort())
      await verify(page,language,width)
      assert.deepEqual(errors,[])
    }catch(error){checks.push(error)}
    finally{await browser.close()}
  }
  if(checks.length)throw new AggregateError(checks,"Bot settings layout checks failed")
}finally{
  server.closeAllConnections()
  await new Promise(resolve=>server.close(resolve))
}
