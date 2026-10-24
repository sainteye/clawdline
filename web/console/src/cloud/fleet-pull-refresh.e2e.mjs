// Run with tools/heavy.sh and PLAYWRIGHT_MODULE pointing to an installed Playwright.
// The actual fleet component and shared gesture hook run against synthetic machines.
import { test } from "node:test"
import assert from "node:assert/strict"
import { spawn } from "node:child_process"
import { existsSync, writeFileSync, unlinkSync } from "node:fs"
import { fileURLToPath } from "node:url"
import { resolve } from "node:path"
import { once } from "node:events"

const root = fileURLToPath(new URL("../../../../", import.meta.url))
const fixture = resolve(root, "web/console/fleet-pull-fixture.html")
const entry = resolve(root, "web/console/fleet-pull-fixture.tsx")

test("fleet pull refresh updates every machine, waits for answers and preserves scrolling and swiping", async () => {
  const { chromium, webkit } = await import(process.env.PLAYWRIGHT_MODULE || "playwright")
  assert.ok(!existsSync(fixture) && !existsSync(entry), "fixture paths must be unused")
  let vite
  const browsers = []
  try {
    writeFileSync(fixture, '<!doctype html><meta name="viewport" content="width=device-width,initial-scale=1"><div id="session-state-filter-slot"></div><div id="root"></div><script type="module" src="/fleet-pull-fixture.tsx"></script>')
    writeFileSync(entry, `import React from 'react';import {createRoot} from 'react-dom/client';
import {FleetSessionList} from './src/cloud/FleetSessionList';import {bootCatalog} from './src/catalog';import {loadStrings} from './src/legacy/bridge';
import './src/legacy/tokens.css';import './src/legacy/list.css';
localStorage.setItem('ui_language',new URLSearchParams(location.search).get('lang')||'en');
await bootCatalog(tag=>'/catalogs/'+tag+'.json');await loadStrings(async()=>({}));
const machines=['alpha','beta'].map(id=>({id,name:id,platform:'Linux',freshness:'current'}));
const calls={alpha:0,beta:0};const pending=[];let held=false;let fail=false;
window.fixture={calls,hold:()=>{held=true},release:()=>{held=false;pending.splice(0).forEach(done=>done())},fail:()=>{fail=true}};
const destination=id=>({machineID:id,sessionID:'session-'+id,executionGeneration:'1'.padStart(32,'0')});
const source={subscribe:()=>()=>{},readMachine:async(id,signal)=>{calls[id]++;if(held)await new Promise(done=>pending.push(done));if(fail&&id==='beta')throw new Error('offline');return {kind:'ready',complete:true,observedAt:Date.now(),snapshotGeneration:String(calls[id]),rows:[{destination:destination(id),state:'working',freshness:'current',needsAttention:false,observedAt:Date.now()}]}},readMachinePresentations:async id=>[{destination:destination(id),title:id+' reading '+calls[id],status:{state:'working',sessionId:'saved-'+id}}]};
createRoot(document.getElementById('root')).render(<FleetSessionList machines={machines} source={source} target={null} filter="" onFilter={()=>{}} onOpen={()=>{}} onConnection={()=>{}} onCloseRequest={()=>{}} onArchiveRequest={()=>{}}/>);
const style=document.createElement('style');style.textContent='body{margin:0}.cloud-all-list-scroll{height:650px;overflow:auto;overscroll-behavior:contain}.cloud-all-groups{min-height:1100px}.row{min-height:90px}.ptr{flex-shrink:0}';document.head.append(style);`)
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
        // No request reaches a real daemon or Cloud service.
        await page.route("**/v1/**", route => route.fulfill({ json: {} }))
        await page.goto(origin + "/fleet-pull-fixture.html?lang=" + lang)
        await page.getByText("alpha reading 1", { exact: true }).waitFor()
        await page.getByText("beta reading 1", { exact: true }).waitFor()
        const gesture = async (dx, dy, release = true, row = false) => page.evaluate(({ dx, dy, release, row }) => {
          const scroller = document.querySelector('#list-scroll')
          const target = row ? scroller.querySelector('li.row') : scroller
          // WebKit exposes Touch but does not allow constructing it. Supply the
          // browser listener's event fields explicitly in both engines.
          const touch = (x,y) => ({ identifier: 1, target, clientX:x, clientY:y })
          const send = (type,x,y) => {
            const event = new Event(type,{bubbles:true})
            Object.defineProperties(event,{touches:{value:type==='touchend'?[]:[touch(x,y)]},changedTouches:{value:[touch(x,y)]}})
            target.dispatchEvent(event)
          }
          send('touchstart',280,200);send('touchmove',280+dx,200+dy);if(release)send('touchend',280+dx,200+dy)
        }, { dx, dy, release, row })
        const counts = () => page.evaluate(() => ({...window.fixture.calls}))
        const baseline = await counts()
        await gesture(0, 35)
        await gesture(-150, 90, true, true)
        assert.deepEqual(await counts(), baseline, "short pulls and horizontal row swipes do not refresh")
        await page.locator('#list-scroll').evaluate(el => { el.scrollTop = 100 })
        await gesture(0, 220)
        assert.deepEqual(await counts(), baseline, "a pull below the top does not refresh")
        await page.locator('#list-scroll').evaluate(el => { el.scrollTop = 0 })
        await page.evaluate(() => window.fixture.hold())
        await gesture(0, 220, false)
        assert.equal(await page.locator('#ptr-label').textContent(), lang === 'en' ? 'Release to refresh' : '放開就重新整理')
        await page.locator('#list-scroll').evaluate(el => {
          const event = new Event('touchend',{bubbles:true})
          Object.defineProperties(event,{touches:{value:[]},changedTouches:{value:[]}})
          el.dispatchEvent(event)
        })
        await page.waitForFunction(() => window.fixture.calls.alpha === 2 && window.fixture.calls.beta === 2)
        assert.equal(await page.locator('#ptr').evaluate(el => el.style.height), '34px', "pad stays open while machines are pending")
        await gesture(0, 220)
        assert.deepEqual(await counts(), { alpha: 2, beta: 2 }, "a busy pull cannot duplicate reads")
        await page.evaluate(() => window.fixture.release())
        await page.getByText('alpha reading 2', {exact:true}).waitFor()
        await page.getByText('beta reading 2', {exact:true}).waitFor()
        await page.waitForFunction(() => document.querySelector('#ptr').style.height === '0px')
        await page.evaluate(() => window.fixture.fail())
        await gesture(0, 220)
        await page.waitForFunction(() => window.fixture.calls.alpha === 3 && window.fixture.calls.beta === 3)
        await page.waitForFunction(() => document.querySelector('#ptr').style.height === '0px')
        await page.getByText('alpha reading 3', {exact:true}).waitFor()
        assert.deepEqual(errors, [], "the fleet has no browser exceptions")
        console.log(engine.name(), lang, await counts())
        await page.close()
      }
    }
  } finally {
    for (const browser of browsers) await browser.close()
    if (vite && vite.exitCode === null) { vite.kill(); await once(vite, "exit") }
    for (const path of [fixture, entry]) if (existsSync(path)) unlinkSync(path)
  }
})
