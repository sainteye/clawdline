// Real host regression: late trace details must preserve existing message targets.
import assert from 'node:assert/strict'
import {createServer} from 'node:http'
import {readFile,mkdir,writeFile} from 'node:fs/promises'
import {fileURLToPath,pathToFileURL} from 'node:url'
import path from 'node:path'
import {performance} from 'node:perf_hooks'
import {build} from 'esbuild'
const {BOTS_ORIGIN:upstream,BOTS_TOKEN:token,BOTS_MACHINE:machine,BOTS_ARTIFACTS:artifacts,PLAYWRIGHT_MODULE:module}=process.env
assert.ok(upstream&&token&&machine&&artifacts&&module)
await mkdir(artifacts,{recursive:true})
const root=fileURLToPath(new URL('..',import.meta.url)),traffic=[],results=[]
const bundle=await build({entryPoints:[path.join(root,'tools/fixtures/project-assistants-message-menu-host.tsx')],bundle:true,write:false,outfile:'host.js',format:'esm',platform:'browser',target:'es2022',jsx:'automatic',define:{'process.env.NODE_ENV':'"development"',TEMPLATE_MACHINE:JSON.stringify(machine)}})
const files=new Map(bundle.outputFiles.map(f=>['/'+path.basename(f.path),f.contents]))
let scenario,releaseTrace,traceGate
const server=createServer(async(req,res)=>{try{
 const url=new URL(req.url,'http://fixture')
 if(url.pathname.startsWith('/v1/')){
  const active=scenario,chunks=[];for await(const chunk of req)chunks.push(chunk);const body=Buffer.concat(chunks),record={case:active?.tag,method:req.method,path:url.pathname,query:url.search,at:performance.now()};if(body.length)record.body=JSON.parse(body);traffic.push(record)
  if(url.searchParams.get('mode')==='traces'){active.traceRequested=true;record.gateEnter=performance.now();await traceGate;record.gateExit=performance.now()}
  if(url.pathname.endsWith('/react')&&active.delay)await new Promise(r=>setTimeout(r,active.delay))
  const headers={Authorization:'Bearer '+token};for(const key of ['content-type','idempotency-key'])if(req.headers[key])headers[key]=req.headers[key]
  record.upstreamStart=performance.now();const answer=await fetch(upstream+url.pathname+url.search,{method:req.method,headers,...body.length?{body}:{}});record.status=answer.status
  const bytes=Buffer.from(await answer.arrayBuffer());if(url.searchParams.has('mode')){const x=JSON.parse(bytes);record.view={mode:url.searchParams.get('mode'),traceCount:x.head?.trace_category_count,items:x.items?.map(r=>({kind:r.kind,id:r.id}))}};record.upstreamDone=performance.now();res.writeHead(answer.status,{'Content-Type':'application/json'});res.end(bytes);record.end=performance.now();return
 }
 if(url.pathname.startsWith('/catalog/')){res.setHeader('Content-Type','application/json');res.end(await readFile(path.join(root,'public/catalogs',path.basename(url.pathname))));return}
 if(files.has(url.pathname)){res.setHeader('Content-Type',url.pathname.endsWith('.css')?'text/css':'text/javascript');res.end(files.get(url.pathname));return}
 res.setHeader('Content-Type','text/html');res.end('<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="stylesheet" href="/host.css"><style>html,body,#app{height:100%;margin:0;background:#080808}</style><div id="app"></div><script type="module" src="/host.js"></script>')
}catch(error){res.writeHead(500);res.end(String(error))}})
await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve))
const pw=await import(pathToFileURL(module).href),origin='http://127.0.0.1:'+server.address().port
function browserProbe(){
 const log={events:[],frames:[],fetches:[],marks:[]};window.__probe=log
 const rect=n=>n?.getBoundingClientRect().toJSON()??null
 const measure=()=>{const row=document.querySelector('.pa-window [data-message-seq="3"]'),s=document.querySelector('.pa-window .pa-chat-scroll'),menu=document.querySelector('[role=menu].pa-message-menu');return {row:rect(row),bubble:rect(row?.querySelector('.pa-message-bubble')),trigger:rect(row?.querySelector('.pa-message-more')),scrollTop:s?.scrollTop,scrollHeight:s?.scrollHeight,clientHeight:s?.clientHeight,trace:!!document.querySelector('.pa-window .pa-trace'),menu:!!menu&&getComputedStyle(menu).visibility==='visible',busy:row?.querySelector('.pa-reactions')?.getAttribute('aria-busy'),state:row?.querySelector('.pa-reactions')?.getAttribute('data-reaction-state'),own:!!row?.querySelector('.pa-reaction-own'),progress:!!row?.querySelector('.pa-reaction-progress')}}
 window.__measure=measure
 for(const type of ['pointerdown','pointerup','click'])document.addEventListener(type,e=>log.events.push({t:performance.now(),type,label:e.target.closest?.('button')?.getAttribute('aria-label'),class:e.target.closest?.('button')?.className,x:e.clientX,y:e.clientY}),true)
 const native=window.fetch
 window.fetch=async function(input,init){const record={start:performance.now(),url:typeof input==='string'?input:input.url,method:init?.method??'GET'};log.fetches.push(record);try{const response=await native.call(this,input,init);record.headers=performance.now();record.status=response.status;response.clone().text().then(text=>{record.body=performance.now();if(record.url.endsWith('/react')){try{const x=JSON.parse(text);record.receipt={state:x.receipt?.state,event_seq:x.receipt?.event_seq}}catch{}}});return response}catch(e){record.error=String(e);throw e}}
 let running=true;window.__stop=()=>{running=false}
 function frame(){if(!running)return;log.frames.push({t:performance.now(),...measure()});requestAnimationFrame(frame)}requestAnimationFrame(frame)
}
try{
 for(const engine of (process.env.BOTS_ENGINES??'chromium,webkit').split(',')){
  const browser=await pw[engine].launch({headless:true,...engine==='chromium'?{args:['--disable-features=OmniboxEverywhere']}:{}})
  try{for(const width of (process.env.BOTS_WIDTHS??'390,1672').split(',').map(Number))for(const delay of (process.env.BOTS_DELAYS??'0,2500').split(',').map(Number)){
   const tag=engine+'-'+width+'-'+delay,context=await browser.newContext({viewport:{width,height:width===1672?976:720},hasTouch:width<700}),page=await context.newPage(),errors=[]
   scenario={tag,delay,traceRequested:false};traceGate=new Promise(r=>{releaseTrace=r});page.on('pageerror',e=>errors.push(String(e)))
   const click=loc=>width<700?loc.tap():loc.click()
   try{
    await page.addInitScript(browserProbe);await page.goto(origin+'/?lang=zh-Hant');await click(page.locator('.pa-dock'));await page.locator('.pa-window .pa-bot-row').first().waitFor();await click(page.locator('.pa-window .pa-bot-row').first());await page.locator('.pa-window [data-message-seq="4"]').waitFor()
    await page.waitForFunction(()=>[...document.querySelectorAll('.pa-window')].every(n=>n.getAnimations().every(a=>a.playState==='finished')))
    assert.equal(await page.locator('.pa-trace-row').count(),0);assert.equal(scenario.traceRequested,true)
    const before=await page.evaluate(()=>{window.__probe.marks.push({name:'before-trace',t:performance.now()});return window.__measure()})
    await page.screenshot({path:path.join(artifacts,tag+'-before-trace.png')});releaseTrace();await page.locator('.pa-trace > summary').first().waitFor({state:'attached'});await page.evaluate(()=>new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r))))
    const after=await page.evaluate(()=>{window.__probe.marks.push({name:'after-trace',t:performance.now()});return window.__measure()});await page.screenshot({path:path.join(artifacts,tag+'-after-trace.png')})
    const point={x:before.trigger.x+before.trigger.width/2,y:before.trigger.y+before.trigger.height/2},hit=await page.evaluate(({x,y})=>{const n=document.elementFromPoint(x,y);return {class:n?.closest('button')?.className??n?.className,seq:n?.closest('[data-message-seq]')?.getAttribute('data-message-seq')}},point)
    assert.ok(Math.abs(after.bubble.y-before.bubble.y)<0.5 && Math.abs(after.trigger.y-before.trigger.y)<0.5,
      'late trace content moved an existing message target: '+JSON.stringify({tag,before,after,oldPointHit:hit}))
    assert.equal(hit.seq,'3','the old message-action point must still hit the same row')
    assert.ok(hit.class.includes('pa-message-more'),'the old message-action point must still hit More')
    // Observe old-coordinate hit only; never replay an uncertain input.
    const row=page.locator('[data-message-seq="3"]'),trigger=row.locator('.pa-message-more');await click(trigger);await page.locator('[role=menu].pa-message-menu').waitFor()
    await click(page.locator('.pa-reaction-palette button').first());await row.locator('.pa-reaction-own').waitFor();await page.waitForFunction(()=>!document.querySelector('[data-message-seq="3"] .pa-reactions[aria-busy=true]'))
    const addDone=await page.evaluate(()=>{window.__probe.marks.push({name:'add-done',t:performance.now()});return window.__measure()})
    await click(row.locator('.pa-reaction-own'));await row.locator('.pa-reaction-own').waitFor({state:'detached'});await page.waitForFunction(()=>!document.querySelector('[data-message-seq="3"] .pa-reactions[aria-busy=true]'))
    await page.evaluate(()=>new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r))));await page.evaluate(()=>window.__stop())
    const timeline=await page.evaluate(()=>window.__probe),writes=traffic.filter(r=>r.case===tag&&r.path.endsWith('/react'))
    assert.equal(writes.length,2);assert.ok(writes.every(x=>x.status===200));assert.deepEqual(errors,[])
    const observation={tag,width,delay,before,after,delta:{triggerY:after.trigger.y-before.trigger.y,bubbleY:after.bubble.y-before.bubble.y,scrollTop:after.scrollTop-before.scrollTop,scrollHeight:after.scrollHeight-before.scrollHeight},oldPoint:point,oldPointHit:hit,addDone,timeline,errors};results.push(observation)
    await writeFile(path.join(artifacts,tag+'.json'),JSON.stringify(observation,null,2));console.log(JSON.stringify({tag,delta:observation.delta,oldPointHit:hit,reactWrites:writes.length}))
   }catch(e){await page.screenshot({path:path.join(artifacts,tag+'-failure.png')}).catch(()=>{});await writeFile(path.join(artifacts,tag+'-failure.json'),JSON.stringify({error:String(e),errors,body:await page.locator('body').innerText(),probe:await page.evaluate(()=>window.__probe)},null,2));throw e}
   finally{releaseTrace();await context.close()}
  }}finally{await browser.close()}
 }
}finally{await writeFile(path.join(artifacts,'result.json'),JSON.stringify({source:process.env.BOTS_SOURCE_REVISION??'working-tree',results,traffic,externalModelRequests:0,limits:'Loopback synthetic source and scripted provider; first rAF samples DOM before browser paint, not OS compositor visibility; no production bridge timing'},null,2));await new Promise(resolve=>server.close(resolve))}
