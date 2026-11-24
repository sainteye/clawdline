/** A run of a group message that names who the room asks: a member's ID, or
 * "*" for @all and @everyone. Offsets count UTF-16 units, as the source sends. */
export type GroupMentionSpan={start:number;end:number;id:string}

/** The source's own reading of a stored message (`mentions` on the entry).
 * The console never parses a sent message again. */
export function sourceGroupMentions(mentions:readonly {start:number;end:number;member?:{bot_id:string};everyone?:boolean}[]|undefined):GroupMentionSpan[] {
  return (mentions??[]).flatMap(m=>m.everyone?[{start:m.start,end:m.end,id:"*"}]:m.member?[{start:m.start,end:m.end,id:m.member.bot_id}]:[])
}

/** What the draft would ask, read as the source's mentions() reads it
 * (internal/domain/projectassistant/groups.go): @handle where handle is the
 * name, the name without spaces, or its first word, not followed by more of a
 * Latin word. Only the draft's highlight uses it. testdata/room-mentions.json
 * holds the cases both readings must agree on. */
export function draftGroupMentions(text:string,members:readonly {id:string;name:string}[],author?:string):GroupMentionSpan[] {
  const out:GroupMentionSpan[]=[]
  const add=(handle:string,id:string)=>{for(const [start,end] of spans(text,handle))out.push({start,end,id})}
  add("all","*");add("everyone","*")
  for(const m of members)if(m.id!==author)for(const h of handles(m.name))add(h,m.id)
  out.sort((a,b)=>a.start-b.start||b.end-a.end)
  return out.filter((m,i)=>i===0||m.start!==out[i-1].start||m.end!==out[i-1].end||m.id!==out[i-1].id)
}

/** The runs to draw: the longest at each place, never overlapping. */
export function shownGroupMentions(spans:readonly GroupMentionSpan[]):GroupMentionSpan[] {
  const sorted=[...spans].sort((a,b)=>a.start-b.start||b.end-a.end),out:GroupMentionSpan[]=[]
  for(const s of sorted)if(!out.length||s.start>=out[out.length-1].end)out.push(s)
  return out
}

const lower=(cp:string)=>cp.toLowerCase()
function handles(name:string):string[] {
  const full=[...name.trim()].map(lower).join(""),fields=full.split(/\s+/u).filter(Boolean)
  return [...new Set([full,fields.join(""),fields[0]??""])].filter(Boolean)
}

function spans(text:string,handle:string):[number,number][] {
  const want=[...handle],out:[number,number][]=[]
  for(let i=text.indexOf("@");i>=0;i=text.indexOf("@",i+1)){
    let end=i+1,ok=true
    for(const w of want){
      const cp=text.codePointAt(end)
      if(cp===undefined||lower(String.fromCodePoint(cp))!==w){ok=false;break}
      end+=cp>0xffff?2:1
    }
    if(!ok)continue
    const next=text.codePointAt(end)
    if(next!==undefined&&/^[a-z0-9_]$/u.test(lower(String.fromCodePoint(next))))continue
    out.push([i,end])
  }
  return out
}
