import type * as W from "@clawdline/contract"
import { buildChat } from "./chat-items"
import type { Words } from "./words"
export function ChatRows({messages,onQuote,w,language,attachmentURL}: {messages:readonly W.ProjectAssistantMessage[];onQuote:(m:W.ProjectAssistantMessage)=>void;w:Words;language:string;attachmentURL?:(id:string)=>string|null}) {
  const byID=new Map(messages.map(m=>[m.id,m]))
  return <>{buildChat(messages).map(item=>item.kind==="separator"?<div className="pa-separator" key={item.id}><time dateTime={item.at}>{new Intl.DateTimeFormat(language,{month:"short",day:"numeric",hour:"numeric",minute:"2-digit"}).format(Date.parse(item.at))}</time></div>:<article key={item.id} data-message-id={item.id} data-message-seq={item.message.seq} className={`pa-message ${item.message.kind==="user"?"pa-mine":""} ${item.groupStart?"pa-group-start":""}`}>
    {item.message.quote_message_id&&<blockquote>{byID.get(item.message.quote_message_id)?.text??w.quoteMissing}</blockquote>}{(item.message.attachments?.length??0)>0&&<div className="pa-message-pictures">{item.message.attachments!.map(a=>{const src=attachmentURL?.(a.id);return src?<a key={a.id} href={src} target="_blank" rel="noreferrer"><img src={src} alt={w.picture} loading="lazy"/></a>:<span key={a.id} className="pa-picture-missing">{w.picture}</span>})}</div>}{item.message.text&&<p>{item.message.text}</p>}<button className="pa-quote" onClick={()=>onQuote(item.message)}>{w.quote}</button>
  </article>)}</>
}
