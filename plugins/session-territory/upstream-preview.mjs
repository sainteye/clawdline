// Synthetic source study only. No daemon, native helper, transcript or host API.
import assert from 'node:assert/strict';
import { readFile, writeFile, mkdir } from 'node:fs/promises';
import { createServer } from 'node:http';
import { resolve, dirname, extname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { createHash } from 'node:crypto';
import { layoutMap, weightOf, squarify } from './upstream/treemap.js';
import { addTouches, heatLevel, collisions, TRAIL_MS, trailOf } from './upstream/activity.js';
import { syntheticFixture, buildFrame, zoneOf } from './upstream-renderer.js';

const here=dirname(fileURLToPath(import.meta.url));
const output=process.env.ARTIFACT_DIR;
if(!output||!process.env.PLAYWRIGHT_MODULE)throw new Error('Set ARTIFACT_DIR and PLAYWRIGHT_MODULE to existing authorized locations');
await mkdir(output,{recursive:true});
const fixture=syntheticFixture();
assert.equal(fixture.repo.files.length,60);
assert.equal(weightOf(8000),20);
assert.equal(weightOf(64000)/weightOf(8000),2);
assert.equal(weightOf(0),Math.cbrt(256));
const spots=squarify([{weight:8},{weight:4},{weight:2}],{x:0,y:0,w:700,h:400});
const expectedSpots=[[0,0,400,400],[400,0,300,800/3],[400,800/3,300,400/3]];
spots.forEach((s,i)=>[s.x,s.y,s.w,s.h].forEach((n,j)=>assert.ok(Math.abs(n-expectedSpots[i][j])<1e-9)));
assert.equal(heatLevel([fixture.now],fixture.now),.7);
assert.equal(heatLevel([fixture.now-300000],fixture.now),0);
const touches=[{at:fixture.now,agent:'a',tool:'Edit',path:'x',kind:'write'},{at:fixture.now,agent:'b',tool:'Read',path:'x',kind:'read'}];
assert.equal(collisions(touches,fixture.now).length,1);
assert.equal(collisions(touches,fixture.now+120000).length,0);
assert.equal(collisions(touches,fixture.now,key=>key==='b'?'a':null).length,0);
assert.equal(addTouches(touches,touches,fixture.now).length,2);
assert.equal(trailOf(touches,'b',fixture.now+TRAIL_MS).length,0);
// Load the retained frame's geometry with only its unused native crew import
// replaced by the unchanged exported MAX_DOTS value. No upstream reader runs.
const retainedFrame=(await readFile(join(here,'upstream/frame.js'),'utf8'))
  .replace('from "./activity.js"',`from ${JSON.stringify(pathToFileURL(join(here,'upstream/activity.js')).href)}`)
  .replace('import { MAX_DOTS } from "./crew.js";','const MAX_DOTS = 12;');
const original=await import('data:text/javascript;base64,'+Buffer.from(retainedFrame).toString('base64'));
for(const width of [390,1440]){
  const map=layoutMap({files:fixture.repo.files,width,height:zoneOf(460)});
  const input={map,crew:fixture.crew,touches:fixture.touches,now:fixture.now,width,height:460,picked:map.tiles[0].key};
  assert.deepEqual(buildFrame(input),original.buildFrame(input));
  for(const r of [...map.tiles,...map.folders])assert.ok(r.x>=0&&r.y>=0&&r.x+r.w<=width&&r.y+r.h<=zoneOf(460));
}
const many=layoutMap({files:Array.from({length:1500},(_,i)=>[`pkg${i%40}/f${i}.ts`,500+i]),width:390,height:388});
assert.ok(many.tiles.length<=140);assert.ok(many.grouped>0);
const hashes={};
for(const name of ['LICENSE','treemap.js','activity.js','frame.js','view.js','crew.js']){
  const retained=await readFile(join(here,'upstream',name));hashes[name]=createHash('sha256').update(retained).digest('hex');
  if(process.env.UPSTREAM_REFERENCE_DIR){const source=join(process.env.UPSTREAM_REFERENCE_DIR,name==='LICENSE'?name:'mods/territory/'+name);assert.deepEqual(retained,await readFile(source));}
}
console.log('PASS geometry, byte weights, exact retained-frame parity, heat/trail/collision windows, grouping, provenance');
const mime={'.html':'text/html','.js':'text/javascript','.mjs':'text/javascript'};
const server=createServer(async(req,res)=>{try{
  const path=resolve(here,'.'+decodeURIComponent(new URL(req.url,'http://local').pathname));
  if(!path.startsWith(here+'/')){res.writeHead(403).end();return;}
  const data=await readFile(path);res.writeHead(200,{'Content-Type':mime[extname(path)]||'text/plain'}).end(data);
}catch{res.writeHead(404).end();}});
await new Promise(r=>server.listen(0,'127.0.0.1',r));
const base=`http://127.0.0.1:${server.address().port}`;
const engines=await import(pathToFileURL(resolve(process.env.PLAYWRIGHT_MODULE)).href);
const results=[];
try{for(const engine of ['chromium','webkit']){
  const browser=await engines[engine].launch({headless:true});
  try{for(const width of [390,1440]){
    const page=await browser.newPage({viewport:{width,height:1000}}),errors=[],requests=[];
    page.on('pageerror',e=>errors.push(e.message));page.on('request',r=>requests.push(r.url()));
    await page.goto(base+'/upstream-preview.html');await page.waitForFunction(()=>window.territoryStudy?.frame()?.map.tiles.length>0);
    assert.match(await page.locator('.synthetic').textContent(),/SYNTHETIC/);
    assert.equal(await page.locator('.state-row').count(),8);
    assert.equal(await page.locator('.tile').count(),await page.evaluate(()=>window.territoryStudy.frame().map.tiles.length));
    const measured=await page.evaluate(()=>{
      const map=window.territoryStudy.frame().map;
      return [...document.querySelectorAll('.tile')].every(n=>{const t=map.tiles.find(t=>t.key===n.dataset.key);return parseFloat(n.style.left)===t.x&&parseFloat(n.style.top)===t.y&&parseFloat(n.style.width)===t.w&&parseFloat(n.style.height)===t.h;});
    });assert.equal(measured,true);
    assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true);
    assert.equal(await page.locator('.trails polyline').count()>0,true);
    assert.equal(await page.locator('.collision-wash').count()>0,true);
    await page.screenshot({path:join(output,`${engine}-${width}-en.png`),fullPage:true});
    if(engine==='chromium'&&width===1440&&process.env.UPSTREAM_REFERENCE_DIR){
      const originalImage=await readFile(join(process.env.UPSTREAM_REFERENCE_DIR,'reference-preview.png'));
      const port=await page.screenshot({fullPage:false});
      const comparison=await browser.newPage({viewport:{width:2880,height:1040}});
      await comparison.setContent(`<style>body{margin:0;background:#121416;color:#eee;font:18px sans-serif}main{display:flex}section{width:1440px}h2{height:40px;margin:0;padding:8px 16px;box-sizing:border-box}img{display:block;width:1440px}</style><main><section><h2>Original upstream preview · native terminal belongs to reference</h2><img src="data:image/png;base64,${originalImage.toString('base64')}"></section><section><h2>Browser port · SYNTHETIC data · no native terminal</h2><img src="data:image/png;base64,${port.toString('base64')}"></section></main>`);
      await comparison.screenshot({path:join(output,'original-versus-port-1440.png'),fullPage:true});
      const portMap=await page.locator('.territory-viewport').screenshot();
      await comparison.setViewportSize({width:2880,height:540});
      await comparison.setContent(`<style>body{margin:0;background:#121416;color:#eee;font:18px sans-serif}main{display:flex}section{width:1440px}h2{height:40px;margin:0;padding:8px 16px;box-sizing:border-box}.crop{height:498px;overflow:hidden}img{display:block;width:1440px}</style><main><section><h2>Original upstream map · reference crop</h2><div class="crop"><img src="data:image/png;base64,${originalImage.toString('base64')}"></div></section><section><h2>Browser map · SYNTHETIC reference only</h2><img src="data:image/png;base64,${portMap.toString('base64')}"></section></main>`);
      await comparison.screenshot({path:join(output,'original-versus-port-map-1440.png'),fullPage:true});await comparison.close();
    }
    await page.locator('.tile').first().click();assert.equal(await page.locator('.file-card').count(),1);
    await page.keyboard.press('Escape');assert.equal(await page.locator('.file-card').count(),0);
    await page.locator('.agent-dot').first().click();assert.equal(await page.locator('.selection button').isDisabled(),true);
    await page.evaluate(()=>window.territoryStudy.setContext({language:'zh-Hant-TW'}));
    assert.match(await page.locator('.synthetic').textContent(),/合成參考資料/);
    await page.getByRole('button',{name:'易讀清單',exact:true}).click();assert.equal(await page.locator('.file-row').count(),60);
    await page.getByRole('button',{name:'易讀清單',exact:true}).click();
    await page.screenshot({path:join(output,`${engine}-${width}-zh.png`),fullPage:true});
    await page.getByRole('button',{name:'靜止模式',exact:true}).click();assert.equal(await page.locator('#study').getAttribute('data-motion'),'on');
    for(const settings of [{visible:false},{visible:true,reducedMotion:true},{reducedMotion:false,freshness:'stale'},{freshness:'unknown'},{freshness:'fresh',connection:'disconnected'}]){
      await page.evaluate(c=>window.territoryStudy.setContext(c),settings);assert.equal(await page.locator('#study').getAttribute('data-motion'),'off');
    }
    assert.equal(await page.locator('.wash,.collision-wash,.trails polyline').count(),0);
    await page.screenshot({path:join(output,`${engine}-${width}-disconnected.png`),fullPage:true});
    await page.evaluate(()=>window.territoryStudy.setContext({connection:'connected',freshness:'fresh'}));
    assert.equal(await page.locator('.wash,.collision-wash,.trails polyline').count(),0);
    await page.evaluate(()=>window.territoryStudy.setFixture(window.territorySyntheticFixture()));
    assert.ok(await page.locator('.wash').count()>0);
    await page.evaluate(()=>{const f=window.territorySyntheticFixture();f.repo.name='<img src=x onerror=alert(1)>測試🌱';f.repo.files=[['src/日本語與繁體🌱<svg onload=alert(1)>.ts',8000]];f.crew[0].name='<img onerror=alert(1)>代理🌱';window.territoryStudy.setFixture(f);});
    assert.equal(await page.locator('img,svg[onload]').count(),0);assert.match(await page.locator('.repo-title').textContent(),/測試🌱/);
    await page.evaluate(()=>{try{window.territoryStudy.setFixture({synthetic:false});return false;}catch(e){return e.message==='synthetic_fixture_required';}}).then(x=>assert.equal(x,true));
    await page.evaluate(()=>{const f=window.territorySyntheticFixture();f.repo.files=[];f.touches=[];window.territoryStudy.setFixture(f);});assert.equal(await page.locator('.tile').count(),0);
    assert.deepEqual(errors,[]);assert.ok(requests.every(url=>url.startsWith(base+'/')));
    await page.close();results.push({engine,width,pass:true,cases:'geometry, labels, card/Escape, disabled navigation, 8 states, en/zh, list, still/hidden/reduced/stale/unknown/disconnected, unsafe text, empty, no external request'});
    console.log(`PASS ${engine} ${width}: geometry + safety + context + bilingual screenshots`);
  }}finally{await browser.close();}
}}finally{await new Promise(r=>server.close(r));}
await writeFile(join(output,'prototype-results.json'),JSON.stringify({synthetic:true,commit:'b53947d6b9ebe10208d1b7edf2948d3e68dd3086',hashes,results},null,2)+'\n');
console.log('PASS all prototype checks; not live-data or production acceptance');
