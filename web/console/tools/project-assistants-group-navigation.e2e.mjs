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
const settle=locator=>locator.evaluate(async el=>{
 await new Promise(resolve=>requestAnimationFrame(resolve))
 await Promise.allSettled(el.getAnimations().map(animation=>animation.finished))
})

async function openRoomActions(page,row) {
 await row.focus()
 await row.press("Shift+F10")
 const menu=page.locator(".pa-message-menu:not([data-closing])")
 await menu.waitFor()
 return menu
}

async function verifyRoomReactions({page,room,row,context,mode,engine,width,language,click,name,roomName}) {
 const zh=language!=="en", reply=zh?"回覆":"Reply"
 const seq=await row.getAttribute("data-room-seq")
 const picker=menu=>menu.locator(".pa-reaction-palette button").first()
 const chip=currentRoom=>currentRoom.locator(`[data-room-seq="${seq}"] .pa-reaction-own`)
 if(mode==="old"||mode==="read"){
  const menu=await openRoomActions(page,row)
  assert.equal(await picker(menu).count(),0)
  assert.equal(await menu.getByRole("menuitem",{name:reply,exact:true}).count(),0)
  await page.keyboard.press("Escape")
  return {writes:false}
 }
 if(width===390&&engine==="chromium"){
  const bubble=row.locator(".pa-room-bubble")
  await bubble.scrollIntoViewIfNeeded()
  const cdp=await context.newCDPSession(page)
  const touch=(type,x,y)=>cdp.send("Input.dispatchTouchEvent",{type,touchPoints:type==="touchEnd"?[]:[{x,y}]})
  let box=await bubble.boundingBox(),x=Math.floor(box.x+box.width/2),y=Math.floor(box.y+box.height/2)
  const scroll=room.locator(".pa-chat-scroll")
  const top=await scroll.evaluate(el=>el.scrollTop)
  const delta=top>80?78:-78
  await touch("touchStart",x,y)
  for(const step of [0.25,0.5,0.75,1])await touch("touchMove",x,y+delta*step)
  await touch("touchEnd")
  await page.waitForFunction(before=>Math.abs(document.querySelector(".pa-room-layout .pa-chat-scroll").scrollTop-before)>8,top)
  await page.waitForTimeout(475)
  assert.equal(await page.locator(".pa-message-menu:not([data-closing])").count(),0,"scroll opened message actions")
  await bubble.scrollIntoViewIfNeeded()
  box=await bubble.boundingBox()
  x=Math.floor(box.x+box.width/2);y=Math.floor(box.y+box.height/2)
  assert.equal(await row.locator(".pa-message-controls").evaluate(el=>getComputedStyle(el).opacity),"0")
  await touch("touchStart",x,y)
  await page.locator(".pa-message-menu:not([data-closing])").waitFor()
  await touch("touchEnd")
  await cdp.detach()
 }else if(width>390){
  const bubble=row.locator(".pa-room-bubble")
  await bubble.hover()
  await page.waitForFunction(seq=>getComputedStyle(document.querySelector(`[data-room-seq="${seq}"] .pa-message-controls`)).opacity==="1",seq)
  await bubble.click({button:"right"})
 }else await openRoomActions(page,row)
 let menu=page.locator(".pa-message-menu:not([data-closing])")
 await menu.waitFor()
 assert.equal(await menu.locator(":scope > :first-child").getAttribute("class"),"pa-reaction-palette")
 await page.keyboard.press("Escape")
 await page.waitForFunction(seq=>document.activeElement===document.querySelector(`[data-room-seq="${seq}"] .pa-message-more`),seq)
 menu=await openRoomActions(page,row)
 const first=picker(menu),firstLabel=await first.getAttribute("aria-label")
 await page.waitForFunction(label=>document.activeElement?.getAttribute("aria-label")===label,firstLabel)
 await page.keyboard.press("ArrowRight")
 assert.equal(await page.evaluate(()=>document.activeElement.textContent),"❤️")
 await page.screenshot({path:path.join(artifacts,name+"-room-message-actions.png")})
 await click(first)
 await row.locator('.pa-reactions[data-reaction-state="confirmed"]').waitFor()
 assert.equal(await chip(room).count(),1)
 const second=await context.newPage()
 try{
  await second.goto(page.url())
  const entry=second.locator("[data-group-key]").filter({has:second.getByText(roomName,{exact:true})})
  await (width===390?entry.tap():entry.click())
  const reopened=second.locator(".pa-room-layout")
  await chip(reopened).waitFor()
  await (width===390?chip(reopened).tap():chip(reopened).click())
  await chip(reopened).waitFor({state:"detached"})
  await second.reload()
  const again=second.locator("[data-group-key]").filter({has:second.getByText(roomName,{exact:true})})
  await (width===390?again.tap():again.click())
  await reopened.locator(`[data-room-seq="${seq}"]`).waitFor()
  assert.equal(await chip(reopened).count(),0,"reaction returned after removal and reload")
  const onRevoke=await openRoomActions(second,reopened.locator(`[data-room-seq="${seq}"]`))
  const revokedPicker=picker(onRevoke)
  await revokedPicker.waitFor()
  const calls=traffic.length
  await revokedPicker.evaluate(button=>{
   window.navigationFixture.revoke()
   button.click()
  })
  assert.equal(traffic.slice(calls).filter(r=>r.word==="project-assistants.group-react"||r.path.endsWith("/reactions")).length,0)
 }finally{await second.close()}
 // Refresh the original tab to observe the other tab's confirmed removal.
 await page.evaluate(()=>window.dispatchEvent(new Event("focus")))
 await chip(room).waitFor({state:"detached"})
 return {savedAndRemoved:true,revoked:false,keyboard:true,touchHold:width===390&&engine==="chromium"}
}

async function verifyInspectorMotion(page,trigger,panel,close,kind){
 const originalTrigger=await trigger.elementHandle()
 const snapshot=async(open,reduce)=>originalTrigger.evaluate(async(button,{kind,open,reduce})=>{
  const layout=button.closest(kind==='group'?'.pa-room-layout':'.pa-chat-layout')
  const panel=layout.querySelector('.pa-side-inspector')
  if(open)button.click()
  else panel.querySelector('header button').click()
  await new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve)))
  const style=getComputedStyle(panel),animations=panel.getAnimations()
  return {hidden:panel.hidden,inert:panel.inert,moving:animations.some(animation=>animation.transitionProperty==='transform'),
   duration:style.transitionDuration,reduce,focus:open?panel.contains(document.activeElement):document.activeElement===button}
 },{kind,open,reduce})
 await page.emulateMedia({reducedMotion:'no-preference'})
 const enter=await snapshot(true,false)
 assert.deepEqual([enter.hidden,enter.inert,enter.moving,enter.focus],[false,false,true,true])
 await settle(panel)
 const exit=await snapshot(false,false)
 assert.deepEqual([exit.hidden,exit.inert,exit.moving,exit.focus],[true,true,true,true])
 // Reopen before the exit edge. The same inspector and its draft stay mounted.
 const element=await panel.elementHandle()
 await originalTrigger.evaluate(button=>button.click())
 await settle(panel)
 assert.equal(await element.evaluate(el=>!el.hidden&&!el.inert),true)
 await close.press('Escape')
 await panel.waitFor({state:'hidden'})
 await page.emulateMedia({reducedMotion:'reduce'})
 const instantEnter=await snapshot(true,true),instantExit=await snapshot(false,true)
 assert.deepEqual([instantEnter.moving,instantExit.moving,instantEnter.focus,instantExit.focus],[false,false,true,true])
 assert.equal(instantEnter.duration,'0s')
 assert.equal(instantExit.duration,'0s')
 await page.emulateMedia({reducedMotion:'no-preference'})
 return {enter,exit,reversal:true,reducedMotion:true}
}

async function verifyCreationMenu(page,click,language,mode,name) {
 const words=language==='en'
  ?{add:'Add',bot:'Create Bot',group:'New group',cancel:'Cancel'}
  :{add:'新增',bot:'新增 Bot',group:'新增群組',cancel:'取消'}
 const roster=page.locator('.pa-roster'),trigger=roster.getByRole('button',{name:words.add,exact:true})
 const legacyEntries=roster.getByRole('button',{name:words.bot,exact:true}).or(roster.getByRole('button',{name:words.group,exact:true}))
 if(mode==='read'){
  assert.equal(await trigger.count(),0,'a read-only source exposed the Add menu')
  assert.equal(await legacyEntries.count(),0,'a read-only source exposed a create action')
  return {choices:[]}
 }
 assert.equal(await trigger.count(),1,'the roster must have one Add button')
 assert.equal(await trigger.getAttribute('aria-haspopup'),'menu')
 assert.equal(await legacyEntries.count(),0,'a standalone create button remained in the roster')
 const menu=page.getByRole('menu',{name:words.add,exact:true})
 const bot=menu.getByRole('menuitem',{name:words.bot,exact:true}),group=menu.getByRole('menuitem',{name:words.group,exact:true})
 const focused=locator=>locator.evaluate(el=>document.activeElement===el)
 await click(trigger)
 await menu.waitFor()
 assert.deepEqual(await menu.getByRole('menuitem').allTextContents(),[words.bot,words.group])
 assert.equal(await focused(bot),true,'opening the menu did not focus its first action')
 assert.equal(await menu.locator('button > svg[aria-hidden=true]').count(),2)
 await settle(menu)
 const anchor=await trigger.boundingBox(),box=await menu.boundingBox(),viewport=page.viewportSize()
 assert.ok(Math.abs(anchor.x+anchor.width-box.x-box.width)<=1&&box.y>=anchor.y+anchor.height&&box.x>=0&&box.y+box.height<=viewport.height,'the menu is outside the viewport or detached from Add')
 await page.screenshot({path:path.join(artifacts,name+'-creation-menu.png')})
 await page.keyboard.press('Escape')
 await menu.waitFor({state:'detached'})
 assert.equal(await focused(trigger),true,'Escape did not return focus to Add')
 for(const [action,title] of [[bot,words.bot],[group,words.group]]){
  if(viewport.width===390){
   await click(trigger)
   await click(action)
  }else{
   await trigger.press('ArrowDown')
   await menu.waitFor()
   if(action===group)await page.keyboard.press('ArrowDown')
   assert.equal(await focused(action),true)
   await page.keyboard.press('Enter')
  }
  const form=page.getByRole('dialog',{name:title,exact:true})
  await form.waitFor()
  await form.getByRole('textbox').first().fill('Unsaved fixture name')
  await click(form.getByRole('button',{name:words.cancel,exact:true}))
  await form.waitFor({state:'detached'})
  assert.equal(await focused(trigger),true,`${title} cancellation lost its Add trigger`)
  assert.equal(await trigger.getAttribute('aria-expanded'),'false')
 }
 return {choices:[words.bot,words.group],icons:2,anchor,box,focusRestored:true}
}

async function verifyDetailsChevron(page,click,language,name) {
 const info=page.getByRole('button',{name:language==='en'?'Stranger · Bot information':'Stranger · Bot 資訊',exact:true})
 const identity=await info.elementHandle()
 const panel=page.locator('.pa-main .pa-info-panel')
 const close=panel.getByRole('button',{name:language==='en'?'Close information':'關閉資訊',exact:true})
 const direction=await info.evaluate(button=>{
  const arrow=button.querySelector('svg[aria-hidden="true"].pa-chat-identity-chevron path')
  if(!arrow)return null
  const length=arrow.getTotalLength(),start=arrow.getPointAtLength(0),tip=arrow.getPointAtLength(length/2),end=arrow.getPointAtLength(length)
  return tip.x>start.x&&tip.x>end.x&&start.y<tip.y&&tip.y<end.y
 })
 assert.equal(direction,true,'the Bot information chevron must point right')
 await page.screenshot({path:path.join(artifacts,name+'-details-chevron.png')})
 if(page.viewportSize().width===390)await click(info.getByText('Stranger',{exact:true}))
 else await info.press('Enter')
 await panel.waitFor({state:'visible'})
 assert.equal(await identity.getAttribute('aria-expanded'),'true')
 await settle(panel)
 await page.screenshot({path:path.join(artifacts,name+'-bot-details.png')})
 await click(close)
 await panel.waitFor({state:'hidden'})
 assert.equal(await info.getAttribute('aria-expanded'),'false')
 return {right:true,opensDetails:true,motion:await verifyInspectorMotion(page,info,panel,close,'bot')}
}

async function verifyGroupCreate(page,click,language,name,start){
 const zh=language!=='en',trigger=page.locator('.pa-roster').getByRole('button',{name:zh?'新增':'Add',exact:true})
 const title=zh?'新增群組':'New group',menu=page.getByRole('menu',{name:zh?'新增':'Add',exact:true})
 await click(trigger)
 await click(menu.getByRole('menuitem',{name:title,exact:true}))
 const form=page.getByRole('dialog',{name:title,exact:true}),submit=form.getByRole('button',{name:zh?'建立':'Create',exact:true})
 const alice=form.getByRole('checkbox',{name:'Alice',exact:true}),bob=form.getByRole('checkbox',{name:'Bob',exact:true}),field=form.getByRole('textbox')
 await form.waitFor()
 const motion=await form.evaluate(el=>getComputedStyle(el).transitionDuration)
 assert.equal(motion,'0.3s, 0.3s')
 await settle(form)
 assert.equal(await submit.isDisabled(),true)
 const project=form.getByRole('button',{name:/^Project/})
 await click(project)
 await click(form.getByRole('option',{name:'other',exact:true}))
 assert.equal(await alice.count(),0)
 assert.equal(await form.getByRole('checkbox',{name:'Stranger',exact:true}).count(),1)
 await click(project)
 await click(form.getByRole('option',{name:'project',exact:true}))
 assert.equal(await form.getByRole('checkbox',{name:'Stranger',exact:true}).count(),0)
 await click(alice)
 await bob.press('Space')
 assert.equal(await alice.isChecked(),true)
 assert.equal(await bob.isChecked(),true)
 assert.equal(await form.locator('.pa-group-form-identity .pa-avatar').count(),2)
 assert.equal(await form.locator('.pa-group-choice-check:visible').count(),2)
 const multiName=await field.getAttribute('placeholder')
 assert.match(multiName,/^Alice.*Bob$/)
 await page.screenshot({path:path.join(artifacts,name+'-group-preview.png')})
 await click(zh?alice:bob)
 const defaultName=await field.getAttribute('placeholder')
 assert.equal(defaultName,zh?'Bob':'Alice')
 const savedName=zh?'Synthetic review circle':defaultName
 if(zh)await field.fill(savedName)
 await page.screenshot({path:path.join(artifacts,name+'-group-create.png')})
 await click(submit)
 await form.waitFor({state:'detached'})
 await page.locator('.pa-room-layout .pa-room-identity').getByText(savedName,{exact:true}).waitFor()
 const writes=traffic.slice(start).filter(r=>r.word==='project-assistants.group-create'||r.method==='POST'&&r.path==='/v1/project-assistants/rooms/groups')
 assert.equal(writes.length,1)
 assert.equal(writes[0].body.settings.name,savedName)
 assert.deepEqual(writes[0].body.settings.members.map(key=>key.bot_id),[zh?'bob':'alice'])
 return {multiName,defaultName,savedName,projectFiltered:true,writes:1}
}

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
     const controlErrors=[]
     const creation=baseline?null:await verifyCreationMenu(page,click,language,mode,name).catch(error=>controlErrors.push(error.message))
     assert.equal(traffic.slice(start).filter(record=>record.word==='project-assistants.group-history'||/\/rooms\/groups\/[^/?]+\?/.test(record.path)).length,0,'list preview fetched room histories')
     assert.equal(await row.locator('.pa-avatar').count(),2)
     assert.match(await row.locator('.pa-row-preview').innerText(),/Bob:/)
     await click(bot('Stranger'))
     await page.locator('.pa-main .pa-chat-header h2').filter({hasText:'Stranger'}).waitFor()
     const details=baseline?null:await verifyDetailsChevron(page,click,language,name).catch(error=>controlErrors.push(error.message))
     assert.deepEqual(controlErrors,[])
     const writes=traffic.slice(start).filter(r=>/^project-assistants\.(create|group-create)$/.test(r.word??'')||r.method==='POST'&&/^\/v1\/project-assistants(?:\/rooms\/groups)?$/.test(r.path))
     assert.equal(writes.length,0,'a cancelled form issued a create request')
     if(width>390)assert.equal(await page.locator('.pa-main .pa-chat-back').isVisible(),false,'ordinary desktop chat gained a group return action')
     if(width===390){await click(page.locator('.pa-main .pa-chat-back'));await page.waitForFunction(()=>!document.querySelector('.pa-shell.pa-selected'))}
     await row.scrollIntoViewIfNeeded();const originTop=await page.locator('.pa-roster-scroll').evaluate(el=>el.scrollTop)
     await click(row)
     const room=page.locator('.pa-room-layout');await room.waitFor({state:'visible'})
     assert.equal(await page.locator('.pa-shell').evaluate(el=>el.classList.contains('pa-selected')),false,'opening the group retained the prior Bot selection')
     const originalRoom=await room.elementHandle()
     await room.locator('[data-room-seq="6"]').waitFor()
     const {bounds,shell,roster,rowHit}=await room.evaluate(el=>{
      const box=node=>node.getBoundingClientRect().toJSON(),shell=el.parentElement
      const roster=shell.querySelector('.pa-roster'),row=roster.querySelector('[data-group-key][aria-current=true]'),r=box(row)
      return {bounds:box(el),shell:box(shell),roster:box(roster),rowHit:row.contains(document.elementFromPoint(r.left+r.width/2,r.top+r.height/2))}
     })
     assert.ok(bounds.y>=0&&bounds.bottom<=height+2&&Math.abs(bounds.right-shell.right)<=1)
     assert.ok(Math.abs(bounds.left-(width>700?roster.right:shell.left))<=1)
     assert.equal(rowHit,width>700,'the roster must receive desktop pointer hits and remain behind the mobile room')
     const text=room.locator('textarea'),quote=mode!=='read'&&mode!=='old'
     const memberToggle=baseline?room.getByRole('button',{name:language==='en'?'Members (2)':'成員 (2)',exact:true}):room.locator('.pa-room-identity')
     const membersPanel=room.locator('.pa-room-members'),closeMembers=membersPanel.getByRole('button',{name:language==='en'?'Close information':'關閉資訊',exact:true})
     const message=room.locator('[data-room-seq="6"]')
     const messageActions=baseline?null:await verifyRoomReactions({page,room,row:message,context,mode,engine,width,language,click,name,roomName:"Review room"})
     if(quote){
      if(baseline)await room.locator('[data-room-seq="6"] .pa-room-reply').click()
      else {
       const menu=await openRoomActions(page,message)
       await click(menu.getByRole("menuitem",{name:language==="en"?"Reply":"回覆",exact:true}))
      }
      await room.locator('.pa-room-draft-quote').waitFor()
     }
     if(mode!=='read')await text.fill('Retained room draft for '+name)
     else assert.equal(await text.isDisabled(),true)
     await room.locator('.pa-chat-scroll').evaluate(el=>{el.scrollTop=Math.floor((el.scrollHeight-el.clientHeight)/3)})
     const inspector=baseline?null:await verifyInspectorMotion(page,memberToggle,membersPanel,closeMembers,'group')
     await click(memberToggle)
     await settle(membersPanel)
     if(!baseline){
      assert.equal(await memberToggle.locator('.pa-avatar').count(),2)
      assert.equal(await membersPanel.locator('header .pa-avatar').count(),2)
      await page.screenshot({path:path.join(artifacts,name+'-group-members.png')})
     }
     if(mode!=='read'){
      const edit=room.getByRole('button',{name:language==='en'?'Edit group':'編輯群組',exact:true})
      await click(edit)
      const editor=room.getByRole('dialog',{name:language==='en'?'Edit group':'編輯群組',exact:true})
      await editor.waitFor();await settle(editor);const cancel=editor.getByRole('button',{name:language==='en'?'Cancel':'取消',exact:true}),box=await cancel.boundingBox()
      const point=width===390?{x:32,y:42}:{x:box.x+box.width/2,y:box.y+box.height/2}
      const hit=await cancel.evaluate((button,point)=>{const target=document.elementFromPoint(point.x,point.y);return {cancelOwnsHit:!!target&&button.contains(target),targetTag:target?.tagName,targetClass:target?.className,stack:document.elementsFromPoint(point.x,point.y).slice(0,8).map(el=>({tag:el.tagName,class:el.className,label:el.getAttribute('aria-label')})),cancelBox:button.getBoundingClientRect().toJSON()}},point)
      if(width===390)await page.touchscreen.tap(point.x,point.y);else await page.mouse.click(point.x,point.y)
      if(hit.cancelOwnsHit)await editor.waitFor({state:'detached'})
      const closed=await editor.count()===0
      let recovery
      if(!closed&&process.env.BOTS_EDITOR_RECOVERY==='1'){
       await page.screenshot({path:path.join(artifacts,name+'-editor-covered.png')})
       const closePanel=room.getByRole('button',{name:language==='en'?'Close information':'關閉資訊',exact:true})
       const closeHit=await closePanel.evaluate(button=>button.contains(document.elementFromPoint(350,42)))
       assert.equal(closeHit,true,'the existing panel close point is not safe')
       await page.touchscreen.tap(350,42)
       const exposed=await cancel.evaluate(button=>button.contains(document.elementFromPoint(32,42)))
       assert.equal(exposed,true,'the actual Cancel is still covered after closing the panel')
       await page.touchscreen.tap(32,42)
       recovery={closed:await editor.count()===0,groupWrites:traffic.slice(start).filter(r=>/^project-assistants\.group-(create|edit|delete|send|stop)$/.test(r.word??'')||r.method!=='GET'&&/\/rooms\/groups/.test(r.path)&&!r.path.endsWith('/reactions')).length}
       assert.equal(recovery.closed,true);assert.equal(recovery.groupWrites,0)
      }
      await writeFile(path.join(artifacts,name+'-editor-cancel.json'),JSON.stringify({point,hit,closed,recovery},null,2)+'\n')
      assert.equal(closed,true,'a real Cancel-coordinate tap did not close the group editor')
      assert.equal(hit.cancelOwnsHit,true,'the visible Cancel control is covered')
      assert.equal(await edit.evaluate(button=>document.activeElement===button),true,'Cancel did not restore the edit trigger')
      const exits=[]
      for(const method of ['role','keyboard','escape']){
       await click(edit);await editor.waitFor();await settle(editor)
       assert.equal(await editor.getByRole('textbox').inputValue(),'Review room','Cancel saved an earlier rename')
       assert.equal(await editor.locator('input[type="checkbox"]:checked').count(),2)
       assert.equal(await cancel.evaluate(button=>document.activeElement===button),true,'the editor did not receive keyboard focus')
       const dimensions=await editor.boundingBox();assert.ok(dimensions.x>=0&&dimensions.y>=0&&dimensions.x+dimensions.width<=width&&dimensions.y+dimensions.height<=height)
       if(method==='role'){
        // Native Tab policies differ: WebKit may return to the name field,
        // while Chromium reports body at its browser focus boundary. Neither
        // may focus an application control behind the active modal.
        for(const key of ['Shift+Tab','Tab']){
         await page.keyboard.press(key)
         assert.equal(await editor.evaluate(el=>document.activeElement===document.body||el.contains(document.activeElement)),true,'Tab focused a control behind the group form')
        }
        await cancel.focus()
        await room.getByRole('button',{name:language==='en'?'Close information':'關閉資訊',exact:true}).evaluate(button=>button.focus())
        assert.equal(await cancel.evaluate(button=>document.activeElement===button),true,'the member panel behind the modal still accepted focus')
        await editor.getByRole('textbox').fill('Unsaved group rename')
        await page.screenshot({path:path.join(artifacts,name+'-editor-visible.png')})
        await click(cancel)
       }else await page.keyboard.press(method==='keyboard'?'Enter':'Escape')
       await editor.waitFor({state:'detached'})
       assert.equal(await edit.evaluate(button=>document.activeElement===button),true)
       assert.equal(await room.locator('.pa-room-members').isVisible(),true,'Cancel closed the parent member panel')
       exits.push(method)
      }
      await click(room.getByRole('button',{name:language==='en'?'Close information':'關閉資訊',exact:true}))
      const more=room.getByRole('button',{name:language==='en'?'More actions':'更多動作',exact:true})
      await click(more);await room.getByRole('menuitem',{name:language==='en'?'Edit group':'編輯群組',exact:true}).click()
      await editor.waitFor();await page.keyboard.press('Escape');await editor.waitFor({state:'detached'})
      assert.equal(await more.evaluate(button=>document.activeElement===button),true,'header edit did not return to its menu trigger')
      await click(memberToggle)
      await settle(membersPanel)
      const groupWrites=traffic.slice(start).filter(r=>/^project-assistants\.group-(create|edit|delete|send|stop)$/.test(r.word??'')||r.method!=='GET'&&/\/rooms\/groups/.test(r.path)&&!r.path.endsWith('/reactions'))
      assert.equal(groupWrites.length,0,'leaving the group form mutated group data')
      await writeFile(path.join(artifacts,name+'-editor-exits.json'),JSON.stringify({coordinate:point,exits,headerEscape:true,groupWrites:0,returnsToMemberPanel:true,focusRestored:true},null,2)+'\n')
     }else assert.equal(await room.getByRole('button',{name:language==='en'?'Edit group':'編輯群組',exact:true}).isDisabled(),true)
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
     assert.equal(traffic.slice(start).filter(r=>/^project-assistants\.group-(create|edit|delete|send|stop)$/.test(r.word??'')||r.method!=='GET'&&/\/rooms\/groups/.test(r.path)&&!r.path.endsWith('/reactions')).length,0,'a cancelled form wrote group data')
     assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false)
     assert.equal(traffic.slice(start).filter(record=>record.word==='project-assistants.send'||record.word==='project-assistants.group-send'||/\/messages$/.test(record.path)).length,0,'navigation sent a prompt')
     assert.deepEqual(errors,[])
     await page.screenshot({path:path.join(artifacts,name+'-returned-list.png')})
     const created=baseline||mode!=='local'?null:await verifyGroupCreate(page,click,language,name,traffic.length)
     evidence.push({messageActions,engine,mode,language,width,height,baseline:baseline??null,creation,details,inspector,created,createWrites:writes.length,originalBot:'stranger',memberKey:selection,roomBounds:bounds,saved,returned,draftPreserved:true,quotePreserved:quote,roomRemainedMounted:true,focusRestored:true,rosterTop:originTop,frontendPrompts:0,sourceAPI:mode==='old'?87:91,legacyRoomAllowed:false,outerCloudEncryptedCarrierSimulated:true})
    }catch(error){await page.screenshot({path:path.join(artifacts,name+'-failure.png')});await writeFile(path.join(artifacts,name+'-failure.json'),JSON.stringify({error:String(error),focus:await page.evaluate(()=>document.activeElement?.outerHTML),rooms:await page.locator('.pa-room-layout').evaluateAll(rooms=>rooms.map(room=>({hidden:room.hidden,connected:room.isConnected}))),text:await page.locator('body').innerText(),errors},null,2));throw error}
    finally{await context.close()}
   }
  }finally{await browser.close()}
 }
 await writeFile(path.join(artifacts,'navigation-evidence.json'),JSON.stringify({evidence,externalModelCalls:0},null,2)+'\n')
 console.log(JSON.stringify({cases:evidence.length,externalModelCalls:0,evidence}))
}finally{await writeFile(path.join(artifacts,'traffic.json'),JSON.stringify(traffic,null,2)+'\n');server.closeAllConnections();await new Promise(resolve=>server.close(resolve))}
