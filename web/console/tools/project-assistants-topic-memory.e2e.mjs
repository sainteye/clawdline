/** Real Chat and SourceClient over synthetic authenticated HTTP/Host/SQLite. */
import assert from 'node:assert/strict'
import {createServer} from 'node:http'
import {readFile,mkdir,writeFile} from 'node:fs/promises'
import {fileURLToPath,pathToFileURL} from 'node:url'
import path from 'node:path'
import {build} from 'esbuild'
const {BOTS_ORIGIN:upstream,BOTS_TOKEN:token,BOTS_READ_TOKEN:reader,BOTS_MACHINE:machine,BOTS_BOT:bot,BOTS_ARTIFACTS:artifacts,PLAYWRIGHT_MODULE:module}=process.env
assert.ok(upstream&&token&&reader&&machine&&bot&&artifacts&&module)
await mkdir(artifacts,{recursive:true})
const root=fileURLToPath(new URL('..',import.meta.url)),traffic=[],evidence=[]
const bundle=await build({entryPoints:[path.join(root,'tools/fixtures/project-assistants-topic-memory.tsx')],bundle:true,write:false,outfile:'topic.js',format:'esm',platform:'browser',target:'es2022',jsx:'automatic',define:{'process.env.NODE_ENV':'"development"',READ_MACHINE:JSON.stringify(machine),READ_BOT:JSON.stringify(bot)}})
const files=new Map(bundle.outputFiles.map(f=>['/'+path.basename(f.path),f.contents]))
let loseNext=false
const upstreamRead=async route=>{const r=await fetch(upstream+route,{headers:{Authorization:'Bearer '+token}});assert.equal(r.status,200);return r.json()}
const memoryPath='/v1/project-assistants/'+bot+'/memory'
const server=createServer(async(req,res)=>{try{
 const url=new URL(req.url,'http://fixture')
 if(url.pathname==='/fixture/lose-next'){loseNext=true;res.end('ready');return}
 if(url.pathname==='/fixture/concurrent-edit'){
  const got=await upstreamRead('/v1/project-assistants/'+bot+'?machine_id='+machine),q=new URLSearchParams({machine_id:machine,conversation_id:got.conversation.id,source_epoch:'',layer:'',entry_id:'',revision:''}),view=await upstreamRead(memoryPath+'-view?'+q),id=crypto.randomUUID()
  const body={command:{key:got.bot.key,operation:'memory_save',idempotency_key:id,content_hash:''},expected_revision:view.view.memory.revision,entry:{id,key:got.bot.key,project:got.bot.project,text:'Concurrent synthetic fact '+id,revision:view.view.memory.revision,sources:[],forgotten:false}}
  const answer=await fetch(upstream+memoryPath,{method:'POST',headers:{Authorization:'Bearer '+token,'Content-Type':'application/json','Idempotency-Key':id},body:JSON.stringify(body)});res.writeHead(answer.status);res.end(await answer.text());return
 }
 if(url.pathname.startsWith('/v1/')){
  const readonly=url.searchParams.has('fixture_read');url.searchParams.delete('fixture_read')
  const chunks=[];for await(const chunk of req)chunks.push(chunk);const raw=Buffer.concat(chunks)
  const record={method:req.method,path:url.pathname,query:url.search,body:raw.length?JSON.parse(raw):null};traffic.push(record)
  const headers={Authorization:'Bearer '+(readonly?reader:token),'Content-Type':'application/json'};if(req.headers['idempotency-key'])headers['Idempotency-Key']=req.headers['idempotency-key']
  const answer=await fetch(upstream+url.pathname+url.search,{method:req.method,headers,...(raw.length?{body:raw}:{})}),bytes=Buffer.from(await answer.arrayBuffer());record.status=answer.status
  if(loseNext&&req.method==='POST'&&url.pathname===memoryPath&&answer.status===200){loseNext=false;record.lostAcknowledgement=true;res.writeHead(503,{'Content-Type':'application/json'});res.end(JSON.stringify({error:{code:'pa_outcome_unknown'}}));return}
  res.writeHead(answer.status,{'Content-Type':'application/json'});res.end(bytes);return
 }
 if(url.pathname.startsWith('/catalog/')){res.setHeader('Content-Type','application/json');res.end(await readFile(path.join(root,'public/catalogs',path.basename(url.pathname))));return}
 if(files.has(url.pathname)){res.setHeader('Content-Type',url.pathname.endsWith('.css')?'text/css':'text/javascript');res.end(files.get(url.pathname));return}
 res.setHeader('Content-Type','text/html');res.end('<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><style>html,body,#app{height:100%;margin:0}</style><link rel="stylesheet" href="/topic.css"><div id="app"></div><script type="module" src="/topic.js"></script>')
}catch(error){res.writeHead(500);res.end(JSON.stringify({error:String(error)}))}})
await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve))
const origin='http://127.0.0.1:'+server.address().port,pw=await import(pathToFileURL(module).href)
try{
 for(const engine of (process.env.BOTS_ENGINES??'chromium,webkit').split(',')){
  const browser=await pw[engine].launch({headless:true,...(engine==='chromium'?{args:['--disable-features=OmniboxEverywhere']}:{})})
  try{
   for(const [language,width,height,mode] of [['en',1440,900,'write'],['zh-Hant',390,720,'write'],['en',1440,900,'read'],['zh-Hant',390,720,'old']]){
    const context=await browser.newContext({viewport:{width,height},hasTouch:width===390}),page=await context.newPage(),errors=[],start=traffic.length,name=[engine,language,mode].join('-'),zh=language!=='en'
    page.on('pageerror',error=>errors.push(String(error)))
    const click=locator=>width===390?locator.tap():locator.click(),memory=zh?'記憶':'Memory'
    try{
     await page.goto(origin+'/?'+new URLSearchParams({lang:language,...(mode!=='write'?{[mode]:'1'}:{})}))
     await page.locator('.pa-composer textarea').waitFor();await click(page.locator('.pa-chat-identity'))
     const tab=label=>page.getByRole('tab',{name:label,exact:true})
     await click(tab(memory));const panel=page.locator('.pa-memory');await panel.waitFor();await panel.locator('.pa-memory-index').first().waitFor()
     if(mode!=='write'){
      if(mode==='old')assert.equal(await panel.locator('.pa-memory-create-note').count(),0)
      else assert.equal(await panel.locator('.pa-memory-create-note').isDisabled(),true)
      await click(panel.getByRole('button',{name:(zh?'閱讀筆記':'Read note')+': readonly-topic',exact:true}));await panel.locator('.pa-memory-detail pre').filter({hasText:'READONLY-TOPIC-BODY'}).waitFor()
      if(mode==='read')assert.equal(await panel.locator('.pa-memory-detail button').first().isDisabled(),true)
      assert.equal(traffic.slice(start).filter(r=>r.method!=='GET').length,0)
      evidence.push({name,readonlyOrOlderSource:true,noWrites:true});continue
     }
     const title='Topic '+name,summary='Summary '+name,body=('BODY-ONLY-'+name+'\n').repeat(120)
     await click(panel.locator('.pa-memory-create-note'));const form=panel.locator('.pa-memory-note-editor'),submit=form.locator('button[type=submit]')
     await form.locator('input').fill(title);await form.locator('textarea').nth(0).fill(summary);await form.locator('textarea').nth(1).fill(body)
     await click(tab(zh?'詳情':'Details'));await click(tab(memory));assert.equal(await form.locator('textarea').nth(1).inputValue(),body,'switching details lost a note draft')
     await fetch(origin+'/fixture/lose-next',{method:'POST'});await click(submit);await panel.getByRole('alert').filter({hasText:'pa_outcome_unknown'}).waitFor()
     assert.equal(await form.locator('input').isDisabled(),true);assert.equal(await form.locator('button[type=button]').isDisabled(),true)
     await click(tab(zh?'詳情':'Details'));await click(tab(memory));await click(submit);await form.waitFor({state:'detached'})
     const saves=traffic.slice(start).filter(r=>r.method==='POST'&&r.path===memoryPath&&r.body.entry?.kind==='note')
     assert.equal(saves.length,2);assert.equal(saves[0].lostAcknowledgement,true);assert.deepEqual(saves[0].body,saves[1].body,'unknown retry changed the original request')
     const id=saves[0].body.entry.id,index=panel.locator('.pa-memory-index').filter({hasText:title})
     await index.waitFor();assert.equal((await index.innerText()).trim(),title+'\n'+summary);assert.equal(await panel.getByText(body,{exact:true}).count(),0,'full body appeared in an index')
     assert.equal(traffic.slice(start).filter(r=>r.path.endsWith('/memory-view')&&new URLSearchParams(r.query).get('entry_id')===id).length,0,'index eagerly read a body')
     await click(index);await panel.locator('.pa-memory-detail pre').filter({hasText:'BODY-ONLY-'+name}).waitFor()
     await page.screenshot({path:path.join(artifacts,name+'-detail.png')})
     await click(panel.locator('.pa-memory-detail button').first());await form.locator('textarea').nth(1).fill('Changed '+body)
     const changed=await fetch(origin+'/fixture/concurrent-edit',{method:'POST'});assert.equal(changed.status,200,await changed.text())
     await click(submit);await panel.getByRole('alert').filter({hasText:'pa_stale_version'}).waitFor();assert.equal(await form.locator('textarea').nth(1).inputValue(),'Changed '+body,'conflict lost the local draft')
     await click(form.locator('button[type=button]'));await form.waitFor({state:'detached'});await click(panel.getByRole('button',{name:zh?'重新讀取記憶':'Refresh memory',exact:true}).first());await click(index);await panel.locator('.pa-memory-detail pre').waitFor()
     await click(panel.locator('.pa-memory-detail button').first());await form.locator('textarea').nth(1).fill('Updated '+body);await click(submit);await form.waitFor({state:'detached'});await click(index);await panel.locator('.pa-memory-detail pre').filter({hasText:'Updated BODY-ONLY-'+name}).waitFor()
     const detailRequest=traffic.filter(r=>r.path.endsWith('/memory-view')&&new URLSearchParams(r.query).get('entry_id')===id).at(-1)
     await click(panel.locator('.pa-memory-detail button').last());await index.waitFor({state:'detached'});await panel.locator('.pa-memory-detail').waitFor({state:'detached'})
     const forgotten=await fetch(upstream+detailRequest.path+detailRequest.query,{headers:{Authorization:'Bearer '+token}});assert.equal(forgotten.status,404)
     assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true)
     assert.deepEqual(errors,[]);await page.screenshot({path:path.join(artifacts,name+'-forgotten.png')})
     evidence.push({name,id,draftKept:true,sameUnknownRequest:true,bodyOnDemand:true,conflictKeptDraft:true,updatedSameID:true,forgottenDetail404:true})
    }catch(error){await page.screenshot({path:path.join(artifacts,name+'-failure.png')});await writeFile(path.join(artifacts,name+'-failure.html'),await page.content());throw error}
    finally{await context.close()}
   }
  }finally{await browser.close()}
 }
 console.log('Topic editor: actual Chat + SourceClient + HTTP/Host/SQLite, bilingual keyboard/touch, CAS conflict, lost reply, exact retry, body detail and forget. Native prompts=0.')
}finally{
 await writeFile(path.join(artifacts,'traffic.json'),JSON.stringify(traffic,null,2));await writeFile(path.join(artifacts,'topic-editor-evidence.json'),JSON.stringify({evidence,nativePrompts:0},null,2));await new Promise(resolve=>server.close(resolve))
}
