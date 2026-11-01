// Actual PairPanel lifecycle: keep following after handover and reveal the conversation.
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { writeFileSync, readFileSync, unlinkSync, existsSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { resolve } from 'node:path'
import { once } from 'node:events'
const root = fileURLToPath(new URL('../../', import.meta.url))
const fixture = resolve(root, 'pair-auto-fixture.html')
const entry = resolve(root, 'pair-auto-fixture.tsx')
const config = resolve(root, 'pair-auto-vite.config.mjs')
test('assisted pairing closes only after matching helper evidence and handover, in either order', async () => {
  const { chromium, webkit } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright')
  assert.ok(!existsSync(fixture) && !existsSync(entry))
  let vite
  const browsers = []
  try {
    writeFileSync(fixture, '<!doctype html><meta name="viewport" content="width=device-width,initial-scale=1"><div id="root"></div><script type="module" src="/pair-auto-fixture.tsx"></script>')
    writeFileSync(entry, `
import React from 'react';import {createRoot} from 'react-dom/client';
import {PairPanel} from './src/cloud/PairPanel';import {pairingOfferDigest} from './src/cloud/pair-agent';
import {bootCatalog} from './src/catalog';import {loadStrings} from './src/legacy/bridge';
import './src/legacy/tokens.css';import './src/legacy/base.css';import './src/legacy/shell.css';import './src/legacy/list.css';import './src/legacy/pages.css';import './src/shell.css';
localStorage.setItem('ui_language',new URLSearchParams(location.search).get('lang')||'en');
await bootCatalog(tag=>'/catalogs/'+tag+'.json');await loadStrings(async()=>({}));
const fragment='synthetic-offer';const digest=await pairingOfferDigest(fragment);
const comparison={machine_id:'mac_target',offer_sha256:digest,viewer_fingerprint:'ABCD-EFGH-IJKL-MNOP',machine_fingerprint:'QRST-UVWX-YZ23-4567'};
window.progress={stage:'working',summary:'',failedReason:''};window.pairClosedCount=0;window.started=0;window.expireIn=600000;
window.finish=(override={})=>{window.progress={stage:'done',summary:'Finished',failedReason:'',comparison:{...comparison,...override}}};
window.missing=()=>{window.progress={stage:'done',summary:'Finished',failedReason:''}};
function Example(){const [state,setState]=React.useState({phase:'idle'});const [open,setOpen]=React.useState(true);
window.handover=()=>setState({phase:'paired',machineID:'mac_target',fingerprint:comparison.viewer_fingerprint,machineFingerprint:comparison.machine_fingerprint});
const close=()=>{window.pairClosedCount++;setOpen(false)};
return <><div id="conversation">Conversation remains mounted</div>{open&&<div role="dialog" aria-modal="true" className="cloud-gate-ask"><PairPanel request={{mode:'offer',machine:{id:'mac_target',name:'Target'}}} state={state} nameOf={()=> 'Target'}
onBegin={()=>setState({phase:'waiting',fragment,fingerprint:comparison.viewer_fingerprint,expiresAt:Date.now()+window.expireIn})} onStop={()=>setState({phase:'stopped',expiresAt:null})} onAgain={()=>setState({phase:'idle'})} onClose={close} onPaired={close}
helpers={[{id:'mac_helper',name:'Helper'}]} onAgentStart={async()=>{window.started++;return {ok:true,mode:'agent',taskID:'task-one',trust:'recorded'}}}
onAgentStatus={async()=>window.progress} device="phone" onScan={()=>{}}/></div>}</>}
createRoot(document.getElementById('root')).render(<React.StrictMode><Example/></React.StrictMode>);
`)
    writeFileSync(config, "import react from '@vitejs/plugin-react';export default {plugins:[react()],publicDir:false}")
    vite = spawn(process.execPath, [resolve(root, '../node_modules/vite/bin/vite.js'), '--config', config, '--host', '127.0.0.1', '--port', '0'], { cwd: root, stdio: ['ignore', 'pipe', 'pipe'] })
    const origin = await new Promise((yes, no) => {
      let output='';const timer=setTimeout(()=>no(new Error('Vite startup timed out')),20000)
      vite.stdout.on('data',data=>{output+=data;const m=output.match(/http:\/\/127\.0\.0\.1:\d+/);if(m){clearTimeout(timer);yes(m[0])}})
      vite.stderr.on('data',data=>process.stderr.write(data));vite.once('exit',code=>{clearTimeout(timer);no(new Error('Vite exited '+code))})
    })
    for (const engine of [chromium, webkit]) {
      const browser=await engine.launch({headless:true,...(engine===chromium?{executablePath:process.env.CHROME||'/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'}:{})});browsers.push(browser)
      for (const lang of ['en','zh-Hant']) for (const width of [390,1280]) {
        const page=await browser.newPage({viewport:{width,height:844}});const errors=[]
        page.setDefaultTimeout(7000);page.on('pageerror',err=>{errors.push(String(err));process.stderr.write(String(err)+'\n')})
        await page.route('**/catalogs/*.json',route=>route.fulfill({contentType:'application/json',body:readFileSync(resolve(root,'public/catalogs',new URL(route.request().url()).pathname.split('/').pop()),'utf8')}))
        await page.route('**/v1/**',route=>route.fulfill({json:{}}))
        const load=async()=>{await page.goto(origin+'/pair-auto-fixture.html?lang='+lang);await page.locator('#cloud-pair-helper-go').waitFor()}
        const start=async()=>{await page.locator('#cloud-pair-helper-go').click();await page.waitForFunction(()=>window.started===1)}
        await load();await start()
        await page.evaluate(()=>window.handover())
        await page.locator('[data-pair-phase="paired"]').waitFor()
        assert.equal(await page.evaluate(()=>window.pairClosedCount),0,'handover alone does not finish')
        await page.evaluate(()=>window.finish())
        await page.waitForFunction(()=>window.pairClosedCount===1,{},{timeout:7000})
        assert.equal(await page.locator('[role="dialog"]').count(),0,'modal closes without a confirmation click')
        assert.equal(await page.locator('#conversation').isVisible(),true)
        await load();await start();await page.evaluate(()=>window.finish())
        await page.waitForFunction(()=>document.querySelector('#cloud-pair-progress')?.getAttribute('data-stage')==='done')
        assert.equal(await page.evaluate(()=>window.pairClosedCount),0,'helper success alone does not finish')
        await page.evaluate(()=>window.handover());await page.waitForFunction(()=>window.pairClosedCount===1)
        if(lang==='zh-Hant'&&width===390){
          await load();await page.evaluate(()=>window.handover());await page.locator('#cloud-pair-close').waitFor()
          assert.equal(await page.evaluate(()=>window.pairClosedCount),0,'manual pairing still requires comparison')
          await page.locator('#cloud-pair-close').click()
          await load();await page.evaluate(()=>{window.expireIn=200});await start();await page.evaluate(()=>window.handover())
          await page.getByRole('status').filter({hasText:'無法完成自動核對'}).waitFor()
          assert.equal(await page.evaluate(()=>window.pairClosedCount),0,'a stalled helper falls back at the original offer deadline')
          for(const failure of ['missing','fingerprint','offer','target']){
            await load();await start()
            await page.evaluate(failure=>failure==='missing'?window.missing():window.finish(failure==='fingerprint'?{machine_fingerprint:'ABCD-EFGH-IJKL-MNOP'}:failure==='offer'?{offer_sha256:'0'.repeat(64)}:{machine_id:'mac_other'}),failure)
            await page.waitForFunction(()=>document.querySelector('#cloud-pair-progress')?.getAttribute('data-stage')==='done')
            await page.evaluate(()=>window.handover());await page.locator('#cloud-pair-close').waitFor()
            assert.equal(await page.evaluate(()=>window.pairClosedCount),0,failure+' keeps manual comparison')
            await page.locator('#cloud-pair-close').click();assert.equal(await page.evaluate(()=>window.pairClosedCount),1)
          }
        }
        assert.deepEqual(errors,[])
        console.log(engine.name(),lang,width,'passed');await page.close()
      }
    }
  } finally {
    for(const browser of browsers)await browser.close()
    if(vite&&vite.exitCode===null){vite.kill('SIGTERM');await once(vite,'exit')}
    for(const path of [fixture,entry,config])if(existsSync(path))unlinkSync(path)
  }
})
