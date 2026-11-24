/** The production chat model dialog saves while its Bot replies, against a real handler, store and synthetic runtime. */
// Run by TestModelRebaseBrowser with BOTS_* set; PLAYWRIGHT_MODULE names a Playwright index.mjs.
// MODEL_REBASE_DIALOG names the dialog module, so the same run can draw the code before the change.
import assert from "node:assert/strict"
import {createServer} from "node:http"
import {mkdir,readFile,writeFile} from "node:fs/promises"
import {fileURLToPath} from "node:url"
import path from "node:path"
import {build} from "esbuild"

const {BOTS_ORIGIN:upstream,BOTS_TOKEN:token,BOTS_MACHINE:machine,BOTS_BOT:bot,BOTS_ARTIFACTS:artifacts,PLAYWRIGHT_MODULE:playwright}=process.env
assert.ok(upstream&&token&&machine&&bot&&artifacts&&playwright)
await mkdir(artifacts,{recursive:true})
const root=fileURLToPath(new URL("..",import.meta.url)),requests=[]
const {chromium}=await import(playwright)
const label=process.env.MODEL_REBASE_LABEL||"after",dialog=process.env.MODEL_REBASE_DIALOG||"./src/project-assistants/ChatModel"

const bundle=await build({stdin:{contents:`
import React,{useState} from "react";import {createRoot} from "react-dom/client";
import * as M from ${JSON.stringify(dialog)};
import {settingsBase} from "./src/project-assistants/settings-conflicts";
import {ProjectAssistantSourceClient} from "./src/cloud/project-assistants";import {command} from "./src/project-assistants/client";
import {words} from "./src/project-assistants/words";import {bootCatalog} from "./src/catalog";import "./src/project-assistants/styles.css";
const q=new URL(location.href).searchParams,language=q.get("lang");localStorage.setItem("ui_language",language);await bootCatalog(tag=>"/catalog/"+tag+".json");
const live={accountKey:"synthetic-account",readerID:"synthetic-reader",machineID:${JSON.stringify(machine)},authorizationRevision:"synthetic-grant",apiLevel:116,paired:true,online:true,writable:true};
const source=new ProjectAssistantSourceClient(live,(input,init)=>fetch(input,init),()=>live);
source.canMemorySettings=()=>true;
const key={machine_id:live.machineID,bot_id:${JSON.stringify(bot)}},signal=()=>new AbortController().signal,w=words(language);
const got=await source.get(key,signal());
let setLive;
// The Bot answers a message: a real synthetic turn that advances the Bot version only; the chat sees the newer Bot.
window.fixtureReply=async()=>{const before=(await source.get(key,signal())).bot.version;const fresh=await source.get(key,signal());const c=fresh.conversation;
  await source.send({command:command(key,"send"),fence:{key,project:fresh.bot.project,conversation_id:c.id,binding_id:c.binding_id,generation:c.generation},text:"Synthetic message while choosing a model",quote_message_id:""},signal());
  for(let i=0;i<100;i++){const now=await source.get(key,signal());if(now.bot.version>before&&!now.conversation.active_turn_id){setLive(now.bot);return now.bot.version}await new Promise(r=>setTimeout(r,100))}
  throw Error("the synthetic turn did not finish")};
// Another editor saves a model.
window.fixtureModelElsewhere=async model=>{const now=(await source.get(key,signal())).bot;await source.edit({command:command(key,"edit"),settings:{name:now.name,appearance:now.appearance,instructions:now.instructions.text,expected_version:now.version,runtime_settings:{model,options:{}}}},signal());const after=(await source.get(key,signal())).bot;setLive(after);return model};
window.fixtureBot=async()=>(await source.get(key,signal())).bot;
// The chat's save: the production modelEdit when this build has it, the pre-change body otherwise.
const save=async(original,model,overwrite=false)=>{
  const settings=M.modelEdit?M.modelEdit(original,model,source.canSettingsBase(),overwrite):{expected_version:original.version,name:original.name,appearance:original.appearance,instructions:original.instructions.text,runtime_settings:{model,options:{}}};
  const saved=await source.edit({command:command(key,"edit"),settings},signal());setLive(saved.bot);window.fixtureSaves=(window.fixtureSaves??0)+1};
function Fixture(){const [current,set]=useState(got.bot);setLive=set;
return <main className="project-assistants" style={{maxWidth:720,margin:"0 auto"}}><M.ChatModel bot={current} source={source} enabled={true} activeTurn={false} save={save} w={w}/></main>}
createRoot(document.getElementById("app")).render(<Fixture/>);
`,resolveDir:root,loader:"tsx"},bundle:true,write:false,outfile:"model-rebase.js",format:"esm",target:"es2022",platform:"browser",jsx:"automatic",define:{"process.env.NODE_ENV":'"development"'},logLevel:"error"})
const files=new Map(bundle.outputFiles.map(f=>["/"+path.basename(f.path),f.contents]))
const server=createServer(async(req,res)=>{try{
  const url=new URL(req.url,"http://fixture")
  if(url.pathname.startsWith("/v1/")){
    const chunks=[];for await(const chunk of req)chunks.push(chunk)
    const headers={Authorization:"Bearer "+token};for(const key of ["content-type","idempotency-key"])if(req.headers[key])headers[key]=req.headers[key]
    const answer=await fetch(upstream+url.pathname+url.search,{method:req.method,headers,...(chunks.length?{body:Buffer.concat(chunks)}:{})}),body=Buffer.from(await answer.arrayBuffer())
    if(req.method==="PATCH")requests.push({status:answer.status,body:JSON.parse(Buffer.concat(chunks)),answer:JSON.parse(body)})
    res.writeHead(answer.status,{"Content-Type":"application/json"});res.end(body);return
  }
  if(url.pathname.startsWith("/catalog/")){res.setHeader("Content-Type","application/json");res.end(await readFile(path.join(root,"public/catalogs",path.basename(url.pathname))));return}
  if(files.has(url.pathname)){res.setHeader("Content-Type",url.pathname.endsWith(".css")?"text/css":"text/javascript");res.end(files.get(url.pathname));return}
  res.setHeader("Content-Type","text/html");res.end('<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="stylesheet" href="/model-rebase.css"><body style="background:#111;color:#eee"><div id="app"></div><script type="module" src="/model-rebase.js"></script></body>')
}catch(error){res.writeHead(500);res.end(String(error))}})
await new Promise(resolve=>server.listen(0,"127.0.0.1",resolve))
const origin=`http://127.0.0.1:${server.address().port}`
const browser=await chromium.launch({...(process.env.PLAYWRIGHT_CHANNEL?{channel:process.env.PLAYWRIGHT_CHANNEL}:{}),args:["--disable-features=OmniboxEverywhere"]})
const results=[]
try{
  for(const [name,viewport] of [["mobile",{width:390,height:844}],["desktop",{width:1280,height:800}]]){
    const page=await browser.newPage({viewport})
    page.on("pageerror",e=>console.error("pageerror",e.message))
    await page.goto(origin+"/?lang=zh-Hant")
    await page.locator(".pa-chat-model").click()
    const dlg=page.locator(".pa-chat-model-dialog")
    await dlg.locator(".pa-select-button").waitFor()
    // Pick a model the dialog does not hold now; the run reads back what was saved.
    const pick=async()=>{await dlg.locator(".pa-select-button").click();await dlg.locator("[role=option][aria-selected=false]").first().click()}
    const before=(await page.evaluate(()=>window.fixtureBot())).runtime_settings?.model??""
    await pick()
    // The person's report: the Bot replies while the dialog is open, then Save.
    await page.evaluate(()=>window.fixtureReply())
    const saveButton=dlg.locator(".pa-actions button").last()
    await page.waitForTimeout(200)
    const disabled=await saveButton.isDisabled()
    await page.screenshot({path:path.join(artifacts,`${label}-${name}-reply-then-save.png`)})
    results.push({label,viewport:name,scenario:"reply-then-save",saveDisabledAfterReply:disabled})
    if(label==="after"){
      assert.equal(disabled,false,"Bot activity disabled Save")
      await saveButton.click()
      await page.waitForFunction(()=>window.fixtureSaves>=1)
      const want=(await page.evaluate(()=>window.fixtureBot())).runtime_settings?.model??""
      assert.notEqual(want,before,"the model did not change")
      await page.screenshot({path:path.join(artifacts,`${label}-${name}-saved.png`)})
      // A real conflict: the model changed elsewhere to a different value.
      await dlg.getByRole("button",{name:w("close")}).click().catch(()=>{})
      await page.locator(".pa-chat-model").click()
      await pick()
      const values=["","synthetic-small","synthetic-large"]
      // The draft's value is not readable from the closed list; the other editor takes a value that differs from both.
      const shown=await dlg.locator(".pa-select-button").textContent()
      const mine=shown.includes("large")?"synthetic-large":values.find(v=>v!==want&&v!=="synthetic-large")
      const elsewhere=values.find(v=>v!==want&&v!==mine)
      await page.evaluate(m=>window.fixtureModelElsewhere(m),elsewhere)
      await page.locator(".pa-chat-model-dialog .pa-actions button").last().click()
      const conflict=page.locator(".pa-chat-model-dialog .pa-settings-conflict-actions")
      await conflict.waitFor()
      const text=await page.locator(".pa-chat-model-dialog [role=alert]").textContent()
      assert.match(text,/模型/,"the conflict does not name the model")
      assert.doesNotMatch(text,/重新開啟/,"the conflict asks to reopen")
      await page.screenshot({path:path.join(artifacts,`${label}-${name}-conflict.png`)})
      await conflict.getByRole("button",{name:"用我的版本覆蓋"}).click()
      await page.waitForFunction(()=>window.fixtureSaves>=2)
      assert.equal((await page.evaluate(()=>window.fixtureBot())).runtime_settings?.model??"",mine)
      await page.screenshot({path:path.join(artifacts,`${label}-${name}-overwrite-saved.png`)})
      results.push({label,viewport:name,scenario:"conflict",overwrite:true})
    }
    await page.close()
  }
}finally{
  await browser.close();server.close()
  await writeFile(path.join(artifacts,`${label}-results.json`),JSON.stringify({results,requests},null,2))
}
function w(k){return {close:"關閉"}[k]}
console.log(JSON.stringify(results))
