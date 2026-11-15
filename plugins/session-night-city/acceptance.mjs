/** Local synthetic acceptance through the production manager, registry, frame and SDK. */
import assert from 'node:assert/strict'
import {createServer} from 'node:http'
import {readFile,writeFile,mkdir} from 'node:fs/promises'
import {fileURLToPath,pathToFileURL} from 'node:url'
import {createRequire} from 'node:module'
import {resolve} from 'node:path'
import {createHash} from 'node:crypto'
const root=fileURLToPath(new URL('../../',import.meta.url)),require=createRequire(root+'web/package.json'),{build}=require('esbuild')
const artifacts=resolve(process.env.PLUGIN_ARTIFACTS||'/tmp/session-night-city-acceptance')
if(!process.env.PLAYWRIGHT_MODULE)throw Error('Set PLAYWRIGHT_MODULE to an existing local Playwright module')
await mkdir(artifacts,{recursive:true})
const built=await build({stdin:{contents:`
import React,{useState} from 'react';import{createRoot}from'react-dom/client';
import{useViewPlugins}from'./view-plugins/Host';import{setDockAvailable}from'./project-assistants/BotOrb';setDockAvailable(true);
const states=['ready','working','holding','waiting_you','waiting_session','unknown','milestone_complete','work_complete'];
const titles=['Prepare next review','Build the console','Hold for design review','Approve the release notes','Wait for integration','Investigate unavailable status','Report a milestone','Report work complete'];
const original=states.map((workState,i)=>({key:'s'+i,title:titles[i],project:{key:'p'+i%3,label:['Orion','Harbor','Lumen'][i%3]},workState,freshness:'fresh'}));
original.push({key:'stale',title:'Old background reading',project:{key:'p0',label:'Orion'},workState:'working',freshness:'stale'},{key:'unknown',title:'Unassigned reading',project:null,workState:'working',freshness:'unknown'},{key:'stale-needs',title:'Earlier approval request',project:null,workState:'waiting_you',freshness:'stale'});
let listeners=new Set();window.fixture={rows:[...original],navigations:[],connected:true,original,change(){for(const f of listeners)f()},reset(){this.rows=[...original];this.connected=true;this.change()},hidden(value){Object.defineProperty(document,'visibilityState',{configurable:true,value:value?'hidden':'visible'});document.dispatchEvent(new Event('visibilitychange'))}};
function Host(){const api=useViewPlugins([{key:'synthetic',label:'Synthetic fixture only',async read(){return{sessions:fixture.rows,connected:fixture.connected}},subscribe(fn){listeners.add(fn);return()=>listeners.delete(fn)},async prepareOpen(key){return()=>fixture.navigations.push(key)}}],document.documentElement.lang);return <><p>LOCAL SYNTHETIC FIXTURE — no personal data or daemon</p><button id='manager' onClick={e=>api.openManager(e.currentTarget)}>Manage candidate</button>{api.dock}{api.content}</>};createRoot(document.getElementById('root')).render(<Host/>);
`,resolveDir:root+'web/console/src',loader:'tsx'},bundle:true,write:false,outfile:'host.js',format:'esm',platform:'browser',jsx:'automatic',define:{'process.env.NODE_ENV':'"development"'}})
const files=new Map(built.outputFiles.map(f=>['/'+f.path.split('/').at(-1),f.text]))
const archive=await readFile(root+'web/console/public/view-plugins/session-night-city.zip')
// Only the inspected pure model is evaluated; upstream native renderer is never executed.
const upstream=await import('data:text/javascript;base64,'+Buffer.from(await readFile(root+'plugins/session-night-city/upstream/model.js')).toString('base64'))
const originalPreview=process.env.NIGHT_CITY_ORIGINAL_PREVIEW
if(!originalPreview)throw Error('Set NIGHT_CITY_ORIGINAL_PREVIEW to the pinned original preview')
const originalPNG=await readFile(originalPreview)
await writeFile(artifacts+'/upstream-original.png',originalPNG)

const server=createServer((req,res)=>{const u=new URL(req.url,'http://test');const body=u.pathname==='/'?`<!doctype html><html lang="${u.searchParams.get('lang')==='zh-Hant'?'zh-Hant':'en'}"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="stylesheet" href="/host.css"><div id="root"></div><script type="module" src="/host.js"></script></html>`:files.get(u.pathname);if(body===undefined){res.writeHead(404);res.end();return}res.setHeader('Content-Type',u.pathname.endsWith('.js')?'text/javascript':u.pathname.endsWith('.css')?'text/css':'text/html');res.end(body)})
await new Promise(r=>server.listen(0,'127.0.0.1',r));const origin='http://127.0.0.1:'+server.address().port
const pw=await import(pathToFileURL(process.env.PLAYWRIGHT_MODULE).href),reports=[]
try{for(const engine of ['chromium','webkit'])for(const width of [1440,390]){
 const lang=width===390?'zh-Hant':'en',zh=lang!=='en',browser=await pw[engine].launch({headless:true}),page=await browser.newPage({viewport:{width,height:1000},reducedMotion:'no-preference'}),errors=[]
 page.on('pageerror',e=>errors.push(e.message));const prefix=engine+'-'+width+'-'+lang
 try{
  await page.goto(origin+'/?lang='+lang);await page.locator('#manager').click()
  await page.waitForFunction(()=>document.querySelector('input[type=file]')?.disabled===false)
  await page.locator('input[type=file]').setInputFiles({name:'session-night-city.zip',mimeType:'application/zip',buffer:archive})
  await page.getByRole('button',{name:zh?'安裝套件':'Install package',exact:true}).click()
  const candidate=page.locator('.vp-installed').filter({hasText:zh?'Session 夜城':'Session Night City'})
  await candidate.getByRole('button',{name:zh?/^開啟/:/^Open/}).click()
  const frame=page.frameLocator('iframe'),body=frame.locator('body')
  await frame.locator('.window').first().waitFor();assert.equal(await frame.locator('.window').count(),11)
  assert.equal(await frame.locator('.building').count(),4);assert.equal(await frame.locator('.legend>span').count(),8)
  assert.equal(await frame.locator('#attention .session-row').count(),2)
  assert.match(await frame.locator('#attention').innerText(),/Approve the release notes/)
  assert.match(await frame.locator('#attention').innerText(),/Earlier approval request/)
  assert.match(await frame.locator('#attention').innerText(),zh?/可能過時/:/May be stale/)
  assert.equal(await page.locator('iframe').getAttribute('sandbox'),'allow-scripts')
  const embedded=await frame.locator('img').evaluateAll(nodes=>[...new Set(nodes.map(n=>n.src))])
  const expectedAssets=await Promise.all(['moon','neon-sign','rooftop-sign','search-beam','tower-body'].map(async name=>'data:image/svg+xml;base64,'+(await readFile(root+'plugins/session-night-city/upstream/'+name+'.svg')).toString('base64')))
  assert.deepEqual([...embedded].sort(),expectedAssets.sort(),'All five rendered SVGs are byte-identical upstream assets')
  const authoredPalette=await body.evaluate(el=>({foreground:getComputedStyle(el).color,background:getComputedStyle(el).backgroundImage,working:getComputedStyle(el.querySelector('.window.working.fresh')).backgroundColor,needs:getComputedStyle(el.querySelector('.window.waiting_you.fresh')).backgroundColor}))
  assert.equal(authoredPalette.foreground,'rgb(226, 232, 246)');assert.equal(authoredPalette.working,'rgb(241, 207, 131)');assert.equal(authoredPalette.needs,'rgb(182, 59, 80)')
  await page.evaluate(()=>{document.documentElement.dataset.theme='light'})
  await body.evaluate(el=>new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r))))
  assert.deepEqual(await body.evaluate(el=>({foreground:getComputedStyle(el).color,background:getComputedStyle(el).backgroundImage,working:getComputedStyle(el.querySelector('.window.working.fresh')).backgroundColor,needs:getComputedStyle(el.querySelector('.window.waiting_you.fresh')).backgroundColor})),authoredPalette,'Source palette survives light host context')

  const overflow=()=>body.evaluate(el=>el.scrollWidth<=el.clientWidth+1&&document.documentElement.scrollWidth<=innerWidth+1)
  assert.equal(await overflow(),true,'No whole-page horizontal overflow')
  // Real measured DOM dimensions must match the upstream geometry on the documented single-bank adaptation.
  const measured=await frame.locator('#buildings').evaluate(el=>({width:el.getBoundingClientRect().width,height:el.getBoundingClientRect().height,k:Number(el.dataset.scale),bandWidth:document.getElementById('city-scroll').clientWidth,blocks:[...el.querySelectorAll('.building')].map(b=>({n:b.querySelectorAll('.window').length,width:b.getBoundingClientRect().width,height:b.getBoundingClientRect().height,body:b.querySelector('.tower-body').getBoundingClientRect().height,roof:b.querySelector('.roof').getBoundingClientRect().height,window:b.querySelector('.window').getBoundingClientRect().height,bottom:b.getBoundingClientRect().bottom}))}))
  const sourceSpaces=measured.blocks.map((b,i)=>({id:'p'+i,tabs:[{id:'bank'+i}]})),sourcePanes=measured.blocks.flatMap((b,i)=>Array.from({length:b.n},(_,j)=>({tab:'bank'+i,terminal:'s'+i+'-'+j})))
  const sourceBlocks=upstream.towers({spaces:sourceSpaces,panes:sourcePanes,mapped:new Map(),descendants:new Map()},[])
  const expected=upstream.skylineGeometry(sourceBlocks,{width:measured.bandWidth,height:width===390?260:320})
  const close=(a,b)=>assert.ok(Math.abs(a-b)<.15,`${a} should match source ${b}`)
  close(measured.k,expected.scale);close(measured.width,expected.width);close(measured.height,expected.height)
  measured.blocks.forEach((b,i)=>{close(b.width,sourceBlocks[i].width*expected.scale);close(b.height,(sourceBlocks[i].height+54)*expected.scale);close(b.body,sourceBlocks[i].height*expected.scale);close(b.roof,23*expected.scale);close(b.window,18*expected.scale);close(b.bottom,measured.blocks[0].bottom)})
  if(width===390){const box=await frame.locator('#city').boundingBox();assert.ok(box.y+box.height<500,'Compact skyline appears before the attention rail and below the mobile header')}
  for(const b of await frame.locator('#attention .session-row').all()){const box=await b.boundingBox();assert.ok(box.height>=44,'Named touch fallback')}
  await writeFile(artifacts+'/'+prefix+'-geometry.json',JSON.stringify({measured,expected,sourceBlocks},null,2))

  await page.screenshot({path:artifacts+'/'+prefix+'-city.png',fullPage:true})
  if(engine==='chromium'&&width===1440){
   const port=await page.locator('iframe').screenshot({path:artifacts+'/port-desktop.png'})
   const compare=await browser.newPage({viewport:{width:1600,height:560}})
   await compare.setContent(`<html><style>*{box-sizing:border-box}body{margin:0;background:#080e20;color:#e2e8f6;font:13px system-ui}.pair{display:flex}.pair>section{width:800px}h2,p{margin:10px 16px;font-size:13px}img{width:800px;height:450px;object-fit:contain;object-position:top}</style><div class="pair"><section><h2>Original · pinned Herdr Night City (1600 × 900)</h2><img src="data:image/png;base64,${originalPNG.toString('base64')}"></section><section><h2>Port · actual production iframe / synthetic v1 data (1440 px host)</h2><img src="data:image/png;base64,${port.toString('base64')}"></section></div><p>Same source art, palette, ground and geometry. Port: one unpartitioned Session window bank per Project; no native tabs, child windows, dead history or terminals are supplied. Missing terminal area is replaced by honest Session detail and a readable state legend.</p></html>`)
   await compare.screenshot({path:artifacts+'/original-versus-port.png',fullPage:true});await compare.close()
  }

  await frame.locator('#attention .session-row').filter({hasText:'Approve the release notes'}).click()
  assert.deepEqual(await page.evaluate(()=>fixture.navigations),[],'Selection does not navigate')
  await page.screenshot({path:artifacts+'/'+prefix+'-detail.png',fullPage:true})
  await frame.locator('#open-session').press('Escape');assert.equal(await frame.locator('#open-session').count(),0,'Escape clears selection before closing')
  assert.equal(await page.locator('iframe').count(),1)
  await frame.locator('#attention .session-row').filter({hasText:'Approve the release notes'}).click()
  const selectedRef=await frame.locator('.window[aria-pressed=true]').getAttribute('data-ref')
  const order=await frame.locator('.window').evaluateAll(nodes=>nodes.map(n=>n.dataset.ref))
  await page.evaluate(()=>{fixture.rows=[...fixture.rows].reverse().map(r=>r.key==='s3'?{...r,title:'<img src=x onerror=alert(1)>'+'Very long synthetic title '.repeat(7),project:{...r.project,label:'<svg onload=alert(2)>'+'超長專案'.repeat(20)}}:r);fixture.change()})
  await frame.locator('#detail h3').filter({hasText:'<img src=x onerror=alert(1)>'}).waitFor()
  assert.equal(await frame.locator('img:not([src^="data:image/svg+xml;base64,"])').count(),0,'Only embedded upstream images exist; hostile title stays text')
  assert.equal(await frame.locator('svg[onload]').count(),0,'Hostile Project is text')
  assert.equal(await overflow(),true,'Long hostile labels stay contained')
  assert.equal(await frame.locator('.window[aria-pressed=true]').getAttribute('data-ref'),selectedRef)
  assert.deepEqual(await frame.locator('.window').evaluateAll(nodes=>nodes.map(n=>n.dataset.ref)),order,'Snapshot reordering preserves skyline order')
  await frame.locator('#list').click();assert.equal(await frame.locator('#all-list .session-row').count(),11)
  for(const b of await frame.locator('#all-list .session-row').all()){const box=await b.boundingBox();assert.ok(box.height>=44)}
  assert.equal(await frame.locator('#all-list .session-row[aria-pressed=true]').getAttribute('data-ref'),selectedRef)
  await frame.locator('#all-list .session-row').filter({hasText:'Investigate unavailable status'}).click();assert.equal(await frame.locator('#open-session').isDisabled(),true,'Unknown work state never navigates')
  await frame.locator('#all-list .session-row').filter({hasText:'Unassigned reading'}).click();assert.equal(await frame.locator('#open-session').isDisabled(),true)
  await frame.locator('#all-list .session-row').filter({hasText:'Old background reading'}).click();assert.equal(await frame.locator('#open-session').isDisabled(),true)
  await page.evaluate(()=>fixture.reset());await frame.locator('#all-list').getByText('Approve the release notes',{exact:true}).waitFor()
  await page.screenshot({path:artifacts+'/'+prefix+'-list.png',fullPage:true})
  await frame.locator('#list').click()
  const animated=()=>frame.locator('.window.working.fresh .glyph').first().evaluate(el=>el.getAnimations().filter(a=>a.playState==='running').length)
  assert.ok(await animated()>0,'Visible fresh working windows animate')
  for(const b of await frame.locator('.window.working.stale .glyph').all())assert.equal(await b.evaluate(el=>el.getAnimations().length),0)
  await frame.locator('#still').click();assert.equal(await animated(),0,'Still mode removes animation')
  await frame.locator('#still').click();await page.emulateMedia({reducedMotion:'reduce'})
  await body.evaluate(el=>new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r))));assert.equal(await animated(),0,'Reduced motion removes animation')
  await page.emulateMedia({reducedMotion:'no-preference'});await body.evaluate(el=>new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r))));assert.ok(await animated()>0)
  await page.evaluate(()=>fixture.hidden(true));await body.evaluate(el=>new Promise(r=>setTimeout(r,60)));assert.equal(await animated(),0,'Hidden context removes animation')
  await page.evaluate(()=>fixture.hidden(false));await body.evaluate(el=>new Promise(r=>setTimeout(r,60)));assert.ok(await animated()>0)
  await page.evaluate(()=>{fixture.connected=false;fixture.change()});await frame.locator('#connection').filter({hasText:zh?'已斷線':'Disconnected'}).waitFor()
  assert.equal(await frame.locator('.window.working.fresh').count(),0,'Disconnected retained rows stop working motion')
  await frame.locator('.window.working').first().click();assert.equal(await frame.locator('#open-session').isDisabled(),true)
  await page.evaluate(()=>fixture.reset());await frame.locator('#connection').filter({hasText:zh?'已連線':'Connected'}).waitFor()
  await page.evaluate(()=>{fixture.rows=Array.from({length:80},(_,i)=>({key:'dense'+i,title:'Synthetic Session '+i,project:{key:'dense-p'+i%10,label:'Synthetic Project '+i%10},workState:i%2?'working':'waiting_you',freshness:'fresh'}));fixture.change()})
  await frame.locator('.window').nth(79).waitFor({state:'attached'});assert.equal(await frame.locator('.window').count(),80);assert.equal(await overflow(),true);assert.equal(await frame.locator('#attention .session-row').count(),40,'Every waiting_you row remains in the named rail');assert.equal(await frame.locator('.window.pulse').count(),3,'Source bounds lamp pulsing to three fresh blockers')
  await page.evaluate(()=>{fixture.rows=[1,8,16].flatMap((count,p)=>Array.from({length:count},(_,i)=>({key:'geometry-'+p+'-'+i,title:'Geometry Session '+i,project:{key:'geometry-project-'+p,label:'Geometry Project '+p},workState:'ready',freshness:'fresh'})));fixture.change()})
  await frame.locator('.window').nth(24).waitFor({state:'attached'});await page.waitForFunction(()=>fixture.rows.length===25)
  await frame.locator('.window').first().evaluate(el=>new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r))))
  const fixtureGeometry=await frame.locator('#buildings').evaluate(el=>({k:Number(el.dataset.scale),width:el.getBoundingClientRect().width,height:el.getBoundingClientRect().height,bandWidth:document.getElementById('city-scroll').clientWidth,blocks:[...el.querySelectorAll('.building')].map(b=>({count:b.querySelectorAll('.window').length,width:b.getBoundingClientRect().width,height:b.getBoundingClientRect().height,glass:b.querySelector('.window').getBoundingClientRect().width}))}))
  assert.equal(fixtureGeometry.blocks.reduce((n,b)=>n+b.count,0),25)
  const fixtureSpaces=fixtureGeometry.blocks.map((b,i)=>({id:'f'+i,tabs:[{id:'fb'+i}]})),fixturePanes=fixtureGeometry.blocks.flatMap((b,i)=>Array.from({length:b.count},(_,j)=>({tab:'fb'+i,terminal:'f'+i+'-'+j})))
  const fixtureBlocks=upstream.towers({spaces:fixtureSpaces,panes:fixturePanes,mapped:new Map(),descendants:new Map()},[]),fixtureExpected=upstream.skylineGeometry(fixtureBlocks,{width:fixtureGeometry.bandWidth,height:width===390?260:320})
  close(fixtureGeometry.k,fixtureExpected.scale);close(fixtureGeometry.width,fixtureExpected.width);close(fixtureGeometry.height,fixtureExpected.height)
  fixtureGeometry.blocks.forEach((b,i)=>{const g=fixtureBlocks[i];close(b.width,g.width*fixtureExpected.scale);close(b.height,(g.height+54)*fixtureExpected.scale);close(b.glass,fixtureExpected.scale*Math.max(10,Math.min(24,(g.width-22-4*Math.max(0,b.count-1))/b.count)))})
  await writeFile(artifacts+'/'+prefix+'-geometry-1-8-16.json',JSON.stringify({fixtureGeometry,fixtureExpected,fixtureBlocks},null,2))
  await page.evaluate(()=>{fixture.rows=Array.from({length:80},(_,i)=>({key:'dense'+i,title:'Synthetic Session '+i,project:{key:'dense-p'+i%10,label:'Synthetic Project '+i%10},workState:i%2?'working':'waiting_you',freshness:'fresh'}));fixture.change()})
  await frame.locator('.window').nth(79).waitFor({state:'attached'})
  await frame.locator('#list').click();assert.equal(await frame.locator('#all-list .session-row').count(),80)
  await page.evaluate(()=>{fixture.rows=[];fixture.change()});await frame.locator('#all-list .empty').waitFor()
  assert.equal(await frame.locator('.window').count(),0);assert.equal(await frame.locator('#open-session').count(),0)
  await page.evaluate(()=>fixture.reset());await frame.locator('#all-list .session-row').first().waitFor()
  await frame.locator('#all-list .session-row').filter({hasText:'Prepare next review'}).click()
  assert.deepEqual(await page.evaluate(()=>fixture.navigations),[])
  await frame.locator('#open-session').click();await page.locator('iframe').waitFor({state:'detached'})
  await page.waitForFunction(()=>fixture.navigations.length===1);assert.deepEqual(await page.evaluate(()=>fixture.navigations),['s0'])
  assert.deepEqual(errors,[]);reports.push({engine,width,language:lang,archiveSHA256:createHash('sha256').update(archive).digest('hex'),passed:true,cases:['production ZIP import','11 rows / 8 states / null Project','named stale blockers','source-measured tower / roof / window / scale / baseline geometry; 1/8/16-window branches','five byte-identical SVG assets / source palette / light host','three pulsing blocker lamps','44px named touch/list fallback','mobile skyline before rail','hostile text','snapshot order and identity','same-data readable list','stale and unknown navigation disabled','still / reduced / hidden / disconnected motion','80-row contained overflow','empty snapshot','explicit navigation only']})
 }catch(e){await page.screenshot({path:artifacts+'/'+prefix+'-failure.png',fullPage:true});throw e}finally{await browser.close()}
}}finally{await new Promise(r=>server.close(r));await writeFile(artifacts+'/results.json',JSON.stringify(reports,null,2)+'\n')}
console.log(JSON.stringify(reports,null,2))
