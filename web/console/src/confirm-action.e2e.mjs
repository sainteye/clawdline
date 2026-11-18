// Real DOM and focus behavior in Chromium and WebKit; no daemon or external calls.
import { test } from "node:test"
import assert from "node:assert/strict"
import { spawn } from "node:child_process"
import { existsSync, writeFileSync, unlinkSync } from "node:fs"
import { fileURLToPath } from "node:url"
import { resolve } from "node:path"
import { once } from "node:events"

const web = fileURLToPath(new URL("../../", import.meta.url))
const fixture = resolve(web, "console/confirm-action-fixture.html")
const entry = resolve(web, "console/confirm-action-fixture.ts")

test("shared confirmation keeps explicit decisions, cancellation and nested focus in both engines", async () => {
  const { chromium, webkit } = await import(process.env.PLAYWRIGHT_MODULE || "playwright")
  assert.ok(!existsSync(fixture) && !existsSync(entry))
  let vite
  const browsers = []
  try {
    writeFileSync(fixture, '<!doctype html><meta name="viewport" content="width=device-width,initial-scale=1"><dialog id="parent"><button id="open">Open</button></dialog><script type="module" src="/confirm-action-fixture.ts"></script>')
    writeFileSync(entry, `
import {confirmAction} from './src/confirm-action';
import {bootCatalog} from './src/catalog';
import {bindModalEscape} from './src/modal-escape';
import './src/legacy/tokens.css';
localStorage.setItem('ui_language',new URLSearchParams(location.search).get('lang')||'en');
await bootCatalog(tag=>'/catalogs/'+tag+'.json');
bindModalEscape(window);
window.confirm=()=>{throw new Error('native confirmation must not be used')};
window.ask=confirmAction;window.answer=null;
document.querySelector('#open').onclick=()=>{window.answer=null;void confirmAction('Run this task?').then(value=>{window.answer=value})};
document.querySelector('#parent').showModal();window.ready=true;
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
      for (const lang of ["en", "zh-Hant"]) for (const width of [390, 1280]) {
        const page = await browser.newPage({ viewport: { width, height: 844 } })
        const errors = []
        page.on("pageerror", error => errors.push(String(error)))
        try {
          await page.goto(origin + "/confirm-action-fixture.html?lang=" + lang)
          await page.waitForFunction(() => window.ready)
          const open = async () => { await page.locator('#open').focus(); await page.locator('#open').press('Enter') }
          const choose = value => page.locator('.app-confirm [data-choice=' + value + ']').click()
          const answer = async value => {
            await page.waitForFunction(expected => window.answer === expected, value)
            assert.equal(await page.locator('.app-confirm').count(), 0)
            assert.equal(await page.locator('#parent').evaluate(node => node.open), true)
            assert.equal(await page.evaluate(() => document.activeElement.id), 'open')
          }
          await open()
          assert.equal(await page.locator('.app-confirm [data-choice=cancel]').textContent(), lang === 'en' ? 'Cancel' : '取消')
          assert.equal(await page.locator('.app-confirm [data-choice=confirm]').textContent(), lang === 'en' ? 'Confirm' : '確認')
          assert.equal(await page.evaluate(() => document.activeElement.className), 'app-confirm')
          await page.keyboard.press('Enter')
          assert.equal(await page.evaluate(() => window.answer), null, 'initial Enter cannot approve an action')
          await page.keyboard.press('Tab')
          assert.equal(await page.evaluate(() => document.activeElement.dataset.choice), 'cancel')
          await page.keyboard.press('Shift+Tab')
          assert.equal(await page.evaluate(() => document.querySelector('.app-confirm').contains(document.activeElement)), true)
          await page.keyboard.press('Escape')
          await answer(false)
          await open()
          await page.mouse.click(4, 4)
          await answer(false)
          await open(); await choose('confirm'); await answer(true)
          await open(); await choose('cancel'); await answer(false)
          await open()
          assert.equal(await page.evaluate(() => window.ask('A second action')), false, 'a repeated request is never queued or accepted')
          assert.equal(await page.locator('#app-confirm-message').textContent(), 'Run this task?')
          await choose('cancel'); await answer(false)
          await page.evaluate(() => {
            window.abortOwner = new AbortController(); window.answer = null
            void window.ask('Owned action', {signal:window.abortOwner.signal}).then(value => {window.answer=value})
            window.abortOwner.abort()
          })
          await answer(false)
          await open()
          await page.locator('.app-confirm').evaluate(node => node.close())
          await answer(false)
          await open()
          await page.locator('.app-confirm').evaluate(node => node.remove())
          await answer(false)
          await page.evaluate(() => {
            window.answer = null
            void window.ask('<img src=x onerror=alert(1)> '.repeat(150), {confirmLabel:'Continue',cancelLabel:'Stay'}).then(value => {window.answer=value})
          })
          assert.equal(await page.locator('.app-confirm img').count(), 0)
          const longDialog = page.locator('.app-confirm')
          assert.ok(await longDialog.evaluate(node => node.getBoundingClientRect().width <= innerWidth))
          await longDialog.evaluate(node => { node.scrollTop = node.scrollHeight })
          const cancelAtBottom = await longDialog.locator('[data-choice=cancel]').boundingBox()
          assert.ok(cancelAtBottom && cancelAtBottom.y >= 0 && cancelAtBottom.y < 120, 'cancel remains visible at the top')
          await page.mouse.click(cancelAtBottom.x + cancelAtBottom.width / 2, cancelAtBottom.y + cancelAtBottom.height / 2)
          await answer(false)
          await open()
          await page.evaluate(() => document.querySelector('#open').remove())
          await page.waitForFunction(() => window.answer === false)
          assert.equal(await page.locator('.app-confirm').count(), 0)
          assert.deepEqual(errors, [], engine.name() + ' ' + lang + ' ' + width)
        } finally { await page.close() }
      }
    }
  } finally {
    for (const browser of browsers) await browser.close()
    if (vite) { const stopped = once(vite, "exit"); vite.kill(); await stopped }
    for (const file of [fixture, entry]) if (existsSync(file)) unlinkSync(file)
  }
})
