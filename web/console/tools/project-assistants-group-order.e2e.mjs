/** Member order through actual GroupEditor, GroupRoom and stored room selection. */
import assert from 'node:assert/strict'
import {createServer} from 'node:http'
import {readFile,mkdir,writeFile} from 'node:fs/promises'
import {execFileSync} from 'node:child_process'
import {fileURLToPath,pathToFileURL} from 'node:url'
import path from 'node:path'
import {build} from 'esbuild'
const {BOTS_ORIGIN:upstream,BOTS_CLOUD_ORIGIN:cloud,BOTS_ORDER_STATE:stateURL,BOTS_ARTIFACTS:artifacts,PLAYWRIGHT_MODULE:module}=process.env
assert.ok(upstream&&cloud&&stateURL&&artifacts&&module)
await mkdir(artifacts,{recursive:true})
const root=fileURLToPath(new URL('..',import.meta.url)),baseline=process.env.BOTS_ORDER_BASELINE
const bundle=await build({entryPoints:[path.join(root,'tools/fixtures/project-assistants-group-order.tsx')],bundle:true,write:false,outfile:'order.js',format:'esm',platform:'browser',target:'es2022',jsx:'automatic',define:{'process.env.NODE_ENV':'"development"'},plugins:baseline?[{name:'order-baseline',setup(plugin){plugin.onLoad({filter:/project-assistants\/Groups\.tsx$/},args=>({contents:execFileSync('git',['show',baseline+':'+path.relative(path.resolve(root,'../..'),args.path)],{cwd:path.resolve(root,'../..'),encoding:'utf8'}),loader:'tsx',resolveDir:path.dirname(args.path)}))}}]:[]})
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
  res.setHeader('Content-Type','text/html');res.end('<!doctype html><html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><style>html,body,#app{height:100%;margin:0}</style><link rel="stylesheet" href="/order.css"><div id="app"></div><script type="module" src="/order.js"></script></html>')
 }catch(error){res.writeHead(500);res.end(JSON.stringify({error:String(error)}))}
})
await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve))
const origin=`http://127.0.0.1:${server.address().port}`,pw=await import(pathToFileURL(module).href),evidence=[]
const idKeys=group=>group.members.map(member=>member.key.bot_id)
const readState=async()=>{const response=await fetch(stateURL);assert.equal(response.status,200);return response.json()}
const writes=start=>traffic.slice(start).filter(r=>r.word==='project-assistants.group-edit'||r.method==='PATCH'&&r.path.includes('/rooms/groups/'))
try{
 for(const engine of (process.env.BOTS_ENGINES??'chromium,webkit').split(',')){
  const browser=await pw[engine].launch({headless:true,...(engine==='chromium'?{args:['--disable-features=OmniboxEverywhere']}:{})})
  try{
   for(const [mode,language,width,height] of [['local','en',1440,900],['local','zh-Hant',390,720],['cloud','en',1440,900],['cloud','zh-Hant',390,720]]){
    const context=await browser.newContext({viewport:{width,height},hasTouch:width===390}),page=await context.newPage(),start=traffic.length,errors=[],name=[engine,mode,language].join('-')
    page.on('pageerror',error=>errors.push(String(error)))
    const click=locator=>width===390?locator.tap():locator.click(),row=page.locator('[data-group-key]'),room=page.locator('.pa-room-layout'),dialog=page.getByRole('dialog',{name:language==='en'?'Edit group':'編輯群組',exact:true}),editLabel=language==='en'?'Edit group':'編輯群組',saveLabel=language==='en'?'Save':'儲存',cancelLabel=language==='en'?'Cancel':'取消'
    const openPanel=async()=>{await click(row);await room.waitFor({state:'visible'});await click(room.locator('.pa-room-identity'))}
    const open=async()=>{await openPanel();await click(room.getByRole('button',{name:editLabel,exact:true}));await dialog.waitFor()}
    const back=async()=>{await click(room.getByRole('button',{name:language==='en'?'Close information':'關閉資訊',exact:true}));await click(room.getByRole('button',{name:language==='en'?'Back':'返回',exact:true}));await room.waitFor({state:'detached'});await row.waitFor({state:'visible'})}
    const avatars=()=>row.locator('.pa-group-avatars .pa-avatar').evaluateAll(nodes=>nodes.map(svg=>svg.querySelector('linearGradient stop:nth-child(2)')?.getAttribute('stop-color')))
    const verify=async(expected,label)=>{
     const state=await readState();assert.deepEqual(idKeys(state.group),expected,label+' changed saved member order');assert.deepEqual(state.first_round.map(key=>key.bot_id),expected,label+' changed first-round @all selection');assert.equal(state.history_lines,0)
     const names=await room.locator('.pa-room-member strong').allTextContents();assert.deepEqual(names,expected.map(id=>id[0].toUpperCase()+id.slice(1)),label+' changed displayed member order')
     for(const id of ['bob','alice'])assert.equal(state.group.members.find(member=>member.key.bot_id===id).joined_revision,1)
     await back();assert.deepEqual(await avatars(),expected.map(id=>({bob:'#b39af2',alice:'#e39567',charlie:'#75c6a5'})[id]),label+' changed rendered avatar order')
     return state
    }
    try{
     await page.goto(origin+'/?'+new URLSearchParams({mode,lang:language}));await row.waitFor();await open()
     const candidates=await dialog.locator('.pa-group-choice-name').allTextContents()
     assert.deepEqual(candidates,['Alice','Bob','Charlie']);assert.notDeepEqual(candidates.filter(name=>name!=='Charlie').map(name=>name.toLowerCase()),['bob','alice'],'fixture did not exercise different candidate order')
     await dialog.getByRole('textbox').fill('Renamed '+name)
     // Removing and reselecting a retained member is not an order request.
     await click(dialog.getByRole('checkbox',{name:'Bob',exact:true}));await click(dialog.getByRole('checkbox',{name:'Bob',exact:true}))
     await click(dialog.getByRole('button',{name:saveLabel,exact:true}));await dialog.waitFor({state:'detached'})
     const rename=await verify(['bob','alice'],'name-only')
     await openPanel()
     const add=room.locator('.pa-room-add-member'),addDialog=page.locator('dialog.pa-group-editor')
     await click(add)
     await addDialog.waitFor()
     assert.equal(await addDialog.getByRole('textbox').count(),0)
     const saveMembers=addDialog.getByRole('button',{name:saveLabel,exact:true})
     assert.equal(await saveMembers.isDisabled(),true)
     for(const name of ['Alice','Bob']){
      const current=addDialog.getByRole('checkbox',{name,exact:true})
      assert.equal(await current.isChecked(),true)
      assert.equal(await current.isDisabled(),true)
     }
     await click(addDialog.getByRole('checkbox',{name:'Charlie',exact:true}))
     await page.screenshot({path:path.join(artifacts,name+'-add-members.png')})
     await click(saveMembers);await addDialog.waitFor({state:'detached'})
     const added=await verify(['bob','alice','charlie'],'add member');assert.equal(added.group.members.at(-1).joined_revision,rename.group.membership_revision+1)
     assert.equal(added.group.name,rename.group.name)
     await open();assert.equal(await dialog.getByRole('checkbox',{name:'Charlie',exact:true}).isChecked(),true);await click(dialog.getByRole('checkbox',{name:'Charlie',exact:true}));await dialog.getByRole('textbox').fill('Review room');await click(dialog.getByRole('button',{name:saveLabel,exact:true}));await dialog.waitFor({state:'detached'})
     const removed=await verify(['bob','alice'],'remove new member'),beforeCancel=writes(start).length
     await open();await dialog.getByRole('textbox').fill('Unsaved order draft');await click(dialog.getByRole('checkbox',{name:'Charlie',exact:true}));await click(dialog.getByRole('button',{name:cancelLabel,exact:true}));await dialog.waitFor({state:'detached'});await back()
     await open();assert.equal(await dialog.getByRole('textbox').inputValue(),'Review room');assert.equal(await dialog.getByRole('checkbox',{name:'Charlie',exact:true}).isChecked(),false);await click(dialog.getByRole('button',{name:cancelLabel,exact:true}));await dialog.waitFor({state:'detached'});await back()
     assert.equal(writes(start).length,beforeCancel,'Cancel wrote group data');assert.equal(beforeCancel,3);assert.deepEqual((await readState()).group,removed.group)
     assert.equal(traffic.slice(start).filter(r=>r.word==='project-assistants.group-send'||r.path.endsWith('/messages')).length,0)
     assert.deepEqual(errors,[]);await page.screenshot({path:path.join(artifacts,name+'-preserved-order.png')})
     evidence.push({engine,mode,language,width,height,candidateOrder:candidates,renamedOrder:idKeys(rename.group),addedOrder:idKeys(added.group),removedOrder:idKeys(removed.group),writeCount:3,cancelWrites:0,avatarOrderVerified:true,firstRoundOrderVerified:true,externalModelCalls:0})
    }catch(error){await page.screenshot({path:path.join(artifacts,name+'-failure.png')});await writeFile(path.join(artifacts,name+'-failure.json'),JSON.stringify({error:String(error),state:await readState(),text:await page.locator('body').innerText(),traffic:traffic.slice(start),errors},null,2));throw error}
    finally{await context.close()}
   }
  }finally{await browser.close()}
 }
 await writeFile(path.join(artifacts,'group-order-evidence.json'),JSON.stringify({evidence,externalModelCalls:0},null,2)+'\n');console.log(JSON.stringify({cases:evidence.length,evidence,externalModelCalls:0}))
}finally{await writeFile(path.join(artifacts,'traffic.json'),JSON.stringify(traffic,null,2)+'\n');server.closeAllConnections();await new Promise(resolve=>server.close(resolve))}
