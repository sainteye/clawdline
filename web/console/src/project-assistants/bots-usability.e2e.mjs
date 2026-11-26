// Bots settings and memory usability in a real browser, at 1280x800 and 390x844:
// the shared-memory Project scope list is wholly visible inside the dialog and
// every checkbox carries its Project's name; the memory forget section's confirm
// is reachable by scrolling and each entry checkbox carries its entry text;
// forgetting says a fresh conversation follows and, while the Bot works, offers
// waiting; Cancel in Bot settings discards the draft and keeps the panel open.
// PLAYWRIGHT_MODULE=<playwright index.mjs> BOTS_ARTIFACTS=<dir> node bots-usability.e2e.mjs
import assert from "node:assert/strict"
import {createServer} from "node:http"
import {mkdir,readFile} from "node:fs/promises"
import path from "node:path"
import {pathToFileURL} from "node:url"
import {build} from "esbuild"

const {PLAYWRIGHT_MODULE,BOTS_ARTIFACTS}=process.env
assert.ok(PLAYWRIGHT_MODULE&&BOTS_ARTIFACTS,"Use an existing Playwright installation and a task-owned evidence directory")
await mkdir(BOTS_ARTIFACTS,{recursive:true})
const browsers=await import(pathToFileURL(PLAYWRIGHT_MODULE).href)
const bundle=async entry=>{
  const out=await build({entryPoints:[new URL(entry,import.meta.url).pathname],outfile:"project-assistants-fixture.js",write:false,bundle:true,format:"esm",platform:"browser",jsx:"automatic",define:{"process.env.NODE_ENV":'"development"'}})
  return new Map(out.outputFiles.map(file=>["/"+path.basename(file.path),file.contents]))
}
const fixtures={settings:await bundle("bots-settings.fixture.tsx"),bots:await bundle("fixtures.ts")}
const html=await readFile(new URL("../../tools/fixtures/project-assistants.html",import.meta.url))
const catalogs=new URL("../../public/catalogs/",import.meta.url)
const types={".css":"text/css",".json":"application/json"}
const server=createServer(async(req,res)=>{
  const url=new URL(req.url,"http://fixture"),pathname=url.pathname
  if(pathname==="/"){res.setHeader("Content-Type","text/html");res.end(html);return}
  if(pathname.startsWith("/catalog/")){try{res.setHeader("Content-Type","application/json");res.end(await readFile(new URL(path.basename(pathname),catalogs)))}catch{res.writeHead(404).end()}return}
  const set=req.headers.referer?.includes("fixture=bots")?fixtures.bots:fixtures.settings
  const file=set.get(pathname)
  if(!file){res.writeHead(404).end();return}
  res.setHeader("Content-Type",types[path.extname(pathname)]??"text/javascript");res.end(file)
})
await new Promise(resolve=>server.listen(0,"127.0.0.1",resolve))
const origin="http://127.0.0.1:"+server.address().port
const browser=await browsers.chromium.launch({args:["--disable-features=OmniboxEverywhere"]})
const failures=[]
const check=(label,fn)=>{try{fn()}catch(error){failures.push(`${label}: ${error.message.split("\n")[0]}`)}}

// The accessible names Chrome itself computes, from its accessibility tree.
async function checkboxNames(page){
  const cdp=await page.context().newCDPSession(page)
  const {nodes}=await cdp.send("Accessibility.getFullAXTree")
  await cdp.detach()
  return nodes.filter(n=>n.role?.value==="checkbox"&&!n.ignored).map(n=>n.name?.value??"")
}

async function scopeList(page,name){
  await page.goto(origin+"/?projects=many&delay=50")
  await page.click("#opener")
  await page.getByRole("dialog").waitFor()
  await page.getByRole("button",{name:"共用記憶"}).click()
  await page.getByRole("button",{name:"新增記憶"}).click()
  const scope=page.locator(".pa-bs-memory").last().locator(".pa-bs-scope-button")
  await scope.click()
  await page.locator(".pa-bs-scope-pop").waitFor()
  // Choose a Project so the per-Project checkboxes are enabled, as in real use.
  await page.locator(".pa-bs-scope-pop input").first().evaluate(input=>input.click())
  await page.locator(".pa-bs-scope-pop input").nth(1).waitFor()
  await page.screenshot({path:path.join(BOTS_ARTIFACTS,`scope-${name}.png`)})
  const seen=await page.evaluate(()=>{
    const pop=document.querySelector(".pa-bs-scope-pop"),body=document.querySelector(".pa-bs-body")
    const p=pop.getBoundingClientRect(),b=body.getBoundingClientRect()
    // Each option's checkbox and the start of its name must be inside the scrolling pane that clips it.
    const options=[...pop.querySelectorAll("label")].map(l=>{const box=l.querySelector("input").getBoundingClientRect();return box.left})
    return {pop:{left:p.left,right:p.right},body:{left:b.left,right:b.right},minCheckbox:Math.min(...options),scrollWidth:pop.scrollWidth,clientWidth:pop.clientWidth}
  })
  check(`${name} scope list inside the pane`,()=>assert.ok(seen.pop.left>=seen.body.left-0.5&&seen.pop.right<=seen.body.right+0.5,JSON.stringify(seen)))
  check(`${name} scope checkboxes visible`,()=>assert.ok(seen.minCheckbox>=seen.body.left,JSON.stringify(seen)))
  check(`${name} scope list has no sideways overflow`,()=>assert.ok(seen.scrollWidth<=seen.clientWidth+1,JSON.stringify(seen)))
  const names=await checkboxNames(page)
  check(`${name} scope checkbox names`,()=>assert.ok(names.length>=23&&names.every(Boolean),JSON.stringify(names)))
  const explicit=await page.locator(".pa-bs-scope-pop input").evaluateAll(inputs=>inputs.map(i=>i.getAttribute("aria-labelledby")||i.getAttribute("aria-label")||(i.id&&document.querySelector(`label[for="${i.id}"]`)?.textContent)||""))
  check(`${name} scope checkboxes are explicitly labelled`,()=>assert.ok(explicit.every(Boolean),JSON.stringify(explicit.slice(0,3))))
}

const memoryEntries=Array.from({length:14},(_,i)=>({id:`m-${i}`,author:"bot",text:`Synthetic remembered fact ${i+1}: prefer short answers and name the file first.`,sources:[{id:`s-${i}`,kind:"message",revision:1}],updated_at:"2026-11-26T00:00:00Z"}))

async function openBot(page,width){
  await page.goto(origin+"/?fixture=bots&lang=zh-Hant")
  await page.evaluate(entries=>{
    const {a,open}=window.botsFixture
    Object.assign(a,{
      canMemorySettings:()=>true,
      memoryView:async(key,conversation_id)=>{
        const bot=a.bots.find(b=>b.key.bot_id===key.bot_id)
        const source=s=>({...s,key,project:bot.project})
        return {contract_version:"project-assistants.v1",source_epoch:"epoch-1",view:{bot,conversation_id,layers:[
          {layer:"project",available:false,cap:0,used:0,revision:"",truncated:false,entries:[]},
          {layer:"bot",available:true,cap:200,used:entries.length,revision:"1",truncated:false,entries:entries.map(e=>({...e,sources:e.sources.map(source)}))}],
          memory:{key,project:bot.project,revision:1,forgotten_through:0,forgotten_count:0}}}
      },
    })
    open()
  },memoryEntries)
  await page.locator('.pa-bot-row[data-bot-key=\'["studio","bot-0"]\']').click()
  await page.locator(".pa-chat-identity").click()
  return page.locator(".pa-info-panel")
}

async function forgetSection(page,name,width){
  const panel=await openBot(page,width)
  await panel.getByRole("tab",{name:"記憶",exact:true}).click()
  await panel.locator(".pa-memory-forget summary").click()
  const confirm=panel.locator(".pa-memory-forget fieldset button",{hasText:"忘記所選來源"}).last()
  await confirm.waitFor()
  await panel.locator(".pa-memory-forget input[type=checkbox]").first().check()
  // With the section's heading at the top of the panel, the confirm stays in view
  // while the list is longer than the screen; the wheel also reaches it.
  await panel.locator(".pa-memory-forget summary").evaluate(node=>node.scrollIntoView({block:"start"}))
  await page.waitForTimeout(200)
  await page.screenshot({path:path.join(BOTS_ARTIFACTS,`forget-top-${name}.png`)})
  const inView=()=>confirm.evaluate(button=>{const r=button.getBoundingClientRect();return r.top>=0&&r.bottom<=innerHeight&&document.elementFromPoint(r.left+r.width/2,r.top+r.height/2)===button})
  const atStart=await inView()
  check(`${name} forget confirm in view at the section's start`,()=>assert.equal(atStart,true))
  const box=await panel.boundingBox()
  await page.mouse.move(box.x+box.width/2,box.y+box.height/2)
  for(let i=0;i<30;i++)await page.mouse.wheel(0,400)
  await page.waitForTimeout(200)
  await page.screenshot({path:path.join(BOTS_ARTIFACTS,`forget-${name}.png`)})
  const wheeled=await inView()
  check(`${name} forget confirm reachable by wheel`,()=>assert.equal(wheeled,true))
  const names=await checkboxNames(page)
  const entryNames=names.filter(n=>n.startsWith("Synthetic remembered fact"))
  check(`${name} forget checkbox names`,()=>assert.equal(entryNames.length,memoryEntries.length,JSON.stringify(names.slice(0,4))))
  const explicit=await panel.locator(".pa-memory-forget input[type=checkbox]").evaluateAll(inputs=>inputs.map(i=>i.getAttribute("aria-labelledby")||i.getAttribute("aria-label")||(i.id&&document.querySelector(`label[for="${i.id}"]`)?.textContent)||""))
  check(`${name} forget checkboxes are explicitly labelled`,()=>assert.ok(explicit.every(Boolean),JSON.stringify(explicit.slice(0,3))))
  // A fresh conversation is said before confirming.
  const text=await panel.locator(".pa-memory-forget").innerText()
  check(`${name} forget names the fresh conversation`,()=>assert.match(text,/新的對話/))
  // While the Bot works, forgetting first offers waiting.
  await page.evaluate(()=>window.botsFixture.a.workTurn("running"))
  // The turn reaches the chat through its event stream; wait until Stop shows it.
  await page.getByRole("button",{name:"停止目前與待送工作",exact:true}).or(page.locator("button[aria-label=\"停止目前與待送工作\"]")).first().waitFor({state:"attached"})
  await page.waitForTimeout(300)
  await panel.locator(".pa-memory-forget input[type=checkbox]").first().check()
  const before=await page.evaluate(()=>window.botsFixture.log.filter(r=>r.method==="forget").length)
  await confirm.click()
  await page.waitForTimeout(200)
  const working=await panel.locator(".pa-memory-forget").innerText()
  check(`${name} forget warns while working`,()=>assert.match(working,/正在工作/))
  const afterFirst=await page.evaluate(()=>window.botsFixture.log.filter(r=>r.method==="forget").length)
  check(`${name} forget waits for a choice while working`,()=>assert.equal(afterFirst,before))
  await page.screenshot({path:path.join(BOTS_ARTIFACTS,`forget-working-${name}.png`)})
  const wait=panel.getByRole("button",{name:"等它做完",exact:true})
  if(await wait.count())await wait.click()
  const proceed=panel.getByRole("button",{name:"仍要忘記",exact:true})
  if(await confirm.count())await confirm.click()
  if(await proceed.count()){
    await proceed.click()
    await page.waitForFunction(n=>window.botsFixture.log.filter(r=>r.method==="forget").length>n,before,{timeout:3000}).catch(()=>{})
  }
  const afterProceed=await page.evaluate(()=>window.botsFixture.log.filter(r=>r.method==="forget").length)
  check(`${name} proceeding forgets`,()=>assert.equal(afterProceed,before+1))
}

async function settingsCancel(page,name,width){
  const panel=await openBot(page,width)
  await panel.getByRole("tab",{name:"Bot 設定",exact:true}).click()
  const instructions=panel.locator(".pa-settings-instructions textarea")
  await instructions.waitFor()
  const original=await instructions.inputValue()
  await instructions.fill(original+" 草稿")
  await panel.getByRole("button",{name:"取消",exact:true}).click()
  await page.waitForTimeout(200)
  await page.screenshot({path:path.join(BOTS_ARTIFACTS,`cancel-${name}.png`)})
  const open=await panel.isVisible()
  check(`${name} cancel keeps the panel open`,()=>assert.equal(open,true))
  if(open){
    const selected=await panel.getByRole("tab",{name:"Bot 設定",exact:true}).getAttribute("aria-selected")
    check(`${name} cancel stays on Bot settings`,()=>assert.equal(selected,"true"))
    const value=await panel.locator(".pa-settings-instructions textarea").inputValue()
    check(`${name} cancel discards the draft`,()=>assert.equal(value,original))
  }
}

try{
  for(const [name,viewport] of [["1280",{width:1280,height:800}],["390",{width:390,height:844}]]){
    const touch=name==="390"
    const make=()=>browser.newPage({viewport,deviceScaleFactor:2,locale:"zh-TW",isMobile:touch,hasTouch:touch,reducedMotion:"reduce"})
    for(const run of [scopeList,forgetSection,settingsCancel]){
      const page=await make()
      page.setDefaultTimeout(4000)
      const errors=[];page.on("pageerror",e=>errors.push(e.message))
      try{await run(page,name,viewport.width)}catch(error){failures.push(`${name} ${run.name}: ${error.message.split("\n")[0]}`)}
      check(`${name} ${run.name} page errors`,()=>assert.deepEqual(errors,[]))
      await page.close()
    }
  }
}finally{await browser.close();server.close()}
for(const f of failures)console.log("FAIL",f)
if(failures.length){console.log(failures.length,"failed");process.exit(1)}
console.log("ok")
