/**
 * Leaving a Bot chat while its send is in flight. Actual Chat, ViewState and
 * Cloud SourceClient in a real Chrome tab driven by `clawdline browser`,
 * against the real Go HTTP/Host/SQLite source with a scripted provider
 * (project_assistants_send_lifetime_test.go). No model is reachable.
 */
import assert from "node:assert/strict"
import {createServer} from "node:http"
import {mkdir,writeFile} from "node:fs/promises"
import {execFile} from "node:child_process"
import {promisify} from "node:util"
import {fileURLToPath} from "node:url"
import path from "node:path"
import {build} from "esbuild"
const {BOTS_ORIGIN:upstream,BOTS_ARTIFACTS:artifacts,BOTS_MACHINE:machine,BOTS_TOKEN:token,BOTS_BROWSER_CLAWDLINE_DIR:browserDir}=process.env
if(!upstream||!artifacts||!machine||!token||!browserDir)throw Error("Requires the owned Go fixture and browser broker")
await mkdir(artifacts,{recursive:true})
const built=await build({entryPoints:[fileURLToPath(new URL("./send-lifetime.fixture.tsx",import.meta.url))],bundle:true,write:false,outfile:"lifetime.js",format:"esm",platform:"browser",jsx:"automatic",define:{"process.env.NODE_ENV":'"development"'}})
const files=new Map(built.outputFiles.map(f=>["/"+path.basename(f.path),f.contents])),requests=[],operations=[],observations=[]
let tab
const controls=new Set()
// Same-origin proxy: a page that drops its request drops the upstream one too.
const server=createServer(async(req,res)=>{
  const url=new URL(req.url,"http://fixture")
  if(url.pathname.startsWith("/v1/")) {
    const control=new AbortController();controls.add(control);res.on("close",()=>{if(!res.writableEnded)control.abort()})
    try {
      const chunks=[];for await(const c of req)chunks.push(c)
      const response=await fetch(upstream+req.url,{method:req.method,signal:control.signal,headers:{Authorization:`Bearer ${token}`,"Content-Type":"application/json",...(req.headers["idempotency-key"]?{"Idempotency-Key":req.headers["idempotency-key"]}:{})},...(chunks.length?{body:Buffer.concat(chunks)}:{})})
      const body=await response.text();requests.push({method:req.method,path:url.pathname,mode:url.searchParams.get("mode")??"",status:response.status})
      res.writeHead(response.status,{"Content-Type":"application/json"});res.end(body)
    }catch(error){requests.push({method:req.method,path:url.pathname,mode:url.searchParams.get("mode")??"",error:String(error?.name??error)});if(!res.destroyed){res.writeHead(502);res.end("")}}finally{controls.delete(control)}
    return
  }
  if(url.pathname==="/fixture.json"){res.end(JSON.stringify({machine}));return}
  if(files.has(url.pathname)){res.setHeader("Content-Type",url.pathname.endsWith(".css")?"text/css":"text/javascript");res.end(files.get(url.pathname));return}
  res.setHeader("Content-Type","text/html");res.end('<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><style>body{margin:0;background:#101010;color:#eee;font:15px system-ui}#app{height:85vh}output{display:block}</style><link rel="stylesheet" href="/lifetime.css"><div id="app"></div><script type="module" src="/lifetime.js"></script>')
})
await new Promise(r=>server.listen(0,"127.0.0.1",r))
const origin=`http://127.0.0.1:${server.address().port}`,exec=promisify(execFile),pause=ms=>new Promise(r=>setTimeout(r,ms))
async function browser(op,...args){const {stdout}=await exec("clawdline",["browser",op,...(tab&&op!=="open"?["--tab",String(tab)]:[]),...args],{env:{...process.env,CLAWDLINE_NEXT_DIR:browserDir},timeout:20000,maxBuffer:4e6});const result=JSON.parse(stdout);operations.push({op,args,ok:result.ok});if(!result.ok)throw Error(stdout);return result.value??result}
const observe=async()=>{const page=await browser("read");const m=/obs [^\n]*? end/.exec(page.text??"");return {line:m?.[0]??"",text:page.text??""}}
async function until(name,test,tries=60){let last;for(let i=0;i<tries;i++){last=await observe();if(test(last.line,last.text)){observations.push({name,line:last.line});return last}await pause(200)}observations.push({name,line:last?.line,failed:true});throw Error(`${name}: ${last?.line}`)}
const state=()=>fetch(upstream+"/fixture/state").then(r=>r.json())
async function sourceUntil(name,test){let last;for(let i=0;i<100;i++){last=await state();if(test(last))return last;await pause(100)}throw Error(`${name}: ${JSON.stringify(last)}`)}
const fixture=async(op,what="")=>{const r=await fetch(upstream+`/fixture/${op}?what=${what}`,{method:"POST"});assert.equal(r.status,204)}
const shot=name=>browser("screenshot","--out",path.join(artifacts,name+".jpg"))
const openChat=async()=>{await until("roster",(l,t)=>t.includes("Synthetic Bot"));await browser("click","Synthetic Bot");return until("chat open",l=>l.includes("chat=1")&&!l.includes("composer=none"))}
const type=async text=>{await browser("click","Message");await browser("type",text)}
const summary={}
try {
  tab=(await browser("open",origin+"/")).tab
  await browser("viewport","desktop")

  // A: the production reproducer. Leave while the admitted send's answer is held.
  await openChat();await until("composer ready",l=>l.includes("composer=enabled"))
  await fixture("arm","send");await type("First message");await browser("click","Send message")
  await sourceUntil("first admitted",s=>s.held==="send"&&s.posts===1)
  await browser("click","Fixture leave chat");await until("left chat",l=>l.includes("chat=0"))
  const reopened=await openChat();summary.reopenedBeforeAnswer=reopened.line;await shot("a-reopened-before-answer")
  await fixture("release")
  try{await until("receipt settles remounted chat",l=>l.includes("composer=enabled")&&l.includes("held=0")&&l.includes("sending=0")&&/replies=[1-9]/.test(l),50)}
  finally{await shot("a-after-answer");summary.afterAnswer=observations.at(-1)?.line;summary.sourceAfterA=await state()}
  assert.equal(summary.sourceAfterA.client_gone,0,"leaving the chat must not drop the admitted send's request")
  await type("Second message");await browser("click","Send message")
  await sourceUntil("second admitted and answered",s=>s.posts===2&&s.provider_sends===2&&s.assistant===2)
  await until("second reply shown",l=>/replies=2/.test(l)&&l.includes("composer=enabled"))

  // B: leaving before admission (during the send's preflight read) cancels it; the draft returns.
  await fixture("arm","snapshot");await type("Draft before admission");await browser("click","Send message")
  await sourceUntil("preflight held",s=>s.held==="snapshot")
  await browser("click","Fixture leave chat");await sourceUntil("preflight dropped",s=>s.client_gone===1)
  await fixture("release")
  await openChat();const b=await until("draft back, nothing sent",l=>l.includes("composer=enabled")&&l.includes("draft=[Draft before admission]")&&l.includes("held=0"))
  summary.beforeAdmission=b.line;assert.equal((await state()).posts,2,"no send left before admission")

  // C: a full reload loses the request: the send stays genuinely unknown until checked; never resent.
  await fixture("arm","send");await browser("click","Send message")
  await sourceUntil("third admitted",s=>s.held==="send"&&s.posts===3)
  await browser("click","Fixture reload");await sourceUntil("reload dropped request",s=>s.client_gone===2);await fixture("release")
  await openChat();const c=await until("unknown after reload",l=>l.includes("composer=disabled")&&l.includes("held=1"),40);summary.afterReload=c.line;await shot("c-unknown-after-reload")
  await browser("click","Check with the machine");await until("explicit check delivered",(l,t)=>t.includes("The machine has this message. It was not sent again.")&&l.includes("composer=enabled"))
  assert.equal((await state()).posts,3,"checking never resends")

  // D: the source's grant is revoked after leaving and before the answer: no reattachment.
  await fixture("arm","send");await type("Revoked follow-up");await browser("click","Send message")
  await sourceUntil("fourth admitted",s=>s.held==="send"&&s.posts===4)
  await browser("click","Fixture leave chat");await browser("click","Fixture revoke");await sourceUntil("revoke dropped request",s=>s.client_gone===3);await fixture("release")
  await browser("click","Fixture reload");await openChat();const d=await until("unknown after revoke",l=>l.includes("composer=disabled")&&l.includes("held=1"),40);summary.afterRevoke=d.line
  await browser("click","Check with the machine");await until("revoked send checked",(l,t)=>t.includes("The machine has this message. It was not sent again.")&&l.includes("composer=enabled"))
  summary.final=await sourceUntil("four turns answered",s=>s.assistant===4)
  assert.deepEqual([summary.final.posts,summary.final.provider_sends,summary.final.user,summary.final.assistant],[4,4,4,4])
  await shot("d-final")
}catch(error) {
  summary.error=String(error?.message??error)
  if(tab)await shot("failure").catch(()=>{})
  throw error
}finally {
  if(tab)await browser("close").catch(()=>{})
  await writeFile(path.join(artifacts,"summary.json"),JSON.stringify({summary,observations},null,2))
  await writeFile(path.join(artifacts,"http.json"),JSON.stringify(requests,null,2))
  await writeFile(path.join(artifacts,"operations.json"),JSON.stringify(operations,null,2))
  for(const control of controls)control.abort()
  server.closeAllConnections();await new Promise(r=>server.close(r))
}
