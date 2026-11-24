/** The production Bot settings editor saves while its Bot works, against a real handler, store and synthetic runtime. */
// Run by TestSettingsRebaseBrowser with BOTS_* set; PLAYWRIGHT_MODULE names a Playwright index.mjs.
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

const bundle=await build({stdin:{contents:`
import React,{useState} from "react";import {createRoot} from "react-dom/client";
import {BotSettings} from "./src/project-assistants/BotSettings";
import {FallbackSourceScope} from "./src/project-assistants/FallbackSettings";
import {ProjectAssistantSourceClient} from "./src/cloud/project-assistants";import {command} from "./src/project-assistants/client";
import {words} from "./src/project-assistants/words";import {bootCatalog} from "./src/catalog";import "./src/project-assistants/styles.css";
const q=new URL(location.href).searchParams,language=q.get("lang");localStorage.setItem("ui_language",language);await bootCatalog(tag=>"/catalog/"+tag+".json");
const live={accountKey:"synthetic-account",readerID:"synthetic-reader",machineID:${JSON.stringify(machine)},authorizationRevision:"synthetic-grant",apiLevel:Number(q.get("api")),paired:true,online:true,writable:true};
const source=new ProjectAssistantSourceClient(live,(input,init)=>fetch(input,init),()=>live);
const key={machine_id:live.machineID,bot_id:${JSON.stringify(bot)}},signal=()=>new AbortController().signal,w=words(language);
const got=await source.get(key,signal());
// The Bot answers a message: a real synthetic turn that advances the Bot version only.
window.fixtureReply=async()=>{const before=(await source.get(key,signal())).bot.version;const fresh=await source.get(key,signal());const c=fresh.conversation;
  await source.send({command:command(key,"send"),fence:{key,project:fresh.bot.project,conversation_id:c.id,binding_id:c.binding_id,generation:c.generation},text:"Synthetic message while editing",quote_message_id:""},signal());
  for(let i=0;i<100;i++){const now=await source.get(key,signal());if(now.bot.version>before&&!now.conversation.active_turn_id)return now.bot.version;await new Promise(r=>setTimeout(r,100))}
  throw Error("the synthetic turn did not finish")};
// Another editor saves standing instructions.
window.fixtureEditElsewhere=async text=>{const now=(await source.get(key,signal())).bot;await source.edit({command:command(key,"edit"),settings:{name:now.name,appearance:now.appearance,instructions:text,expected_version:now.version}},signal());return text};
window.fixtureBot=async()=>(await source.get(key,signal())).bot;
function Fixture(){const [current,setCurrent]=useState(got.bot),[message,setMessage]=useState(""),[revision,setRevision]=useState(0);
return <FallbackSourceScope source={source}><main className="project-assistants" style={{maxWidth:720,margin:"0 auto"}}>
<BotSettings key={revision} embedded bot={current} enabled={true} w={w} onClose={()=>setRevision(n=>n+1)} onCheck={async()=>{const fresh=(await source.get(key,signal())).bot;setCurrent(fresh);return fresh}}
  onSave={async settings=>{const saved=await source.edit({command:command(key,"edit"),settings},signal());setCurrent(saved.bot);setRevision(n=>n+1);window.fixtureSaves=(window.fixtureSaves??0)+1;setMessage(w.settingsSaved+" #"+window.fixtureSaves)}}/>
<p role="status" className="fixture-status">{message}</p></main></FallbackSourceScope>}
createRoot(document.getElementById("app")).render(<Fixture/>);
`,resolveDir:root,loader:"tsx"},bundle:true,write:false,outfile:"settings-rebase.js",format:"esm",target:"es2022",platform:"browser",jsx:"automatic",define:{"process.env.NODE_ENV":'"development"'}})
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
  res.setHeader("Content-Type","text/html");res.end('<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="stylesheet" href="/settings-rebase.css"><body style="background:#111;color:#eee"><div id="app"></div><script type="module" src="/settings-rebase.js"></script></body>')
}catch(error){res.writeHead(500);res.end(String(error))}})
await new Promise(resolve=>server.listen(0,"127.0.0.1",resolve))
const origin=`http://127.0.0.1:${server.address().port}`
const label=process.env.SETTINGS_REBASE_LABEL||"after",api=process.env.SETTINGS_REBASE_API||"116"
const browser=await chromium.launch({args:["--disable-features=OmniboxEverywhere"]})
const results=[]
try{
  for(const [name,viewport] of [["mobile",{width:390,height:844}],["desktop",{width:1280,height:800}]]){
    const page=await browser.newPage({viewport})
    page.on("pageerror",e=>console.error("pageerror",e.message))
    page.on("console",m=>{if(m.type()==="error")console.error("console",m.text())})
    await page.goto(origin+"/?lang=zh-Hant&api="+api)
    const instructions=page.locator(".pa-settings-instructions textarea")
    await instructions.waitFor()
    // The person's report: edit standing instructions, the Bot replies, then Save.
    const draft=`請用短句回答（${name}）`
    await instructions.fill(draft)
    await page.evaluate(()=>window.fixtureReply())
    await page.locator(".pa-settings-savebar > .pa-settings-save").click()
    await page.waitForFunction(()=>document.querySelector(".pa-settings-save-error")||window.fixtureSaves>=1)
    const refusal=await page.locator(".pa-settings-save-error").allTextContents()
    await page.screenshot({path:path.join(artifacts,`${label}-${name}-reply-then-save.png`)})
    const saved=(await page.evaluate(()=>window.fixtureBot())).instructions.text
    results.push({label,viewport:name,scenario:"reply-then-save",refusal,saved:saved===draft})
    if(label==="after"){
      assert.deepEqual(refusal,[],"a Bot reply refused the save")
      assert.equal(saved,draft)
      // A real conflict: the same field changed elsewhere to a different value.
      await instructions.waitFor()
      const mine=`我的版本（${name}）`
      await instructions.fill(mine)
      await page.evaluate(n=>window.fixtureEditElsewhere("別處的版本 "+n),name)
      await page.locator(".pa-settings-savebar > .pa-settings-save").click()
      const conflict=page.locator(".pa-settings-conflict-actions")
      await conflict.waitFor()
      const text=await page.locator(".pa-settings-save-error").textContent()
      assert.match(text,/固定指示/,"the conflict does not name the field")
      assert.equal(await instructions.inputValue(),mine,"the draft was lost")
      assert.equal(await conflict.locator("button").count(),2)
      await page.screenshot({path:path.join(artifacts,`${label}-${name}-conflict.png`)})
      await conflict.getByRole("button",{name:"用我的版本覆蓋"}).click()
      await page.waitForFunction(()=>window.fixtureSaves>=2)
      assert.equal((await page.evaluate(()=>window.fixtureBot())).instructions.text,mine)
      await page.screenshot({path:path.join(artifacts,`${label}-${name}-overwrite-saved.png`)})
      // Load latest keeps other edits and takes the newer value of the conflicting field.
      await instructions.fill("又一個草稿")
      await page.locator(".pa-settings-field input").first().fill("Xavia｜COO "+name)
      await page.evaluate(()=>window.fixtureEditElsewhere("別處又改了"))
      await page.locator(".pa-settings-savebar > .pa-settings-save").click()
      await conflict.waitFor()
      await conflict.getByRole("button",{name:"載入最新設定"}).click()
      await page.waitForFunction(()=>!document.querySelector(".pa-settings-conflict-actions"))
      assert.equal(await instructions.inputValue(),"別處又改了")
      assert.equal(await page.locator(".pa-settings-field input").first().inputValue(),"Xavia｜COO "+name)
      await page.locator(".pa-settings-savebar > .pa-settings-save").click()
      await page.waitForFunction(()=>window.fixtureSaves>=3)
      const final=await page.evaluate(()=>window.fixtureBot())
      assert.equal(final.name,"Xavia｜COO "+name);assert.equal(final.instructions.text,"別處又改了")
      results.push({label,viewport:name,scenario:"conflict",overwrite:true,loadLatest:true})
    }
    await page.close()
  }
}finally{
  await browser.close();server.close()
  await writeFile(path.join(artifacts,`${label}-results.json`),JSON.stringify({results,requests},null,2))
}
console.log(JSON.stringify(results))
