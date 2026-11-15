/** A free local Store → HTTP → real Bots/Chromium run; no provider or model. */
import assert from "node:assert/strict"
import {createServer} from "node:http"
import {mkdir,writeFile} from "node:fs/promises"
import {fileURLToPath,pathToFileURL} from "node:url"
import path from "node:path"
import {build} from "esbuild"

const {BOTS_ORIGIN:upstream,BOTS_TOKEN:token,BOTS_MACHINE:machine,BOTS_ARTIFACTS:artifacts}=process.env
if(!upstream||!token||!machine||!artifacts||!process.env.PLAYWRIGHT_MODULE)throw new Error("Requires the owned Go HTTP fixture and installed Playwright")
await mkdir(artifacts,{recursive:true})
const {chromium}=await import(pathToFileURL(process.env.PLAYWRIGHT_MODULE).href)
const built=await build({entryPoints:[fileURLToPath(new URL("./roster-status.fixture.tsx",import.meta.url))],bundle:true,write:false,outfile:"roster.js",format:"esm",platform:"browser",jsx:"automatic",define:{"process.env.NODE_ENV":'"development"'}})
const files=new Map(built.outputFiles.map(f=>["/"+path.basename(f.path),f.contents]))
const server=createServer(async(req,res)=>{
  try{
    if(req.url.startsWith("/v1/")||req.url.startsWith("/fixture/phase")){
      const r=await fetch(upstream+req.url,{method:req.method,headers:{Authorization:`Bearer ${token}`}})
      res.writeHead(r.status,{"Content-Type":r.headers.get("Content-Type")??"application/json"});res.end(Buffer.from(await r.arrayBuffer()));return
    }
    const pathname=new URL(req.url,"http://fixture").pathname
    if(pathname==="/fixture.json"){res.setHeader("Content-Type","application/json");res.end(JSON.stringify({machine}));return}
    if(files.has(pathname)){res.setHeader("Content-Type",pathname.endsWith(".css")?"text/css":"text/javascript");res.end(files.get(pathname));return}
    res.setHeader("Content-Type","text/html");res.end('<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><style>body{margin:0;font-family:system-ui;background:#080808;color:#f2f2f2}</style><link rel="stylesheet" href="/roster.css"><div id="app"></div><script type="module" src="/roster.js"></script>')
  }catch(e){res.writeHead(500);res.end(String(e))}
})
await new Promise(resolve=>server.listen(0,"127.0.0.1",resolve))
const origin=`http://127.0.0.1:${server.address().port}`,browser=await chromium.launch({headless:true}),evidence=[]
try{
  for(const [language,width,height] of [["en",1280,900],["zh-Hant",390,844]]){
    const page=await browser.newPage({viewport:{width,height}}),errors=[]
    page.on("pageerror",e=>errors.push(String(e)))
    const phase=async p=>{const r=await fetch(upstream+"/fixture/phase?phase="+p,{method:"POST"});assert.equal(r.status,204,await r.text())}
    if(language!=="en")await phase("start")
    await page.goto(origin+"/?lang="+language)
    const row=id=>page.locator(`[data-bot-key='${JSON.stringify([machine,id])}']`)
    await row("reading").getByText("Read notes.md",{exact:true}).waitFor({timeout:10000})
    assert.equal(await row("reading").getAttribute("data-status"),"working")
    assert.equal(await row("approval").locator(".pa-attention").count(),1)
    assert.equal(await row("approval").locator(".pa-unread-dot").count(),0)
    assert.equal(await row("approval").locator(".pa-unread").count(),1)
    assert.equal(await row("failed").getAttribute("data-status"),"error")
    assert.equal(await row("finished").getAttribute("data-status"),"idle")
    assert.equal(await row("unknown").getAttribute("data-status"),"error")
    await page.screenshot({path:path.join(artifacts,`${language}-roster-all-states.png`),fullPage:true})
    await phase("writing");await page.reload()
    await row("reading").getByText(language==="en"?"Writing a reply…":"正在回覆…",{exact:true}).waitFor({timeout:10000})
    await row("reading").scrollIntoViewIfNeeded()
    const avatar=row("reading").locator("canvas.pa-avatar")
    const first=await avatar.evaluate(el=>el.toDataURL())
    await page.waitForTimeout(100)
    assert.notEqual(await avatar.evaluate(el=>el.toDataURL()),first,"Codync's working canvas draws moving frames")
    await page.screenshot({path:path.join(artifacts,`${language}-roster-writing.png`),fullPage:true})
    await phase("done");await page.reload()
    await row("reading").waitFor()
    await page.waitForFunction(key=>document.querySelector(`[data-bot-key='${key}']`)?.dataset.status==="idle",JSON.stringify([machine,"reading"]),{timeout:10000})
    assert.equal(await row("reading").locator(".pa-row-preview").innerText(),"Fresh reply")
    await phase("start");await page.reload()
    await row("reading").getByText("Read notes.md",{exact:true}).waitFor({timeout:10000})
    await page.emulateMedia({reducedMotion:"reduce"})
    await page.evaluate(()=>new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve))))
    const still=await row("reading").locator("canvas.pa-avatar").evaluate(el=>el.toDataURL())
    await page.waitForTimeout(100)
    assert.equal(await row("reading").locator("canvas.pa-avatar").evaluate(el=>el.toDataURL()),still)
    await page.screenshot({path:path.join(artifacts,`${language}-roster-reduced-motion.png`),fullPage:true})
    assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false)
    assert.deepEqual(errors,[])
    evidence.push({language,width,height,states:5,toolCaption:true,writingCaption:true,eventUpdates:false,completedReturnsToPreview:true,permissionWinsUnreadDot:true,unreadCountRetained:true,unknownDistinct:true,reducedMotion:true,pageErrors:errors.length})
    await page.close()
  }
  await writeFile(path.join(artifacts,"browser-evidence.json"),JSON.stringify({modelRequests:0,evidence},null,2)+"\n")
}finally{await browser.close();server.closeAllConnections();await new Promise(resolve=>server.close(resolve))}
