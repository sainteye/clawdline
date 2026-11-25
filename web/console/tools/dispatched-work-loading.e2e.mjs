import assert from "node:assert/strict"
import { mkdir, readFile } from "node:fs/promises"
import { createServer } from "node:http"
import path from "node:path"
import { fileURLToPath, pathToFileURL } from "node:url"
import { build } from "esbuild"

const {PLAYWRIGHT_MODULE, BOTS_ARTIFACTS, CHROME}=process.env
assert.ok(PLAYWRIGHT_MODULE&&BOTS_ARTIFACTS,"Set PLAYWRIGHT_MODULE and BOTS_ARTIFACTS")
const {chromium}=await import(pathToFileURL(PLAYWRIGHT_MODULE).href)
await mkdir(BOTS_ARTIFACTS,{recursive:true})
const bundle=await build({
  entryPoints:[fileURLToPath(new URL("../src/project-assistants/fixtures.ts",import.meta.url))],
  outfile:"project-assistants-fixture.js",write:false,bundle:true,format:"esm",platform:"browser",
  jsx:"automatic",define:{"process.env.NODE_ENV":'"development"'}
})
const files=new Map(bundle.outputFiles.map(file=>[
  "/"+path.basename(file.path),[file.path.endsWith(".css")?"text/css":"text/javascript",file.contents]
]))
files.set("/",["text/html",await readFile(new URL("./fixtures/project-assistants.html",import.meta.url))])
for(const tag of ["en","zh-Hant"]){
  files.set(`/catalog/${tag}.json`,["application/json",await readFile(new URL(`../public/catalogs/${tag}.json`,import.meta.url))])
}
const server=createServer((req,res)=>{
  const file=files.get(new URL(req.url,"http://fixture").pathname)
  if(!file){res.writeHead(404).end();return}
  res.writeHead(200,{"Content-Type":file[0]}).end(file[1])
})
await new Promise((resolve,reject)=>server.once("error",reject).listen(0,"127.0.0.1",resolve))
const origin=`http://127.0.0.1:${server.address().port}`

let browser
const failures=[]
try{
  browser=await chromium.launch({headless:true,executablePath:CHROME,args:["--disable-features=OmniboxEverywhere","--disable-gpu"]})
  for(const [width,height] of [[390,844],[1280,800]]){
    const label=`${width}×${height}`
    let page
    try{
      page=await browser.newPage({viewport:{width,height}})
      page.setDefaultTimeout(10000)
      const errors=[]
      page.on("pageerror",error=>errors.push(error.message))
      await page.addInitScript(()=>localStorage.setItem("ui_language","zh-Hant"))
      await page.goto(origin+"/?lang=zh-Hant&header=1")
      await page.waitForFunction(()=>!!window.botsFixture)
      await page.evaluate(()=>{
        const {a,open}=window.botsFixture
        const list=a.list
        a.list=async (...args)=>{
          const roster=await list(...args)
          const items=roster.items.map(item=>item.bot.key.bot_id==="bot-0"
            ? {...item,dispatched_work:{running:0,needs_you:0,delivered:14}}
            : item)
          return {...roster,items}
        }
        a.seedHistory(101)
        a.holdView("snapshot")
        a.holdEvents()
        open()
      })
      await page.locator(`.pa-bot-row[data-bot-key='["studio","bot-0"]']`).click()
      const chat=page.locator(".pa-main .pa-chat")
      const skeleton=chat.locator(".pa-chat-skeleton")
      const capsule=chat.getByRole("button",{name:/14 個待整合/})
      const waitForRead=mode=>page.waitForFunction(mode=>window.botsFixture.log.some(
        row=>row.method==="view"&&row.machine==="studio"&&row.request.key.bot_id==="bot-0"&&row.request.mode===mode
      ),mode)
      const shot=phase=>page.screenshot({path:path.join(BOTS_ARTIFACTS,`${phase}-${width}x${height}.png`)})
      await waitForRead("snapshot")
      await skeleton.waitFor()
      await shot("loading")
      assert.equal(await capsule.isVisible(),false,"the roster's pending integration count is hidden while the chat skeleton is visible")

      const transcript=chat.locator(".pa-chat-scroll")
      const assertBelowLastMessage=async()=>{
        await capsule.waitFor()
        assert.equal(await transcript.getByRole("button",{name:/14 個待整合/}).count(),1)
        const [last,work]=await Promise.all([
          transcript.locator("[data-message-id]").last().boundingBox(),capsule.boundingBox()
        ])
        assert.ok(last&&work&&work.y>=last.y+last.height,"the capsule is below the last message in the scrollable transcript")
      }
      // The first snapshot has been captured. Only the subsequent event read can deliver this reply.
      await page.evaluate(()=>{
        window.botsFixture.a.emitMessage("Reply from a background update")
        window.botsFixture.a.releaseView()
      })
      await transcript.getByText("History message 101",{exact:true}).waitFor()
      await skeleton.waitFor({state:"hidden"})
      await assertBelowLastMessage()
      await shot("loaded")
      const original=await capsule.elementHandle()
      const assertCapsuleStayed=async()=>{
        await assertBelowLastMessage()
        assert.equal(await capsule.evaluate((element,previous)=>element===previous,original),true,"the capsule stays mounted")
      }

      const newReply=transcript.getByText("Reply from a background update",{exact:true})
      await waitForRead("events")
      assert.equal(await newReply.count(),0)
      await assertCapsuleStayed()
      await page.evaluate(()=>window.botsFixture.a.releaseEvents())
      await newReply.waitFor()
      await assertCapsuleStayed()

      await page.evaluate(()=>window.botsFixture.a.holdHistory())
      await transcript.getByRole("button",{name:"較早的訊息",exact:true}).click()
      await waitForRead("history")
      await transcript.getByRole("button",{name:"正在載入較早的訊息…",exact:true}).waitFor()
      const oldest=transcript.getByText("History message 1",{exact:true})
      assert.equal(await oldest.count(),0)
      await assertCapsuleStayed()
      await page.evaluate(()=>window.botsFixture.a.releaseHistory())
      await oldest.waitFor()
      await assertCapsuleStayed()
      assert.deepEqual(errors,[])
      console.log(`PASS ${label}: initial loading, background update and earlier messages`)
    }catch(error){
      console.error(`FAIL ${label}`,error)
      failures.push(error)
    }finally{
      await page?.close()
    }
  }
}finally{
  await browser?.close()
  server.closeAllConnections()
  await new Promise(resolve=>server.close(resolve))
}
assert.equal(failures.length,0,"all viewports must pass")
