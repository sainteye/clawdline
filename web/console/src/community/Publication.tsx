import { useCallback, useState } from "react"
import { communityCall } from "./api.js"
import { CommunityError, CommunityLink } from "./components.js"
import { useCommunityMutation, type CommunityCall } from "./hooks.js"
import { supportWord as w } from "../support/words.js"
import type { CommunityTopic } from "./model.js"
export function CommunityPublication({id,revision,published,origin,go,onSaved}:{id:string;revision:number;published:boolean;origin:string;go:(path:string)=>void;onSaved:()=>void}) {
  const [consent,setConsent]=useState(false)
  const call=useCallback<CommunityCall>((path,method,body,key)=>communityCall(origin,path,method,body,key),[origin])
  const mutation=useCommunityMutation(call,"publication:"+id)
  return <section className="community-publication">{published?<CommunityLink to={"/community/topics/"+id} go={go}>{w("communityViewPublic")} ↗</CommunityLink>:<form onSubmit={e=>{e.preventDefault();void mutation.run<{topic:CommunityTopic}>("/su/v1/community/topics/"+id,{action:"publish",consent:true,expected_revision:revision},a=>{onSaved();go("/community/topics/"+a.topic.id)})}}><label className="help-check"><input type="checkbox" checked={consent} disabled={mutation.locked} required onChange={e=>setConsent(e.target.checked)}/>{w("communityAuthorConsent")}</label><CommunityError code={mutation.error}/><button className="help-btn" disabled={mutation.busy||(!mutation.uncertain&&!consent)}>{mutation.busy?w("busy"):mutation.uncertain?w("retry"):w("communityPublishExisting")}</button></form>}</section>
}
