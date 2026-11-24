/** Queued messages scroll with the conversation instead of growing the fixed footer. */
import assert from "node:assert/strict"
import {createServer} from "node:http"
import {mkdir,writeFile} from "node:fs/promises"
import {fileURLToPath,pathToFileURL} from "node:url"
import path from "node:path"
import {build} from "esbuild"

const {BOTS_ARTIFACTS:artifacts,PLAYWRIGHT_MODULE:module}=process.env
assert.ok(artifacts&&module)
await mkdir(artifacts,{recursive:true})
const bundle=await build({entryPoints:[fileURLToPath(new URL("./fixtures/project-assistants-conversation-stages.tsx",import.meta.url))],
  bundle:true,write:false,outfile:"stages.js",format:"esm",platform:"browser",target:"es2022",jsx:"automatic",define:{"process.env.NODE_ENV":'"development"'}})
const files=new Map(bundle.outputFiles.map(file=>["/"+path.basename(file.path),file.contents]))
const server=createServer((req,res)=>{
  const body=files.get(req.url)
  if(body){res.setHeader("Content-Type",req.url.endsWith(".css")?"text/css":"text/javascript");res.end(body);return}
  res.setHeader("Content-Type","text/html")
  res.end('<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><style>body{margin:0}</style><link rel="stylesheet" href="/stages.css"><div id="app"></div><script type="module" src="/stages.js"></script>')
})
await new Promise(resolve=>server.listen(0,"127.0.0.1",resolve))
const origin="http://127.0.0.1:"+server.address().port,pw=await import(pathToFileURL(module).href)
const browser=await pw.chromium.launch({headless:true,args:["--disable-features=OmniboxEverywhere"]})
const long="So the observation items can go ahead and be wired through, but they must not turn into hard rules. When something looks wrong, ask us first and explain what you saw."
async function verify({width,height,language}){
  const tag=`${width}x${height}`,mobile=width<700
  const context=await browser.newContext({viewport:{width,height},hasTouch:mobile,reducedMotion:"no-preference"})
  const page=await context.newPage(),failures=[],facts={},errors=[]
  page.on("pageerror",error=>errors.push(String(error)))
  await page.route("**/*",route=>route.request().url().startsWith(origin+"/")?route.continue():route.abort())
  const check=(name,ok,detail)=>{if(!ok)failures.push({name,detail})}
  const editor=page.locator(".pa-editor textarea"),action=page.locator(".pa-composer .pa-send"),sending=page.locator(".pa-sending")
  const shot=state=>page.screenshot({path:path.join(artifacts,tag+"-"+state+".png")})
  const read=(name,arg)=>page.evaluate(([name,arg])=>window.stages[name](...(arg??[])),[name,arg])
  const click=locator=>mobile?locator.tap():locator.click()
  const wait=fn=>page.waitForFunction(fn,null,{timeout:12000})
  const begin=async text=>{await editor.fill(text);await click(action);await sending.waitFor();await page.waitForFunction(()=>window.stages.sending())}
  const queue=async text=>{await begin(text);await read("release",["accepted"]);await page.locator(".pa-queued").filter({hasText:text.slice(0,30)}).waitFor();await page.waitForTimeout(120)}
  const footer=()=>page.locator(".pa-chat-footer").evaluate(el=>Math.round(el.getBoundingClientRect().height))
  const placement=()=>page.evaluate(()=>{
    const scroll=document.querySelector(".pa-chat-scroll"),cards=[...document.querySelectorAll(".pa-queued")]
    const rows=[...document.querySelectorAll(".pa-chat-scroll [data-message-id]")],last=rows.at(-1)
    return {cards:cards.length,inside:cards.every(card=>scroll.contains(card)),
      after:!!last&&cards.every(card=>last.compareDocumentPosition(card)&Node.DOCUMENT_POSITION_FOLLOWING&&card.getBoundingClientRect().top>=last.getBoundingClientRect().bottom-1)}
  })
  const visible=selector=>page.evaluate(selector=>{
    const scroll=document.querySelector(".pa-chat-scroll").getBoundingClientRect(),el=document.querySelector(selector)
    if(!el)return false
    const r=el.getBoundingClientRect(),bottom=Math.min(scroll.bottom,window.visualViewport?.height??innerHeight)
    return r.top>=scroll.top-1&&r.bottom<=bottom+1
  },selector)
  try{
    await page.goto(origin+"/?lang="+language)
    await wait(()=>document.querySelector(".pa-editor textarea:not(:disabled)"))
    await begin("Start the running synthetic turn");await read("release",["accepted"])
    await page.locator('[data-message-id="person-turn-1"]').waitFor()
    await read("running",["turn-1"])
    await page.locator(".pa-working").waitFor()
    for(let i=0;i<6;i++)await read("publish",[`filler-${i+1}`,`Synthetic progress paragraph ${i+1}. `+"Earlier context that the person may want to scroll back and read. ".repeat(4)])
    await page.locator('[data-message-id="bot-filler-6"]').waitFor()
    const base=await footer()
    await queue(long)
    await shot("long-queued")
    const one=await footer()
    facts.long=await placement()
    check("long queued message is inside the scroll area after the last message",facts.long.inside&&facts.long.after,facts.long)
    check("the newest queued message is visible when the person was at the bottom",await visible(".pa-queued:last-of-type"))
    await queue("Second queued synthetic message")
    await queue("Third queued synthetic message")
    await shot("three-queued")
    const three=await footer()
    facts.footer={base,one,three}
    check("footer height does not grow with queued messages",one<=base+1&&three<=base+1,facts.footer)
    facts.three=await placement()
    check("three queued messages are inside the scroll area after the last message",facts.three.cards===3&&facts.three.inside&&facts.three.after,facts.three)

    // Reading earlier messages: a message queued from another device must not pull the view down.
    // (Sending from this composer deliberately follows the newest end.)
    await page.locator(".pa-chat-scroll").evaluate(el=>{el.scrollTop=0;el.dispatchEvent(new Event("scroll"))})
    await page.waitForTimeout(80)
    await read("queueElsewhere",["remote-1","Fourth queued while reading earlier messages"])
    await page.locator(".pa-queued").filter({hasText:"Fourth queued"}).waitFor()
    await page.waitForTimeout(120)
    const top=await page.locator(".pa-chat-scroll").evaluate(el=>el.scrollTop)
    facts.readingTop=top
    check("scrolling up keeps its place when a message is queued",top<=2,{top})
    check("the jump capsule is shown while reading earlier messages",await page.locator(".pa-jump").getAttribute("data-shown")==="on")
    await click(page.locator(".pa-jump .tx-jump-go"))
    await page.waitForTimeout(80)
    check("jumping to the latest shows the last queued message",await visible(".pa-queued:last-of-type"))

    // Edit one queued message; a phone keyboard shrinks the viewport.
    const target=page.locator(".pa-queued").filter({hasText:"Second queued"})
    await click(target.locator(".pa-queued-edit"))
    await page.locator(".pa-queued-editor").waitFor()
    if(mobile){await page.setViewportSize({width,height:Math.round(height*.52)});await page.waitForTimeout(200)}
    else await page.waitForTimeout(80)
    await shot("editing")
    check("the queued message being edited stays in view",await visible(".pa-queued[data-editing]"))
    check("the jump capsule does not cover the message being edited",await page.locator(".pa-jump").getAttribute("data-shown")==="off")
    check("the editor has focus",await page.evaluate(()=>document.activeElement?.classList.contains("pa-queued-editor")))
    await click(target.locator(`.pa-queued-action[aria-label]`).nth(1))
    await page.locator(".pa-queued-editor").waitFor({state:"detached"})
    if(mobile)await page.setViewportSize({width,height})
    const edits=await page.evaluate(()=>window.stages.queueEdits.map(edit=>edit.action))
    check("edit begins and keeps the original",JSON.stringify(edits)==='["begin","cancel"]',edits)

    // Remove and send-now still work from inside the conversation.
    const removed=page.locator(".pa-queued").filter({hasText:"Fourth queued"})
    await click(removed.locator(".pa-queued-remove"))
    await wait(()=>window.stages.queueActions.length===1);await read("releaseQueueAction")
    await removed.waitFor({state:"detached"})
    const steered=page.locator(".pa-queued").filter({hasText:"Third queued"})
    await click(steered.locator(".pa-queued-send"))
    await wait(()=>window.stages.queueActions.length===2);await read("releaseQueueAction")
    await page.locator('[data-message-id="person-turn-4"]').waitFor()
    await steered.waitFor({state:"detached"})
    facts.actions=await page.evaluate(()=>window.stages.queueActions.map(row=>row.command.operation))

    // When the running turn ends, waiting messages become ordinary messages without vanishing first.
    await page.evaluate(()=>{
      const watched=["Second queued synthetic message","So the observation items"]
      window.queueGaps=[]
      const observer=new MutationObserver(()=>{for(const text of watched)if(!document.querySelector(".pa-chat-scroll")?.textContent?.includes(text))window.queueGaps.push(text)})
      observer.observe(document.querySelector(".pa-chat-scroll"),{subtree:true,childList:true,characterData:true})
      window.stopQueueWatch=()=>observer.disconnect()
    })
    const animationsBefore=(await page.evaluate(()=>window.stages.animations.length))
    await read("finish",["turn-1"])
    await page.locator('[data-message-id="person-turn-2"]').waitFor()
    await page.locator(".pa-queued").first().waitFor({state:"detached"})
    await page.waitForTimeout(400)
    const gaps=await page.evaluate(()=>{window.stopQueueWatch();return window.queueGaps})
    const entered=await page.evaluate(n=>window.stages.animations.slice(n).filter(row=>row.id==="person-turn-2"||row.id==="person-turn-3").map(row=>row.id),animationsBefore)
    facts.transition={gaps,entered}
    check("queued messages never disappear while becoming ordinary messages",gaps.length===0,gaps)
    check("a message that was already shown does not play an entrance again",entered.length===0,entered)
    check("no page errors",errors.length===0,errors)
    return {tag,facts,failures}
  }catch(error){
    await shot("failure").catch(()=>{})
    await writeFile(path.join(artifacts,tag+"-failure.json"),JSON.stringify({error:String(error),facts,failures,errors},null,2))
    throw error
  }finally{await context.close()}
}
try{
  const results=await Promise.allSettled([
    verify({width:390,height:844,language:"zh-Hant"}),
    verify({width:1280,height:800,language:"en"}),
  ])
  const evidence=results.flatMap(result=>result.status==="fulfilled"?[result.value]:[])
  await writeFile(path.join(artifacts,"queue-in-chat.json"),JSON.stringify({evidence,externalModelRequests:0},null,2))
  for(const result of results)if(result.status==="rejected")throw result.reason
  const failed=evidence.flatMap(row=>row.failures.map(failure=>`${row.tag}: ${failure.name} ${JSON.stringify(failure.detail??"")}`))
  if(failed.length){console.error(failed.join("\n"));process.exitCode=1}
  else console.log(`PASS ${evidence.length} queue-in-chat cases; external model requests=0`)
}finally{
  await browser.close()
  server.closeAllConnections()
  await new Promise(resolve=>server.close(resolve))
}
