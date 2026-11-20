/** Call-owned execution observations. They never authorize or replay an operation. */
export type OperationOutcome='running'|'done'|'queued'|'asked'|'many'|'failed'|'cancelled'|'cached'|'fallback'|'unavailable'|'unknown'
export interface OperationSpec {name:string;source:'tool'|'background';parent?:string;target?:string}
export interface CallOperation extends OperationSpec {id:string;call:string;started:number;offset:number;ended?:number;ms:number;outcome:OperationOutcome}
export interface OperationSpan {id:string;end:(result:{outcome:OperationOutcome;target?:string})=>void}
export type OperationObserver=(spec:OperationSpec)=>OperationSpan

export class CallHistory {
 private call=''
 private records:CallOperation[]=[]
 private generation=0
 private sequence=0
 private clocks=new Map<string,number>()
 private changed:(records:CallOperation[],event?:CallOperation)=>void
 private now:()=>number
 private wall:()=>number
 constructor(changed:(records:CallOperation[],event?:CallOperation)=>void,now:()=>number=()=>performance.now(),wall:()=>number=()=>Date.now()){this.changed=changed;this.now=now;this.wall=wall}
 reset(call:string){this.call=call;this.records=[];this.clocks.clear();this.sequence=0;this.generation++;this.changed([])}
 begin(spec:OperationSpec):OperationSpan {
  const generation=this.generation,id=`${this.call}:${++this.sequence}`
  const record:CallOperation={...spec,id,call:this.call,started:this.wall(),offset:this.now(),ms:0,outcome:'running'}
  this.clocks.set(id,this.now());this.records=[...this.records,record];this.changed(this.records,record)
  return {id,end:result=>{
   if(generation!==this.generation||!this.clocks.has(id))return
   const finished={...record,...result,ended:this.wall(),ms:Math.max(0,Math.round(this.now()-this.clocks.get(id)!))}
   this.clocks.delete(id);this.records=this.records.map(row=>row.id===id?finished:row);this.changed(this.records,finished)
  }}
 }
 cancel(outcome:'cancelled'|'unknown'='cancelled'){
  for(const record of this.records.filter(row=>row.outcome==='running')){
   const finished={...record,outcome,ended:this.wall(),ms:Math.max(0,Math.round(this.now()-this.clocks.get(record.id)!))}
   this.clocks.delete(record.id);this.records=this.records.map(row=>row.id===record.id?finished:row);this.changed(this.records,finished)
  }
 }
}

/** Additive operation time includes overlap and nested work; elapsed coverage does not. */
export function historySummary(records:readonly CallOperation[]){
 const tools=records.filter(row=>row.source==='tool').length
 const background=records.length-tools
 const cached=records.filter(row=>row.outcome==='cached').length
 const intervals=records.filter(row=>row.ended!==undefined&&!row.parent).map(row=>[row.offset,row.offset+row.ms] as const).sort((a,b)=>a[0]-b[0])
 let activeMs=0,end=0
 for(const [start,stop] of intervals){activeMs+=Math.max(0,stop-Math.max(start,end));end=Math.max(end,stop)}
 const groups=new Map<string,{name:string;count:number;ms:number;running:number}>()
 for(const row of records){const key=row.name;const group=groups.get(key)??{name:key,count:0,ms:0,running:0};group.count++;group.ms+=row.ms;if(row.outcome==='running')group.running++;groups.set(key,group)}
 return {tools,background,cached,count:records.length,activeMs,groups:[...groups.values()]}
}
