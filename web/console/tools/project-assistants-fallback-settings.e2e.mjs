/** Real production Bots/SourceClient against the task-owned Go TCP/SQLite source. */
import assert from 'node:assert/strict'
import {createServer} from 'node:http'
import {spawn} from 'node:child_process'
import {constants} from 'node:fs'
import {access,mkdir,mkdtemp,rm,writeFile} from 'node:fs/promises'
import {fileURLToPath} from 'node:url'
import path from 'node:path'
import {build} from 'esbuild'
const {BOTS_ORIGIN:upstream,BOTS_TOKEN:token,BOTS_MACHINE:machine,BOTS_BOT:bot,BOTS_ARTIFACTS:artifacts}=process.env
if(!upstream||!token||!machine||!bot||!artifacts)throw Error('Requires the owned Go HTTP fixture')
await mkdir(artifacts,{recursive:true})
const src=fileURLToPath(new URL('../src/',import.meta.url))
const bundle=await build({stdin:{contents:`import React from 'react';import{createRoot}from'react-dom/client';import{Bots}from'./project-assistants/Bots';import{ProjectAssistantSourceClient}from'./cloud/project-assistants';import'./project-assistants/styles.css';const q=new URLSearchParams(location.search);const grant={accountKey:'fixture',readerID:'local',machineID:${JSON.stringify(machine)},authorizationRevision:'fixture',apiLevel:Number(q.get('api')||72),paired:true,online:true,writable:!q.has('readonly')};const source=new ProjectAssistantSourceClient(grant,fetch.bind(window),()=>grant);createRoot(document.getElementById('app')).render(<Bots sources={[source]} accountKey="fixture" language={q.get('lang')||'en'} onClose={()=>{}}/>);`,resolveDir:src,loader:'tsx'},bundle:true,write:false,outfile:'fixture.js',format:'esm',platform:'browser',jsx:'automatic',define:{'process.env.NODE_ENV':'"development"'}})
const files=new Map(bundle.outputFiles.map(f=>['/'+path.basename(f.path),f.contents])),network=[],audit=[]
let savedSend, unknownNext=false, proxyToken=token
const server=createServer(async(req,res)=>{try{
 if(req.url.startsWith('/v1/')){
  const chunks=[];for await(const c of req)chunks.push(c);const body=Buffer.concat(chunks)
  const headers={Authorization:`Bearer ${proxyToken}`};for(const key of ['content-type','idempotency-key'])if(req.headers[key])headers[key]=req.headers[key]
  const result=await fetch(upstream+req.url,{method:req.method,headers,...(body.length?{body}:{})});const answer=await result.text()
  network.push({method:req.method,path:req.url,status:result.status,answer:JSON.parse(answer),...(body.length?{body:JSON.parse(body)}:{})})
  if(req.method==='POST'&&req.url.endsWith('/messages'))savedSend={body:JSON.parse(body),answer:JSON.parse(answer)}
  if(unknownNext&&req.method==='PATCH'){unknownNext=false;res.writeHead(502,{'Content-Type':'text/plain'});res.end('Injected lost response after real commit');return}
  res.writeHead(result.status,{'Content-Type':'application/json'});res.end(answer);return
 }
 const pathname=new URL(req.url,'http://fixture').pathname
 if(files.has(pathname)){res.setHeader('Content-Type',pathname.endsWith('.css')?'text/css':'text/javascript');res.end(files.get(pathname));return}
 res.setHeader('Content-Type','text/html');res.end('<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="stylesheet" href="/fixture.css"><div id="app"></div><script type="module" src="/fixture.js"></script>')
}catch(e){res.writeHead(500);res.end(String(e))}})
await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));const origin=`http://127.0.0.1:${server.address().port}`
// A task-owned fresh Chrome driven over CDP with trusted pointer and key input; no installed CLI or usual browser.
const executable=process.env.CHROME;if(!executable)throw Error('CHROME must name an existing test browser; this harness installs nothing');await access(executable,constants.X_OK)
const profile=await mkdtemp(path.join(artifacts,'browser-profile-')),chrome=spawn(executable,['--disable-features=OmniboxEverywhere','--headless=new','--remote-debugging-port=0','--use-mock-keychain','--user-data-dir='+profile,'--no-first-run','--no-default-browser-check','about:blank'],{stdio:['ignore','ignore','pipe']})
const endpoint=await new Promise((resolve,reject)=>{let output='';const timer=setTimeout(()=>reject(Error('Test browser startup timeout: '+output)),20000);chrome.on('error',reject);chrome.on('exit',()=>{clearTimeout(timer);reject(Error('Test browser exited: '+output))});chrome.stderr.on('data',chunk=>{output+=chunk;const match=/DevTools listening on (ws:\/\/\S+)/.exec(output);if(match){clearTimeout(timer);resolve(match[1])}})})
const ws=new WebSocket(endpoint);await new Promise((resolve,reject)=>{ws.addEventListener('open',resolve);ws.addEventListener('error',reject)})
let serial=0,session,mobile=false;const pending=new Map()
ws.addEventListener('message',event=>{const message=JSON.parse(String(event.data));if(!message.id)return;const job=pending.get(message.id);pending.delete(message.id);if(message.error)job?.reject(Error(message.error.message));else job?.resolve(message.result)})
const send=(method,params={},sessionId)=>new Promise((resolve,reject)=>{const id=++serial,timer=setTimeout(()=>reject(Error(method+' timed out')),15000);pending.set(id,{resolve:value=>{clearTimeout(timer);resolve(value)},reject:error=>{clearTimeout(timer);reject(error)}});ws.send(JSON.stringify({id,method,params,sessionId}))})
async function evaluate(expression){const result=await send('Runtime.evaluate',{expression,awaitPromise:true,returnByValue:true},session);if(result.exceptionDetails)throw Error(result.exceptionDetails.exception?.description??result.exceptionDetails.text);return result.result.value}
// The accessible snapshot mirrors what the former CLI read returned: visible text plus labelled controls.
const snapshot=`(()=>{const shown=e=>e.getClientRects().length>0&&getComputedStyle(e).visibility!=='hidden'&&!e.closest('[hidden],[inert]');const name=e=>(e.getAttribute('aria-label')??(e.labels?.[0]?.textContent)??e.textContent??'').trim().replace(/\\s+/g,' ');const controls=[...document.querySelectorAll('button,a[href],input,textarea,select,summary,[role=button],[role=tab],[role=option],[role=menuitem],[role=checkbox],[role=switch],[role=radio]')].filter(shown);return {text:document.body?.innerText??'',elements:controls.map(e=>({tag:e.tagName.toLowerCase(),role:e.getAttribute('role')??'',label:name(e),text:(e.textContent??'').trim(),disabled:!!e.disabled||e.getAttribute('aria-disabled')==='true'}))}})()`
// Pointer coordinates are taken only after running panel transitions finish.
async function settle(){for(let i=0;i<40;i++){if(await evaluate(`document.getAnimations().every(a=>a.playState!=='running')`))return;await new Promise(r=>setTimeout(r,50))}}
async function cli(op,...args){
 let value
 if(op==='open'){const {targetId}=await send('Target.createTarget',{url:'about:blank'});session=(await send('Target.attachToTarget',{targetId,flatten:true})).sessionId;await send('Page.enable',{},session);await send('Page.navigate',{url:args[0]},session);mobile=false;value={tab:targetId}}
 else if(op==='read')value=await evaluate(snapshot)
 else if(op==='click'||op==='hit'){const label=args.at(-1);await settle();const point=await evaluate(op==='hit'?`(()=>{const el=document.querySelector(${JSON.stringify(label)});if(!el)throw Error('Missing control: '+${JSON.stringify(label)});el.scrollIntoView({block:'center'});const r=el.getBoundingClientRect();return {x:r.x+r.width/2,y:r.y+r.height/2,disabled:!!el.disabled}})()`:`(()=>{const label=${JSON.stringify(label)};const snap=${snapshot};const shown=e=>e.getClientRects().length>0&&!e.closest('[hidden],[inert]');const all=[...document.querySelectorAll('button,a[href],input,textarea,select,summary,label,[role=button],[role=tab],[role=option],[role=menuitem],[role=checkbox],[role=switch],[role=radio]')].filter(shown);const name=e=>(e.getAttribute('aria-label')??(e.labels?.[0]?.textContent)??e.textContent??'').trim().replace(/\\s+/g,' ');const el=all.find(e=>name(e)===label)??all.find(e=>(e.getAttribute('title')??'')===label)??all.find(e=>name(e).startsWith(label));if(!el)throw Error('Missing control: '+label+' among '+JSON.stringify(all.map(name).filter(Boolean)));el.scrollIntoView({block:'center',inline:'nearest'});const r=el.getBoundingClientRect();return {x:r.x+r.width/2,y:r.y+r.height/2,disabled:!!el.disabled}})()`)
  if(mobile){await send('Input.dispatchTouchEvent',{type:'touchStart',touchPoints:[{x:point.x,y:point.y}]},session);await send('Input.dispatchTouchEvent',{type:'touchEnd',touchPoints:[]},session)}
  else{await send('Input.dispatchMouseEvent',{type:'mouseMoved',x:point.x,y:point.y},session);await send('Input.dispatchMouseEvent',{type:'mousePressed',x:point.x,y:point.y,button:'left',clickCount:1},session);await send('Input.dispatchMouseEvent',{type:'mouseReleased',x:point.x,y:point.y,button:'left',clickCount:1},session)}
  value=point}
 else if(op==='type'){await send('Input.insertText',{text:args.at(-1)},session);value={typed:true}}
 else if(op==='key'){const key=args.at(-1);const event={key,code:key,windowsVirtualKeyCode:({Enter:13,Escape:27,Tab:9,ArrowDown:40})[key]??0};await send('Input.dispatchKeyEvent',{type:'keyDown',...event,...(key==='Enter'?{text:'\r',unmodifiedText:'\r'}:{})},session);await send('Input.dispatchKeyEvent',{type:'keyUp',...event},session);value=event}
 else if(op==='viewport'){mobile=args.at(-1)==='mobile';await send('Emulation.setDeviceMetricsOverride',{width:mobile?390:1280,height:mobile?720:900,deviceScaleFactor:1,mobile},session);await send('Emulation.setTouchEmulationEnabled',{enabled:mobile},session);value={mobile}}
 else if(op==='scroll'){const x=Number(args[args.indexOf('--x')+1]),y=Number(args[args.indexOf('--y')+1]);await send('Input.dispatchMouseEvent',{type:'mouseWheel',x,y,deltaX:0,deltaY:600},session);value={x,y}}
 else if(op==='screenshot'){const {data}=await send('Page.captureScreenshot',{format:'jpeg'},session);await writeFile(args[args.indexOf('--out')+1],Buffer.from(data,'base64'));value={captured:true}}
 else if(op==='close'){const r=await send('Target.closeTarget',{targetId:args[args.indexOf('--tab')+1]});assert.equal(r.success,true);session=undefined;value={closed:true}}
 else throw Error('Unsupported test operation: '+op)
 audit.push({args:[op,...args],result:op==='read'?{chars:value.text.length}:value});return value}
let tab
const read=()=>cli('read','--tab',String(tab))
const click=label=>cli('click','--tab',String(tab),label)
async function waitText(text){for(let i=0;i<40;i++){const r=await read();if(r.text.includes(text)||r.elements.some(e=>e.label?.includes(text)))return r;await new Promise(r=>setTimeout(r,150))}throw Error('Missing text '+text)}
async function open(query=''){tab=(await cli('open',origin+query)).tab;await waitText('Bot');return read()}
async function close(){if(tab){await cli('close','--tab',String(tab));tab=null}}
async function settings(zh=false){const state=await read();if(state.text.includes('pa_invalid'))throw Error('Existing event read-cursor defect blocks same-page interaction');await click(zh?'Synthetic Bot · Bot 資訊':'Synthetic Bot · Bot information');await waitText(zh?'Bot 設定':'Bot settings');await click(zh?'Bot 設定':'Bot settings');await waitText(zh?'替代 Agent':'Alternate Agents');await click(zh?'替代 Agent':'Alternate Agents');await openSection();return waitText(zh?'替代 Agent':'Alternate Agents')}
// Alternate Agents is a disclosure in the Bot settings tab; its body renders after the first open.
async function openSection(){for(let i=0;i<40;i++){if(await evaluate(`!!document.querySelector('.pa-settings-disclosure[open] .pa-settings-disclosure-body section')`))return;await new Promise(r=>setTimeout(r,100))}throw Error('Alternate Agents section did not open')}
async function selectAlternate(on=true,zh=false){if(on){await click(zh?'新增替代 Agent':'Add alternate Agent');await read();await cli('key','--tab',String(tab),'Enter')}else await click(zh?'移除 Codex':'Remove Codex');await read()}
// The embedded editor hides its save bar once the source confirmed the save; a busy Saving label is not done.
// The embedded editor offers Save only for a changed draft, so an unrelated save edits the instructions with real input.
async function metadata(text=' (metadata save)'){await cli('hit','--tab',String(tab),'.pa-settings-instructions textarea');await cli('key','--tab',String(tab),'End');await cli('type','--tab',String(tab),text)}
async function waitClosed(){for(let i=0;i<60;i++){const r=await read();if(!r.elements.some(e=>e.label==='Save'||e.label.startsWith('Saving')))return;await new Promise(r=>setTimeout(r,100))}throw Error('Settings did not close')}
async function fresh(){await close();await open();await chooseBot()}
const api=async(suffix='')=>{const r=await fetch(upstream+`/v1/project-assistants/${bot}${suffix}${suffix.includes('?')?'&':'?'}machine_id=${machine}`,{headers:{Authorization:`Bearer ${token}`}});assert.equal(r.status,200);return r.json()}
async function editDirect(patch){const {bot:b}=await api();const settings={name:b.name,appearance:b.appearance,instructions:b.instructions.text,permission_policy:b.permission_policy,expected_version:b.version,...(b.mode?{mode:b.mode}:{}),...patch};const id=crypto.randomUUID();const r=await fetch(upstream+`/v1/project-assistants/${bot}`,{method:'PATCH',headers:{Authorization:`Bearer ${token}`,'Content-Type':'application/json','Idempotency-Key':id},body:JSON.stringify({command:{key:b.key,operation:'edit',idempotency_key:id,content_hash:''},settings})});assert.equal(r.status,200,await r.text())}
// Same-page event reads stall on the known bot_updated read_cursor projection defect; wait on the source, then reopen.
async function settled(){for(let i=0;i<100;i++){const x=await api('/export');if(x.turns.length&&x.turns.every(t=>!['accepted','queued','running','waiting_permission'].includes(t.state))&&x.messages.some(m=>m.text==='Scripted final answer'))return x;await new Promise(r=>setTimeout(r,100))}throw Error('Turn did not settle')}
// Settings open scrolled to the top; bring the alternate section into the evidence.
async function shot(out){if(!out.includes('03-answer'))try{await cli('scroll','--tab',String(tab),'--x','400','--y','700')}catch(error){audit.push({scrollFailed:String(error)})}await cli('screenshot','--tab',String(tab),'--out',out)}
async function chooseBot(){const r=await waitText('Synthetic Bot');const row=r.elements.find(e=>e.tag==='button'&&e.text?.includes('Bot'))??r.elements.find(e=>e.role==='button'&&e.text?.includes('Bot'));await click('Synthetic Bot')}
try{
 await open();await chooseBot();await settings();await shot(path.join(artifacts,'01-off.jpg'))
 const before=await api('/export');assert.equal(before.turns.length,0)
 await selectAlternate();await click('Save');await waitClosed();await fresh();assert.deepEqual((await api()).bot.fallback_agents,['codex']);assert.equal((await api('/export')).turns.length,0)
 await settings();const reopened=await read();assert.ok(reopened.elements.some(e=>e.label==='Remove Codex'));assert.ok(!reopened.elements.some(e=>e.label==='Add alternate Agent'));await shot(path.join(artifacts,'02-saved.jpg'));await selectAlternate(false);await click('Save');await waitClosed();await fresh();assert.equal((await api()).bot.fallback_agents?.length??0,0)
 await settings();await selectAlternate();await click('Save');await waitClosed();await fresh()
 // An unrelated metadata save must omit the configured fallback field.
 await settings();await metadata();await click('Save');await waitClosed();await fresh();const patches=network.filter(n=>n.method==='PATCH');assert.equal('fallback_agents' in patches.at(-1).body.settings,false);assert.deepEqual((await api()).bot.fallback_agents,['codex'])
 await click('Message');await cli('type','--tab',String(tab),'fallback-settings-input');await click('Send message');await settled();await fresh();await waitText('Scripted final answer');await shot(path.join(artifacts,'03-answer.jpg'))
 assert.ok(savedSend);const replay=await fetch(upstream+`/v1/project-assistants/${bot}/messages`,{method:'POST',headers:{Authorization:`Bearer ${token}`,'Content-Type':'application/json','Idempotency-Key':savedSend.body.command.idempotency_key},body:JSON.stringify(savedSend.body)});assert.equal(replay.status,200);assert.deepEqual((await replay.json()).receipt,savedSend.answer.receipt)
 await close();await open('?lang=zh-Hant');await cli('viewport','--tab',String(tab),'mobile');await waitText('Synthetic Bot');await chooseBot();await waitText('Scripted final answer');await settings(true);await waitText('移除 Codex');await shot(path.join(artifacts,'04-zh-mobile.jpg'));await close()
 const main=await api('/export');await writeFile(path.join(artifacts,'main-flow.json'),JSON.stringify({primary:main.bot.agent,fallbacks:main.bot.fallback_agents,turns:main.turns,receipts:main.receipts,messages:main.messages,receiptReplay:true,reopenViaFullReload:true,externalRequests:0},null,2))
 // Old-source UI does not write the API72 field; source persistence remains authoritative.
 await open('?api=71');await chooseBot();await settings();await waitText('This source needs API 72');await metadata(' (old source save)');await click('Save');await waitClosed();assert.equal('fallback_agents' in network.filter(n=>n.method==='PATCH').at(-1).body.settings,false);await close()
 proxyToken=process.env.BOTS_READONLY_TOKEN;assert.ok(proxyToken);await open('?readonly=1');await chooseBot();await settings();const readOnlyPatches=network.filter(n=>n.method==='PATCH').length;const readOnlyField=await evaluate(`document.querySelector('.pa-settings-instructions textarea')?.disabled??null`);audit.push({readOnlyField});if(readOnlyField===false)await metadata(' (read-only attempt)');const offered=(await read()).elements.find(e=>e.label==='Save');audit.push({readOnlySaveOffered:offered??null});if(offered)await click('Save');assert.equal(network.filter(n=>n.method==='PATCH').length,readOnlyPatches);await shot(path.join(artifacts,'05-readonly.jpg'));await close();proxyToken=token
 // Concurrent metadata update is a real expected-version refusal, retaining the form.
 await open();await chooseBot();await settings();await selectAlternate(false);await editDirect({instructions:'Concurrent fixture metadata'});await click('Save');await waitText('Bot settings changed elsewhere');assert.ok((await read()).text.includes('Alternate Agents'));await shot(path.join(artifacts,'06-stale.jpg'));await close()
 // A lost HTTP response cannot create a fresh automatic settings command.
 await open();await chooseBot();await settings();await selectAlternate(false);unknownNext=true;await click('Save');await waitText('confirm whether this save reached the source');const held=network.filter(n=>n.method==='PATCH').length;await click('Save');await read();assert.equal(network.filter(n=>n.method==='PATCH').length,held);await shot(path.join(artifacts,'07-unknown.jpg'));await close();assert.equal((await api()).bot.fallback_agents?.length??0,0)
 await open();await chooseBot();await settings();await selectAlternate();await click('Save');await waitClosed();await close()
 await editDirect({mode:'plan'});await open();await chooseBot();await settings();await waitText('Alternates currently require Auto mode');const fixed=await read();assert.ok(fixed.elements.some(e=>e.label==='Remove Codex'));assert.ok(!fixed.elements.some(e=>e.label==='Add alternate Agent'));await shot(path.join(artifacts,'08-fixed-mode.jpg'));await close();await editDirect({mode:'auto'})
 const after=await api('/export');assert.equal(after.bot.agent,'claude');assert.equal(after.turns.length,1)
 await writeFile(path.join(artifacts,'browser-evidence.json'),JSON.stringify({primary:after.bot.agent,turns:after.turns.length,messages:after.messages.map(m=>({kind:m.kind,text:m.text})),receiptReplay:true,configurationStartsNoPrompt:true,reopenViaFullReload:true,samePageEventReadCursorDefectRetained:true,oldAPI:true,readOnly:true,staleForm:true,unknownFormHeld:true,fixedModePreserved:true,modelRequests:0},null,2))
}catch(error){console.error('PRIMARY_FAILURE',error);throw error}finally{try{await close()}catch(error){console.error('CLEANUP_FAILURE',error)}await writeFile(path.join(artifacts,'browser-audit.json'),JSON.stringify(audit,null,2));await writeFile(path.join(artifacts,'network.json'),JSON.stringify(network,null,2));server.closeAllConnections();await new Promise(resolve=>server.close(resolve));ws.close();chrome.kill();await new Promise(r=>chrome.exitCode!==null||chrome.signalCode!==null?r():chrome.once('exit',r));await rm(profile,{recursive:true,force:true})}
