// PLAYWRIGHT_MODULE=<playwright index.mjs> BOTS_ARTIFACTS=<dir> node memory-retry.e2e.mjs
import assert from "node:assert/strict"
import {createServer} from "node:http"
import {mkdir,readFile} from "node:fs/promises"
import path from "node:path"
import {pathToFileURL} from "node:url"
import {build} from "esbuild"

const {PLAYWRIGHT_MODULE,BOTS_ARTIFACTS,CHROME}=process.env
assert.ok(PLAYWRIGHT_MODULE&&BOTS_ARTIFACTS,"Use an existing Playwright installation and a task-owned evidence directory")
const {chromium}=await import(pathToFileURL(PLAYWRIGHT_MODULE).href)
await mkdir(BOTS_ARTIFACTS,{recursive:true})
const bundle=await build({
  entryPoints:[new URL("fixtures.ts",import.meta.url).pathname],
  outfile:"project-assistants-fixture.js",write:false,bundle:true,format:"esm",platform:"browser",
  jsx:"automatic",define:{"process.env.NODE_ENV":'"development"'},
})
const files=new Map(bundle.outputFiles.map(file=>["/"+path.basename(file.path),file.contents]))
const html=await readFile(new URL("../../tools/fixtures/project-assistants.html",import.meta.url))
const catalog=await readFile(new URL("../../public/catalogs/zh-Hant.json",import.meta.url))
const failures=[]
let reads,refuseRead
const server=createServer((req,res)=>{
  const {pathname}=new URL(req.url,"http://fixture")
  if(pathname==="/fixture/memory"){
    reads++
    res.setHeader("Content-Type","application/json")
    if(refuseRead)res.writeHead(503).end(JSON.stringify({error:{code:"pa_offline",field:"read_deadline",retry_after_seconds:0}}))
    else res.end(JSON.stringify({revision:reads,text:`測試記憶 · 第 ${reads} 次讀取`}))
    return
  }
  const file=files.get(pathname)
  if(file){res.setHeader("Content-Type",pathname.endsWith(".css")?"text/css":"text/javascript");res.end(file);return}
  if(pathname==="/catalog/zh-Hant.json"){res.setHeader("Content-Type","application/json");res.end(catalog);return}
  if(pathname==="/"){res.setHeader("Content-Type","text/html");res.end(html);return}
  res.writeHead(404).end()
})
await new Promise((resolve,reject)=>server.once("error",reject).listen(0,"127.0.0.1",resolve))
const origin=`http://127.0.0.1:${server.address().port}`

async function memoryRetry(page,width) {
  reads=0
  refuseRead=false
  await page.addInitScript(()=>localStorage.setItem("ui_language","zh-Hant"))
  await page.goto(origin+"/?lang=zh-Hant")
  await page.waitForFunction(()=>!!window.botsFixture)
  await page.evaluate(()=>{
    const {a,open}=window.botsFixture
    const view=a.view.bind(a)
    a.view=async(request,signal)=>{
      const answer=await view(request,signal)
      if(a.refuseFixtureChat&&request.mode==="snapshot")throw {error:{code:"pa_capacity_full"}}
      answer.head.memory_revision=answer.head.conversation.memory_revision=a.memoryFixtureRevision
      return answer
    }
    a.memoryFixtureRevision=1
    a.canMemorySettings=()=>true
    a.memoryView=async(key,conversation_id,signal)=>{
      const response=await fetch("/fixture/memory",{signal})
      const value=await response.json()
      if(!response.ok)throw value
      const bot=a.bots.find(bot=>bot.key.bot_id===key.bot_id)
      return {contract_version:"project-assistants.v1",source_epoch:"epoch-1",view:{bot,conversation_id,
        layers:[{layer:"bot",available:true,cap:200,used:1,revision:String(value.revision),truncated:false,
          entries:[{id:"fixture-memory",author:"bot",text:value.text,sources:[],updated_at:"2026-11-26T00:00:00Z"}]}],
        memory:{key,project:bot.project,revision:value.revision,forgotten_through:0,forgotten_count:0}}}
    }
    open()
  })
  await page.locator('.pa-bot-row[data-bot-key=\'["studio","bot-0"]\']').click()
  await page.getByText("Help me plan the next tasks.",{exact:true}).waitFor()
  const info=page.locator(".pa-info-panel"), identity=page.locator(".pa-chat-identity")
  const entry=number=>info.getByRole("button",{name:`閱讀筆記: 測試記憶 · 第 ${number} 次讀取`,exact:true})
  await identity.click()
  await info.getByRole("tab",{name:"記憶",exact:true}).click()
  await entry(1).waitFor()
  assert.equal(reads,1,"the initial opening reads memory once")

  const failNextReload=async()=>{
    refuseRead=true
    await page.evaluate(()=>{window.botsFixture.a.memoryFixtureRevision++})
  }
  const chatSnapshots=()=>page.evaluate(()=>window.botsFixture.log.filter(row=>row.method==="view"&&row.request.mode==="snapshot").length)
  await failNextReload()
  const line=page.locator(".pa-chat-error")
  const retry=line.getByRole("button",{name:"重試",exact:true,includeHidden:true})
  await retry.waitFor({state:"attached"})
  assert.equal(reads,2,"the new revision caused a second read, which failed")
  if(width===390)await info.locator(".pa-info-close").click()
  await retry.waitFor()
  const screenshot=name=>page.screenshot({path:path.join(BOTS_ARTIFACTS,`${name}-${width}.png`)})
  await screenshot("memory-reload-failed")

  refuseRead=false
  const before=reads
  const snapshots=await chatSnapshots()
  await retry.click()
  await page.waitForFunction(()=>!document.querySelector(".pa-chat-error[role='alert'] button[disabled]"))
  assert.equal(reads-before,1,"the chat error line's Retry must make one memory read")
  assert.equal(await chatSnapshots(),snapshots,"a memory-only error does not request a chat snapshot")
  await line.waitFor({state:"detached"})
  if(width===390){
    await identity.click()
    await info.getByRole("tab",{name:"記憶",exact:true}).click()
  }
  await entry(3).waitFor()
  await screenshot("memory-retry-recovered")
  console.log(`PASS ${width}: memory requests 1 (open), 2 (failed reload), 3 (Retry); new memory visible and error line cleared`)

  if(width!==1440)return
  await failNextReload()
  await retry.waitFor()
  await info.getByRole("tab",{name:"詳情",exact:true}).click()
  const checkChat=info.getByRole("button",{name:"檢查新訊息",exact:true})
  await checkChat.click()
  await page.waitForFunction(()=>!document.querySelector(".pa-info-actions button:last-child[aria-busy='true']"))
  assert.equal(await retry.isVisible(),true,"a successful chat snapshot leaves a memory failure on the line")

  await page.evaluate(()=>{window.botsFixture.a.refuseFixtureChat=true})
  await checkChat.click()
  await line.getByText(/pa_capacity_full/).waitFor()
  refuseRead=false
  const bothReads=reads,bothSnapshots=await chatSnapshots()
  await retry.click()
  const retryChat=line.getByRole("button",{name:"檢查新訊息",exact:true})
  await retryChat.waitFor()
  assert.equal(reads,bothReads+1,"Retry with both failures reads memory")
  assert.equal(await chatSnapshots(),bothSnapshots+1,"Retry with both failures also reads chat")
  assert.match(await line.innerText(),/pa_capacity_full/,"recovering memory leaves a failed chat read on the line")

  await page.evaluate(()=>{window.botsFixture.a.refuseFixtureChat=false})
  await retryChat.click()
  await line.waitFor({state:"detached"})
  assert.equal(reads,bothReads+1,"a chat-only failure retries chat without rereading memory")
  console.log("PASS 1440: chat and memory errors survive each other and retry only their pending reads")
}

let browser
try{
  browser=await chromium.launch({headless:true,executablePath:CHROME,args:["--disable-features=OmniboxEverywhere"]})
  for(const [width,height] of [[1440,900],[390,844]]){
    const page=await browser.newPage({viewport:{width,height},reducedMotion:"reduce"})
    page.setDefaultTimeout(8000)
    const errors=[]
    page.on("pageerror",error=>errors.push(error.message))
    await page.route("**/*",route=>new URL(route.request().url()).origin===origin?route.continue():route.abort())
    try{await memoryRetry(page,width);assert.deepEqual(errors,[])}
    catch(error){failures.push(error);console.error(`FAIL ${width}`,error)}
    finally{await page.close()}
  }
}finally{await browser?.close();server.close()}
if(failures.length)process.exitCode=1
