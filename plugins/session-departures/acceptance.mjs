/** Synthetic loopback fixture using the production manager, store, frame, bridge and SDK. */
import assert from 'node:assert/strict'
import {createServer} from 'node:http'
import {readFile,mkdir,writeFile} from 'node:fs/promises'
import {createRequire} from 'node:module'
import {fileURLToPath,pathToFileURL} from 'node:url'
import {resolve} from 'node:path'
const root=fileURLToPath(new URL('../../',import.meta.url))
const require=createRequire(resolve(root,'web/package.json'))
const {build}=require('esbuild')
const output=process.env.PLUGIN_ARTIFACTS
if(!output||!process.env.PLAYWRIGHT_MODULE)throw Error('Set PLUGIN_ARTIFACTS and PLAYWRIGHT_MODULE')
await mkdir(output,{recursive:true})
const bundle=await build({stdin:{contents:`
import React from 'react';import{createRoot}from'react-dom/client';import{useViewPlugins}from'./view-plugins/Host';

const states=['ready','working','holding','waiting_you','waiting_session','unknown','milestone_complete','work_complete'];
const titles=['準備出發的設計研究','整理繁體中文工作航班板與 Unicode 名稱 — 完整標題保留 ✦ 東京 / 臺北','暫停中的合成工作','請確認設計方向：繁體中文不應變成空白','等待另一個 Session','尚無可信狀態','回報階段完成，尚待驗收','回報工作完成，尚待驗收'];
const rows=states.map((workState,i)=>({key:'row-'+i,title:titles[i],project:i===5?null:{key:'project-'+i,label:i===1?'示範 Project・臺灣 / 日本':'Synthetic Project '+i},workState,freshness:'fresh'}));
rows.push({key:'stale',title:'過時的合成 Session',project:null,workState:'working',freshness:'stale'},{key:'hostile',title:'<img src=x onerror=alert(1)> 中文 & 🚀',project:{key:'hostile-project',label:'<svg onload=alert(1)> 專案'},workState:'unknown',freshness:'unknown'});
const listeners=new Set();let connected=true;const navigations=[];
window.fixture={rows,navigations,change(){listeners.forEach(fn=>fn())},disconnect(){connected=false;this.change()},reconnect(){connected=true;this.change()},empty(){rows.splice(0);this.change()},visibility(visible){Object.defineProperty(document,'visibilityState',{configurable:true,value:visible?'visible':'hidden'});document.dispatchEvent(new Event('visibilitychange'))}};
function Host(){const api=useViewPlugins([{key:'synthetic-source',label:'Synthetic acceptance data',async read(){return{sessions:[...rows],connected}},subscribe(fn){listeners.add(fn);return()=>listeners.delete(fn)},async prepareOpen(key){return()=>navigations.push(key)}}],document.documentElement.lang);return <><p>Synthetic acceptance data · no real Sessions</p><button id="manager" onClick={e=>api.openManager(e.currentTarget)}>Manager</button>{api.dock}{api.content}</>}
createRoot(document.getElementById('root')).render(<Host/>);
`,resolveDir:resolve(root,'web/console/src'),loader:'tsx'},bundle:true,write:false,outfile:'host.js',format:'esm',platform:'browser',jsx:'automatic',define:{'process.env.NODE_ENV':'"development"'}})
const files=new Map(bundle.outputFiles.map(f=>['/'+f.path.split('/').at(-1),f.text]))
let forbidden=0
const server=createServer((req,res)=>{const u=new URL(req.url,'http://fixture');let body;if(u.pathname==='/')body=`<!doctype html><html lang="${u.searchParams.get('lang')==='en'?'en':'zh-Hant'}"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="stylesheet" href="/host.css"><div id="root"></div><script type="module" src="/host.js"></script>`;else body=files.get(u.pathname);if(u.pathname.startsWith('/v1/'))forbidden++;if(body===undefined){res.writeHead(404);res.end();return}res.setHeader('Content-Type',u.pathname.endsWith('.js')?'text/javascript':u.pathname.endsWith('.css')?'text/css':'text/html');res.end(body)})
await new Promise(r=>server.listen(0,'127.0.0.1',r))
const origin=`http://127.0.0.1:${server.address().port}`,pw=await import(pathToFileURL(process.env.PLAYWRIGHT_MODULE).href),reports=[]
const archive=await readFile(resolve(root,'web/console/public/view-plugins/session-departures.zip'))
const upstream=await import('data:text/javascript;base64,'+Buffer.from(await readFile(resolve(root,'plugins/session-departures/upstream/logic.js'),'utf8')).toString('base64'));const actualHTML=await readFile(resolve(root,'plugins/session-departures/index.html'),'utf8');const pure=actualHTML.slice(actualHTML.indexOf('// One drum'),actualHTML.indexOf('const $='));const port=new Function(pure+';return {fitBoard,columnsOf,flipPath,Flaps}')();
for(const width of [700,900,1440,1800,3000])for(const zoom of [1,1.2,1.5])assert.deepEqual(port.fitBoard(width,400,zoom),upstream.fitBoard(width,400,zoom));
for(const dest of [6,16,17,32])assert.deepEqual(port.columnsOf(dest),upstream.columnsOf(dest));
for(const from of [' ','A','Z','?'])for(const to of [' ','B','H','?'])assert.deepEqual(port.flipPath(from,to),upstream.flipPath(from,to));
const a=new port.Flaps(2,6,['AAAAAA','ABCDEF']),b=new upstream.Flaps(2,6,['AAAAAA','ABCDEF']);a.aim(['BBBBBB','FEDCBA']);b.aim(['BBBBBB','FEDCBA']);let ticks=0;do{assert.deepEqual(a.grid,b.grid);assert.equal(a.tick(),b.tick());ticks++}while(a.busy);assert.ok(ticks>1);console.log('Pure fixture parity: sizing 15, columns 4, drum 16, full two-row flap burst '+ticks+' ticks');const started=Date.now()
try{for(const engine of ['chromium','webkit'])for(const width of [390,1440]){
 const browser=await pw[engine].launch({headless:true}),page=await browser.newPage({viewport:{width,height:900},reducedMotion:'no-preference'}),errors=[]
 page.on('pageerror',e=>errors.push(e.message))
 const prefix=`${engine}-${width}`
 try{
 const zh=width===390;await page.goto(origin+'/?lang='+(zh?'zh-Hant':'en'));await page.locator('#manager').click()
 await page.locator('input[type=file]:not([disabled])').waitFor({state:'attached'})
 await page.locator('input[type=file]').setInputFiles({name:'session-departures.zip',mimeType:'application/zip',buffer:archive})
 await page.locator('.vp-review').waitFor();assert.match(await page.locator('.vp-review').textContent(),/com.clawdline.session-departures · 0.5.0/)
 await page.getByRole('button',{name:zh?'安裝套件':'Install package',exact:true}).click()
 await page.locator('.vp-installed').getByRole('button',{name:zh?'開啟':'Open',exact:true}).click()
 const frame=page.frameLocator('iframe');async function boardRow(title){for(let i=0;i<10;i++){const row=frame.locator('.row[aria-label^='+JSON.stringify(title)+']');if(await row.count())return row;if(!await frame.locator('#nextPage').isVisible())break;await frame.locator('#nextPage').click()}throw Error('Session missing from every board page: '+title)};await frame.locator('.row[data-ref]').first().waitFor();await frame.locator('body').evaluate(()=>document.fonts.ready)
 assert.equal(await frame.locator('.list-row').count(),10)
 assert.match(await frame.locator('.row').first().textContent(),/請確認設計方向/)
 assert.equal(await frame.locator('.list-state').allTextContents().then(x=>new Set(x).size),8)
 assert.equal(await frame.locator('img').count(),0)
 assert.match(await frame.locator('.list-row').filter({hasText:'<img src=x'}).textContent(),/<svg onload=alert\(1\)>/)
 assert.equal(await frame.locator('body').evaluate(el=>document.documentElement.scrollWidth<=innerWidth&&el.scrollWidth<=innerWidth),true,'No whole-page horizontal overflow')
 assert.equal(await page.evaluate(()=>fixture.navigations.length),0)
 await frame.locator('#board[data-moving=false]').waitFor();await page.screenshot({path:`${output}/${prefix}-board.png`})
 if(process.env.PLUGIN_FIDELITY_MUTATION==='cell-width')await frame.locator('.row .cell').evaluateAll(cells=>cells.forEach(cell=>cell.style.width='20px'));
 const geometry=await frame.locator('.row[data-ref]').first().evaluate(el=>{const cells=[...el.querySelectorAll('.cell')],box=n=>{const r=n.getBoundingClientRect();return{x:r.x,y:r.y,width:r.width,height:r.height}};return{row:box(el),cells:cells.map(box),seam:box(el.querySelector('.seam')),columns:[...document.querySelectorAll('.column')].map(n=>({name:n.textContent,...box(n)})),paper:getComputedStyle(cells[0]).backgroundColor,ink:getComputedStyle(document.body).backgroundColor}})
 const boardWidth=await frame.locator('#board').evaluate(el=>el.clientWidth);const shape=upstream.fitBoard(boardWidth,400)
 assert.equal(geometry.cells.length,shape.cells);assert.ok(Math.abs(geometry.cells[0].width-shape.cell)<.05);assert.ok(Math.abs(geometry.cells[0].height-shape.cellH)<.05);assert.ok(Math.abs(geometry.seam.y-geometry.row.y-shape.half)<.05)
 assert.equal(geometry.ink,'rgb(5, 5, 5)');assert.equal(geometry.paper,'rgb(25, 25, 23)')
 for(const col of upstream.columnsOf(shape.dest))assert.ok(Math.abs(geometry.columns.find(x=>x.name===col.name).x-geometry.cells[col.start].x)<.1,'Heading aligns to actual cell')
 await writeFile(`${output}/${prefix}-geometry.json`,JSON.stringify({boardWidth,shape,geometry},null,2))
 await frame.locator('#list').click();assert.equal(await frame.locator('.list-row').first().evaluate(el=>el.getBoundingClientRect().height>=44),true);await page.screenshot({path:`${output}/${prefix}-list.png`});await frame.locator('#list').click()

 const hostile=await boardRow('<img src=x');await hostile.click();assert.equal(await frame.locator('#open').isDisabled(),true)
 assert.match(await frame.locator('#detail').textContent(),zh?/新鮮度未知/:/Freshness unknown/)
 await (await boardRow('過時的合成 Session')).click();assert.equal(await frame.locator('#open').isDisabled(),true)
 await (await boardRow('尚無可信狀態')).click();assert.match(await frame.locator('#detail').textContent(),zh?/未歸屬 Project/:/No Project/);assert.equal(await frame.locator('#open').isDisabled(),true)
 const full='整理繁體中文工作航班板與 Unicode 名稱 — 完整標題保留 ✦ 東京 / 臺北'
 await (await boardRow(full)).click();assert.equal(await frame.locator('#detail h2').textContent(),full)
 assert.equal(await page.evaluate(()=>fixture.navigations.length),0,'Selection never navigates')
 await frame.locator('#list').click();assert.equal(await frame.locator('.list-row').count(),10);assert.equal(await frame.locator('#detail h2').textContent(),full)
 await frame.locator('#list').click()
 await page.evaluate(()=>{fixture.rows[1].workState='waiting_you';fixture.change()})
 await frame.locator('.row.waiting_you').filter({hasText:full}).waitFor();await page.evaluate(()=>{fixture.rows[2].title='PORT THE PARSER';fixture.change()});await frame.locator('.row.holding').filter({hasText:'PORT THE PARSER'}).waitFor();await page.evaluate(()=>{fixture.rows[2].title='REVIEW TOKENS';fixture.change()})
 assert.equal(await frame.locator('#detail h2').textContent(),full,'Selection survives a reordered snapshot')
 await boardRow('REVIEW TOKENS');await frame.locator('.row.holding .half').first().waitFor();assert.ok(await frame.locator('.row.holding .half').count()>0,'Original half-flap wave moves changing Latin cells')
 await frame.locator('#still').click();assert.equal(await frame.locator('body').evaluate(el=>el.getAnimations({subtree:true}).length),0);assert.equal(await frame.locator('.half').count(),0)
 await frame.locator('#still').click()
 await page.emulateMedia({reducedMotion:'reduce'});await frame.locator('body.still').waitFor()
 assert.equal(await frame.locator('body').evaluate(el=>el.getAnimations({subtree:true}).length),0);assert.equal(await frame.locator('.half').count(),0)
 await page.emulateMedia({reducedMotion:'no-preference'});await frame.locator('body:not(.still)').waitFor()
 await page.evaluate(()=>fixture.visibility(false));await frame.locator('body.still').waitFor()
 assert.equal(await frame.locator('body').evaluate(el=>el.getAnimations({subtree:true}).length),0);assert.equal(await frame.locator('.half').count(),0)
 await page.evaluate(()=>fixture.visibility(true));await frame.locator('body:not(.still)').waitFor()
 await page.evaluate(()=>fixture.disconnect());await frame.locator('body.still').waitFor();await frame.locator('#open:disabled').waitFor()
 assert.equal(await frame.locator('.row[data-freshness=fresh]').count(),0)
 await page.evaluate(()=>fixture.reconnect());await frame.locator('#open:not(:disabled)').waitFor()
 await page.evaluate(lang=>{document.documentElement.lang=lang},zh?'en':'zh-Hant')
 await frame.locator('#title').filter({hasText:zh?'Session Departures':'Session 航班板'}).waitFor()
 await frame.locator('#detail').screenshot({path:`${output}/${prefix}-detail.png`})
 assert.equal(await frame.locator('body').evaluate(el=>document.documentElement.scrollWidth<=innerWidth&&el.scrollWidth<=innerWidth),true)
 await frame.locator('#open').click();await page.waitForFunction(()=>fixture.navigations.length===1)
 assert.deepEqual(await page.evaluate(()=>fixture.navigations),['row-1'])
 await page.locator('iframe').waitFor({state:'detached'})
 await page.locator('.vp-dock').click();await frame.locator('.row').first().waitFor()
 await page.evaluate(()=>fixture.empty());await frame.locator('#empty:not([hidden])').waitFor();assert.equal(await frame.locator('.row[data-ref]').count(),0)
 await page.screenshot({path:`${output}/${prefix}-empty.png`})
 await page.locator('.vp-frame-bar button[aria-label]').click();await page.locator('iframe').waitFor({state:'detached'})
 assert.deepEqual(errors,[]);assert.equal(forbidden,0)
 reports.push({engine,width,initialLanguage:zh?'zh-Hant':'en',switchedLanguage:zh?'en':'zh-Hant',passed:true,cases:['STORE ZIP import and review','all 8 states','waiting_you priority','Unicode full detail','hostile text','null Project','stale and unknown freshness','snapshot update and selection','list parity','explicit navigation','motion active/still/reduced/hidden/disconnected','reconnect','empty','no horizontal overflow','trusted return']})
 }catch(error){await page.screenshot({path:`${output}/${prefix}-failure.png`});throw error}finally{await browser.close()}
}}finally{await new Promise(r=>server.close(r));await writeFile(`${output}/results.json`,JSON.stringify({synthetic:true,seconds:(Date.now()-started)/1000,reports},null,2))}
console.log(JSON.stringify(reports,null,2))
