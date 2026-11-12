import type * as W from "@clawdline/contract"
import { buildChat } from "./chat-items"
import type { Words } from "./words"
export function ChatRows({messages,onQuote,w,language}: {messages:readonly W.ProjectAssistantMessage[];onQuote:(m:W.ProjectAssistantMessage)=>void;w:Words;language:string}) {
  const byID=new Map(messages.map(m=>[m.id,m]))
  return <>{buildChat(messages).map(item=>item.kind==="separator"?<div className="pa-separator" key={item.id}><time dateTime={item.at}>{new Intl.DateTimeFormat(language,{month:"short",day:"numeric",hour:"numeric",minute:"2-digit"}).format(Date.parse(item.at))}</time></div>:<article key={item.id} data-message-id={item.id} data-message-seq={item.message.seq} className={`pa-message ${item.message.kind==="user"?"pa-mine":""} ${item.groupStart?"pa-group-start":""}`}>
    {item.message.quote_message_id&&<blockquote>{byID.get(item.message.quote_message_id)?.text??w.quoteMissing}</blockquote>}<p>{item.message.text}</p><button className="pa-quote" onClick={()=>onQuote(item.message)}>{w.quote}</button>
  </article>)}</>
}
