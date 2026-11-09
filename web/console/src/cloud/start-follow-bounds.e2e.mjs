// Synthetic launch only: every /v1 request is intercepted before reaching a daemon.
import { test } from "node:test"
import assert from "node:assert/strict"
import { spawn } from "node:child_process"
import { existsSync, writeFileSync, unlinkSync } from "node:fs"
import { fileURLToPath } from "node:url"
import { resolve } from "node:path"
import { once } from "node:events"

const root = fileURLToPath(new URL("../../../../", import.meta.url))
const fixture = resolve(root, "web/console/start-follow-fixture.html")
const entry = resolve(root, "web/console/start-follow-fixture.tsx")

test("a confirmed launch has the existing simple progress and stops its temporary observer without retry controls", async () => {
  const { chromium, webkit } = await import(process.env.PLAYWRIGHT_MODULE || "playwright")
  assert.ok(!existsSync(fixture) && !existsSync(entry))
  let vite
  const browsers = []
  try {
    writeFileSync(fixture, '<!doctype html><meta name="viewport" content="width=device-width,initial-scale=1"><div id="root"></div><script type="module" src="/start-follow-fixture.tsx"></script>')
    writeFileSync(entry, `import React from 'react';import {createRoot} from 'react-dom/client';
import {Start,StartSheet} from './src/session/Start';import {Starting} from './src/session/Starting';import {StartProgressDialog} from './src/session/StartProgress';
import {CloudSessionSheets} from './src/cloud/CloudSessionSheets';import {bootCatalog} from './src/catalog';import {loadStrings} from './src/legacy/bridge';
import './src/legacy/tokens.css';import './src/legacy/base.css';import './src/legacy/header.css';import './src/legacy/detail.css';import './src/legacy/sheets.css';
localStorage.setItem('ui_language',new URLSearchParams(location.search).get('lang')||'en');await bootCatalog(tag=>'/catalogs/'+tag+'.json');await loadStrings(async()=>({}));
const listeners=new Set();const machineID='synthetic-machine';const destination={machineID,sessionID:'%42',executionGeneration:'a'.repeat(32)};
const state={reads:0,opened:0,arrived:false,emit:()=>listeners.forEach(fn=>fn({machineID,kind:'changed'})),subscriptions:()=>listeners.size};window.fixture=state;
const source={subscribe:fn=>{listeners.add(fn);return()=>listeners.delete(fn)},readMachine:async(id,signal)=>{state.reads++;return {kind:'ready',complete:false,observedAt:Date.now(),rows:state.arrived?[{destination,freshness:'current',observedAt:Date.now()}]:[]}}};
createRoot(document.getElementById('root')).render(<><button id="start-go" onClick={()=>Start.open()}>Start fixture</button><StartSheet/><Starting/><StartProgressDialog/><CloudSessionSheets machineID={machineID} source={source} pending={null} onConsumed={()=>{}} onOpened={()=>state.opened++}/></>);
document.body.style.background='#0d0d0f';document.body.style.color='#eee';`)
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
        for (const mode of ["expiry", "cancel"]) {
          const page = await browser.newPage({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true })
          let launches = 0
          const errors = []
          page.on("pageerror", error => errors.push(String(error)))
          await page.route("**/v1/**", route => {
            const path = new URL(route.request().url()).pathname
            if (route.request().method() === "POST" && path.includes("/start/")) {
              launches++
              return route.fulfill({ json: { ok: true, id: "%42", backend: "tmux", assistant: "codex" } })
            }
            if (path === "/v1/places") return route.fulfill({ json: { places: [{ id: "fixture", label: "Synthetic Project", path: "/work/synthetic", machine: "synthetic-machine" }], assistants: [{ id: "codex", installed: true }] } })
            return route.fulfill({ json: {} })
          })
          await page.goto(origin + "/start-follow-fixture.html?lang=" + lang)
          await page.locator("#start-go").click()
          await page.locator('#start-list .place[data-id="fixture"]').waitFor()
          await page.clock.install()
          await page.locator('#start-list .place[data-id="fixture"]').dispatchEvent("click")
          await page.waitForFunction(() => window.fixture.reads === 1)
          assert.equal(launches, 1)
          assert.doesNotMatch(await page.locator('#start-progress-say').textContent(), /12|60|重新查詢|checks/)
          assert.equal(await page.locator('dialog.start-progress button').count(), 1,
            "the existing background action is the only action")
          if (process.env.SESSION_REFRESH_SCREENSHOTS) await page.screenshot({ path: resolve(process.env.SESSION_REFRESH_SCREENSHOTS, engine.name()+'-'+lang+'-waiting.png'), animations: "disabled" })
          if (mode === "cancel") {
            await page.locator('dialog.start-progress button').dispatchEvent("click")
            await page.locator('#starting-close').dispatchEvent("click")
            await page.clock.runFor(600000)
            assert.equal(await page.evaluate(() => window.fixture.reads), 1)
          } else {
            for (let i = 0; i < 12; i++) await page.clock.runFor(5000)
            assert.equal(await page.evaluate(() => window.fixture.reads), 12)
            assert.equal(await page.evaluate(() => window.fixture.subscriptions()), 0)
            assert.equal(await page.locator('dialog.start-progress').evaluate(el => el.open), false)
            assert.equal(await page.locator('#starting').evaluate(el => el.hidden), true)
            await page.clock.runFor(600000)
            await page.evaluate(() => window.fixture.emit())
            assert.equal(await page.evaluate(() => window.fixture.reads), 12)
            assert.equal(launches, 1, "expiry neither launches another Session nor schedules more reads")
            assert.equal(await page.getByRole('button', {name: /Check again|重新查詢/}).count(), 0)

          }
          assert.deepEqual(errors, [])
          console.log(engine.name(), lang, mode, 'launches', launches, 'reads', await page.evaluate(() => window.fixture.reads))
          await page.close()
        }
      }
    }
  } finally {
    for (const browser of browsers) await browser.close()
    if (vite && vite.exitCode === null) { vite.kill(); await once(vite, "exit") }
    for (const path of [fixture, entry]) if (existsSync(path)) unlinkSync(path)
  }
})
