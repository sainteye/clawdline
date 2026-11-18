/** Real useProjectAssistantsHost -> Bots -> Chat, including paint-order hit testing. */
import assert from 'node:assert/strict'
import {createServer} from 'node:http'
import {readFile,mkdir,writeFile} from 'node:fs/promises'
import {fileURLToPath,pathToFileURL} from 'node:url'
import path from 'node:path'
import {build} from 'esbuild'
const {BOTS_ORIGIN:upstream,BOTS_TOKEN:token,BOTS_MACHINE:machine,BOTS_ARTIFACTS:artifacts,PLAYWRIGHT_MODULE:module}=process.env
assert.ok(upstream&&token&&machine&&artifacts&&module)
await mkdir(artifacts,{recursive:true})
const root=fileURLToPath(new URL('..',import.meta.url)),traffic=[],results=[]
const bundle=await build({entryPoints:[path.join(root,'tools/fixtures/project-assistants-message-menu-host.tsx')],bundle:true,write:false,outfile:'host.js',format:'esm',platform:'browser',target:'es2022',jsx:'automatic',define:{'process.env.NODE_ENV':'"development"',TEMPLATE_MACHINE:JSON.stringify(machine)}})
const files=new Map(bundle.outputFiles.map(f=>['/'+path.basename(f.path),f.contents]))
const server=createServer(async(req,res)=>{try{
 const url=new URL(req.url,'http://fixture')
 if(url.pathname.startsWith('/v1/')){
  const chunks=[];for await(const chunk of req)chunks.push(chunk);const body=Buffer.concat(chunks),record={method:req.method,path:url.pathname,query:url.search};if(body.length)record.body=JSON.parse(body);traffic.push(record)
  const headers={Authorization:'Bearer '+token};for(const key of ['content-type','idempotency-key'])if(req.headers[key])headers[key]=req.headers[key]
  const answer=await fetch(upstream+url.pathname+url.search,{method:req.method,headers,...body.length?{body}:{}});record.status=answer.status
  res.writeHead(answer.status,{'Content-Type':'application/json'});res.end(Buffer.from(await answer.arrayBuffer()));return
 }
 if(url.pathname.startsWith('/catalog/')){res.setHeader('Content-Type','application/json');res.end(await readFile(path.join(root,'public/catalogs',path.basename(url.pathname))));return}
 if(files.has(url.pathname)){res.setHeader('Content-Type',url.pathname.endsWith('.css')?'text/css':'text/javascript');res.end(files.get(url.pathname));return}
 res.setHeader('Content-Type','text/html');res.end('<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="stylesheet" href="/host.css"><style>html,body,#app{height:100%;margin:0;background:#080808}</style><div id="app"></div><script type="module" src="/host.js"></script>')
}catch(error){res.writeHead(500);res.end(String(error))}})
await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve))
const pw=await import(pathToFileURL(module).href),origin='http://127.0.0.1:'+server.address().port
try{
 for(const engine of (process.env.BOTS_ENGINES??'chromium,webkit').split(',')){
  const browser=await pw[engine].launch({headless:true,...engine==='chromium'?{args:['--disable-features=OmniboxEverywhere']}:{}})
  try{
   for(const width of (process.env.BOTS_WIDTHS??'1672,390,320').split(',').map(Number)){
    const language=process.env.BOTS_LANGUAGE??(width===390?'zh-Hant':'en'),catalog=JSON.parse(await readFile(path.join(root,'public/catalogs',language+'.json'),'utf8')),word=key=>catalog['projectAssistants.'+key]
    const height=width===1672?976:720,tag=engine+'-'+width,context=await browser.newContext({viewport:{width,height},hasTouch:width<700}),page=await context.newPage(),errors=[]
    page.on('pageerror',error=>errors.push(String(error)))
    const click=loc=>width<700?loc.tap():loc.click()
    try{
     await page.goto(origin+'/?lang='+language);await click(page.locator('.pa-orb'));await page.locator('.pa-window .pa-bot-row').first().waitFor();await click(page.locator('.pa-window .pa-bot-row').first());await page.locator('.pa-window .pa-mine').last().waitFor()
     await page.waitForFunction(()=>[...document.querySelectorAll('.pa-window')].every(node=>node.getAnimations().every(a=>a.playState==='finished')))
     const row=page.locator('.pa-window .pa-mine').last(),trigger=row.locator('.pa-message-more')
     await row.scrollIntoViewIfNeeded();const geometry=()=>row.evaluate(node=>({bubble:node.querySelector('.pa-message-bubble').getBoundingClientRect().toJSON(),scroll:node.closest('.pa-chat-scroll').scrollTop,height:node.closest('.pa-chat-scroll').scrollHeight}))
     const before=await geometry();await click(trigger);const menu=page.getByRole('menu',{name:word('messageActions'),exact:true});await menu.waitFor()
     const hits=await menu.evaluate(node=>[...node.querySelectorAll('button')].map(button=>{const rect=button.getBoundingClientRect(),hit=document.elementFromPoint(rect.x+rect.width/2,rect.y+rect.height/2);return {label:button.getAttribute('aria-label')||button.textContent,hit:hit?.tagName,hitClass:hit?.className,topmost:!!hit&&button.contains(hit),rect:rect.toJSON()}}))
     const opened=await geometry(),observation={tag,language,hostPresent:await page.locator('.pa-window').count(),hostLayer:await page.locator('.pa-window').evaluate(node=>getComputedStyle(node).zIndex),hits,before,opened};results.push(observation)
     await page.screenshot({path:path.join(artifacts,tag+'-opened.png')})
     assert.ok(hits.length>=8&&hits.every(hit=>hit.topmost),'every visible menu control must actually win hit testing over the production window: '+JSON.stringify(observation))
     assert.deepEqual(opened,before,'opening menu moved the chat')
     const count=traffic.filter(r=>r.path.endsWith('/react')).length
     await click(page.getByRole('button',{name:word('reactWith')+' 👍',exact:true}));await row.locator('.pa-reaction-own').waitFor();await page.waitForFunction(()=>!document.querySelector('.pa-window .pa-reactions[aria-busy=true]'))
     assert.equal(traffic.filter(r=>r.path.endsWith('/react')).length,count+1);await click(row.getByRole('button',{name:word('removeReaction')+' 👍',exact:true}));await row.locator('.pa-reaction-own').waitFor({state:'detached'});await page.waitForFunction(()=>!document.querySelector('.pa-window .pa-reactions[aria-busy=true]'));assert.equal(traffic.filter(r=>r.path.endsWith('/react')).length,count+2)
     await click(trigger);await page.keyboard.press('Escape');assert.equal(await trigger.evaluate(node=>node===document.activeElement),true);assert.equal(await menu.count(),0)
     await click(trigger);await click(page.locator('.pa-window .pa-composer textarea'));assert.equal(await menu.count(),0,'portal must not intercept clicks outside its menu');assert.equal(await page.locator('.pa-window .pa-composer textarea').evaluate(node=>node===document.activeElement),true)
     await click(trigger);await click(page.locator('.pa-window .pa-chat-identity'));assert.equal(await menu.count(),0,'details must dismiss the menu')
     assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true);assert.deepEqual(errors,[]);observation.reactionWrites=2;observation.escapeFocus=true;observation.outsidePointerPasses=true;observation.detailsDismisses=true
     await page.screenshot({path:path.join(artifacts,tag+'-done.png')})
    }catch(error){await writeFile(path.join(artifacts,tag+'-page-errors.json'),JSON.stringify(errors));await page.screenshot({path:path.join(artifacts,tag+'-failure.png')}).catch(()=>{});throw error}
    finally{await context.close()}
   }
  }finally{await browser.close()}
 }
 console.log('PASS real window host: menu paint/hit testing, pointer/touch reactions, geometry, Escape, outside click and details; external model requests=0')
}finally{await writeFile(path.join(artifacts,'host-menu-evidence.json'),JSON.stringify({results,traffic,externalModelRequests:0},null,2));await new Promise(resolve=>server.close(resolve))}
