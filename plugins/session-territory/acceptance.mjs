/** Candidate-only, synthetic acceptance through the unchanged production host. */
import assert from 'node:assert/strict';
import {createServer} from 'node:http';
import {readFile, writeFile, mkdir} from 'node:fs/promises';
import {createRequire} from 'node:module';
import {fileURLToPath, pathToFileURL} from 'node:url';
import {resolve} from 'node:path';
import {spawnSync} from 'node:child_process';
const root = fileURLToPath(new URL('../../', import.meta.url));
const require = createRequire(resolve(root,'web/package.json'));
const {build} = require('esbuild');
const artifacts = resolve(process.argv[2] || '');
if (!process.argv[2] || !process.env.PLAYWRIGHT_MODULE) throw Error('Pass an artifacts directory and set PLAYWRIGHT_MODULE');
await mkdir(artifacts,{recursive:true});
const began = Date.now();
const states = ['ready','working','holding','waiting_you','waiting_session','unknown','milestone_complete','work_complete'];
const hostile = '<img src=x onerror=alert(1)> & synthetic';
const host = await build({stdin:{contents:`
import React from 'react';import{createRoot}from'react-dom/client';
import{useViewPlugins}from'./view-plugins/Host';
import{setDockAvailable}from'./project-assistants/BotOrb';setDockAvailable(true);
const states=${JSON.stringify(states)};
const initial=states.map((workState,i)=>({key:'session-'+i,title:i===0?${JSON.stringify(hostile)}:['', 'Map the estuary','Review the bridge','Choose a direction','Wait for the survey','Unknown signal','Survey checkpoint','Map delivered'][i],project:i===7?null:{key:i<4?'north':'south',label:i<4?'North / 北岸':'<svg onload=alert(1)> South / 南岸'},workState,freshness:i===5?'unknown':'fresh'}));
let rows=structuredClone(initial),connected=true;const listeners=new Set(),navigations=[];
window.fixture={navigations,many(){rows=Array.from({length:120},(_,i)=>({...initial[i%8],key:'many-'+i,title:'Synthetic Session '+i}));for(const fn of listeners)fn()},update(){rows[0].title='Updated synthetic Session';for(const fn of listeners)fn()},empty(){rows=[];for(const fn of listeners)fn()},reset(){rows=structuredClone(initial);connected=true;for(const fn of listeners)fn()},stale(){rows=rows.map(r=>({...r,freshness:'stale'}));for(const fn of listeners)fn()},disconnect(){connected=false;for(const fn of listeners)fn()},visibility(value){Object.defineProperty(document,'visibilityState',{configurable:true,get:()=>value?'visible':'hidden'});document.dispatchEvent(new Event('visibilitychange'))}};
const source={key:'synthetic-authorized',label:'Synthetic authorized fixture',async read(){return{sessions:structuredClone(rows),connected}},subscribe(fn){listeners.add(fn);return()=>listeners.delete(fn)},async prepareOpen(key){return()=>navigations.push(key)}};
function Host(){const api=useViewPlugins([source],document.documentElement.lang);return <><p>SYNTHETIC HOST — no personal data</p><button id="manager" onClick={e=>api.openManager(e.currentTarget)}>Manage fixture</button>{api.dock}{api.entry}{api.content}</>};createRoot(document.getElementById('root')).render(<Host/>);
`,resolveDir:resolve(root,'web/console/src'),loader:'tsx'},bundle:true,write:false,outfile:'host.js',format:'esm',platform:'browser',jsx:'automatic',define:{'process.env.NODE_ENV':'"development"'}});
const activity = await build({stdin:{contents:`import{renderActivity}from'./activity.js';const snapshot={synthetic:true,freshness:'fresh',connection:'connected',files:[{ref:'f1',relativePath:'src/harbour.ts'},{ref:'f2',relativePath:'docs/<img src=x>.md'},{ref:'f3',relativePath:'src/not-observed.ts'}],events:[{fileRef:'f1',operation:'write',outcome:'succeeded',provenance:'native_success_receipt',observedAt:'2026-01-01T00:00:10Z'},{fileRef:'f2',operation:'read',outcome:'succeeded',provenance:'synthetic_read_receipt',observedAt:'2026-01-01T00:00:11Z'},{fileRef:'f3',operation:'write',outcome:'failed',provenance:'synthetic_failed_receipt',observedAt:'2026-01-01T00:00:12Z'}]};window.fileFixture={stale(){snapshot.freshness='stale';draw()},empty(){snapshot.files=[];draw()},unavailable(){renderActivity(document.querySelector('main'),null)}};function draw(){renderActivity(document.querySelector('main'),snapshot,{language:new URL(location.href).searchParams.get('lang')||'en'})}draw();`,resolveDir:fileURLToPath(new URL('.',import.meta.url)),loader:'js'},bundle:true,write:false,format:'esm',outfile:'activity.js'});
const files=new Map([...host.outputFiles,...activity.outputFiles].map(f=>['/'+f.path.split('/').at(-1),f.text]));
let unexpectedRequests=0;
const server=createServer((req,res)=>{const u=new URL(req.url,'http://synthetic');let body=files.get(u.pathname);if(u.pathname==='/')body=`<!doctype html><html lang="${u.searchParams.get('lang')==='zh-Hant'?'zh-Hant':'en'}"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="stylesheet" href="/host.css"><div id="root"></div><script type="module" src="/host.js"></script></html>`;if(u.pathname==='/activity')body='<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><style>body{background:#101c21;color:#e4eee6;font:15px/1.5 system-ui;margin:24px}h2{font:24px Georgia}h3{font-size:15px}</style><main></main><script type="module" src="/activity.js"></script>';if(body===undefined){unexpectedRequests++;res.writeHead(404);res.end();return}res.setHeader('Content-Type',u.pathname.endsWith('.js')?'text/javascript':u.pathname.endsWith('.css')?'text/css':'text/html');res.end(body)});
await new Promise(r=>server.listen(0,'127.0.0.1',r));
const origin='http://127.0.0.1:'+server.address().port;
const pw=await import(pathToFileURL(process.env.PLAYWRIGHT_MODULE).href);
const reports=[];
const archive=await readFile(resolve(root,'web/console/public/view-plugins/session-territory.zip'));
const packed=spawnSync('python3',['-c',`import io,sys,zipfile,json,hashlib
z=zipfile.ZipFile(io.BytesIO(sys.stdin.buffer.read()))
assert z.namelist()==['manifest.json','index.html']
assert all(f.compress_type==0 for f in z.infolist())
assert b'CLAWDLINE_SDK' not in z.read('index.html')
print(json.dumps({'entries':z.namelist(),'sha256':hashlib.sha256(open(sys.argv[1],'rb').read()).hexdigest()}))`,resolve(root,'web/console/public/view-plugins/session-territory.zip')],{input:archive});assert.equal(packed.status,0,packed.stderr.toString());
try {for(const engine of ['chromium','webkit']){
const browser=await pw[engine].launch({headless:true});
try{for(const width of [390,1440])for(const language of ['en','zh-Hant']){
console.log('case',engine,width,language);
const ctx=await browser.newContext({viewport:{width,height:1000},reducedMotion:'no-preference'}),page=await ctx.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));page.on('dialog',()=>{throw Error('Hostile label executed')});
try{
await page.goto(origin+'/?lang='+language);await page.locator('#manager').click();
await page.locator('input[type=file]').setInputFiles({name:'session-territory.zip',mimeType:'application/zip',buffer:archive});
await page.getByRole('button',{name:language==='en'?'Install package':'安裝套件',exact:true}).click();
const installed=page.locator('.vp-installed').filter({hasText:'com.clawdline.session-territory'});
await installed.getByRole('button',{name:new RegExp(language==='en'?'^Open':'^開啟')}).click();
const frame=page.frameLocator('iframe');await frame.locator('.cell').first().waitFor();
assert.equal(await frame.locator('.cell').count(),8);assert.equal(await frame.locator('.legend .state').count(),8);
assert.equal(await frame.locator('#fileBadge').textContent(),language==='en'?'UNAVAILABLE IN THIS VIEW':'此視圖尚無這項資料');
assert.ok((await frame.locator('.cell-title').allTextContents()).includes(hostile));assert.equal(await frame.locator('img').count(),0);assert.equal(await frame.locator('svg').count(),1);
assert.equal(await frame.locator('.project').count(),3);assert.equal(await frame.locator('.cell[data-freshness=unknown]').count(),1);
assert.ok(await frame.locator('.project-name').allTextContents().then(v=>v.includes(language==='en'?'No Project':'未屬於 Project')));
assert.equal(await frame.locator('body').evaluate(el=>el.scrollWidth<=innerWidth),true,'No frame horizontal overflow');
assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true,'No host horizontal overflow');
assert.equal(await frame.locator('button:visible').evaluateAll(buttons=>buttons.every(b=>b.getBoundingClientRect().width>=44&&b.getBoundingClientRect().height>=44)),true,'Visible controls meet touch size');
await frame.locator('.cell').filter({hasText:hostile}).click();assert.equal(await page.evaluate(()=>fixture.navigations.length),0);
const ref=await frame.locator('.cell.selected').getAttribute('data-ref');
await frame.locator('#listButton').click();assert.equal(await frame.locator('.list-row').count(),8);assert.equal(await frame.locator('.list-row.selected').getAttribute('data-ref'),ref);
await page.evaluate(()=>fixture.update());await frame.locator('#detailTitle').getByText('Updated synthetic Session',{exact:true}).waitFor();assert.equal(await frame.locator('.list-row.selected').getAttribute('data-ref'),ref);
await frame.locator('.list-row[data-freshness=unknown]').click();assert.equal(await frame.locator('#openButton').isDisabled(),true);
await frame.locator('#mapButton').click();
await frame.locator('body[data-motion=on]').waitFor();
const animationCount=()=>frame.locator('body').evaluate(el=>el.getAnimations({subtree:true}).filter(a=>a.playState==='running').length);
assert.ok(await animationCount()>0,'Fresh visible working marker animates');
await frame.locator('#stillButton').click();assert.equal(await animationCount(),0);await frame.locator('#stillButton').click();
await page.emulateMedia({reducedMotion:'reduce'});await frame.locator('body[data-motion=off]').waitFor();assert.equal(await animationCount(),0);
await page.emulateMedia({reducedMotion:'no-preference'});await frame.locator('body[data-motion=on]').waitFor();
// Synthetic visibility is injected at the host Document boundary; Frame.tsx sends the real context update.
await page.evaluate(()=>fixture.visibility(false));await frame.locator('body[data-motion=off]').waitFor();assert.equal(await animationCount(),0);
await page.evaluate(()=>fixture.visibility(true));await frame.locator('body[data-motion=on]').waitFor();
await page.evaluate(()=>fixture.stale());await frame.locator('.cell[data-freshness=stale]').first().waitFor();assert.equal(await animationCount(),0);assert.equal(await frame.locator('.cell[data-freshness=stale]').count(),8);
await page.evaluate(()=>fixture.disconnect());await frame.locator('#connection').getByText(language==='en'?'Disconnected · retained summaries are stale':'連線中斷 · 保留的摘要已視為過時',{exact:true}).waitFor();assert.equal(await frame.locator('#openButton').isDisabled(),true);
await page.screenshot({path:resolve(artifacts,`${engine}-${width}-${language}-stale.png`)});
await page.evaluate(()=>fixture.many());await frame.locator('.cell').nth(119).waitFor({state:'attached'});assert.equal(await frame.locator('.cell').count(),120);await frame.locator('#listButton').click();assert.equal(await frame.locator('.list-row').count(),120);await frame.locator('#mapButton').click();await page.evaluate(()=>fixture.empty());await frame.locator('#empty').waitFor();assert.equal(await frame.locator('.cell').count(),0);
await page.evaluate(()=>fixture.reset());await frame.locator('.cell').first().waitFor();
await frame.locator('.cell').filter({hasText:hostile}).click();await frame.locator('#stillButton').click();
await page.screenshot({path:resolve(artifacts,`${engine}-${width}-${language}-territory.png`)});
if(width===390){await frame.locator('#details').scrollIntoViewIfNeeded();await page.screenshot({path:resolve(artifacts,`${engine}-${width}-${language}-details.png`)})}
await frame.locator('#openButton').click();await page.waitForFunction(()=>fixture.navigations.length===1);assert.deepEqual(await page.evaluate(()=>fixture.navigations),['session-0']);await page.locator('iframe').waitFor({state:'detached'});
assert.deepEqual(errors,[]);
reports.push({engine,width,language,pass:true,cases:['production ZIP import','eight states','safe labels','null Project','update','selection/list parity','navigation','unknown/stale','empty','still/reduced/hidden/disconnected motion','layout/touch']});
}catch(error){await page.screenshot({path:resolve(artifacts,`${engine}-${width}-${language}-failure.png`)});await writeFile(resolve(artifacts,'failure.txt'),String(error)+'\n'+await page.locator('body').innerText());throw error}finally{await ctx.close()}
}
const page=await browser.newPage({viewport:{width:1440,height:900}});try{await page.goto(origin+'/activity');await page.locator('.file-cell').first().waitFor();assert.equal(await page.locator('.file-cell').count(),3);assert.equal(await page.locator('img').count(),0);assert.equal(await page.getByText(/synthetic_failed_receipt/).count(),0);await page.screenshot({path:resolve(artifacts,`${engine}-synthetic-files.png`)});await page.evaluate(()=>fileFixture.stale());assert.equal(await page.locator('.file-cell[data-freshness=stale]').count(),3);await page.evaluate(()=>fileFixture.empty());assert.equal(await page.locator('.file-cell').count(),0);await page.evaluate(()=>fileFixture.unavailable());await page.getByText('File activity unavailable.',{exact:true}).waitFor();reports.push({engine,fixture:'separate synthetic file renderer',pass:true})}finally{await page.close()}
}finally{await browser.close()}
}
assert.equal(unexpectedRequests,0);
await writeFile(resolve(artifacts,'acceptance.json'),JSON.stringify({synthetic:true,base:'a02ae3311fce57b4e7911051f110cf3ae594e766',package:JSON.parse(packed.stdout.toString()),seconds:(Date.now()-began)/1000,reports},null,2)+'\n');
console.log(JSON.stringify({pass:true,cases:reports.length,seconds:(Date.now()-began)/1000}));
}finally{await new Promise(r=>server.close(r))}
