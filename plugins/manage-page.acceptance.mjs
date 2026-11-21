/** Manage views: production host, real packages, a synthetic official catalog and synthetic Sessions. */
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
  if (url.pathname === '/view-plugins/catalog.json') {
    // One official release newer than the installed board, so its row offers the update label.
    const sha = 'a'.repeat(64)
    res.setHeader('Content-Type', 'application/json'); res.end(JSON.stringify({ schemaVersion: 1, packages: [{ id: 'com.clawdline.session-departures', version: '9.0.0',
      url: '/view-plugins/session-departures.zip', sha256: sha, bytes: 1000, hostApi: 1, capabilities: ['sessions.read', 'sessions.open'], knownDigests: [sha] }] })); return
  }
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
  // Nothing installed: an introduction, ZIP import, the author guide and every official view.
  await page.locator('#manager').click()
  await page.locator('.vp-manage-empty').waitFor()
  assert.equal(await page.locator('.vp-manage-empty a[href*="docs/plugins/view-plugin-v1.md"]').count(), 1, 'the empty page links to the public author guide')
  assert.ok(await page.locator('.vp-example').count() >= 6, 'every official view is offered')
  await settled(); await page.screenshot({ path: output + '/manage-empty.png' })

  // A ZIP review lists what the package may and may not do, and dims the list behind it.
  await install('codecraft')
  await page.locator('input[type=file]:not([disabled])').setInputFiles({ name: 'd.zip', mimeType: 'application/zip', buffer: await readFile(root + 'web/console/public/view-plugins/session-departures.zip') })
  await page.locator('.vp-review[data-phase=review]').waitFor()
  assert.equal(await page.locator('.vp-permissions li[data-allowed=false]').count(), 1, 'the review states what a package cannot do')
  assert.equal(await page.locator('.vp-library.vp-dimmed').count(), 1, 'the list is dimmed during review')
  await settled(); await page.screenshot({ path: output + '/manage-review.png' })
  await page.getByRole('button', { name: '安裝套件', exact: true }).click()
  await page.locator('.vp-review[data-phase=installed]').waitFor()

  // Rows: the newer official release is the version label itself, a switch hides a view, the menu removes.
  const board = page.locator('.vp-installed[data-package-id="com.clawdline.session-departures"]')
  await board.locator('.vp-update-button').waitFor()
  assert.match(await board.locator('.vp-update-button').textContent(), /有新版本 v9\.0\.0/)
  assert.equal(await page.locator('.vp-update-summary[data-state=available]').count(), 1, 'the summary counts the update')
  const village = page.locator('.vp-installed[data-package-id="com.clawdline.session-codecraft"]')
  await village.getByRole('switch').click()
  await page.locator('.vp-installed[data-package-id="com.clawdline.session-codecraft"][data-enabled=false]').waitFor()
  assert.equal(await page.locator('.vp-examples .vp-example', { hasText: '奇幻村莊' }).count(), 0, 'installed views are not offered again')
  await board.getByRole('button', { name: '管理', exact: true }).click()
  await page.getByRole('menuitem', { name: '移除' }).waitFor()
  await settled(); await page.screenshot({ path: output + '/manage-list.png' })
  await page.keyboard.press('Escape')
  assert.equal(await page.locator('.vp-host').count(), 1, 'Escape closes only the menu')

  // A phone: the same compact list; the row menu also opens the view.
  await page.setViewportSize({ width: 390, height: 844 })
  assert.equal(await board.locator('.vp-row-open').isVisible(), true)
  assert.ok(await page.evaluate(() => document.querySelector('.vp-manager').scrollWidth <= innerWidth), 'no sideways scroll on a phone')
  await page.screenshot({ path: output + '/manage-phone.png' })
  await board.getByRole('button', { name: '管理', exact: true }).click()
  await page.getByRole('menuitem', { name: '開啟' }).click()
  await page.locator('.vp-host iframe').waitFor()
  assert.deepEqual(errors, [])
  console.log('Manage views: empty page, review permissions, update label, switch, menu, phone and open passed')
} finally { await browser.close(); await new Promise(r => server.close(r)) }
