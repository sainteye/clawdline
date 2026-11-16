/** Real IndexedDB replacement, official catalog reads and running-view leases. Synthetic data only. */
import assert from 'node:assert/strict'
import { createServer } from 'node:http'
import { createHash } from 'node:crypto'
import { readFile, mkdir, writeFile } from 'node:fs/promises'
import { createRequire } from 'node:module'
import { fileURLToPath, pathToFileURL } from 'node:url'
import { spawnSync } from 'node:child_process'
const root=fileURLToPath(new URL('../',import.meta.url)),source=process.env.UPDATE_HOST_ROOT||root,output=process.env.PLUGIN_ARTIFACTS
if(!output||!process.env.PLAYWRIGHT_MODULE)throw Error('Set PLUGIN_ARTIFACTS and PLAYWRIGHT_MODULE')
await mkdir(output,{recursive:true})
const require=createRequire(root+'web/package.json'),{build}=require('esbuild')
const archive=await readFile(root+'web/console/public/view-plugins/session-galaxy.zip'),catalog=JSON.parse(await readFile(root+'web/console/public/view-plugins/catalog.json','utf8'))
const release=catalog.packages.find(row=>row.id==='com.clawdline.session-galaxy')
const fixture=spawnSync('python3',['-c',`import subprocess,zipfile,io,json,sys
path='web/console/public/view-plugins/session-galaxy.zip'
for rev in subprocess.check_output(['git','log','ef40d5aa515c43669124f4fcf7d08a58e75d0401','--format=%H','--',path]).decode().splitlines():
 raw=subprocess.check_output(['git','show',rev+':'+path])
 with zipfile.ZipFile(io.BytesIO(raw)) as z: m=json.loads(z.read('manifest.json'))
 if m['version']!='${release.version}':open(sys.argv[1]+'/old.zip','wb').write(raw);break
with zipfile.ZipFile(path) as z: files={n:z.read(n) for n in z.namelist()}
m=json.loads(files['manifest.json']);m['version']='0.0.1';m['capabilities']=['sessions.read'];files['manifest.json']=json.dumps(m).encode()
with zipfile.ZipFile(sys.argv[1]+'/read-only.zip','w',compression=zipfile.ZIP_STORED) as z:
 for n,b in files.items():z.writestr(n,b)
`,output],{cwd:root,encoding:'utf8'});assert.equal(fixture.status,0,fixture.stderr)
const old=await readFile(output+'/old.zip'),readOnly=await readFile(output+'/read-only.zip'),digest=b=>createHash('sha256').update(b).digest('hex')
const futureRun=spawnSync('python3',['-c',`import zipfile,json,sys
with zipfile.ZipFile(sys.argv[1]) as z: files={n:z.read(n) for n in z.namelist()}
m=json.loads(files['manifest.json']);v=m['version'].split('.');v[-1]=str(int(v[-1])+1);m['version']='.'.join(v);files['manifest.json']=json.dumps(m).encode()
with zipfile.ZipFile(sys.argv[2],'w',compression=zipfile.ZIP_STORED) as z:
 for n,b in files.items():z.writestr(n,b)`,root+'web/console/public/view-plugins/session-galaxy.zip',output+'/future.zip'],{encoding:'utf8'});assert.equal(futureRun.status,0,futureRun.stderr);const future=await readFile(output+'/future.zip')
const oldVersion='0.1.0'
const futureVersion=release.version.split('.').map((part,i)=>i===2?String(BigInt(part)+1n):part).join('.')
const built=await build({stdin:{contents:`
import React from'react';import{createRoot}from'react-dom/client';import{useViewPlugins}from'./view-plugins/Host';import{installedPackages}from'./view-plugins/store';
window.fixture={list:installedPackages,async seed(path,policy){const archive=await(await fetch(path)).arrayBuffer(),digest=[...new Uint8Array(await crypto.subtle.digest('SHA-256',archive))].map(b=>b.toString(16).padStart(2,'0')).join('');await new Promise((resolve,reject)=>{const open=indexedDB.open('clawdline-view-plugins',1);open.onupgradeneeded=()=>open.result.createObjectStore('packages',{keyPath:'id'});open.onerror=()=>reject(open.error);open.onsuccess=()=>{const db=open.result,tx=db.transaction('packages','readwrite');tx.objectStore('packages').put({id:'com.clawdline.session-galaxy',enabled:true,active:{archive,digest},...policy});tx.oncomplete=()=>{db.close();resolve()};tx.onabort=()=>reject(tx.error)}})}};
function Host(){const api=useViewPlugins([{key:'synthetic',label:'Synthetic only',async read(){return{sessions:[{key:'example',title:'Synthetic Session',project:null,workState:'ready',freshness:'fresh'}],connected:true}},subscribe(){return()=>{}},async prepareOpen(){return()=>{}}}],document.documentElement.lang);return <><button id='manager' onClick={e=>api.openManager(e.currentTarget)}>Manager</button>{api.content}</>};createRoot(document.getElementById('root')).render(<Host/>);
`,resolveDir:source+'web/console/src',loader:'tsx'},bundle:true,write:false,outfile:'host.js',format:'esm',platform:'browser',jsx:'automatic',define:{'process.env.NODE_ENV':'"development"'}})
const files=new Map(built.outputFiles.map(f=>['/'+f.path.split('/').at(-1),{bytes:f.contents,type:f.path.endsWith('.css')?'text/css':'text/javascript'}]))
for(const row of JSON.parse(await readFile(root+'web/console/src/view-plugins/previews.json','utf8')))files.set(row.src,{bytes:await readFile(root+'web/console/public'+row.src),type:'image/webp'})
let mode='normal',requests=[]
const currentCatalog=()=>{const next=structuredClone(catalog),item=next.packages.find(row=>row.id===release.id)
 if(mode==='old'){item.version=oldVersion;item.bytes=old.length;item.sha256=digest(old);item.knownDigests.push(item.sha256)}
 if(mode==='corrupt'){item.sha256='0'.repeat(64);item.knownDigests.push(item.sha256)}
 if(mode==='permissions')item.knownDigests.push(digest(readOnly))
 if(mode==='future'){item.version=futureVersion;item.bytes=future.length;item.sha256=digest(future);item.knownDigests.push(item.sha256)}
 return next}
const server=createServer((req,res)=>{const u=new URL(req.url,'http://fixture');requests.push(u.pathname)
 let item=files.get(u.pathname)
 if(u.pathname==='/')item={type:'text/html',bytes:`<!doctype html><html lang="${u.searchParams.get('lang')||'en'}"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="stylesheet" href="/host.css"><div id="root"></div><script type="module" src="/host.js"></script>`}
 if(u.pathname==='/view-plugins/catalog.json'){if(mode==='offline'){res.writeHead(503);res.end();return}item={type:'application/json',bytes:JSON.stringify(currentCatalog())}}
 if(u.pathname===release.url)item={type:'application/zip',bytes:mode==='future'?future:archive}
 if(u.pathname==='/old.zip')item={type:'application/zip',bytes:old}
 if(u.pathname==='/read-only.zip')item={type:'application/zip',bytes:readOnly}
 if(!item){res.writeHead(404);res.end();return}res.setHeader('Content-Type',item.type);res.setHeader('Cache-Control','no-store');res.end(item.bytes)})
await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));const origin=`http://127.0.0.1:${server.address().port}`,pw=await import(pathToFileURL(process.env.PLAYWRIGHT_MODULE).href),reports=[]
try{for(const engine of (process.env.UPDATE_ENGINES||'chromium,webkit').split(','))for(const width of (process.env.UPDATE_WIDTHS||'390,1440').split(',').map(Number)){
 const browser=await pw[engine].launch({headless:true}),context=await browser.newContext({viewport:{width,height:900},reducedMotion:'reduce'}),page=await context.newPage(),zh=width===390,language=zh?'zh-Hant':'en',errors=[]
 page.setDefaultTimeout(10000);page.on('pageerror',error=>errors.push({message:error.message,stack:error.stack,mode}))
 const card=page.locator('.vp-installed'),versions=()=>page.evaluate(async()=>{const r=(await fixture.list())[0];return r?{active:r.active.manifest.version,previous:r.previous?.manifest.version,enabled:r.enabled,official:r.official,autoUpdate:r.autoUpdate}:null})
 const waitVersion=async(target,version)=>{const deadline=Date.now()+10000;let active;do{active=await target.evaluate(async()=>(await fixture.list())[0]?.active.manifest.version);if(active===version)return;await new Promise(resolve=>setTimeout(resolve,50))}while(Date.now()<deadline);assert.equal(active,version)}
 const ready=async(path='/old.zip',policy={official:true,autoUpdate:false})=>{await page.goto(origin+'/?lang='+language);await page.evaluate(({path,policy})=>fixture.seed(path,policy),{path,policy});await page.reload();await page.locator('#manager').click();await card.waitFor()}
 try{
  mode='normal';await ready('/old.zip',{official:true,autoUpdate:false,enabled:false})
  await card.locator('.vp-update-button').waitFor();await card.locator('.vp-update-button').click()
  await page.locator('.vp-review[data-phase=review]').waitFor();assert.match(await page.locator('.vp-review').innerText(),new RegExp(release.version.replaceAll('.','\\.')))
  await page.getByRole('button',{name:zh?'取代已安裝版本':'Replace installed version',exact:true}).click()
  await waitVersion(page,release.version)
  assert.deepEqual(await versions(),{active:release.version,previous:oldVersion,enabled:false,official:true,autoUpdate:false});assert.equal(await card.count(),1)
  await card.getByRole('button',{name:zh?'管理':'Manage',exact:true}).click();await page.getByRole('menuitem',{name:new RegExp(zh?'回退版本':'Roll back')}).click()
  await waitVersion(page,oldVersion)
  await page.reload();await page.waitForTimeout(150);assert.equal((await versions()).active,oldVersion);assert.equal((await versions()).autoUpdate,false)
  reports.push({engine,width,case:'card-update-without-removal-and-rollback-pin',passed:true})
  if(process.env.UPDATE_RED_ONLY)continue
  // A separate context is not needed for seeding: the exact active record is replaced, preserving the real schema.
  mode='normal';await ready('/old.zip',{enabled:false,official:undefined,autoUpdate:undefined})
  await waitVersion(page,release.version)
  assert.equal((await versions()).enabled,false);assert.equal((await versions()).official,true);assert.equal((await versions()).autoUpdate,true)
  reports.push({engine,width,case:'legacy-official-digest-migrates-and-updates',passed:true})
  mode='corrupt';await ready('/old.zip',{official:true,autoUpdate:true});await card.locator('[role=status]').filter({hasText:zh?'更新失敗':'Update failed'}).waitFor();assert.equal((await versions()).active,oldVersion)
  reports.push({engine,width,case:'checksum-failure-keeps-current',passed:true})
  mode='permissions';await ready('/read-only.zip',{official:true,autoUpdate:true});await card.locator('[role=status]').filter({hasText:zh?'檢查更新內容':'Review the update'}).waitFor();assert.equal((await versions()).active,'0.0.1')
  reports.push({engine,width,case:'permission-increase-requires-review',passed:true})
  mode='old';await ready('/old.zip',{official:true,autoUpdate:true});await card.getByRole('button',{name:zh?'開啟':'Open',exact:true}).click();await page.locator('iframe').waitFor();await page.frameLocator('iframe').locator('body').filter({hasText:'Synthetic Session'}).waitFor()
  const other=await context.newPage();other.setDefaultTimeout(10000);mode='normal';await other.goto(origin+'/?lang='+language);await other.locator('#manager').click();await other.locator('.vp-installed[data-update-state=waitingClose]').waitFor();assert.equal((await versions()).active,oldVersion)
  await page.locator('.vp-frame-bar button').first().click();await waitVersion(other,release.version)
  await other.screenshot({path:`${output}/${engine}-${width}-updated.png`,fullPage:true});await other.close()
  reports.push({engine,width,case:'cross-tab-running-view-defers-atomic-update',passed:true})
  mode='offline';await ready('/old.zip',{official:true,autoUpdate:true});await page.locator('.vp-update-error').waitFor();assert.equal((await versions()).active,oldVersion)
  mode='normal';await page.locator('.vp-check-all').click();await waitVersion(page,release.version)
  reports.push({engine,width,case:'offline-retains-view-and-manual-retry-recovers',passed:true})
  mode='future';await page.evaluate(()=>window.dispatchEvent(new Event('focus')))
  await waitVersion(page,futureVersion)
  assert.equal((await versions()).previous,release.version)
  reports.push({engine,width,case:'loaded-host-discovers-newer-live-catalog-without-reload',passed:true})
  mode='normal';await ready('/read-only.zip',{official:undefined,autoUpdate:undefined});await page.locator('.vp-check-all:not([disabled])').waitFor();assert.equal((await versions()).active,'0.0.1');assert.equal(await card.locator('input[type=checkbox]').count(),0)
  reports.push({engine,width,case:'unrecognized-import-is-not-adopted-by-package-id',passed:true})
  assert.deepEqual(errors,[]);console.log(engine,width,'updates passed')
 }catch(error){await page.screenshot({path:`${output}/${engine}-${width}-failure.png`,fullPage:true});throw error}finally{await context.close();await browser.close()}
}}finally{await new Promise(resolve=>server.close(resolve));await writeFile(output+'/results.json',JSON.stringify(reports,null,2)+'\n')}
