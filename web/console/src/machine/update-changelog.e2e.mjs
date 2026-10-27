// node --test web/console/src/machine/update-changelog.e2e.mjs
// Real React components in an isolated Chrome profile; every transport is synthetic.
import assert from "node:assert/strict"
import { test, before, after } from "node:test"
import { build } from "esbuild"
import { createServer } from "node:http"
import { spawn } from "node:child_process"
import { mkdtempSync, rmSync, writeFileSync } from "node:fs"
import { tmpdir } from "node:os"
import { join, dirname } from "node:path"
import { fileURLToPath } from "node:url"

let chrome, profile, server, ws, origin, session
let sequence = 0
const pending = new Map()
const directory = dirname(fileURLToPath(import.meta.url))
const fixture = `
import React from 'react'; import {createRoot} from 'react-dom/client';
import {UpdateBanner} from './UpdateBanner.tsx'; import {UpdatePanel} from './UpdatePanel.tsx';
import {readingThrough} from '../cloud/machine-switch.ts';
import {activateCatalog} from '../catalog.ts';
import zh from '../../public/catalogs/zh-Hant.json';
const status = version => ({kind:'status', status:{install_kind:'release',state:'update_available',
 running:{stamp:'old',version:'v2.1.0'},latest:{stamp:version,version},apply:{state:'idle'}}});
const listeners = new Set();
const f = window.fixture = { read:status('v2.3.0'), checks:0, installs:0, notes:[], fail:false, hold:false,
 next:null, release:null, switch:()=>readingThrough({}),
 publish(value){this.read=value; for(const fn of listeners)fn()},
 async check(){this.checks++; this.publish({kind:'checking'}); if(this.hold)await new Promise(r=>this.release=r);
 const next=this.next || status('v2.3.0'); this.publish(next); return next},
 subscribe(fn){listeners.add(fn);return ()=>listeners.delete(fn)}, status };
readingThrough({});
window.fetch = async url => {f.notes.push(String(url)); if(f.fail)return new Response('',{status:429});
 const version=decodeURIComponent(String(url).split('/').pop());
 return Response.json({tag_name:version,body:'## Changes for '+version+'\\n\\n- Review before installing.\\n\\n<script>window.injected=true</script>'})};
if(location.search.includes('zh'))activateCatalog(zh,'zh-Hant');
createRoot(document.getElementById('root')).render(location.search.includes('panel') ? <UpdatePanel shown={false}/> : <UpdateBanner/>);
`

function send(method, params = {}, sessionId) {
  const id = ++sequence
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => { pending.delete(id); reject(new Error(method + " timed out")) }, 10000)
    pending.set(id, { resolve: value => { clearTimeout(timer); resolve(value) }, reject })
    ws.send(JSON.stringify({ id, method, params, sessionId }))
  })
}
async function run(expression) {
  const { result, exceptionDetails } = await send("Runtime.evaluate", { expression, awaitPromise: true, returnByValue: true }, session)
  if (exceptionDetails) throw new Error(exceptionDetails.exception?.description || exceptionDetails.text)
  return result.value
}
async function until(expression) {
  await run(`new Promise((resolve,reject)=>{const end=Date.now()+5000;const tick=()=>{if(${expression})resolve(true);else if(Date.now()>end)reject(new Error('Condition timed out'));else setTimeout(tick,20)};tick()})`)
}
async function load(query = "") {
  await send("Page.navigate", { url: origin + "/" + query }, session)
  await until("window.fixture && document.querySelector('button')")
}
const click = text => run(`[...document.querySelectorAll('button')].find(b=>b.textContent===${JSON.stringify(text)}).click()`)
const review = async (label = "Review update") => { await click(label); await until("document.querySelector('dialog[open] .update-changelog-copy')") }

before(async () => {
  const { outputFiles } = await build({
    stdin: { contents: fixture, resolveDir: directory, loader: "tsx" }, bundle: true,
    format: "iife", write: false, jsx: "automatic", loader: { ".css": "empty" },
    plugins: [{ name: "synthetic-machine", setup(b) {
      b.onResolve({ filter: /^(\.\/update\.js|\.\.\/client\.js|\.\/restart-app\.js|\.\.\/legacy\/bridge\.js|\.\/NeedsUpdate\.js|\.\.\/pages\/settings\/api\.js)$/ }, ({ path }) => ({ path, namespace: "fixture" }))
      b.onLoad({ filter: /.*/, namespace: "fixture" }, ({ path }) => ({ contents:
        path.includes("client") ? "export const followsRelay=()=>false"
        : path.includes("restart-app") ? "export const canRestartStagedApp=()=>false;export const restartStagedApp=()=>false"
        : path.includes("bridge") ? "export const failureSentence=()=>''"
        : path.includes("NeedsUpdate") ? "export const useMachineVersion=()=>null"
        : path.includes("api") ? "export const writeSettings=()=>Promise.resolve()"
        : `export const currentUpdateRead=()=>window.fixture.read;
           export const subscribeUpdate=fn=>window.fixture.subscribe(fn);
           export const checkUpdateNow=()=>window.fixture.check();
           export const applyUpdate=async()=>{window.fixture.installs++; return {kind:'refused',code:'fixture_done',detail:''}};
           export const refreshUpdate=()=>Promise.resolve(window.fixture.read);
           export const publishUpdateRead=value=>window.fixture.publish(value);
           export const requestDevRelease=()=>{throw new Error('Unexpected release')};export const servedConsoleChanged=async()=>false;`,
      }))
    } }],
  })
  const css = (await import("node:fs/promises")).readFile(join(directory, "update-changelog.css"), "utf8")
  server = createServer(async (req, res) => {
    if (req.url === "/fixture.js") { res.setHeader("content-type", "text/javascript"); res.end(outputFiles[0].text) }
    else { res.setHeader("content-type", "text/html"); res.end(`<!doctype html><html><head><meta name="viewport" content="width=device-width,initial-scale=1"><style>:root{--ink:#eee;--dim:#aaa;--card:#202024;--line:#444;--accent:#e99b5b}body{background:#111;color:#eee;font:14px system-ui}.chip{font:inherit;padding:8px 12px;border-radius:8px;border:1px solid #555;background:#333;color:#eee}.primary{background:#a95120} ${await css}</style></head><body><div id="root"></div><script src="/fixture.js"></script></body></html>`) }
  })
  await new Promise(resolve => server.listen(0, "127.0.0.1", resolve))
  origin = "http://127.0.0.1:" + server.address().port
  profile = mkdtempSync(join(tmpdir(), "clawdline-changelog-browser-"))
  chrome = spawn(process.env.CHROME || "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", ["--headless=new", "--no-first-run", "--no-default-browser-check", "--remote-debugging-port=0", "--user-data-dir=" + profile, "about:blank"], { stdio: ["ignore", "ignore", "pipe"] })
  const address = await new Promise((resolve, reject) => {
    let log = ""
    const timer = setTimeout(() => reject(new Error("Chrome did not start: " + log)), 10000)
    chrome.stderr.on("data", chunk => { log += chunk; const match = log.match(/DevTools listening on (ws:\/\/\S+)/); if (match) { clearTimeout(timer); resolve(match[1]) } })
  })
  ws = new WebSocket(address)
  await new Promise(resolve => ws.addEventListener("open", resolve, { once: true }))
  ws.addEventListener("message", event => {
    const message = JSON.parse(String(event.data)); const call = pending.get(message.id)
    if (!call) return
    pending.delete(message.id)
    if (message.error) call.reject(new Error(message.error.message)); else call.resolve(message.result)
  })
  const { targetId } = await send("Target.createTarget", { url: "about:blank" })
  session = (await send("Target.attachToTarget", { targetId, flatten: true })).sessionId
  await send("Runtime.enable", {}, session)
})
after(async () => {
  ws?.close()
  if (chrome && chrome.exitCode === null && chrome.signalCode === null) { const stopped = new Promise(resolve => chrome.once("exit", resolve)); chrome.kill(); await stopped }
  server?.closeAllConnections()
  await new Promise(resolve => server?.close(resolve) ?? resolve())
  if (profile) rmSync(profile, { recursive: true, force: true })
})

test("both update entries show notes first; cancel and Escape never install", async () => {
  for (const query of ["", "?panel"]) {
    await load(query); await review()
    assert.equal(await run("fixture.installs"), 0)
    assert.equal(await run("document.activeElement===document.querySelector('dialog')"), true)
    assert.equal(await run("Boolean(window.injected || document.querySelector('dialog script'))"), false)
    await click("Not now"); await until("!document.querySelector('dialog')")
    assert.equal(await run("fixture.installs"), 0)
    await review()
    await send("Input.dispatchKeyEvent", { type: "keyDown", key: "Escape", code: "Escape", windowsVirtualKeyCode: 27 }, session)
    await send("Input.dispatchKeyEvent", { type: "keyUp", key: "Escape", code: "Escape", windowsVirtualKeyCode: 27 }, session)
    await until("!document.querySelector('dialog')")
    assert.equal(await run("fixture.installs"), 0)
  }
})

test("a confirmed update checks fresh and installs once from either entry", async () => {
  for (const query of ["", "?panel"]) {
    await load(query); await review(); await click("Confirm update")
    await until("fixture.installs===1 && !document.querySelector('dialog')")
    assert.equal(await run("fixture.checks"), 1)
  }
})

test("a newer release replaces notes and requires a second confirmation", async () => {
  await load(); await review()
  await run("fixture.next=fixture.status('v2.4.0')")
  await click("Confirm update")
  await until("document.querySelector('.update-changelog-copy')?.textContent.includes('v2.4.0')")
  assert.equal(await run("fixture.installs"), 0)
  await click("Confirm update"); await until("fixture.installs===1")
})

test("a changed commit under the same release tag needs another confirmation", async () => {
  await load(); await review()
  await run("fixture.next=fixture.status('v2.3.0');fixture.next.status.latest.stamp='replacement'")
  await click("Confirm update"); await until("document.querySelector('.update-changelog-notice') && document.querySelector('.update-changelog-copy')")
  assert.equal(await run("fixture.installs"), 0)
  assert.equal(await run("fixture.notes.length"), 2)
  await click("Confirm update"); await until("fixture.installs===1")
})

test("withdrawn releases and failed checks never install", async () => {
  for (const next of ["{kind:'unreachable'}", "{kind:'status',status:{...fixture.read.status,state:'current'}}"]) {
    await load("?panel"); await review(); await run("fixture.next=" + next)
    await click("Confirm update"); await until("document.querySelector('.update-changelog-notice')")
    assert.equal(await run("fixture.installs"), 0)
  }
})

test("switching machines or cancelling during a fresh check invalidates its confirmation", async () => {
  for (const action of ["fixture.switch()", "document.querySelector('.update-changelog-buttons button').click() "]) {
    await load(); await review(); await run("fixture.hold=true")
    await click("Confirm update"); await until("fixture.release")
    await run(action); await until("!document.querySelector('dialog')")
    await run("fixture.release()"); assert.equal(await run("fixture.installs"), 0)
  }
})

test("notes failures offer an explicit fallback and retry recovers", async () => {
  await load(); await run("fixture.fail=true"); await click("Review update")
  await until("document.querySelector('dialog')?.textContent.includes('Update anyway')")
  assert.equal(await run("fixture.installs"), 0)
  await run("fixture.fail=false"); await click("Retry")
  await until("document.querySelector('.update-changelog-copy')")
  assert.equal(await run("fixture.notes.length"), 2)
  await click("Confirm update"); await until("fixture.installs===1")
})

test("Taiwan Chinese mobile review fits the screen", async () => {
  await send("Emulation.setDeviceMetricsOverride", { width: 390, height: 844, mobile: true, deviceScaleFactor: 1 }, session)
  await load("?zh"); await review("查看更新")
  assert.match(await run("document.querySelector('dialog').textContent"), /確認更新/)
  const fits = await run("(()=>{const r=document.querySelector('dialog').getBoundingClientRect();return r.left>=0 && r.right<=innerWidth && r.top>=0 && r.bottom<=innerHeight})()")
  assert.equal(fits, true)
  if (process.env.CLAWDLINE_SHOTS) {
    const { data } = await send("Page.captureScreenshot", { format: "png" }, session)
    writeFileSync(join(process.env.CLAWDLINE_SHOTS, "update-changelog-zh-mobile.png"), Buffer.from(data, "base64"))
  }
})
