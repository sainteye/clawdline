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
 const lang=width===390?'zh-Hant':'en',zh=lang!=='en',browser=await pw[engine].launch({headless:true}),page=await browser.newPage({viewport:{width,height:width===390?720:1000},reducedMotion:'reduce'}),errors=[],requests=[]
 page.on('pageerror',e=>errors.push(e.message));page.on('request',r=>{if(!r.url().startsWith(origin)&&!r.url().startsWith('data:'))requests.push(r.url())})
 const prefix=slug+'-'+engine+'-'+width
 try{
  await page.goto(origin+'/?lang='+lang);await page.locator('#manager').click();await page.locator('input[type=file]:not([disabled])').waitFor()
  await page.locator('input[type=file]').setInputFiles({name:slug+'.zip',mimeType:'application/zip',buffer:await readFile((process.env.SCENE_PACKAGE_ROOT||root+'web/console/public/view-plugins/')+'session-'+slug+'.zip')})
  await page.getByRole('button',{name:zh?'安裝套件':'Install package',exact:true}).click()
  const name=slug==='codecraft'?(zh?'奇幻村莊':'World of Codecraft'):(zh?'怪獸城市':'Kaiju City')
  await page.locator('.vp-installed').filter({hasText:name}).getByRole('button',{name:zh?'開啟':'Open',exact:true}).click()
  const f=page.frameLocator('iframe'),body=f.locator('body');await f.locator('body[data-art-ready=true]').waitFor({timeout:30000});await f.locator('.row').nth(9).waitFor()
  const dismiss=async()=>{if(await f.locator('#selection[open]').count())await f.locator('#dismiss-selection').click()}
  const selectRow=async title=>{await dismiss();await f.locator('.row').filter({hasText:title}).click()}
  if(process.env.SCENE_FIT_ONLY){
   await page.evaluate(slug=>{fixture.rows=Array.from({length:11},(_,i)=>({key:'crowd-'+i,title:'Crowded Session '+i+' 星圖 Ω — a complete descriptive title',project:{key:'one',label:'One Project'},workState:i<9?(slug==='kaiju'?'working':'waiting_you'):i===9?'ready':'unknown',freshness:'fresh'}));fixture.change()},slug)
   await f.locator('.row').nth(10).waitFor()
   if(slug==='kaiju'){
    await page.waitForTimeout(100)
    assert.equal(Number(await f.locator('#scene').getAttribute('data-characters')),2,'Only the original two standing points appear for this Project; overflow stays reachable')
    await f.locator('.project-marker').getByText('One Project',{exact:false}).click()
    assert.equal(await f.locator('.project-member').count(),11)
    await f.getByRole('button',{name:zh?'下一批怪獸':'Next monsters',exact:true}).click()
    assert.match(await f.locator('.cast-controls p').innerText(),/3–4 \/ 9/)
    await f.locator('.project-member').filter({hasText:'Crowded Session 8'}).click()
    assert.match(await f.locator('.cast-controls p').innerText(),/9–9 \/ 9/)
    await page.waitForTimeout(100);assert.equal(Number(await f.locator('#scene').getAttribute('data-characters')),1)

   }else{
    await selectRow('Crowded Session 8')
   }
   if(slug==='codecraft')assert.equal(await f.locator('#scene-label').isVisible(),true,'The selected full name has a readable foreground label')
   if(width===390){
    const rect=await f.locator('#open-session').evaluate(e=>{const b=e.getBoundingClientRect();return{top:b.top,bottom:b.bottom,height:innerHeight}})
    assert.ok(rect.top>=0&&rect.bottom<=rect.height,'A phone selection exposes its Open Session action inside the visible frame')
    await page.screenshot({path:output+'/'+prefix+'-selection.png',fullPage:true})
    await f.locator('#dismiss-selection').click()
   }
   if(slug==='codecraft'){
    await f.locator('#scene-label').waitFor({state:'visible'})
    const label=await f.locator('#scene-label').evaluate(e=>{const b=e.getBoundingClientRect(),v=document.querySelector('.viewport').getBoundingClientRect();return{text:e.textContent,left:b.left,right:b.right,top:b.top,bottom:b.bottom,v:{left:v.left,right:v.right,top:v.top,bottom:v.bottom}}})
    assert.match(label.text,/Crowded Session 8.*星圖 Ω/)
    assert.ok(label.left>=label.v.left&&label.right<=label.v.right&&label.top>=label.v.top&&label.bottom<=label.v.bottom,'Selected name stays inside the village viewport')
    assert.ok(await f.locator('.status-marker').count()>0,'Attention markers remain visible')
    const boxes=await f.locator('.status-marker').evaluateAll(es=>es.map(e=>{const r=e.getBoundingClientRect();return{x:r.x,y:r.y,w:r.width,h:r.height}}))
    for(let i=0;i<boxes.length;i++)for(let j=0;j<i;j++){const a=boxes[i],b=boxes[j];assert.ok(a.x+a.w<=b.x||b.x+b.w<=a.x||a.y+a.h<=b.y||b.y+b.h<=a.y,'Status labels do not cover each other')}
   }
   if(slug==='codecraft'){
    await page.evaluate(()=>{fixture.rows=Array.from({length:256},(_,i)=>({key:'notice-'+i,title:'Attention Session '+i+' 完整名稱',project:{key:'dense',label:'Dense Project'},workState:'waiting_you',freshness:'fresh'}));fixture.change()})
    await f.locator('.row').nth(255).waitFor();await selectRow('Attention Session 128 完整名稱');await dismiss();await f.locator('#scene').scrollIntoViewIfNeeded();await page.waitForTimeout(100)
    const geometry=await f.locator('.status-marker').evaluateAll(es=>es.map(e=>{const r=e.getBoundingClientRect();return{x:r.x,y:r.y,w:r.width,h:r.height}}))
    assert.ok(geometry.length>0)
    for(let i=0;i<geometry.length;i++)for(let j=0;j<i;j++){const a=geometry[i],b=geometry[j];assert.ok(a.x+a.w<=b.x||b.x+b.w<=a.x||a.y+a.h<=b.y||b.y+b.h<=a.y,'Dense attention markers remain disjoint: '+JSON.stringify({a,b}))}
    await f.locator('.status-marker').first().click();assert.ok(await f.locator('#selection[open]').count()||await f.locator('#open-session').count());await dismiss()
   }
   await page.screenshot({path:output+'/'+prefix+'-fit.png',fullPage:true})
   reports.push({slug,engine,width,passed:true,cases:['single-project-density','visible-phone-selection','world-name-framing','nonoverlapping-status-markers']});console.log(prefix+' fit passed');continue
  }
  assert.equal(await page.locator('iframe').getAttribute('sandbox'),'allow-scripts')
  assert.equal(await f.locator('.row').count(),10);assert.equal(await f.locator('#legend span').count(),8)
  assert.equal(await body.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1),true)
  assert.equal(await f.locator('.row').evaluateAll(es=>es.every(e=>e.getBoundingClientRect().height>=44)),true)
  const decoded=await body.evaluate(()=>[...document.images].filter(i=>!i.complete||i.naturalWidth===0).length);assert.equal(decoded,0)
  await selectRow('Package ready for review')
  assert.match(await f.locator('#detail').innerText(),zh?/不代表已驗收/:/not acceptance/)
  assert.deepEqual(await page.evaluate(()=>fixture.navigations),[])
  await selectRow('<img src=x');assert.equal(await f.locator('#open-session').isDisabled(),true)
  assert.equal(await f.locator('img[src=x]').count(),0)
  await selectRow('Unknown reading');assert.equal(await f.locator('#open-session').isDisabled(),true)
  await selectRow('Freshness unknown');assert.equal(await f.locator('#open-session').isDisabled(),true)
  await selectRow('Build the village');assert.equal(await f.locator('#open-session').isDisabled(),false)
  await dismiss();await f.locator('#lighting').click();await page.waitForTimeout(150)
  const canvas=f.locator('#scene');assert.ok(Number(await canvas.getAttribute('data-characters'))===(slug==='kaiju'?6:10))
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
  if(slug==='kaiju'){assert.equal(await f.locator('#next').isDisabled(),false);await f.locator('#next').click();assert.match(await f.locator('#district-label').innerText(),/2 \/ 2/);assert.equal(await f.locator('#next').isDisabled(),true);await selectRow('Complete Unicode Session 11 星圖 Ω');assert.match(await f.locator('#district-label').innerText(),/2 \/ 2/)}
  await dismiss();await page.screenshot({path:output+'/'+prefix+'-dense.png',fullPage:true})
  assert.equal(await body.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1),true)
  await page.evaluate(()=>{fixture.rows=[];fixture.change()});await f.locator('#empty').waitFor();assert.equal(await f.locator('.row').count(),0)
  await page.evaluate(()=>{fixture.rows=fixture.saved;fixture.change()});await f.locator('.row').nth(9).waitFor();await selectRow('Build the village');await f.locator('#open-session').click()
  await page.locator('.vp-host').waitFor({state:'hidden'});await page.waitForFunction(()=>fixture.navigations.length===1);assert.deepEqual(await page.evaluate(()=>fixture.navigations),['s0'])
  assert.deepEqual(errors,[]);assert.deepEqual(requests,[])
  reports.push({slug,engine,width,lang,passed:true,cases:['production-import','original-art-loaded','all-eight-states','unicode-text-safety','stale-unknown-navigation-refusal','selection-before-navigation','day-night','list-parity','animated-frames','still','reduced','hidden','disconnect-reconnect','256-sessions','project-overflow','empty-replacement','explicit-navigation','no-network','no-page-overflow']});console.log(prefix+' passed')
 }catch(e){await page.screenshot({path:output+'/'+prefix+'-failure.png',fullPage:true});console.error(errors);throw e}finally{await browser.close()}
}}finally{await new Promise(r=>server.close(r));await writeFile(output+'/results.json',JSON.stringify(reports,null,2)+'\n')}
