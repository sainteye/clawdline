// Real native dialog cancellation and React handlers in Chromium and WebKit.
import { test } from "node:test"
import assert from "node:assert/strict"
import { spawn } from "node:child_process"
import { existsSync, writeFileSync, unlinkSync } from "node:fs"
import { fileURLToPath } from "node:url"
import { resolve } from "node:path"
import { once } from "node:events"

const root = fileURLToPath(new URL("../../", import.meta.url))
const fixture = resolve(root, "console/modal-escape-fixture.html")
const entry = resolve(root, "console/modal-escape-fixture.tsx")

test("modal Escape consumes the native default and preserves cancellation ownership", async () => {
  const { chromium, webkit } = await import(process.env.PLAYWRIGHT_MODULE || "playwright")
  assert.ok(!existsSync(fixture) && !existsSync(entry))
  let vite
  const browsers = []
  try {
    writeFileSync(fixture, '<!doctype html><div id="session-state-filter-slot"></div><div id="root"></div><script type="module" src="/modal-escape-fixture.tsx"></script>')
    writeFileSync(entry, `
import React from 'react';import {createRoot} from 'react-dom/client';
import {bindModalEscape} from './src/modal-escape';
import {FleetSessionList} from './src/cloud/FleetSessionList';
import {bootCatalog} from './src/catalog';import {loadStrings} from './src/legacy/bridge';
import './src/legacy/tokens.css';import './src/legacy/base.css';import './src/legacy/shell.css';import './src/legacy/list.css';import './src/legacy/pages.css';import './src/shell.css';
localStorage.setItem('ui_language',new URLSearchParams(location.search).get('lang')||'en');
await bootCatalog(tag=>'/catalogs/'+tag+'.json');await loadStrings(async()=>({}));
window.unbind=new URLSearchParams(location.search).has('control')?()=>{}:bindModalEscape(window);window.cancelCount=0;window.closeCount=0;
window.addEventListener('keydown',e=>{window.lastKey=e},{capture:true});
window.addEventListener('cancel',e=>{if(e.target.id==='native')window.cancelCount++},{capture:true});
window.addEventListener('close',e=>{if(e.target.id==='native')window.closeCount++},{capture:true});
const machines=['alpha','beta'].map(id=>({id,name:id,platform:'Linux',freshness:'current'}));
const source={subscribe:()=>()=>{},readMachine:async()=>({kind:'ready',complete:true,observedAt:Date.now(),rows:[]}),readMachinePresentations:async()=>[]};
function Examples(){const [busy,setBusy]=React.useState(false);return <>
<button id="native-open" onClick={()=>document.getElementById('native').showModal()}>Native</button>
<dialog id="native"><input id="native-input"/><button id="nested-open" onClick={()=>{document.getElementById('nested').hidden=false}}>Picker</button>
<div id="nested" hidden onKeyDown={e=>{if(e.key==='Escape'){e.preventDefault();e.stopPropagation();e.currentTarget.hidden=true;document.getElementById('nested-open').focus()}}}><button id="nested-choice">Choice</button></div></dialog>
<button id="busy-open" onClick={()=>{setBusy(true);document.getElementById('busy').showModal()}}>Busy</button>
<dialog id="busy" onCancel={e=>{if(busy)e.preventDefault()}}><button id="finish" onClick={()=>setBusy(false)}>Finish</button></dialog>
<button id="custom-open" onClick={()=>{document.getElementById('custom').hidden=false;document.getElementById('custom-input').focus()}}>Custom</button>
<div id="custom" role="dialog" aria-modal="true" hidden onKeyDown={e=>{if(e.key==='Escape')e.currentTarget.hidden=true}}><input id="custom-input"/></div>
<input id="outside"/>
<FleetSessionList machines={machines} source={source} target={null} filter="" onFilter={()=>{}} onOpen={()=>{}} onConnection={()=>{}} onMachineAction={()=>{}} onCloseRequest={()=>{}} onArchiveRequest={()=>{}}/>
</>}
createRoot(document.getElementById('root')).render(<Examples/>);
`)
    vite = spawn(process.execPath, [resolve(root, "node_modules/vite/bin/vite.js"), "--host", "127.0.0.1", "--port", "0"],
      { cwd: resolve(root, "console"), stdio: ["ignore", "pipe", "pipe"] })
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
        const page = await browser.newPage()
        const errors = []
        page.on("pageerror", error => errors.push(String(error)))
        await page.route("**/v1/**", route => route.fulfill({ json: {} }))
        await page.goto(origin + "/modal-escape-fixture.html?lang=" + lang + (process.env.CLAWDLINE_MODAL_ESCAPE_CONTROL ? '&control=1' : ''))
        await page.locator('#start-go').waitFor()
        const canceled = () => page.evaluate(() => window.lastKey.defaultPrevented)
        await page.locator('#outside').press('Escape')
        assert.equal(await canceled(), false, 'Escape outside a modal keeps its native behavior')

        await page.locator('#start-go').press('Enter')
        await page.locator('.cloud-all-machine-dialog').waitFor({state:'visible'})
        assert.equal(await page.locator('.cloud-all-machine-dialog').getAttribute('aria-label'), lang === 'en' ? 'Start it on which machine?' : '要開在哪一台機器？')
        await page.keyboard.press('Escape')
        await page.locator('.cloud-all-machine-dialog').waitFor({state:'hidden'})
        assert.equal(await canceled(), true, 'the actual machine chooser consumes keydown, not only cancel')
        assert.equal(await page.evaluate(() => document.activeElement.id), 'start-go', engine.name()+' '+lang+' restores the chooser opener')

        await page.locator('#native-open').press('Enter')
        const before = await page.evaluate(() => ({cancel:window.cancelCount,close:window.closeCount}))
        await page.keyboard.press('Escape')
        await page.locator('#native').waitFor({state:'hidden'})
        assert.equal(await canceled(), true)
        await page.waitForFunction(expected => window.closeCount === expected, before.close+1)
        assert.deepEqual(await page.evaluate(() => ({cancel:window.cancelCount,close:window.closeCount})), {cancel:before.cancel+1,close:before.close+1})
        assert.equal(await page.evaluate(() => document.activeElement.id), 'native-open')

        await page.locator('#busy-open').click()
        await page.keyboard.press('Escape')
        assert.equal(await canceled(), true, 'a busy refusal also consumes Escape')
        assert.equal(await page.locator('#busy').isVisible(), true, 'cancel remains vetoable')
        await page.locator('#finish').click()
        await page.keyboard.press('Escape')
        await page.locator('#busy').waitFor({state:'hidden'})

        await page.locator('#native-open').click()
        await page.locator('#nested-open').click()
        await page.locator('#nested-choice').press('Escape')
        assert.equal(await canceled(), true)
        assert.equal(await page.locator('#nested').isVisible(), false)
        assert.equal(await page.locator('#native').isVisible(), true, 'an inner picker owns the first Escape')
        await page.keyboard.press('Escape')
        await page.locator('#native').waitFor({state:'hidden'})

        await page.locator('#custom-open').click()
        await page.keyboard.press('Escape')
        assert.equal(await canceled(), true, 'a React sheet that hides during keydown still consumes the key')
        assert.equal(await page.locator('#custom').isVisible(), false)

        await page.locator('#native-open').click()
        assert.equal(await page.locator('#native').isVisible(), true)
        assert.equal(await page.locator('#native-input').evaluate(el => {
          const event=new KeyboardEvent('keydown',{key:'Escape',bubbles:true,cancelable:true,isComposing:true});el.dispatchEvent(event);return event.defaultPrevented
        }), false, 'IME composition is left alone')
        if (!await page.locator('#native').isVisible()) await page.locator('#native-open').press('Enter')
        assert.equal(await page.locator('#native-input').evaluate(el => {
          Object.defineProperty(document,'fullscreenElement',{configurable:true,value:document.body})
          const event=new KeyboardEvent('keydown',{key:'Escape',bubbles:true,cancelable:true});el.dispatchEvent(event)
          delete document.fullscreenElement;return event.defaultPrevented
        }), false, 'the native fullscreen exit keeps Escape')
        if (!await page.locator('#native').isVisible()) await page.locator('#native-open').press('Enter')
        await page.evaluate(() => window.unbind())
        await page.keyboard.press('Escape')
        assert.equal(await canceled(), false, 'cleanup removes the shared handler')
        assert.deepEqual(errors, [])
        console.log(engine.name(), lang, 'machine chooser, native, busy, nested, React, IME and fullscreen paths passed')
        await page.close()
      }
    }
  } finally {
    for (const browser of browsers) await browser.close()
    if (vite && vite.exitCode === null) { vite.kill(); await once(vite, "exit") }
    for (const path of [fixture, entry]) if (existsSync(path)) unlinkSync(path)
  }
})
