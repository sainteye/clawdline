/** Background work, dispatched work and waiting messages scroll at the end of the conversation; the footer keeps only the composer's own rows. */
import assert from "node:assert/strict"
import {createServer} from "node:http"
import {mkdir,writeFile} from "node:fs/promises"
import {fileURLToPath,pathToFileURL} from "node:url"
import path from "node:path"
import {build} from "esbuild"

const {BOTS_ARTIFACTS:artifacts,PLAYWRIGHT_MODULE:module}=process.env
assert.ok(artifacts&&module,"Set BOTS_ARTIFACTS and PLAYWRIGHT_MODULE")
await mkdir(artifacts,{recursive:true})
const label=process.env.BOTS_SHOT_LABEL||"after"
const bundle=await build({entryPoints:[fileURLToPath(new URL("./fixtures/project-assistants-conversation-stages.tsx",import.meta.url))],
  bundle:true,write:false,outfile:"stages.js",format:"esm",platform:"browser",target:"es2022",jsx:"automatic",define:{"process.env.NODE_ENV":'"development"'}})
const files=new Map(bundle.outputFiles.map(file=>["/"+path.basename(file.path),file.contents]))
const server=createServer((req,res)=>{
  const body=files.get(req.url)
  if(body){res.setHeader("Content-Type",req.url.endsWith(".css")?"text/css":"text/javascript");res.end(body);return}
  res.setHeader("Content-Type","text/html")
  res.end('<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1,viewport-fit=cover"><style>body{margin:0}</style><link rel="stylesheet" href="/stages.css"><div id="app"></div><script type="module" src="/stages.js"></script>')
})
await new Promise(resolve=>server.listen(0,"127.0.0.1",resolve))
const origin="http://127.0.0.1:"+server.address().port,pw=await import(pathToFileURL(module).href)

async function verify(browser,engine,{width,height,language}){
  const tag=`${engine}-${width}x${height}`,mobile=width<700
  const context=await browser.newContext({viewport:{width,height},hasTouch:mobile})
  const page=await context.newPage(),failures=[],facts={},errors=[]
  page.on("pageerror",error=>errors.push(String(error)))
  await page.route("**/*",route=>route.request().url().startsWith(origin+"/")?route.continue():route.abort())
  const check=(name,ok,detail)=>{if(!ok)failures.push({name,detail})}
  const editor=page.locator(".pa-editor textarea"),action=page.locator(".pa-composer .pa-send")
  const shot=state=>page.screenshot({path:path.join(artifacts,`${label}-${tag}-${state}.png`)})
  const read=(name,arg)=>page.evaluate(([name,arg])=>window.stages[name](...(arg??[])),[name,arg])
  const click=locator=>mobile?locator.tap():locator.click()
  const scrollEl=page.locator(".pa-chat-scroll")
  const footer=()=>page.locator(".pa-chat-footer").evaluate(el=>Math.round(el.getBoundingClientRect().height))
  const begin=async text=>{await editor.fill(text);await click(action);await page.locator(".pa-sending").waitFor();await page.waitForFunction(()=>window.stages.sending())}
  const queue=async text=>{await begin(text);await read("release",["accepted"]);await page.locator(".pa-queued").filter({hasText:text.slice(0,24)}).waitFor();await page.waitForTimeout(120)}
  // Each selector is inside the scroll area, after the last message, and in the order given.
  const placement=selectors=>page.evaluate(selectors=>{
    const scroll=document.querySelector(".pa-chat-scroll"),footer=document.querySelector(".pa-chat-footer")
    const last=[...scroll.querySelectorAll("[data-message-id]")].at(-1)
    const nodes=selectors.map(selector=>document.querySelector(selector))
    const missing=selectors.filter((_,i)=>!nodes[i])
    if(missing.length)return {missing}
    const following=(a,b)=>!!(a.compareDocumentPosition(b)&Node.DOCUMENT_POSITION_FOLLOWING)
    return {missing,
      inside:nodes.every(node=>scroll.contains(node)&&!footer.contains(node)),
      afterLast:!!last&&nodes.every(node=>following(last,node)&&node.getBoundingClientRect().top>=last.getBoundingClientRect().bottom-1),
      ordered:nodes.every((node,i)=>i===0||following(nodes[i-1],node))}
  },selectors)
  const visible=selector=>page.evaluate(selector=>{
    const scroll=document.querySelector(".pa-chat-scroll").getBoundingClientRect(),el=document.querySelector(selector)
    if(!el)return false
    const r=el.getBoundingClientRect()
    return r.height>0&&r.top>=scroll.top-1&&r.bottom<=scroll.bottom+1
  },selector)
  try{
    await page.goto(origin+"/?lang="+language)
    await page.waitForFunction(()=>document.querySelector(".pa-editor textarea:not(:disabled)"),null,{timeout:12000})
    await begin("Start the running synthetic turn");await read("release",["accepted"])
    await page.locator('[data-message-id="person-turn-1"]').waitFor()
    await read("running",["turn-1"])
    await page.locator(".pa-working").waitFor()
    for(let i=0;i<6;i++)await read("publish",[`filler-${i+1}`,`Synthetic progress paragraph ${i+1}. `+"Earlier context that the person may want to scroll back and read. ".repeat(4)])
    await page.locator('[data-message-id="bot-filler-6"]').waitFor()
    await page.waitForTimeout(150)
    const base=await footer()

    await read("background",["active"])
    await page.locator(".pa-background-capsule").waitFor()
    await page.waitForTimeout(150)
    await shot("background")
    facts.background=await placement([".pa-background-capsule"])
    check("the background capsule scrolls after the last message",facts.background.inside&&facts.background.afterLast,facts.background)
    check("the background capsule is in view at the newest end",await visible(".pa-background-capsule"))
    const withBackground=await footer()

    await read("dispatched",[{running:1,needs_you:0,delivered:13}])
    await page.locator(".pa-dispatched").waitFor()
    await page.waitForTimeout(150)
    await shot("dispatched")
    facts.dispatched=await placement([".pa-dispatched"])
    check("the dispatched-work capsule scrolls after the last message",facts.dispatched.inside&&facts.dispatched.afterLast,facts.dispatched)
    const withDispatched=await footer()

    await queue("A synthetic message waiting behind the running turn")
    await shot("all-three")
    facts.all=await placement([".pa-queued",".pa-dispatched",".pa-background-capsule"])
    check("queued, dispatched and background work all scroll at the end, waiting messages first",facts.all.inside&&facts.all.afterLast&&facts.all.ordered,facts.all)
    check("the work row is in view at the newest end",await visible(".pa-dispatched"))
    const withAll=await footer()
    facts.footer={base,withBackground,withDispatched,withAll}
    check("the footer does not grow with background work, dispatched work or waiting messages",[withBackground,withDispatched,withAll].every(h=>h<=base+1),facts.footer)

    // Reading earlier messages: work starting, ending and ticking must not move the view.
    await scrollEl.evaluate(el=>{el.scrollTop=40;el.dispatchEvent(new Event("scroll"))})
    await page.waitForTimeout(120)
    const top=await scrollEl.evaluate(el=>el.scrollTop)
    await read("background",["succeeded"]);await page.locator(".pa-background-capsule").waitFor({state:"detached"})
    await read("background",["active","Check the synthetic notes again"]);await page.locator(".pa-background-capsule").waitFor()
    await page.waitForTimeout(2200)
    const after=await scrollEl.evaluate(el=>el.scrollTop)
    facts.reading={top,after}
    check("work starting, ending and its timer do not move a reader who scrolled up",Math.abs(after-top)<=1,facts.reading)
    const jump=page.locator(".pa-jump .tx-jump-go")
    check("the jump capsule shows while reading earlier messages",await page.locator(".pa-jump").getAttribute("data-shown")==="on")
    check("the jump capsule says background work is running",await jump.locator(".pa-jump-working").count()===1)
    facts.jumpName=await jump.getAttribute("aria-label")
    check("the jump capsule's name includes the working state",!!facts.jumpName&&facts.jumpName.length>0,facts.jumpName)
    await shot("reading-earlier")

    // The timer updates only its own text: the content height stays the same over two ticks.
    await click(jump);await page.waitForTimeout(150)
    const heights=[]
    for(let i=0;i<3;i++){heights.push(await page.locator(".pa-chat-content").evaluate(el=>el.getBoundingClientRect().height));await page.waitForTimeout(1050)}
    facts.heights=heights
    check("the ticking timer does not change the conversation's height",heights.every(h=>Math.abs(h-heights[0])<0.5),heights)
    check("after jumping, the stop control is in view",await visible(".pa-background-capsule .pa-background-stop"))
    const stop=page.locator(".pa-background-capsule .pa-background-stop")
    check("the stop control is enabled",await stop.isEnabled())
    await click(stop)
    await page.locator("dialog.pa-roster-confirm").waitFor()
    check("stopping asks for confirmation",await page.locator("dialog.pa-roster-confirm[open]").count()===1)
    await page.locator("dialog.pa-roster-confirm button").first().click()
    await page.locator("dialog.pa-roster-confirm").waitFor({state:"detached"})
    // Opening the dispatched list at the newest end keeps the list in view.
    await click(page.locator(".pa-dispatched-toggle"))
    await page.waitForTimeout(200)
    check("the opened dispatched list stays in view at the newest end",await visible(".pa-dispatched-toggle"))
    check("the page never scrolls sideways",await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth))
    check("no page errors",errors.length===0,errors)
    return {tag,facts,failures}
  }catch(error){
    await shot("failure").catch(()=>{})
    return {tag,facts,failures:[...failures,{name:"threw",detail:String(error)}]}
  }finally{await context.close()}
}
const evidence=[]
try{
  for(const engine of ["chromium","webkit"]){
    const browser=await pw[engine].launch({headless:true,...(engine==="chromium"?{args:["--disable-features=OmniboxEverywhere"]}:{})})
    try{
      for(const size of [{width:390,height:844,language:"zh-Hant"},{width:1280,height:800,language:"en"}])evidence.push(await verify(browser,engine,size))
    }finally{await browser.close()}
  }
}finally{
  server.closeAllConnections()
  await new Promise(resolve=>server.close(resolve))
}
await writeFile(path.join(artifacts,`${label}-work-in-chat.json`),JSON.stringify(evidence,null,2))
const failed=evidence.flatMap(row=>row.failures.map(failure=>`${row.tag}: ${failure.name} ${JSON.stringify(failure.detail??"")}`))
if(failed.length){console.error(failed.join("\n"));process.exitCode=1}
else console.log(`PASS ${evidence.length} work-in-chat cases`)
