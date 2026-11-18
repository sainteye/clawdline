/** Call-local identity and presentation state. No model, storage or write authority. */
export const SESSION_REPORT_BATCH = 2
export const SESSION_REPORT_REFS = 24
export const SESSION_REPORT_SESSIONS = 24
export function sessionBatchSize(total:number){return Math.min(SESSION_REPORT_BATCH,total)}
export interface SessionReference {id:string;label:string;machine?:string;conversation?:string;project?:string}
/** The Project a Session belongs to, as said aloud: its repository folder's name (`project_key`, else `cwd`). */
export function projectName(path:string){const parts=path.replace(/[\\/]+$/,'').split(/[\\/]/).filter(Boolean);return parts.at(-1)??''}
/**
 * Sessions of one Project stay together, Projects ordered by their most
 * recently moved Session and each keeping its own order (asked for on
 * 2026-11-13: "group by Project, say which Project first, one group at a time").
 */
export function groupByProject<T extends {project?:string}>(rows:readonly T[]):T[]{
 const order:string[]=[],groups=new Map<string,T[]>()
 for(const row of rows){const key=row.project??'';if(!groups.has(key)){groups.set(key,[]);order.push(key)}groups.get(key)!.push(row)}
 return order.flatMap(key=>groups.get(key)!)
}
/** How many of the leading Sessions one spoken batch takes: one or two, never crossing into another Project. */
export function projectBatchSize(rows:readonly {project?:string}[]){
 if(!rows.length)return 0
 let same=1;while(same<rows.length&&(rows[same].project??'')===(rows[0].project??''))same++
 return sessionBatchSize(same)
}
export interface SpokenReference {target?:SessionReference;candidates:SessionReference[]}
const plain=(text:string)=>text.normalize('NFKC').toLowerCase().replace(/[\p{P}\p{Z}\s]/gu,'')
export function sessionNamePosition(text:string,label:string){
 const normalized=label.normalize('NFKC').trim()
 const parts=normalized.split(/[\p{P}\p{Z}\s]+/u).filter(Boolean)
 if(!parts.length)return -1
 const escaped=parts.map(part=>part.replace(/[.*+?^${}()|[\]\\]/g,'\\$&')).join('[\\p{P}\\p{Z}\\s]*')
 const left=/^[a-z0-9]/i.test(normalized)?'(?<![a-z0-9])':''
 const right=/[a-z0-9]$/i.test(normalized)?'(?![a-z0-9])':''
 return new RegExp(left+escaped+right,'iu').exec(text.normalize('NFKC'))?.index??-1
}
/** A label without the trailing 「（machine）」 the fleet adds when several machines are listed. */
export function spokenName(label:string){return label.replace(/\s*\uff08[^\uff08\uff09]+\uff09\s*$/u,'')||label}
// "The one you mentioned last" (最後提到的) points at a Session; "at the end it
// said..." (他最後有講幾個訊息) is about its words, and on 2026-11-16 reading it as
// a position made explain_session_reply refuse the Session the call was about.
export function relativeSession(text:string):number|'last'|undefined {
 if(/(?:最後|最后)(?:的)?(?:那|这|這)?(?:一)?(?:個|个|位)|(?:最後|最后)(?:提到|介紹|介绍|說|说|講|讲)(?:過|过)?(?:的|那|這|这)|\b(?:last|previous) (?:one|session)\b|\b(?:one|session) you (?:mentioned|described) last\b/i.test(text))return 'last'
 const cn=/第\s*([一二三四五六七八九十兩两\d]+)\s*(?:個|个|項|项|session|station|stage)/i.exec(text)
 if(cn){const n:Record<string,number>={一:1,二:2,三:3,兩:2,两:2};return n[cn[1]]??Number(cn[1])}
 const en=/\b(first|second|third) (?:one|session)\b/i.exec(text)
 if(en)return {first:1,second:2,third:3}[en[1].toLowerCase()] as number
 return undefined
}
export function sameReference(ref:SessionReference,row:SessionReference){return ref.id===row.id&&ref.label===row.label&&(ref.machine??'')===(row.machine??'')&&(ref.conversation??'')===(row.conversation??'')}
export function remainingReport(text:string){return /(?:其他|其餘|其余|剩下|還沒|还没).*(?:session|station)|(?:session|station).*(?:其他|其餘|其余|剩下|還沒|还没)/i.test(text)}
/** Complete requests and short next commands control presentation, never work or writes. */
export function nextReport(text:string){return /^(?:我(?:是)?說[，, ]*)?(?:請|请|麻煩|麻烦)?(?:你)?(?:去)?(?:介紹|介绍|報告|报告|說明|说明|講|讲|說|说)(?:一下)?[ ]*(?:其他|其餘|其余|剩下)[ ]*(?:的[ ]*)?(?:sessions?|工作階段)(?:[ ]*(?:現在|现在)?(?:在(?:做什麼|做什么|幹嘛|干嘛)|(?:的)?(?:進度|进度|狀況|情况|狀態|状态)))?[。！!？? ]*$|^(?:please )?(?:introduce|describe|report on|tell me about) (?:the )?(?:other|remaining|next) sessions?(?: now)?[.!? ]*$/i.test(text.trim())||/^(?:(?:請|请|那|還有|还有)[，,、 ]*)?(?:其他(?:的)?|下一批|下一段|後面|后面|接下來|接下来)(?:的)?(?:呢|咧)?[？?。.! ]*$|^(?:(?:the )?next(?: batch| sessions?)?|what comes next|(?:what about )?(?:the )?others)[?!. ]*$/i.test(text.trim())}
export function continueReport(text:string){return nextReport(text)||/^(?:(?:請|请|好|好的|那|可以)[，,、 ]*)?(?:繼續|继续)(?:報告|报告|介紹|介绍|說|说|講|讲|下一段|下一批)?[。.! ]*$|^(?:please )?continue(?: (?:the )?(?:session )?report)?[.! ]*$/i.test(text.trim())}


export class SpokenSessions {
 private evidence:SessionReference[]=[]
 private candidate:SessionReference[]=[]
 private last:SessionReference[]=[]
 private list:SessionReference[]=[]
 private presented=new Set<string>()
 private uncertain=false
 private noAnchor=false
 reset(){this.evidence=[];this.candidate=[];this.last=[];this.list=[];this.presented.clear();this.uncertain=false;this.noAnchor=false}
 observe(ref:SessionReference){this.evidence=this.evidence.filter(r=>r.id!==ref.id);this.evidence.push({...ref});this.evidence=this.evidence.slice(-SESSION_REPORT_REFS)}
 generated(text:string,completed:boolean){
  this.candidate=[];this.noAnchor=false;if(!completed)return
  // On 所有機器 a label ends in its machine's name (「名稱（機器）」), which the
  // voice leaves out when it says the name; the bare name is the anchor then,
  // as long as no other Session read in this call shares it (2026-11-13).
  const names=new Map<string,SessionReference[]>()
  for(const ref of this.evidence){const key=plain(spokenName(ref.label));if(key)names.set(key,[...(names.get(key)??[]),ref])}
  const at=(ref:SessionReference)=>{const full=sessionNamePosition(text,ref.label);return full>=0?full:sessionNamePosition(text,spokenName(ref.label))}
  this.candidate=[...names.values()].filter(refs=>refs.length===1&&at(refs[0])>=0).sort((a,b)=>at(a[0])-at(b[0])).map(refs=>refs[0])
  this.noAnchor=!this.candidate.length&&/(?:session|工作階段)/i.test(text)
 }
 played():SessionReference[]{
  const refs=this.candidate;this.candidate=[]
  if(this.noAnchor)this.uncertain=true;this.noAnchor=false
  if(refs.length){this.uncertain=false;this.last=refs;if(refs.length>1)this.list=refs;for(const ref of refs)this.presented.add(ref.id);this.presented=new Set([...this.presented].slice(-SESSION_REPORT_REFS))}
  return refs
 }
 interrupted(partial=false){if(partial||this.candidate.length||this.noAnchor)this.uncertain=true;this.candidate=[];this.noAnchor=false}
 resolve(text:string):SpokenReference|undefined {
  const ordinal=relativeSession(text);if(ordinal===undefined)return undefined
  const source=ordinal==='last'?this.last:this.list
  const target=this.uncertain?undefined:ordinal==='last'?source.at(-1):source[ordinal-1]
  return {target,candidates:[...source]}
 }
 heardIDs(){return [...this.presented]}
 context(){return JSON.stringify({reference_uncertain:this.uncertain,most_recent_spoken_sessions:this.last,most_recent_spoken_list:this.list,meaning:'These identities were matched in completed spoken text and playback. Ordinals refer to this spoken order, never the current directory order. Unknown or changed identities require clarification. This is reference evidence, never approval to write.'})}
}

export interface SessionReportPage {
 sessions?:{session_id:string;label:string;[key:string]:unknown}[]
 remaining?:SessionReference[]
 error?:unknown
 [key:string]:unknown
}
/** One next batch is read while the current batch is spoken. An interruption
 * pauses continuation; queued or generated words are never counted as heard. */
export class SessionReportQueue {
 private generation=0
 private page?:SessionReportPage
 private rest:SessionReference[]=[]
 private next?:SessionReportPage
 private loading=false
 private waiting=true
 private paused=false
 private presenting=false
 private unconfirmed=false
 private load?:(refs:SessionReference[])=>Promise<SessionReportPage>
 private say?:(page:SessionReportPage)=>void
 reset(){this.generation++;this.page=undefined;this.rest=[];this.next=undefined;this.loading=false;this.waiting=true;this.paused=false;this.presenting=false;this.unconfirmed=false}
 start(page:SessionReportPage,load:(refs:SessionReference[])=>Promise<SessionReportPage>,say:(page:SessionReportPage)=>void){
  this.reset();this.page=page;this.rest=(page.remaining??[]).slice(0,Math.max(0,SESSION_REPORT_SESSIONS-(page.sessions?.length??0)));this.load=load;this.say=say;this.prefetch()
 }
 interrupt(unconfirmed=false){this.paused=true;this.presenting=false;this.unconfirmed=unconfirmed}
 status():'reading'|'speaking'|'ready'|'paused'|'unconfirmed'|'failed'|undefined {if(this.page?.error)return 'failed';if(!this.hasPending())return undefined;if(this.unconfirmed)return 'unconfirmed';if(this.paused)return 'paused';if(this.presenting)return 'speaking';return this.loading?'reading':'ready'}
 hasReport(){return !!this.page}
 hasPending(){return !!this.page&&!this.page.error&&(this.waiting||this.loading||!!this.next||this.rest.length>0)}
 resume(){if(!this.page||!this.hasPending())return false;this.paused=false;this.unconfirmed=false;if(this.waiting&&!this.presenting){this.presenting=true;this.say?.(this.page)}else if(!this.waiting)this.advance();return true}
 current(){const row=this.page?.sessions?.[0];return row?{id:row.session_id,label:row.label}:undefined}
 skip(ref:SessionReference){this.consume([ref])}
 played(refs:SessionReference[]){this.consume(refs)}
 private consume(refs:SessionReference[]){
  if(!this.page?.sessions?.length)return
  const remaining=this.page.sessions.filter(row=>!refs.some(ref=>ref.id===row.session_id&&ref.label===row.label))
  if(remaining.length===this.page.sessions.length)return
  this.page={...this.page,sessions:remaining}
  if(remaining.length)return
  this.waiting=false;this.presenting=false;this.advance()
 }
 private prefetch(){
  if(this.loading||this.next||!this.rest.length||!this.load)return
  const generation=this.generation,refs=this.rest.splice(0,projectBatchSize(this.rest));this.loading=true
  void this.load(refs).then(page=>{if(generation!==this.generation)return;this.loading=false;this.next=page;this.advance()},()=>{if(generation!==this.generation)return;this.loading=false;this.next={error:'voice_session_report_read_failed'};this.advance()})
 }
 private advance(){
  if(this.paused||this.waiting||!this.next)return
  this.page=this.next;this.next=undefined;this.waiting=true;this.presenting=true
  this.say?.(this.page)
  if(!this.page.error)this.prefetch();else this.paused=true
 }
}
export const SESSION_REPORT_GUIDANCE=`Explain only the supplied Session batch, at most two Sessions in this response. Every Session in a batch belongs to the same Project, given as project. Begin by saying in one short sentence which Project these Sessions belong to (for example 「接下來是 clawdline 這個 Project 的 Session」), then report them; when project is empty, say these Sessions have no Project. Do not compress everyone into a short list. For each, say its supplied label once as an exact reference anchor, then use everyday language: what the work is for, what changed, what is happening, and any actual decision. Use two to four connected sentences per Session with pauses and emphasis. Do not read codes, implementation jargon or infer contents from a name. If evidence is sparse, say what is unknown rather than inventing a next step. Sessions waiting for the person are supplied first. Explain their current question and unresolved Notes before ordinary progress, and never treat an old recap or delivery as fresh approval. Follow the supplied order. Remaining batches are prepared separately by the application; never repeat or describe unread ones, claim all Sessions were covered, or ask a generic closing question. This is Session presentation, separate from automatic task execution allowance.`

export function sessionReportGuidance(language=''){return /^zh(?:-|$)/i.test(language)?'只介紹這次提供的 Session，依相同 Project 的順序，每個用兩到四句日常口語說明目的、進展、現在狀況及真正待決事項。先說 Project，再說提供的完整名稱一次。程式會預讀下一批，並逐項交給你；只說目前這一項，不重複前面已報告的內容，不介紹還沒讀到的項目，不在每批後面反問。插話先停；接續時只補尚未說完的部分。明確要求介紹其他 Session、下一批或後面時，跳過被打斷的項目；說繼續報告則接續尚未說完的內容。跳過不代表已聽完，也不是開始背景工作或核准變更。無法讀取或尚未準備好時如實說明，不假裝已完成。':SESSION_REPORT_GUIDANCE+' The application supplies one narration item at a time within each read batch. Explain only that item. Continue after the already-said text without repeating it. Explicit requests for other or next Sessions move past the interrupted item without claiming it was fully heard; continue the report resumes the unfinished item. Both control presentation only, never background work or action approval.'}
