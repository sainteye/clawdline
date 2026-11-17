/** Complete parent/room/member navigation. No production browser or provider is used. */
import assert from 'node:assert/strict'
import {createServer} from 'node:http'
import {readFile,mkdir,writeFile} from 'node:fs/promises'
import {execFileSync} from 'node:child_process'
import {fileURLToPath,pathToFileURL} from 'node:url'
import path from 'node:path'
import {build} from 'esbuild'
const {BOTS_ORIGIN:upstream,BOTS_CLOUD_ORIGIN:cloud,BOTS_ARTIFACTS:artifacts,PLAYWRIGHT_MODULE:module}=process.env
assert.ok(upstream&&cloud&&artifacts&&module,'Requires the task-owned HTTP/SQLite fixture and installed Playwright')
await mkdir(artifacts,{recursive:true})
const root=fileURLToPath(new URL('..',import.meta.url)),baseline=process.env.BOTS_NAV_BASELINE
const bundle=await build({entryPoints:[path.join(root,'tools/fixtures/project-assistants-group-navigation.tsx')],bundle:true,write:false,outfile:'navigation.js',format:'esm',platform:'browser',target:'es2022',jsx:'automatic',define:{'process.env.NODE_ENV':'"development"'},plugins:baseline?[{name:'recorded-navigation-baseline',setup(plugin){plugin.onLoad({filter:/project-assistants\/(Groups|BotRoster|Bots)\.tsx$/},args=>({contents:execFileSync('git',['show',baseline+':'+path.relative(path.resolve(root,'../..'),args.path)],{cwd:path.resolve(root,'../..'),encoding:'utf8'}),loader:'tsx',resolveDir:path.dirname(args.path)}))}}]:[]})
const files=new Map(bundle.outputFiles.map(file=>['/'+path.basename(file.path),file.contents])),traffic=[]
const server=createServer(async(req,res)=>{
 try{
  const url=new URL(req.url,'http://fixture')
  if(url.pathname.startsWith('/proxy/')||url.pathname.startsWith('/relay/')){
   const chunks=[];for await(const chunk of req)chunks.push(chunk);const raw=Buffer.concat(chunks).toString(),relay=url.pathname.startsWith('/relay/')
   const record={method:req.method,path:url.pathname,relay,body:raw?JSON.parse(raw):null};traffic.push(record)
   let answer
   if(relay){const word=url.pathname.slice(7);record.word=word;answer=await fetch(cloud,{method:'POST',body:JSON.stringify({type:word,session:'__clawdline_machine__',request:req.headers['idempotency-key']??'read-'+crypto.randomUUID(),...record.body})});const payload=await answer.json();record.status=answer.status;res.writeHead(answer.status,{'Content-Type':'application/json'});res.end(JSON.stringify(payload.body??{error:payload.error}));return}
   const mode=url.pathname.split('/')[2],pathname=url.pathname.replace('/proxy/'+mode,'')+url.search;record.path=pathname
   answer=await fetch(upstream+pathname,{method:req.method,headers:{Authorization:'Bearer '+(mode==='read'?'reader':'person'),...(raw?{'Content-Type':'application/json','Idempotency-Key':req.headers['idempotency-key']??''}:{})},...(raw?{body:raw}:{})})
   record.status=answer.status;res.writeHead(answer.status,{'Content-Type':'application/json'});res.end(Buffer.from(await answer.arrayBuffer()));return
  }
  if(url.pathname.startsWith('/catalog/')){res.setHeader('Content-Type','application/json');res.end(await readFile(path.join(root,'public/catalogs',path.basename(url.pathname))));return}
  if(files.has(url.pathname)){res.setHeader('Content-Type',url.pathname.endsWith('.css')?'text/css':'text/javascript');res.end(files.get(url.pathname));return}
  res.setHeader('Content-Type','text/html');res.end('<!doctype html><html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><style>html,body,#app{height:100%;margin:0}</style><link rel="stylesheet" href="/navigation.css"><div id="app"></div><script type="module" src="/navigation.js"></script></html>')
 }catch(error){res.writeHead(500);res.end(JSON.stringify({error:String(error)}))}
})
await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve))
const origin=`http://127.0.0.1:${server.address().port}`,pw=await import(pathToFileURL(module).href),evidence=[]
const position=room=>room.locator('.pa-chat-scroll').evaluate(el=>{const top=el.getBoundingClientRect().top,row=[...el.querySelectorAll('[data-room-seq]')].find(row=>row.getBoundingClientRect().bottom>top);return {top:el.scrollTop,seq:row?.dataset.roomSeq,offset:row?row.getBoundingClientRect().top-top:null}})
try{
 for(const engine of (process.env.BOTS_ENGINES??'chromium,webkit').split(',')){
  const browser=await pw[engine].launch({headless:true,...(engine==='chromium'?{args:['--disable-features=OmniboxEverywhere']}:{})})
  try{
   const cases=process.env.BOTS_NAV_PHONE_ONLY==='1'?[['local','zh-Hant',390,720],['cloud','zh-Hant',390,720]]:[['local','en',1440,900],['local','zh-Hant',390,720],['cloud','en',1440,900],['cloud','zh-Hant',390,720],['read','en',1440,900],['old','zh-Hant',390,720]]
   for(const [mode,language,width,height] of cases){
    const context=await browser.newContext({viewport:{width,height},hasTouch:width===390}),page=await context.newPage(),errors=[],start=traffic.length
    page.on('pageerror',error=>errors.push(String(error)))
    const name=[engine,mode,language].join('-'),click=locator=>width===390?locator.tap():locator.click()
    try{
     await page.goto(origin+'/?'+new URLSearchParams({mode,lang:language,history:mode==='cloud'?'1':'0'}))
     const bot=id=>page.locator('[data-bot-key]').filter({has:page.locator('strong',{hasText:new RegExp('^'+id+'$')})}),row=page.locator('[data-group-key]').filter({has:page.getByText('Review room',{exact:true})})
     await bot('Stranger').waitFor();await row.waitFor()
     assert.equal(traffic.slice(start).filter(record=>record.word==='project-assistants.group-history'||/\/rooms\/groups\/[^/?]+\?/.test(record.path)).length,0,'list preview fetched room histories')
     assert.equal(await row.locator('.pa-avatar').count(),2)
     assert.match(await row.locator('.pa-row-preview').innerText(),/Bob:/)
     await click(bot('Stranger'))
     await page.locator('.pa-main .pa-chat-header h2').filter({hasText:'Stranger'}).waitFor()
     if(width>390)assert.equal(await page.locator('.pa-main .pa-chat-back').isVisible(),false,'ordinary desktop chat gained a group return action')
     if(width===390){await click(page.locator('.pa-main .pa-chat-back'));await page.waitForFunction(()=>!document.querySelector('.pa-shell.pa-selected'))}
     await row.scrollIntoViewIfNeeded();const originTop=await page.locator('.pa-roster-scroll').evaluate(el=>el.scrollTop)
     await click(row)
     const room=page.locator('.pa-room-layout');await room.waitFor({state:'visible'})
     assert.equal(await page.locator('.pa-shell').evaluate(el=>el.classList.contains('pa-selected')),false,'opening the group retained the prior Bot selection')
     const originalRoom=await room.elementHandle()
     await room.locator('[data-room-seq="6"]').waitFor()
     const bounds=await room.boundingBox();assert.ok(bounds.x>=0&&bounds.y>=0&&bounds.width>=width-2&&bounds.y+bounds.height<=height+2,'Room remains trapped in roster')
     const text=room.locator('textarea'),quote=mode!=='read'&&mode!=='old'
     if(mode==='old')assert.equal(await room.locator('.pa-room-reply').count(),0,'API87 source exposed API88 quote actions')
     if(mode==='read')assert.equal(await room.locator('.pa-room-reply').first().isDisabled(),true)
     if(quote){await room.locator('[data-room-seq="6"] .pa-room-reply').click();await room.locator('.pa-room-draft-quote').waitFor()}
     if(mode!=='read')await text.fill('Retained room draft for '+name)
     else assert.equal(await text.isDisabled(),true)
     await room.locator('.pa-chat-scroll').evaluate(el=>{el.scrollTop=Math.floor((el.scrollHeight-el.clientHeight)/3)})
     await room.getByRole('button',{name:language==='en'?'Members (2)':'成員 (2)',exact:true}).click()
     const member=room.locator('.pa-room-members .pa-room-member').filter({has:page.getByText('Bob',{exact:true})}).getByRole('button',{name:'Bob',exact:true})
     await member.waitFor({state:'visible'})
     const saved=await position(room),memberElement=await member.elementHandle(),draft=await text.inputValue(),quoteText=quote?await room.locator('.pa-room-draft-quote').innerText():null
     await click(member)
     await page.locator('.pa-main .pa-chat-header h2').filter({hasText:'Bob'}).waitFor()
     assert.equal(await originalRoom.evaluate(el=>el.isConnected&&el.hidden),true,'opening a member unmounted the room')
     const selection=await page.evaluate(()=>window.navigationFixture.selections.at(-1));assert.deepEqual(selection,{machine_id:'machine',bot_id:'bob'})
     const back=page.locator('.pa-main .pa-chat-back');await back.waitFor({state:'visible'})
     assert.equal(await back.getAttribute('aria-label'),language==='en'?'Back to group':'返回群組')
     await page.screenshot({path:path.join(artifacts,name+'-member-chat.png')});await click(back)
     await room.waitFor({state:'visible'});await page.evaluate(()=>new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve))))
     assert.equal(await originalRoom.evaluate(el=>el.isConnected&&!el.hidden),true)
     assert.equal(await memberElement.evaluate(el=>document.activeElement===el),true,'returning to the room did not restore the original member control')
     assert.equal(await text.inputValue(),draft);if(quote)assert.equal(await room.locator('.pa-room-draft-quote').innerText(),quoteText)
     const returned=await position(room);assert.equal(returned.seq,saved.seq);assert.ok(Math.abs(returned.offset-saved.offset)<=1,JSON.stringify({saved,returned}))
     await page.screenshot({path:path.join(artifacts,name+'-returned-room.png')})
     await click(room.getByRole('button',{name:language==='en'?'Close information':'關閉資訊',exact:true}))
     await click(room.getByRole('button',{name:language==='en'?'Back':'返回',exact:true}))
     await room.waitFor({state:'detached'});await page.waitForFunction(()=>document.activeElement?.matches('[data-group-key]'))
     assert.equal(await row.evaluate(el=>document.activeElement===el),true)
     assert.equal(await page.locator('.pa-roster-scroll').evaluate(el=>el.scrollTop),originTop)
     assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false)
     assert.equal(traffic.slice(start).filter(record=>record.word==='project-assistants.send'||record.word==='project-assistants.group-send'||/\/messages$/.test(record.path)).length,0,'navigation sent a prompt')
     assert.deepEqual(errors,[])
     evidence.push({engine,mode,language,width,height,baseline:baseline??null,originalBot:'stranger',memberKey:selection,roomBounds:bounds,saved,returned,draftPreserved:true,quotePreserved:quote,roomRemainedMounted:true,focusRestored:true,rosterTop:originTop,clicks:{openGroup:1,openMembers:1,openMemberChat:1,returnRoom:1,closeMembers:1,returnList:1},frontendPrompts:0,sourceAPI:mode==='old'?87:88,legacyRoomAllowed:false,outerCloudEncryptedCarrierSimulated:true})
     await page.screenshot({path:path.join(artifacts,name+'-returned-list.png')})
    }catch(error){await page.screenshot({path:path.join(artifacts,name+'-failure.png')});await writeFile(path.join(artifacts,name+'-failure.json'),JSON.stringify({error:String(error),focus:await page.evaluate(()=>document.activeElement?.outerHTML),rooms:await page.locator('.pa-room-layout').evaluateAll(rooms=>rooms.map(room=>({hidden:room.hidden,connected:room.isConnected}))),text:await page.locator('body').innerText(),errors},null,2));throw error}
    finally{await context.close()}
   }
  }finally{await browser.close()}
 }
 await writeFile(path.join(artifacts,'navigation-evidence.json'),JSON.stringify({evidence,externalModelCalls:0},null,2)+'\n')
 console.log(JSON.stringify({cases:evidence.length,externalModelCalls:0,evidence}))
}finally{await writeFile(path.join(artifacts,'traffic.json'),JSON.stringify(traffic,null,2)+'\n');server.closeAllConnections();await new Promise(resolve=>server.close(resolve))}
