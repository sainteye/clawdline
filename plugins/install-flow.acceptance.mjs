/** Measure attention and layout through the production view installer, using synthetic storage. */
import assert from 'node:assert/strict'
import { createServer } from 'node:http'
import { readFile, mkdir, writeFile } from 'node:fs/promises'
import { createRequire } from 'node:module'
import { fileURLToPath, pathToFileURL } from 'node:url'
const root = fileURLToPath(new URL('../', import.meta.url))
const require = createRequire(root + 'web/package.json'), { build } = require('esbuild')
const output = process.env.PLUGIN_ARTIFACTS
if (!output || !process.env.PLAYWRIGHT_MODULE) throw Error('Set PLUGIN_ARTIFACTS and PLAYWRIGHT_MODULE; no dependency installation is performed')
await mkdir(output, { recursive: true })
const bundle = await build({ stdin: { contents: `
import React from 'react';import{createRoot}from'react-dom/client';import{useViewPlugins}from'./view-plugins/Host';
function Host(){const api=useViewPlugins([],document.documentElement.lang);return <><button id="manager" onClick={e=>api.openManager(e.currentTarget)}>Manager</button>{api.content}</>};createRoot(document.getElementById('root')).render(<Host/>);
`, resolveDir: root + 'web/console/src', loader: 'tsx' }, bundle: true, write: false, outfile: 'host.js', format: 'esm', platform: 'browser', jsx: 'automatic', define: { 'process.env.NODE_ENV': '"development"' } })
const files = new Map(bundle.outputFiles.map(f => ['/' + f.path.split('/').at(-1), { bytes: f.contents, type: f.path.endsWith('.js') ? 'text/javascript' : 'text/css' }]))
for (const item of JSON.parse(await readFile(root + 'web/console/src/view-plugins/previews.json', 'utf8'))) {
  files.set(item.src, { bytes: await readFile(root + 'web/console/public' + item.src), type: 'image/webp' })
}
const views = [
  ['galaxy', 'Install Galaxy', '安裝星系'], ['orrery', 'Install Orrery', '安裝星象儀'],
  ['night-city', 'Install Night City', '安裝夜城'], ['departures', 'Install Departures', '安裝航班板'],
  ['territory', 'Install Territory', '安裝工作地圖'],
]
const archives = new Map(await Promise.all(views.map(async ([slug]) => [slug, await readFile(root + 'web/console/public/view-plugins/session-' + slug + '.zip')])))
const server = createServer((req, res) => {
  const url = new URL(req.url, 'http://fixture')
  const match = /^\/view-plugins\/session-([a-z-]+)\.zip$/.exec(url.pathname)
  if (match) {
    res.writeHead(200, { 'Content-Type': 'application/zip', 'Cache-Control': 'no-store' }); setTimeout(() => res.end(archives.get(match[1])), 400); return
  }
  const item = url.pathname === '/' ? { type: 'text/html', bytes: `<!doctype html><html lang="${url.searchParams.get('lang') === 'zh-Hant' ? 'zh-Hant' : 'en'}"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="stylesheet" href="/host.css"><div id="root"></div><script type="module" src="/host.js"></script>` } : files.get(url.pathname)
  if (!item) { res.writeHead(404); res.end(); return }
  res.setHeader('Content-Type', item.type); res.end(item.bytes)
})
await new Promise(r => server.listen(0, '127.0.0.1', r))
const origin = `http://127.0.0.1:${server.address().port}`
const pw = await import(pathToFileURL(process.env.PLAYWRIGHT_MODULE).href), results = []
try {
 for (const engine of process.env.INSTALL_ENGINE ? [process.env.INSTALL_ENGINE] : ['chromium', 'webkit']) {
  const browser = await pw[engine].launch({ headless: true })
  try { for (const width of [390, 1440]) for (const language of ['en', 'zh-Hant']) for (const motion of ['no-preference', 'reduce']) {
   const context = await browser.newContext({ viewport: { width, height: 800 }, reducedMotion: motion })
   const page = await context.newPage(), errors = [], zh=language==='zh-Hant'
   page.on('pageerror', e => errors.push(e.message))
   try {
    await page.goto(origin + '/?lang='+language)
    await page.locator('#manager').click()
    await page.locator('input[type=file]:not([disabled])').waitFor()
    await page.waitForFunction(() => [...document.querySelectorAll('.vp-example img')].every(i => i.complete))
    // A failed download should bring the actual error into view without changing storage.
    await page.route('**/view-plugins/session-night-city.zip',r=>r.fulfill({status:401,contentType:'application/json',body:'{}'}))
    await page.getByRole('button',{name:zh?'安裝夜城':'Install Night City',exact:true}).click()
    await page.locator('.vp-error').waitFor()
    await page.waitForTimeout(900)
    const alertBox=await page.locator('.vp-error').boundingBox()
    assert.ok(alertBox.y>=0&&alertBox.y+alertBox.height<800,'Failed downloads guide attention to the error')
    assert.equal(await page.locator('.vp-error').evaluate(e=>e===document.activeElement),true)
    assert.equal(await page.locator('.vp-installed').count(),0)
    await page.unroute('**/view-plugins/session-night-city.zip')
    const button = page.getByRole('button', {name:zh?'安裝工作地圖':'Install Territory', exact:true})
    await button.scrollIntoViewIfNeeded()
    await page.waitForTimeout(100)
    // Cancel returns to the originating card, ready for another attempt.
    await button.click();await page.locator('.vp-review').waitFor();await page.waitForTimeout(900)
    await page.getByRole('button',{name:zh?'取消':'Cancel',exact:true}).click()
    await page.locator('.vp-review').waitFor({state:'detached'});await page.waitForTimeout(900)
    assert.equal(await button.evaluate(e=>e===document.activeElement),true)
    const returned=await button.boundingBox()
    assert.ok(returned.y>=0&&returned.y+returned.height<800,'Cancel returns the original action to view')
    const before = await button.boundingBox(),opacityBefore=await button.evaluate(e=>getComputedStyle(e).opacity)
    await page.evaluate(() => {
     window.samples=[];window.sampling=true;
     const sample=()=>{const m=document.querySelector('.vp-manager');window.samples.push({top:m.scrollTop, review:!!document.querySelector('.vp-review')});if(window.sampling)requestAnimationFrame(sample)};requestAnimationFrame(sample)
    })
    await button.click()
    await page.waitForTimeout(150)
    const during = await button.boundingBox(),opacityDuring=await button.evaluate(e=>getComputedStyle(e).opacity)
    assert.equal(opacityDuring,opacityBefore,'Starting a download must not dim and flash every action')
    assert.ok(Math.abs(before.y-during.y)<2, 'Starting a download must not move the selected card: '+JSON.stringify({before:before.y,during:during.y}))
    await page.locator('.vp-review').waitFor()
    await page.waitForTimeout(900)
    const samples=await page.evaluate(()=>{window.sampling=false;return window.samples})
    const scrollPositions=[...new Set(samples.filter(s=>s.review).map(s=>Math.round(s.top)))]
    if(motion==='no-preference')assert.ok(scrollPositions.length>3,'Review should move smoothly through intermediate positions: '+JSON.stringify(scrollPositions))
    const review=page.locator('.vp-review'), manager=page.locator('.vp-manager')
    const reviewBefore=await review.boundingBox(), scrollBefore=await manager.evaluate(e=>e.scrollTop)
    assert.ok(reviewBefore.y>=0&&reviewBefore.y<180,'The review starts inside the viewport')
    assert.equal(await review.evaluate(e=>e.contains(document.activeElement)),true)
    await page.screenshot({path:`${output}/${engine}-${width}-${language}-${motion}-review.png`})
    await page.getByRole('button',{name:zh?'安裝套件':'Install package',exact:true}).click()
    await page.locator('.vp-installed').waitFor()
    await page.locator('.vp-review [data-install-result]').waitFor()
    const reviewAfter=await review.boundingBox(),scrollAfter=await manager.evaluate(e=>e.scrollTop)
    assert.ok(Math.abs(reviewBefore.y-reviewAfter.y)<2,'Completion keeps the result in the same panel')
    assert.ok(Math.abs(scrollBefore-scrollAfter)<2,'Completion must not jump the viewport')
    await page.screenshot({path:`${output}/${engine}-${width}-${language}-${motion}-installed.png`})
    await page.getByRole('button',{name:zh?'已安裝視圖':'Installed views',exact:true}).click()
    await page.waitForTimeout(900)
    const installed=await page.locator('.vp-installed').boundingBox()
    assert.ok(installed.y>=0&&installed.y+Math.min(installed.height,200)<=800,'The installed card is brought into view')
    assert.equal(await page.locator('.vp-installed').evaluate(e=>e.contains(document.activeElement)),true)
    await page.screenshot({path:`${output}/${engine}-${width}-${language}-${motion}.png`})
    assert.deepEqual(errors,[])
    results.push({engine,width,language,motion,downloadShift:during.y-before.y,reviewPositions:scrollPositions.length,completionShift:reviewAfter.y-reviewBefore.y,completionScroll:scrollAfter-scrollBefore,passed:true})
   } catch(e) {await page.screenshot({path:`${output}/${engine}-${width}-${language}-${motion}-failure.png`});throw e}
   finally{await context.close()}
  }} finally{await browser.close()}
 }
} finally {
 await new Promise(r=>server.close(r))
 await writeFile(output+'/results.json',JSON.stringify(results,null,2))
}
console.log(JSON.stringify(results,null,2))
