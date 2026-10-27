import { SupportNotifications } from "./notifications.js"
import { useSupportImages, SupportImagePicker, SupportImageGallery } from "./images.js"
import { richText } from "../legacy/js/view/markdown.js"
import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent, type MouseEvent, type ReactNode } from "react"
import { currentCatalogTag } from "../catalog.js"
import { chooseTransport, cloudViewerDeviceMetadata, newCloudSession, readCloudConfig, type CloudConfig } from "../cloud/copied.js"
import { bootAdminCatalog } from "../cloud/admin/words.js"
import { BRAND_MARK } from "../brand-mark.js"
import { DeviceLimitPanel } from "../cloud/DeviceLimitPanel.js"
import { supportCall, SupportFailure, requestKey, SUPPORT_UI_LIMITS } from "./api.js"
import { supportWord as w, supportError, type SupportWord } from "./words.js"
import { LANGUAGE_NAMES, SUPPORT_LOCALES, TOPICS, helpRoute, helpAccess, translationState,
  type SupportLocale, type Question, type Faq, type Article, type Sources, type HelpRoute } from "./model.js"
import "./support.css"

type Call = <T>(path: string, method?: string, body?: unknown, key?: string) => Promise<T>
type Navigation = (path: string) => void
function useRead<T>(path: string | null, call: Call) {
  const [state, setState] = useState<{ key: string | null; data: T | null; error: string | null; loading: boolean }>({ key: null, data: null, error: null, loading: false })
  const [revision, refresh] = useState(0)
  useEffect(() => {
    if (!path) return
    let active = true
    setState({ key: path, data: null, error: null, loading: true })
    void call<T>(path).then(data => { if (active) setState({ key: path, data, error: null, loading: false }) }, e => {
      if (active) setState({ key: path, data: null, error: e instanceof SupportFailure ? e.code : "support_unavailable", loading: false })
    })
    return () => { active = false }
  }, [path, call, revision])
  return { data: state.key === path ? state.data : null, error: state.key === path ? state.error : null,
    loading: !!path && (state.key !== path || state.loading), refresh: () => refresh(n => n + 1),
    replace: (data: T) => setState(s => s.key === path ? { ...s, data, error: null, loading: false } : s) }
}
function useMutation(call: Call, scope: string) {
  const storageKey = "clawdline-support-pending:" + scope
  const pending = useRef<{ path: string; method: string; body: unknown; key: string } | null>((() => {
    try { const v = JSON.parse(sessionStorage.getItem(storageKey) ?? "null"); return v && typeof v.path === "string" && v.path.startsWith("/") && typeof v.key === "string" && ["POST", "PUT"].includes(v.method) ? v : null } catch { return null }
  })())
  const mounted = useRef(true)
  const [busy, setBusy] = useState(false), [error, setError] = useState<string | null>(null), [uncertain, setUncertain] = useState(!!pending.current)
  useEffect(() => { mounted.current = true; return () => { mounted.current = false } }, [])
  const run = async <T,>(path: string, body: unknown, done: (answer: T) => void, method = "POST") => {
    if (busy) return
    const input = pending.current ?? { path, body, method, key: requestKey() }; pending.current = input
    try { sessionStorage.setItem(storageKey, JSON.stringify(input)) } catch {}
    setBusy(true); setError(null)
    try {
      const answer = await call<T>(input.path, input.method, input.body, input.key)
      pending.current = null
      try { sessionStorage.removeItem(storageKey) } catch {}
      if (mounted.current) { setUncertain(false); done(answer) }
    } catch (e) {
      const known = e instanceof SupportFailure && e.definite
      if (known) { pending.current = null; try { sessionStorage.removeItem(storageKey) } catch {} }
      if (mounted.current) { setUncertain(!known); setError(!known ? "support_outcome_unknown" : e instanceof SupportFailure ? e.code : "support_unavailable") }
    } finally { if (mounted.current) setBusy(false) }
  }
  return { run, busy, error, uncertain, locked: busy || uncertain }
}
function ErrorBox({ code, retry }: { code: string | null; retry?: () => void }) {
  return code ? <div className="help-error" role="alert"><p>{supportError(code)}</p>{retry ? <button className="help-btn" onClick={retry}>{w("retry")}</button> : null}</div> : null
}
function State({ value, quiet = false }: { value: string; quiet?: boolean }) { return <span className={quiet ? "help-thread-state" : `help-badge help-state-${value}`} data-state={value} role={quiet ? "status" : undefined}>{w(["queued", "answering", "needs_information", "needs_human", "answered", "resolved"].includes(value) ? value as SupportWord : "unavailable")}</span> }
function SourceList({ sources }: { sources: Sources }) { return sources.length ? <div className="help-sources"><small>{w("sources")}</small>{sources.map(s => <a key={s.url + s.revision} href={s.url} target="_blank" rel="noopener noreferrer">↗ {s.revision}</a>)}</div> : null }
function Link({ to, go, children, primary = false, current = false }: { to: string; go: Navigation; children: ReactNode; primary?: boolean; current?: boolean }) {
  return <a className={`help-btn ${primary ? "help-primary" : ""}`} href={to} aria-current={current ? "page" : undefined} onClick={(e: MouseEvent) => { if (!e.metaKey && !e.ctrlKey && !e.shiftKey && e.button === 0) { e.preventDefault(); go(to) } }}>{children}</a>
}
function SignIn({ url }: { url: string }) { return <div className="help-card"><h2>{w("signInNeeded")}</h2><a className="help-btn help-primary" href={url}>{w("signIn")}</a></div> }
export function HelpCenter({ declared }: { declared: string }) {
  const config = useMemo<CloudConfig | null>(() => {
    // Explicit local integration harness; production builds remove this branch.
    if (import.meta.env.DEV && import.meta.env.VITE_SUPPORT_FIXTURE_API && location.hostname === "127.0.0.1") {
      const api = new URL(String(import.meta.env.VITE_SUPPORT_FIXTURE_API))
      if (api.protocol === "http:" && api.hostname === "127.0.0.1") return { appOrigin: location.origin, apiOrigin: api.origin, relayURL: "wss://support-fixture.invalid", build: "", strings: {} }
    }
    try { const c = readCloudConfig({ __clawdlineCloud: JSON.parse(declared) }); return c && chooseTransport({ mock: false, origin: location.origin, config: c }) === "cloud" ? c : null } catch { return null } }, [declared])
  const [catalogReady, setCatalogReady] = useState(false), [, languageChanged] = useState(0)
  const [address, setAddress] = useState(location.pathname + location.search)
  const dirty = useRef(false)
  const [account, setAccount] = useState<{ id: string; operations: boolean } | null>(null), [sessionError, setSessionError] = useState<string | null>(null)
  const [sessionState, setSessionState] = useState<"checking" | "signed_out" | "ready" | "limit" | "unavailable">("checking")
  const [limit, setLimit] = useState<{tier:string;limit:number|null}>({tier:"",limit:null})
  const session = useMemo(() => config ? newCloudSession({ config, deviceKind: cloudViewerDeviceMetadata(window).kind,
    deviceName: cloudViewerDeviceMetadata(window).name, returnTo: config.appOrigin + location.pathname + location.search,
    onOperationsAccess: (allowed: boolean) => setAccount(a => a ? { ...a, operations: allowed } : a) }) : null, [config])
  const started = useRef<Promise<void> | null>(null)
  const start = useCallback(() => {
    if (!session || started.current) return
    setSessionError(null); setSessionState("checking")
    started.current = (async () => {
      try {
        const result = await session.ensureSession()
        if (result.state === "ready") {
          const identity = await supportCall<{ account_id: string; operations_access: boolean }>(config!.apiOrigin, "/v1/auth/session")
          setAccount({ id: result.accountID, operations: identity.operations_access === true }); setSessionState("ready")
        } else if (result.state === "device_limit_reached") { setLimit({ tier: result.tier, limit: result.limit }); setSessionState("limit") }
        else setSessionState("signed_out")
      } catch (error) { setSessionError(error instanceof SupportFailure ? error.code : "support_outcome_unknown"); setSessionState("unavailable") }
      finally { started.current = null }
    })()
  }, [session, config])
  useEffect(() => { void bootAdminCatalog(config, document.baseURI).then(() => { setCatalogReady(true); languageChanged(x => x + 1); document.documentElement.classList.remove("booting") }); start() }, [config, start])
  useEffect(() => { const changed = () => setAddress(location.pathname + location.search); window.addEventListener("popstate", changed); return () => window.removeEventListener("popstate", changed) }, [])
  const go = useCallback((path: string) => {
    if (dirty.current && ["editor", "thread", "operator"].includes(helpRoute(location.pathname).page) && !window.confirm(w("leaveDraft"))) return
    dirty.current = false; history.pushState(null, "", path); setAddress(path)
  }, [])
  useEffect(() => {
    const root = document.querySelector<HTMLElement>(".help-root"), main = document.querySelector<HTMLElement>(".help-wrap")
    if (root) root.scrollTop = 0
    main?.focus({ preventScroll: true })
  }, [address, catalogReady])
  const route = helpRoute(address.split("?")[0]!), locale = currentCatalogTag()
  const call = useCallback<Call>((path, method, body, key) => supportCall(config?.apiOrigin ?? "", path, method, body, key), [config])
  const signInUrl = config ? config.apiOrigin + "/v1/auth/oauth/start?return_to=" + encodeURIComponent(config.appOrigin + address) : "#"
  const href = (path: string) => path
  const access = helpAccess(route), protectedPage = access !== "public"
  if (!catalogReady) return null
  if (!config) return <main className="help-root"><ErrorBox code="support_unavailable" /></main>
  const content = protectedPage && sessionState === "checking" ? <p role="status">{w("loading")}</p> : protectedPage && sessionState === "unavailable" ? null : protectedPage && !account ? sessionState === "limit" ? <><p>{w("deviceLimit")}</p><DeviceLimitPanel session={session} tier={limit.tier} limit={limit.limit} onContinue={start} signInLabel={w("signIn")} /></> : <SignIn url={signInUrl} /> : access === "admin" && !account?.operations ? <ErrorBox code="support_admin_required" /> :
        <HelpBody key={route.page + ":" + (route.id ?? "") + ":" + locale} route={route} locale={locale} go={go} href={href} call={call} account={account} onSaved={() => { dirty.current = false }} apiOrigin={config.apiOrigin} />
  return <div className="help-root">
    <header className="help-header"><a className="help-brand" href="/"><svg viewBox="0 0 8 4" width="25" height="20" aria-hidden="true">{BRAND_MARK.cells.flatMap((row,y)=>row.map((fill,x)=><rect key={x+":"+y} x={x} y={y} width="1" height="1" fill={fill??"transparent"}/>))}</svg>clawdline <small>cloud</small></a><nav><a href="/">{w("workspace")}</a></nav></header>
    <main className="help-wrap" tabIndex={-1} onChangeCapture={e => { if ((e.target as Element).closest("form")) dirty.current = true }}>{import.meta.env.DEV && import.meta.env.VITE_SUPPORT_FIXTURE_API ? <div className="help-notice">{w("fixture")}</div> : null}<div className="help-crumb">Clawdline Cloud / {w("nav")}</div>
      <nav className="help-tabs" aria-label={w("nav")}>
        <Link to={href("/help")} go={go} current={route.page === "faq" || route.page === "article"}>{w("faq")}</Link><Link to={href("/help/questions")} go={go} current={["mine", "ask", "thread"].includes(route.page)}>{w("mine")}</Link>
      </nav>
      {sessionError ? <div className="help-notice">{w("sessionFailed")} <span>{supportError(sessionError)}</span> <button onClick={start}>{w("retry")}</button></div> : null}
      {account?.operations && access === "admin" ? <section className="help-admin-panel" aria-labelledby="help-admin-label">
        <div className="help-admin-caption" id="help-admin-label"><svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" aria-hidden="true"><rect x="5" y="10" width="14" height="11" rx="2"/><path d="M8 10V7a4 4 0 0 1 8 0v3"/></svg>{w("adminArea")}</div>
        <nav className="help-admin-tabs" aria-label={w("adminNavigation")}>
          <Link to={href("/help/admin")} go={go} current={route.page === "inbox" || route.page === "operator"}>{w("inbox")}</Link>
          <Link to={href("/help/admin/faq")} go={go} current={route.page === "drafts" || route.page === "editor"}>{w("drafts")}</Link>
          <Link to={href("/help/admin/worker")} go={go} current={route.page === "worker"}>{w("worker")}</Link>
        </nav>
        {content}
      </section> : <>{account?.operations ? <div className="help-admin-entry"><Link to={href("/help/admin")} go={go}>{w("adminArea")} <span aria-hidden="true">→</span></Link></div> : null}{content}</>}

    </main>
  </div>
}
type BodyProps = {apiOrigin:string;route:HelpRoute; locale:SupportLocale; go:Navigation; href:(p:string)=>string; call:Call; account:{id:string;operations:boolean}|null;onSaved:()=>void}
function HelpBody(p: BodyProps) {
  if (p.route.page === "faq") return <FaqList {...p} />
  if (p.route.page === "article") return <FaqDetail {...p} />
  if (p.route.page === "ask") return <Ask {...p} />
  if (p.route.page === "mine" || p.route.page === "inbox") return <QuestionList {...p} />
  if (p.route.page === "thread" || p.route.page === "operator") return <Thread {...p} />
  if (p.route.page === "drafts") return <DraftList {...p} />
  if (p.route.page === "editor") return <Editor {...p} />
  return <Worker {...p} />
}
function CheckIcon() { return <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" aria-hidden="true" focusable="false"><path d="m5 12 4 4L19 6"/></svg> }
function AskIcon() { return <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.75" aria-hidden="true" focusable="false"><path d="M12 5v14M5 12h14"/></svg> }
function AskAside({go,href}:Pick<BodyProps,"go"|"href">) { return <aside><div className="help-card"><div className="help-eyebrow">{w("nav")}</div><h2>{w("noAnswer")}</h2><p>{w("askCopy")}</p><Link to={href("/help/ask")} go={go} primary><AskIcon/>{w("ask")}</Link></div></aside> }
function FaqList(p:BodyProps) {
  const [search,setSearch]=useState(""),[query,setQuery]=useState(""),[topic,setTopic]=useState("all"),[after,setAfter]=useState<string|null>(null)
  const path="/v1/faq?locale="+p.locale+"&q="+encodeURIComponent(query)+(topic==="all"?"":"&topic="+topic)+(after?"&after="+encodeURIComponent(after):""), read=useRead<{items:Faq[];next_cursor:string|null}>(path,p.call)
  const items=read.data?.items??[]
  return <><div className="help-hero"><div><div className="help-eyebrow">{w("eyebrow")}</div><h1>{w("title")}</h1><p>{w("intro")}</p></div><div className="help-bubble" aria-hidden="true">···</div></div><div className="help-columns"><section>
    <form className="help-search" onSubmit={e=>{e.preventDefault();setQuery(search);setAfter(null);if(search===query&&!after)read.refresh()}}><label htmlFor="support-faq-search">{w("search")}</label><div className="help-search-row"><input id="support-faq-search" type="search" placeholder={w("searchHint")} value={search} onChange={e=>setSearch(e.target.value)} /><button className="help-btn" type="submit">{w("searchGo")}</button></div></form>
    <div className="help-chips">{["all",...TOPICS].map(t=><button className={topic===t?"selected":""} key={t} onClick={()=>{setTopic(t);setAfter(null)}}>{w(t==="all"?"allTopics":t as SupportWord)}</button>)}</div>
    {read.loading?<p role="status">{w("loading")}</p>:null}<ErrorBox code={read.error} retry={read.refresh}/>
    {read.data&&!items.length?<div className="help-empty"><p>{query?w("noSearch"):w("emptyFaq")}</p></div>:null}
    {items.map(r=><a className="help-question-row" key={r.id} href={p.href("/help/faq/"+r.id)} onClick={e=>{e.preventDefault();p.go(p.href("/help/faq/"+r.id))}}><div><h3>{r.title}</h3><small>{w(TOPICS.includes(r.topic as typeof TOPICS[number])?r.topic as SupportWord:"other")} · {new Date(r.updated_at).toLocaleDateString()}</small></div><span>›</span></a>)}
    {read.data?.next_cursor?<button className="help-btn" disabled={read.loading} onClick={()=>setAfter(read.data?.next_cursor??null)}>{w("more")}</button>:null}
  </section><AskAside {...p}/></div></>
}
function FaqDetail(p:BodyProps) {
  const read=useRead<{article:Faq}>("/v1/faq/"+p.route.id+"?locale="+p.locale,p.call),a=read.data?.article
  return <><Link to={p.href("/help")} go={p.go}>{w("cancel")}</Link><ErrorBox code={read.error} retry={read.refresh}/>{read.loading?<p>{w("loading")}</p>:null}{a?<div className="help-columns"><article className="help-article"><div className="help-eyebrow">{LANGUAGE_NAMES[a.locale]} · {w("faq")}</div><h1>{a.title}</h1><small>{w("updated")} {new Date(a.updated_at).toLocaleDateString()}</small><p className="help-prose">{a.body}</p><SourceList sources={a.sources}/></article><AskAside {...p}/></div>:null}</>
}
function Ask(p:BodyProps) {
  const draftKey="clawdline-support-draft:"+p.account!.id
  const [draft]=useState(()=>{try{return JSON.parse(sessionStorage.getItem(draftKey)??sessionStorage.getItem("clawdline-support-draft:anonymous")??"null") as {body:string;topic:string}|null}catch{return null}})
  const [body,setBody]=useState(draft?.body??""),[topic,setTopic]=useState(draft?.topic??"cloud")
  const images=useSupportImages(p.call,p.apiOrigin,p.account!.id+":ask")
  const mutation=useMutation(p.call, (p.account?.id ?? "anonymous") + ":" + p.route.page + ":" + (p.route.id ?? "") + ":" + p.locale)
  useEffect(()=>{try{sessionStorage.setItem(draftKey,JSON.stringify({body,topic}))}catch{}},[draftKey,body,topic])
  const submit=async(e:FormEvent)=>{
    e.preventDefault()
    try{
      const image_ids=await images.upload()
      await mutation.run<{question:Question}>("/v1/support/questions",{body,topic,image_ids,source_language:p.locale,reply_locale:p.locale},a=>{try{sessionStorage.removeItem(draftKey);sessionStorage.removeItem("clawdline-support-draft:anonymous")}catch{}images.clear();p.go(p.href("/help/questions/"+a.question.id))})
    }catch{/* The image picker displays its actionable error and preserves the draft. */}
  }
  return <><div className="help-title"><h1>{w("newQuestion")}</h1></div><p className="help-question-note"><strong>{w("privacy")}</strong></p><div className="help-composer"><form className="help-card" onSubmit={submit}><fieldset disabled={mutation.locked||images.busy}>
    <label>{w("questionBody")}<textarea required={!images.items.length} rows={7} value={body} onPaste={images.paste} onChange={e=>setBody(e.target.value)}/></label>
    <SupportImagePicker images={images} disabled={mutation.locked}/>
    <div className="help-form-pair"><label>{w("topic")}<select value={topic} onChange={e=>setTopic(e.target.value)}>{TOPICS.map(t=><option key={t} value={t}>{w(t)}</option>)}</select></label></div>
    <div className="help-submission-note"><p><strong>{w("sensitiveContent")}</strong></p></div></fieldset><ErrorBox code={mutation.error}/><div className="help-actions"><Link to={p.href("/help")} go={p.go}>{w("cancel")}</Link><button className="help-btn help-primary" disabled={mutation.busy||images.busy||(!mutation.uncertain&&!body.trim()&&!images.items.length)} type="submit">{mutation.busy?w("busy"):images.busy?w("imageWorking"):mutation.uncertain?w("retry"):w("send")}</button></div></form></div></>
}
function QuestionList(p:BodyProps) {
  const admin=p.route.page==="inbox",[locale,setLocale]=useState(""),[state,setState]=useState(""),[extra,setExtra]=useState<Question[]>([]),[next,setNext]=useState<string|null>(null),[moreError,setMoreError]=useState<string|null>(null)
  const path=(admin?"/su":"")+"/v1/support/questions?"+new URLSearchParams({...locale?{locale}:{},...state?{state}:{}}).toString(),read=useRead<{questions:Question[];next_cursor:string|null}>(path,p.call)
  useEffect(()=>{setExtra([]);setNext(null)},[path])
  useEffect(()=>{const refresh=()=>{if(!document.hidden)read.refresh()};window.addEventListener("focus",refresh);document.addEventListener("visibilitychange",refresh);return()=>{window.removeEventListener("focus",refresh);document.removeEventListener("visibilitychange",refresh)}},[path])
  const more=async()=>{try{const a=await p.call<{questions:Question[];next_cursor:string|null}>(path+"&cursor="+encodeURIComponent(next??read.data?.next_cursor??""));read.replace(a);setExtra([]);setNext(null)}catch(e){setMoreError(e instanceof SupportFailure?e.code:"support_unavailable")}}
  return <><div className="help-title"><h1>{w(admin?"inbox":"mine")}</h1>{!admin?<Link to={p.href("/help/ask")} go={p.go} primary><AskIcon/>{w("ask")}</Link>:null}</div>
    {admin?<SupportNotifications call={p.call} locale={p.locale}/>:null}
    {admin?<div className="help-filters"><select aria-label={w("sourceLanguage")} value={locale} onChange={e=>setLocale(e.target.value)}><option value="">{w("allLanguages")}</option>{SUPPORT_LOCALES.map(l=><option key={l} value={l}>{LANGUAGE_NAMES[l]}</option>)}</select><select aria-label={w("allStates")} value={state} onChange={e=>setState(e.target.value)}><option value="">{w("allStates")}</option>{["queued","answering","needs_information","needs_human","answered","resolved"].map(s=><option key={s} value={s}>{w(s as SupportWord)}</option>)}</select></div>:null}
    <ErrorBox code={read.error??moreError} retry={read.refresh}/>{read.loading?<p role="status">{w("loading")}</p>:null}
    {read.data&&!read.data.questions.length?<p className="help-empty">{w(admin?"noneInbox":"noneMine")}</p>:null}
    {[...(read.data?.questions??[]),...extra].map(q=><a className="help-question-row" key={q.id} href={p.href((admin?"/help/admin/questions/":"/help/questions/")+q.id)} onClick={e=>{e.preventDefault();p.go(p.href((admin?"/help/admin/questions/":"/help/questions/")+q.id))}}><div><h3>{q.title} {q.unread?<span className="help-unread" aria-label={w("unread")}>●</span>:null}</h3><small>{admin?<>{q.reporter?.name??w("reporter")} · {q.source_language} · </>:null}{new Date(q.updated_at).toLocaleString()}</small></div><State value={q.state}/></a>)}
    {(next??(extra.length?null:read.data?.next_cursor))?<button className="help-btn" onClick={()=>void more()}>{w("more")}</button>:null}
  </>
}
function Thread(p:BodyProps) {
  const admin=p.route.page==="operator",path=(admin?"/su":"")+"/v1/support/questions/"+p.route.id,read=useRead<{question:Question}>(path,p.call),q=read.data?.question
  const [body,setBody]=useState(""),[url,setUrl]=useState(""),[version,setVersion]=useState(""),mutation=useMutation(p.call, (p.account?.id ?? "anonymous") + ":" + p.route.page + ":" + (p.route.id ?? "") + ":" + p.locale)
  const images=useSupportImages(p.call,p.apiOrigin,p.account!.id+":"+p.route.id)
  const stateMutation=useMutation(p.call,p.account!.id+":"+p.route.page+":"+p.route.id+":state:"+p.locale)
  useEffect(()=>{const refresh=()=>{if(!document.hidden&&!mutation.locked&&!stateMutation.locked)read.refresh()};const timer=setInterval(refresh,SUPPORT_UI_LIMITS.refreshMs);window.addEventListener("focus",refresh);return()=>{clearInterval(timer);window.removeEventListener("focus",refresh)}},[mutation.locked,stateMutation.locked])
  useEffect(()=>{if(!admin&&q?.unread&&q.messages?.length){let active=true;void p.call<{question:Question}>(path+"/read","POST",{message_id:q.messages.at(-1)!.id},requestKey()).then(a=>{if(active)read.replace(a)},()=>{});return()=>{active=false}}},[q?.messages?.at(-1)?.id,admin])
  const submit=async(e:FormEvent)=>{e.preventDefault();if(!q)return;try{const image_ids=admin?[]:await images.upload();await mutation.run<{question:Question}>(admin?path:path+"/messages",admin?{action:"reply",expected_revision:q.revision,body,locale:q.reply_locale,sources:[{url,revision:version}]}:{expected_revision:q.revision,body,image_ids,source_language:p.locale,reply_locale:p.locale},a=>{p.onSaved();read.replace(a);setBody("");images.clear()})}catch{/* Image errors retain the follow-up. */}}
  const action=(type:"retry"|"resolve")=>{if(!q)return;void stateMutation.run<{question:Question}>(type==="retry"?path:path+"/resolve",{expected_revision:q.revision,...type==="retry"?{action:"retry"}:{}},read.replace)}
  return <><ErrorBox code={read.error} retry={read.refresh}/>{read.loading&&!q?<p>{w("loading")}</p>:null}{q?<><header className="help-thread-heading"><div className="help-thread-meta"><State value={q.state} quiet/></div><h1>{q.title}</h1></header><div className={admin?"help-columns":"help-thread-content"}><section>{q.messages?.map(m=><article className={"help-message "+(m.kind==="member"?"help-card":"")} key={m.id}><div className="help-message-meta"><strong>{m.kind==="member"&&admin?(q.reporter?.name??w("reporter")):w(m.kind==="member"?"you":m.kind==="agent"?"agent":"operator")}</strong><small>{new Date(m.at).toLocaleString()}</small></div>{m.kind==="member"?<p className="help-prose" lang={m.original_language}>{m.body}</p>:<div className="help-prose help-markdown" lang={m.original_language} dangerouslySetInnerHTML={{__html:richText(m.body,{codeCopy:false})}}/>}<SupportImageGallery images={m.images??[]} origin={p.apiOrigin} question={q.id} admin={admin}/><SourceList sources={m.sources}/></article>)}
      <form className="help-reply" onSubmit={submit}><fieldset disabled={mutation.locked||stateMutation.locked||images.busy}><label>{w(admin?"operatorReply":"followup")}<textarea required={admin||!images.items.length} onPaste={admin?undefined:images.paste} rows={4} value={body} onChange={e=>setBody(e.target.value)}/></label>{admin?<SourceFields url={url} version={version} setUrl={setUrl} setVersion={setVersion}/>:<SupportImagePicker images={images} disabled={mutation.locked||stateMutation.locked}/>}</fieldset><ErrorBox code={mutation.error??stateMutation.error}/><div className="help-actions help-reply-actions">{!admin&&q.state==="resolved"?<span className="help-resolved" role="status"><CheckIcon/>{w("resolved")}</span>:<button className={admin?"help-btn":"help-resolve"} type="button" disabled={mutation.locked||stateMutation.busy||images.busy} onClick={()=>action(admin?"retry":"resolve")}>{!admin?<CheckIcon/>:null}{stateMutation.busy?w("busy"):stateMutation.uncertain?w("retry"):w(admin?"retryJob":"resolve")}</button>}<button className="help-btn help-primary" disabled={mutation.busy||stateMutation.locked||images.busy||(!mutation.uncertain&&!body.trim()&&(admin||!images.items.length))}>{mutation.busy?w("busy"):images.busy?w("imageWorking"):mutation.uncertain?w("retry"):w(admin?"operatorReply":"sendFollowup")}</button></div></form>{!admin?<p className="help-thread-privacy">{w("threadPrivacy")}</p>:null}</section>{admin?<aside className="help-card"><section className="help-reporter" aria-label={w("reporter")}><small>{w("reporter")}</small><strong>{q.reporter?.name??w("reporterUnnamed")}</strong><small>{w("reporterId")}</small><code>{q.reporter?.id??"—"}</code></section><p className="help-hint">{w("replyLanguage")}: {LANGUAGE_NAMES[q.reply_locale]}<br/>{w("sourceLanguage")}: {q.source_language}</p><dl><dt>{w("delivery")}</dt><dd>{q.delivery?.state??"—"} · {q.delivery?.attempts??"—"}</dd><dt>{w("workState")}</dt><dd>{q.work?.state??"—"} · {q.work?.attempts??"—"}</dd><dt>{w("processingDetail")}</dt><dd>{q.delivery?.error??q.work?.error??"—"}</dd></dl><Link to={p.href("/help/admin/faq/new?source="+q.id)} go={p.go}>{w("createFaq")}</Link></aside>:null}</div></>:null}</>
}
function SourceFields({url,version,setUrl,setVersion}:{url:string;version:string;setUrl:(v:string)=>void;setVersion:(v:string)=>void}) {return <div className="help-form-pair"><label>{w("sourceUrl")}<input required type="url" value={url} onChange={e=>setUrl(e.target.value)}/></label><label>{w("sourceRevision")}<input required value={version} onChange={e=>setVersion(e.target.value)}/></label></div>}
function DraftList(p:BodyProps) {
  const read=useRead<{articles:Article[];next_cursor:string|null}>("/su/v1/support/faq",p.call),[extra,setExtra]=useState<Article[]>([]),[next,setNext]=useState<string|null>(null),[error,setError]=useState<string|null>(null)
  const more=async()=>{try{const a=await p.call<{articles:Article[];next_cursor:string|null}>("/su/v1/support/faq?after="+encodeURIComponent(next??read.data?.next_cursor??""));read.replace(a);setExtra([]);setNext(null)}catch(e){setError(e instanceof SupportFailure?e.code:"support_unavailable")}}
  const mutation=useMutation(p.call, (p.account?.id ?? "anonymous") + ":" + p.route.page + ":" + (p.route.id ?? "") + ":" + p.locale),[notice,setNotice]=useState(false)
  return <><div className="help-title"><h1>{w("allDrafts")}</h1><Link to={p.href("/help/admin/faq/new")} go={p.go} primary>{w("createFaq")}</Link></div><ErrorBox code={read.error??error}/>{read.loading?<p>{w("loading")}</p>:null}{read.data&&!read.data.articles.length?<p>{w("noDrafts")}</p>:null}{[...(read.data?.articles??[]),...extra].map(a=><a key={a._id} className="help-question-row" href={p.href("/help/admin/faq/"+a._id)} onClick={e=>{e.preventDefault();p.go(p.href("/help/admin/faq/"+a._id))}}><div><h3>{a.translations[p.locale]?.draft.title??a.translations[a.source_locale]?.draft.title??a._id}</h3><small>{w(translationState(a,p.locale))} · {LANGUAGE_NAMES[a.source_locale]}</small></div><span>›</span></a>)}
    {(next??(extra.length?null:read.data?.next_cursor))?<button className="help-btn" onClick={()=>void more()}>{w("more")}</button>:null}<div className="help-card"><p>{w("snapshotNote")}</p><ErrorBox code={mutation.error}/>{notice?<p role="status">{w("snapshotDone")}</p>:null}<button className="help-btn" disabled={mutation.busy} onClick={()=>void mutation.run("/su/v1/support/snapshots/"+p.locale,{},()=>setNotice(true))}>{mutation.busy?w("busy"):w("publishSnapshot")}</button></div></>
}
function Editor(p:BodyProps) {
  const isNew=p.route.id==="new",read=useRead<{article:Article}>(isNew?null:"/su/v1/support/faq/"+p.route.id,p.call),article=read.data?.article
  const [id]=useState(()=>isNew?"faq_"+requestKey():p.route.id!),[title,setTitle]=useState(""),[body,setBody]=useState(""),[slug,setSlug]=useState(""),[topic,setTopic]=useState("cloud"),[url,setUrl]=useState(""),[version,setVersion]=useState(""),[privacy,setPrivacy]=useState(false),[evidence,setEvidence]=useState(false),[expected,setExpected]=useState<number|null>(isNew?0:null),[basedOn,setBasedOn]=useState<number|null>(null),[notice,setNotice]=useState(false)
  const mutation=useMutation(p.call, (p.account?.id ?? "anonymous") + ":" + p.route.page + ":" + (p.route.id ?? "") + ":" + p.locale),initialized=useRef(false)
  useEffect(()=>{if(!article||initialized.current)return;initialized.current=true;const t=article.translations[p.locale]?.draft;setTitle(t?.title??"");setBody(t?.body??"");setSlug(t?.slug??"");setTopic(article.topic);setUrl(t?.sources[0]?.url??"");setVersion(t?.sources[0]?.revision??"");setExpected(article.revision);setBasedOn(article.source_revision)},[article,p.locale])
  const save=(e:FormEvent)=>{e.preventDefault();void mutation.run<{article:Article}>(isNew?"/su/v1/support/faq":"/su/v1/support/faq/"+id,{article_id:id,source_question_id:new URLSearchParams(location.search).get("source"),topic,locale:p.locale,expected_revision:expected,based_on_source_revision:basedOn,title,body,slug,sources:[{url,revision:version},...(article?.translations[p.locale]?.draft.sources.slice(1)??[])]},a=>{p.onSaved();setNotice(true);setExpected(a.article.revision);setBasedOn(a.article.source_revision);if(isNew)p.go(p.href("/help/admin/faq/"+a.article._id));else read.replace(a)})}
  const publish=()=>{if(!article)return;void mutation.run<{article:Article}>(`/su/v1/support/faq/${id}/translations/${p.locale}/publish`,{expected_revision:article.revision,privacy_reviewed:privacy,evidence_reviewed:evidence},a=>{p.onSaved();read.replace(a);setExpected(a.article.revision);setNotice(true)})}
  return <><Link to={p.href("/help/admin/faq")} go={p.go}>{w("cancel")}</Link><div className="help-title"><h1>{w("editFaq")}</h1><span className="help-badge">{LANGUAGE_NAMES[p.locale]}</span></div><ErrorBox code={read.error} retry={read.refresh}/>{read.loading?<p>{w("loading")}</p>:null}<div className="help-editor-grid"><form className="help-card" onSubmit={save}><fieldset disabled={mutation.locked||expected===null}><label>{w("faqTitle")}<input required maxLength={SUPPORT_UI_LIMITS.titleChars} value={title} onChange={e=>{setTitle(e.target.value);setNotice(false)}}/></label><label>{w("faqBody")}<textarea required rows={8} value={body} onChange={e=>{setBody(e.target.value);setNotice(false)}}/></label><div className="help-form-pair"><label>{w("slug")}<input required pattern="[a-z0-9]+(-[a-z0-9]+)*" value={slug} onChange={e=>setSlug(e.target.value)}/></label><label>{w("topic")}<select value={topic} onChange={e=>setTopic(e.target.value)}>{TOPICS.map(t=><option key={t} value={t}>{w(t)}</option>)}</select></label></div><SourceFields url={url} version={version} setUrl={setUrl} setVersion={setVersion}/><p className="help-hint">{w("sourceChanged")}</p></fieldset><ErrorBox code={mutation.error}/>{notice?<p role="status">{w("saved")}</p>:null}<button className="help-btn help-primary" disabled={mutation.busy||expected===null}>{mutation.busy?w("busy"):mutation.uncertain?w("retry"):w("save")}</button></form><aside><div className="help-card"><h2>{w("version")}</h2>{SUPPORT_LOCALES.map(l=><div className="help-lang-row" key={l}><span>{LANGUAGE_NAMES[l]}</span><span className="help-badge">{w(article?translationState(article,l):"missing")}</span></div>)}</div><div className="help-card"><p>{w("finishReview")}</p><label className="help-check"><input type="checkbox" checked={privacy} onChange={e=>setPrivacy(e.target.checked)}/>{w("privacyReview")}</label><label className="help-check"><input type="checkbox" checked={evidence} onChange={e=>setEvidence(e.target.checked)}/>{w("evidenceReview")}</label><button className="help-btn help-primary" disabled={!article||!privacy||!evidence||mutation.busy||mutation.uncertain||translationState(article,p.locale)==="stale"} onClick={publish}>{w("publish")}</button></div></aside></div></>
}
function Worker(p:BodyProps) {
  const read=useRead<{worker:{revision:number;hook_id:string;enabled:boolean}|null}>("/su/v1/support/worker",p.call),hooks=useRead<{hooks:{hook_id:string;state:string;machine_id:string}[];next_cursor?:string}>("/v1/schedule-webhooks",p.call)
  const [hook,setHook]=useState(""),[enabled,setEnabled]=useState(false),[credential,setCredential]=useState<string|null>(null),mutation=useMutation(p.call, (p.account?.id ?? "anonymous") + ":" + p.route.page + ":" + (p.route.id ?? "") + ":" + p.locale)
  useEffect(()=>{if(read.data?.worker){setHook(read.data.worker.hook_id);setEnabled(read.data.worker.enabled)}},[read.data])
  const [showCredential,setShowCredential]=useState(false),[copyState,setCopyState]=useState<"idle"|"copying"|"copied"|"failed">("idle")
  const copyCredential=async()=>{
    if(!credential||copyState==="copying")return
    setCopyState("copying")
    try{await navigator.clipboard.writeText(credential);setCopyState("copied")}
    catch{setCopyState("failed")}
  }
  const submit=(e:FormEvent)=>{e.preventDefault();void mutation.run<{worker:{revision:number;hook_id:string;enabled:boolean};credential:string}>("/su/v1/support/worker",{hook_id:hook,enabled,expected_revision:read.data?.worker?.revision??0},a=>{read.replace({worker:a.worker});setCredential(a.credential);setShowCredential(false);setCopyState("idle")},"PUT")}
  return <><h1>{w("worker")}</h1><p>{w("workerNote")}</p><ErrorBox code={read.error??hooks.error}/><form className="help-card" onSubmit={submit}><fieldset disabled={mutation.locked}><label>{w("hook")}<input required value={hook} placeholder="swh_…" onChange={e=>setHook(e.target.value)} list="support-hooks"/><datalist id="support-hooks">{hooks.data?.hooks?.filter(h=>h.state==="active").map(h=><option key={h.hook_id} value={h.hook_id}>{h.machine_id}</option>)}</datalist></label><p className="help-hint">{w("noHook")}</p><label className="help-check"><input type="checkbox" checked={enabled} onChange={e=>setEnabled(e.target.checked)}/>{w("enabled")}</label></fieldset><ErrorBox code={mutation.error}/><button className="help-btn help-primary" disabled={mutation.busy||read.loading}>{mutation.busy?w("busy"):w("workerSave")}</button>{credential?<div className="help-secret"><p>{w("credentialOnce")}</p><label htmlFor="support-worker-credential">{w("credentialLabel")}</label><input id="support-worker-credential" type={showCredential?"text":"password"} readOnly value={credential} autoComplete="off" spellCheck={false}/><div className="help-credential-actions"><button className="help-btn" type="button" aria-controls="support-worker-credential" aria-pressed={showCredential} onClick={()=>setShowCredential(v=>!v)}>{w(showCredential?"hideCredential":"showCredential")}</button><button className="help-btn" type="button" disabled={copyState==="copying"} onClick={()=>void copyCredential()}>{w(copyState==="copying"?"copyingCredential":"copyCredential")}</button></div>{copyState==="copied"?<p role="status">{w("credentialCopied")}</p>:copyState==="failed"?<p role="alert">{w("credentialCopyFailed")}</p>:null}</div>:null}</form></>
}
