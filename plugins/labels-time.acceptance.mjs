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
let listeners=new Set(),rows=[];const slug=new URLSearchParams(location.search).get('view');
const make=(n)=>Array.from({length:n},(_,i)=>({key:'row-'+i,title:'工作 '+i+' · A long readable Session title',project:{key:i===n-1?'other':'main',label:i===n-1?'Haven':'Clawdline'},workState:i===0?'waiting_you':i===n-1?'work_complete':'working',freshness:'fresh'}));rows=make(9);
if(slug==='departures')rows=make(9).map((r,i)=>({...r,workState:'working',...(i===0?{stateTime:{at:1700000000000,basis:'declaration'}}:i===1?{stateTime:{at:1700000100000,basis:'turn'}}:i===3?{freshness:'stale'}:i===4?{workState:'unknown'}:{})}));
window.fixture={get rows(){return rows},change(){for(const f of listeners)f()},resize(n){rows=make(n);this.change()},set(i,patch){rows[i]={...rows[i],...patch};this.change()},navigations:[]};
const source={key:'synthetic',label:'Synthetic fixture',async read(){return{sessions:[...rows],connected:true}},subscribe(fn){listeners.add(fn);return()=>listeners.delete(fn)},async prepareOpen(key){return()=>fixture.navigations.push(key)}};
function Host(){const api=useViewPlugins([source],document.documentElement.lang);return <><button id="manager" onClick={e=>api.openManager(e.currentTarget)}>Manager</button>{api.content}</>};createRoot(document.getElementById('root')).render(<Host/>);
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
const archives = new Map(await Promise.all(['orrery','departures'].map(async slug => [slug, await readFile(process.env.BEFORE_ARCHIVES ? process.env.BEFORE_ARCHIVES+'/'+slug+'-before.zip' : root + 'web/console/public/view-plugins/session-' + slug + '.zip')])))
const server=createServer((req,res)=>{const u=new URL(req.url,'http://fixture'),match=/^\/view-plugins\/session-(orrery|departures)\.zip$/.exec(u.pathname);const item=match?{type:'application/zip',bytes:archives.get(match[1])}:u.pathname==='/'?{type:'text/html',bytes:`<!doctype html><html lang="${u.searchParams.get('lang')||'en'}"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="stylesheet" href="/host.css"><div id="root"></div><script type="module" src="/host.js"></script>`}:files.get(u.pathname);if(!item){res.writeHead(404);res.end();return}res.setHeader('Content-Type',item.type);res.end(item.bytes)});
await new Promise(r=>server.listen(0,'127.0.0.1',r));const origin=`http://127.0.0.1:${server.address().port}`;
const pw=await import(pathToFileURL(process.env.PLAYWRIGHT_MODULE).href),results=[];
const overlap=(a,b)=>a.x<b.x+b.width-1&&a.x+a.width-1>b.x&&a.y<b.y+b.height-1&&a.y+a.height-1>b.y;
try{for(const engine of process.env.FEATURE_ENGINE?[process.env.FEATURE_ENGINE]:['chromium','webkit']){
 const browser=await pw[engine].launch({headless:true});try{for(const slug of process.env.FEATURE_VIEW?[process.env.FEATURE_VIEW]:['orrery','departures'])for(const language of ['en','zh-Hant']){
  const page=await browser.newPage({viewport:{width:1600,height:940},reducedMotion:'reduce',timezoneId:'UTC'}),errors=[];page.on('pageerror',e=>errors.push(e.message));
  try{
   await page.clock.install({time:new Date('2026-01-01T12:30:00Z')});await page.goto(origin+'/?view='+slug+'&lang='+language);await page.locator('#manager').click();
   await page.getByRole('button',{name:language==='en'?(slug==='orrery'?'Install Orrery':'Install Departures'):(slug==='orrery'?'安裝星象儀':'安裝航班板'),exact:true}).click();await page.locator('.vp-review').waitFor();
   await page.getByRole('button',{name:language==='en'?'Install package':'安裝套件',exact:true}).click();await page.locator('.vp-installed .vp-primary').click();const f=page.frameLocator('iframe');
   if(slug==='orrery'){
    await f.locator('.planet').nth(8).waitFor();await f.locator('body').evaluate(()=>document.fonts.ready);
    for(const count of [9,40,256]){
     if(count!==9){await page.evaluate(n=>fixture.resize(n),count);await page.clock.runFor(50);await f.locator('.planet').nth(count-1).waitFor()}
     const rects=await f.locator('.planet-label').evaluateAll(es=>es.filter(e=>e.getClientRects().length).map(e=>{const r=e.getBoundingClientRect();return{x:r.x,y:r.y,width:r.width,height:r.height}}));
     const bounds=await f.locator('#dial').boundingBox();
     for(let i=0;i<rects.length;i++){const r=rects[i];assert.ok(r.x>=-1&&r.y>=-1&&r.x+r.width<=bounds.width+1&&r.y+r.height<=bounds.height+1,'Visible labels stay inside the dial');for(let j=0;j<i;j++)assert.ok(!overlap(r,rects[j]),'Visible labels never overlap: '+JSON.stringify([r,rects[j]]))}
     if(count===9)assert.equal(rects.length,9,'Nine Sessions retain readable labels on desktop');
     const hidden=await f.locator('.planet-label[hidden]').count();assert.equal(rects.length+hidden,count);
     if(hidden){const ref=await f.locator('.planet').filter({has:f.locator('.planet-label[hidden]')}).first().getAttribute('data-ref');await f.locator('.row[data-ref="'+ref+'"]').click();assert.equal(await f.locator('.planet[data-ref="'+ref+'"] .planet-label').isVisible(),true,'Selecting a tucked-away label makes it visible')}
     if(count===9)await page.screenshot({path:`${output}/${engine}-${language}-orrery-nine.png`});
    }
    await page.setViewportSize({width:390,height:900});assert.equal(await f.locator('.planet-label').evaluateAll(es=>es.filter(e=>e.getClientRects().length).length),0,'Phone keeps the complete register instead of diagram labels');
   }else{
    await f.locator('.row[data-ref]').first().waitFor();
    const read=()=>f.locator('.row[data-ref]').evaluateAll(es=>es.map(e=>({name:e.getAttribute('aria-label').split(' · ')[0],ref:e.dataset.ref,code:e.dataset.flight,basis:e.dataset.timeBasis,time:[...e.querySelectorAll('.cell')].slice(0,5).map(c=>c.textContent).join('')})));
    const readAll=async count=>{const all=new Map();for(let n=0;n<count;n++){for(const r of await read())all.set(r.ref,r);if(all.size===count)return [...all.values()];await f.locator('#nextPage').click()}throw Error('Missing rows across pages')};
    let rows=await readAll(9);const first=rows.find(r=>r.name==='工作 0'),second=rows.find(r=>r.name==='工作 1'),observed=rows.find(r=>r.name==='工作 2');
    assert.equal(first.time,'22:13');assert.equal(first.basis,'declaration');assert.equal(second.time,'22:15');assert.equal(second.basis,'turn');assert.equal(observed.time,'12:30');assert.equal(observed.basis,'observed');
    assert.equal(rows.find(r=>r.name==='工作 3').time.trim(),'—');assert.equal(rows.find(r=>r.name==='工作 4').time.trim(),'—');
    const codes=new Map(rows.map(r=>[r.name,r.code]));assert.ok(rows.every(r=>/^[A-Z0-9]{2} \d{3}$/.test(r.code)));assert.equal(new Set(rows.map(r=>r.code)).size,rows.length);
    await page.clock.fastForward(120000);await page.evaluate(()=>fixture.change());await page.clock.runFor(50);rows=await readAll(9);assert.equal(rows.find(r=>r.name==='工作 2').time,'12:30','Repeated snapshots preserve the observation time');
    await page.evaluate(()=>fixture.set(2,{workState:'holding',project:{key:'main',label:'Renamed Project'}}));await page.clock.runFor(50);rows=await readAll(9);assert.equal(rows.find(r=>r.name==='工作 2').time,'12:32');assert.equal(rows.find(r=>r.name==='工作 2').code,codes.get('工作 2'),'Identity survives state and Project label changes');
    await page.screenshot({path:`${output}/${engine}-${language}-departures.png`});
    await page.evaluate(()=>fixture.resize(256));await page.clock.runFor(50);const all=new Map();
    for(let n=0;n<256;n++){for(const r of await read())all.set(r.name,r.code);if(all.size===256)break;await f.locator('#nextPage').click()}
    assert.equal(all.size,256);assert.equal(new Set(all.values()).size,256,'All supported Sessions get distinct identifiers');
    await page.evaluate(()=>fixture.set(0,{key:'new-cjk',project:{key:'cjk',label:'繁體中文'}}));await page.clock.runFor(50);let cjk;for(let n=0;n<256;n++){cjk=(await read()).find(r=>r.name==='工作 0');if(cjk)break;await f.locator('#nextPage').click()}assert.match(cjk.code,/^繁體 \d{3}$/,'Chinese Project abbreviations remain meaningful');
   }
   assert.deepEqual(errors,[]);assert.equal(await page.evaluate(()=>fixture.navigations.length),0);results.push({engine,slug,language,passed:true});
  }catch(e){await page.screenshot({path:`${output}/${engine}-${slug}-${language}-failure.png`});throw e}finally{await page.close()}
 }}finally{await browser.close()}
}}finally{await new Promise(r=>server.close(r));await writeFile(output+'/results.json',JSON.stringify(results,null,2))}
console.log(JSON.stringify(results,null,2));
