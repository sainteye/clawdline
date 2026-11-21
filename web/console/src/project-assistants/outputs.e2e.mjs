/** The browser reads the actual owned-MCP publications over the real HTTP gate. */
import assert from "node:assert/strict"
import {createServer} from "node:http"
import {mkdir,readFile,writeFile} from "node:fs/promises"
import {createHash} from "node:crypto"
import path from "node:path"
import {fileURLToPath,pathToFileURL} from "node:url"
import {build} from "esbuild"

const {BOTS_ORIGIN:upstream,BOTS_TOKEN:token,BOTS_ARTIFACTS:artifacts}=process.env
if(!upstream||!token||!artifacts)throw new Error("Requires the task-owned Go MCP/HTTP fixture")
await mkdir(artifacts,{recursive:true})
if(!process.env.PLAYWRIGHT_MODULE)throw new Error("Set PLAYWRIGHT_MODULE to the installed local Playwright module")
const {chromium}=await import(pathToFileURL(process.env.PLAYWRIGHT_MODULE).href)
const bundle=await build({entryPoints:[fileURLToPath(new URL("./outputs.fixture.tsx",import.meta.url))],bundle:true,write:false,outfile:"outputs.js",format:"esm",platform:"browser",jsx:"automatic",define:{"process.env.NODE_ENV":'"development"'}})
const catalogs=new Map(await Promise.all(["en","zh-Hant"].map(async tag=>[`/catalog/${tag}.json`,await readFile(new URL(`../../public/catalogs/${tag}.json`,import.meta.url))])))
const content=new Map(bundle.outputFiles.map(f=>["/"+path.basename(f.path),f.contents]))
let reads=0
const server=createServer(async(req,res)=>{
  try{
    if(req.url.startsWith("/v1/")){
      if(req.url.includes("/output?"))reads++
      const chunks=[];for await(const chunk of req)chunks.push(chunk)
      const body=Buffer.concat(chunks),headers={Authorization:`Bearer ${token}`}
      if(req.headers['content-type'])headers['Content-Type']=req.headers['content-type']
      if(req.headers['idempotency-key'])headers['Idempotency-Key']=req.headers['idempotency-key']
      const r=await fetch(upstream+req.url,{method:req.method,headers,...(body.length?{body}:{})})
      if(!r.ok)console.log("HTTP refusal",req.url,r.status)
      res.writeHead(r.status,{"Content-Type":r.headers.get("content-type")??"application/json"});res.end(Buffer.from(await r.arrayBuffer()));return
    }
    const pathname=new URL(req.url,"http://fixture").pathname
    if(pathname==="/fixture.json"){res.setHeader("Content-Type","application/json");res.end(JSON.stringify({machine:process.env.BOTS_MACHINE,bot:process.env.BOTS_BOT}));return}
    if(catalogs.has(pathname)){res.setHeader("Content-Type","application/json");res.end(catalogs.get(pathname));return}
    if(content.has(pathname)){res.setHeader("Content-Type",pathname.endsWith(".css")?"text/css":"text/javascript");res.end(content.get(pathname));return}
    res.setHeader("Content-Type","text/html");res.end('<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><style>body{margin:0}</style><link rel="stylesheet" href="/outputs.css"><div id="app"></div><script type="module" src="/outputs.js"></script>')
  }catch(e){res.writeHead(500);res.end(String(e))}
})
await new Promise(resolve=>server.listen(0,"127.0.0.1",resolve))
const origin=`http://127.0.0.1:${server.address().port}`,browser=await chromium.launch({headless:true,executablePath:process.env.CHROME,args:["--disable-features=OmniboxEverywhere"]})
const evidence=[]
try{
  for(const [language,width,height] of [["en",1280,900],["zh-Hant",390,844]]){
    const page=await browser.newPage({viewport:{width,height},locale:language==="en"?"en-US":"zh-TW",acceptDownloads:true}),errors=[],external=[]
    page.on("pageerror",e=>errors.push(String(e)))
    await page.route("https://**/*",r=>{external.push(r.request().url());return r.abort()})
    await page.goto(origin+"/?lang="+language)
    const zh=language!=="en",close=zh?"關閉":"Close"
    try{await page.locator(".pa-output-card").first().waitFor({timeout:10000})}catch(e){console.log("page errors",errors,"body",await page.locator("body").innerText());await page.screenshot({path:path.join(artifacts,"failure.png"),fullPage:true});throw e}
    assert.equal(await page.locator(".pa-output-card").count(),4,"two files and two artifact publish cards; retries create none")
    const reply=page.locator("article").filter({hasText:"You can open it here."})
    const openReport=reply.getByRole("button",{name:"Interactive report",exact:true})
    assert.equal(await openReport.count(),1,"the last reply has one shortcut for both versions of its artifact")
    await page.waitForFunction(() => {
      const bounds = document.querySelector(".pa-reply-artifacts button")?.getBoundingClientRect()
      return bounds && bounds.top >= 0 && bounds.bottom <= innerHeight
    })
    assert.equal(await page.locator("article").filter({hasText:"The report is ready."}).getByRole("button",{name:"Interactive report",exact:true}).count(),0)
    await page.screenshot({path:path.join(artifacts,language+"-chat.png"),fullPage:true})
    await page.locator(".pa-output-card>button").filter({hasText:"result.bin"}).click()
    const dialog=page.locator(".pa-output-overlay")
    await dialog.getByRole("button",{name:zh?"下載檔案":"Download file",exact:true}).waitFor()
    const download=page.waitForEvent("download");await dialog.getByRole("button",{name:zh?"下載檔案":"Download file",exact:true}).click();const file=await download
    const saved=path.join(artifacts,language+"-result.bin");await file.saveAs(saved)
    const expected=Buffer.alloc(384*1024+4);for(let i=0;i<expected.length;i++)expected[i]=[0,255,3,10][i%4]
    assert.deepEqual(await readFile(saved),expected,"verified bytes save exactly")
    assert.equal(await dialog.getByRole("heading",{name:"閱讀手冊"}).count(),0)
    await dialog.getByRole("button",{name:close,exact:true}).click()
    await page.locator(".pa-output-card>button").filter({hasText:"guide.MD"}).click()
    const markdown=page.getByRole("dialog",{name:"guide.MD",exact:true})
    await markdown.getByRole("heading",{name:"閱讀手冊",exact:true}).waitFor({timeout:3000})
    await markdown.getByRole("listitem").filter({hasText:"First item"}).waitFor()
    await markdown.getByRole("cell",{name:"Ready",exact:true}).waitFor()
    assert.equal(await markdown.locator("pre").textContent(),"example command")
    assert.equal(await markdown.getByRole("link",{name:"Unsafe"}).count(),0)
    assert.equal(await markdown.locator("script").count(),0)
    assert.equal(await page.locator("body").getAttribute("data-markdown-executed"),null)
    assert.equal(await markdown.evaluate(el=>el.scrollWidth),await markdown.evaluate(el=>el.clientWidth))
    const markdownDownload=page.waitForEvent("download")
    await markdown.getByRole("button",{name:zh?"下載檔案":"Download file",exact:true}).click()
    const markdownSaved=path.join(artifacts,language+"-guide.MD")
    await(await markdownDownload).saveAs(markdownSaved)
    assert.match(await readFile(markdownSaved,"utf8"),/^# 閱讀手冊\n\n- First item/)
    await page.screenshot({path:path.join(artifacts,language+"-markdown.png"),fullPage:true})
    await page.keyboard.press("Escape")
    await markdown.waitFor({state:"detached"})
    const chatDraft=page.locator(".pa-chat .pa-composer textarea")
    await chatDraft.fill("Synthetic main chat draft")
    // Opening the first artifact card follows the newest whole snapshot.
    await page.locator(".pa-artifact-card").first().scrollIntoViewIfNeeded()
    const preview=page.locator(".pa-artifact-preview iframe").first()
    await preview.waitFor({state:"attached"})
    assert.match(await preview.getAttribute("srcdoc"),/Interactive report/)
    assert.equal(await preview.evaluate(el=>getComputedStyle(el).pointerEvents),"none")
    await openReport.click()
    try {
      await page.frameLocator(".pa-artifact-frame").frameLocator("iframe").getByRole("button",{name:"Try interaction",exact:true}).waitFor({timeout:9000})
    } catch(error) {
      await page.screenshot({path:path.join(artifacts,language+"-artifact-failure.png"),fullPage:true})
      console.log("Artifact workspace diagnosis",await page.locator("dialog,[role=dialog]").allTextContents(),page.frames().map(frame=>frame.url()),errors)
      throw error
    }
    const workspace=page.getByRole("dialog",{name:"Interactive report"})
    const share=workspace.getByLabel(zh?"分享 Artifact":"Share artifact")
    assert.equal(await workspace.evaluate(el=>el.offsetWidth),width)
    const inner=page.frameLocator(".pa-artifact-frame").frameLocator("iframe")
    assert.equal(await inner.locator("#isolation").textContent(),"Isolated 2")
    await inner.getByRole("button",{name:"Try interaction",exact:true}).click();await inner.getByRole("button",{name:"Clicked",exact:true}).waitFor()
    assert.equal(await page.locator("body").getAttribute("data-compromised"),null)
    await inner.getByRole("link",{name:"Reference",exact:true}).click();await inner.getByRole("button",{name:"Clicked",exact:true}).waitFor()
    await page.screenshot({path:path.join(artifacts,language+"-artifact.png"),fullPage:true})
    await share.click()
    const htmlDownload=page.waitForEvent("download");await workspace.getByRole("button",{name:zh?"另存 HTML":"Save as HTML",exact:true}).click()
    const htmlSaved=path.join(artifacts,language+"-report.html");await(await htmlDownload).saveAs(htmlSaved);assert.match(await readFile(htmlSaved,"utf8"),/Interactive report/)
    await inner.getByRole("button",{name:"Try blocked navigation",exact:true}).click();await page.waitForTimeout(100)
    assert.deepEqual(external,[],"scripted navigation is blocked by the trusted wrapper before a request")
    await workspace.getByRole("button",{name:zh?"檢查新訊息":"Check for updates",exact:true}).click()
    await inner.getByRole("button",{name:"Try interaction",exact:true}).waitFor()
    const artifactDraft=workspace.locator(".pa-composer textarea")
    await artifactDraft.fill("Synthetic document change")
    await artifactDraft.press("Escape")
    assert.equal(await workspace.isVisible(),true,"Escape in an editor keeps the viewer open")
    assert.match(await artifactDraft.getAttribute("aria-label"),/Interactive report/)
    const quoteMessageID=await page.locator(".pa-artifact-card").last().evaluate(card=>card.closest("[data-message-id]").dataset.messageId)
    const inset=await artifactDraft.evaluate(el=>el.getBoundingClientRect().left-el.closest("form").getBoundingClientRect().left+parseFloat(getComputedStyle(el).paddingLeft))
    assert.ok(inset>=14,`artifact composer text is only ${inset}px from the left border`)
    let sent,release
    let pending=new Promise(resolve=>release=resolve)
    let accept=false
    await page.route("**/v1/project-assistants/*/messages",async route=>{
      if(route.request().method()!=="POST")return route.continue()
      sent=route.request().postDataJSON()
      await pending
      if(accept){
        await route.fulfill({json:{contract_version:"project-assistants.v1",receipt:{command:sent.command,id:"synthetic-receipt",resource_id:"synthetic-turn",event_seq:0,state:"accepted"}}})
      }else await route.fulfill({status:409,json:{contract_version:"project-assistants.v1",error:{code:"pa_stale_version",field:"synthetic_REFUSAL"}}})
    })
    await workspace.locator(".pa-send").click()
    await workspace.getByText(zh?"傳送中…":"Sending…",{exact:true}).waitFor()
    assert.equal(await workspace.locator(".pa-send").isDisabled(),true)
    release()
    await workspace.getByRole("alert").waitFor()
    assert.equal(sent.quote_message_id,quoteMessageID)
    assert.equal(sent.text,"Synthetic document change")
    assert.equal(await artifactDraft.inputValue(),"Synthetic document change")
    accept=true;pending=new Promise(resolve=>release=resolve)
    await workspace.locator(".pa-send").click()
    await workspace.getByText(zh?"傳送中…":"Sending…",{exact:true}).waitFor()
    release()
    await workspace.getByText(zh?"訊息已送出":"Message sent",{exact:true}).waitFor()
    assert.equal(await artifactDraft.inputValue(),"")
    await workspace.getByRole("button",{name:zh?"查看對話":"View conversation",exact:true}).click()
    await workspace.waitFor({state:"detached"})
    assert.equal(await chatDraft.inputValue(),"Synthetic main chat draft")
    const before=reads;await page.waitForTimeout(100);assert.equal(reads,before,"closed viewer performs no further reads")
    await page.locator(".pa-chat-identity").click()
    const libraryName=zh?"檔案庫":"Library"
    await page.getByRole("tab",{name:libraryName,exact:true}).click()
    const library=page.getByRole("tabpanel",{name:libraryName,exact:true});await library.locator(".pa-output-card").first().waitFor()
    assert.equal(await library.locator(".pa-output-card").count(),3,"library deduplicates artifact revisions")
    await page.screenshot({path:path.join(artifacts,language+"-library.png"),fullPage:true})
    await library.locator(".pa-output-card>button").filter({hasText:"guide.MD"}).click()
    await markdown.getByRole("heading",{name:"閱讀手冊",exact:true}).waitFor()
    await markdown.getByRole("button",{name:close,exact:true}).click()
    assert.deepEqual(external,[],"sandbox blocks script network and popups before requests")
    assert.deepEqual(errors,[])
    evidence.push({language,width,height,downloadBytes:expected.length,sha256:createHash("sha256").update(expected).digest("hex"),cards:4,libraryItems:3,markdownPreview:true,scriptExecuted:true,parentDenied:true,storageDenied:true,scriptNavigationBlocked:true,externalRequests:external.length})
    await page.close()
  }
  await writeFile(path.join(artifacts,"browser-evidence.json"),JSON.stringify({modelRequests:0,evidence},null,2)+"\n")
}finally{await browser.close();server.closeAllConnections();await new Promise(resolve=>server.close(resolve))}
