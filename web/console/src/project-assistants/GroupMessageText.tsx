import type * as W from "@clawdline/contract"
import { BotAvatar } from "./BotAvatar"
import { BotMessageMarkdown } from "./BotMessageMarkdown"
import { shownGroupMentions, sourceGroupMentions, type GroupMentionSpan } from "./group-mentions"
import type { Words } from "./words"
import "./bot-mentions.css"

type Member={id:string;name:string;appearance?:W.ProjectAssistantAppearance}

/** A room line drawn as a one-to-one chat draws its messages: the person's
 * text as written, a member's as Markdown, and each @ the source routed as a
 * mention chip. The chips come from the entry's `mentions`, not from the text. */
export function GroupMessageText({line,members,w}:{line:W.ProjectAssistantRoomEntry;members:readonly Member[];w:Words}) {
  const runs=shownGroupMentions(sourceGroupMentions(line.mentions))
  const who=(run:GroupMentionSpan)=>members.find(m=>m.id===run.id)
  if(line.kind==="member")return <BotMessageMarkdown text={line.text} w={w} mentions={runs.map(run=>({...run,title:who(run)?.name}))}/>
  if(!runs.length)return <p>{line.text}</p>
  const out=[]
  let at=0
  for(const run of runs){
    if(run.start>at)out.push(line.text.slice(at,run.start))
    const m=who(run)
    out.push(<span key={run.start} className="pa-mention-chip" data-mention={run.id} title={m?.name}>{m?.appearance&&<BotAvatar appearance={m.appearance} size={16}/>}{line.text.slice(run.start,run.end)}</span>)
    at=run.end
  }
  out.push(line.text.slice(at))
  return <p>{out}</p>
}
