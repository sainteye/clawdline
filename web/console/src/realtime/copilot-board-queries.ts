import type {SessionBoardTarget,SessionMessageBoard} from '../session/SessionBoards.js'
import type {CopilotSessionRow} from './copilot-tools.js'
import type {OperationObserver} from './copilot-history.js'

type BoardEntry={board:SessionMessageBoard;target:SessionBoardTarget}
export interface BoardDirectory {targets:readonly SessionBoardTarget[];boards:BoardEntry[];available:Set<string>;singleSourceKey?:string;unavailable:{machine:string;machine_name:string;error:string}[]}
type BoardDeps={boardTargets?:()=>readonly SessionBoardTarget[];observe?:OperationObserver;operationParent?:string;operationSignal?:AbortSignal}
const exact=(a:string,b:string)=>a.normalize('NFKC').toLocaleLowerCase()===b.normalize('NFKC').toLocaleLowerCase()
const machine=(target:SessionBoardTarget)=>target.machine??'local'
const actor=(row:CopilotSessionRow)=>row.assistant&&row.conversation?`${row.assistant}:${row.conversation}`:undefined
const onMachine=(row:CopilotSessionRow,target:SessionBoardTarget,singleSourceKey?:string)=>row.machine?row.machine===target.machine:singleSourceKey===target.source.key

/** Read only board indexes, never posts or mutation endpoints. */
export async function readBoardDirectory(deps:BoardDeps,wantedMachine=''):Promise<BoardDirectory>{
 const seen=new Set<string>(),all=(deps.boardTargets?.()??[]).filter(target=>{
  if(seen.has(target.source.key))return false
  seen.add(target.source.key)
  return true
 })
 const targets=all.filter(target=>!wantedMachine||exact(machine(target),wantedMachine)||exact(target.label,wantedMachine))
 const directory:BoardDirectory={targets,boards:[],available:new Set(),singleSourceKey:all.length===1?all[0].source.key:undefined,unavailable:[]}
 await Promise.all(targets.map(async target=>{
  const span=deps.observe?.({name:'read_session_boards',source:'background',parent:deps.operationParent,target:target.label})
  try{
   const result=await target.source.read<{boards:SessionMessageBoard[]}>({},deps.operationSignal)
   if(!Array.isArray(result?.boards)||result.boards.some(board=>!board||typeof board.id!=='string'||!board.id||typeof board.name!=='string'||!board.name||typeof board.lead!=='string'||(board.members!==undefined&&(!Array.isArray(board.members)||board.members.some(member=>!member||typeof member.id!=='string'||!member.id||typeof member.label!=='string')))))throw Error('session_board_response_invalid')
   if(!(deps.boardTargets?.()??[]).some(current=>current.source.key===target.source.key&&current.machine===target.machine))throw Error('session_board_source_changed')
   directory.available.add(target.source.key)
   directory.boards.push(...result.boards.filter(board=>!board.archived_at).map(board=>({board,target})))
   span?.end({outcome:'done'})
  }catch(error){
   const code=error instanceof Error&&['session_board_response_invalid','session_board_source_changed'].includes(error.message)?error.message:'session_boards_unavailable'
   directory.unavailable.push({machine:machine(target),machine_name:target.label,error:code});span?.end({outcome:'unavailable',failure_code:code})
  }
 }))
 return directory
}

/** Missing source/identity is unknown membership, never an empty membership list. */
export function boardMembership(row:CopilotSessionRow,directory:BoardDirectory){
 const targets=directory.targets.filter(target=>onMachine(row,target,directory.singleSourceKey))
 if(!actor(row)||targets.length!==1||!directory.available.has(targets[0].source.key))return {message_board_status:'unavailable' as const}
 const scoped=directory.boards.filter(entry=>entry.target.source.key===targets[0].source.key)
 if(scoped.some(entry=>!Array.isArray(entry.board.members)))return {message_board_status:'unavailable' as const}
 const entries=scoped.filter(entry=>entry.board.members?.some(member=>member.id===actor(row)))
 return {message_board_status:'known' as const,message_boards:entries.map(({board})=>({id:board.id,name:board.name,is_lead:board.lead===actor(row)}))}
}
function boardResult({board,target}:BoardEntry,rows:readonly CopilotSessionRow[],singleSourceKey:string|undefined,rowsKnown:boolean){
 const lead=rows.find(row=>onMachine(row,target,singleSourceKey)&&actor(row)===board.lead)
 const saved=board.members?.find(member=>member.id===board.lead)??target.candidates.find(member=>member.id===board.lead)
 return {id:board.id,name:board.name,project:typeof board.project==='string'?board.project.split(/[\\/]/).filter(Boolean).at(-1):undefined,purpose:board.purpose,machine:machine(target),machine_name:target.label,
  lead:board.lead ? {actor_id:board.lead,label:lead?.label??saved?.label??board.lead,...(lead?{session_id:lead.id,availability:'listed',label_source:'session_directory'}:{availability:rowsKnown?'not_listed':'unknown',label_source:saved?'board_metadata':'actor_id'})} : null,
  member_count:board.members?.length??null}
}
export async function querySessionBoards(name:string,args:Record<string,unknown>,deps:BoardDeps,readRows:()=>Promise<{rows:CopilotSessionRow[]}|{error:string}>){
 const selectedMachine=typeof args.machine==='string'?args.machine.trim():''
 const [directory,reading]=await Promise.all([readBoardDirectory(deps,selectedMachine),readRows()])
 const known='rows' in reading,rows=known?reading.rows:[],boards=directory.boards.map(entry=>boardResult(entry,rows,directory.singleSourceKey,known))
 const common={unavailable:directory.unavailable,complete:directory.targets.length>0&&!directory.unavailable.length}
 if(!directory.targets.length)return {error:'session_boards_unavailable',boards:[],...common}
 if(!directory.available.size)return {error:'session_boards_unavailable',boards:[],...common}
 if(name==='list_session_boards')return {boards,...common}
 const wanted=typeof args.board==='string'?args.board.trim():''
 if(!wanted)return {error:'session_board_reference_required'}
 const ids=boards.filter(board=>board.id===wanted),matches=ids.length?ids:boards.filter(board=>exact(board.name,wanted))
 if(matches.length>1)return {error:'session_board_name_ambiguous',choices:matches,...common}
 if(!common.complete)return {error:'session_boards_unavailable',choices:matches,...common}
 if(!matches.length)return {error:'session_board_not_found',choices:boards.map(({id,name,machine,machine_name,project})=>({id,name,machine,machine_name,project})),...common}
 return {board:matches[0],...common}
}
