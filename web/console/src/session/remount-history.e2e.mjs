// Run directly with PLAYWRIGHT_MODULE pointing to an installed Playwright.
// The actual App is unmounted behind an opening screen and restored in the
// same document; no daemon, Cloud account or real conversation is used.
import { test } from "node:test"
import assert from "node:assert/strict"
import { spawn } from "node:child_process"
import { existsSync, readFileSync, writeFileSync, unlinkSync } from "node:fs"
import { fileURLToPath } from "node:url"
import { resolve } from "node:path"
import { once } from "node:events"

const root = fileURLToPath(new URL("../../../../", import.meta.url))
const fixture = resolve(root, "web/console/remount-history-fixture.html")
const entry = resolve(root, "web/console/remount-history-fixture.tsx")
const catalog = tag => JSON.parse(readFileSync(resolve(root, "web/console/public/catalogs/" + tag + ".json"), "utf8"))

test("a phone Session keeps Back after an opening screen remounts the view without reloading the document", async () => {
  const { chromium, webkit } = await import(process.env.PLAYWRIGHT_MODULE || "playwright")
  assert.ok(!existsSync(fixture) && !existsSync(entry), "fixture paths must be unused")
  let vite
  const browsers = []
  try {
    const styles = [...readFileSync(resolve(root, "web/console/src/main.tsx"), "utf8").matchAll(/import "(\.\/[^"\n]+\.css)"/g)]
      .map(match => 'import "' + match[1].replace("./", "./src/") + '";').join("\n")
    writeFileSync(fixture, '<!doctype html><meta name="viewport" content="width=device-width,initial-scale=1"><div id="root"></div><script type="module" src="/remount-history-fixture.tsx"></script>')
    writeFileSync(entry, `import React,{StrictMode} from 'react';import {createRoot} from 'react-dom/client';import {flushSync} from 'react-dom';
import App from './src/App';${styles}
const root=createRoot(document.getElementById('root'));let generation=0;
const show=()=>flushSync(()=>root.render(<StrictMode><App key={generation++}/></StrictMode>));
window.fixture={hide:()=>flushSync(()=>root.render(<div role="status">Opening fixture</div>)),show,documentToken:crypto.randomUUID()};show();`)
    vite = spawn(process.execPath, [resolve(root, "web/node_modules/vite/bin/vite.js"), "--host", "127.0.0.1", "--port", "0"],
      { cwd: resolve(root, "web/console"), stdio: ["ignore", "pipe", "pipe"] })
    const origin = await new Promise((yes, no) => {
      let output = ""
      const timer = setTimeout(() => no(new Error("Vite startup timed out")), 20000)
      vite.stdout.on("data", data => { output += data; const match = output.match(/http:\/\/127\.0\.0\.1:\d+/); if (match) { clearTimeout(timer); yes(match[0]) } })
      vite.stderr.on("data", data => process.stderr.write(data))
      vite.once("exit", code => { clearTimeout(timer); no(new Error("Vite exited: " + code)) })
    })
    for (const engine of [chromium, webkit]) {
      const browser = await engine.launch({ headless: true, ...(engine === chromium ? {
        executablePath: process.env.CHROME || "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" } : {}) })
      browsers.push(browser)
      for (const lang of ["en", "zh-Hant"]) {
        const page = await browser.newPage({ viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true })
        const errors = []; page.on("pageerror", error => errors.push(String(error)))
        await page.addInitScript(tag => localStorage.setItem('ui_language', tag), lang)
        let sequence = 0
        const row = {id:'fixture-session',label:'Fixture Session',backend:'owned',state:'idle',work_state:'ready',evidence:'process',isClaude:true,sessionId:'fixture-conversation',cwd:'/tmp/fixture'}
        await page.route("**/v1/**", route => {
          const url = new URL(route.request().url())
          if (url.pathname === '/v1/strings') return route.fulfill({json:catalog(url.searchParams.get('lang') || lang)})
          if (url.pathname === '/v1/health') return route.fulfill({json:{authed:true,version:'fixture',api_level:99}})
          if (url.pathname === '/v1/sessions') return route.fulfill({json:{at:Date.now(),scan:{complete:true,completed:{complete:true,sequence:++sequence},emptyAuthoritative:true,epoch:1,generation:sequence,provenance:'fixture'},sessions:[row]}})
          if (url.pathname === '/v1/transcript') return route.fulfill({json:{entries:[],evidence:'process',id:row.id,signature:'fixture'}})
          return route.fulfill({status:404,json:{error:{code:'not_found',message:'Fixture route unavailable'}}})
        })
        await page.goto(origin + '/remount-history-fixture.html')
        await page.locator('#rows .row').click()
        const detail = () => page.waitForFunction(() => document.querySelector('#app')?.dataset.view === 'detail' && document.querySelector('#rows .row.open'))
        const list = () => page.waitForFunction(() => document.querySelector('#app')?.dataset.view === 'list' && !document.querySelector('#rows .row.open'), {}, {timeout:5000})
        await detail()
        const token = await page.evaluate(() => window.fixture.documentToken)
        // Use the real modal's reusable close step before the opening screen.
        await page.evaluate(() => {const link=document.createElement('a');link.href='/#document=1&machine=this-mac&session=fixture-session&scope=project&path=fixture.mp4';document.body.append(link);link.click();document.querySelector('.video-lightbox-close').click();link.remove()})
        assert.ok(await page.evaluate(() => history.state?.videoPreviewClosed))
        await page.evaluate(() => window.fixture.hide())
        await page.getByRole('status', {name:''}).filter({hasText:'Opening fixture'}).waitFor()
        await page.evaluate(() => window.fixture.show())
        await detail()
        assert.equal(await page.evaluate(() => window.fixture.documentToken), token, 'the document has not reloaded')
        const steps = await page.evaluate(() => history.length)
        await page.evaluate(() => history.back());await list()
        await page.evaluate(() => history.forward());await detail()
        assert.equal(await page.evaluate(() => history.length), steps, 'Forward does not grow history')
        assert.deepEqual(errors, [], 'the remounted App has no browser exceptions')
        console.log(engine.name(), lang, 'same-document opening-screen recovery and Back/Forward passed')
        await page.close()
      }
    }
  } finally {
    for (const browser of browsers) await browser.close()
    if (vite && vite.exitCode === null) { vite.kill(); await once(vite, "exit") }
    for (const path of [fixture, entry]) if (existsSync(path)) unlinkSync(path)
  }
})
