/** Exchange cards, compaction notices and summaries at phone and desktop sizes (PLAYWRIGHT_MODULE, optional CHROME, BOTS_ARTIFACTS; phase before|after|first-version, default after). */
import assert from "node:assert/strict"
import {createServer} from "node:http"
import {readFile} from "node:fs/promises"
import path from "node:path"
import {createRequire} from "node:module"
import {fileURLToPath,pathToFileURL} from "node:url"

const repo=fileURLToPath(new URL("../../..",import.meta.url))
const consoleRoot=path.join(repo,"web/console")
const artifacts=process.env.BOTS_ARTIFACTS
const require=createRequire(path.join(consoleRoot,"package.json"))
const {build}=require("esbuild")
assert.ok(process.env.PLAYWRIGHT_MODULE,"Set PLAYWRIGHT_MODULE to an existing Playwright entry")
const {chromium}=await import(pathToFileURL(process.env.PLAYWRIGHT_MODULE).href)
const phase=process.argv[2]??"after"
assert.ok(["before","after","first-version"].includes(phase),"Pass before, after or first-version as the first argument")
const bundle=await build({entryPoints:[path.join(consoleRoot,"src/project-assistants/fixtures.ts")],
  outfile:"project-assistants-fixture.js",write:false,bundle:true,format:"esm",platform:"browser",jsx:"automatic",
  define:{"process.env.NODE_ENV":'"development"'}})
const files=new Map(bundle.outputFiles.map(file=>["/"+path.basename(file.path),[file.contents,file.path.endsWith(".css")?"text/css":"text/javascript"]]))
files.set("/",[await readFile(path.join(consoleRoot,"tools/fixtures/project-assistants.html")),"text/html"])
for(const tag of ["en","zh-Hant"])files.set(`/catalog/${tag}.json`,[await readFile(path.join(consoleRoot,`public/catalogs/${tag}.json`)),"application/json"])
const server=createServer((req,res)=>{
  const file=files.get(new URL(req.url,"http://fixture").pathname)
  if(!file){res.writeHead(404).end();return}
  res.writeHead(200,{"Content-Type":file[1]}).end(file[0])
})
await new Promise((resolve,reject)=>server.once("error",reject).listen(0,"127.0.0.1",resolve))
const failures=[]
const check=async(label,action)=>{
  try{await action();console.log("PASS",label)}
  catch(error){failures.push(label);console.error("FAIL",label,error.message)}
}
let browser
try{
  browser=await chromium.launch({headless:true,executablePath:process.env.CHROME,args:["--disable-features=OmniboxEverywhere"]})
  for(const [width,height,language] of [[390,844,"zh-Hant"],[1280,800,"zh-Hant"],[390,844,"en"]]){
    const mobile=width===390,zh=language==="zh-Hant",scenario=`${width}×${height} ${language}`
    const context=await browser.newContext({viewport:{width,height},isMobile:mobile,hasTouch:mobile,
      deviceScaleFactor:1,reducedMotion:"reduce",locale:zh?"zh-TW":"en-US",timezoneId:"Asia/Taipei"})
    try{
      const page=await context.newPage()
      page.setDefaultTimeout(5000)
      const errors=[]
      page.on("pageerror",error=>errors.push(error.message))
      if(mobile){
        const cdp=await context.newCDPSession(page)
        await cdp.send("Emulation.setSafeAreaInsetsOverride",{insets:{top:47,bottom:34,topMax:47,bottomMax:34,left:0,right:0}})
      }
      await page.addInitScript(tag=>localStorage.setItem("ui_language",tag),language)
      const origin=`http://127.0.0.1:${server.address().port}`
      await page.goto(origin+"/?exchange-layout=1&lang="+language)
      await page.addStyleTag({content:"#voice-owner { display:none }"})
      await page.waitForFunction(()=>!!window.botsFixture)
      await page.evaluate(()=>window.botsFixture.open())
      await page.locator('.pa-bot-row[data-bot-key=\'["studio","bot-2"]\']').click()
      await page.locator(".pa-exchange-summary").nth(1).waitFor()
      await page.locator(".pa-background-handoff-done").nth(1).waitFor()
      await page.locator(".pa-composer textarea").waitFor()
      if(artifacts&&zh&&phase==="after")await page.screenshot({path:path.join(artifacts,`${phase}-${width}x${height}.png`)})
      await check(`${scenario} composer and Safe Area`,async()=>{
        const box=await page.locator(".pa-composer").boundingBox()
        assert.ok(box&&box.y>=0&&box.y+box.height<=height-(mobile?34:0)+1,JSON.stringify(box))
        if(mobile)assert.equal(await page.locator(".pa-chat > .pa-chat-footer").evaluate(el=>getComputedStyle(el).paddingBottom),"34px")
      })
      await check(`${scenario} summaries use readable text and fit three lines`,async()=>{
        const summaries=page.locator(".pa-exchange-summary")
        const texts=await summaries.allTextContents()
        assert.equal(texts.length,2)
        assert.match(texts[0],zh?/需要處理/:/Needs attention/)
        assert.match(texts[1],zh?/欄位完整/:/Complete fields/)
        for(const value of texts)assert.doesNotMatch(value,/\*\*|###/)
        for(const summary of await summaries.all()){
          const geometry=await summary.evaluate(el=>{const css=getComputedStyle(el);return{
            align:css.textAlign,height:el.clientHeight,line:parseFloat(css.lineHeight),clamp:css.webkitLineClamp}})
          assert.ok(["start","left"].includes(geometry.align),JSON.stringify(geometry))
          assert.ok(geometry.height<=geometry.line*3+1 && geometry.clamp==="3",JSON.stringify(geometry))
        }
      })
      const request=page.locator('[data-message-id="compact-request"]')
      const summarized=page.locator('[data-message-id="compacted-exchange-layout-compact"]')
      await check(`${scenario} localized and compact system rows`,async()=>{
        assert.equal(await request.locator("p").innerText(),zh?"立即壓縮":"Compact now")
        assert.equal(await summarized.locator("p").innerText(),zh?"已摘要較早的內容，騰出空間":"Earlier context was summarized to make room")
        for(const notice of [request,summarized]){
          const css=await notice.locator("p").evaluate(el=>{const s=getComputedStyle(el);return[s.textAlign,s.fontStyle,s.fontSize]})
          assert.deepEqual(css,["center","normal","12px"])
        }
        const completed=page.locator(".pa-chat-content > p.pa-turn").filter({hasText:zh?/^已完成$/:/^Completed$/})
        assert.equal(await completed.evaluate(el=>getComputedStyle(el).textAlign),"center")
      })
      await check(`${scenario} compact groups and consistent message width`,async()=>{
        const normal=await page.locator('[data-message-id="exchange-layout-reply"]').evaluate(el=>getComputedStyle(el).maxWidth)
        const results=await page.locator(".pa-exchange-entry").evaluateAll(openers=>openers.map(opener=>{
          const card=opener.parentElement
          const summary=card.querySelector(".pa-exchange-summary")
          const reply=card.querySelector(".pa-background-handoff-done")
          const left=card.getBoundingClientRect().left
          return {maxWidth:getComputedStyle(card).maxWidth,left,summaryInset:summary?.getBoundingClientRect().left-left,
            nestedBorder:reply?getComputedStyle(reply).borderLeftWidth:null}
        }))
        assert.equal(results.length,2)
        for(const row of results){
          assert.equal(row.maxWidth,normal)
          assert.ok(row.summaryInset>=8,"the preview has a text inset within its card")
          assert.equal(row.nestedBorder,"0px")
        }
        const bounds=await page.locator(".pa-exchange-attribution, [data-message-id=compact-request], [data-message-id=compacted-exchange-layout-compact]").evaluateAll(rows=>rows.map(el=>el.getBoundingClientRect().toJSON()))
        for(let i=1;i<bounds.length;i++)assert.ok(bounds[i].top-bounds[i-1].bottom<=16,"consecutive rows follow normal chat spacing")
      })
      if(phase!=="first-version") {
        await check(`${scenario} system notices have no menu buttons`,async()=>{
          for(const notice of [request,summarized])assert.equal(await notice.locator(".pa-message-more").count(),0)
        })
        await check(`${scenario} summaries have no leading or redundant middle dot`,async()=>{
          const previews=await page.locator(".pa-exchange-summary").evaluateAll(nodes=>nodes.map(node=>{
            const text=node.firstChild
            const range=document.createRange()
            const topAt=index=>{
              range.setStart(text,index)
              range.setEnd(text,index+1)
              return range.getBoundingClientRect().top
            }
            const value=text.textContent
            const wrappedDots=Array.from(value.matchAll(/\S\s+(·)\s+/gu),
              match=>topAt(match.index)!==topAt(match.index+match[0].indexOf("·")))
            return {value,wrapsBeforeDot:wrappedDots.includes(true)}
          }))
          const violations=previews.flatMap(({value,wrapsBeforeDot})=>[
            ...(/^\s*·|[.?!:;。？！：；]\s+·/u.test(value)?[`Punctuation: ${value}`]:[]),
            ...(wrapsBeforeDot?[`Line starts with a middle dot: ${value}`]:[]),
          ])
          assert.deepEqual(violations,[])
          assert.match(previews[zh?0:1].value,zh?/項目。 更新排程清單/u:/export\. Follow-up/u)
          assert.match(previews[0].value,zh?/台北時間）\s· 需要處理/u:/Taipei time\)\s· Needs attention/u)
        })
      }
      const legacyControls=phase==="first-version"
      const ordinaryMessage=page.locator('[data-message-id="exchange-layout-reply"]')
      if(mobile)await check(`${scenario} ${legacyControls?"notice":"ordinary message"} menu copies displayed text and can quote`,async()=>{
        await context.grantPermissions(["clipboard-read","clipboard-write"],{origin})
        const action=(legacyControls?request:ordinaryMessage).locator(".pa-message-more")
        await action.tap()
        const menu=page.getByRole("menu")
        const quote=menu.getByRole("menuitem",{name:zh?/引用/:/Reply/})
        await quote.waitFor()
        await menu.getByRole("menuitem",{name:zh?/複製/:/Copy/}).click()
        const displayed=legacyControls?await request.locator("p").innerText():await ordinaryMessage.locator("p").innerText()
        assert.equal(await page.evaluate(()=>navigator.clipboard.readText()),displayed)
        await action.tap()
        await quote.click()
        await page.locator(".pa-composer-wrap > .pa-reply").waitFor()
        await page.locator(".pa-composer-wrap > .pa-reply button").click()
      })
      await check(`${scenario} both exchange actions reach their original details`,async()=>{
        const summaryText=zh?"檢查匯出報表":"Review the export"
        const opener=page.locator(".pa-exchange-entry").first()
        await opener.click()
        const detail=page.locator("dialog.pa-exchange-panel[open]")
        await detail.waitFor()
        await detail.getByText(summaryText,{exact:false}).first().waitFor()
        await detail.getByRole("button",{name:zh?"關閉":"Close"}).click()
        await detail.waitFor({state:"hidden"})
        const reply=page.locator(".pa-background-handoff-done").nth(1)
        assert.match(await reply.innerText(),zh?/已回覆 Clawdline.*2:47/s:/Replied to Clawdline.*2:47/s)
        await reply.click()
        await page.locator('.pa-thread-inspector [data-message-id="exchange-layout-second-reply"]').waitFor()
      })
      assert.deepEqual(errors,[])
    }finally{await context.close()}
  }
}finally{
  await browser?.close()
  await new Promise(resolve=>server.close(resolve))
}
assert.equal(failures.length,0,failures.join("\n"))
