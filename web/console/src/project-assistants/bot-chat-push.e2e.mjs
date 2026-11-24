/** Measures how soon an open Bot chat shows a committed reply, and its idle requests. */
import assert from "node:assert/strict"
import {createServer} from "node:http"
import {mkdir,readFile,writeFile} from "node:fs/promises"
import {fileURLToPath,pathToFileURL} from "node:url"
import path from "node:path"
import {build} from "esbuild"

const {BOTS_ORIGIN:upstream,BOTS_TOKEN:token,BOTS_MACHINE:machine,BOTS_ARTIFACTS:artifacts,BOTS_BASELINE_DIR:baseline}=process.env
if(!upstream||!token||!machine||!artifacts||!process.env.PLAYWRIGHT_MODULE)throw new Error("Requires the owned live HTTP fixture and installed Playwright")
const samples=Number(process.env.BOTS_SAMPLES??10),idleMs=Number(process.env.BOTS_IDLE_MS??60000),variant=baseline?"baseline":"pushed"
await mkdir(artifacts,{recursive:true})
const {chromium}=await import(pathToFileURL(process.env.PLAYWRIGHT_MODULE).href)
// A baseline run builds the same fixture with the previous Chat.tsx and Bots.tsx.
const swap={name:"baseline",setup(b){if(!baseline)return;b.onLoad({filter:/project-assistants[\\/](Chat|Bots)\.tsx$/},async args=>({contents:await readFile(path.join(baseline,path.basename(args.path)),"utf8"),loader:"tsx",resolveDir:path.dirname(args.path)}))}}
const built=await build({entryPoints:[fileURLToPath(new URL("./roster-live.fixture.tsx",import.meta.url))],bundle:true,write:false,outfile:"roster.js",format:"esm",platform:"browser",jsx:"automatic",plugins:[swap],define:{"process.env.NODE_ENV":'"production"'}})
const files=new Map(built.outputFiles.map(f=>["/"+path.basename(f.path),f.contents]))
let requests=[]
const server=createServer(async(req,res)=>{
  const url=new URL(req.url,"http://fixture")
  if(url.pathname.startsWith("/v1/")){
    requests.push({at:Date.now(),route:req.method+" "+url.pathname.replace(/\/bots\/[^/]+\/[^/]+/,"/bots/:key")})
    const controller=new AbortController();res.on("close",()=>controller.abort())
    const headers={Authorization:`Bearer ${token}`}
    for(const key of ["accept","content-type","idempotency-key","x-fixture-reader","last-event-id"])if(typeof req.headers[key]==="string")headers[key]=req.headers[key]
    const chunks=[];if(req.method!=="GET")for await(const c of req)chunks.push(c)
    try{
      const answer=await fetch(upstream+req.url,{method:req.method,headers,signal:controller.signal,...(chunks.length?{body:Buffer.concat(chunks)}:{})})
      res.writeHead(answer.status,{"Content-Type":answer.headers.get("Content-Type")??"application/json"})
      if(!answer.body){res.end();return}
      for await(const chunk of answer.body){if(res.destroyed)break;res.write(chunk)}
      res.end()
    }catch(error){if(!res.destroyed){if(!res.headersSent)res.writeHead(502);res.end(String(error))}}
    return
  }
  if(url.pathname==="/fixture.json"){res.setHeader("Content-Type","application/json");res.end(JSON.stringify({machine}));return}
  if(files.has(url.pathname)){res.setHeader("Content-Type",url.pathname.endsWith(".css")?"text/css":"text/javascript");res.end(files.get(url.pathname));return}
  res.setHeader("Content-Type","text/html");res.end('<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="stylesheet" href="/roster.css"><div id="app"></div><script type="module" src="/roster.js"></script>')
})
await new Promise(resolve=>server.listen(0,"127.0.0.1",resolve))
const origin=`http://127.0.0.1:${server.address().port}`,browser=await chromium.launch({headless:true})
const phase=async name=>{const r=await fetch(upstream+"/fixture/phase?"+new URLSearchParams({phase:name,bot:"reading"}),{method:"POST"});assert.equal(r.status,204,await r.text())}
const quantile=(xs,q)=>{const s=[...xs].sort((a,b)=>a-b);return s[Math.min(s.length-1,Math.ceil(q*s.length)-1)]}
const report={variant,samples,idleMs,runs:[]}
try{
  for(const [label,width,height,throttle] of [["1280x800",1280,800,1],["390x844",390,844,1],["1280x800-cpu4x",1280,800,4],["390x844-cpu4x",390,844,4]]){
    await phase("reset")
    const page=await browser.newPage({viewport:{width,height}}),errors=[]
    page.on("pageerror",e=>errors.push(String(e)))
    if(throttle>1){const cdp=await page.context().newCDPSession(page);await cdp.send("Emulation.setCPUThrottlingRate",{rate:throttle})}
    await page.goto(origin+"/?lang=en")
    const row=page.locator(`[data-bot-key='${JSON.stringify([machine,"reading"])}']`)
    await row.getByText("Read notes.md",{exact:true}).waitFor({timeout:15000})
    await row.click()
    const chat=page.locator(".pa-main")
    await chat.getByText("Read the notes",{exact:true}).first().waitFor({timeout:15000})
    const replies=()=>page.evaluate(()=>[...document.querySelectorAll(".pa-main *")].filter(e=>e.childElementCount===0&&e.textContent==="Fresh reply").length)
    const latencies=[]
    for(let i=0;i<samples;i++){
      const before=await replies()
      // Vary the offset within the 3 s cadence so the baseline is not phase locked.
      await page.waitForTimeout(400+Math.round(Math.random()*2600))
      const started=Date.now()
      await phase("done")
      try{await page.waitForFunction(n=>[...document.querySelectorAll(".pa-main *")].filter(e=>e.childElementCount===0&&e.textContent==="Fresh reply").length>n,before,{timeout:20000,polling:16})}
      catch(error){await page.screenshot({path:path.join(artifacts,`timeout-${label}.png`)});console.log(await chat.innerText());throw error}
      latencies.push(Date.now()-started)
      assert.equal(await replies(),before+1,"one reply appears exactly once")
      await phase("start")
      await page.waitForTimeout(200)
    }
    let idle
    if(idleMs>0&&throttle===1){
      await page.waitForTimeout(3500)
      requests=[];const from=Date.now()
      await page.waitForTimeout(idleMs)
      const perRoute={};for(const r of requests.filter(r=>r.at>=from))perRoute[r.route]=(perRoute[r.route]??0)+1
      const minutes=idleMs/60000;for(const k of Object.keys(perRoute))perRoute[k]=+(perRoute[k]/minutes).toFixed(2)
      idle=perRoute
    }
    assert.deepEqual(errors,[])
    report.runs.push({label,throttle,latencies,median:quantile(latencies,0.5),p90:quantile(latencies,0.9),worst:Math.max(...latencies),idlePerMinute:idle})
    console.log(JSON.stringify(report.runs.at(-1)))
    await page.close()
  }
}finally{
  await writeFile(path.join(artifacts,`push-${variant}.json`),JSON.stringify(report,null,2))
  await browser.close();server.close()
}
