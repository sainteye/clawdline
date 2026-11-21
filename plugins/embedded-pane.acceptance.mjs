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
try {
  await page.goto(origin)
  assert.equal(await page.locator('.vp-pane').count(), 0, 'nothing is pinned yet: the pane keeps its home screen')
  await page.locator('#manager').click()
  for (const slug of ['departures', 'codecraft']) await install(slug)
  await page.getByRole('button', { name: '關閉', exact: true }).click()
  await page.locator('.vp-host').waitFor({ state: 'detached' })

  // Pin from the status-bar launcher; the host closes and the pane shows the view.
  await page.locator('.vp-dock').click()
  await page.locator('.vp-launch-row', { hasText: '航班板' }).getByRole('button', { name: '固定在右側' }).click()
  await page.locator('.vp-host').waitFor({ state: 'detached' })
  await frame().locator('body.embedded').waitFor()
  assert.equal(await frame().locator('#close').isVisible(), false, 'the package close is hidden when embedded')
  assert.equal(await page.locator('.vp-pane .vp-frame-bar').count(), 0, 'no host return bar inside the pane')
  const paneBox = await page.locator('.vp-pane').boundingBox()
  assert.ok(paneBox.x >= 360 && paneBox.width > 1000, 'the pane is the second column')
  assert.ok(await frame().locator('.row:not(.blank)').count() >= 6, 'every synthetic Session is on the board')
  await page.screenshot({ path: output + '/embedded-departures.png' })

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
  await page.locator('.vp-pane-switch select').selectOption({ label: '奇幻村莊' })
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

  // The header's remove control gives the pane back.
  await page.locator('.vp-pane-unpin').click()
  await page.locator('.vp-pane').waitFor({ state: 'detached' })
  assert.deepEqual(errors, [])
  console.log('Embedded pane: pin, embedded layouts, open-and-return, Escape, switch, full screen and remove passed')
} finally { await browser.close(); await new Promise(r => server.close(r)) }
