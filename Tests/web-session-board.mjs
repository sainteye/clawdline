import assert from 'node:assert/strict';
import {pathToFileURL} from 'node:url';
const {createSessionBoardController} = await import(process.argv[2]
    ? pathToFileURL(process.argv[2]).href : '../Resources/web/app/js/input/session-board.js');
let checks=0;
function check(name, condition) { assert.ok(condition,name); checks++; }
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
console.log('web Session Board: '+checks+' assertions passed');
