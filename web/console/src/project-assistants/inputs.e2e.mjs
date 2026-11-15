/** The browser reads the actual explicit inputs over the real HTTP gate. */
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
const bundle=await build({entryPoints:[fileURLToPath(new URL("./inputs.fixture.tsx",import.meta.url))],bundle:true,write:false,outfile:"inputs.js",format:"esm",platform:"browser",jsx:"automatic",define:{"process.env.NODE_ENV":'"development"'}})
const content=new Map(bundle.outputFiles.map(f=>["/"+path.basename(f.path),f.contents]))
let reads=0
const server=createServer(async(req,res)=>{
  try{
    if(req.url.startsWith("/v1/")){
      if(req.url.includes("/input?"))reads++
      const chunks=[];for await(const chunk of req)chunks.push(chunk)
      const body=Buffer.concat(chunks),headers={Authorization:`Bearer ${token}`}
      if(req.headers['content-type'])headers['Content-Type']=req.headers['content-type']
      if(req.headers['idempotency-key'])headers['Idempotency-Key']=req.headers['idempotency-key']
      const r=await fetch(upstream+req.url,{method:req.method,headers,...(body.length?{body}:{})})
      if(!r.ok)console.log("HTTP refusal",req.url,r.status,await r.clone().text())
      res.writeHead(r.status,{"Content-Type":r.headers.get("content-type")??"application/json"});res.end(Buffer.from(await r.arrayBuffer()));return
    }
    const pathname=new URL(req.url,"http://fixture").pathname
    if(pathname==="/fixture.json"){res.setHeader("Content-Type","application/json");res.end(JSON.stringify({machine:process.env.BOTS_MACHINE,bot:process.env.BOTS_BOT}));return}
    if(content.has(pathname)){res.setHeader("Content-Type",pathname.endsWith(".css")?"text/css":"text/javascript");res.end(content.get(pathname));return}
    res.setHeader("Content-Type","text/html");res.end('<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><style>body{margin:0}</style><link rel="stylesheet" href="/inputs.css"><div id="app"></div><script type="module" src="/inputs.js"></script>')
  }catch(e){res.writeHead(500);res.end(String(e))}
})
await new Promise(resolve=>server.listen(0,"127.0.0.1",resolve))
const origin=`http://127.0.0.1:${server.address().port}`,browser=await chromium.launch({headless:true})
const evidence=[]
const input=Buffer.from("Explicit input from the real HTTP fixture.\n")
try{
  for(const [language,width,height] of [["en",1280,900],["zh-Hant",390,844]]){
    const page=await browser.newPage({viewport:{width,height},acceptDownloads:true}),errors=[]
    page.on("pageerror",e=>errors.push(String(e)))
    await page.goto(origin+"/?lang="+language)
    const zh=language!=="en",filename=`notes-${language}.txt`
    await page.locator(".pa-composer input[type=file]").waitFor({state:"attached"})
    await page.waitForFunction(()=>document.querySelector(".pa-attach")?.disabled===false)
    await page.locator(".pa-composer input[type=file]").setInputFiles({name:filename,mimeType:"text/plain",buffer:input})
    await page.locator(".pa-input-chips").filter({hasText:filename}).waitFor()
    assert.equal(await page.locator("textarea").inputValue(),"","files-only draft")
    await page.screenshot({path:path.join(artifacts,language+"-selection.png"),fullPage:true})
    await page.getByRole("button",{name:zh?"傳送訊息":"Send message",exact:true}).click()
    const card=page.locator(".pa-input-card").filter({hasText:filename})
    try{await card.waitFor({timeout:20000})}catch(e){console.log("page errors",errors,"body",await page.locator("body").innerText());await page.screenshot({path:path.join(artifacts,language+"-failure.png"),fullPage:true});throw e}
    await page.screenshot({path:path.join(artifacts,language+"-sent.png"),fullPage:true})
    await page.reload()
    await page.locator(".pa-input-card").filter({hasText:filename}).getByRole("button").click()
    const dialog=page.getByRole("dialog",{name:filename,exact:true})
    await dialog.getByRole("button",{name:zh?"下載檔案":"Download file",exact:true}).waitFor()
    await page.screenshot({path:path.join(artifacts,language+"-readback.png"),fullPage:true})
    const download=page.waitForEvent("download");await dialog.getByRole("button",{name:zh?"下載檔案":"Download file",exact:true}).click()
    const saved=path.join(artifacts,filename);await(await download).saveAs(saved)
    assert.deepEqual(await readFile(saved),input)
    await dialog.getByRole("button",{name:zh?"關閉":"Close",exact:true}).click()
    assert.deepEqual(errors,[])
    evidence.push({language,width,height,filename,fileOnly:true,reopened:true,downloadBytes:input.length,sha256:createHash("sha256").update(input).digest("hex"),pageErrors:errors})
    await page.close()
  }
  await writeFile(path.join(artifacts,"minimal-browser-evidence.json"),JSON.stringify({surface:"local Chromium with actual HTTP/SQLite/owned synthetic ACP; not production Cloud or a real phone",modelRequests:0,evidence},null,2)+"\n")
  // Secondary cases follow a recorded successful minimal path. DOM File paste
  // exercises browser-exposed File objects, not the operating-system clipboard.
  const page=await browser.newPage({viewport:{width:1280,height:900},acceptDownloads:true}),errors=[]
  page.on("pageerror",e=>errors.push(String(e)))
  await page.goto(origin+"/?lang=en")
  const picker=page.locator(".pa-composer input[type=file]"),send=page.getByRole("button",{name:"Send message",exact:true})
  await picker.waitFor({state:"attached"})
  await page.waitForFunction(()=>document.querySelector(".pa-attach")?.disabled===false)
  await picker.setInputFiles({name:"remove.txt",mimeType:"text/plain",buffer:input})
  await page.getByRole("button",{name:"Remove file remove.txt",exact:true}).click()
  assert.equal(await page.locator(".pa-input-chips").count(),0)
  const binary=Buffer.alloc(384*1024+4);for(let i=0;i<binary.length;i++)binary[i]=[0,255,10,3][i%4]
  await page.evaluate(()=>{
    const transfer=new DataTransfer();transfer.items.add(new File([],"dropped-empty",{type:"application/octet-stream"}))
    document.querySelector(".pa-composer").dispatchEvent(new DragEvent("drop",{bubbles:true,cancelable:true,dataTransfer:transfer}))
  })
  await page.evaluate(encoded=>{
    const transfer=new DataTransfer(),raw=atob(encoded),bytes=Uint8Array.from(raw,c=>c.charCodeAt(0));transfer.items.add(new File([bytes],"pasted.bin",{type:"application/octet-stream"}))
    document.querySelector("textarea").dispatchEvent(new ClipboardEvent("paste",{bubbles:true,cancelable:true,clipboardData:transfer}))
  },binary.toString("base64"))
  await page.locator(".pa-input-chips").filter({hasText:"pasted.bin"}).waitFor()
  await page.locator("textarea").fill("Mixed text, an empty dropped file and pasted binary")
  let sends=0;page.on("request",r=>{if(r.method()==="POST"&&r.url().endsWith("/messages"))sends++})
  await page.route("**/upload",route=>route.fulfill({status:503,contentType:"application/json",body:JSON.stringify({contract_version:"project-assistants.v1",error:{code:"pa_offline",field:"injected_upload_failure",retry_after_seconds:0}})}))
  await send.click();await page.getByRole("alert").filter({hasText:"pa_offline"}).waitFor()
  assert.equal(sends,0,"a failed upload must never send text only")
  assert.equal(await page.locator(".pa-input-chips>span").count(),2)
  assert.equal(await page.locator("textarea").inputValue(),"Mixed text, an empty dropped file and pasted binary")
  await page.screenshot({path:path.join(artifacts,"retry-retains-selection.png"),fullPage:true})
  await page.unroute("**/upload");await send.click()
  for(const [name,expected] of [["dropped-empty",Buffer.alloc(0)],["pasted.bin",binary]]){
    await page.locator(".pa-input-card").filter({hasText:name}).getByRole("button").click()
    const dialog=page.getByRole("dialog",{name,exact:true}),download=page.waitForEvent("download")
    await dialog.getByRole("button",{name:"Download file",exact:true}).click()
    const saved=path.join(artifacts,name);await(await download).saveAs(saved);assert.deepEqual(await readFile(saved),expected)
    await dialog.getByRole("button",{name:"Close",exact:true}).click()
  }
  assert.equal(sends,1,"one explicit retry sends the complete selection once")
  await picker.setInputFiles({name:"lost.txt",mimeType:"text/plain",buffer:input});await page.locator("textarea").fill("Keep this draft")
  await page.reload();await page.getByRole("alert").filter({hasText:"selected files are no longer available"}).waitFor()
  assert.equal(await page.locator("textarea").inputValue(),"Keep this draft")
  assert.equal(await send.isDisabled(),true)
  await page.screenshot({path:path.join(artifacts,"reload-refuses-partial-send.png"),fullPage:true})
  await picker.setInputFiles({name:"selected-again.txt",mimeType:"text/plain",buffer:input})
  assert.equal(await send.isEnabled(),true,"explicit reselection replaces missing File objects")
  await page.getByRole("button",{name:"Remove file selected-again.txt",exact:true}).click();await page.locator("textarea").fill("")
  await picker.setInputFiles({name:"cancel.bin",mimeType:"application/octet-stream",buffer:binary})
  let release,started;const ready=new Promise(resolve=>{started=resolve})
  await page.route("**/upload",async route=>{started();await new Promise(resolve=>{release=resolve});try{await route.continue()}catch{}})
  await send.click();await ready
  await page.getByRole("button",{name:"Cancel",exact:true}).click()
  await page.waitForFunction(()=>!document.querySelector(".pa-composer button[type=submit]").disabled)
  release();await page.unroute("**/upload")
  assert.equal(sends,1,"cancellation and ignored late upload responses do not send")
  assert.equal(await page.locator(".pa-input-chips>span").count(),1)
  await send.click();await page.locator(".pa-input-card").filter({hasText:"cancel.bin"}).waitFor()
  assert.equal(sends,2)
  await page.screenshot({path:path.join(artifacts,"mixed-and-retried-files.png"),fullPage:true})
  await page.close()
  const gates=[]
  for(const query of ["api=69","state=offline","state=readonly","state=revoked"]){
    const p=await browser.newPage({viewport:{width:390,height:844}});await p.goto(origin+"/?lang=zh-Hant&"+query)
    await p.locator(".pa-composer").waitFor()
    if(query==="api=69")assert.match(await p.locator("input[type=file]").getAttribute("accept"),/image\/png/)
    else assert.equal(await p.locator(".pa-attach").isDisabled(),true)
    gates.push({query,generalInputUnavailable:true});await p.close()
  }
  assert.deepEqual(errors,[])
  await writeFile(path.join(artifacts,"secondary-browser-evidence.json"),JSON.stringify({modelRequests:0,domFilePaste:true,drop:true,removableChip:true,mixedSend:true,emptyBytes:0,binaryBytes:binary.length,failedUploadSentMessages:0,retryRetainsCompleteSelection:true,reloadRefusesPartialSend:true,reselection:true,cancelDiscardsLateResponse:true,gates,pageErrors:errors},null,2)+"\n")

}finally{await browser.close();server.closeAllConnections();await new Promise(resolve=>server.close(resolve))}
