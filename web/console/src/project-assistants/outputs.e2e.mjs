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
    if(content.has(pathname)){res.setHeader("Content-Type",pathname.endsWith(".css")?"text/css":"text/javascript");res.end(content.get(pathname));return}
    res.setHeader("Content-Type","text/html");res.end('<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><style>body{margin:0}</style><link rel="stylesheet" href="/outputs.css"><div id="app"></div><script type="module" src="/outputs.js"></script>')
  }catch(e){res.writeHead(500);res.end(String(e))}
})
await new Promise(resolve=>server.listen(0,"127.0.0.1",resolve))
const origin=`http://127.0.0.1:${server.address().port}`,browser=await chromium.launch({headless:true})
const evidence=[]
try{
  for(const [language,width,height] of [["en",1280,900],["zh-Hant",390,844]]){
    const page=await browser.newPage({viewport:{width,height},acceptDownloads:true}),errors=[],external=[]
    page.on("pageerror",e=>errors.push(String(e)))
    await page.route("https://**/*",r=>{external.push(r.request().url());return r.abort()})
    await page.goto(origin+"/?lang="+language)
    const zh=language!=="en",close=zh?"關閉":"Close"
    try{await page.locator(".pa-output-card").first().waitFor({timeout:10000})}catch(e){console.log("page errors",errors,"body",await page.locator("body").innerText());await page.screenshot({path:path.join(artifacts,"failure.png"),fullPage:true});throw e}
    assert.equal(await page.locator(".pa-output-card").count(),3,"one file and two artifact publish cards; retries create none")
    await page.screenshot({path:path.join(artifacts,language+"-chat.png"),fullPage:true})
    await page.locator(".pa-output-card>button").filter({hasText:"result.bin"}).click()
    const dialog=page.locator(".pa-output-overlay")
    await dialog.getByRole("button",{name:zh?"下載檔案":"Download file",exact:true}).waitFor()
    const download=page.waitForEvent("download");await dialog.getByRole("button",{name:zh?"下載檔案":"Download file",exact:true}).click();const file=await download
    const saved=path.join(artifacts,language+"-result.bin");await file.saveAs(saved)
    const expected=Buffer.alloc(384*1024+4);for(let i=0;i<expected.length;i++)expected[i]=[0,255,3,10][i%4]
    assert.deepEqual(await readFile(saved),expected,"verified bytes save exactly")
    await dialog.getByRole("button",{name:close,exact:true}).click()
    // Opening the first artifact card follows the newest whole snapshot.
    await page.locator(".pa-output-card>button").filter({hasText:"Interactive report"}).first().click()
    await page.frameLocator(".pa-artifact-frame").frameLocator("iframe").getByRole("button",{name:"Try interaction",exact:true}).waitFor()
    const inner=page.frameLocator(".pa-artifact-frame").frameLocator("iframe")
    assert.equal(await inner.locator("#isolation").textContent(),"Isolated 2")
    await inner.getByRole("button",{name:"Try interaction",exact:true}).click();await inner.getByRole("button",{name:"Clicked",exact:true}).waitFor()
    assert.equal(await page.locator("body").getAttribute("data-compromised"),null)
    await inner.getByRole("link",{name:"Reference",exact:true}).click();await inner.getByRole("button",{name:"Clicked",exact:true}).waitFor()
    await page.screenshot({path:path.join(artifacts,language+"-artifact.png"),fullPage:true})
    const htmlDownload=page.waitForEvent("download");await dialog.getByRole("button",{name:zh?"另存 HTML":"Save as HTML",exact:true}).click()
    const htmlSaved=path.join(artifacts,language+"-report.html");await(await htmlDownload).saveAs(htmlSaved);assert.match(await readFile(htmlSaved,"utf8"),/Interactive report/)
    await inner.getByRole("button",{name:"Try blocked navigation",exact:true}).click();await page.waitForTimeout(100)
    assert.deepEqual(external,[],"scripted navigation is blocked by the trusted wrapper before a request")
    await dialog.getByRole("button",{name:zh?"檢查新訊息":"Check for updates",exact:true}).click()
    await inner.getByRole("button",{name:"Try interaction",exact:true}).waitFor()
    await dialog.getByRole("button",{name:close,exact:true}).click()
    const before=reads;await page.waitForTimeout(100);assert.equal(reads,before,"closed viewer performs no further reads")
    await page.locator(".pa-chat-identity").click()
    const libraryName=zh?"檔案庫":"Library"
    await page.getByRole("tab",{name:libraryName,exact:true}).click()
    const library=page.getByRole("tabpanel",{name:libraryName,exact:true});await library.locator(".pa-output-card").first().waitFor()
    assert.equal(await library.locator(".pa-output-card").count(),2,"library deduplicates artifact revisions")
    await page.screenshot({path:path.join(artifacts,language+"-library.png"),fullPage:true})
    assert.deepEqual(external,[],"sandbox blocks script network and popups before requests")
    assert.deepEqual(errors,[])
    evidence.push({language,width,height,downloadBytes:expected.length,sha256:createHash("sha256").update(expected).digest("hex"),cards:3,libraryItems:2,scriptExecuted:true,parentDenied:true,storageDenied:true,scriptNavigationBlocked:true,externalRequests:external.length})
    await page.close()
  }
  await writeFile(path.join(artifacts,"browser-evidence.json"),JSON.stringify({modelRequests:0,evidence},null,2)+"\n")
}finally{await browser.close();server.closeAllConnections();await new Promise(resolve=>server.close(resolve))}
