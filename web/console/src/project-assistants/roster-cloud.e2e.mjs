/** Nonlocal SourceClient → real Go HTTP/Service/SQLite; no encrypted relay claim. */
import assert from "node:assert/strict"
import {createServer} from "node:http"
import {mkdir,writeFile} from "node:fs/promises"
import {execFile} from "node:child_process"
import {promisify} from "node:util"
import {fileURLToPath} from "node:url"
import path from "node:path"
import {build} from "esbuild"
const {BOTS_ORIGIN:upstream,BOTS_ARTIFACTS:artifacts,BOTS_MACHINE:machine,BOTS_BROWSER_CLAWDLINE_DIR:browserDir}=process.env
if(!upstream||!artifacts||!machine||!browserDir)throw Error("Requires owned Go fixture and browser broker")
await mkdir(artifacts,{recursive:true})
const built=await build({entryPoints:[fileURLToPath(new URL("./roster-live.fixture.tsx",import.meta.url))],bundle:true,write:false,outfile:"roster.js",format:"esm",platform:"browser",jsx:"automatic",define:{"process.env.NODE_ENV":'"development"'}})
const files=new Map(built.outputFiles.map(f=>["/"+path.basename(f.path),f.contents])),requests=[],operations=[]
let active=0,maxActive=0,tab
const controls=new Set()
const server=createServer(async(req,res)=>{
  const url=new URL(req.url,"http://fixture")
  if(url.pathname==="/probe"){res.setHeader("Content-Type","text/html");res.end("<!doctype html><title>Owned browser probe</title><p>Owned browser probe</p>");return}
  if(url.pathname.startsWith("/v1/")) {
    const control=new AbortController();controls.add(control);res.on("close",()=>control.abort())
    const list=url.pathname==="/v1/project-assistants"
    if(list){active++;maxActive=Math.max(maxActive,active)}
    try {
      const chunks=[];for await(const c of req)chunks.push(c)
      const token=req.headers["x-fixture-reader"]==="read-only"?process.env.BOTS_READONLY_TOKEN:process.env.BOTS_TOKEN
      const response=await fetch(upstream+req.url,{method:req.method,signal:control.signal,headers:{Authorization:`Bearer ${token??"fixture-only"}`,"Content-Type":"application/json","X-Fixture-Reader":req.headers["x-fixture-reader"]??"reader"},...(chunks.length?{body:Buffer.concat(chunks)}:{})})
      const body=await response.text();requests.push({path:req.url,status:response.status,body})
      res.writeHead(response.status,{"Content-Type":"application/json"});res.end(body)
    }catch(error){if(!res.destroyed){res.writeHead(500);res.end(String(error))}}finally{controls.delete(control);if(list)active--}
    return
  }
  if(url.pathname==="/fixture.json"){res.end(JSON.stringify({machine}));return}
  if(files.has(url.pathname)){res.setHeader("Content-Type",url.pathname.endsWith(".css")?"text/css":"text/javascript");res.end(files.get(url.pathname));return}
  res.setHeader("Content-Type","text/html");res.end('<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><style>body{margin:0;background:#101010;color:#eee;font:15px system-ui}#app{height:90vh}output{display:block}</style><link rel="stylesheet" href="/roster.css"><div id="app"></div><script type="module" src="/roster.js"></script>')
})
await new Promise(r=>server.listen(0,"127.0.0.1",r))
const origin=`http://127.0.0.1:${server.address().port}`,exec=promisify(execFile),pause=ms=>new Promise(r=>setTimeout(r,ms))
async function browser(op,...args){const {stdout}=await exec("clawdline",["browser",op,...(tab&&op!=="open"?["--tab",String(tab)]:[]),...args],{cwd:process.cwd(),env:{...process.env,CLAWDLINE_NEXT_DIR:browserDir},timeout:20000,maxBuffer:2e6});const result=JSON.parse(stdout);operations.push({op,args,result});if(!result.ok)throw Error(stdout);return result.value??result}
async function waitText(text){let last;for(let i=0;i<30;i++){last=await browser("read");if(last.text?.includes(text))return last;await pause(200)}throw Error(`Missing ${text}: ${JSON.stringify(last)}`)}
async function phase(name){const r=await fetch(upstream+"/fixture/phase?"+new URLSearchParams({phase:name}),{method:"POST"});assert.equal(r.status,204,await r.text())}
try {
  // Real broker round trip before the product fixture.
  let opened=await browser("open",origin+"/probe");tab=opened.tab;await browser("read");await browser("close");tab=undefined
  for(const [lang,viewport] of [["en","desktop"],["zh-Hant","mobile"]]) {
    opened=await browser("open",origin+"/?"+new URLSearchParams({cloud:"1",lang,...(lang!=="en"?{readonly:"1",reader:"read-only"}:{})}));tab=opened.tab
    await browser("viewport",viewport)
    await waitText(lang==="en"?"reading=error":"reading=idle")
    if(process.env.BOTS_PAGINATION==="1") {
      await browser("click",lang==="en"?"Load more":"載入更多")
      await waitText(lang==="en"?"rows=106":"rows=105")
    }
    await phase("start")
    await browser("click","Reading helper")
    await waitText("chatWorking=true")
    await browser("click",lang==="en"?"Fixture history back":"返回 Bots")
    await waitText("reading=working")
    await browser("screenshot","--out",path.join(artifacts,`${lang}-returned-working.jpg`))
    if(process.env.BOTS_MINIMAL!=="1") {
      await phase("waiting");await waitText("reading=needsInput")
      await browser("screenshot","--out",path.join(artifacts,`${lang}-waiting.jpg`))
      await phase("done");await waitText("reading=idle")
      if(process.env.BOTS_PAGINATION==="1"&&lang==="en") {await phase("delete-extra");await waitText("rows=105")}
      await browser("screenshot","--out",path.join(artifacts,`${lang}-completed.jpg`))
      await browser("click","Fixture background")
      await pause(300)
      const before=requests.length
      await phase("start");await pause(3300)
      assert.equal(requests.length,before,"Hidden roster schedules no reads")
      await browser("click","Fixture foreground");await waitText("reading=working")
      await phase("done");await waitText("reading=idle")
      if(lang!=="en") {
        await browser("click","Fixture revoke");await waitText("rows=0")
        const count=requests.length;await pause(3300);assert.equal(requests.length,count,"Revoked source stops snapshot reads")
      }
    }
    await browser("close");tab=undefined
    if(process.env.BOTS_MINIMAL==="1")break
  }
  assert.equal(maxActive,1)
  assert.equal(requests.filter(r=>r.path.includes("roster-events")).length,0)
}catch(error) {
  if(tab)await browser("screenshot","--out",path.join(artifacts,"failure.jpg")).catch(()=>{})
  throw error
}finally {
  if(tab)await browser("close").catch(()=>{})
  await writeFile(path.join(artifacts,"http.json"),JSON.stringify({maxActive,requests},null,2))
  await writeFile(path.join(artifacts,"operations.json"),JSON.stringify(operations,null,2))
  for(const control of controls)control.abort()
  server.closeAllConnections();await new Promise(r=>server.close(r))
}
