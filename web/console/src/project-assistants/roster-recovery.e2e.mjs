/** Local browser evidence for automatic and manual recovery on both roster transports. */
import assert from "node:assert/strict"
import {createServer} from "node:http"
import {mkdir,writeFile} from "node:fs/promises"
import {fileURLToPath,pathToFileURL} from "node:url"
import path from "node:path"
import {build} from "esbuild"
const artifacts=process.env.BOTS_ARTIFACTS
if(!artifacts||!process.env.PLAYWRIGHT_MODULE)throw Error("Set BOTS_ARTIFACTS and an installed PLAYWRIGHT_MODULE")
await mkdir(artifacts,{recursive:true})
const {chromium,webkit}=await import(pathToFileURL(process.env.PLAYWRIGHT_MODULE).href)
const plugins=process.env.BOTS_RECOVERY_BASELINE?[{name:"original-roster",setup(build){build.onLoad({filter:/\/(Bots|BotRoster|live-roster|source-errors)\.tsx?$/},async args=>{
  const {readFile}=await import("node:fs/promises")
  return {contents:await readFile(path.join(process.env.BOTS_RECOVERY_BASELINE,path.basename(args.path)),"utf8"),loader:args.path.endsWith("tsx")?"tsx":"ts",resolveDir:path.dirname(args.path)}
})}}]:[]
const built=await build({plugins,entryPoints:[fileURLToPath(new URL("./roster-recovery.fixture.tsx",import.meta.url))],bundle:true,write:false,outfile:"roster.js",format:"esm",platform:"browser",jsx:"automatic",define:{"process.env.NODE_ENV":'"development"'}})
const files=new Map(built.outputFiles.map(f=>["/"+path.basename(f.path),f.contents]))
let forbidden=0
const server=createServer((req,res)=>{
  const url=new URL(req.url,"http://fixture")
  if(url.pathname.startsWith("/v1/")){forbidden++;res.writeHead(500);res.end("No live API");return}
  if(files.has(url.pathname)){res.setHeader("Content-Type",url.pathname.endsWith(".css")?"text/css":"text/javascript");res.end(files.get(url.pathname));return}
  res.setHeader("Content-Type","text/html");res.end('<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><style>body{margin:0;font:15px system-ui;background:#080808;color:#fafafa}#app{height:100dvh}</style><link rel="stylesheet" href="/roster.css"><div id="app"></div><script type="module" src="/roster.js"></script>')
})
await new Promise(r=>server.listen(0,"127.0.0.1",r))
const origin=`http://127.0.0.1:${server.address().port}`,results=[]
try {
  for(const engine of [chromium,webkit])for(const [language,width] of [["zh-Hant",390],["en",1280]]) {
    const browser=await engine.launch({headless:true}),page=await browser.newPage({viewport:{width,height:844}})
    const errors=[];page.on("pageerror",error=>errors.push(error.message))
    const name=`${engine.name()}-${language}-${width}`
    try {
      await page.goto(origin+"/?lang="+language)
      const card=id=>page.locator(".pa-connection-notice").filter({has:page.locator("strong",{hasText:id})})
      const retry=id=>card(id).getByRole("button")
      await page.waitForFunction(()=>document.querySelectorAll(".pa-source-error").length===2)
      if(process.env.BOTS_RECOVERY_BASELINE){
        assert.equal(await page.locator(".pa-source-error button").count(),2,"offline sources need manual retry controls")
        continue
      }
      assert.equal(await page.locator(".pa-connection-notice").count(),2)
      assert.equal(await page.getByText("pa_offline",{exact:true}).first().isVisible(),false,"raw errors are collapsed")
      assert.match(await page.locator(".pa-roster-filter").innerText(),language==="en"?/Connecting/:/正在連線/)
      assert.equal(await page.locator(".pa-roster-filter").innerText().then(s=>/Bots\s+0/.test(s)),false)
      assert.equal(await page.locator(".pa-connection-notice").first().evaluate(n=>getComputedStyle(n).color),"rgb(153, 153, 159)")
      await page.screenshot({path:path.join(artifacts,`${name}-offline.png`)})
      await card("studio").locator("summary").click()
      assert.equal(await card("studio").getByText("pa_offline",{exact:true}).isVisible(),true)
      await page.evaluate(()=>{window.rosterRecovery.studio.setCode(null);window.rosterRecovery.studio.hold()})
      await retry("studio").click()
      await page.waitForFunction(()=>window.rosterRecovery.studio.snapshot().active===1)
      assert.equal(await retry("studio").isDisabled(),true)
      await retry("studio").evaluate(node=>{node.click();node.click()})
      await page.evaluate(()=>window.rosterRecovery.studio.release())
      await card("studio").waitFor({state:"detached"})
      assert.equal(await page.locator("[data-bot-key]").count(),3)
      // The other machine recovers at the existing cadence without any click.
      await page.evaluate(()=>window.rosterRecovery.notebook.setCode(null))
      await card("notebook").waitFor({state:"detached",timeout:10000})
      assert.equal(await page.locator("[data-bot-key]").count(),6)
      await page.evaluate(()=>window.rosterRecovery.studio.setCode("pa_offline"))
      await card("studio").waitFor()
      assert.equal(await page.locator("[data-bot-key]").count(),6,"offline observations remain in the roster")
      await page.screenshot({path:path.join(artifacts,`${name}-retained.png`)})
      await page.evaluate(()=>window.rosterRecovery.studio.setCode(null));await retry("studio").click()
      await card("studio").waitFor({state:"detached"})
      await page.evaluate(()=>window.rosterRecovery.notebook.setCode("pa_offline"))
      await card("notebook").waitFor({timeout:10000})
      await page.evaluate(()=>{window.rosterRecovery.notebook.setCode(null);window.rosterRecovery.notebook.hold()})
      await retry("notebook").click();await page.waitForFunction(()=>window.rosterRecovery.notebook.snapshot().active===1)
      assert.equal(await retry("notebook").isDisabled(),true)
      await page.evaluate(()=>window.rosterRecovery.notebook.release());await card("notebook").waitFor({state:"detached"})
      await page.evaluate(()=>window.rosterRecovery.studio.setCode("pa_revoked"))
      await card("studio").waitFor()
      assert.equal(await retry("studio").count(),0,"revoked authority is never retried")
      assert.equal(await page.locator("[data-bot-key]").count(),3,"revocation clears only the affected source")
      const proof=await page.evaluate(()=>({studio:window.rosterRecovery.studio.snapshot(),notebook:window.rosterRecovery.notebook.snapshot()}))
      assert.equal(proof.studio.maxActive,1);assert.equal(proof.studio.maxStreams,1);assert.equal(proof.notebook.maxActive,1)
      assert.equal(proof.studio.cursors[1],"fixture-cursor","manual reconnect preserves the applied cursor")
      assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true)
      assert.deepEqual(errors,[])
      results.push({name,...proof})
    }catch(error){await page.screenshot({path:path.join(artifacts,`${name}-FAIL.png`)});throw error}
    finally{await browser.close()}
  }
  assert.equal(forbidden,0)
}finally{server.closeAllConnections();await new Promise(r=>server.close(r));await writeFile(path.join(artifacts,"roster-recovery-results.json"),JSON.stringify({results,forbidden},null,2)+"\n")}
console.log(JSON.stringify({results,forbidden}))
