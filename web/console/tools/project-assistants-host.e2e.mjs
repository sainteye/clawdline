/** Real App + Sessions + existing voice owner. All data is synthetic and all /v1 is intercepted. */
import assert from "node:assert/strict"
import { createServer } from "node:http"
import { readFile, mkdir, writeFile } from "node:fs/promises"
import path from "node:path"
import { fileURLToPath, pathToFileURL } from "node:url"
import { build } from "esbuild"

export async function hostBundle() {
  const src = fileURLToPath(new URL("../src/",import.meta.url))
  const html = await readFile(new URL("fixtures/project-assistants-host.html",import.meta.url),"utf8")
  const contents = /<script type="text\/fixture-tsx">([\s\S]*?)<\/script>/.exec(html)?.[1]
  if (!contents) throw new Error("Missing App fixture entry")
  const result = await build({stdin:{contents,resolveDir:src,loader:"tsx"},outfile:"host.js",bundle:true,write:false,
    format:"esm",platform:"browser",jsx:"automatic",external:["/hero-orchestration-v4-task-clinic.webp"],define:{"process.env.NODE_ENV":'"development"',"import.meta.env":"{}"},
    plugins:[{name:"fixture-page-registry",setup(build){
      build.onResolve({filter:/\?raw$/},args => ({path:path.resolve(args.resolveDir,args.path.replace(/\?raw$/,"")),namespace:"fixture-raw"}))
      build.onLoad({filter:/.*/,namespace:"fixture-raw"},async args => ({loader:"text",contents:await readFile(args.path,"utf8")}))
      build.onLoad({filter:/\/App\.tsx$/},async args => ({loader:"tsx",resolveDir:src,contents:
      (await readFile(args.path,"utf8")).replace(/import\.meta\.glob<\{ page\?: PageModule \}>\("\.\/pages\/\*\.tsx", \{ eager: true \}\)/,
        '{"./pages/project-assistants.tsx":{page:assistantFixturePage},"./pages/work.tsx":{page:workFixturePage}}')+'\nimport {page as assistantFixturePage} from "./pages/project-assistants";import {page as workFixturePage} from "./pages/work"\n'}))}}]})
  return {html:html.replace(/<script type="text\/fixture-tsx">[\s\S]*?<\/script>/,""),files:new Map(result.outputFiles.map(file => ["/"+path.basename(file.path),file.text]))}
}

export async function runHostAcceptance({artifacts,playwrightModule,engines=["chromium","webkit"],widths=[320,390,1440],languages=["en","zh-Hant"]}) {
  await mkdir(artifacts,{recursive:true})
  const {html,files} = await hostBundle(), playwright = await import(pathToFileURL(playwrightModule).href)
  const catalogDir = new URL("../public/catalogs/",import.meta.url)
  const network = [], results = []
  const server = createServer(async (request,response) => {
    try {
      const url = new URL(request.url,"http://fixture")
      if (url.pathname.startsWith("/v1/")) {network.push(url.pathname);response.writeHead(500);response.end("Fixture forbids real API");return}
      let body = url.pathname === "/" ? html : files.get(url.pathname)
      if (/^\/catalogs\/(en|zh-Hant)\.json$/.test(url.pathname)) body = await readFile(new URL(path.basename(url.pathname),catalogDir),"utf8")
      if (body === undefined) {response.writeHead(404);response.end();return}
      response.setHeader("Content-Type",url.pathname.endsWith(".js") ? "text/javascript" : url.pathname.endsWith(".css") ? "text/css" : url.pathname.endsWith(".json") ? "application/json" : "text/html")
      response.end(body)
    } catch (error) {response.writeHead(500);response.end(String(error))}
  })
  await new Promise(resolve => server.listen(0,"127.0.0.1",resolve))
  const origin = `http://127.0.0.1:${server.address().port}`
  try {
    for (const engine of engines) for (const width of widths) for (const lang of languages) {
      const browser = await playwright[engine].launch({headless:true})
      const name = `${engine}-${width}-${lang}`, errors = [], page = await browser.newPage({viewport:{width,height:900},reducedMotion:"reduce"})
      page.on("pageerror",error => errors.push(error.message))
      const w = lang === "en" ? {all:"All Bots",message:"Message",back:"Back to Bots",sessions:"Back to Sessions",select:"Select a Bot to start reading",openWork:"Open work",refresh:"Check for updates"} : {all:"所有 Bots",message:"訊息",back:"返回 Bots",sessions:"返回 Sessions",select:"選擇一位 Bot 開始閱讀",openWork:"查看任務",refresh:"檢查新訊息"}
      const row = (machine="studio",bot="Project assistant") => page.getByRole("button",{name:new RegExp(`^${bot}, ${machine},`)})
      const back = () => width > 700 ? page.evaluate(() => history.back()) : page.getByRole("button",{name:w.back,exact:true}).click()
      const shot = code => page.screenshot({path:path.join(artifacts,`${code}-${name}.png`),fullPage:true})
      try {
        await page.route("**/v1/**",route => {network.push(route.request().url());return route.abort()})
        await page.goto(origin+"/?lang="+lang)
        const orb = page.getByRole("button",{name:new RegExp("^"+w.all)})
        await orb.waitFor()
        await page.locator("#filter").waitFor()
        const paneBox=await page.locator("#app .pane-list").boundingBox(),orbBox=await orb.boundingBox()
        assert.ok(paneBox&&orbBox&&orbBox.x>=paneBox.x&&orbBox.y>=paneBox.y&&orbBox.x+orbBox.width<=paneBox.x+paneBox.width&&orbBox.y+orbBox.height<=paneBox.y+paneBox.height,
          "The actual orb stays inside the Session-list pane without fixture position overrides")
        await page.evaluate(() => window.fixtureHost.beginSyntheticCall())
        await page.locator(".copilot-bubble").waitFor()
        // Real Session voice controls are retained as DOM nodes through every flip.
        await page.evaluate(() => {window.savedVoice=document.querySelector("#voice-go");window.savedDock=document.querySelector(".copilot-bubble");window.savedApp=document.querySelector("#app");document.querySelector("#list-scroll").scrollTop=420})
        await shot("session-entry")
        const originalHash = await page.evaluate(() => location.hash), historyLength = await page.evaluate(() => history.length)
        await orb.click();await row().waitFor();await row("notebook").waitFor()
        if (width > 700) await page.getByText(w.select,{exact:true}).waitFor()
        assert.equal(await page.evaluate(() => history.length),historyLength+1)
        assert.equal(await page.evaluate(() => location.hash),originalHash)
        await shot("all-sources")
        await row().click()
        const editor = page.getByRole("textbox",{name:w.message,exact:true});await editor.waitFor()
        await editor.fill("source-one draft")
        if (width > 700) {
          await row("notebook").click();await editor.waitFor()
          assert.equal(await page.evaluate(() => history.length),historyLength+2,"desktop Bot switch replaces the single chat entry")
        }
        await page.evaluate(() => history.back())
        await editor.waitFor({state:"hidden"});await row().waitFor();assert.equal(await editor.count(),0)
        await row("notebook").click();await editor.waitFor();await editor.fill("source-two draft")
        await editor.evaluate(node => node.dispatchEvent(new CompositionEvent("compositionstart",{bubbles:true,data:"中"})))
        await editor.press("Enter")
        assert.equal(await page.evaluate(() => window.fixtureHost.log.filter(row => row.method === "send").length),0)
        await editor.evaluate(node => node.dispatchEvent(new CompositionEvent("compositionend",{bubbles:true,data:"中"})))
        await back();await row().waitFor()
        await row().click();await editor.waitFor();assert.equal(await editor.inputValue(),"source-one draft")
        await shot("source-chat")
        assert.equal(await page.evaluate(() => savedVoice === document.querySelector("#voice-go") && savedApp === document.querySelector("#app")),true)
        assert.equal(await page.evaluate(() => savedDock === document.querySelector(".copilot-bubble") && window.fixtureHost.voiceState().status === "connected"),true)
        // Existing CopilotDock opens and closes its real modal without starting a call.
        await page.evaluate(() => window.fixtureHost.showVoice())
        await page.locator(".realtime-modal-sheet").waitFor()
        await page.evaluate(() => window.fixtureHost.closeVoice())
        await page.locator(".realtime-modal-sheet").waitFor({state:"hidden"})
        await page.locator(".copilot-bubble").waitFor()
        await back();await row().waitFor()
        await page.getByRole("button",{name:w.sessions,exact:true}).click();await orb.waitFor()
        assert.equal(await page.evaluate(() => document.activeElement?.classList.contains("pa-orb")),true)
        assert.equal(await page.evaluate(() => document.querySelector("#list-scroll").scrollTop),420)
        assert.equal(await page.evaluate(() => location.hash),originalHash)
        // App remount is CloudGate's active-machine key; the account-owned draft survives.
        await page.evaluate(() => window.fixtureHost.remount());await orb.waitFor();await orb.click();await row().click();await editor.waitFor()
        assert.equal(await editor.inputValue(),"source-one draft")
        const beforeUnsupported = await page.evaluate(() => window.fixtureHost.log.filter(row => row.machine === "studio" && row.method === "get").length)
        await page.evaluate(() => window.fixtureHost.unsupported())
        await page.getByRole("button",{name:w.refresh,exact:true}).click()
        await page.getByText(/pa_unsupported/).first().waitFor()
        assert.equal(await editor.isDisabled(),true)
        assert.equal(await page.evaluate(() => window.fixtureHost.log.filter(row => row.machine === "studio" && row.method === "get").length),beforeUnsupported)
        await shot("unsupported-source")
        await page.evaluate(() => window.fixtureHost.readOnly());await row().waitFor();await row().click();await editor.waitFor()
        assert.equal(await editor.isDisabled(),true)
        await shot("history-only")
        await page.evaluate(() => window.fixtureHost.offline())
        await page.getByRole("button",{name:w.refresh,exact:true}).click()
        await page.getByText(/pa_offline/).first().waitFor()
        assert.equal(await editor.inputValue(),"source-one draft")
        assert.equal(await editor.isDisabled(),true)
        await shot("offline-history")
        await page.evaluate(() => window.fixtureHost.revoke())
        await row("notebook").waitFor();assert.equal(await row().count(),0)
        assert.equal(await editor.count(),0)
        await shot("revoked-source")
        await row("notebook").click();await editor.waitFor()
        await page.getByRole("button",{name:new RegExp("^"+w.openWork)}).click()
        await page.getByRole("dialog").filter({has:page.getByText("Board result notebook",{exact:true})}).waitFor()
        assert.equal(await page.evaluate(() => window.fixtureHost.stats().active),"notebook")
        assert.equal(await page.evaluate(() => window.fixtureHost.log.filter(row => row.method === "workItem").at(-1).machine),"notebook")
        assert.equal(await page.locator('[data-page-view="project-assistants"]').count(),0)
        await shot("board-after-source-remount")
        const stats = await page.evaluate(() => window.fixtureHost.stats())
        assert.equal(stats.mediaOpens,0);assert.equal(stats.sessionSends,0)
        const requests = await page.evaluate(() => window.fixtureHost.requests)
        assert.equal(requests.filter(row => /voice|copilot|realtime/.test(row.path) && row.method !== "GET").length,0)
        assert.equal(errors.length,0,errors.join("\n"))
        // A fresh never-enabled account performs metadata only and has no entry.
        await page.goto(origin+"/?lang="+lang+"&feature=off")
        await page.locator("#filter").waitFor()
        await page.waitForFunction(() => window.fixtureHost.log.filter(row => row.method === "status").length >= 2)
        assert.equal(await orb.count(),0)
        assert.equal(await page.evaluate(() => window.fixtureHost.log.filter(row => row.method === "list").length),0)
        await shot("default-hidden")
        results.push({name,passed:true,stats,requests,placement:{pane:paneBox,orb:orbBox,inside:true,fixturePositionOverride:false}})
      } catch (error) {await shot("failure");results.push({name,passed:false,error:String(error),errors});throw error}
      finally {await page.close();await browser.close()}
    }
    assert.equal(network.length,0,"No /v1 request escapes the synthetic fixture")
  } finally {await new Promise(resolve => server.close(resolve));await writeFile(path.join(artifacts,"host-results.json"),JSON.stringify({results,network},null,2)+"\n")}
  return results
}

if (process.argv[1] && fileURLToPath(import.meta.url) === path.resolve(process.argv[1])) {
  if (!process.env.BOTS_HOST_ARTIFACTS || !process.env.PLAYWRIGHT_MODULE) throw new Error("Set BOTS_HOST_ARTIFACTS and existing PLAYWRIGHT_MODULE; no installation occurs")
  const results = await runHostAcceptance({artifacts:process.env.BOTS_HOST_ARTIFACTS,playwrightModule:process.env.PLAYWRIGHT_MODULE,
    ...(process.env.BOTS_HOST_SMOKE ? {engines:["chromium"],widths:[390],languages:["en"]} : {})})
  console.log(`App host: ${results.length} actual browser cases passed`)
}
