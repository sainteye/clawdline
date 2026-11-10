export const VISIT_BOUNDS = { hours: 2160, days: 90, rows: 20, countries: 250, page: 10000, readMs: 10000, path: 300, activeMs: 1800000 } as const
export interface VisitGroup { key: string; views: number; active_ms: number }
export interface VisitHour { hour: string; views: number; requests: number; member_accounts: number; active_ms: number }
export interface Visits {
  schema: string; generated_at: string; started_at: string; timezone: "UTC"; days: number; from: string; until: string;
  views: number; member_views: number; member_accounts: number; active_ms: number; requests: number; previous_views: number | null;
  hourly?: VisitHour[];
  /** Accounts distinct within each Taipei calendar day; an earlier API omits it. */
  member_days?: { timezone: "Asia/Taipei"; rows: { day: string; member_accounts: number }[] };
  /** member_accounts is distinct within each UTC day; an earlier API omits it. */
  daily: { day: string; views: number; requests: number; member_accounts?: number }[];
  pages: VisitGroup[]; pages_more: boolean; pages_total: number; pages_page: number; sources: VisitGroup[]; campaigns: VisitGroup[]; devices: VisitGroup[]; agents: VisitGroup[]; countries: VisitGroup[];
  members_total: number; page: number; page_size: number; retention_days: number;
  members: { account_id: string; account_name: string | null; path: string; at: string; active_ms: number }[];
}
export function visitPath(value: unknown): value is string {
  if (typeof value !== "string" || value.length>VISIT_BOUNDS.path || !/^\/(?:[A-Za-z0-9_-]+\/?)*$/.test(value) || /(?:^|\/)(?:admin|auth|account|session|sessions|private|preview|drafts?)(?:\/|$)/i.test(value)) return false
  return value === "/" || /^\/(?:pricing|security|faq|friends|clawdfather|redeem(?:\/reddit)?)$/.test(value) || /^\/(?:docs|blog|editors|ai-review)(?:\/[A-Za-z0-9_-]+)*$/.test(value)
}
export function parseVisits(value: unknown): Visits {
  const s = value as Visits
  const count = (v: unknown) => typeof v === "number" && Number.isSafeInteger(v) && v>=0
  const stamp = (v: unknown) => typeof v === "string" && Number.isFinite(Date.parse(v))
  const fail = () => { throw new Error("invalid_analytics_report") }
  if (!s || s.schema!=="clawdline.public-analytics.v1" || s.timezone!=="UTC" || ![1,7,30,90].includes(s.days) ||
    ![s.generated_at,s.started_at,s.from,s.until].every(stamp) || Date.parse(s.from)>=Date.parse(s.until) ||
    ![s.views,s.member_views,s.member_accounts,s.active_ms,s.requests,s.members_total,s.page,s.pages_total,s.pages_page].every(count) || s.member_views>s.views || s.member_accounts>s.member_views ||
    !(s.previous_views===null || count(s.previous_views)) || s.pages_page<1 || s.pages_page>VISIT_BOUNDS.page || s.page<1 || s.page>VISIT_BOUNDS.page || s.page_size!==VISIT_BOUNDS.rows || s.retention_days!==90 || typeof s.pages_more!=="boolean") fail()
  for (const [name,bound] of [["pages",20],["sources",9],["campaigns",3],["devices",4],["agents",4],["countries",VISIT_BOUNDS.countries]] as const) {
    const rows = s[name]
    if (!Array.isArray(rows) || rows.length>bound || rows.some(r => !r || typeof r.key!=="string" || r.key.length>VISIT_BOUNDS.path || !count(r.views) || !count(r.active_ms)) || new Set(rows.map(r=>r.key)).size!==rows.length) fail()
  }
  if (s.pages.some(r=>!visitPath(r.key)) || s.sources.some(r=>!["direct","reddit","github","google","bing","hackernews","producthunt","x","other"].includes(r.key)) ||
    s.campaigns.some(r=>!["none","launch","reddit-launch"].includes(r.key)) || s.devices.some(r=>!["desktop","mobile","tablet","unknown"].includes(r.key)) ||
    s.agents.some(r=>!["browser","crawler","ai","unknown"].includes(r.key)) || s.countries.some(r=>r.key!=="unknown"&&!/^[A-Z]{2}$/.test(r.key))) fail()
  if (!Array.isArray(s.daily) || s.daily.length>VISIT_BOUNDS.days || s.daily.some(r=>!r || !/^\d{4}-\d{2}-\d{2}$/.test(r.day) || !stamp(r.day) || !count(r.views) || !count(r.requests) || (r.member_accounts!==undefined && (!count(r.member_accounts) || r.member_accounts>r.views || r.member_accounts>s.member_accounts))) || new Set(s.daily.map(r=>r.day)).size!==s.daily.length) fail()
  if (s.hourly !== undefined) {
    const first = Math.max(Date.parse(s.from), Math.floor(Date.parse(s.started_at)/3600000)*3600000)
    const last = Math.min(Date.parse(s.until)-1, Date.parse(s.generated_at))
    if (!Array.isArray(s.hourly) || s.hourly.length>VISIT_BOUNDS.hours || s.hourly.some((r,i) =>
      !r || !/^\d{4}-\d{2}-\d{2}T\d{2}:00:00Z$/.test(r.hour) || !stamp(r.hour) ||
      new Date(r.hour).toISOString().slice(0,19)+"Z"!==r.hour || Date.parse(r.hour)<first || Date.parse(r.hour)>last ||
      (i>0 && Date.parse(r.hour)-Date.parse(s.hourly![i-1].hour)!==3600000) ||
      ![r.views,r.requests,r.member_accounts,r.active_ms].every(count) || r.member_accounts>r.views || r.active_ms>r.views*VISIT_BOUNDS.activeMs)) fail()
    if (s.hourly.length!==Math.max(0,Math.floor(last/3600000)-Math.floor(first/3600000)+1) || (s.hourly.length>0 && Date.parse(s.hourly[0].hour)!==first)) fail()
    if (s.hourly.reduce((n,r)=>n+r.views,0)!==s.views || s.hourly.reduce((n,r)=>n+r.requests,0)!==s.requests || s.hourly.reduce((n,r)=>n+r.active_ms,0)!==s.active_ms) fail()
  }
  if (s.member_days !== undefined) {
    const m = s.member_days
    if (!m || m.timezone!=="Asia/Taipei" || !Array.isArray(m.rows) || m.rows.length>VISIT_BOUNDS.days+1 ||
      m.rows.some((r,i)=>!r || !/^\d{4}-\d{2}-\d{2}$/.test(r.day) || !stamp(r.day) || !count(r.member_accounts) || r.member_accounts>s.member_accounts ||
        (i>0 && Date.parse(r.day)-Date.parse(m.rows[i-1].day)!==86400000))) fail()
  }
  if (!Array.isArray(s.members) || s.members.length>VISIT_BOUNDS.rows || s.members.length>s.members_total || s.members.some(r=>!r || typeof r.account_id!=="string" || !r.account_id || r.account_id.length>200 || !(r.account_name===null || typeof r.account_name==="string"&&r.account_name.length<=200) || !visitPath(r.path) || !stamp(r.at) || !count(r.active_ms) || r.active_ms>VISIT_BOUNDS.activeMs)) fail()
  return s
}
export async function readVisits(get: typeof fetch, origin: string, days: number, page: number, signal: AbortSignal, pagesPage = 1): Promise<Visits> {
  let response: Response
  try { response = await get(`${origin}/su/v1/analytics/overview?days=${days}&page=${page}&pages_page=${pagesPage}`,{credentials:"include",cache:"no-store",signal}) }
  catch { throw new Error(signal.aborted ? "analytics_timeout" : "analytics_network_error") }
  if (!response.ok) {
    let code = response.status===404 ? "analytics_not_available" : `http_${response.status}`
    try {
      const refusal = await response.json()
      if (response.status!==404 && typeof refusal?.error?.code==="string" && /^[a-z][a-z0-9_]{0,63}$/.test(refusal.error.code)) code=refusal.error.code
    } catch { /* Preserve the HTTP status when the refusal body cannot be read. */ }
    throw new Error(code)
  }
  let value: unknown
  try { value = await response.json() } catch { throw new Error("invalid_analytics_report") }
  const data = parseVisits(value)
  if (data.days!==days || data.page!==page || data.pages_page!==pagesPage) throw new Error("analytics_query_mismatch")
  return data
}
export function demoVisits(days = 30, page = 1, now = new Date()): Visits {
  const until=new Date(Date.UTC(now.getUTCFullYear(),now.getUTCMonth(),now.getUTCDate()+1)), from=new Date(+until-days*86400000)
  const hourly=Array.from({length:Math.floor((+now-+from)/3600000)+1},(_,i)=>{
    const views=1+(i*13)%5
    return {hour:new Date(+from+i*3600000).toISOString().slice(0,19)+"Z",views,requests:2+(i*17)%9,member_accounts:i%3===0?1:0,active_ms:views*(15000+(i%6)*7000)}
  })
  const daily=Array.from({length:days},(_,i)=>{
    const day=new Date(+from+i*86400000).toISOString().slice(0,10), rows=hourly.filter(h=>h.hour.startsWith(day))
    return {day,views:rows.reduce((n,r)=>n+r.views,0),requests:rows.reduce((n,r)=>n+r.requests,0),member_accounts:Math.min(rows.reduce((n,r)=>n+r.member_accounts,0),3)}
  })
  const views=hourly.reduce((n,r)=>n+r.views,0), requests=hourly.reduce((n,r)=>n+r.requests,0), active_ms=hourly.reduce((n,r)=>n+r.active_ms,0)
  return {schema:"clawdline.public-analytics.v1",generated_at:now.toISOString(),started_at:new Date(+from-90*86400000).toISOString(),timezone:"UTC",days,from:from.toISOString(),until:until.toISOString(),
    views,member_views:32,member_accounts:8,active_ms,requests,previous_views:days===90?null:Math.floor(views*.8),daily,hourly,
    pages:[{key:"/",views:Math.floor(views*.4),active_ms:views*12000},{key:"/docs/install",views:Math.floor(views*.35),active_ms:views*20000},{key:"/docs/first-session",views:views-Math.floor(views*.4)-Math.floor(views*.35),active_ms:views*10000}],pages_more:false,pages_total:3,pages_page:1,
    sources:[{key:"reddit",views:Math.floor(views*.6),active_ms:views*21000},{key:"direct",views:views-Math.floor(views*.6),active_ms:views*21000}],campaigns:[{key:"reddit-launch",views,active_ms:views*42000}],devices:[{key:"desktop",views,active_ms:views*42000}],agents:[{key:"browser",views:Math.floor(requests*.8),active_ms:0},{key:"ai",views:requests-Math.floor(requests*.8),active_ms:0}],countries:[{key:"TW",views:requests,active_ms:0}],
    member_days:{timezone:"Asia/Taipei",rows:daily.map((d,i)=>({day:d.day,member_accounts:i%4===0?2:i%3===0?0:1}))},
    members_total:32,page,page_size:20,retention_days:90,members:page===1?[{account_id:"example-account",account_name:"Example member",path:"/docs/install",at:now.toISOString(),active_ms:54000}]:[] }
}

export type VisitMetric = "views" | "requests" | "member_accounts" | "duration"
export interface VisitPoint { at: string; value: number | null }
export function visitHourlyValues(hours: VisitHour[], metric: VisitMetric): VisitPoint[] {
  return hours.map(h=>({at:h.hour,value:metric==="duration"?(h.views?h.active_ms/h.views/1000:null):h[metric]}))
}
export type VisitZone = "UTC" | "Asia/Taipei"
/** Accounts deduplicated per Taipei day, else per UTC day from an earlier API, or null so the caller keeps hourly rows. */
export function visitDailyAccounts(data: Visits): { zone: VisitZone; points: VisitPoint[] } | null {
  if (data.days===1) return null
  if (data.member_days?.rows.length) return { zone:"Asia/Taipei", points:data.member_days.rows.map(d=>({at:d.day+"T00:00:00+08:00",value:d.member_accounts})) }
  if (!data.daily.length || data.daily.some(d=>d.member_accounts===undefined)) return null
  return { zone:"UTC", points:data.daily.map(d=>({at:d.day+"T00:00:00Z",value:d.member_accounts!})) }
}
