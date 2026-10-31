import { useEffect, useState } from "react"
import { failureSentence } from "../../refusals/refusal-text.js"
import { DailyChart } from "./AdminDashboard.js"
import { demoVisits, readVisits, VISIT_BOUNDS, type VisitGroup, type Visits } from "./visits.js"
import type { Say } from "./words.js"

export function VisitsOverview({ apiOrigin, demo, refreshToken, say, locale }: { apiOrigin: string; demo: boolean; refreshToken: string; say: Say; locale: string }) {
  const [days,setDays]=useState(30), [page,setPage]=useState(1), [pagesPage,setPagesPage]=useState(1), [retry,setRetry]=useState(0)
  const [state,setState]=useState<{kind:"loading"}|{kind:"failed";code:string}|{kind:"ready";data:Visits}>({kind:"loading"})
  useEffect(()=>{
    const abort=new AbortController(), timeout=setTimeout(()=>abort.abort("analytics_timeout"),VISIT_BOUNDS.readMs)
    setState({kind:"loading"})
    const data=demo?Promise.resolve(demoVisits(days,page)):readVisits(fetch,apiOrigin,days,page,abort.signal,pagesPage)
    let active=true
    void data.then(data=>{if(active)setState({kind:"ready",data})}).catch(error=>{if(active)setState({kind:"failed",code:error instanceof Error?error.message:"analytics_unavailable"})}).finally(()=>clearTimeout(timeout))
    return()=>{active=false;clearTimeout(timeout);abort.abort()}
  },[apiOrigin,demo,refreshToken,days,page,pagesPage,retry])
  const n=new Intl.NumberFormat(locale), format=(v:number)=>n.format(v), seconds=(ms:number)=>say("visitsSeconds",{n:format(Math.round(ms/1000))})
  const label=(key:string)=>({reddit:"Reddit",github:"GitHub",google:"Google",bing:"Bing",hackernews:"Hacker News",producthunt:"Product Hunt",x:"X",direct:say("visitsDirect"),other:say("visitsOther"),none:say("visitsNoCampaign"),unknown:say("visitsUnknown"),desktop:say("visitsDesktop"),mobile:say("visitsMobile"),tablet:say("visitsTablet"),browser:say("visitsBrowser"),crawler:say("visitsCrawler"),ai:say("visitsAI")}[key]??key)
  const group=(title:string,rows:VisitGroup[],country=false)=> <section className="adm-panel adm-visit-distribution"><h3 className="adm-h3">{title}</h3>{rows.length?<table className="adm-table"><tbody>{rows.map(r=><tr key={r.key}><th scope="row">{country&&r.key!=="unknown"?new Intl.DisplayNames([locale],{type:"region"}).of(r.key)??r.key:label(r.key)}</th><td className="num">{format(r.views)}</td></tr>)}</tbody></table>:<p>{say("visitsEmpty")}</p>}</section>
  return <section className="adm-visits" aria-labelledby="adm-visits-title">
    <div className="adm-visit-heading"><div><h2 id="adm-visits-title" className="adm-h2">{say("visitsTitle")}</h2><p className="adm-fine">{say("visitsIntro")}</p></div>
      <label className="adm-visit-period">{say("visitsPeriod")}<select value={days} onChange={e=>{setDays(Number(e.target.value));setPage(1);setPagesPage(1)}}>{[7,30,90].map(d=><option key={d} value={d}>{say("visitsDays",{n:d})}</option>)}</select></label>
    </div>
    {state.kind==="loading"?<div className="adm-panel" role="status" aria-busy="true">{say("visitsLoading")}</div>:state.kind==="failed"?<div className="adm-panel" role="alert"><p>{failureSentence({code:state.code},say("visitsFailed"))}</p><button className="adm-btn" onClick={()=>setRetry(r=>r+1)}>{say("tryAgain")}</button></div>:<>
      <p className="adm-fine">{say("visitsCoverage",{time:state.data.started_at.slice(0,16).replace("T"," ")})}</p>
      <div className="adm-tiles adm-tiles-4">{[
        [say("visitsViews"),format(state.data.views)], [say("visitsMembers"),format(state.data.member_accounts)],
        [say("visitsDuration"),state.data.views?seconds(state.data.active_ms/state.data.views):"—"],[say("visitsRequests"),format(state.data.requests)],
      ].map(([title,value])=><div className="adm-tile" key={title}><span className="adm-tile-label">{title}</span><strong className="adm-tile-value">{value}</strong></div>)}</div>
      <p className="adm-fine">{state.data.previous_views===null?say("visitsNoComparison"):say("visitsComparison",{current:format(state.data.views),previous:format(state.data.previous_views)})}</p>
      <div className="adm-visit-charts"><DailyChart title={say("visitsViews")} days={state.data.daily.map(r=>({day:r.day,count:r.views}))} say={say} locale={locale}/><DailyChart title={say("visitsRequests")} days={state.data.daily.map(r=>({day:r.day,count:r.requests}))} say={say} locale={locale}/></div>
      <section className="adm-panel"><h3 className="adm-h3">{say("visitsPages")}</h3><div className="adm-table-wrap"><table className="adm-table"><thead><tr><th>{say("visitsPage")}</th><th className="num">{say("visitsViews")}</th><th className="num">{say("visitsDuration")}</th></tr></thead><tbody>{state.data.pages.map(r=><tr key={r.key}><th scope="row"><a href={"https://clawdline.com"+r.key+(r.key==="/"?"":"/")} target="_blank" rel="noreferrer">{r.key}</a></th><td className="num">{format(r.views)}</td><td className="num">{r.views?seconds(r.active_ms/r.views):"—"}</td></tr>)}</tbody></table></div>{!state.data.pages.length?<p>{say("visitsEmpty")}</p>:null}<nav className="adm-visit-pager" aria-label={say("visitsPages")}><button className="adm-btn adm-btn-small" disabled={pagesPage<=1} onClick={()=>setPagesPage(p=>p-1)}>{say("visitsPrevious")}</button><span>{say("visitsPagePagination",{page:pagesPage,total:format(state.data.pages_total)})}</span><button className="adm-btn adm-btn-small" disabled={!state.data.pages_more} onClick={()=>setPagesPage(p=>p+1)}>{say("visitsNext")}</button></nav></section>
      <div className="adm-visit-groups">{group(say("visitsSources"),state.data.sources)}{group(say("visitsCampaigns"),state.data.campaigns)}{group(say("visitsDevices"),state.data.devices)}{group(say("visitsAgents"),state.data.agents)}{group(say("visitsCountries"),state.data.countries,true)}</div>
      <p className="adm-fine">{say("visitsUnits")}</p>
      <section className="adm-panel"><h3 className="adm-h3">{say("visitsMemberHistory")}</h3><p className="adm-fine">{say("visitsMemberNote")}</p><div className="adm-table-wrap"><table className="adm-table"><thead><tr><th>{say("visitsAccount")}</th><th>{say("visitsPage")}</th><th>{say("visitsTime")}</th><th className="num">{say("visitsActiveTime")}</th></tr></thead><tbody>{state.data.members.map((r,i)=><tr key={r.account_id+r.at+r.path+i}><td><span>{r.account_name??r.account_id}</span>{r.account_name?<small className="adm-visit-account">{r.account_id}</small>:null}</td><td><a href={"https://clawdline.com"+r.path+(r.path==="/"?"":"/")} target="_blank" rel="noreferrer">{r.path}</a></td><td><time dateTime={r.at}>{r.at.slice(0,19).replace("T"," ")}</time></td><td className="num">{seconds(r.active_ms)}</td></tr>)}</tbody></table></div>{!state.data.members.length?<p>{say("visitsNoMembers")}</p>:null}
      <nav className="adm-visit-pager" aria-label={say("visitsMemberHistory")}><button className="adm-btn adm-btn-small" disabled={page<=1} onClick={()=>setPage(p=>p-1)}>{say("visitsPrevious")}</button><span>{say("visitsPagination",{page,total:format(state.data.members_total)})}</span><button className="adm-btn adm-btn-small" disabled={page*state.data.page_size>=state.data.members_total} onClick={()=>setPage(p=>p+1)}>{say("visitsNext")}</button></nav></section>
    </>}
  </section>
}
