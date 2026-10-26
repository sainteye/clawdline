// Run directly with PLAYWRIGHT_MODULE pointing to an installed Playwright.
// The actual fleet component runs against synthetic machine status and compact presentations.
import { test } from "node:test"
import assert from "node:assert/strict"
import { spawn } from "node:child_process"
import { existsSync, writeFileSync, unlinkSync } from "node:fs"
import { fileURLToPath } from "node:url"
import { resolve } from "node:path"
import { once } from "node:events"

const root = fileURLToPath(new URL("../../../../", import.meta.url))
const fixture = resolve(root, "web/console/fleet-agent-count-fixture.html")
const entry = resolve(root, "web/console/fleet-agent-count-fixture.tsx")

test("fleet agent counts clear and recover without presentation reads in both browser engines", async () => {
  const { chromium, webkit } = await import(process.env.PLAYWRIGHT_MODULE || "playwright")
  assert.ok(!existsSync(fixture) && !existsSync(entry), "fixture paths must be unused")
  let vite
  const browsers = []
  try {
    writeFileSync(fixture, '<!doctype html><meta name="viewport" content="width=device-width,initial-scale=1"><div id="session-state-filter-slot"></div><div id="root"></div><script type="module" src="/fleet-agent-count-fixture.tsx"></script>')
    writeFileSync(entry, `import React from 'react';import {createRoot} from 'react-dom/client';
import {FleetSessionList} from './src/cloud/FleetSessionList';import {bootCatalog} from './src/catalog';import {loadStrings} from './src/legacy/bridge';
import './src/legacy/tokens.css';import './src/legacy/base.css';import './src/legacy/header.css';import './src/legacy/shell.css';import './src/legacy/list.css';import './src/legacy/responsive.css';import './src/legacy/pages.css';import './src/shell.css';
localStorage.setItem('ui_language',new URLSearchParams(location.search).get('lang')||'en');
await bootCatalog(tag=>'/catalogs/'+tag+'.json');await loadStrings(async()=>({}));
const machines=['alpha','beta'].map(id=>({id,name:id,platform:'Linux',freshness:'current'}));
const calls={alpha:0,beta:0};const displayReads={alpha:0,beta:0};const listeners=new Set();let agents=6;
window.fixture={calls,displayReads,setAgents:n=>{agents=n;listeners.forEach(fn=>fn({machineID:'alpha',kind:'changed'}))}};
const destination=id=>({machineID:id,sessionID:'session-'+id,executionGeneration:'1'.padStart(32,'0')});
const source={subscribe:fn=>{listeners.add(fn);return()=>listeners.delete(fn)},readMachine:async(id,signal)=>{calls[id]++;return {kind:'ready',complete:true,observedAt:Date.now(),snapshotGeneration:String(calls[id]),presentationGeneration:'fixed',rows:[{destination:destination(id),runningAgentCount:agents,agentCountUnknown:agents===null,state:'working',freshness:'current',needsAttention:false,observedAt:Date.now()}]}},readMachinePresentations:async id=>{displayReads[id]++;return [{destination:destination(id),title:id+' reading '+calls[id],status:{state:'working',sessionId:'saved-'+id,work_state:'working',line:'Inspecting a long fixture sentence that must give room to the agent count'}}]}};
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
        await page.goto(origin + "/fleet-agent-count-fixture.html?lang=" + lang)
        await page.getByText("alpha reading 1", { exact: true }).waitFor()
        await page.getByText("beta reading 1", { exact: true }).waitFor()
        const countLabel = lang === 'en' ? '6 subagents running' : '6 個 subagent 在跑'
        assert.equal(await page.locator('.session-agent-count').first().textContent(), countLabel)
        const boxes = await page.locator('.session-agent-count').first().evaluate(el => {
          const r=el.getBoundingClientRect(), p=el.closest('.row').getBoundingClientRect()
          return {left:r.left,right:r.right,rowLeft:p.left,rowRight:p.right,width:r.width,scroll:el.scrollWidth,client:el.clientWidth}
        })
        assert.ok(boxes.left>=boxes.rowLeft && boxes.right<=boxes.rowRight && boxes.scroll<=boxes.client+1, JSON.stringify(boxes))
        const displayBefore = await page.evaluate(() => ({...window.fixture.displayReads}))
        const alphaCount = page.locator('.cloud-all-group').first().locator('.session-agent-count')
        await page.evaluate(() => window.fixture.setAgents(0))
        await alphaCount.waitFor({state:'detached'})
        await page.evaluate(() => window.fixture.setAgents(null))
        await alphaCount.waitFor()
        assert.equal(await alphaCount.textContent(), lang === 'en' ? 'subagents ?' : 'subagent ?')
        await page.evaluate(() => window.fixture.setAgents(2))
        await page.waitForFunction(word => document.querySelector('.cloud-all-group .session-agent-count')?.textContent === word,
          lang === 'en' ? '2 subagents running' : '2 個 subagent 在跑')
        assert.deepEqual(await page.evaluate(() => window.fixture.displayReads), displayBefore,
          'count-only events never reread presentations')
        if (process.env.SUBAGENT_SHOT && lang === 'zh-Hant' && engine === webkit)
          await page.screenshot({path:process.env.SUBAGENT_SHOT})
        assert.deepEqual(errors, [], "the fleet has no browser exceptions")
        console.log(engine.name(), lang, await page.locator('.session-agent-count').allTextContents())
        await page.close()
      }
    }
  } finally {
    for (const browser of browsers) await browser.close()
    if (vite && vite.exitCode === null) { vite.kill(); await once(vite, "exit") }
    for (const path of [fixture, entry]) if (existsSync(path)) unlinkSync(path)
  }
})
