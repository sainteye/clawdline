/** Exercise curated installation through the production host and cookie-protected downloads. */
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
const sources=[{key:"test",label:"Test machine",async read(){return{sessions:[],connected:true}},subscribe(){return()=>{}}}];
function Host(){const api=useViewPlugins(sources,document.documentElement.lang);return <><button id="manager" onClick={e=>api.openManager(e.currentTarget)}>Manager</button>{api.dock}{api.content}</>};createRoot(document.getElementById('root')).render(<Host/>);
`, resolveDir: root + 'web/console/src', loader: 'tsx' }, bundle: true, write: false, outfile: 'host.js', format: 'esm', platform: 'browser', jsx: 'automatic', define: { 'process.env.NODE_ENV': '"development"' } })
const files = new Map(bundle.outputFiles.map(f => ['/' + f.path.split('/').at(-1), { bytes: f.contents, type: f.path.endsWith('.js') ? 'text/javascript' : 'text/css' }]))
files.set('/view-plugins/catalog.json', { bytes: await readFile(root + 'web/console/public/view-plugins/catalog.json'), type: 'application/json' })
for (const item of JSON.parse(await readFile(root + 'web/console/src/view-plugins/previews.json', 'utf8'))) {
  files.set(item.src, { bytes: await readFile(root + 'web/console/public' + item.src), type: 'image/webp' })
}
const views = [
  ['galaxy', 'Install Galaxy', '安裝星系'], ['orrery', 'Install Orrery', '安裝星象儀'],
  ['night-city', 'Install Night City', '安裝夜城'], ['departures', 'Install Departures', '安裝航班板'],
  ['territory', 'Install Territory', '安裝工作地圖'],
  ['codecraft', 'Install Codecraft', '安裝奇幻村莊'], ['kaiju', 'Install Kaiju City', '安裝怪獸城市'], ['lantern-deep', 'Install Lantern Deep', '安裝燈淵'],
]
const archives = new Map(await Promise.all(views.map(async ([slug]) => [slug, await readFile(root + 'web/console/public/view-plugins/session-' + slug + '.zip')])))
const requests = [], redirected = []
// Cookies are scoped by host rather than port. A cross-origin redirect must not
// carry the native cookie even when its destination is another local port.
const other = createServer((req, res) => {
  redirected.push({ cookie: !!req.headers.cookie })
  res.setHeader('Access-Control-Allow-Origin', '*')
  res.setHeader('Content-Type', 'application/zip')
  res.end(archives.get('galaxy'))
})
await new Promise(r => other.listen(0, '127.0.0.1', r))
const otherOrigin = `http://127.0.0.1:${other.address().port}`
const server = createServer((req, res) => {
  const url = new URL(req.url, 'http://fixture')
  const match = /^\/view-plugins\/session-([a-z-]+)\.zip$/.exec(url.pathname)
  if (match) {
    const cookie = req.headers.cookie?.includes('clawdline-next=synthetic-install') || false
    const mode = req.headers.referer?.includes('mode=public') ? 'public' : req.headers.referer?.includes('mode=redirect') ? 'redirect' : req.headers.referer?.includes('mode=denied') ? 'denied' : 'native'
    const status = mode === 'denied' || mode !== 'public' && !cookie ? 401 : 200
    requests.push({ slug: match[1], mode, cookie, status })
    if (status !== 200) { res.writeHead(status, { 'Content-Type': 'application/json' }); res.end('{"error":{"code":"unauthorized"}}'); return }
    if (mode === 'redirect') { res.writeHead(302, { Location: otherOrigin + '/package.zip' }); res.end(); return }
    res.writeHead(200, { 'Content-Type': 'application/zip', 'Cache-Control': 'no-store' }); res.end(archives.get(match[1])); return
  }
  const item = url.pathname === '/' ? { type: 'text/html', bytes: `<!doctype html><html lang="${url.searchParams.get('lang') === 'zh-Hant' ? 'zh-Hant' : 'en'}"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="stylesheet" href="/host.css"><div id="root"></div><script type="module" src="/host.js"></script>` } : files.get(url.pathname)
  if (!item) { res.writeHead(404); res.end(); return }
  res.setHeader('Content-Type', item.type); res.end(item.bytes)
})
await new Promise(r => server.listen(0, '127.0.0.1', r))
const origin = `http://127.0.0.1:${server.address().port}`
const pw = await import(pathToFileURL(process.env.PLAYWRIGHT_MODULE).href), results = []
async function installFromLauncher(page, language, screenshot) {
  const zh = language === 'zh-Hant'
  const entry = page.getByRole('button', { name: zh ? '開啟已安裝視圖' : 'Open installed views', exact: true })
  const recommended = page.getByRole('region', { name: zh ? '推薦視圖' : 'Recommended views' })
  const install = page.getByRole('button', { name: zh ? '安裝套件' : 'Install package', exact: true })
  const cancel = page.getByRole('button', { name: zh ? '取消' : 'Cancel', exact: true })
  const launches = page.locator('.vp-view-open')
  await entry.click()
  await recommended.waitFor({ timeout: 5000 })
  // The empty launcher's introduction is the only place it offers ZIP import; it never manages packages.
  assert.equal(await page.locator('input[type=file]').count(), await page.locator('.vp-hero input[type=file]').count())
  assert.equal(await page.locator('.vp-launcher .vp-menu-button').count(), 0)
  assert.equal(await recommended.getByRole('button').count(), 3)
  const previews = recommended.locator('img')
  assert.equal(await previews.count(), 3)
  await previews.evaluateAll(images => Promise.all(images.map(image => image.decode())))
  await page.screenshot({ path: screenshot })
  const choices = views.filter(([slug]) => slug === 'departures' || slug === 'codecraft' || slug === 'lantern-deep')
  const launchButtons = []
  for (const [slug, en, tw] of choices) {
    const button = recommended.getByRole('button', { name: zh ? tw : en, exact: true })
    const id = 'com.clawdline.session-' + slug
    const manifest = JSON.parse(await readFile(root + 'plugins/session-' + slug + '/manifest.json', 'utf8'))
    const launch = launches.filter({ has: page.getByText(manifest.name[language], { exact: true }) })
    launchButtons.push(launch)
    const review = page.locator('.vp-review[data-phase=review][data-package-id="' + id + '"]')
    await button.click()
    await review.waitFor()
    await review.evaluate(el => { if (document.activeElement !== el) throw new Error('Package review should take focus') })
    if (slug === 'departures') {
      await cancel.click()
      await review.waitFor({ state: 'detached' })
      await button.evaluate(el => { if (document.activeElement !== el) throw new Error('Cancel should restore focus') })
      assert.equal(await launches.count(), 0)
      await button.click()
      await review.waitFor()
    }
    await install.click()
    await page.locator('.vp-review[data-phase=installed]').waitFor()
    await launch.waitFor()
    assert.equal(await button.count(), 0, 'Installed views are removed from recommendations')
  }
  assert.equal(await recommended.count(), 0)
  await page.reload()
  await entry.click()
  await launches.nth(choices.length - 1).waitFor()
  assert.equal(await launches.count(), choices.length)
  for (const launch of launchButtons) {
    await launch.click()
    await page.locator('iframe').waitFor({ state: 'visible' })
    await page.locator('.vp-frame-loading').waitFor({ state: 'hidden' })
    await page.getByRole('button', { name: zh ? '關閉視圖並返回' : 'Close view and return', exact: true }).click()
    await page.getByRole('dialog').waitFor({ state: 'hidden' })
    await entry.click()
    await launches.nth(1).waitFor()
  }
}
try {
  for (const engine of process.env.INSTALL_ENGINE ? [process.env.INSTALL_ENGINE] : ['chromium', 'webkit']) {
    const browser = await pw[engine].launch({ headless: true })
    try {
      for (const language of process.env.INSTALL_LANGUAGE ? [process.env.INSTALL_LANGUAGE] : ['en', 'zh-Hant']) {
        for (const mode of process.env.INSTALL_MODE ? [process.env.INSTALL_MODE] : ['native', 'public', 'redirect', 'denied', 'launcher-desktop', 'launcher-mobile']) {
          const context = await browser.newContext({ viewport: { width: mode === 'launcher-mobile' ? 390 : 1440, height: 900 }, reducedMotion: 'reduce' })
          if (mode !== 'public') await context.addCookies([{ name: 'clawdline-next', value: 'synthetic-install', url: origin, httpOnly: true, sameSite: 'Strict' }])
          const page = await context.newPage(), errors = [], start = requests.length, redirectStart = redirected.length
          page.on('pageerror', error => errors.push(error.message))
          try {
            await page.goto(origin + '/?mode=' + mode + '&lang=' + language)
            if (mode.startsWith('launcher-')) {
              await installFromLauncher(page, language, `${output}/${engine}-${language}-${mode}.png`)
              const actual = requests.slice(start)
              assert.deepEqual(actual.map(r => r.slug), ['departures', 'departures', 'codecraft', 'lantern-deep'])
              assert.ok(actual.every(r => r.cookie && r.status === 200))
              assert.deepEqual(errors, [])
              results.push({ engine, language, mode, installed: 2, requests: actual, passed: true })
              continue
            }
            await page.locator('#manager').click()
            await page.locator('input[type=file]:not([disabled])').waitFor()
            for (const [slug, en, zh] of mode === 'native' || mode === 'public' ? views : views.slice(0, 1)) {
              await page.getByRole('button', { name: language === 'en' ? en : zh, exact: true }).click()
              await page.locator('.vp-review[data-phase=review][data-package-id="com.clawdline.session-' + slug + '"],.vp-error').first().waitFor()
              if (mode === 'denied') {
                assert.match(await page.locator('.vp-error').innerText(), language === 'en' ? /download/i : /下載/)
                assert.equal(await page.locator('.vp-installed').count(), 0)
                break
              }
              assert.equal(await page.locator('.vp-review').count(), 1, 'A signed-in local download must reach package review: ' + (await page.locator('.vp-error').allTextContents()).join(' '))
              const manifest = JSON.parse(await readFile(root + 'plugins/session-' + slug + '/manifest.json', 'utf8'))
              assert.match(await page.locator('.vp-review').innerText(), new RegExp('com.clawdline.session-' + slug + ' · ' + manifest.version.replaceAll('.', '\\.')))
              const before = await page.locator('.vp-installed').count()
              await page.getByRole('button', { name: language === 'en' ? 'Install package' : '安裝套件', exact: true }).click()
              await page.locator('.vp-review[data-phase=installed]').waitFor()
              await page.locator('.vp-installed').nth(before).waitFor()
              await page.locator('input[type=file]:not([disabled])').waitFor()
            }
            const expected = mode === 'native' || mode === 'public' ? views.length : mode === 'denied' ? 0 : 1
            assert.equal(await page.locator('.vp-installed').count(), expected)
            await page.reload(); await page.locator('#manager').click(); await page.locator('input[type=file]:not([disabled])').waitFor()
            if (expected) await page.locator('.vp-installed').nth(expected - 1).waitFor()
            assert.equal(await page.locator('.vp-installed').count(), expected, 'Installed archives survive a normal reload')
            const actual = requests.slice(start)
            assert.ok(actual.length > 0)
            assert.ok(actual.every(r => mode === 'public' ? !r.cookie : r.cookie), 'Only same-origin native requests carry their own cookie')
            if (mode === 'redirect') assert.deepEqual(redirected.slice(redirectStart), [{ cookie: false }], 'The cookie is never forwarded to a different origin')
            assert.deepEqual(errors, [])
            await page.screenshot({ path: `${output}/${engine}-${language}-${mode}.png` })
            results.push({ engine, language, mode, installed: expected, requests: actual, passed: true })
          } catch (error) {
            await page.screenshot({ path: `${output}/${engine}-${language}-${mode}-failure.png` }); throw error
          } finally { await context.close() }
        }
      }
    } finally { await browser.close() }
  }
} finally {
  await Promise.all([new Promise(r => server.close(r)), new Promise(r => other.close(r))])
  await writeFile(output + '/results.json', JSON.stringify(results, null, 2))
}
console.log(JSON.stringify(results, null, 2))
