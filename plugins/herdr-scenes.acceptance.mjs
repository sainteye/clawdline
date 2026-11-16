/** Exercise both original-art ports through the production registry, frame and SDK. */
import assert from 'node:assert/strict'
import {createServer} from 'node:http'
import {readFile,writeFile,mkdir} from 'node:fs/promises'
import {fileURLToPath,pathToFileURL} from 'node:url'
import {createRequire} from 'node:module'
const root=fileURLToPath(new URL('../',import.meta.url)),require=createRequire(root+'web/package.json'),{build}=require('esbuild')
const output=process.env.PLUGIN_ARTIFACTS,pwPath=process.env.PLAYWRIGHT_MODULE
if(!output||!pwPath)throw Error('Set PLUGIN_ARTIFACTS and PLAYWRIGHT_MODULE')
await mkdir(output,{recursive:true})
const built=await build({stdin:{contents:`
import React from 'react';import{createRoot}from'react-dom/client';import{useViewPlugins}from'./view-plugins/Host';
const states=['working','waiting_you','milestone_complete','ready','holding','waiting_session','unknown','work_complete'];
const titles=['Build the village','Review the new artwork','Package ready for review','Prepare next task','Hold the design','Wait for integration','Unknown reading','Reported completion'];
const rows=states.map((workState,i)=>({key:'s'+i,title:titles[i],project:{key:'p'+i%3,label:['Design','Console','Research'][i%3]},workState,freshness:'fresh',...(i===0?{stateTime:{at:Date.now()-180000,basis:'turn'}}:{})}));
rows.push({key:'old',title:'<img src=x onerror=alert(1)> 星圖 Ω café',project:null,workState:'working',freshness:'stale'},{key:'unknown',title:'Freshness unknown',project:null,workState:'working',freshness:'unknown'});
let listeners=new Set();window.fixture={rows,connected:true,navigations:[],change(){for(const fn of listeners)fn()},hidden(value){Object.defineProperty(document,'visibilityState',{configurable:true,value:value?'hidden':'visible'});document.dispatchEvent(new Event('visibilitychange'))}};
function Host(){const api=useViewPlugins([{key:'synthetic-scenes',label:'Synthetic only',async read(){return{sessions:fixture.rows,connected:fixture.connected}},subscribe(fn){listeners.add(fn);return()=>listeners.delete(fn)},async prepareOpen(key){return()=>fixture.navigations.push(key)}}],document.documentElement.lang);return <><button id='manager' onClick={e=>api.openManager(e.currentTarget)}>Manager</button>{api.content}</>};createRoot(document.getElementById('root')).render(<Host/>);
`,resolveDir:root+'web/console/src',loader:'tsx'},bundle:true,write:false,outfile:'host.js',format:'esm',platform:'browser',jsx:'automatic',define:{'process.env.NODE_ENV':'"development"'}})
const files=new Map(built.outputFiles.map(f=>['/'+f.path.split('/').at(-1),f.text]))
const server=createServer((req,res)=>{const u=new URL(req.url,'http://local');const body=u.pathname==='/'?`<!doctype html><html data-theme="light" lang="${u.searchParams.get('lang')==='zh-Hant'?'zh-Hant':'en'}"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="stylesheet" href="/host.css"><div id="root"></div><script type="module" src="/host.js"></script></html>`:files.get(u.pathname);if(body===undefined){res.writeHead(404);res.end();return}res.setHeader('Content-Type',u.pathname.endsWith('.js')?'text/javascript':u.pathname.endsWith('.css')?'text/css':'text/html');res.end(body)})
await new Promise(r=>server.listen(0,'127.0.0.1',r));const origin='http://127.0.0.1:'+server.address().port,pw=await import(pathToFileURL(pwPath).href),reports=[]
const slugs=(process.env.SCENE_VIEWS||'codecraft,kaiju').split(','),engines=(process.env.SCENE_ENGINES||'chromium,webkit').split(','),widths=(process.env.SCENE_WIDTHS||'1440,390').split(',').map(Number)
try{for(const slug of slugs)for(const engine of engines)for(const width of widths){
 const lang=width===390?'zh-Hant':'en',zh=lang!=='en',browser=await pw[engine].launch({headless:true}),page=await browser.newPage({viewport:{width,height:1000},reducedMotion:'reduce'}),errors=[],requests=[]
 page.on('pageerror',e=>errors.push(e.message));page.on('request',r=>{if(!r.url().startsWith(origin)&&!r.url().startsWith('data:'))requests.push(r.url())})
 const prefix=slug+'-'+engine+'-'+width
 try{
  await page.goto(origin+'/?lang='+lang);await page.locator('#manager').click();await page.locator('input[type=file]:not([disabled])').waitFor()
  await page.locator('input[type=file]').setInputFiles({name:slug+'.zip',mimeType:'application/zip',buffer:await readFile(root+'web/console/public/view-plugins/session-'+slug+'.zip')})
  await page.getByRole('button',{name:zh?'安裝套件':'Install package',exact:true}).click()
  const name=slug==='codecraft'?(zh?'奇幻村莊':'World of Codecraft'):(zh?'怪獸城市':'Kaiju City')
  await page.locator('.vp-installed').filter({hasText:name}).getByRole('button',{name:zh?'開啟':'Open',exact:true}).click()
  const f=page.frameLocator('iframe'),body=f.locator('body');await f.locator('body[data-art-ready=true]').waitFor({timeout:30000});await f.locator('.row').nth(9).waitFor()
  assert.equal(await page.locator('iframe').getAttribute('sandbox'),'allow-scripts')
  assert.equal(await f.locator('.row').count(),10);assert.equal(await f.locator('#legend span').count(),8)
  assert.equal(await body.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1),true)
  assert.equal(await f.locator('.row').evaluateAll(es=>es.every(e=>e.getBoundingClientRect().height>=44)),true)
  const decoded=await body.evaluate(()=>[...document.images].filter(i=>!i.complete||i.naturalWidth===0).length);assert.equal(decoded,0)
  await f.locator('.row').filter({hasText:'Package ready for review'}).click()
  assert.match(await f.locator('#detail').innerText(),zh?/不代表已驗收/:/not acceptance/)
  assert.deepEqual(await page.evaluate(()=>fixture.navigations),[])
  await f.locator('.row').filter({hasText:'<img src=x'}).click();assert.equal(await f.locator('#open-session').isDisabled(),true)
  assert.equal(await f.locator('img[src=x]').count(),0)
  await f.locator('.row').filter({hasText:'Unknown reading'}).click();assert.equal(await f.locator('#open-session').isDisabled(),true)
  await f.locator('.row').filter({hasText:'Freshness unknown'}).click();assert.equal(await f.locator('#open-session').isDisabled(),true)
  await f.locator('.row').filter({hasText:'Build the village'}).click();assert.equal(await f.locator('#open-session').isDisabled(),false)
  await f.locator('#lighting').click();await page.waitForTimeout(150)
  const canvas=f.locator('#scene');assert.ok(Number(await canvas.getAttribute('data-characters'))===10)
  await page.screenshot({path:output+'/'+prefix+'-night.png',fullPage:true})
  await f.locator('#lighting').click();await page.waitForTimeout(150)
  await page.screenshot({path:output+'/'+prefix+'-day.png',fullPage:true})
  if(engine==='chromium'&&width===1440){
   await page.setViewportSize({width:1600,height:1000});const iframe=page.locator('iframe'),box=await iframe.boundingBox();await page.setViewportSize({width:1600,height:1000+1000-box.height});await page.waitForTimeout(100);await iframe.screenshot({path:output+'/'+slug+'-desktop.png'});await page.setViewportSize({width,height:1000})
  }
  await f.locator('#list').click();assert.equal(await f.locator('#scene').isVisible(),false);assert.equal(await f.locator('.row').count(),10);await f.locator('#list').click()
  await page.emulateMedia({reducedMotion:'no-preference'});await page.waitForTimeout(250);const phase=Number(await canvas.getAttribute('data-phase')),painting=await canvas.screenshot();await page.waitForTimeout(250);assert.ok(Number(await canvas.getAttribute('data-phase'))>phase,'Actual scene animation advances');assert.notDeepEqual(await canvas.screenshot(),painting,'Rendered pixels change while live')
  await f.locator('#still').click();await page.waitForTimeout(150);const still=await canvas.getAttribute('data-phase');await page.waitForTimeout(250);assert.equal(await canvas.getAttribute('data-phase'),still)
  await f.locator('#still').click();await page.emulateMedia({reducedMotion:'reduce'});await page.waitForTimeout(150);const reduced=await canvas.getAttribute('data-phase');await page.waitForTimeout(200);assert.equal(await canvas.getAttribute('data-phase'),reduced)
  await page.emulateMedia({reducedMotion:'no-preference'});await page.evaluate(()=>fixture.hidden(true));await page.waitForTimeout(150);const hidden=await canvas.getAttribute('data-phase');await page.waitForTimeout(200);assert.equal(await canvas.getAttribute('data-phase'),hidden)
  await page.evaluate(()=>fixture.hidden(false));await page.evaluate(()=>{fixture.connected=false;fixture.change()});await f.locator('#warning').filter({hasText:zh?'已斷線':'Disconnected'}).waitFor();assert.equal(await f.locator('#open-session').isDisabled(),true)
  await page.waitForTimeout(150);const offline=await canvas.getAttribute('data-phase');await page.waitForTimeout(200);assert.equal(await canvas.getAttribute('data-phase'),offline)
  await page.evaluate(()=>{fixture.connected=true;fixture.change()});await f.locator('#open-session:not([disabled])').waitFor()
  await page.emulateMedia({reducedMotion:'reduce'})
  await page.evaluate(()=>{fixture.saved=[...fixture.rows];fixture.rows=Array.from({length:256},(_,i)=>({key:'dense-'+i,title:'Complete Unicode Session '+i+' 星圖 Ω',project:{key:'project-'+i%12,label:'District '+i%12},workState:i%4?'working':'waiting_you',freshness:'fresh'}));fixture.change()})
  await f.locator('.row').nth(255).waitFor();assert.equal(await f.locator('.row').count(),256)
  if(slug==='kaiju'){assert.equal(await f.locator('#next').isDisabled(),false);await f.locator('#next').click();assert.match(await f.locator('#district-label').innerText(),/2 \/ 2/);assert.equal(await f.locator('#next').isDisabled(),true);await f.locator('.row').filter({hasText:'Complete Unicode Session 11 星圖 Ω'}).click();assert.match(await f.locator('#district-label').innerText(),/2 \/ 2/)}
  await page.screenshot({path:output+'/'+prefix+'-dense.png',fullPage:true})
  assert.equal(await body.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1),true)
  await page.evaluate(()=>{fixture.rows=[];fixture.change()});await f.locator('#empty').waitFor();assert.equal(await f.locator('.row').count(),0)
  await page.evaluate(()=>{fixture.rows=fixture.saved;fixture.change()});await f.locator('.row').nth(9).waitFor();await f.locator('.row').filter({hasText:'Build the village'}).click();await f.locator('#open-session').click()
  await page.locator('.vp-host').waitFor({state:'hidden'});await page.waitForFunction(()=>fixture.navigations.length===1);assert.deepEqual(await page.evaluate(()=>fixture.navigations),['s0'])
  assert.deepEqual(errors,[]);assert.deepEqual(requests,[])
  reports.push({slug,engine,width,lang,passed:true,cases:['production-import','original-art-loaded','all-eight-states','unicode-text-safety','stale-unknown-navigation-refusal','selection-before-navigation','day-night','list-parity','animated-frames','still','reduced','hidden','disconnect-reconnect','256-sessions','project-overflow','empty-replacement','explicit-navigation','no-network','no-page-overflow']});console.log(prefix+' passed')
 }catch(e){await page.screenshot({path:output+'/'+prefix+'-failure.png',fullPage:true});console.error(errors);throw e}finally{await browser.close()}
}}finally{await new Promise(r=>server.close(r));await writeFile(output+'/results.json',JSON.stringify(reports,null,2)+'\n')}
