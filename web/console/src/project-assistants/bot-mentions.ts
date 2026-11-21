/** A Bot picked from the @ list is stored inline as @[Name](bot:ID). Editors
 * show only @Name; the daemon reads the ID and passes it to message_bot. */
export type BotMentionPick={id:string;name:string}
export type BotMentionSegment=string|BotMentionPick

const token=/@\[([^\]\n]+)\]\(bot:([^)\s]+)\)/gu
const cleanName=(name:string)=>name.replace(/[\]\n\r]+/gu," ").trim()
const boundary=/[\s\p{P}\p{S}]/u

export function botMentionToken(pick:BotMentionPick):string { return `@[${cleanName(pick.name)}](bot:${pick.id})` }

export function botMentionSegments(text:string):BotMentionSegment[] {
  const out:BotMentionSegment[]=[]
  let last=0
  for(const m of text.matchAll(token)){
    if(m.index>last)out.push(text.slice(last,m.index))
    out.push({name:m[1],id:m[2]})
    last=m.index+m[0].length
  }
  if(last<text.length)out.push(text.slice(last))
  return out
}

export function decodeBotMentions(text:string):{display:string;picks:BotMentionPick[]} {
  const picks:BotMentionPick[]=[]
  const display=botMentionSegments(text).map(s=>{if(typeof s==="string")return s;if(!picks.some(p=>p.name===s.name))picks.push(s);return "@"+s.name}).join("")
  return {display,picks}
}

/** Re-attach IDs to every intact @Name. An edited name stays plain text. */
export function encodeBotMentions(display:string,picks:readonly BotMentionPick[]):string {
  const byName=new Map<string,BotMentionPick>()
  for(const p of picks){const name=cleanName(p.name);if(name)byName.set(name,{id:p.id,name})}
  const names=[...byName.keys()].sort((a,b)=>b.length-a.length)
  if(!names.length)return display
  let out="",i=0
  while(i<display.length){
    if(display[i]==="@"&&(i===0||boundary.test(display[i-1]))){
      const name=names.find(n=>display.startsWith(n,i+1)&&(i+1+n.length===display.length||boundary.test(display[i+1+n.length])))
      if(name){out+=botMentionToken(byName.get(name)!);i+=1+name.length;continue}
    }
    out+=display[i];i++
  }
  return out
}

/** The @word being typed at the caret, if any. */
export function botMentionQuery(display:string,caret:number):{start:number;query:string}|null {
  const before=display.slice(0,caret)
  const at=before.lastIndexOf("@")
  if(at<0||(at>0&&!boundary.test(before[at-1])))return null
  const query=before.slice(at+1)
  if(/[\s@]/u.test(query)||query.length>40)return null
  return {start:at,query}
}

export function insertBotMention(display:string,start:number,caret:number,name:string):{display:string;caret:number} {
  const tail=display.slice(caret),spacer=/^\s/u.test(tail)?"":" "
  const head=display.slice(0,start)+"@"+cleanName(name)+spacer
  return {display:head+tail,caret:head.length}
}
