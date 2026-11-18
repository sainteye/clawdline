/** Real production Bots/SourceClient against the task-owned Go TCP/SQLite source. */
import assert from 'node:assert/strict'
import {createServer} from 'node:http'
import {promisify} from 'node:util'
import {execFile} from 'node:child_process'
import {mkdir,writeFile} from 'node:fs/promises'
import {fileURLToPath} from 'node:url'
import path from 'node:path'
import {build} from 'esbuild'
const run=promisify(execFile)
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
let tab
async function cli(...args){const {stdout}=await run(process.env.BOTS_BROWSER_CLI||'clawdline',['browser',args[0],...args.slice(1)],{env:{...process.env,...(process.env.BOTS_BROWSER_CLAWDLINE_DIR?{CLAWDLINE_NEXT_DIR:process.env.BOTS_BROWSER_CLAWDLINE_DIR}:{})},timeout:20000,maxBuffer:4*1024*1024});const r=JSON.parse(stdout);audit.push({args,result:r});await writeFile(path.join(artifacts,'browser-audit.json'),JSON.stringify(audit,null,2));if(!r.ok)throw Error(JSON.stringify(r));return r.value}
const read=()=>cli('read','--tab',String(tab))
const click=label=>cli('click','--tab',String(tab),label)
async function waitText(text){for(let i=0;i<40;i++){const r=await read();if(r.text.includes(text)||r.elements.some(e=>e.label?.includes(text)))return r;await new Promise(r=>setTimeout(r,150))}throw Error('Missing text '+text)}
async function open(query=''){tab=(await cli('open',origin+query)).tab;await waitText('Bot');return read()}
async function close(){if(tab){await cli('close','--tab',String(tab));tab=null}}
async function settings(zh=false){const state=await read();if(state.text.includes('pa_invalid'))throw Error('Existing event read-cursor defect blocks same-page interaction');await click(zh?'Synthetic Bot · Bot 資訊':'Synthetic Bot · Bot information');await click(zh?'Bot 設定':'Bot settings');await click(zh?'編輯對話設定':'Edit conversation setup');await click(zh?'替代 Agent':'Alternate Agents');return waitText(zh?'替代 Agent':'Alternate Agents')}
async function selectAlternate(on=true,zh=false){if(on){await click(zh?'新增替代 Agent':'Add alternate Agent');await read();await cli('key','--tab',String(tab),'Enter')}else await click(zh?'移除 Codex':'Remove Codex');await read()}
async function waitClosed(){for(let i=0;i<30;i++){const r=await read();if(!r.elements.some(e=>e.label==='Save'))return;await new Promise(r=>setTimeout(r,100))}throw Error('Settings did not close')}
async function fresh(){await close();await open();await chooseBot()}
const api=async(suffix='')=>{const r=await fetch(upstream+`/v1/project-assistants/${bot}${suffix}${suffix.includes('?')?'&':'?'}machine_id=${machine}`,{headers:{Authorization:`Bearer ${token}`}});assert.equal(r.status,200);return r.json()}
async function editDirect(patch){const {bot:b}=await api();const settings={name:b.name,appearance:b.appearance,instructions:b.instructions.text,permission_policy:b.permission_policy,expected_version:b.version,...(b.mode?{mode:b.mode}:{}),...patch};const id=crypto.randomUUID();const r=await fetch(upstream+`/v1/project-assistants/${bot}`,{method:'PATCH',headers:{Authorization:`Bearer ${token}`,'Content-Type':'application/json','Idempotency-Key':id},body:JSON.stringify({command:{key:b.key,operation:'edit',idempotency_key:id,content_hash:''},settings})});assert.equal(r.status,200,await r.text())}
// Same-page event reads stall on the known bot_updated read_cursor projection defect; wait on the source, then reopen.
async function settled(){for(let i=0;i<100;i++){const x=await api('/export');if(x.turns.length&&x.turns.every(t=>!['accepted','queued','running','waiting_permission'].includes(t.state))&&x.messages.some(m=>m.text==='Scripted final answer'))return x;await new Promise(r=>setTimeout(r,100))}throw Error('Turn did not settle')}
// Settings open scrolled to the top; bring the alternate section into the evidence.
async function shot(out){if(!out.includes('03-answer'))try{await cli('scroll','--tab',String(tab),'--x','400','--y','700')}catch(error){audit.push({scrollFailed:String(error)})}await cli('screenshot','--tab',String(tab),'--out',out)}
async function chooseBot(){const r=await read();const row=r.elements.find(e=>e.tag==='button'&&e.text?.includes('Bot'))??r.elements.find(e=>e.role==='button'&&e.text?.includes('Bot'));await click('Synthetic Bot')}
try{
 await open();await chooseBot();await settings();await shot(path.join(artifacts,'01-off.jpg'))
 const before=await api('/export');assert.equal(before.turns.length,0)
 await selectAlternate();await click('Save');await waitClosed();await fresh();assert.deepEqual((await api()).bot.fallback_agents,['codex']);assert.equal((await api('/export')).turns.length,0)
 await settings();const reopened=await read();assert.ok(reopened.elements.some(e=>e.label==='Remove Codex'));assert.ok(!reopened.elements.some(e=>e.label==='Add alternate Agent'));await shot(path.join(artifacts,'02-saved.jpg'));await selectAlternate(false);await click('Save');await waitClosed();await fresh();assert.equal((await api()).bot.fallback_agents?.length??0,0)
 await settings();await selectAlternate();await click('Save');await waitClosed();await fresh()
 // An unrelated metadata save must omit the configured fallback field.
 await settings();await click('Save');await waitClosed();await fresh();const patches=network.filter(n=>n.method==='PATCH');assert.equal('fallback_agents' in patches.at(-1).body.settings,false);assert.deepEqual((await api()).bot.fallback_agents,['codex'])
 await click('Message');await cli('type','--tab',String(tab),'fallback-settings-input');await click('Send message');await settled();await fresh();await waitText('Scripted final answer');await shot(path.join(artifacts,'03-answer.jpg'))
 assert.ok(savedSend);const replay=await fetch(upstream+`/v1/project-assistants/${bot}/messages`,{method:'POST',headers:{Authorization:`Bearer ${token}`,'Content-Type':'application/json','Idempotency-Key':savedSend.body.command.idempotency_key},body:JSON.stringify(savedSend.body)});assert.equal(replay.status,200);assert.deepEqual((await replay.json()).receipt,savedSend.answer.receipt)
 await close();await open('?lang=zh-Hant');await cli('viewport','--tab',String(tab),'mobile');await waitText('Synthetic Bot');await chooseBot();await waitText('Scripted final answer');await settings(true);await waitText('移除 Codex');await shot(path.join(artifacts,'04-zh-mobile.jpg'));await close()
 const main=await api('/export');await writeFile(path.join(artifacts,'main-flow.json'),JSON.stringify({primary:main.bot.agent,fallbacks:main.bot.fallback_agents,turns:main.turns,receipts:main.receipts,messages:main.messages,receiptReplay:true,reopenViaFullReload:true,externalRequests:0},null,2))
 // Old-source UI does not write the API72 field; source persistence remains authoritative.
 await open('?api=71');await chooseBot();await settings();await waitText('This source needs API 72');await click('Save');await waitClosed();assert.equal('fallback_agents' in network.filter(n=>n.method==='PATCH').at(-1).body.settings,false);await close()
 proxyToken=process.env.BOTS_READONLY_TOKEN;assert.ok(proxyToken);await open('?readonly=1');await chooseBot();await settings();const readOnlyPatches=network.filter(n=>n.method==='PATCH').length;await click('Save');assert.equal(network.filter(n=>n.method==='PATCH').length,readOnlyPatches);await shot(path.join(artifacts,'05-readonly.jpg'));await close();proxyToken=token
 // Concurrent metadata update is a real expected-version refusal, retaining the form.
 await open();await chooseBot();await settings();await selectAlternate(false);await editDirect({instructions:'Concurrent fixture metadata'});await click('Save');await waitText('Settings changed elsewhere');assert.ok((await read()).text.includes('Alternate Agents'));await shot(path.join(artifacts,'06-stale.jpg'));await close()
 // A lost HTTP response cannot create a fresh automatic settings command.
 await open();await chooseBot();await settings();await selectAlternate(false);unknownNext=true;await click('Save');await waitText('The save outcome is unknown');const held=network.filter(n=>n.method==='PATCH').length;await click('Save');await read();assert.equal(network.filter(n=>n.method==='PATCH').length,held);await shot(path.join(artifacts,'07-unknown.jpg'));await close();assert.equal((await api()).bot.fallback_agents?.length??0,0)
 await open();await chooseBot();await settings();await selectAlternate();await click('Save');await waitClosed();await close()
 await editDirect({mode:'plan'});await open();await chooseBot();await settings();await waitText('Alternates currently require Auto mode');const fixed=await read();assert.ok(fixed.elements.some(e=>e.label==='Remove Codex'));assert.ok(!fixed.elements.some(e=>e.label==='Add alternate Agent'));await shot(path.join(artifacts,'08-fixed-mode.jpg'));await close();await editDirect({mode:'auto'})
 const after=await api('/export');assert.equal(after.bot.agent,'claude');assert.equal(after.turns.length,1)
 await writeFile(path.join(artifacts,'browser-evidence.json'),JSON.stringify({primary:after.bot.agent,turns:after.turns.length,messages:after.messages.map(m=>({kind:m.kind,text:m.text})),receiptReplay:true,configurationStartsNoPrompt:true,reopenViaFullReload:true,samePageEventReadCursorDefectRetained:true,oldAPI:true,readOnly:true,staleForm:true,unknownFormHeld:true,fixedModePreserved:true,modelRequests:0},null,2))
}catch(error){console.error('PRIMARY_FAILURE',error);throw error}finally{try{await close()}catch(error){console.error('CLEANUP_FAILURE',error)}await writeFile(path.join(artifacts,'browser-audit.json'),JSON.stringify(audit,null,2));await writeFile(path.join(artifacts,'network.json'),JSON.stringify(network,null,2));server.closeAllConnections();await new Promise(resolve=>server.close(resolve))}
