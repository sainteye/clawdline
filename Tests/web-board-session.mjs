import assert from 'node:assert/strict';
import {pathToFileURL} from 'node:url';
const {createBoardSessionController,bindBoardSession} = await import(process.argv[2]
    ? pathToFileURL(process.argv[2]).href : '../Resources/web/app/js/input/board-session.js');
let checks=0, failures=0;
const check=(name,condition)=>{checks++;if(!condition){failures++;console.error('FAIL: '+name);}};
const locatorModule=await import('../Resources/web/app/js/net/session-links.js').catch(()=>({}));
const id='11111111-1111-4111-8111-111111111111', project={id:'p',displayPath:'/project',label:'Project'};
const stableProject='project-0123456789abcdef01234567';
const locator={machine:'mac-b',conversation:id,project:stableProject};
const stableURL=locatorModule.sessionShareURL?.(locator);
check('stable locator is hosted and never carries a terminal id or title',
    stableURL?.startsWith('https://app.clawdline.com/#session_ref=1&') && !stableURL.includes('%25'));
check('stable machine/conversation/project locator roundtrips',
    JSON.stringify(locatorModule.sessionLocatorFromHash?.(stableURL?.split('#')[1]))===JSON.stringify(locator));
for(const suffix of ['&machine=other','&title=guess','&conversation=%zz','&project=bad'])
    check('closed locator refuses '+suffix,locatorModule.sessionLocatorFromHash?.(stableURL?.split('#')[1]+suffix)==null);
check('machine-local alias is not a shareable identity',locatorModule.sessionShareURL?.({...locator,machine:'this-mac'})==null);
for(const machine of ['mac%zz','mac%FF','mac%00','mac+space','x'.repeat(201)])
    check('standalone malformed/oversized machine fails closed '+machine.slice(0,12),
        locatorModule.sessionLocatorFromHash?.('session_ref=1&machine='+machine+'&conversation='+id)==null);
check('wire byte limit rejects oversized encoded input',locatorModule.sessionLocatorFromHash?.(
    'session_ref=1&machine='+ '%61'.repeat(700)+'&conversation='+id)==null);
check('optional Project is omitted without guessing',locatorModule.sessionLocatorFromHash?.(
    'session_ref=1&machine=mac-a&conversation='+id)?.project===null);
function fixture(storage = new Map()) {
    const f={sessions:[],reads:[],resumes:[],opens:[],began:[],write:true,
        places:{places:[{id:'opaque',path:'/project'}],assistants:[{id:'claude'},{id:'codex'}]},
        histories:{claude:{sessions:[]},codex:{sessions:[{id:id.toUpperCase(),title:'Known history',live:false}]}}};
    f.env={render:()=>{},sessions:()=>f.sessions.map(row=>({assistant:'codex',...row})),canWrite:()=>f.write,
        storage: {getItem:key=>storage.get(key) ?? null,setItem:(key,value)=>storage.set(key,value)},
        places:async()=>{f.reads.push('places');return f.places;},
        history:async(p,a)=>{f.reads.push([p,a]);return f.histories[a];},
        resume:async(...args)=>{f.resumes.push(args);return f.response ? f.response() : {id:'%2',attach:'observed attach'};},
        openLive:id=>f.opens.push(id),began:(...args)=>{f.began.push(args);if(f.onBegan)f.onBegan();}};
    f.c=createBoardSessionController(f.env);
    return f;
}
{
    const f=fixture(); f.env.inventoryReady=()=>false;
    f.c=createBoardSessionController(f.env);
    await f.c.openLocator?.(locator);
    check('cold stable locator waits for inventory without starting history or resume',
        f.c.state.status==='waiting_inventory'&&!f.reads.length&&!f.resumes.length);
    f.sessions=[{id:'%wrong',sessionId:id,machine:'mac-a',title:'Same title'},
        {id:'%right',sessionId:id,machine:'mac-b',title:'Same title'}];
    f.env.inventoryReady=()=>true; f.c.observe();
    await new Promise(r=>setImmediate(r));
    check('observed inventory resolves exact machine and conversation, not title',f.opens[0]==='%right'&&!f.resumes.length);
}
{
    const f=fixture();f.sessions=[{id:'%already',sessionId:id,machine:'mac-b'}];
    f.env.inventoryReady=()=>true;f.c=createBoardSessionController(f.env);
    await f.c.openLocator(locator);
    check('already live locator opens exactly once without recursive observation',f.opens.length===1&&f.opens[0]==='%already');
}
{
    const f=fixture();f.env.inventoryReady=()=>true;
    f.env.project=async(p,m)=>{f.reads.push(['project',p,m]);return {...project,id:p};};
    f.places.places=[{id:'opaque',path:'/project',machine:'mac-b'}];
    f.c=createBoardSessionController(f.env);
    await f.c.openLocator?.(locator);
    check('closed stable link reads exact project and offers explicit resume only',
        f.c.state.status==='ready'&&!f.resumes.length&&JSON.stringify(f.reads[0])===JSON.stringify(['project',stableProject,'mac-b']));
}
{
    const f=fixture();let ready=false;
    f.env.inventoryReady=()=>ready;f.c=createBoardSessionController(f.env);
    await f.c.openLocator({...locator,project:null});
    ready=true;f.sessions=[{id:'%unrelated',sessionId:id,machine:'mac-a'}];f.c.observe();
    await new Promise(r=>setImmediate(r));
    check('unrelated first inventory keeps a visible honest unresolved result',f.c.state.error==='session_not_observed'&&!f.opens.length);
    f.sessions.push({id:'%target',sessionId:id,machine:'mac-b'});f.c.observe();
    check('target arriving later resolves original stable intent without reads or resume',f.opens[0]==='%target'&&!f.reads.length&&!f.resumes.length);
}
{
    const f=fixture();f.env.inventoryReady=()=>true;
    f.env.project=async()=>{f.reads.push('project');throw {code:'offline'};};f.c=createBoardSessionController(f.env);
    await f.c.openLocator(locator);
    f.sessions=[{id:'%one',sessionId:id,machine:'mac-b'},{id:'%two',sessionId:id,machine:'mac-b'}];f.c.observe();
    check('late ambiguous targets stay refused',!f.opens.length&&f.c.state.error==='session_ambiguous');
    f.sessions.pop();f.c.observe();
    check('later unique target resolves project-read failure without retrying history',f.opens[0]==='%one'&&f.reads.length===1&&!f.resumes.length);
    await f.c.openLocator({...locator,conversation:'22222222-2222-4222-8222-222222222222'});f.c.close();
    f.sessions=[{id:'%cancelled',sessionId:'22222222-2222-4222-8222-222222222222',machine:'mac-b'}];f.c.observe();
    check('closing unresolved locator cancels later arrival intent',f.opens.length===1&&f.c.state.status==='closed');
}
{
    const f=fixture();f.env.inventoryReady=()=>true;f.c=createBoardSessionController(f.env);
    await f.c.openLocator?.({...locator,project:null});
    check('missing historical project is honest and does not scan every project',
        f.c.state.error==='session_not_observed'&&!f.reads.length&&!f.resumes.length);
    await f.c.openLocator?.({...locator,machine:'this-mac'});
    check('unresolved local alias cannot select a cloud or local Session',f.c.state.error==='session_link_invalid'&&!f.opens.length);
}
{
    const f=fixture();let finish;
    f.env.inventoryReady=()=>true;
    f.env.project=()=>new Promise(resolve=>finish=resolve);
    f.c=createBoardSessionController(f.env);
    const waiting=f.c.openLocator(locator);f.c.close();finish({...project,id:stableProject});await waiting;
    check('leaving a locator fences late project response before history',f.c.state.status==='closed'&&!f.reads.length&&!f.resumes.length);
}
{
    const f=fixture();f.sessions=[{id:'%1',sessionId:id.toUpperCase()}];await f.c.open(id,project);
    check('same UUID case opens one live Session without history or resume',f.opens[0]==='%1'&&!f.reads.length&&!f.resumes.length);
    f.sessions.push({id:'%2',sessionId:id});await f.c.open(id,project);
    check('ambiguous live identity never opens or resumes',f.c.state.error==='session_ambiguous'&&f.opens.length===1&&!f.resumes.length);
}
{
    const f=fixture();await f.c.open(id,project);
    check('historical open only reads two provider catalogs',f.reads.length===3&&!f.resumes.length&&f.c.state.status==='ready');
    check('exact provider and opaque Project preserved',f.c.state.candidate.assistant==='codex'&&f.c.state.candidate.place.id==='opaque');
    f.write=false;await f.c.resume();check('write OFF refuses resume',!f.resumes.length);
    f.write=true;await f.c.resume();
    check('explicit resume uses observed catalog identity',JSON.stringify(f.resumes[0].slice(0,3))==='["opaque","'+id+'","codex"]');
    check('accepted answer reaches shared arriving-Session path',f.began[0][0].id==='%2'&&f.began[0][0].attach==='observed attach');
}
{
    const f=fixture();f.places.places.push({id:'other-mac',path:'/project'});await f.c.open(id,project);
    check('same path on two Macs fails before history',f.c.state.error==='project_ambiguous'&&f.reads.length===1);
}
{
    const f=fixture();f.places.places=[
        {id:'mac-a-place',path:'/project',machine:'mac-a'},
        {id:'mac-b-place',path:'/project',machine:'mac-b'}
    ];
    f.sessions=[{id:'%wrong',sessionId:id,machine:'mac-a'}];
    await f.c.open(id,project,'mac-b');
    check('an explicit Board machine ignores a same-conversation live Session on another Mac',
        !f.opens.length&&f.c.state.status==='ready'&&f.c.state.candidate.place.id==='mac-b-place');
    await f.c.resume();
    check('explicit resume preserves the selected Mac place identity',f.resumes[0][0]==='mac-b-place');
}
{
    const f=fixture();f.histories.claude.sessions=[{id,title:'Duplicate'}];await f.c.open(id,project);
    check('provider ambiguity is not a guess',f.c.state.error==='session_ambiguous');
}
{
    const f=fixture();f.histories.codex={sessions:[],more:true};await f.c.open(id,project);
    check('bounded history omission is not called deletion',f.c.state.error==='history_incomplete');
}
{
    const f=fixture();f.histories.codex.sessions[0].live=true;await f.c.open(id,project);await f.c.resume();
    check('catalog live observation prevents second process',f.c.state.status==='live_unobserved'&&!f.resumes.length);
}
{
    const f=fixture();await f.c.open(id,project);f.sessions=[{id:'%fresh',sessionId:id}];await f.c.resume();
    check('new live Session between prepare and confirm opens instead',f.opens[0]==='%fresh'&&!f.resumes.length);
}
{
    const f=fixture();f.response=()=>Promise.reject({code:'offline'});await f.c.open(id,project);await f.c.resume();
    check('lost action reply stays unknown',f.c.state.status==='uncertain'&&f.resumes.length===1);
    f.c.close();await f.c.open(id,project);await f.c.resume();
    check('reopening cannot blindly resubmit unknown action',f.c.state.status==='uncertain'&&f.resumes.length===1);
}
{
    const f=fixture();let finish;f.response=()=>new Promise(r=>{finish=r;});await f.c.open(id,project);
    const pending=f.c.resume();await f.c.resume();f.c.close();
    check('double press and close do not repeat mutation',f.resumes.length===1&&f.c.state.status==='resuming');
    finish({id:'%new'});await pending;
}
{
    const f=fixture();let finish;
    f.histories.codex=new Promise(r=>{finish=r;});
    const pending=f.c.open(id,project);
    await new Promise(r=>setImmediate(r));
    f.c.close();
    const next='22222222-2222-4222-8222-222222222222';
    await f.c.open(next,{id:'next',displayPath:'/next'});
    finish({sessions:[{id,title:'Old history'}]});await pending;
    check('superseded history cannot fill another selected conversation',f.c.state.conversation===next&&f.c.state.candidate===null&&f.c.state.error==='read_pending');
    check('overlapping history selection does not create a second read',f.reads.length===3);
}
{
    const f=fixture();let finish;
    f.histories.codex=new Promise(r=>{finish=r;});
    const pending=f.c.open(id,project);await new Promise(r=>setImmediate(r));
    const next='22222222-2222-4222-8222-222222222222';
    f.sessions=[{id:'%live',sessionId:next}];await f.c.open(next,project);
    finish({sessions:[{id,title:'Old history'}]});await pending;
    check('a live destination remains available during a slow history read',f.opens[0]==='%live'&&f.c.state.status==='closed');
}
{
    const f=fixture();f.onBegan=()=>{throw {code:'not_found'};};
    await f.c.open(id,project);await f.c.resume();f.c.close();await f.c.open(id,project);await f.c.resume();
    check('accepted resume with failed UI handoff cannot be mistaken for server refusal or resubmitted',f.resumes.length===1&&f.c.state.status==='uncertain');
}
{
    const f=fixture();await f.c.open(id,project);await f.c.resume();
    await f.c.open(id,project);await f.c.resume();
    check('accepted resume stays fenced until live inventory observes it',f.resumes.length===1&&f.c.state.status==='uncertain');
    f.sessions=[{id:'%2',sessionId:id}];await f.c.open(id,project);
    check('observed resumed Session opens without a new launch',f.opens[0]==='%2'&&f.resumes.length===1);
    f.sessions=[];await f.c.open(id,project);await f.c.resume();
    check('after observed Session ends a later explicit resume is allowed',f.resumes.length===2);
}
{
    const f=fixture();await f.c.open(id,project);await f.c.resume();
    f.sessions=[{id:'%2',sessionId:id}];if(f.c.observe)f.c.observe();
    f.sessions=[];await f.c.open(id,project);await f.c.resume();
    check('ordinary inventory observation settles resume without reopening Board',f.resumes.length===2);
}
{
    const storage=new Map(), f=fixture(storage);
    f.response=()=>Promise.reject({code:'offline'});
    await f.c.open(id,project);await f.c.resume();
    const next=fixture(storage);await next.c.open(id,project);await next.c.resume();
    check('reload keeps unknown resume fenced',next.resumes.length===0&&next.c.state.status==='uncertain');
    const request=f.resumes[0][3];
    check('resume carries a stable action UUID',typeof request==='string'&&/^[0-9a-f-]{36}$/i.test(request));
    check('pending action retains its original wire request across reload',typeof request==='string'&&[...storage.values()].join('').includes(request));
    check('persistent fence contains no transcript or attach token',![...storage.values()].join('').includes('observed attach'));
}
{
    const f=fixture();f.env.storage.setItem=()=>{throw Error('quota');};
    await f.c.open(id,project);await f.c.resume();
    check('cannot safely persist resume refuses before launch',!f.resumes.length&&f.c.state.error==='resume_storage_unavailable');
}
{
    const storage=new Map(), f=fixture(storage);
    f.response=()=>Promise.reject({code:'forbidden'});
    await f.c.open(id,project);await f.c.resume();
    const next=fixture(storage);await next.c.open(id,project);
    check('definite server refusal releases the durable fence',next.c.state.status==='ready'&&f.c.state.error==='forbidden');
}
{
    const storage=new Map(), f=fixture(storage);f.places.places[0].machine='mac-a';
    await f.c.open(id,project);await f.c.resume();
    f.sessions=[{id:'%2',sessionId:id,machine:'mac-b'}];f.c.observe();
    let next=fixture(storage);next.places.places[0].machine='mac-a';await next.c.open(id,project);
    check('a different Mac cannot clear pending resume',next.c.state.status==='uncertain');
    f.sessions=[{id:'%2',sessionId:id,machine:'mac-a',assistant:'claude'}];f.c.observe();
    await next.c.open(id,project);
    check('a different provider cannot clear pending resume',next.c.state.status==='uncertain');
    f.sessions=[{id:'%2',sessionId:id,machine:'mac-a',assistant:'codex'}];f.c.observe();
    await next.c.open(id,project);
    check('matching observed machine and provider clear persistent fence',next.c.state.status==='ready');
}
{
    const storage=new Map([['clawdline.board.resume-fences.v1','malformed']]),f=fixture(storage);
    await f.c.open(id,project);await f.c.resume();
    check('corrupt receipt storage is visible and never authorizes another launch',!f.resumes.length&&f.c.state.error==='resume_storage_unavailable');
}
class Node {
    constructor(){this.children=[];this.attrs={};this.open=false;this._text='';}
    set textContent(v){this._text=v;this.children=[];}
    get textContent(){return this._text+this.children.map(x=>x.textContent).join(' ');}
    appendChild(n){this.children.push(n);}
    setAttribute(k,v){this.attrs[k]=v;}
    addEventListener(){}
    showModal(){this.open=true;}
    close(){this.open=false;}
}
for(const [code,en,zh] of [
    ['forbidden','not allowed','權限'],['not_found','no longer recognizes','找不到'],
    ['bad_request','refused this resume','拒絕這筆'],['terminal_unsupported','terminal cannot','終端不支援']
])for(const lang of ['en','zh-Hant']){
    const f=fixture();f.response=()=>Promise.reject({code});
    const doc={body:new Node(),documentElement:{lang},createElement:()=>new Node()};
    const c=bindBoardSession(doc,f.env);await c.open(id,project);await c.resume();
    check('dialog preserves '+code+' in '+lang,doc.body.textContent.includes(lang==='en'?en:zh));
}
// Assignment UI is a proposal writer, never a process launcher or receiver identity.
const assignmentModule=await import('../Resources/web/app/js/input/board-assignment.js').catch(()=>({}));
const assignments=assignmentModule.assignmentCandidates||(()=>[]);
const makeAssignment=assignmentModule.createBoardAssignmentController||(()=>({state:{},open:async()=>{},select:()=>{},propose:async()=>{},cancel:async()=>{},retry:async()=>{},close:()=>{}}));
const assignmentTarget={projectId:stableProject,itemId:'work-1',machine:'mac-b'};
const assignmentRow={id:'%12',sessionId:id,assistant:'codex',title:'Readable worker',cwd:'/project',machine:'mac-b'};
function assignmentFixture(storage=new Map()) {
    const f={rows:[assignmentRow],write:true,ready:true,calls:[],renders:[],storage};
    f.board={schemaVersion:1,enabled:true,revision:12,readState:{status:'ready',revision:12},
        projects:[{id:stableProject,displayPath:'/project'}],item:{id:'work-1',projectId:stableProject,
        title:'A useful feature',owner:'old-owner',scopeRevision:2,state:'execution',handoff:null,sessionAssignment:null}};
    f.env={requireMachine:true,sessions:()=>f.rows,canWrite:()=>f.write,inventoryReady:()=>f.ready,
        read:async()=>({board:structuredClone(f.board)}),render:s=>f.renders.push(s.status),
        storage:{get length(){return storage.size;},key:i=>[...storage.keys()][i]??null,
            getItem:k=>storage.get(k)||null,setItem:(k,v)=>storage.set(k,v),removeItem:k=>storage.delete(k)},
        command:async(body,machine)=>{f.calls.push({body:structuredClone(body),machine});
            if(f.failure)throw f.failure;
            f.board.revision++;f.board.readState.revision=f.board.revision;
            f.board.item.handoff=body.operation==='assign_session'?{id:'proposal-1',provider:body.provider,
                proposedOwner:body.sessionId,fromOwner:'old-owner',scopeRevision:2,status:'pending',note:body.note}:null;
            return {board:structuredClone(f.board)};}};
    f.c=makeAssignment(f.env);return f;
}
check('assignment module exposes a bounded proposal controller',typeof assignmentModule.createBoardAssignmentController==='function');
{
    const rows=assignments([assignmentRow,{...assignmentRow,machine:'mac-a'},
        {...assignmentRow,sessionId:'bad-id'},{...assignmentRow,sessionId:'22222222-2222-4222-8222-222222222222',cwd:'/other'}],
        {machine:'mac-b',projectPath:'/project',owner:'old-owner'});
    check('picker uses exact Mac/Project/provider/conversation and readable title',rows.length===1&&rows[0].title==='Readable worker');
    check('ambiguous same provider/conversation is not selectable',assignments([assignmentRow,{...assignmentRow,id:'%13'}],
        {machine:'mac-b',projectPath:'/project'}).length===0);
    check('existing owner is not offered as a new receiver',assignments([assignmentRow],
        {machine:'mac-b',projectPath:'/project',owner:id}).length===0);
}
{
    const f=assignmentFixture();await f.c.open(assignmentTarget);
    check('opening picker reads but does not mutate',f.calls.length===0&&f.c.state.status==='ready');
    f.c.select(f.c.state.candidates?.[0]?.key);await f.c.propose('Please review the next step');
    check('deliberate proposal pins selected machine and exact Board revision',f.calls.length===1&&f.calls[0].machine==='mac-b'
        &&f.calls[0].body.operation==='assign_session'&&f.calls[0].body.expectedRevision===12
        &&f.calls[0].body.projectId===stableProject&&f.calls[0].body.sessionId===id);
    check('proposal remains pending and does not transfer owner',f.c.state.item?.handoff?.id==='proposal-1'
        &&f.c.state.item?.owner==='old-owner'&&f.calls.every(x=>!['decide_session_assignment','handoff','accept_handoff'].includes(x.body.operation)));
    await f.c.cancel('Withdraw proposal');
    check('cancel names exact pending proposal',f.calls.length===2&&f.calls[1].body.operation==='cancel_session_assignment'
        &&f.calls[1].body.assignmentId==='proposal-1');
}
for(const variant of ['stale','disabled','legacy','wrong-item','wrong-project','write-off','inventory-late']) {
    const f=assignmentFixture();
    if(variant==='stale')f.board.readState.status='stale';
    if(variant==='disabled')f.board.enabled=false;
    if(variant==='legacy')delete f.board.item.sessionAssignment;
    if(variant==='wrong-item')f.board.item.id='other';
    if(variant==='wrong-project')f.board.item.projectId='other';
    if(variant==='write-off')f.write=false;
    if(variant==='inventory-late')f.ready=false;
    await f.c.open(assignmentTarget);f.c.select(f.c.state.candidates?.[0]?.key);await f.c.propose('intent');
    check('picker fails closed for '+variant,!f.calls.length&&f.c.state.status!=='ready');
}
{
    const f=assignmentFixture();await f.c.open(assignmentTarget);f.c.select(f.c.state.candidates[0].key);
    let release;f.env.command=async(body,machine)=>{f.calls.push({body,machine});await new Promise(r=>release=r);
        return {board:{...f.board,revision:13,readState:{status:'ready',revision:13}}};};
    const first=f.c.propose('two\nlines');await f.c.propose('second');
    check('multiline note is allowed but double click sends once',f.calls.length===1&&f.calls[0].body.note==='two\nlines');
    f.c.close();release();await first;
    check('late command does not reopen closed sheet',f.c.state.status==='closed'&&f.storage.size===0);
}
{
    const f=assignmentFixture();await f.c.open(assignmentTarget);f.c.select(f.c.state.candidates[0].key);
    f.env.command=async()=>({board:{...f.board,revision:13,item:{...f.board.item,id:'foreign'}}});
    await f.c.propose('intent');
    check('wrong-item command receipt remains uncertain',f.c.state.status==='uncertain'&&f.storage.size===1);
}
{
    const f=assignmentFixture();await f.c.open(assignmentTarget);f.c.select(f.c.state.candidates[0].key);f.write=false;
    await f.c.propose('intent');check('write revocation before click sends nothing',!f.calls.length);
    check('conflicting row machine identities are excluded',assignments([{...assignmentRow,identity:{machine:'mac-a'}}],
        {machine:'mac-b',projectPath:'/project'}).length===0);
}
{
    const f=assignmentFixture();f.storage.set('clawdline.board.assignment.v1','not-json');await f.c.open(assignmentTarget);
    await f.c.retry();check('corrupt journal refuses without clearing evidence or sending',!f.calls.length&&f.storage.size===1&&f.c.state.status==='error');
}
{
    const f=assignmentFixture();await f.c.open(assignmentTarget);f.c.select(f.c.state.candidates?.[0]?.key);
    f.rows=[];await f.c.propose('intent');
    check('inventory change before click refuses a vanished target',!f.calls.length&&f.c.state.error==='session_unavailable');
}
{
    const storage=new Map(),f=assignmentFixture(storage);f.failure={code:'offline'};
    await f.c.open(assignmentTarget);f.c.select(f.c.state.candidates?.[0]?.key);await f.c.propose('intent');
    check('uncertain delivery retains a request before sending',f.calls.length===1&&storage.size>0&&f.c.state.status==='uncertain');
    const next=assignmentFixture(storage);await next.c.open(assignmentTarget);
    check('reload does not automatically resend',next.calls.length===0&&next.c.state.status==='uncertain');
    await next.c.retry();
    check('explicit retry reuses exact original body and request id',next.calls.length===1
        &&JSON.stringify(next.calls[0])===JSON.stringify(f.calls[0])&&storage.size===0);
}
{
    const f=assignmentFixture();await f.c.open(assignmentTarget);f.c.select(f.c.state.candidates?.[0]?.key);
    f.env.storage.setItem=()=>{throw Error('quota');};await f.c.propose('intent');
    check('storage failure refuses before any proposal',!f.calls.length&&f.c.state.error==='storage_unavailable');
}
{
    const f=assignmentFixture();f.failure={status:409,code:'revision_conflict'};
    await f.c.open(assignmentTarget);f.c.select(f.c.state.candidates?.[0]?.key);await f.c.propose('intent');
    check('definite CAS refusal clears retry intent without automatic resend',f.calls.length===1&&f.storage.size===0&&f.c.state.error==='revision_conflict');
}
{
    const f=assignmentFixture();let finish;f.env.read=()=>new Promise(r=>finish=r);
    const reading=f.c.open(assignmentTarget);f.c.close();finish?.({board:f.board});await reading;
    check('close fences a late read and never reopens the sheet',f.c.state.status==='closed'&&!f.calls.length);
}
{
    class Element {
        constructor(tag){this.tagName=tag;this.children=[];this.listeners={};this.dataset={};this._text='';}
        set textContent(t){this._text=t;this.children=[];} get textContent(){return this._text+this.children.map(x=>x.textContent).join('');}
        appendChild(n){this.children.push(n);return n;} setAttribute(){} focus(){}
        addEventListener(k,f){this.listeners[k]=f;} showModal(){this.open=true;} close(){this.open=false;}
        all(){return [this,...this.children.flatMap(n=>n.all())];}
        click(){if(!this.disabled)this.listeners.click?.({target:this});}
    }
    const doc={documentElement:{lang:'zh-Hant'},body:new Element('body'),createElement:t=>new Element(t)};
    const f=assignmentFixture();
    const bound=assignmentModule.bindBoardAssignment?.(doc,f.env);
    await bound?.open(assignmentTarget);
    const find=action=>doc.body.all().find(n=>n.dataset.assignmentAction===action);
    check('assignment sheet shows readable title and honest unsent boundary',doc.body.textContent.includes('Readable worker')&&doc.body.textContent.includes('不會送訊息'));
    const picker=find('receiver');if(picker){picker.value=id;picker.value=f.c.state.candidates?.[0]?.key||'codex:'+id;picker.listeners.change?.();}
    const note=find('note');if(note)note.value='明確指派';
    find('propose')?.click();await new Promise(r=>setImmediate(r));
    check('DOM confirmation creates only a pending proposal',f.calls.length===1&&doc.body.textContent.includes('待接受')
        &&doc.body.textContent.includes('此介面不會通知')&&!doc.body.textContent.includes('尚未通知'));
    find('close')?.click();check('closing the sheet does not withdraw responsibility',f.calls.length===1);
    f.board.item.handoff={id:'legacy',proposedOwner:id,note:'legacy'};await bound?.open(assignmentTarget);
    check('legacy handoff receives no invented pending or notification status',doc.body.textContent.includes('舊式交接')
        &&!doc.body.textContent.includes('待接受')&&!doc.body.textContent.includes('尚未通知'));
    doc.documentElement.lang='en';f.board.item.handoff={id:'typed',provider:'codex',status:'pending',proposedOwner:id};
    await bound?.open(assignmentTarget);
    check('English typed status scopes notification claim to picker',doc.body.textContent.includes('Awaiting acceptance')
        &&doc.body.textContent.includes('This picker does not send notifications')&&!doc.body.textContent.includes('not notified'));
}
for(const scenario of ['wrong-provider','both-providers','wrong-history-only','exact']) {
    const f=assignmentFixture();f.board.item.handoff={id:'h',status:'pending',provider:'codex',proposedOwner:id};
    const opened=[];f.env.openLive=value=>opened.push(value);f.env.history=()=>{throw Error('must not search untyped history');};
    if(scenario==='wrong-provider')f.rows=[{...assignmentRow,assistant:'claude'}];
    if(scenario==='both-providers')f.rows.push({...assignmentRow,id:'%13',assistant:'claude'});
    if(scenario==='wrong-history-only')f.rows=[];
    await f.c.open(assignmentTarget);await f.c.openReceiver?.();
    check('receiver provider stays exact for '+scenario,
        ['both-providers','exact'].includes(scenario)?opened.length===1&&opened[0]==='%12'
            :!opened.length&&f.c.state.error==='receiver_unavailable');
}
for(const lateResult of ['success','refusal']) {
    const shared=new Map(),a=assignmentFixture(shared);let resolve;
    a.env.command=async()=>{await new Promise(r=>resolve=r);if(lateResult==='refusal')throw {status:409,code:'revision_conflict'};
        return {board:{...a.board,revision:13,readState:{status:'ready',revision:13}}};};
    await a.c.open(assignmentTarget);a.c.select(a.c.state.candidates[0].key);const late=a.c.propose('first');
    const b=assignmentFixture(shared);await b.c.open(assignmentTarget);await b.c.retry();
    b.failure={code:'offline'};await b.c.cancel('new intent');
    const newer=JSON.stringify([...shared]);resolve();await late;
    check('late '+lateResult+' preserves newer same-scope exact intent',JSON.stringify([...shared])===newer&&shared.size>0);
    const reloaded=assignmentFixture(shared);await reloaded.c.open(assignmentTarget);await reloaded.c.retry();
    check('newer same-scope intent remains retryable after '+lateResult,reloaded.calls.length===1
        &&JSON.stringify(reloaded.calls[0])===JSON.stringify(b.calls[1]));
}
{
    const shared=new Map(),a=assignmentFixture(shared);let release;
    a.env.command=async()=>{await new Promise(r=>release=r);return {board:{...a.board,revision:13,readState:{status:'ready',revision:13}}};};
    await a.c.open(assignmentTarget);a.c.select(a.c.state.candidates[0].key);const late=a.c.propose('item one');
    const b=assignmentFixture(shared);b.board.item.id='item-two';b.failure={code:'offline'};
    await b.c.open({...assignmentTarget,itemId:'item-two'});b.c.select(b.c.state.candidates[0].key);await b.c.propose('item two');
    release();await late;
    const next=assignmentFixture(shared);next.board.item.id='item-two';await next.c.open({...assignmentTarget,itemId:'item-two'});
    await next.c.retry();check('settlement preserves unrelated scope intent without an aggregate rewrite',next.calls.length===1
        &&JSON.stringify(next.calls[0])===JSON.stringify(b.calls[0])&&shared.size===0);
}
console.log('web Board Session: '+(checks-failures)+'/'+checks+' assertions passed');
assert.equal(failures,0,'Board Session assertion failures');
