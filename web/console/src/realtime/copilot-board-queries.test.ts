import {test} from 'node:test'
import assert from 'node:assert/strict'
// @ts-expect-error -- Node loads TypeScript directly.
import {runCopilotTool,sessionUpdateEvent,type ToolDeps} from './copilot-tools.ts'
import type {SessionBoardTarget,SessionMessageBoard} from '../session/SessionBoards.tsx'
const rows=[{id:'%1',label:'Current lead',assistant:'codex',sessionId:'conversation-one',state:'idle'},{id:'%2',label:'Member',assistant:'codex',sessionId:'conversation-two',state:'idle'},{id:'%3',label:'Unassigned',assistant:'codex',sessionId:'conversation-three',state:'idle'}]
const board:SessionMessageBoard={id:'board-one',project:'/fixture/project',name:'Delivery board',purpose:'Review work',lead:'codex:conversation-one',summary:'',latest:0,members:[{id:'codex:conversation-one',label:'Old lead name'},{id:'codex:conversation-two',label:'Member'}]}
function fixture(){
 let boards=[board,{...board,id:'archived',name:'Archived board',archived_at:1}],reads=0
 const target:SessionBoardTarget={source:{key:'local',writable:false,read:async(fields)=>{assert.deepEqual(fields,{});reads++;return {boards} as never},change:async()=>assert.fail('read tools must not change a board')},label:'Fixture machine',candidates:[]}
 const deps:ToolDeps={fetch:async(path,init)=>{assert.ok(!init?.method||init.method==='GET');assert.equal(path,'/v1/sessions');return Response.json({sessions:rows})},sleep:async()=>{},boardTargets:()=>[target]}
 return {deps,target,set:(next:SessionMessageBoard[])=>{boards=next},reads:()=>reads}
}
const result=async(name:string,args:unknown,deps:ToolDeps)=>JSON.parse((await runCopilotTool(name,JSON.stringify(args),deps)).output)
test('voice can list active boards and resolve the live Lead name without write permission',async()=>{
 const f=fixture(),out=await result('list_session_boards',{},f.deps)
 assert.equal(out.boards.length,1);assert.equal(out.boards[0].name,'Delivery board');assert.equal(out.boards[0].lead.session_id,'%1');assert.equal(out.boards[0].lead.label,'Current lead');assert.equal(f.reads(),1)
})
test('a named-board Lead is read fresh after a transfer',async()=>{
 const f=fixture();assert.equal((await result('get_session_board',{board:'Delivery board'},f.deps)).board.lead.session_id,'%1')
 f.set([{...board,lead:'codex:conversation-two'}]);const out=await result('get_session_board',{board:'board-one'},f.deps)
 assert.equal(out.board.lead.session_id,'%2');assert.equal(f.reads(),2)
})
test('Session list entries report membership and Lead by actor identity',async()=>{
 const f=fixture(),out=await result('list_sessions',{},f.deps),all=[...out.idle,...out.working,...out.done,...out.needs_you]
 assert.deepEqual(all.find((r:any)=>r.id==='%1').message_boards,[{id:'board-one',name:'Delivery board',is_lead:true}])
 assert.deepEqual(all.find((r:any)=>r.id==='%2').message_boards,[{id:'board-one',name:'Delivery board',is_lead:false}])
 assert.deepEqual(all.find((r:any)=>r.id==='%3').message_boards,[]);assert.equal(all.find((r:any)=>r.id==='%3').message_board_status,'known')
})
test('a failed board source cannot become an unassigned Session',async()=>{
 const f=fixture();f.target.source.read=async()=>{throw Error('offline')}
 const out=await result('list_sessions',{},f.deps),row=out.idle.find((r:any)=>r.id==='%1');assert.equal(row.message_board_status,'unavailable');assert.equal(row.message_boards,undefined)
 const query=await result('get_session_board',{board:'Delivery board'},f.deps);assert.equal(query.error,'session_boards_unavailable')
})
test('both managed and direct configurations expose the read tools',()=>{
 for(const workEnabled of [false,true]){const event=sessionUpdateEvent('',{workEnabled}) as any;const names=event.session.tools.map((t:any)=>t.name);assert.ok(names.includes('list_session_boards'));assert.ok(names.includes('get_session_board'));assert.equal(new Set(names).size,names.length)}
})


test('same board names on two machines require an explicit scope and preserve actor identities',async()=>{
 const f=fixture(),other={...f.target,machine:'second',label:'Second machine',source:{...f.target.source,key:'second',read:async()=>({boards:[{...board,id:'board-two'}]} as never)}}
 f.target.machine='first';f.deps.boardTargets=()=>[f.target,other]
 f.deps.fleet=async()=>[...rows.map(row=>({machine:'first',machineName:'First machine',row})),...rows.map(row=>({machine:'second',machineName:'Second machine',row:{...row,label:'Second '+row.label}}))]
 const ambiguous=await result('get_session_board',{board:'Delivery board'},f.deps);assert.equal(ambiguous.error,'session_board_name_ambiguous');assert.equal(ambiguous.choices.length,2)
 const chosen=await result('get_session_board',{board:'Delivery board',machine:'second'},f.deps);assert.equal(chosen.board.id,'board-two');assert.equal(chosen.board.lead.session_id,'fleet:second:%251');assert.equal(chosen.board.lead.label,'Second Current lead（Second machine）')
 const listing=await result('list_sessions',{},f.deps),all=[...listing.idle,...listing.working,...listing.done,...listing.needs_you]
 assert.equal(all.find((r:any)=>r.id==='fleet:first:%251').message_boards[0].id,'board-one')
 assert.equal(all.find((r:any)=>r.id==='fleet:second:%251').message_boards[0].id,'board-two')
})
test('filtering two board sources cannot reassign an unscoped local Session to the selected machine',async()=>{
 const f=fixture(),other={...f.target,machine:'second',label:'Second machine',source:{...f.target.source,key:'second'}}
 f.target.machine='first';f.deps.boardTargets=()=>[f.target,other]
 const chosen=await result('get_session_board',{board:'Delivery board',machine:'second'},f.deps)
 assert.equal(chosen.board.lead.session_id,undefined);assert.equal(chosen.board.lead.label_source,'board_metadata')
})
test('partial failures keep healthy boards, but a name lookup cannot claim global uniqueness',async()=>{
 const f=fixture(),offline={...f.target,machine:'offline',label:'Offline machine',source:{...f.target.source,key:'offline',read:async()=>{throw Error('offline')}}}
 f.deps.boardTargets=()=>[f.target,offline]
 const listing=await result('list_session_boards',{},f.deps);assert.equal(listing.boards.length,1);assert.equal(listing.complete,false);assert.equal(listing.unavailable.length,1)
 const named=await result('get_session_board',{board:'Delivery board'},f.deps);assert.equal(named.error,'session_boards_unavailable')
})
test('missing member metadata and stale source scope remain unknown rather than unassigned',async()=>{
 const f=fixture();f.set([{...board,members:undefined}])
 const listing=await result('list_sessions',{},f.deps);assert.equal(listing.idle.find((r:any)=>r.id==='%2').message_board_status,'unavailable')
 let current=true;f.deps.boardTargets=()=>current?[f.target]:[];f.target.source.read=async()=>{current=false;return {boards:[board]} as never}
 const named=await result('get_session_board',{board:'Delivery board'},f.deps);assert.equal(named.error,'session_boards_unavailable');assert.equal(named.unavailable[0].error,'session_board_source_changed')
})
test('an ended Lead retains its board actor identity without inventing a live Session handle',async()=>{
 const f=fixture();f.set([{...board,lead:'codex:ended',members:[{id:'codex:ended',label:'Previous lead'}]}])
 const named=await result('get_session_board',{board:'board-one'},f.deps);assert.equal(named.board.lead.actor_id,'codex:ended');assert.equal(named.board.lead.session_id,undefined);assert.equal(named.board.lead.availability,'not_listed')
})


test('malformed metadata and an unavailable index are typed failures instead of empty board lists',async()=>{
 const f=fixture();f.target.source.read=async()=>({boards:[{...board,members:{}}]} as never)
 const out=await result('list_session_boards',{},f.deps);assert.equal(out.error,'session_boards_unavailable');assert.equal(out.unavailable[0].error,'session_board_response_invalid')
 f.target.source.read=async()=>{throw Error('offline')};assert.equal((await result('list_session_boards',{},f.deps)).error,'session_boards_unavailable')
})
