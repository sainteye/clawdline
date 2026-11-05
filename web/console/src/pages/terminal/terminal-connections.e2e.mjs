// The real terminal page opens connection management after a capacity refusal, on both browser engines.
import test from "node:test"
import assert from "node:assert/strict"
import { spawn } from "node:child_process"
import { existsSync, writeFileSync, unlinkSync } from "node:fs"
import { fileURLToPath } from "node:url"
import { resolve } from "node:path"
import { once } from "node:events"

const web = fileURLToPath(new URL("../../../../", import.meta.url))
const html = resolve(web, "console/terminal-connections-fixture.html")
const entry = resolve(web, "console/terminal-connections-fixture.tsx")

test("a full terminal offers its connection list, confirms only the chosen disconnect and keeps keyboard focus", async () => {
  const { chromium, webkit } = await import(process.env.PLAYWRIGHT_MODULE || "playwright")
  assert.ok(!existsSync(html) && !existsSync(entry))
  let vite
  const browsers = []
  try {
    writeFileSync(html, '<!doctype html><div id="root"></div><script type="module" src="/terminal-connections-fixture.tsx"></script>')
    writeFileSync(entry, `
import React from 'react';import {createRoot} from 'react-dom/client';
import {CloudTerminalPage} from './src/pages/terminal/CloudTerminalPage';
import {setTerminalHost} from './src/cloud/terminal-host';
import {activateCatalog} from './src/catalog';import {loadStrings} from './src/legacy/bridge';
import './src/legacy/tokens.css';import './src/legacy/base.css';import './src/legacy/board.css';
import './src/pages/terminal/terminal.css';
const lang=new URLSearchParams(location.search).get('lang')||'en';
const words=await fetch('/catalogs/'+lang+'.json').then(r=>r.json());activateCatalog(words,lang);await loadStrings(async()=>words);
const listeners=new Set();window.requests=[];
let rows=Array.from({length:16},(_,i)=>({machine:'fixture-machine',connection:'connection-'+i,opened_at:Date.now()-i*60000,expires_at:Date.now()+600000,active:i!==15}));
const emit=e=>{for(const f of listeners)f(e)};
const client={deviceID:'fixture-viewer',devicePrivateKey:null,ready:true,retired:false,socketSubscriptions:new Map(),subscriptionHolds:new Map(),pendingSubscriptions:new Set(),subscriptionLimit:64,
_outboundMachinePairing:async()=>({}),_receiveEnvelope:async()=>{},_trimSubscriptions:()=>{},events:f=>{listeners.add(f);return()=>listeners.delete(f)},
_sendSubscriptionFrame:(type,channels)=>{if(type==='subscribe')queueMicrotask(()=>emit({type:'error',error:{code:'terminal_connections_full',subscriptionChannels:channels}}))},
_send:request=>{window.requests.push(request);if(request.action==='disconnect')rows=rows.filter(r=>r.connection!==request.connection||r.machine!==request.machine);queueMicrotask(()=>emit({type:'terminal_connections_result',request_id:request.request_id,action:request.action,limit:16,connections:rows}))}};
setTerminalHost({client,machine:'fixture-machine'});
createRoot(document.getElementById('root')).render(<CloudTerminalPage project="project" channelProject="project" machine="fixture-machine" label="Example project" id="trm_fixture" shown/>);
`)
    vite = spawn(process.execPath, [resolve(web, "node_modules/vite/bin/vite.js"), "--host", "127.0.0.1", "--port", "0"],
      { cwd: resolve(web, "console"), stdio: ["ignore", "pipe", "pipe"] })
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
      for (const lang of ["en", "zh-Hant"]) for (const width of [320, 1280]) {
        const page = await browser.newPage({ viewport: { width, height: 900 } })
        const errors = []
        page.on("pageerror", error => errors.push(String(error)))
        await page.route("**/v1/**", route => route.fulfill({ json: {} }))
        await page.goto(origin + "/terminal-connections-fixture.html?lang=" + lang)
        const dialog = page.locator('.terminal-connections-dialog')
        await dialog.waitFor({ state: 'visible' })
        await page.locator('.terminal-connections-list li').last().waitFor()
        assert.equal(await page.locator('.terminal-connections-list li').count(), 16)
        assert.equal(await dialog.evaluate(el => el.scrollWidth <= el.clientWidth + 1), true, 'dialog fits its viewport')
        await page.locator('.terminal-connections-list li button').first().click()
        assert.equal(await page.evaluate(() => window.requests.filter(r=>r.action==='disconnect').length), 0, 'selecting a row is not confirmation')
        await page.locator('.terminal-connection-confirm .terminal-danger').click()
        await page.waitForFunction(() => document.querySelectorAll('.terminal-connections-list li').length === 15)
        const disconnects = await page.evaluate(() => window.requests.filter(r=>r.action==='disconnect'))
        assert.equal(disconnects.length, 1)
        assert.equal(disconnects[0].connection, 'connection-0')
        assert.equal(disconnects[0].type, 'terminal_connections', 'no shell-close or input action is sent')
        await page.keyboard.press('Escape')
        await dialog.waitFor({ state: 'detached' })
        await page.locator('.terminal-action-more summary').click()
        await page.getByRole('button', { name: lang === 'en' ? 'Terminal connections' : '終端連線', exact: true }).click()
        await dialog.waitFor({ state: 'visible' })
        await page.keyboard.press('Escape')
        await dialog.waitFor({ state: 'detached' })
        assert.equal(await page.evaluate(() => document.activeElement?.tagName !== 'BODY'), true, 'focus returns to a live control')
        assert.deepEqual(errors, [])
        if (process.env.TERMINAL_CONNECTIONS_SCREENSHOT && lang === 'zh-Hant' && width === 320 && engine === webkit) {
          await page.locator('.terminal-action-more summary').click()
          await page.getByRole('button', { name: '終端連線', exact: true }).click()
          await page.screenshot({ path: process.env.TERMINAL_CONNECTIONS_SCREENSHOT, fullPage: true })
        }
        await page.close()
      }
    }
  } finally {
    for (const browser of browsers) await browser.close()
    if (vite && vite.exitCode === null) { vite.kill('SIGTERM'); await once(vite, 'exit') }
    for (const path of [html, entry]) if (existsSync(path)) unlinkSync(path)
  }
})
