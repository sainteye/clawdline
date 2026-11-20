/** Production Chat + manager + SourceClient against the owned Go TCP fixture. */
import assert from "node:assert/strict"
import {createServer} from "node:http"
import {mkdir,readFile,writeFile} from "node:fs/promises"
import {execFile} from "node:child_process"
import {promisify} from "node:util"
import {fileURLToPath} from "node:url"
import path from "node:path"
import {build} from "esbuild"

const {BOTS_ORIGIN:upstream,BOTS_TOKEN:token,BOTS_MACHINE:machine,BOTS_BOT:bot,BOTS_ARTIFACTS:artifacts}=process.env
if(!upstream||!token||!machine||!bot||!artifacts)throw Error("Requires the owned Go HTTP fixture")
await mkdir(artifacts,{recursive:true})
const browserDir=process.env.BOTS_BROWSER_CLAWDLINE_DIR
if(!browserDir)throw Error("Requires the configured browser broker directory, independently of the isolated test store")
const consoleRoot=fileURLToPath(new URL("..",import.meta.url)),taskDir=path.dirname(artifacts),exec=promisify(execFile)
const bundle=await build({stdin:{contents:`
import React from "react";import {createRoot} from "react-dom/client";
import {Chat} from "./src/project-assistants/Chat";
import {ProjectAssistantSourceClient} from "./src/cloud/project-assistants";
import {createViewState} from "./src/project-assistants/view-state";
import {en,zhHant} from "./src/project-assistants/words";
import {activateCatalog} from "./src/catalog";
import taiwan from "./public/catalogs/zh-Hant.json";
import "./src/project-assistants/styles.css";
const language=new URL(location.href).searchParams.get("lang")??"en";
if(language==="zh-Hant")activateCatalog(taiwan,"zh-Hant");
const grant={accountKey:"synthetic-account",readerID:"synthetic-reader",machineID:${JSON.stringify(machine)},authorizationRevision:"synthetic-grant",apiLevel:95,paired:true,online:true,writable:true};
const source=new ProjectAssistantSourceClient(grant,(input,init)=>fetch(input,init),()=>grant);
await source.status(new AbortController().signal);
const list=await source.list({machine_id:grant.machineID,query:"",cursor:"",page_size:100},new AbortController().signal);
const summary=list.items.find(x=>x.bot.key.bot_id===${JSON.stringify(bot)});
const view=createViewState(sessionStorage);
if(new URL(location.href).searchParams.get("journal")==="broken") {
  const key="clawdline:routine-command:"+JSON.stringify([grant.accountKey,grant.readerID,summary.bot.key.machine_id,summary.bot.key.bot_id]);
  sessionStorage.setItem(key,'{"action":"test","request":');
}
createRoot(document.getElementById("app")).render(<main className="project-assistants" style={{height:"100vh"}}><Chat summary={summary} source={source} current={()=>[source]} accountKey={grant.accountKey} readerID={grant.readerID} view={view} w={language==="en"?en:zhHant} language={language} onBack={()=>{}} onBotChanged={()=>{}}/></main>);
`,resolveDir:consoleRoot,loader:"tsx"},bundle:true,write:false,outfile:"routine.js",format:"esm",target:"es2022",platform:"browser",jsx:"automatic",define:{"process.env.NODE_ENV":'"development"'}})
const files=new Map(bundle.outputFiles.map(f=>["/"+path.basename(f.path),f.contents])),requests=[]
let lostSave=false,pageLoads=0
const server=createServer(async(req,res)=>{
  try {
    if(req.url.startsWith("/v1/")) {
      const chunks=[];for await(const chunk of req)chunks.push(chunk)
      const headers={Authorization:`Bearer ${token}`};for(const k of ["content-type","idempotency-key"])if(req.headers[k])headers[k]=req.headers[k]
      const answer=await fetch(upstream+req.url,{method:req.method,headers,...(chunks.length?{body:Buffer.concat(chunks)}:{})})
      const body=Buffer.from(await answer.arrayBuffer());requests.push({method:req.method,path:req.url,status:answer.status,...(chunks.length?{body:JSON.parse(Buffer.concat(chunks))}:{})})
      if(!lostSave&&req.method==="POST"&&req.url.endsWith("/routines")&&answer.status===200) {
        lostSave=true;requests[requests.length-1].response_status=502
        res.writeHead(502,{"Content-Type":"application/json"});res.end('{"synthetic":"response lost after actual commit"}');return
      }
      res.writeHead(answer.status,{"Content-Type":answer.headers.get("Content-Type")??"application/json"});res.end(body);return
    }
    const url=new URL(req.url,"http://fixture")
    if(files.has(url.pathname)){res.setHeader("Content-Type",url.pathname.endsWith(".css")?"text/css":"text/javascript");res.end(files.get(url.pathname));return}
    pageLoads++;res.setHeader("Content-Type","text/html");res.end('<!doctype html><html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Routine management TCP fixture</title><script>addEventListener("error",e=>{document.body.append("Fixture error: "+e.message)});addEventListener("unhandledrejection",e=>{document.body.append("Fixture rejection: "+JSON.stringify(e.reason))})</script><style>body{margin:0;background:#101010;color:#eee;font:15px system-ui}#app{height:100vh;max-width:1000px;margin:auto}button{cursor:pointer}.pa-chat{height:100%}</style><link rel="stylesheet" href="/routine.css"><div id="app"></div><script type="module" src="/routine.js"></script></html>')
  }catch(error){res.writeHead(500);res.end(String(error))}
})
await new Promise(resolve=>server.listen(0,"127.0.0.1",resolve))
const origin=`http://127.0.0.1:${server.address().port}`,operations=[]
let tab
async function browser(op,...args) {
  const {stdout}=await exec("clawdline",["browser",op,...(tab&&op!=="open"&&op!=="audit"&&op!=="status"?["--tab",String(tab)]:[]),...args],{cwd:process.env.BOTS_BROWSER_CWD??taskDir,env:{...process.env,CLAWDLINE_NEXT_DIR:browserDir},timeout:20000,maxBuffer:2*1024*1024})
  const result=JSON.parse(stdout);operations.push({op,args,result});if(!result.ok)throw Error(stdout);return result.value??result
}
const controls=async language=>{const catalog=JSON.parse(await readFile(path.join(consoleRoot,"public/catalogs",language+".json"),"utf8"));return name=>catalog["botRoutineControls."+name]}
const pause=ms=>new Promise(resolve=>setTimeout(resolve,ms))
async function waitText(text) { let last;for(let i=0;i<40;i++){last=await browser("read");if(last.text?.includes(text))return last;await pause(150)}throw Error(`Missing ${text}: ${JSON.stringify(last)}`) }
try {
  const status=await browser("status");assert.equal(status.mode,"user_chrome")
  // A damaged durable record is still an uncertain action, never permission
  // to replace it with another test/save. Exercise the production component.
  {
    const {en:copy}=await import("../src/project-assistants/words.ts")
    const opened=await browser("open",origin+"/?lang=en&journal=broken");tab=opened.tab
    await waitText("Synthetic Bot");await browser("click","Synthetic Bot · "+copy.botInformation)
    await waitText(copy.routineJournalUnreadable??copy.unknown)
    const before=requests.filter(r=>r.method!=="GET").length
    try{await browser("click",(await controls("en"))("setUp"))}catch(error){operations.push({op:"blocked-new-routine",detail:String(error)})}
    const read=await browser("read")
    assert.ok(!read.text?.includes(copy.routineInstruction),"Unreadable original command must prevent a replacement form")
    assert.equal(requests.filter(r=>r.method!=="GET").length,before,"Unreadable journal must never send a new operation")
    await browser("screenshot","--out",path.join(artifacts,"unreadable-command-held.jpg"))
    await browser("click",copy.closeInformation);await browser("click","Synthetic Bot · "+copy.botInformation)
    await waitText(copy.routineJournalUnreadable??copy.unknown)
    assert.equal(requests.filter(r=>r.method!=="GET").length,before,"Panel remount must preserve the held state")
    await browser("close");tab=undefined
  }
  for(const language of ["en","zh-Hant"]) {
    const zh=language!=="en",{en,zhHant}=await import("../src/project-assistants/words.ts"),copy=zh?zhHant:en
    const control=await controls(language)
    const w={identity:"Synthetic Bot · "+copy.botInformation,create:control("setUp"),name:copy.name,instructions:copy.routineInstruction,save:copy.save,edit:copy.routineEdit,test:copy.routineTest,pause:copy.routinePause,enable:copy.routineEnable,paused:copy.routinePaused,remove:copy.routineDelete,confirm:copy.routineConfirmDelete,empty:control("emptyRoutines")}

    const opened=await browser("open",origin+"/?lang="+language);tab=opened.tab
    await browser("viewport",zh?"mobile":"desktop")
    await waitText("Synthetic Bot");await browser("click",w.identity);await waitText(w.empty)
    await browser("click",w.create);await browser("click",control("webhook"));await waitText(control("webhookSave"));await browser("click",control("schedule"));await browser("click",w.name);await browser("type","Browser interval "+language);await browser("click",w.instructions);await browser("type","Synthetic browser routine task "+language);await browser("screenshot","--out",path.join(artifacts,`${language}-routine-editor.jpg`));await browser("click",control("createRoutine"))
    if(!zh) {
      await waitText(copy.routineHeld)
      assert.equal(requests.filter(r=>r.method==="POST"&&r.path.endsWith("/routines")).length,1,"Unknown outcome never automatically sends a replacement")
      const beforeReload=pageLoads;await browser("viewport","mobile");await waitText("Synthetic Bot");await browser("viewport","desktop");await waitText("Synthetic Bot");assert.ok(pageLoads>=beforeReload+2,"Actual origin reloads were served");await browser("click",w.identity);await waitText(copy.routineHeld)
      assert.equal(requests.filter(r=>r.method==="POST"&&r.path.endsWith("/routines")).length,1,"Reload preserves the command without sending it")
      await browser("screenshot","--out",path.join(artifacts,"en-routine-held-after-reload.jpg"));await browser("click",copy.routineReplay)
    }
    await waitText("Browser interval "+language)
    await browser("click",w.edit);await waitText(w.instructions)
    // Appending verifies an actual edit while keeping the trigger untouched.
    await browser("click",w.name);await browser("type"," edited");await browser("click",w.save)
    await waitText("edited")
    await browser("click",w.test)
    for(let i=0;i<8;i++){await pause(250);await browser("click",copy.routineHistory);const read=await browser("read");if(read.text?.includes("Actual isolated routine result"))break}
    await waitText("Actual isolated routine result")
    await browser("scroll","down")
    await browser("screenshot","--out",path.join(artifacts,`${language}-routine-result.jpg`))
    await browser("click",w.pause);await waitText(w.paused);await browser("click",w.enable)
    await browser("click",w.remove);await waitText(w.confirm);await browser("click",w.confirm);await waitText(w.empty)
    await browser("screenshot","--out",path.join(artifacts,`${language}-routine-deleted-history.jpg`))
    await browser("close");tab=undefined
  }
  const writes=requests.filter(r=>r.method!=="GET")
  assert.equal(writes.filter(r=>r.path.includes("/messages")).length,0,"Manager cannot send main chat text")
  assert.equal(writes.filter(r=>r.path.endsWith("/runs")).length,2,"Only explicit test buttons request runs")
  assert.equal(writes.filter(r=>r.path.endsWith("/routines")).length,5,"Create/edit per language plus one exact lost-response replay")
  assert.ok(writes.every(r=>r.status===200),JSON.stringify(writes))
  const saved=writes.filter(r=>r.path.endsWith("/routines"));assert.deepEqual(saved[0].body,saved[1].body,"Explicit lost-response replay preserves exact command/payload")
  await writeFile(path.join(artifacts,"browser-requests.json"),JSON.stringify(requests,null,2)+"\n")
  await writeFile(path.join(artifacts,"browser-operations.json"),JSON.stringify(operations,null,2)+"\n")
  await writeFile(path.join(artifacts,"browser-audit.json"),JSON.stringify(await browser("audit","--limit","100"),null,2)+"\n")
  console.log("Production Chat/SourceClient -> real Go TCP/Host/Store; desktop English and mobile Taiwan Chinese; synthetic create/edit/test/results/pause/enable/delete; no chat sends; external/native/model requests=0")
} finally {
  if(tab)await browser("close").catch(error=>console.error("Owned tab cleanup:",error.message))
  await writeFile(path.join(artifacts,"browser-operations.json"),JSON.stringify(operations,null,2)+"\n")
  await writeFile(path.join(artifacts,"browser-requests.json"),JSON.stringify(requests,null,2)+"\n")
  await new Promise(resolve=>server.close(resolve))
}
