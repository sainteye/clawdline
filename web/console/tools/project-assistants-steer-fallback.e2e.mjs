/** A steering Bot offers Send now during a reply, including for older queued messages. */
import assert from "node:assert/strict"
import {createServer} from "node:http"
import {mkdir,mkdtemp,writeFile,access,rm} from "node:fs/promises"
import {spawn} from "node:child_process"
import {constants} from "node:fs"
import {fileURLToPath} from "node:url"
import path from "node:path"
import {build} from "esbuild"

const {BOTS_ARTIFACTS:artifacts,CHROME:executable}=process.env
assert.ok(artifacts,"BOTS_ARTIFACTS names a task-owned evidence directory")
assert.ok(executable,"CHROME must name an existing test browser; this harness installs nothing");await access(executable,constants.X_OK)
await mkdir(artifacts,{recursive:true})
const root=fileURLToPath(new URL("..",import.meta.url))
const bundle=await build({entryPoints:[path.join(root,"tools/fixtures/project-assistants-steer-fallback.tsx")],bundle:true,write:false,outfile:"steer.js",format:"esm",target:"es2022",platform:"browser",jsx:"automatic",define:{"process.env.NODE_ENV":'"development"'}})
const files=new Map(bundle.outputFiles.map(f=>["/"+path.basename(f.path),f.contents]))
const server=createServer((req,res)=>{
  const url=new URL(req.url,"http://fixture")
  if(files.has(url.pathname)){res.setHeader("Content-Type",url.pathname.endsWith(".css")?"text/css":"text/javascript");res.end(files.get(url.pathname));return}
  res.setHeader("Content-Type","text/html")
  res.end('<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1,viewport-fit=cover"><link rel="stylesheet" href="/steer.css"><style>html,body,#app{margin:0;background:#080808;color:#eee}</style><div id="app"></div><script type="module" src="/steer.js"></script>')
})
await new Promise(resolve=>server.listen(0,"127.0.0.1",resolve));const origin=`http://127.0.0.1:${server.address().port}`
const profile=await mkdtemp(path.join(artifacts,"browser-profile-"))
const chrome=spawn(executable,["--disable-features=OmniboxEverywhere","--headless=new","--remote-debugging-port=0","--use-mock-keychain","--user-data-dir="+profile,"--no-first-run","--no-default-browser-check","about:blank"],{stdio:["ignore","ignore","pipe"]})
const endpoint=await new Promise((resolve,reject)=>{let output="";const timer=setTimeout(()=>reject(Error("Test browser startup timeout: "+output)),20000);chrome.on("exit",()=>reject(Error("Test browser exited: "+output)));chrome.stderr.on("data",chunk=>{output+=chunk;const m=/DevTools listening on (ws:\/\/\S+)/.exec(output);if(m){clearTimeout(timer);resolve(m[1])}})})
const ws=new WebSocket(endpoint);await new Promise((resolve,reject)=>{ws.addEventListener("open",resolve);ws.addEventListener("error",reject)})
let serial=0;const pending=new Map()
ws.addEventListener("message",event=>{const m=JSON.parse(String(event.data));if(!m.id)return;const job=pending.get(m.id);pending.delete(m.id);if(m.error)job?.reject(Error(m.error.message));else job?.resolve(m.result)})
const send=(method,params={},sessionId)=>new Promise((resolve,reject)=>{const id=++serial,timer=setTimeout(()=>reject(Error(method+" timed out")),15000);pending.set(id,{resolve:v=>{clearTimeout(timer);resolve(v)},reject:e=>{clearTimeout(timer);reject(e)}});ws.send(JSON.stringify({id,method,params,sessionId}))})
let session,tab
const evaluate=async expression=>{const r=await send("Runtime.evaluate",{expression,awaitPromise:true,returnByValue:true},session);if(r.exceptionDetails)throw Error(r.exceptionDetails.exception?.description??r.exceptionDetails.text);return r.result.value}
const waitFor=async expression=>{for(let n=0;n<160;n++){if(await evaluate("!!("+expression+")"))return;await new Promise(r=>setTimeout(r,50))}throw Error("Condition did not become true: "+expression+"\n"+await evaluate("document.body.innerText"))}
async function open(query,mobile){
  ({targetId:tab}=await send("Target.createTarget",{url:"about:blank"}));session=(await send("Target.attachToTarget",{targetId:tab,flatten:true})).sessionId
  await send("Page.enable",{},session)
  await send("Emulation.setDeviceMetricsOverride",{width:mobile?390:1280,height:mobile?720:900,deviceScaleFactor:mobile?2:1,mobile},session)
  await send("Page.navigate",{url:origin+"/?"+query},session)
}
const close=()=>send("Target.closeTarget",{targetId:tab})
const shot=async name=>{const {data}=await send("Page.captureScreenshot",{format:"png"},session);await writeFile(path.join(artifacts,name+".png"),Buffer.from(data,"base64"))}
const results=[]
try{
  for(const [mobile,language] of [[false,"en"],[true,"zh-Hant"]]){
    const tag=mobile?"390px":"desktop",words=language==="en"
      ?{switch:"Steer while replying",send:"Send now, into the current reply",permission:"Waiting for a permission answer, so it’s queued",images:"This agent can’t take pictures mid-reply, so it’s queued"}
      :{switch:"回覆中允許插話",send:"立即送出，加進目前的回覆",permission:"等待權限確認，已排隊",images:"這個 agent 不支援帶圖插話，已排隊"}
    await open(`view=settings&lang=${language}`,mobile)
    const sw=`document.querySelector('button[role=switch][aria-label="${words.switch}"]')`
    await waitFor(sw)
    assert.equal(await evaluate(sw+".getAttribute('aria-checked')"),"true","a Bot without a saved choice shows steering on")
    await evaluate(sw+".scrollIntoView({block:'center'})");await shot(tag+"-settings-steer-default")
    await close()
    await open(`lang=${language}`,mobile)
    await waitFor("document.querySelectorAll('.pa-queued').length===3")
    const rows=await evaluate(`[...document.querySelectorAll('.pa-queued')].map(row=>({text:row.querySelector('.pa-queued-preview')?.textContent??'',reason:row.querySelector('.pa-queued-reason')?.textContent??'',sendNow:!!row.querySelector('button[aria-label="${words.send}"]')}))`)
    const legacy=rows.find(r=>!r.reason),permission=rows.find(r=>r.reason===words.permission),images=rows.find(r=>r.reason===words.images)
    assert.ok(legacy?.sendNow,"an older queued message can Send now with no fallback reason: "+JSON.stringify(rows))
    assert.ok(permission?.sendNow&&images?.sendNow,"messages put back in the queue offer send now with their reason: "+JSON.stringify(rows))
    const overflow=await evaluate("document.documentElement.scrollWidth>innerWidth")
    assert.equal(overflow,false,"no horizontal page scroll")
    await evaluate("(()=>{const s=document.querySelector('.pa-chat-scroll');s.scrollTop=s.scrollHeight;s.dispatchEvent(new Event('scroll'))})()");await new Promise(r=>setTimeout(r,400))
    await shot(tag+"-queued-fallback")
    await close()
    results.push({tag,language,rows})
    console.log(`PASS ${tag} ${language}: switch on by default; Send now on all ${rows.filter(r=>r.sendNow).length} queued messages, including one without a fallback reason`)
  }
  await writeFile(path.join(artifacts,"steer-fallback-results.json"),JSON.stringify(results,null,2))
}catch(error){console.error("FAIL",error);process.exitCode=1}
finally{ws.close();const exited=new Promise(resolve=>chrome.once("exit",resolve));chrome.kill();await exited;server.close();await rm(profile,{recursive:true,force:true,maxRetries:5})}
