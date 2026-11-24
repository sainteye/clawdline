// A push link opens the Bot chat at its decision card; the reply then shows
// working and done. Screenshots at 390×844 (zh-Hant) and 1280×800 (en).
import assert from "node:assert/strict"
import {createServer} from "node:http"
import {mkdir,readFile} from "node:fs/promises"
import path from "node:path"
import {pathToFileURL} from "node:url"
import {build} from "esbuild"

const {PLAYWRIGHT_MODULE,BOTS_ARTIFACTS,CHROME}=process.env
assert.ok(PLAYWRIGHT_MODULE&&BOTS_ARTIFACTS,"Set PLAYWRIGHT_MODULE and BOTS_ARTIFACTS")
const {chromium}=await import(pathToFileURL(PLAYWRIGHT_MODULE).href)
await mkdir(BOTS_ARTIFACTS,{recursive:true})
const bundle=await build({entryPoints:[new URL("../src/project-assistants/fixtures.ts",import.meta.url).pathname],
  outfile:"project-assistants-fixture.js",write:false,bundle:true,format:"esm",platform:"browser",jsx:"automatic",define:{"process.env.NODE_ENV":'"development"'}})
const files=new Map(bundle.outputFiles.map(file=>["/"+path.basename(file.path),file.contents]))
const html=await readFile(new URL("fixtures/project-assistants.html",import.meta.url))
const catalogs=new Map(await Promise.all(["en","zh-Hant"].map(async tag=>[`/catalog/${tag}.json`,await readFile(new URL(`../public/catalogs/${tag}.json`,import.meta.url))])))
const server=createServer((req,res)=>{
  const {pathname}=new URL(req.url,"http://fixture"),file=files.get(pathname)
  if(file){res.setHeader("Content-Type",pathname.endsWith(".css")?"text/css":"text/javascript");res.end(file);return}
  if(catalogs.has(pathname)){res.setHeader("Content-Type","application/json");res.end(catalogs.get(pathname));return}
  if(pathname==="/"){res.setHeader("Content-Type","text/html");res.end(html);return}
  res.writeHead(404).end()
})
await new Promise((resolve,reject)=>server.once("error",reject).listen(0,"127.0.0.1",resolve))
const origin=`http://127.0.0.1:${server.address().port}`
let browser
try{
  browser=await chromium.launch({headless:true,executablePath:CHROME,args:["--disable-features=OmniboxEverywhere"]})
  for(const [language,width,height,scheme] of [["zh-Hant",390,844,"light"],["en",1280,800,"dark"]]){
    const context=await browser.newContext({viewport:{width,height},colorScheme:scheme})
    try{
      const page=await context.newPage()
      page.setDefaultTimeout(10000)
      const errors=[],viewing=[]
      page.on("pageerror",e=>errors.push(e.message))
      await page.route("**/*",route=>{const u=new URL(route.request().url());if(u.pathname.endsWith("/viewing")){viewing.push(route.request().postData());return route.fulfill({status:200,body:'{"ok":true}',contentType:"application/json"})}if(u.origin!==origin||u.pathname.startsWith("/v1"))return route.abort();return route.continue()})
      await page.addInitScript(tag=>localStorage.setItem("ui_language",tag),language)
      const shot=name=>page.screenshot({path:path.join(BOTS_ARTIFACTS,`decision-${name}-${width}.png`)})
      await page.goto(origin+"/?lang="+language+"#bot=bot-0&conversation=chat-0&machine=studio&decision=decision-7")
      await page.waitForFunction(()=>!!window.botsFixture)
      await page.evaluate(()=>{window.botsFixture.a.decisionFixture(40);window.botsFixture.open()})
      const card=page.locator("#pa-decision-decision-7")
      await card.waitFor()
      await page.waitForFunction(()=>document.querySelector("#pa-decision-decision-7")?.hasAttribute("data-focused"))
      await page.waitForTimeout(400)
      const box=await card.boundingBox()
      assert.ok(box&&box.y>=0&&box.y+box.height<=height+1,`card is in view: ${JSON.stringify(box)}`)
      await shot("opened-from-notification")
      await page.getByRole("button",{name:"Ship it now"}).click()
      await page.locator('#pa-decision-decision-7[data-state="answered"]').getByText("Ship it now",{exact:false}).waitFor()
      await page.evaluate(()=>window.botsFixture.a.workTurn("running"))
      await page.locator('#pa-decision-decision-7[data-follow-up="working"]').waitFor()
      await shot("replied-working")
      await page.evaluate(()=>window.botsFixture.a.workTurn("succeeded"))
      await page.locator('#pa-decision-decision-7[data-follow-up="done"]').waitFor()
      await shot("done")
      await page.goto(origin+"/?lang="+language)
      await page.waitForFunction(()=>!!window.botsFixture)
      await page.evaluate(()=>{window.botsFixture.a.decisionFixture(40);window.botsFixture.open()})
      await page.getByRole("button",{name:/Merge and deploy this release now/}).click()
      const reply=page.locator('#pa-decision-decision-7[data-state="pending"]').getByRole("button",{name:"Ship it now"})
      await reply.waitFor()
      await page.waitForFunction(()=>{const reply=document.querySelector("#pa-decision-decision-7 button");if(!reply||reply.disabled)return false;const box=reply.getBoundingClientRect();return box.top>=0&&box.bottom<=innerHeight})
      await shot("opened-from-roster")
      const scroll=await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)
      assert.ok(scroll,"no horizontal page scroll")
      assert.deepEqual(errors,[])
      console.log(`${language} ${width}x${height} ${scheme}: focused, working, done, reply visible from roster; viewing reports ${JSON.stringify(viewing.slice(0,2))}`)
    }finally{await context.close()}
  }
  console.log("decision focus: ALL PASS")
}finally{await browser?.close();server.close()}
