import "../../src/loading-skeleton.css"
import "../../src/legacy/tokens.css"
import "../../src/legacy/base.css"
import "../../src/legacy/header.css"
import "../../src/legacy/shell.css"
import "../../src/legacy/list.css"
import "../../src/legacy/schedules.css"
import "../../src/pages/schedules/run-loading.css"
import "../../src/legacy/detail.css"
import "../../src/legacy/transcript.css"
import "../../src/legacy/composer.css"
import "../../src/legacy/live.css"
import "../../src/legacy/agents.css"
import "../../src/legacy/status-line.css"
import "../../src/legacy/sheets.css"
import "../../src/legacy/coordinator.css"
import "../../src/legacy/door.css"
import "../../src/legacy/responsive.css"
import "../../src/legacy/pages.css"
import "../../src/legacy/devices.css"
import "../../src/legacy/projects.css"
import "../../src/legacy/worktrees.css"
import "../../src/legacy/plan.css"
import "../../src/legacy/board.css"
import "../../src/legacy/session-board.css"
import "../../src/legacy/documents.css"
import "../../src/legacy/user-messages.css"
import "../../src/legacy/snippets.css"
import "../../src/side-border-overrides.css"
import "../../src/shell.css"
import "../../src/sidebar-scroll.css"
import "../../src/ui/disclosure-motion.css"
import "../../src/brand-handle.css"
import "../../src/font-scale.css"

import React,{useState} from 'react'
import {createRoot} from 'react-dom/client'
import {SessionBoardRows,sessionBoardActor,type SessionBoardSource} from '../../src/session/SessionBoards.js'
import {Row,ProjectedRow} from '../../src/session/List.js'
import {activateCatalog} from '../../src/catalog.js'
import {loadStrings,publish} from '../../src/legacy/bridge.js'
const query=new URLSearchParams(location.search),lang=query.get('lang')==='en'?'en':'zh-Hant',fleet=query.get('mode')==='fleet'
document.documentElement.lang=lang
const native=window.fetch.bind(window)
window.fetch=async(input,init)=>{
 const url=new URL(typeof input==='string'?input:input instanceof URL?input.href:input.url,location.href)
 if(!url.pathname.startsWith('/v1/'))return native(input,init)
 if(init?.method&&init.method!=='GET')throw Error('Fixture refused a write')
 if(url.pathname==='/v1/personas')return Response.json({personas:[]})
 return Response.json({error:{code:'not_found',message:'Synthetic fixture'}},{status:404})
}
const words=await(await native('/catalogs/'+lang+'.json')).json();activateCatalog(words,lang);await loadStrings(async()=>words)
const rows=[0,1,2,3].map(i=>({id:'%'+(i+1),sessionId:'conversation-'+i,assistant:'codex',label:['Current lead','Member','Unassigned','Helper child'][i],title:['Current lead','Member','Unassigned','Helper child'][i],backend:'tmux',state:'idle',cwd:'/fixture/project',agents:[],shells:[]}))
publish(rows as never,rows[0].id,'',rows[0].id,[{id:'fixture-helper',state:'briefed',title:'Fixture helper',root:{terminalId:rows[0].id},child:{terminalId:rows[3].id}}] as never)
let name='Delivery board',lead='codex:conversation-0',offline=false
const source:SessionBoardSource={key:'affiliation-fixture',writable:false,read:async()=>{if(offline)throw Error('offline');return {boards:[{id:'board',project:'/fixture/project',name,purpose:'Review',lead,summary:'',latest:0,members:rows.slice(0,2).map(row=>({id:'codex:'+row.sessionId,label:row.title}))}]} as never},change:async()=>{throw Error('Fixture refused a write')}}
const refresh=()=>window.dispatchEvent(new CustomEvent('clawdline:session-board-changed',{detail:source.key}))
function Fixture(){
 const [result,setResult]=useState('Ready'),[controls,setControls]=useState(true)
 const validate=()=>{try{
  const cards=[...document.querySelectorAll<HTMLElement>('.row[data-id]')];if(cards.length!==4)throw Error('Expected four real Session rows')
  for(const [index,row] of rows.entries()){
   const card=cards.find(card=>card.dataset.id===row.id)!,badge=card.querySelector<HTMLElement>('.session-board-membership')
   if(offline){if(badge)throw Error('Unavailable membership was shown as known');continue}
   if(!badge||!badge.textContent?.includes(index<2?name:words['next.sessionBoardUnassigned']))throw Error('Wrong board affiliation for '+row.id)
   const a=card.getBoundingClientRect(),b=badge.getBoundingClientRect();if(b.width<=0||b.height<=0||b.left<a.left-1||b.right>a.right+1||b.top<a.top-1||b.bottom>a.bottom+1)throw Error('Membership label overflows its row')
  }
  if(!offline&&cards.find(card=>card.hasAttribute('data-board-lead'))?.dataset.id!==rows[lead.endsWith('1')?1:0].id)throw Error('Lead role did not refresh')
  if(!offline&&cards.find(card=>card.dataset.id===rows[3].id)?.dataset.depth!=='1')throw Error('Helper child lost its grouped depth')
  if(document.documentElement.scrollWidth>innerWidth+1)throw Error('List overflows horizontally: '+document.documentElement.scrollWidth+'/'+innerWidth+'/'+document.documentElement.clientWidth+'; '+[...document.querySelectorAll<HTMLElement>('body *')].filter(node=>node.getBoundingClientRect().right>innerWidth+1).slice(0,8).map(node=>node.tagName+'.'+node.className+':'+node.getBoundingClientRect().right).join(','))
  setResult('PASS · '+(fleet?'fleet':'local')+' affiliation · '+(offline?'unavailable preserved':'current Lead and unassigned')+' · provider calls 0')
 }catch(error){setResult(String(error))}}
 return <>{controls&&<div style={{position:'fixed',zIndex:99999,background:'#fff',color:'#111',top:0,left:0,right:0}}><button onClick={validate}>驗證留言板</button><button onClick={()=>{lead='codex:conversation-1';refresh()}}>轉移 Lead</button><button onClick={()=>{name=lang==='en'?'Long board name '.repeat(6):'長名稱留言板'.repeat(12);refresh()}}>長留言板名稱</button><button onClick={()=>{offline=true;refresh()}}>模擬離線</button><button onClick={()=>setControls(false)}>隱藏驗收列</button><output style={{display:'block'}}>{result}</output></div>}
 <main className="app" data-view="list" style={{width:'100%',height:'100dvh',display:'flex'}}><ul className="list" style={{width:'100%',maxWidth:340,margin:0,padding:0,listStyle:'none'}}><SessionBoardRows source={source} rows={rows.map(row=>({id:row.id,parent:row.id===rows[3].id?rows[0].id:undefined,actor:sessionBoardActor(row.assistant,row.sessionId,row.title),render:(board,boardName)=>fleet?<ProjectedRow key={row.id} title={row.title} sessionID={row.id} selectionKey={row.id} machineID="fixture" machineName="Fixture machine" assistant="codex" state="idle" stateLabel="Idle" freshness="" attention={false} open={false} depth={board?.depth??0} branchThrough={board?.branchThrough??false} ancestorThrough={board?.ancestorThrough??false} boardLead={board?.lead} boardAvatar={board?.avatar} boardName={boardName} onOpen={()=>{}}/>:<Row key={row.id} row={row as never} depth={board?.depth??0} branchThrough={board?.branchThrough??false} ancestorThrough={board?.ancestorThrough??false} selected={false} open={false} swiped={false} onOpen={()=>{}} boardLead={board?.lead} boardAvatar={board?.avatar} boardName={boardName}/>}))}/></ul></main></>
}
createRoot(document.getElementById('root')!).render(<Fixture/>)
