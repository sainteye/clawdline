// Exercise the actual pairing components without registering devices or changing access.
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { once } from 'node:events'
import { readFileSync, writeFileSync, existsSync, unlinkSync, mkdirSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { resolve } from 'node:path'

const root = fileURLToPath(new URL('../../', import.meta.url))
const paths = ['pair-intent-fixture.html', 'pair-intent-fixture.tsx', 'pair-intent-vite.config.mjs'].map(p => resolve(root, p))
test('Mac browser pairing leads with the app; native completion requires matching evidence', async () => {
  const { chromium, webkit } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright')
  const evidence = process.env.PAIRING_EVIDENCE
  if (evidence) mkdirSync(evidence, { recursive: true })
  const browsers = []
  let vite
  try {
    for (const path of paths) assert.equal(existsSync(path), false)
    writeFileSync(paths[0], '<!doctype html><meta name="viewport" content="width=device-width,initial-scale=1"><div id="root"></div><script type="module" src="/pair-intent-fixture.tsx"></script>')
    writeFileSync(paths[1], `
import React from 'react';import {createRoot} from 'react-dom/client';
import {PairPanel} from './src/cloud/PairPanel';import {Welcome} from './src/setup/SetupGuide';
import {LocalMachineSetup} from './src/cloud/LocalMachineSetup';
import {bootCatalog} from './src/catalog';import {loadStrings} from './src/legacy/bridge';
import './src/legacy/tokens.css';import './src/legacy/base.css';import './src/legacy/door.css';import './src/legacy/shell.css';import './src/legacy/pages.css';import './src/shell.css';import './src/cloud/cloud.css';
const q=new URLSearchParams(location.search);const mode=q.get('mode');
Object.defineProperty(navigator,'platform',{value:'MacIntel',configurable:true});
localStorage.setItem('ui_language',q.get('lang')||'en');
const viewer='ABCD-EFGH-IJKL-MNOP',machine='QRST-UVWX-YZ23-4567';
window.requests=[];window.pairClosedCount=0;window.started=0;window.release=null;
if(mode==='native'||mode==='setup')window.webkit={messageHandlers:{clawdlinePairLocal:{postMessage:async body=>{
window.requests.push(body);if(body.kind)return {ok:true,native_setup:true,configured:true,enabled:true,machine_id:'mac_target',machine_name:'My Mac'};
return await new Promise(resolve=>{window.release=(override={})=>resolve({ok:true,machine_id:'mac_target',pairing:{phase:'paired',viewer_fingerprint:viewer,machine_fingerprint:machine},...override})})}}}};
await bootCatalog(tag=>'/catalogs/'+tag+'.json');await loadStrings(async()=>({}));
function Example(){const [state,setState]=React.useState({phase:'idle'});const [open,setOpen]=React.useState(true);
window.handover=(override={})=>setState({phase:'paired',machineID:'mac_target',fingerprint:viewer,machineFingerprint:machine,...override});
window.reset=()=>{setState({phase:'idle'});setOpen(true)};
if(mode==='welcome')return <Welcome inApp onStart={()=>{}} onLocal={()=>{}}/>;
if(mode==='setup')return <LocalMachineSetup onPair={()=>{}}/>;
return <div className="door" data-step="cloud" data-cloud-screen="pairing"><div className="door-card"><div className="door-head"><b>clawdline</b></div><section data-step="cloud">{open?<PairPanel device="desktop" request={{mode:'offer',machine:mode==='native'?{id:'mac_target',name:'My Mac'}:null,startLocal:mode==='native'}} state={state}
nameOf={()=> 'My Mac'} onBegin={()=>{window.started++;setState({phase:'waiting',fragment:'synthetic-offer',fingerprint:viewer,expiresAt:Date.now()+600000})}} onStop={()=>setState({phase:'stopped',expiresAt:null})} onAgain={()=>setState({phase:'idle'})}
onClose={()=>setOpen(false)} onPaired={()=>{window.pairClosedCount++;setOpen(false)}} onScan={()=>{}}/>:<p id="ready">Ready</p>}</section></div></div>}
createRoot(document.getElementById('root')).render(<React.StrictMode><Example/></React.StrictMode>);
`)
    writeFileSync(paths[2], "import react from '@vitejs/plugin-react';export default {plugins:[react()],publicDir:false}")
    vite = spawn(process.execPath, [resolve(root, '../node_modules/vite/bin/vite.js'), '--config', paths[2], '--host', '127.0.0.1', '--port', '0'], { cwd: root, stdio: ['ignore', 'pipe', 'pipe'] })
    const origin = await new Promise((yes, no) => {
      let output = ''
      const timer = setTimeout(() => no(new Error('Vite startup timed out')), 20000)
      vite.stdout.on('data', data => { output += data; const match = output.match(/http:\/\/127\.0\.0\.1:\d+/); if (match) { clearTimeout(timer); yes(match[0]) } })
      vite.stderr.on('data', data => process.stderr.write(data))
      vite.once('exit', code => { clearTimeout(timer); no(new Error('Vite exited ' + code)) })
    })
    for (const engine of [chromium, webkit]) {
      const browser = await engine.launch({ headless: true, ...(engine === chromium ? { executablePath: process.env.CHROME || '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome' } : {}) })
      browsers.push(browser)
      for (const lang of ['en', 'zh-Hant']) for (const width of [390, 1280]) {
        const page = await browser.newPage({ viewport: { width, height: 900 } })
        page.setDefaultTimeout(5000)
        const errors = []
        page.on('pageerror', e => { errors.push(String(e)); process.stderr.write(String(e) + '\n') })
        await page.route('**/catalogs/*.json', route => route.fulfill({ contentType: 'application/json', body: readFileSync(resolve(root, 'public/catalogs', new URL(route.request().url()).pathname.split('/').pop()), 'utf8') }))
        await page.route('**/v1/**', route => route.fulfill({ json: {} }))
        const load = async mode => { await page.goto(origin + '/pair-intent-fixture.html?lang=' + lang + '&mode=' + mode) }
        const primary = () => page.locator('.go:visible')
        await load('browser')
        await page.locator('#cloud-pair-go').waitFor()
        assert.equal(await primary().count(), 1)
        assert.equal(await page.locator('#cloud-pair-code:visible').count(), 0)
        assert.equal(await page.locator('.cloud-pair-manual').first().getAttribute('open'), null)
        if (evidence) await page.screenshot({ path: resolve(evidence, engine.name() + '-' + lang + '-' + width + '-start.png') })
        await page.locator('#cloud-pair-go').click()
        const link = page.locator('#cloud-pair-open-app')
        await link.waitFor()
        const url = new URL(await link.getAttribute('href'))
        assert.equal(url.protocol, 'clawdline-next:')
        assert.equal(url.host, 'pair-browser')
        assert.equal(url.searchParams.get('offer'), 'synthetic-offer')
        assert.equal(await primary().count(), 1)
        assert.equal(await page.locator('#cloud-pair-code:visible').count(), 0)
        if (evidence) await page.screenshot({ path: resolve(evidence, engine.name() + '-' + lang + '-' + width + '-open-app.png') })
        await page.evaluate(() => window.handover())
        await page.locator('#cloud-pair-machine-key').waitFor()
        assert.equal(await page.evaluate(() => window.pairClosedCount), 0, 'a browser still compares the codes')
        await page.locator('#cloud-pair-close').click()
        assert.equal(await page.evaluate(() => window.pairClosedCount), 1)
        await load('browser')
        await page.locator('input[type=radio]').nth(1).check()
        await page.locator('#cloud-pair-go').click()
        await page.locator('#cloud-pair-code:visible').waitFor()
        assert.equal(await page.locator('#cloud-pair-open-app:visible').count(), 0)
        for (const order of ['bridge-first', 'handover-first']) {
          await load('native')
          await page.waitForFunction(() => typeof window.release === 'function')
          assert.equal(await page.evaluate(() => window.started), 1, 'native action starts once under StrictMode')
          assert.equal(await page.evaluate(() => window.requests.filter(r => r.offer).length), 1)
          if (order === 'bridge-first') { await page.evaluate(() => window.release()); await page.waitForTimeout(50) }
          else { await page.evaluate(() => window.handover()); await page.locator('#cloud-pair-machine-key').waitFor() }
          assert.equal(await page.evaluate(() => window.pairClosedCount), 0, 'one witness alone never completes')
          await page.evaluate(order => order === 'bridge-first' ? window.handover() : window.release(), order)
          await page.locator('#ready').waitFor()
          assert.equal(await page.evaluate(() => window.pairClosedCount), 1)
        }
        for (const failure of ['missing', 'fingerprint', 'target', 'cancelled']) {
          await load('native'); await page.waitForFunction(() => typeof window.release === 'function')
          await page.evaluate(failure => window.release(failure === 'missing' ? { pairing: {} } : failure === 'fingerprint' ? { pairing: { phase: 'paired', viewer_fingerprint: 'WRONG', machine_fingerprint: 'WRONG' } } : failure === 'target' ? { machine_id: 'mac_other' } : { ok: false, error: 'cancelled' }), failure)
          if (failure === 'cancelled') await page.getByRole('alert').waitFor()
          else { await page.evaluate(() => window.handover()); await page.locator('#cloud-pair-machine-key').waitFor() }
          assert.equal(await page.evaluate(() => window.pairClosedCount), 0, failure + ' cannot automatically complete')
        }
        await load('setup'); await page.locator('button.go').waitFor()
        assert.equal(await primary().count(), 1)
        await load('welcome'); await page.locator('.setup-primary').waitFor()
        assert.equal(await page.locator('.setup-primary:visible').count(), 1)
        assert.deepEqual(errors, [])
        assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'no horizontal overflow')
        console.log(engine.name(), lang, width, 'passed')
        await page.close()
      }
    }
  } finally {
    for (const browser of browsers) await browser.close()
    if (vite && vite.exitCode === null) { vite.kill('SIGTERM'); await once(vite, 'exit') }
    for (const path of paths) if (existsSync(path)) unlinkSync(path)
  }
})
