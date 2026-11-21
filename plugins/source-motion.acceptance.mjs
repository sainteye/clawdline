/** Source-timing and actual DOM acceptance through the production package host. */
import assert from 'node:assert/strict'
import {createServer} from 'node:http'
import {readFile,mkdir,writeFile} from 'node:fs/promises'
import {createRequire} from 'node:module'
import {fileURLToPath,pathToFileURL} from 'node:url'
import {resolve} from 'node:path'
const root=fileURLToPath(new URL('../',import.meta.url)),require=createRequire(root+'web/package.json')
const output=process.env.PLUGIN_ARTIFACTS
if(!output||!process.env.PLAYWRIGHT_MODULE)throw Error('Set PLUGIN_ARTIFACTS and PLAYWRIGHT_MODULE to existing authorized locations')
await mkdir(output,{recursive:true})
const {build}=require('esbuild')
const bundle=await build({stdin:{contents:`
import React from 'react';import{createRoot}from'react-dom/client';import{useViewPlugins}from'./view-plugins/Host';

const states=['ready','working','holding','waiting_you','waiting_session','unknown','milestone_complete','work_complete'];
let listeners=new Set();window.fixture={rows:Array.from({length:Number(new URLSearchParams(location.search).get('count'))||8},(_,i)=>({key:'s'+i,title:i===1?'Working alpha':i===3?'中文甲':('Synthetic '+i),project:{key:i%2?'p1':'p0',label:i%2?'Orion':'Harbor'},workState:states[i%8],freshness:new URLSearchParams(location.search).get('freshness')||'fresh'})),connected:true,navigations:[],change(){for(const f of listeners)f()},state(key,value){this.rows.find(r=>r.key===key).workState=value;this.change()},title(key,value){this.rows.find(r=>r.key===key).title=value;this.change()},hidden(value){Object.defineProperty(document,'visibilityState',{configurable:true,value:value?'hidden':'visible'});document.dispatchEvent(new Event('visibilitychange'))}};
function Host(){const api=useViewPlugins([{key:'synthetic',label:'Synthetic source',async read(){return{sessions:fixture.rows,connected:fixture.connected}},subscribe(fn){listeners.add(fn);return()=>listeners.delete(fn)},async prepareOpen(key){return()=>fixture.navigations.push(key)}}],document.documentElement.lang);return <><p>LOCAL SYNTHETIC DATA</p><button id="manager" onClick={e=>api.openManager(e.currentTarget)}>Manager</button>{api.dock}{api.content}</>};createRoot(document.getElementById('root')).render(<Host/>);
`,resolveDir:root+'web/console/src',loader:'tsx'},bundle:true,write:false,outfile:'host.js',format:'esm',platform:'browser',jsx:'automatic',define:{'process.env.NODE_ENV':'"development"'}})
const files=new Map(bundle.outputFiles.map(f=>['/'+f.path.split('/').at(-1),f.text]))
const server=createServer((req,res)=>{const u=new URL(req.url,'http://fixture'),body=u.pathname==='/'?`<!doctype html><html lang="${u.searchParams.get('lang')||'en'}"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="stylesheet" href="/host.css"><div id="root"></div><script type="module" src="/host.js"></script>`:files.get(u.pathname);if(body===undefined){res.writeHead(404);res.end();return}res.setHeader('Content-Type',u.pathname.endsWith('.js')?'text/javascript':u.pathname.endsWith('.css')?'text/css':'text/html');res.end(body)})
await new Promise(r=>server.listen(0,'127.0.0.1',r))
const origin=`http://127.0.0.1:${server.address().port}`,pw=await import(pathToFileURL(process.env.PLAYWRIGHT_MODULE).href),results=[]
try{for(const engine of (process.env.MOTION_ENGINE?[process.env.MOTION_ENGINE]:['chromium','webkit']))for(const width of [390,1440])for(const slug of (process.env.MOTION_VIEW?[process.env.MOTION_VIEW]:['orrery','night-city','departures'])){
 const browser=await pw[engine].launch({headless:true}),page=await browser.newPage({viewport:{width,height:640},reducedMotion:'no-preference'}),errors=[]
 page.on('pageerror',e=>errors.push(e.message))
 try{
  if(slug==='departures')await page.clock.install({time:new Date('2026-01-01T12:34:57Z')})
  await page.goto(origin+'/?count='+(slug==='departures'?40:8)+'&freshness='+(process.env.MOTION_STALE_ENTRY?'stale':'fresh')+'&lang='+(width===390?'zh-Hant':'en'));await page.locator('#manager').click()
  await page.locator('input[type=file]:not([disabled])').waitFor();const archive=await readFile(resolve(process.env.SOURCE_MOTION_ARCHIVE_DIR||root+'web/console/public/view-plugins','session-'+slug+'.zip'))
  await page.locator('input[type=file]').setInputFiles({name:slug+'.zip',mimeType:'application/zip',buffer:archive})
  await page.locator('.vp-review').waitFor();await page.getByRole('button',{name:width===390?'安裝套件':'Install package',exact:true}).click()
  await page.locator('.vp-review[data-phase=installed]').getByRole('button',{name:width===390?'開啟':'Open',exact:true}).click()
  const f=page.frameLocator('iframe');await f.locator('body').waitFor();await f.locator('#still').waitFor()
  if(process.env.MOTION_STALE_ENTRY){await f.locator(slug==='orrery'?'.planet':slug==='night-city'?'.window':'.row[data-ref]').first().waitFor();assert.equal(await f.locator('body').evaluate(n=>n.getAnimations({subtree:true}).length),0,'Cached stale rows do not consume the entrance');await page.evaluate(()=>{fixture.rows.forEach(r=>r.freshness='fresh');fixture.change()})}
  if(slug==='orrery'){
   const target=f.locator('.planet[aria-label*="Working alpha"]');await target.waitFor()
   assert.ok(await target.evaluate(n=>n.getAnimations().some(a=>a.id==='entry'&&a.effect.getTiming().duration===900)),'Opening starts one non-looping planet entrance');
   const before=await target.evaluate(n=>({x:parseFloat(n.style.left),y:parseFloat(n.style.top)}))
   if(width===1440)assert.equal(await f.locator('#dial').evaluate(n=>n.clientHeight<=innerHeight+1),true,'The source dial fits the visible content height')
   await page.evaluate(()=>fixture.state('s1','waiting_you'))
   await target.locator('.blocked-halo').waitFor()
   const animation=await target.evaluate(n=>{const a=n.getAnimations().find(a=>a.effect.getTiming().duration===900);if(!a)return null;a.pause();a.currentTime=450;const b=n.getBoundingClientRect();return {duration:a.effect.getTiming().duration,left:getComputedStyle(n).left,top:getComputedStyle(n).top,target:{x:parseFloat(n.style.left),y:parseFloat(n.style.top)},box:{x:b.x,y:b.y}}})
   assert.ok(animation,'Actual state changes use the source 900ms glide');assert.equal(animation.duration,900)
   assert.ok(Math.abs(parseFloat(animation.left)-before.x)>0.1||Math.abs(parseFloat(animation.top)-before.y)>0.1)
   assert.ok(Math.abs(parseFloat(animation.left)-animation.target.x)>0.1||Math.abs(parseFloat(animation.top)-animation.target.y)>0.1,'The half-way frame has not jumped to its destination')
   await writeFile(`${output}/${engine}-${width}-${slug}-motion.json`,JSON.stringify(animation,null,2))
  }else if(slug==='night-city'){
   await f.locator('.window.working.fresh .glyph').first().waitFor()
   assert.ok(await f.locator('.window.working.fresh').first().evaluate(n=>n.getAnimations().some(a=>a.id==='entry'&&a.effect.getTiming().iterations===1)),'Opening lights the fresh windows once');
   const cursor=await f.locator('.window.working.fresh .glyph').first().evaluate(n=>{const a=n.getAnimations({subtree:true}).find(a=>a.effect.getTiming().duration===600);if(!a)return null;a.pause();a.currentTime=0;const first=getComputedStyle(n,'::after').content;a.currentTime=400;return [first,getComputedStyle(n,'::after').content]})
   assert.ok(cursor&&cursor[0].includes('_')&&cursor[1].includes('▏'),'The working window alternates actual cursor glyphs')
   await page.evaluate(()=>{for(const r of fixture.rows)if(r.project.key==='p1')r.workState='working';fixture.change()})
   await f.locator('.building[aria-label=Orion][data-blocked=false]').waitFor();await f.locator('.neon').evaluateAll(ns=>ns.forEach(n=>n.getAnimations().forEach(a=>a.finish())))
   await page.evaluate(()=>fixture.state('s1','waiting_you'))
   const neon=f.locator('.building[data-blocked=true] .neon').filter({hasText:/1$/})
   await neon.waitFor()
   const motion=await neon.evaluate(n=>{const a=n.getAnimations().find(a=>a.effect.getTiming().duration===300);if(!a)return null;a.pause();a.currentTime=150;return{duration:a.effect.getTiming().duration,height:n.getBoundingClientRect().height,target:parseFloat(n.style.height)}})
   assert.ok(motion,'The attention sign opens with the source height transition');assert.ok(motion.height>0&&motion.height<motion.target)
   await writeFile(`${output}/${engine}-${width}-${slug}-motion.json`,JSON.stringify({cursor,neon:motion},null,2))
  }else{
   await page.clock.pauseAt(new Date(await page.evaluate(()=>Date.now()+1000)));await page.clock.runFor(2500)
   assert.equal(await f.locator('.list-row').count(),40,'Every Session remains available independently of paging')
   await f.locator('#nextPage').waitFor({state:'visible'})
   const prior=await f.locator('#pageLabel').textContent();await f.locator('#nextPage').click();assert.notEqual(await f.locator('#pageLabel').textContent(),prior)
   await f.locator('#previousPage').click();await page.clock.runFor(2500)
   // The waiting row is pinned on pages that have room, as in upstream pagesOf.
   assert.ok(await f.locator('.row.waiting_you').filter({hasText:'中文甲'}).count())
   await page.evaluate(()=>fixture.title('s3','中文乙'))
   let split=false
   for(let i=0;i<30&&!split;i++){await page.clock.runFor(50);split=await f.locator('.row').filter({hasText:'中文乙'}).evaluate(n=>[...n.querySelectorAll('.cell')].some(c=>c.querySelector('.half.top')?.textContent==='乙'&&c.querySelector('.half.bottom')?.textContent==='甲'))}
   assert.equal(split,true,'A changed Chinese grapheme uses the real old/new half-flap faces')
   await page.clock.runFor(2500);const untilMinute=await f.locator('body').evaluate(()=>60000-Date.now()%60000);await page.clock.runFor(Math.max(0,untilMinute-50));let clockSplit=false
   for(let i=0;i<90&&!clockSplit;i++){await page.clock.runFor(50);clockSplit=await f.locator('#clock .half').count()>0}
   assert.equal(clockSplit,true,'The local clock uses actual half-flaps on minute changes')
   await page.clock.runFor(2500);const p=await f.locator('#pageLabel').textContent();await page.clock.runFor(8000);assert.notEqual(await f.locator('#pageLabel').textContent(),p,'The source eight-second page turn continues without a hover')
   await f.locator('#sound').waitFor();assert.equal(await f.locator('#sound').getAttribute('aria-pressed'),'false');await f.locator('#sound').click();let played=false;for(let i=0;i<60&&!played;i++){await page.waitForTimeout(50);played=await f.locator('#clack').evaluate(n=>n.played.length>0)}assert.ok(played,'Explicit sound activation plays the embedded original media: '+JSON.stringify(await f.locator('#clack').evaluate(n=>({paused:n.paused,ready:n.readyState,error:n.error?.code,stopped:document.body.classList.contains('still')}))));await f.locator('#sound').click()
   await writeFile(`${output}/${engine}-${width}-${slug}-motion.json`,JSON.stringify({unicodeHalf:true,clockHalf:true,pageTurns:true,sessionCount:40},null,2))
  }
  await page.screenshot({path:`${output}/${engine}-${width}-${slug}-moving.png`})
  await f.locator('#still').click()
  assert.equal(await f.locator('body').evaluate(n=>n.getAnimations({subtree:true}).length),0,'Still mode cancels actual animations')
  if(slug==='departures')assert.equal(await f.locator('.half').count(),0)
  await f.locator('#still').click();await page.emulateMedia({reducedMotion:'reduce'});await f.locator(slug==='orrery'?'body.no-motion':slug==='night-city'?'body:not(.moving)':'body.still').waitFor()
  assert.equal(await f.locator('body').evaluate(n=>n.getAnimations({subtree:true}).length),0,'Reduced motion cancels actual animations')
  assert.equal(await f.locator('body').evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true)
  assert.deepEqual(errors,[])
  results.push({engine,width,slug,passed:true})
 }catch(error){await page.screenshot({path:`${output}/${engine}-${width}-${slug}-failure.png`});throw error}finally{await browser.close()}
}}finally{await new Promise(r=>server.close(r));await writeFile(`${output}/results.json`,JSON.stringify(results,null,2))}
console.log(JSON.stringify(results,null,2))
