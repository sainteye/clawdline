// Real hook, shared Lightbox and Blob lifetime in Chromium and WebKit; synthetic HTTP only.
import { test } from "node:test"
import assert from "node:assert/strict"
import { spawn } from "node:child_process"
import { existsSync, readFileSync, writeFileSync, unlinkSync } from "node:fs"
import { fileURLToPath } from "node:url"
import { resolve } from "node:path"
import { once } from "node:events"

const web = fileURLToPath(new URL("../../../../", import.meta.url))
const fixture = resolve(web, "console/reference-preview-fixture.html")
const entry = resolve(web, "console/reference-preview-fixture.tsx")
const picture = readFileSync(resolve(web, "console/public/icon-192.png"))

test("reference images share the existing viewer and retain only their own live bytes", async () => {
  const { chromium, webkit } = await import(process.env.PLAYWRIGHT_MODULE || "playwright")
  assert.ok(!existsSync(fixture) && !existsSync(entry))
  let vite
  const browsers = []
  try {
    writeFileSync(fixture, '<!doctype html><meta name="viewport" content="width=device-width,initial-scale=1"><div id="root"></div><script type="module" src="/reference-preview-fixture.tsx"></script>')
    writeFileSync(entry, `
import React from 'react';import {createRoot} from 'react-dom/client';
import {useReferenceImage} from './src/pages/work/useReferenceImage';import {bootCatalog} from './src/catalog';
import './src/legacy/tokens.css';import './src/legacy/base.css';import './src/legacy/transcript.css';
localStorage.setItem('ui_language',new URLSearchParams(location.search).get('lang')||'en');
await bootCatalog(tag=>'/catalogs/'+tag+'.json');
window.popupCalls=0;window.open=()=>{window.popupCalls++;return null};window.revoked=[];
const revoke=URL.revokeObjectURL.bind(URL);URL.revokeObjectURL=url=>{window.revoked.push(url);revoke(url)};
function Card({id}){const {source,fullFailed,opening,openFull}=useReferenceImage(id,id+'.png');return <section id={id}>
<button type="button" disabled={!source||opening} onClick={openFull}>{id}</button><p role="status">{fullFailed}</p></section>}
function Fixture(){const [ids,setIds]=React.useState(['alpha','beta','late']);window.remove=id=>setIds(all=>all.filter(value=>value!==id));return ids.map(id=><Card key={id} id={id}/>)}
createRoot(document.querySelector('#root')).render(<React.StrictMode><Fixture/></React.StrictMode>);
`)
    vite = spawn(process.execPath, [resolve(web, "node_modules/vite/bin/vite.js"), "--host", "127.0.0.1", "--port", "0"],
      { cwd: resolve(web, "console"), env: { ...process.env, CLAWDLINE_NEXT_ORIGIN: "http://127.0.0.1:1" }, stdio: ["ignore", "pipe", "pipe"] })
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
      for (const lang of ["en", "zh-Hant"]) for (const width of [390, 1280]) {
        const page = await browser.newPage({ viewport: { width, height: 844 } })
        const errors = [], reads = []
        let fail = true, releaseLate
        const late = new Promise(resolve => { releaseLate = resolve })
        page.on("pageerror", error => errors.push(String(error)))
        await page.route("**/v1/**", async route => {
          const url = new URL(route.request().url())
          if (!url.pathname.startsWith('/v1/work/v2/images/')) return route.fulfill({ json: {} })
          const full = !url.searchParams.has('size')
          if (full) reads.push(url.pathname.split('/').at(-1))
          if (full && url.pathname.endsWith('/alpha') && fail) { fail = false; return route.fulfill({status:503,json:{error:{code:'fixture_unavailable',message:'Synthetic failure'}}}) }
          if (full && url.pathname.endsWith('/late')) await late
          await route.fulfill({contentType:'image/png',body:picture}).catch(() => {})
        })
        try {
          await page.goto(origin + '/reference-preview-fixture.html?lang=' + lang)
          await page.waitForFunction(() => document.querySelector('#alpha button')?.disabled === false)
          assert.deepEqual(reads, [], 'opening the page reads no originals')
          const shown = () => page.waitForFunction(() => !document.querySelector('#image-lightbox')?.hidden && document.querySelector('#image-lightbox-image')?.naturalWidth > 0)
          const close = async () => { await page.keyboard.press('Escape'); await page.waitForFunction(() => document.querySelector('#image-lightbox').hidden) }
          await page.locator('#alpha button').click()
          await page.waitForFunction(() => document.querySelector('#alpha [role=status]').textContent.length > 0)
          assert.equal(await page.locator('#image-lightbox').count(), 0)
          await page.locator('#alpha button').click(); await shown()
          assert.deepEqual(reads, ['alpha','alpha'], 'a failed original can be retried')
          const alpha = await page.locator('#image-lightbox-image').getAttribute('src')
          await page.locator('#image-lightbox-image').dblclick()
          assert.equal(await page.locator('#image-lightbox-image').getAttribute('data-zoomed'), 'true')
          await close()
          assert.equal(await page.evaluate(() => document.activeElement.closest('section')?.id), 'alpha')
          await page.locator('#alpha button').click(); await shown()
          assert.deepEqual(reads, ['alpha','alpha'], 'reopening a cached original sends no new read')
          await page.evaluate(() => document.querySelector('#beta button').click()); await shown()
          await page.waitForFunction(previous => document.querySelector('#image-lightbox-image').src !== previous, alpha)
          const beta = await page.locator('#image-lightbox-image').getAttribute('src')
          await page.evaluate(() => window.remove('alpha'))
          await page.waitForFunction(url => window.revoked.includes(url), alpha)
          assert.equal(await page.locator('#image-lightbox').evaluate(node => node.hidden), false, 'removing an old owner must not close a newer image')
          await page.evaluate(() => window.remove('beta'))
          await page.waitForFunction(url => window.revoked.includes(url) && document.querySelector('#image-lightbox').hidden, beta)
          await page.locator('#late button').click()
          await page.waitForFunction(() => document.querySelector('#late button').disabled)
          await page.evaluate(() => window.remove('late'))
          releaseLate()
          await page.waitForLoadState('networkidle')
          assert.equal(await page.locator('#image-lightbox').evaluate(node => node.hidden), true, 'a late read cannot reopen a removed reference')
          assert.equal(await page.evaluate(() => window.popupCalls), 0)
          assert.deepEqual(errors, [], engine.name() + ' ' + lang + ' ' + width)
        } finally { releaseLate(); await page.close() }
      }
    }
  } finally {
    for (const browser of browsers) await browser.close()
    if (vite) { const stopped = once(vite, "exit"); vite.kill(); await stopped }
    for (const file of [fixture, entry]) if (existsSync(file)) unlinkSync(file)
  }
})
