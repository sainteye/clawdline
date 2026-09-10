import assert from 'node:assert/strict';
import {pathToFileURL} from 'node:url';
const {createBoardSessionController,bindBoardSession} = await import(process.argv[2]
    ? pathToFileURL(process.argv[2]).href : '../Resources/web/app/js/input/board-session.js');
let checks=0, failures=0;
const check=(name,condition)=>{checks++;if(!condition){failures++;console.error('FAIL: '+name);}};
const id='11111111-1111-4111-8111-111111111111', project={id:'p',displayPath:'/project',label:'Project'};
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
console.log('web Board Session: '+(checks-failures)+'/'+checks+' assertions passed');
assert.equal(failures,0,'Board Session assertion failures');
