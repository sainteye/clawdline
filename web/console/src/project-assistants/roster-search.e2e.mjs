/** Real Bots UI with paginated snapshots and one refused machine; no live data. */
import assert from "node:assert/strict"
import {createServer} from "node:http"
import {spawn} from "node:child_process"
import {access,mkdir,mkdtemp,readFile,readdir,rm,writeFile} from "node:fs/promises"
import {constants} from "node:fs"
import {userInfo,tmpdir} from "node:os"
import path from "node:path"
import {fileURLToPath} from "node:url"
import {build} from "esbuild"

const artifacts=process.env.BOTS_ARTIFACTS
assert.ok(artifacts,"Set BOTS_ARTIFACTS to a task-owned directory")
await mkdir(artifacts,{recursive:true})
const built=await build({entryPoints:[fileURLToPath(new URL("./roster-search.fixture.tsx",import.meta.url))],bundle:true,write:false,outfile:"search.js",format:"esm",platform:"browser",jsx:"automatic",define:{"process.env.NODE_ENV":'"development"'}})
const files=new Map(built.outputFiles.map(file=>["/"+path.basename(file.path),file.contents]))
const consoleRoot=fileURLToPath(new URL("../..",import.meta.url))
let forbidden=0
const server=createServer(async(req,res)=>{
  const url=new URL(req.url,"http://fixture")
  if(url.pathname.startsWith("/v1/")){forbidden++;res.writeHead(500);res.end("No live API");return}
  if(url.pathname.startsWith("/catalog/")){res.setHeader("Content-Type","application/json");res.end(await readFile(path.join(consoleRoot,"public/catalogs",path.basename(url.pathname))));return}
  if(files.has(url.pathname)){res.setHeader("Content-Type",url.pathname.endsWith(".css")?"text/css":"text/javascript");res.end(files.get(url.pathname));return}
  res.setHeader("Content-Type","text/html");res.end('<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><style>html,body,#app{height:100%;margin:0;background:#080808;color:#fafafa;font:15px system-ui}</style><link rel="stylesheet" href="/search.css"><div id="app"></div><script type="module" src="/search.js"></script>')
})
await new Promise(resolve=>server.listen(0,"127.0.0.1",resolve))
const origin=`http://127.0.0.1:${server.address().port}`
async function executable(){
  if(process.env.CHROME){await access(process.env.CHROME,constants.X_OK);return process.env.CHROME}
  const home=userInfo().homedir,cache=process.env.PLAYWRIGHT_BROWSERS_PATH??(process.platform==="darwin"?path.join(home,"Library/Caches/ms-playwright"):process.platform==="win32"?path.join(process.env.LOCALAPPDATA??path.join(home,"AppData/Local"),"ms-playwright"):path.join(process.env.XDG_CACHE_HOME??path.join(home,".cache"),"ms-playwright"))
  const shells=(await readdir(cache,{withFileTypes:true})).filter(item=>item.isDirectory()&&/^chromium_headless_shell-\d+$/.test(item.name)).sort((a,b)=>Number(b.name.split("-").at(-1))-Number(a.name.split("-").at(-1)))
  const suffix=process.platform==="darwin"?"chrome-mac/headless_shell":process.platform==="win32"?"chrome-win/headless_shell.exe":"chrome-linux/headless_shell"
  for(const shell of shells){const file=path.join(cache,shell.name,suffix);try{await access(file,constants.X_OK);return file}catch{}}
  throw Error("No installed headless shell; this test installs nothing")
}
const profile=await mkdtemp(path.join(tmpdir(),"clawdline-search-748-")),results=[]
let chrome,ws
try{
  chrome=spawn(await executable(),["--headless=new","--remote-debugging-port=0","--use-mock-keychain","--user-data-dir="+profile,"--no-first-run","--no-default-browser-check","--disable-features=OmniboxEverywhere","about:blank"],{stdio:["ignore","ignore","pipe"]})
  const endpoint=await new Promise((resolve,reject)=>{let output="";const timer=setTimeout(()=>reject(Error("Chrome startup: "+output)),20000);chrome.once("error",reject);chrome.once("exit",()=>{clearTimeout(timer);reject(Error("Chrome exited: "+output))});chrome.stderr.on("data",data=>{output+=data;const match=/DevTools listening on (ws:\/\/\S+)/.exec(output);if(match){clearTimeout(timer);resolve(match[1])}})})
  ws=new WebSocket(endpoint);await new Promise((resolve,reject)=>{ws.addEventListener("open",resolve);ws.addEventListener("error",reject)})
  let serial=0;const pending=new Map(),exceptions=new Map()
  ws.addEventListener("message",event=>{const value=JSON.parse(String(event.data));if(value.method==="Runtime.exceptionThrown")exceptions.set(value.sessionId,value.params.exceptionDetails.exception?.description??value.params.exceptionDetails.text);const job=pending.get(value.id);if(!job)return;pending.delete(value.id);value.error?job.reject(Error(value.error.message)):job.resolve(value.result)})
  const send=(method,params={},sessionId)=>new Promise((resolve,reject)=>{const id=++serial,timer=setTimeout(()=>reject(Error(method+" timed out")),15000);pending.set(id,{resolve:value=>{clearTimeout(timer);resolve(value)},reject:error=>{clearTimeout(timer);reject(error)}});ws.send(JSON.stringify({id,method,params,sessionId}))})
  const evaluate=async(sessionId,expression)=>{const answer=await send("Runtime.evaluate",{expression,awaitPromise:true,returnByValue:true},sessionId);if(answer.exceptionDetails)throw Error(answer.exceptionDetails.exception?.description??answer.exceptionDetails.text);return answer.result.value}
  const until=async(sessionId,expression)=>{const end=Date.now()+8000;while(Date.now()<end){if(exceptions.has(sessionId))throw Error(exceptions.get(sessionId));if(await evaluate(sessionId,"Boolean("+expression+")"))return;await new Promise(resolve=>setTimeout(resolve,50))}throw Error("UI wait: "+expression+"\n"+await evaluate(sessionId,"document.body.innerText"))}
  for(const [lang,width] of [["en",1280],["zh-Hant",390]]){
    const {targetId}=await send("Target.createTarget",{url:"about:blank"}),{sessionId}=await send("Target.attachToTarget",{targetId,flatten:true})
    await send("Runtime.enable",{},sessionId)
    await send("Emulation.setDeviceMetricsOverride",{width,height:720,deviceScaleFactor:1,mobile:width===390},sessionId)
    await send("Page.navigate",{url:origin+"/?lang="+lang},sessionId)
    const search=async query=>{await evaluate(sessionId,`(()=>{const input=document.querySelector('.pa-search input');Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value').set.call(input,${JSON.stringify(query)});input.dispatchEvent(new Event('input',{bubbles:true}));})()`)}
    const count=()=>evaluate(sessionId,"document.querySelectorAll('.pa-roster [data-bot-key]').length")
    const capture=async name=>{await writeFile(path.join(artifacts,`${lang}-${name}.json`),JSON.stringify(await evaluate(sessionId,"({text:document.body.innerText,requests:window.rosterSearch.requests})"),null,2));const image=await send("Page.captureScreenshot",{format:"png"},sessionId);await writeFile(path.join(artifacts,`${lang}-${name}.png`),Buffer.from(image.data,"base64"))}
    try{
      await until(sessionId,"document.querySelectorAll('.pa-roster [data-bot-key]').length===3")
      const orderLabel=lang==="en"?"Sort Bots":"Bot 排序"
      const defaultOrder=lang==="en"?"Default order":"預設順序"
      const unreadOrder=lang==="en"?"Unread first":"未讀優先"
      const machineOrder=lang==="en"?"Group by machine":"依機器分群"
      const orderButton=`.pa-roster-filter button[aria-label="${orderLabel}"]`
      const chooseOrder=async label=>{
        await evaluate(sessionId,`document.querySelector(${JSON.stringify(orderButton)}).click()`)
        await until(sessionId,`!!document.querySelector('.pa-select-list[aria-label="${orderLabel}"]')`)
        await evaluate(sessionId,`[...document.querySelectorAll('.pa-select-list [role=option]')].find(n=>n.textContent.trim()===${JSON.stringify(label)}).click()`)
      }
      const names=()=>evaluate(sessionId,"[...document.querySelectorAll('.pa-roster [data-bot-key] strong')].map(n=>n.textContent)")
      assert.equal(await evaluate(sessionId,"!!document.querySelector('.pa-toolbar .pa-roster-unread')"),false)
      const before=await names()
      assert.equal(before[0],"Code review","the unread Bot starts below newer activity")
      await chooseOrder(unreadOrder)
      assert.deepEqual(await names(),["Project assistant",...before.filter(name=>name!=="Project assistant")])
      assert.equal(await count(),3,"read Bots stay in the list")
      await capture("unread")
      await chooseOrder(machineOrder)
      assert.deepEqual(await names(),before)
      assert.equal(await evaluate(sessionId,"document.querySelector('.pa-roster-scroll section').getAttribute('aria-label')"),"studio")
      await capture("machine")
      await send("Page.reload",{},sessionId)
      await until(sessionId,"document.querySelector('.pa-roster-scroll section')?.getAttribute('aria-label')==='studio'")
      await chooseOrder(defaultOrder)
      assert.deepEqual(await names(),before)
      await evaluate(sessionId,"document.querySelector('.pa-view-toggle[aria-pressed=true]')?.click()")
      await until(sessionId,"!!document.querySelector('.pa-search input')")
      await search("cLaWdLiNe")
      await until(sessionId,"document.querySelectorAll('.pa-roster [data-bot-key]').length===3")
      assert.equal(await count(),3,"Project labels match Bots on the first snapshot page")
      await evaluate(sessionId,"[...document.querySelectorAll('.pa-roster-scroll button')].find(n=>/^(Load more|載入更多)$/.test(n.textContent.trim())).click()")
      await until(sessionId,"document.querySelectorAll('.pa-roster [data-bot-key]').length===6")
      await capture("project")
      assert.equal(await evaluate(sessionId,"document.querySelector('.pa-search-clear').getAttribute('aria-label')"),lang==="en"?"Clear search":"清除搜尋")
      await search("Testing")
      await until(sessionId,"document.querySelectorAll('.pa-roster [data-bot-key]').length===1")
      assert.equal(await evaluate(sessionId,"document.querySelector('.pa-roster [data-bot-key] strong').textContent"),"Testing","Bot name searches still reach a later store page")
      await chooseOrder(unreadOrder)
      assert.equal(await evaluate(sessionId,"document.querySelector('.pa-search input').value"),"Testing")
      const chooseMachine=async label=>{
        await evaluate(sessionId,"document.querySelector('.pa-roster-filter > .pa-select .pa-select-button').click()")
        await until(sessionId,"!!document.querySelector('.pa-select-list')")
        await evaluate(sessionId,`[...document.querySelectorAll('.pa-select-list [role=option]')].find(n=>n.textContent.trim()===${JSON.stringify(label)}).click()`)
      }
      await chooseMachine("unpaired")
      await until(sessionId,"!!document.querySelector('.pa-roster-unavailable [role=status]')")
      assert.equal(await count(),0,"selecting a failed machine does not display healthy peer rows")
      await chooseMachine("studio")
      await until(sessionId,"document.querySelectorAll('.pa-roster [data-bot-key]').length===1")
      assert.doesNotMatch(await evaluate(sessionId,"document.querySelector('.pa-roster-filter').innerText"),/could not be read|無法讀取來源/)
      await chooseMachine(lang==="en"?"All machines":"所有機器")
      await search("no-matching-name")
      await until(sessionId,"document.querySelectorAll('.pa-roster [data-bot-key]').length===0&&!document.querySelector('[data-loading-id=\"bots-roster\"]')")
      assert.doesNotMatch(await evaluate(sessionId,"document.querySelector('.pa-roster-filter').innerText"),/could not be read|無法讀取來源/,"a failed peer does not turn healthy empty search results into a source failure")
      assert.equal(await evaluate(sessionId,"!!document.querySelector('.pa-status-bar,.pa-connection-notice')"),false)
      await capture("empty-with-failed-peer")
      await evaluate(sessionId,"window.rosterSearch.fail('pa_offline')")
      await search("another-empty-query")
      await until(sessionId,"!!document.querySelector('.pa-roster-unavailable [role=status]')")
      await capture("all-failed")
      await evaluate(sessionId,"window.rosterSearch.fail(null)")
      await search("")
      await until(sessionId,"document.querySelectorAll('.pa-roster [data-bot-key]').length===3")
      await until(sessionId,"!!document.querySelector('.pa-roster-read:not(:disabled)')")
      await evaluate(sessionId,"document.querySelector('.pa-roster-read:not(:disabled)').click()")
      await until(sessionId,"!document.querySelector('.pa-roster-read')&&document.querySelectorAll('.pa-roster [data-bot-key]').length===3")
      assert.deepEqual(await names(),before,"marking read restores the original position")
      await chooseOrder(defaultOrder)
      results.push({lang,width,unreadFirst:true,machineGrouping:true,projectRows:6,nameRows:1,partialFailurePreserved:true,selectedFailedMachineVisible:true,allFailedVisible:true})
    }catch(error){await capture("FAIL");throw error}
    finally{await send("Target.closeTarget",{targetId})}
  }
  assert.equal(forbidden,0)
  console.log(JSON.stringify({results,forbidden,modelRequests:0}))
}finally{
  await writeFile(path.join(artifacts,"results.json"),JSON.stringify({results,forbidden},null,2)+"\n")
  ws?.close();if(chrome){chrome.kill("SIGTERM");await new Promise(resolve=>{if(chrome.exitCode!==null)return resolve();const timer=setTimeout(()=>{chrome.kill("SIGKILL");resolve()},3000);chrome.once("exit",()=>{clearTimeout(timer);resolve()})})}
  server.closeAllConnections();await new Promise(resolve=>server.close(resolve));await rm(profile,{recursive:true,force:true})
}
