import assert from "node:assert/strict"
import {mkdir,writeFile} from "node:fs/promises"
import {join} from "node:path"
import {pathToFileURL} from "node:url"
import {test} from "node:test"
const enabled=!!process.env.BOTS_FEEDBACK_ORIGIN&&!!process.env.PLAYWRIGHT_MODULE

test("Bots chime only for new successes and return Chrome control on every panel exit",{skip:!enabled},async()=>{
 const {chromium,webkit}=await import(pathToFileURL(process.env.PLAYWRIGHT_MODULE).href)
 const directory=process.env.BOTS_ARTIFACTS,results=[]
 if(directory)await mkdir(directory,{recursive:true})
 for(const engine of [chromium,webkit])for(const [language,width] of [["zh-Hant",390],["en",1280]]){
  const browser=await engine.launch({headless:true}),page=await browser.newPage({viewport:{width,height:844},reducedMotion:"reduce"}),errors=[]
  page.on("pageerror",error=>errors.push(error.message))
  const api=[];await page.route("**/v1/**",route=>{api.push(route.request().url());return route.abort()})
  const snapshot=()=>page.evaluate(()=>window.botFeedback.snapshot())
  const wait=predicate=>page.waitForFunction(predicate)
  try {
   await page.goto(`${process.env.BOTS_FEEDBACK_ORIGIN}/tools/fixtures/bot-feedback.html?lang=${language}`)
   await wait(()=>window.botFeedback?.snapshot().subscribers===1)
   assert.equal((await snapshot()).tones,0,"opening a browser with history stays quiet")
   await page.getByRole("button",{name:"Unlock audio",exact:true}).click()
   await page.evaluate(()=>window.botFeedback.complete("one"));await wait(()=>window.botFeedback.snapshot().tones===2)
   await page.evaluate(()=>window.botFeedback.complete("one"));await page.waitForTimeout(50)
   assert.equal((await snapshot()).tones,2,"replay stays quiet")
   await page.locator(".pa-dock").click();await page.locator(".pa-roster").waitFor();await wait(()=>window.botFeedback.snapshot().subscribers===1)
   await page.evaluate(()=>window.botFeedback.complete("two"));await wait(()=>window.botFeedback.snapshot().tones===4)
   await page.locator(".pa-collapse").click();await page.locator(".pa-roster").waitFor({state:"detached"});await wait(()=>window.botFeedback.snapshot().subscribers===1)
   assert.equal((await snapshot()).tones,4,"returning to the Session list does not repeat a chime")
   await page.evaluate(()=>window.botFeedback.complete("three"));await wait(()=>window.botFeedback.snapshot().tones===6)
   await page.evaluate(async()=>{const sound=await import("/src/project-assistants/completion-sound.ts");sound.setBotCompletionSound(false);await window.botFeedback.complete("muted")})
   assert.equal((await snapshot()).tones,6)
   await page.evaluate(async()=>{const sound=await import("/src/project-assistants/completion-sound.ts");sound.setBotCompletionSound(true);await window.botFeedback.complete("unmuted")})
   await wait(()=>window.botFeedback.snapshot().tones===8)

   const open=async()=>{await page.getByRole("button",{name:"Open browser fixture",exact:true}).click();await page.locator(".browser-who").waitFor()}
   const left=async()=>{await wait(()=>window.botFeedback.snapshot().person===false);await page.locator(".pa-browser").waitFor({state:"detached"})}
   await open();assert.equal((await snapshot()).person,true,"StrictMode effect replay does not hand back an open panel")
   if(directory)await page.screenshot({path:join(directory,`feedback-${engine.name()}-${language}.png`)})
   await page.locator(".browser-close").click();await left()
   assert.equal((await snapshot()).writes.filter(w=>w.body.person===false).length,1)
   await page.evaluate(()=>window.botFeedback.person(true));await open()
   await page.getByRole("button",{name:"Leave browser fixture",exact:true}).click();await left()
   assert.equal((await snapshot()).writes.filter(w=>w.body.person===false).length,2)
   await open();await page.evaluate(()=>window.botFeedback.hold())
   await page.locator(".browser-take").click()
   await wait(()=>window.botFeedback.snapshot().writes.some(w=>w.body.person===true))
   await page.getByRole("button",{name:"Leave browser fixture",exact:true}).click()
   assert.equal((await snapshot()).writes.filter(w=>w.body.person===false).length,2,"unmount waits for the pending takeover")
   await page.evaluate(()=>window.botFeedback.release());await left()
   assert.equal((await snapshot()).writes.filter(w=>w.body.person===false).length,3)
   await page.evaluate(()=>window.botFeedback.person(true));await open();await page.locator(".browser-close").focus();await page.keyboard.press("Escape");await left()
   await page.evaluate(()=>window.botFeedback.person(true));await open()
   await page.evaluate(()=>window.botFeedback.failExit());await page.locator(".browser-close").click()
   await page.locator(".pa-browser-exit-error").waitFor()
   assert.equal((await snapshot()).person,true,"an unconfirmed handback keeps the panel open")
   await page.locator(".browser-close").click();await left()
   const proof=await snapshot()
   assert.equal(proof.writes.filter(w=>w.body.person===false).length,5)
   assert.ok(proof.writes.every(w=>w.body.tab===9&&w.body.browser_mode==="user_chrome"),"foreign tabs stay untouched")
   assert.deepEqual(errors,[]);assert.deepEqual(api,[],"the synthetic fixture never reaches a live daemon")
   results.push({engine:engine.name(),language,width,tones:proof.tones,handbacks:5,exitFailure:true,inflightTakeover:true,strictMode:true})
  }catch(error){if(directory)await page.screenshot({path:join(directory,`FAIL-feedback-${engine.name()}-${language}.png`)});throw error}
  finally{await browser.close()}
 }
 if(directory)await writeFile(join(directory,"feedback-results.json"),JSON.stringify(results,null,2)+"\n")
 console.log(JSON.stringify(results))
})
