/** Local browser verification of the real roster; no model, network service or live data. */
import assert from "node:assert/strict"
import {createServer} from "node:http"
import {mkdir,writeFile} from "node:fs/promises"
import {fileURLToPath,pathToFileURL} from "node:url"
import path from "node:path"
import {build} from "esbuild"
const artifacts=process.env.BOTS_ARTIFACTS
if(!artifacts||!process.env.PLAYWRIGHT_MODULE)throw Error("Set BOTS_ARTIFACTS and an existing PLAYWRIGHT_MODULE")
await mkdir(artifacts,{recursive:true})
const pw=await import(pathToFileURL(process.env.PLAYWRIGHT_MODULE).href)
const built=await build({entryPoints:[fileURLToPath(new URL("./roster-order.fixture.tsx",import.meta.url))],bundle:true,write:false,outfile:"roster.js",format:"esm",platform:"browser",jsx:"automatic",define:{"process.env.NODE_ENV":'"development"'}})
const files=new Map(built.outputFiles.map(f=>["/"+path.basename(f.path),f.contents]))
const server=createServer((req,res)=>{
  const file=files.get(new URL(req.url,"http://fixture").pathname)
  if(file){res.setHeader("Content-Type",req.url.endsWith(".css")?"text/css":"text/javascript");res.end(file);return}
  res.setHeader("Content-Type","text/html");res.end('<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><style>body{margin:0;background:#111;color:#eee;font-family:system-ui}</style><link rel="stylesheet" href="/roster.css"><div id="app"></div><script type="module" src="/roster.js"></script>')
})
await new Promise(resolve=>server.listen(0,"127.0.0.1",resolve))
const origin=`http://127.0.0.1:${server.address().port}`,evidence=[]
try{
  for(const engine of ["chromium","webkit"]){
    const browser=await pw[engine].launch({headless:true,...(engine==="chromium"?{args:["--disable-features=OmniboxEverywhere"]}:{})})
    try{
      for(const [language,width,height] of [["en",1280,900],["zh-Hant",390,844]]){
        const context=await browser.newContext({viewport:{width,height},hasTouch:width===390}),page=await context.newPage(),errors=[]
        page.on("pageerror",error=>{errors.push(String(error));console.error("pageerror",String(error))})
        await page.goto(origin+"/?lang="+language)
        const row=name=>page.locator(".pa-roster-entry").filter({has:page.locator(".pa-bot-row strong",{hasText:name})})
        const order=()=>page.locator(".pa-roster-entry .pa-bot-row strong").allTextContents()
        await row("Alpha One").waitFor()
        assert.deepEqual(await order(),["Alpha One","Alpha Two","Beta One","Other Alpha","Named Pinned"])
        await row("Alpha Two").locator(".pa-roster-handle").focus()
        await page.keyboard.press("ArrowUp")
        assert.deepEqual((await order()).slice(0,2),["Alpha Two","Alpha One"])
        await page.reload();await row("Alpha One").waitFor()
        assert.deepEqual((await order()).slice(0,2),["Alpha Two","Alpha One"])
        // Real mouse pointer input uses the same capture path as touch and pen.
        await row("Alpha One").locator(".pa-roster-handle").dragTo(row("Alpha Two"))
        assert.deepEqual((await order()).slice(0,2),["Alpha One","Alpha Two"])
        await row("Beta One").locator(".pa-roster-pin").click()
        const pinned=page.locator(".pa-project-group").first()
        assert.equal((await pinned.locator("h2").innerText()).trim(),language==="en"?"Pinned":"已釘選")
        assert.deepEqual(await pinned.locator(".pa-bot-row strong").allTextContents(),["Beta One"])
        await page.reload();await row("Beta One").waitFor()
        assert.equal((await order())[0],"Beta One")
        if(engine==="chromium"&&language==="en"){
          const other=await context.newPage();await other.goto(origin+"/?lang=en")
          await other.locator('.pa-roster-pin[aria-pressed="true"]').waitFor()
          await row("Beta One").locator(".pa-roster-pin").click()
          await other.waitForFunction(()=>document.querySelectorAll('.pa-roster-pin[aria-pressed="true"]').length===0)
          await row("Beta One").locator(".pa-roster-pin").click()
          await other.locator('.pa-roster-pin[aria-pressed="true"]').waitFor();await other.close()
        }
        const projects=()=>page.locator(".pa-project-group h2").allTextContents()
        const heading=name=>page.locator(".pa-project-heading").filter({has:page.locator("span",{hasText:name})}).filter({has:page.locator(".pa-roster-handle")})
        await heading("Real Pinned Project").locator(".pa-roster-handle").focus();await page.keyboard.press("Home")
        assert.match((await projects())[1],/Real Pinned Project/)
        await page.reload();await row("Named Pinned").waitFor();assert.match((await projects())[1],/Real Pinned Project/)
        await page.getByRole("button",{name:"Switch fixture account"}).click()
        assert.deepEqual(await order(),["Alpha One","Alpha Two","Beta One","Other Alpha","Named Pinned"])
        await page.getByRole("button",{name:"Switch fixture account"}).click()
        assert.equal((await order())[0],"Beta One")
        await row("Beta One").locator(".pa-roster-pin").click()
        assert.equal(await page.locator('.pa-roster-pin[aria-pressed="true"]').count(),0)
        // A Bot cannot be reassigned to another Project by presentation dragging.
        const before=await order()
        await row("Alpha One").locator(".pa-roster-handle").dragTo(row("Other Alpha"))
        assert.deepEqual(await order(),before)
        assert.equal(await page.locator("#selection").innerText(),"")
        // Touch uses actual Chromium input; WebKit is verified by mouse drag and keyboard above.
        if(engine==="chromium"&&width===390){
          await row("Alpha One").scrollIntoViewIfNeeded()
          const source=await row("Alpha Two").locator(".pa-roster-handle").boundingBox(),cdp=await context.newCDPSession(page)
          await cdp.send("Input.dispatchTouchEvent",{type:"touchStart",touchPoints:[{x:source.x+source.width/2,y:source.y+source.height/2}]})
          const target=await row("Alpha One").boundingBox()
          await cdp.send("Input.dispatchTouchEvent",{type:"touchMove",touchPoints:[{x:target.x+target.width/2,y:target.y+target.height/2}]})
          await cdp.send("Input.dispatchTouchEvent",{type:"touchEnd",touchPoints:[]})
          assert.ok((await order()).indexOf("Alpha Two")<(await order()).indexOf("Alpha One"))
          await cdp.detach()
        }
        assert.equal(await page.locator("#selection").innerText(),"")
        assert.deepEqual(errors,[])
        assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false)
        await page.screenshot({path:path.join(artifacts,`${engine}-${language}-roster.png`),fullPage:true})
        evidence.push({engine,language,width,height,mouseDrag:true,keyboardOrder:true,pinAndReload:true,projectOrderAndReload:true,accountIsolation:true,crossProjectRefused:true,noSelectionSideEffect:true,touchDrag:engine==="chromium"&&width===390,pageErrors:errors})
        // Broken persisted bytes show a warning and leave the roster usable.
        await page.evaluate(()=>localStorage.setItem(JSON.stringify(["project-assistants","roster","fixture-a"]),"broken"))
        await page.reload();await row("Alpha One").waitFor();assert.equal(await page.locator(".pa-roster-save-error").count(),1)
        await page.evaluate(()=>localStorage.clear())
        await page.addInitScript(()=>{Storage.prototype.setItem=function(){throw Error("Fixture storage denied")}})
        await page.reload();await row("Alpha One").waitFor()
        await row("Alpha One").locator(".pa-roster-pin").click()
        assert.equal(await page.locator('.pa-roster-pin[aria-pressed="true"]').count(),1)
        assert.equal(await page.locator(".pa-roster-save-error").count(),1)
        assert.deepEqual(errors,[])
        await context.close()
      }
    }finally{await browser.close()}
  }
  await writeFile(path.join(artifacts,"browser-evidence.json"),JSON.stringify({modelRequests:0,evidence},null,2)+"\n")
  console.log(JSON.stringify(evidence))
}finally{server.closeAllConnections();await new Promise(resolve=>server.close(resolve))}
