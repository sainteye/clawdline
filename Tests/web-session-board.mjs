import assert from 'node:assert/strict';
import fs from 'node:fs';
import {pathToFileURL} from 'node:url';
const {createSessionBoardController,bindSessionBoard,SessionBoard} = await import(process.argv[2]
    ? pathToFileURL(process.argv[2]).href : '../Resources/web/app/js/input/session-board.js');
let checks=0;
function check(name, condition) { assert.ok(condition,name); checks++; }
const html = fs.readFileSync(new URL('../Resources/web/index.html', import.meta.url), 'utf8');
const infoStart = html.indexOf('id="info-sheet"');
const infoEnd = html.indexOf('id="info-close"', infoStart);
const boardPosition = html.indexOf('id="session-board"');
check('Board relations live inside Session Info, not above the chat',
    infoStart >= 0 && boardPosition > infoStart && boardPosition < infoEnd
    && html.split('id="session-board"').length === 2);
const main = fs.readFileSync(new URL('../Resources/web/app/js/main.js', import.meta.url), 'utf8');
check('opening related work dismisses Session Info before navigating',
    /open: function \(project, item, presentation, machine\) \{ Info\.close\(\); BoardControls\.open\(project, item, presentation, machine\); \}/.test(main));
const a={id:'%1',sessionId:'11111111-1111-4111-8111-111111111111'};
const b={id:'%2',sessionId:'22222222-2222-4222-8222-222222222222'};
let calls=[], renders=[], opens=[], pending=[], visible=true;
const controller=createSessionBoardController({visible:()=>visible,render:s=>renders.push({...s}),
    read:(...args)=>{calls.push(args);return new Promise((resolve,reject)=>pending.push({resolve,reject}));},
    open:(...args)=>opens.push(args)});
const flush=async()=>{for(let i=0;i<8;i++) await new Promise(r=>setImmediate(r));};
function answer(session,rows=[],extra={}) {return {board:{enabled:true,sessionId:session.sessionId,items:rows,projects:[{id:'p',name:'Project'}],readState:{status:'ready'},...extra}};}
controller.follow(a); await controller.load();
check('OFF never reads',calls.length===0);
controller.setEnabled(true);
check('following Session does not fetch before transcript paint',calls.length===0);
visible=false; await controller.load(); check('hidden page never starts a read',calls.length===0); visible=true;
const first=controller.load(); await flush();
check('one exact conversation selector without Project guessing',calls.length===1 && calls[0][0]===null && calls[0][1]==='session:'+a.sessionId);
controller.load(); await flush(); check('repeated paint coalesces reads',calls.length===1);
controller.follow(b); controller.load(); await flush(); check('switching does not overlap physical reads',calls.length===1);
pending[0].resolve(answer(a,[{id:'wrong',projectId:'p',title:'Wrong'}])); await first; await flush();
check('old Session result is not painted into new Session',!renders.some(s=>s.session?.id===b.id && s.rows.some(r=>r.id==='wrong')));
check('latest Session receives one trailing read',calls.length===2 && calls[1][1]==='session:'+b.sessionId);
pending[1].resolve(answer(b,[{id:'right',projectId:'p',title:'Correct',sessionActivity:'related'}])); await flush();
check('matching relation displayed',controller.state.rows[0]?.id==='right');
controller.open('not-listed'); check('unknown item cannot navigate',opens.length===0);
controller.open('right'); check('item navigation carries its Project',opens.length===1 && opens[0][0]==='p' && opens[0][1]==='right');
await controller.load(); check('more transcript paints do not poll',calls.length===2);
const retry=controller.load(true); await flush(); pending[2].reject(Error('offline')); await retry;
check('failed refresh retains last relations',controller.state.status==='error' && controller.state.rows[0]?.id==='right');
const off=controller.load(true); await flush(); controller.setEnabled(false); pending[3].resolve(answer(b,[{id:'late',projectId:'p'}])); await off;
check('OFF fences late response and clears rows',controller.state.rows.length===0 && !controller.state.enabled);
controller.setEnabled(true); controller.follow({id:'%3',sessionId:'%3'}); await controller.load();
check('terminal ID is not accepted as conversation identity',controller.state.session===null && calls.length===4);
controller.follow(a); const mismatch=controller.load(); await flush(); pending[4].resolve(answer(b)); await mismatch;
check('wrong echoed conversation fails closed',controller.state.status==='error' && controller.state.rows.length===0);
const empty=controller.load(true); await flush(); pending[5].resolve(answer(a)); await empty;
check('explicit empty differs from failed read',controller.state.status==='ready' && controller.state.rows.length===0);
const remoteOff=controller.load(true); await flush();
pending[6].resolve({board:{enabled:false,items:[],mode:'standard'}}); await remoteOff;
check('authoritative OFF envelope needs no relation echo and disables navigation',!controller.state.enabled && controller.state.status==='idle' && controller.state.rows.length===0);
const upper={id:'%same',sessionId:'ABCDEFAB-1234-4123-8123-ABCDEFABCDEF'};
controller.setEnabled(true);controller.follow(upper);
const canonical=controller.load();await flush();
pending[7].resolve(answer({...upper,sessionId:upper.sessionId.toLowerCase()},[{id:'same-item',projectId:'p'}]));
await canonical;
check('uppercase provider UUID accepts the same canonical Board relationship',controller.state.status==='ready'&&controller.state.rows[0]?.id==='same-item');
check('reverse selector uses canonical UUID without mutating inventory',calls[7][1]==='session:'+upper.sessionId.toLowerCase()&&upper.sessionId.startsWith('ABCDEF'));
// Cold Cloud entry must discover the selected machine's mode, not depend on the
// one boot-time global settings read which can precede inventory publication.
{
    const reads=[], opened=[]; let response, release;
    const c=createSessionBoardController({discoverMode:true,requireMachine:true,
        visible:()=>true,render:()=>{},read:(...args)=>{reads.push(args);return response();},
        open:(...args)=>opened.push(args)});
    c.follow(null); await c.load(); check('no inventory does not guess a machine',reads.length===0);
    c.follow({...a,machine:'mac-a'});
    response=async()=>answer(a,[{id:'a',projectId:'p'}]); await c.load();
    check('late inventory discovers mode and relations without global enabled callback',c.state.enabled===true&&c.state.rows[0]?.id==='a');
    check('relation read explicitly selects the Session machine',reads[0]?.[2]==='mac-a');
    c.open('a');check('reverse navigation preserves machine',opened[0]?.[3]==='mac-a');
    response=()=>new Promise(r=>release=r);const old=c.load(true);await flush();
    c.follow({...a,machine:'mac-b'}); c.load();
    response=async()=>({board:{enabled:false}});
    release(answer(a,[{id:'late-a',projectId:'p'}]));await old;await flush();
    check('same conversation on different Mac fences old rows and discovers OFF',c.state.enabled===false&&!c.state.rows.length&&reads.at(-1)[2]==='mac-b');
    response=async()=>answer(a);await c.load(true);
    check('deliberate same-Session revalidation can leave authoritative OFF',c.state.enabled===true&&c.state.status==='ready');
    response=()=>new Promise(r=>release=r);const staleMode=c.load(true);await flush();
    c.invalidateMode?.();c.load(true);response=async()=>({board:{enabled:false}});
    release(answer(a,[{id:'before-mode-change',projectId:'p'}]));await staleMode;await flush();
    check('mode invalidation fences in-flight enabled data and observes latest OFF',c.state.enabled===false&&!c.state.rows.length);
    c.follow({...a,machine:'mac-c'});response=async()=>{throw Error('offline');};await c.load();
    check('unknown mode failure stays visible rather than pretending OFF',c.state.enabled===null&&c.state.status==='error');
    response=async()=>answer(a);await c.load(true);
    check('explicit retry recovers mode after offline',c.state.enabled===true&&c.state.status==='ready');
    c.follow({...a});await c.load();check('Cloud missing machine never uses default Mac',c.state.session===null);
}
{
    class Element {
        constructor(){this.children=[];this.hidden=false;this.events={};this.attrs={};this._text='';}
        set textContent(value){this._text=value;this.children=[];}
        get textContent(){return this._text+this.children.map(x=>x.textContent).join(' ');}
        appendChild(node){this.children.push(node);}
        setAttribute(key,value){this.attrs[key]=value;}
        addEventListener(key,fn){this.events[key]=fn;}
    }
    const doc={documentElement:{lang:'zh-Hant'},createElement:()=>new Element()};
    const container=new Element();container.ownerDocument=doc;
    let calls=0, fail=false;
    bindSessionBoard(container,{discoverMode:true,requireMachine:true,visible:()=>true,ready:()=>true,
        read:async(p,i,m)=>{calls++;if(fail)throw Error('offline');return answer(a,[{id:m,projectId:'p',title:'Work'}]);},open:()=>{}});
    SessionBoard.sync(null);await flush();check('cold inventory keeps invalid Session hidden',container.hidden&&calls===0);
    SessionBoard.sync({...a,machine:'mac-a'});await flush();
    check('inventory arrival alone reveals Info heading and reads once',!container.hidden&&container.textContent.includes('看板項目')&&calls===1);
    SessionBoard.sync({...a,machine:'mac-a'});await flush();check('unchanged inventory does not poll Board',calls===1);
    fail=true;SessionBoard.sync({...a,machine:'mac-b'});await flush();
    check('machine-only switch is observed and failure remains readable',calls===2&&!container.hidden&&container.textContent.includes('暫時無法讀取'));
    check('Info relation contents stay collapsed until a deliberate click',container.children[1].hidden===true);
    fail=false;await SessionBoard.revalidateMode?.();await flush();
    check('settings signal revalidates selected machine without adopting another machine mode',calls===3&&!container.hidden&&container.textContent.includes('Work'));
}
console.log('web Session Board: '+checks+' assertions passed');
