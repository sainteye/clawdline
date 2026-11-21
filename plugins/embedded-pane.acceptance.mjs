/** A view pinned beside the Session list: production host, real packages, synthetic Sessions. */
import assert from 'node:assert/strict'
import { createServer } from 'node:http'
import { readFile, mkdir } from 'node:fs/promises'
import { createRequire } from 'node:module'
import { fileURLToPath, pathToFileURL } from 'node:url'
const root = fileURLToPath(new URL('../', import.meta.url))
const require = createRequire(root + 'web/package.json'), { build } = require('esbuild')
const output = process.env.PLUGIN_ARTIFACTS
if (!output || !process.env.PLAYWRIGHT_MODULE) throw Error('Set PLUGIN_ARTIFACTS and PLAYWRIGHT_MODULE; no dependency installation is performed')
await mkdir(output, { recursive: true })
// The page stands in for the Sessions page: a list column, and the pane the host fills.
const bundle = await build({ stdin: { contents: `
import React,{useState} from 'react';import{createRoot}from'react-dom/client';import{useViewPlugins}from'./view-plugins/Host';
const projects=['設計室','開發工作台','研究筆記'],titles=['檢視設計細節','調整介面排版','整理研究筆記','套件等待驗收','核對瀏覽器畫面','等待整合結果'],states=['waiting_you','working','ready','milestone_complete','working','waiting_session'];
const rows=titles.map((title,i)=>({key:'sample-'+i,title,project:{key:'p'+i%3,label:projects[i%3]},workState:states[i],freshness:'fresh'}));
function Host(){const[open,setOpen]=useState(null);
 const source={key:'synthetic',label:'Synthetic Sessions',async read(){return{sessions:rows,connected:true}},subscribe(){return()=>{}},async prepareOpen(key){return()=>{window.__opened=key;setOpen(key)}}};
 const api=useViewPlugins([source],'zh-Hant');
 return <><button id="manager" onClick={e=>api.openManager(e.currentTarget)}>Manager</button>{api.dock}
  <main style={{display:'grid',gridTemplateColumns:'360px minmax(0,1fr)',height:'calc(100vh - 60px)'}}>
   <section id="list">{open?<button id="close-session" onClick={()=>setOpen(null)}>back</button>:null}</section>
   {open?<section id="pane-detail" style={{gridColumn:2,gridRow:1}}>Session {open}</section>:null}
   {api.viewPane(!!open)}
  </main>{api.content}</>}
createRoot(document.getElementById('root')).render(<Host/>);
`, resolveDir: root + 'web/console/src', loader: 'tsx' }, bundle: true, write: false, outfile: 'host.js', format: 'esm', platform: 'browser', jsx: 'automatic', define: { 'process.env.NODE_ENV': '"development"' } })
const files = new Map(bundle.outputFiles.map(f => ['/' + f.path.split('/').at(-1), { bytes: f.contents, type: f.path.endsWith('.js') ? 'text/javascript' : 'text/css' }]))
const server = createServer((req, res) => {
  const url = new URL(req.url, 'http://fixture')
  if (url.pathname.startsWith('/view-plugins/previews/') && /^[a-zA-Z0-9-]+\.webp$/.test(url.pathname.slice(23))) {
    readFile(root + 'web/console/public' + url.pathname).then(bytes => { res.setHeader('Content-Type', 'image/webp'); res.end(bytes) }, () => { res.writeHead(404); res.end() }); return
  }
  const item = url.pathname === '/' ? { type: 'text/html', bytes: '<!doctype html><html lang="zh-Hant" data-theme="dark"><meta charset="utf-8"><link rel="stylesheet" href="/host.css"><body style="margin:0;background:#101014;color:#eee"><div id="root"></div><script type="module" src="/host.js"></script>' } : files.get(url.pathname)
  if (!item) { res.writeHead(404); res.end(); return }
  res.setHeader('Content-Type', item.type); res.end(item.bytes)
})
await new Promise(r => server.listen(0, '127.0.0.1', r))
const origin = `http://127.0.0.1:${server.address().port}`
const pw = await import(pathToFileURL(process.env.PLAYWRIGHT_MODULE).href)
const browser = await pw.chromium.launch({ headless: true })
const page = await browser.newPage({ viewport: { width: 1440, height: 900 }, reducedMotion: 'reduce' }), errors = []
page.on('pageerror', e => errors.push(e.message))
const install = async slug => {
  await page.locator('input[type=file]:not([disabled])').setInputFiles({ name: slug + '.zip', mimeType: 'application/zip', buffer: await readFile(root + 'web/console/public/view-plugins/session-' + slug + '.zip') })
  await page.locator('.vp-review').waitFor()
  await page.getByRole('button', { name: '安裝套件', exact: true }).click()
  await page.locator('.vp-review[data-phase=installed]').waitFor()
}
const frame = () => page.frameLocator('.vp-pane iframe')
// Previews decode asynchronously; a screenshot waits for every picture the host shows.
const settled = () => page.waitForFunction(() => [...document.querySelectorAll('.vp-host img')].every(img => img.complete && img.naturalWidth > 0))
try {
  await page.goto(origin)
  assert.equal(await page.locator('.vp-pane').count(), 0, 'nothing is pinned yet: the pane keeps its home screen')
  // With nothing installed, the launcher introduces views and offers recommendations.
  await page.locator('.vp-dock').click()
  await page.locator('.vp-hero').waitFor()
  assert.ok(await page.locator('.vp-launcher .vp-example').count() > 0, 'the empty launcher offers recommended views')
  await settled()
  await page.screenshot({ path: output + '/launcher-empty.png' })
  await page.getByRole('button', { name: '關閉', exact: true }).click()
  await page.locator('.vp-host').waitFor({ state: 'detached' })
  await page.locator('#manager').click()
  for (const slug of ['departures', 'codecraft']) await install(slug)
  await page.getByRole('button', { name: '關閉', exact: true }).click()
  await page.locator('.vp-host').waitFor({ state: 'detached' })

  // Pin from the status-bar launcher's card; the host closes and the pane shows the view.
  await page.locator('.vp-dock').click()
  await page.locator('.vp-view-card').nth(1).waitFor()
  assert.equal(await page.locator('.vp-view-card').count(), 2, 'both enabled views are cards')
  assert.equal(await page.locator('.vp-view-card .vp-view-thumb img').count(), 2, 'curated views show their previews')
  await page.locator('.vp-view-card[data-package-id="com.clawdline.session-codecraft"] .vp-view-open').hover()
  await settled()
  await page.screenshot({ path: output + '/launcher-cards.png' })
  await page.locator('.vp-view-card[data-package-id="com.clawdline.session-departures"]').getByRole('button', { name: '固定在右側' }).click()
  await page.locator('.vp-host').waitFor({ state: 'detached' })
  await frame().locator('body.embedded').waitFor()
  assert.equal(await frame().locator('#close').isVisible(), false, 'the package close is hidden when embedded')
  assert.equal(await page.locator('.vp-pane .vp-frame-bar').count(), 0, 'no host return bar inside the pane')
  const paneBox = await page.locator('.vp-pane').boundingBox()
  assert.ok(paneBox.x >= 360 && paneBox.width > 1000, 'the pane is the second column')
  assert.ok(await frame().locator('.row:not(.blank)').count() >= 6, 'every synthetic Session is on the board')
  await page.screenshot({ path: output + '/embedded-departures.png' })
  // The pane's narrowest width (900-pixel desktop minus the 360-pixel list) still fits the whole board.
  await page.setViewportSize({ width: 900, height: 800 })
  await page.waitForTimeout(300)
  const fits = await frame().locator('.board-scroll').evaluate(el => el.scrollWidth <= el.clientWidth + 1)
  assert.ok(fits, 'the board fits a 540-pixel embedded pane without horizontal scrolling')
  await page.screenshot({ path: output + '/embedded-departures-narrow.png' })
  await page.setViewportSize({ width: 1440, height: 900 })

  // Opening a Session navigates without closing the view; the pane hides behind it.
  await frame().locator('.row:not(.blank)').first().click()
  await frame().locator('#open').click()
  await page.locator('#pane-detail').waitFor()
  assert.equal(await page.evaluate(() => window.__opened), 'sample-0')
  assert.equal(await page.locator('.vp-pane').isHidden(), true)
  await page.locator('#close-session').click()
  await frame().locator('body.embedded').waitFor()
  assert.equal(await frame().locator('aside h2').textContent(), '檢視設計細節', 'returning shows the same instance, selection kept')

  // Escape inside the package does not unpin.
  await frame().locator('body').press('Escape'); await frame().locator('body').press('Escape')
  await page.waitForTimeout(200)
  assert.equal(await page.locator('.vp-pane').count(), 1)

  // Switch to the village: the roster column and header are gone; the scene fills the pane.
  // The picker is a menu with covers: keyboard moves from the checked view, Escape keeps the pin.
  await page.locator('.vp-switch-trigger').click()
  await page.locator('.vp-switch-menu').waitFor()
  assert.equal(await page.evaluate(() => document.activeElement?.getAttribute('aria-checked')), 'true', 'focus starts on the current view')
  assert.equal(await page.locator('.vp-switch-menu [role=menuitemradio] img').count(), 2, 'every view shows its cover')
  await page.keyboard.press('ArrowUp')
  await page.screenshot({ path: output + '/embedded-switch.png' })
  await page.keyboard.press('Escape')
  await page.locator('.vp-switch-menu').waitFor({ state: 'detached' })
  assert.equal(await page.locator('.vp-pane').count(), 1, 'closing the picker keeps the view pinned')
  // All views opens the launcher in browse mode, with the pinned card marked.
  await page.locator('.vp-switch-trigger').click()
  await page.getByRole('menuitem', { name: '所有視圖…' }).click()
  await page.locator('.vp-view-card[data-pinned=true] .vp-badge-pinned').waitFor()
  await settled()
  await page.screenshot({ path: output + '/launcher-pinned.png' })
  await page.getByRole('button', { name: '關閉', exact: true }).click()
  await page.locator('.vp-host').waitFor({ state: 'detached' })
  await page.locator('.vp-switch-trigger').click()
  // The village is listed before the board, so it is one step up from the current view.
  await page.keyboard.press('ArrowUp')
  assert.match(await page.evaluate(() => document.activeElement?.textContent ?? ''), /奇幻村莊/)
  await page.keyboard.press('Enter')
  await frame().locator('body.embedded[data-art-ready=true]').waitFor({ timeout: 60000 })
  assert.equal(await frame().locator('aside').isVisible(), false)
  assert.equal(await frame().locator('header').isVisible(), false)
  const scene = await frame().locator('#scene').boundingBox(), frameBox = await page.locator('.vp-pane iframe').boundingBox()
  assert.ok(scene.width >= frameBox.width - 2 && scene.height >= frameBox.height * 0.8, 'the scene fills the embedded frame')
  await page.waitForTimeout(300)
  await page.screenshot({ path: output + '/embedded-codecraft.png' })

  // Full screen opens the same view in the host, with its full-screen layout.
  await page.locator('.vp-pane-actions').getByRole('button', { name: /全螢幕/ }).click()
  await page.locator('.vp-host iframe').waitFor()
  await page.frameLocator('.vp-host iframe').locator('header').waitFor({ state: 'visible', timeout: 60000 })
  assert.equal(await page.frameLocator('.vp-host iframe').locator('body.embedded').count(), 0)
  await page.locator('.vp-host').getByRole('button', { name: '關閉視圖並返回' }).click()
  await page.locator('.vp-host').waitFor({ state: 'detached' })

  // A phone has no pane: cards in two columns and no pin control.
  await page.setViewportSize({ width: 390, height: 844 })
  await page.locator('.vp-dock').click()
  await page.locator('.vp-view-card').nth(1).waitFor()
  assert.equal(await page.locator('.vp-launch-pin').count(), 0, 'a phone offers no pinning')
  assert.equal(await page.locator('.vp-badge-pinned').count(), 0, 'a phone has no pane, so nothing is shown as pinned')
  assert.ok(await page.evaluate(() => document.querySelector('.vp-manager').scrollWidth <= innerWidth), 'the phone launcher has no sideways scroll')
  await settled()
  await page.screenshot({ path: output + '/launcher-phone.png' })
  await page.getByRole('button', { name: '關閉', exact: true }).click()
  await page.locator('.vp-host').waitFor({ state: 'detached' })
  await page.setViewportSize({ width: 1440, height: 900 })

  // The header's remove control gives the pane back.
  await page.locator('.vp-pane-unpin').click()
  await page.locator('.vp-pane').waitFor({ state: 'detached' })
  assert.deepEqual(errors, [])
  console.log('Embedded pane: empty launcher, cards, pin, embedded layouts, open-and-return, Escape, picker menu, browse, full screen, phone and remove passed')
} finally { await browser.close(); await new Promise(r => server.close(r)) }
