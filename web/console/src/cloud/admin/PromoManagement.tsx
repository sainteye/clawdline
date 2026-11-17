import { confirmAction } from "../../confirm-action.js"
import { useCallback, useEffect, useId, useMemo, useRef, useState } from "react"
import type { Overview } from "./api.js"
import { campaignWindow, DailyChart, RouteLink, Tile, TrendTile, utcMinute, type Go } from "./AdminDashboard.js"
import {
  canIssue, checkCampaignDraft, checkCodeLimit, createCampaign, fill, issueCode, listedFromCampaign, listedFromOverview,
  loadCampaigns, loadCodes, newIdempotencyKey, pageWindow, revokeCampaign, revokeCode, NAME_MAX,
  type CampaignDraft, type CampaignPhase, type CodePage, type DraftProblem, type Failure, type ListedCampaign, type PromoCampaign, type Read,
} from "./promo-api.js"
import { adminPath, type AdminWord, type Say } from "./words.js"
import { guardCodeNavigation } from "./navigation-guard.js"

/*
 * `/admin/promotions`: what the codes have done, and the three things an
 * operator does to them — make a campaign, issue a code, revoke a campaign.
 *
 * The list reads the paged campaign route. A server that does not serve it yet
 * answers with the same 404 as no access; the overview's latest campaigns
 * stand in then, and the page says how many of the whole that is.
 *
 * One campaign at a time is opened by `?campaign=<id>`, so Back closes it and a
 * link reopens it. A code's plaintext lives only in this component's state,
 * from the issue that returned it until the operator dismisses it or leaves;
 * never in the address, storage, or a log.
 */

type Fetch = (url: string, init: RequestInit) => Promise<Response>
const send: Fetch = (url, init) => fetch(url, init)

type ListState =
  | { kind: "loading" }
  | { kind: "paged"; offset: number; total: number; rows: ListedCampaign[]; byId: Map<string, PromoCampaign>; refreshing: boolean }
  /** The paged route is not served; the overview's latest rows stand in. */
  | { kind: "fallback"; rows: ListedCampaign[]; total: number }
  | { kind: "failed"; code: string }

type Mutation = { kind: "idle" } | { kind: "busy" } | { kind: "error"; code: string } | { kind: "done"; message: string }

const PHASE_TONE: Record<CampaignPhase, string> = {
  open: "ok",
  not_started: "calm",
  expired: "dim",
  revoked: "bad",
  limit_reached: "warn",
  no_codes: "warn",
  unknown: "dim",
}

/** The sentence for a server's refusal, or the generic one with its code. */
function failureText(code: string, say: Say): string {
  const known: Record<string, AdminWord> = {
    network: "errNetwork",
    bad_campaign_window: "errWindow",
    bad_conflict_policy: "errPolicy",
    extend_not_supported: "errPolicy",
    campaign_unavailable: "errCampaignUnavailable",
    bad_redemption_limit: "errCodeLimit",
    bad_public_code: "errPublicCode",
    public_code_taken: "errPublicCodeTaken",
    idempotency_conflict: "errIdempotency",
    unknown_campaign: "errUnknownCampaign",
    admin_pre_auth_rate_limited: "errRateLimited",
    admin_refusal_rate_limited: "errRateLimited",
    bad_origin: "errOrigin",
    no_access: "errNoAccess",
  }
  const key = known[code]
  return key ? say(key) : say("errGeneric", { code })
}

function campaignFrom(search: string): string | null {
  const id = new URLSearchParams(search).get("campaign")
  return id && id.length <= 200 ? id : null
}

function promoHref(campaign: string | null): string {
  return adminPath("promotions") + (campaign ? "?campaign=" + encodeURIComponent(campaign) : "")
}

function publicCampaignPage(code: string): string {
  if (code === "BUILD-TOGETHER-100") return "https://clawdline.com/friends/"
  if (code === "PRODUCT-HUNT") return "https://clawdline.com/redeem/product-hunt/"
  if (code === "HACKER-NEWS") return "https://clawdline.com/redeem/hacker-news/"
  return `https://clawdline.com/redeem/?lang=en#code=${encodeURIComponent(code)}`
}

export function PromoManagement({ overview, apiOrigin, say, locale, search, go, onChanged, onLostAccess }: {
  overview: Overview
  apiOrigin: string
  say: Say
  locale: string
  search: string
  go: Go
  /** A mutation changed the totals the overview reports; read them again. */
  onChanged: () => void
  /** The API stopped accepting this session as a superuser. */
  onLostAccess: () => void
}) {
  const n = useMemo(() => new Intl.NumberFormat(locale), [locale])
  const { promotions } = overview
  const [list, setList] = useState<ListState>({ kind: "loading" })
  const [creating, setCreating] = useState(false)
  const [notice, setNotice] = useState<string | null>(null)
  const selected = campaignFrom(search)
  const generation = useRef(0)

  const fallbackRows = useMemo(() => promotions.campaigns.rows.map(listedFromOverview), [promotions.campaigns.rows])
  const fallbackRef = useRef({ rows: fallbackRows, total: promotions.campaigns.total })
  fallbackRef.current = { rows: fallbackRows, total: promotions.campaigns.total }

  const readList = useCallback(async (offset: number, keep: boolean) => {
    const mine = ++generation.current
    setList((current) => keep && current.kind === "paged" ? { ...current, refreshing: true } : { kind: "loading" })
    const result = await loadCampaigns(send, apiOrigin, offset)
    if (mine !== generation.current) return
    if (result.kind === "ready") {
      const now = Date.now()
      setList({
        kind: "paged", offset: result.value.offset, total: result.value.total, refreshing: false,
        rows: result.value.rows.map((c) => listedFromCampaign(c, now)),
        byId: new Map(result.value.rows.map((c) => [c.id, c])),
      })
    } else if (result.kind === "no_access") {
      // The overview was just read as a superuser, so a 404 here is most
      // likely a server without the list route; the latest rows stand in.
      setList({ kind: "fallback", ...fallbackRef.current })
    } else {
      setList({ kind: "failed", code: result.code })
    }
  }, [apiOrigin])

  useEffect(() => {
    void readList(0, false)
    return () => { generation.current++ }
  }, [readList])

  // The overview stand-in follows a refreshed overview.
  useEffect(() => {
    setList((current) => current.kind === "fallback" ? { kind: "fallback", rows: fallbackRows, total: promotions.campaigns.total } : current)
  }, [fallbackRows, promotions.campaigns.total])

  const afterChange = useCallback((message: string, open?: string) => {
    setNotice(message)
    onChanged()
    // A new campaign is the newest, so the list goes back to its first page.
    void readList(open === undefined && list.kind === "paged" ? list.offset : 0, true)
    if (open !== undefined) go(promoHref(open))
  }, [go, list, onChanged, readList])

  const listed = list.kind === "paged" || list.kind === "fallback" ? list.rows : []
  const selectedRow = selected ? listed.find((r) => r.id === selected) ?? null : null
  const selectedFull = selected && list.kind === "paged" ? list.byId.get(selected) ?? null : null

  if (selected) {
    return (
      <CampaignDetail
        key={selected}
        id={selected}
        row={selectedRow}
        full={selectedFull}
        apiOrigin={apiOrigin}
        say={say}
        locale={locale}
        n={n}
        go={go}
        notice={notice}
        clearNotice={() => setNotice(null)}
        onRevoked={(name) => afterChange(say("revokeDone", { name }))}
        onIssued={() => { onChanged(); void readList(list.kind === "paged" ? list.offset : 0, true) }}
        onLostAccess={onLostAccess}
      />
    )
  }

  return (
    <div className="adm-page">
      <div className="adm-tiles adm-tiles-4">
        <Tile label={say("redemptionsTotal")} value={n.format(promotions.redemptions_total)} lead />
        <Tile label={say("redeemersTotal")} value={n.format(promotions.redeemers_total)} />
        <TrendTile label={say("redeemed7")} value={promotions.redeemed_7d} say={say} n={n} />
        <TrendTile label={say("redeemed30")} value={promotions.redeemed_30d} say={say} n={n} />
      </div>
      <p className="adm-fine">
        {say("promoInventory", { campaigns: n.format(promotions.campaigns_total), codes: n.format(promotions.codes_total) })}
      </p>
      <DailyChart title={say("chartRedemptions")} days={promotions.daily} say={say} locale={locale} />

      {notice ? <Banner tone="ok" text={notice} onDismiss={() => setNotice(null)} say={say} /> : null}

      <section className="adm-panel adm-campaigns" aria-labelledby="adm-camp-h">
        <div className="adm-panel-head">
          <div>
            <h2 id="adm-camp-h" className="adm-h3">{say("campaignsTitle")}</h2>
            <p className="adm-fine">{say("campaignEligibilityNote")}</p>
          </div>
          <div className="adm-panel-actions">
            {list.kind === "paged" ? (
              <button type="button" className="adm-btn adm-btn-quiet" onClick={() => void readList(list.offset, true)} disabled={list.refreshing} aria-busy={list.refreshing}>
                {list.refreshing ? say("refreshing") : say("refresh")}
              </button>
            ) : null}
            <button type="button" className="adm-btn adm-btn-go" aria-expanded={creating} aria-controls="adm-create" onClick={() => setCreating((v) => !v)}>
              {creating ? say("createClose") : say("createOpen")}
            </button>
          </div>
        </div>

        {creating ? (
          <CreateCampaign
            apiOrigin={apiOrigin}
            say={say}
            onCancel={() => setCreating(false)}
            onCreated={(campaign, replay) => {
              setCreating(false)
              afterChange(say(replay ? "createReplayed" : "createDone", { name: campaign.name }), campaign.id)
            }}
            onLostAccess={onLostAccess}
          />
        ) : null}

        <CampaignList list={list} say={say} n={n} go={go} onPage={(offset) => void readList(offset, true)} onRetry={() => void readList(0, false)} />
      </section>
    </div>
  )
}

function Banner({ tone, text, onDismiss, say }: { tone: "ok" | "bad"; text: string; onDismiss?: () => void; say: Say }) {
  return (
    <div className={"adm-banner adm-banner-" + tone} role={tone === "bad" ? "alert" : "status"}>
      <p>{text}</p>
      {onDismiss ? <button type="button" className="adm-btn adm-btn-quiet adm-btn-small" onClick={onDismiss}>{say("dismiss")}</button> : null}
    </div>
  )
}

function PhaseBadge({ phase, say }: { phase: CampaignPhase; say: Say }) {
  return (
    <span className={"adm-status adm-status-" + PHASE_TONE[phase]}>
      <span className="adm-dot" aria-hidden="true" />{say(`phase_${phase}`)}
    </span>
  )
}

function Pager({ offset, shown, total, onPage, say, n, label }: { offset: number; shown: number; total: number; onPage: (offset: number) => void; say: Say; n: Intl.NumberFormat; label: string }) {
  const w = pageWindow(offset, shown, total)
  if (w.previous === null && w.next === null) {
    return <p className="adm-fine adm-pager-count">{say("pageAll", { total: n.format(total) })}</p>
  }
  return (
    <nav className="adm-pager" aria-label={label}>
      <button type="button" className="adm-btn adm-btn-quiet" disabled={w.previous === null} onClick={() => w.previous !== null && onPage(w.previous)}>
        <span aria-hidden="true">← </span>{say("pagePrevious")}
      </button>
      <p className="adm-pager-count" aria-live="polite">{say("pageRange", { from: n.format(w.from), to: n.format(w.to), total: n.format(w.total) })}</p>
      <button type="button" className="adm-btn adm-btn-quiet" disabled={w.next === null} onClick={() => w.next !== null && onPage(w.next)}>
        {say("pageNext")}<span aria-hidden="true"> →</span>
      </button>
    </nav>
  )
}

function CampaignList({ list, say, n, go, onPage, onRetry }: {
  list: ListState
  say: Say
  n: Intl.NumberFormat
  go: Go
  onPage: (offset: number) => void
  onRetry: () => void
}) {
  if (list.kind === "loading") {
    return <div className="adm-list-loading" role="status"><span className="adm-spinner" aria-hidden="true" />{say("campaignsLoading")}</div>
  }
  if (list.kind === "failed") {
    return (
      <div className="adm-inline-error" role="alert">
        <p>{say("campaignsFailed", { reason: failureText(list.code, say) })}</p>
        <button type="button" className="adm-btn" onClick={onRetry}>{say("tryAgain")}</button>
      </div>
    )
  }
  const rows = list.rows
  return (
    <>
      {list.kind === "fallback" ? (
        <p className="adm-fine adm-capped">
          {list.total > rows.length
            ? say("campaignsFallbackCapped", { shown: n.format(rows.length), total: n.format(list.total) })
            : say("campaignsFallback", { shown: n.format(rows.length) })}
        </p>
      ) : null}
      {rows.length === 0 ? (
        <div className="adm-empty">
          <p className="adm-empty-title">{say("campaignsNone")}</p>
          <p className="adm-fine">{say("campaignsNoneHint")}</p>
        </div>
      ) : (
        <div className="adm-table-wrap">
          <table className="adm-table adm-table-campaigns">
            <thead>
              <tr>
                <th scope="col">{say("campaignName")}</th>
                <th scope="col">{say("campaignStatus")}</th>
                <th scope="col">{say("campaignWindow")}</th>
                <th scope="col">{say("campaignRedeemed")}</th>
                <th scope="col"><span className="adm-visually-hidden">{say("campaignActions")}</span></th>
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => {
                const share = row.redemption_limit === null ? null : fill(row.redeemed_count, row.redemption_limit)
                const name = row.name || say("campaignUnnamed")
                return (
                  <tr key={row.id}>
                    <th scope="row" data-label={say("campaignName")}>
                      <RouteLink href={promoHref(row.id)} go={go} className={row.name ? "adm-cname adm-rowlink" : "adm-cname adm-rowlink adm-unnamed"}>{name}</RouteLink>
                    </th>
                    <td data-label={say("campaignStatus")}><PhaseBadge phase={row.phase} say={say} /></td>
                    <td data-label={say("campaignWindow")} className="adm-when">{campaignWindow(row, say)}</td>
                    <td data-label={say("campaignRedeemed")}>
                      <span className="adm-fillnum">
                        {row.redemption_limit === null
                          ? say("campaignNoLimit", { n: n.format(row.redeemed_count) })
                          : say("campaignOf", { n: n.format(row.redeemed_count), limit: n.format(row.redemption_limit) })}
                      </span>
                      {share !== null ? <span className="adm-meter" aria-hidden="true"><span style={{ width: `${Math.round(share * 100)}%` }} /></span> : null}
                    </td>
                    <td className="adm-row-action">
                      <RouteLink href={promoHref(row.id)} go={go} className="adm-btn adm-btn-quiet adm-btn-small">
                        {say("campaignManage")}<span className="adm-visually-hidden"> {name}</span>
                      </RouteLink>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}
      {list.kind === "paged" ? (
        <Pager offset={list.offset} shown={rows.length} total={list.total} onPage={onPage} say={say} n={n} label={say("campaignsPages")} />
      ) : null}
    </>
  )
}

/* ---- create ------------------------------------------------------------- */

function localInput(ms: number): string {
  const d = new Date(ms)
  const pad = (v: number) => String(v).padStart(2, "0")
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

const PROBLEM_WORD: Record<DraftProblem, AdminWord> = {
  name_missing: "problemNameMissing",
  name_too_long: "problemNameLong",
  start_missing: "problemStartMissing",
  end_missing: "problemEndMissing",
  end_before_start: "problemEndBeforeStart",
  end_in_past: "problemEndPast",
  window_too_long: "problemWindowLong",
  limit_not_whole: "problemLimitWhole",
  limit_too_large: "problemLimitLarge",
}

function useIntentKey() {
  // One key per intent: a retry after a failure that may have reached the
  // server sends the same key, and the server answers it as the same request.
  // Any change to what is asked for is a new intent and needs a new key, or
  // the server refuses it as `idempotency_conflict`.
  const key = useRef<string | null>(null)
  return {
    take: () => (key.current ??= newIdempotencyKey()),
    reset: () => { key.current = null },
  }
}

function CreateCampaign({ apiOrigin, say, onCancel, onCreated, onLostAccess }: {
  apiOrigin: string
  say: Say
  onCancel: () => void
  onCreated: (campaign: PromoCampaign, replay: boolean) => void
  onLostAccess: () => void
}) {
  const id = useId()
  const [draft, setDraft] = useState<CampaignDraft>(() => {
    const now = Date.now()
    return { name: "", notBefore: localInput(now), expiresAt: localInput(now + 30 * 86_400_000), redemptionLimit: "100", durationCalendarMonths: "3" }
  })
  const [showProblems, setShowProblems] = useState(false)
  const [state, setState] = useState<Mutation>({ kind: "idle" })
  const intent = useIntentKey()
  const first = useRef<HTMLInputElement>(null)
  const zone = useMemo(() => Intl.DateTimeFormat().resolvedOptions().timeZone || "local", [])

  useEffect(() => first.current?.focus(), [])

  const checked = checkCampaignDraft(draft, Date.now())
  const problems = showProblems && !checked.ok ? checked.problems : {}
  const edit = (field: keyof CampaignDraft) => (event: React.ChangeEvent<HTMLInputElement>) => {
    intent.reset()
    setDraft((d) => ({ ...d, [field]: event.target.value }))
    if (state.kind === "error") setState({ kind: "idle" })
  }

  const submit = async (event: React.FormEvent) => {
    event.preventDefault()
    if (state.kind === "busy") return
    if (!checked.ok) {
      setShowProblems(true)
      const firstBad = (["name", "notBefore", "expiresAt", "redemptionLimit"] as const).find((f) => f in checked.problems)
      if (firstBad) document.getElementById(id + firstBad)?.focus()
      return
    }
    setState({ kind: "busy" })
    const result = await createCampaign(send, apiOrigin, checked.input, intent.take())
    if (result.kind === "ready") {
      intent.reset()
      setState({ kind: "idle" })
      onCreated(result.value.campaign, result.value.replay)
      return
    }
    if (result.kind === "no_access") onLostAccess()
    setState({ kind: "error", code: result.kind === "no_access" ? "no_access" : result.code })
  }

  const field = (name: "name" | "notBefore" | "expiresAt" | "redemptionLimit", label: string, input: React.ReactNode, hint?: string) => {
    const problem = problems[name]
    return (
      <div className={problem ? "adm-field adm-field-bad" : "adm-field"}>
        <label htmlFor={id + name}>{label}</label>
        {input}
        {hint ? <p id={id + name + "h"} className="adm-field-hint">{hint}</p> : null}
        {problem ? <p id={id + name + "e"} className="adm-field-error"><span aria-hidden="true">! </span>{say(PROBLEM_WORD[problem], { max: NAME_MAX })}</p> : null}
      </div>
    )
  }
  const describe = (name: "name" | "notBefore" | "expiresAt" | "redemptionLimit", hint: boolean) =>
    [hint ? id + name + "h" : "", problems[name] ? id + name + "e" : ""].filter(Boolean).join(" ") || undefined
  const preview = (value: string) => {
    const ms = Date.parse(value)
    return Number.isNaN(ms) ? say("createTimeHint", { zone }) : say("createTimeUTC", { zone, utc: utcMinute(new Date(ms).toISOString()) })
  }
  const busy = state.kind === "busy"

  return (
    <form id="adm-create" className="adm-form" onSubmit={submit} noValidate aria-labelledby={id + "h"} aria-busy={busy}>
      <h3 id={id + "h"} className="adm-h3">{say("createTitle")}</h3>
      <p className="adm-fine">{say("createLead")}</p>
      <div className="adm-form-grid">
        {field("name", say("createName"), (
          <input ref={first} id={id + "name"} type="text" maxLength={NAME_MAX + 20} autoComplete="off" value={draft.name} onChange={edit("name")}
            aria-invalid={problems.name ? true : undefined} aria-describedby={describe("name", true)} disabled={busy} />
        ), say("createNameHint"))}
        {field("redemptionLimit", say("createLimit"), (
          <input id={id + "redemptionLimit"} type="number" inputMode="numeric" min={1} step={1} value={draft.redemptionLimit} onChange={edit("redemptionLimit")}
            aria-invalid={problems.redemptionLimit ? true : undefined} aria-describedby={describe("redemptionLimit", true)} disabled={busy} />
        ), say("createLimitHint"))}
        <div className="adm-field">
          <label htmlFor={id + "durationCalendarMonths"}>{say("createDuration")}</label>
          <select id={id + "durationCalendarMonths"} value={draft.durationCalendarMonths} disabled={busy}
            onChange={(event) => { intent.reset(); setDraft((d) => ({ ...d, durationCalendarMonths: event.target.value as "3" | "12" })) }}>
            <option value="3">{say("createDurationThree")}</option>
            <option value="12">{say("createDurationYear")}</option>
          </select>
        </div>
        {field("notBefore", say("createStart"), (
          <input id={id + "notBefore"} type="datetime-local" value={draft.notBefore} onChange={edit("notBefore")}
            aria-invalid={problems.notBefore ? true : undefined} aria-describedby={describe("notBefore", true)} disabled={busy} />
        ), preview(draft.notBefore))}
        {field("expiresAt", say("createEnd"), (
          <input id={id + "expiresAt"} type="datetime-local" value={draft.expiresAt} onChange={edit("expiresAt")}
            aria-invalid={problems.expiresAt ? true : undefined} aria-describedby={describe("expiresAt", true)} disabled={busy} />
        ), preview(draft.expiresAt))}
      </div>
      <div className="adm-policy">
        <p className="adm-policy-title">{say("createPolicyTitle")}</p>
        <p className="adm-fine">{say("createPolicy")}</p>
      </div>
      {state.kind === "error" ? <Banner tone="bad" text={say("createFailed", { reason: failureText(state.code, say) })} say={say} /> : null}
      <div className="adm-form-actions">
        <button type="submit" className="adm-btn adm-btn-go" disabled={busy} aria-busy={busy}>
          {busy ? say("createBusy") : state.kind === "error" ? say("createRetry") : say("createSubmit")}
        </button>
        <button type="button" className="adm-btn adm-btn-quiet" onClick={onCancel} disabled={busy}>{say("cancel")}</button>
      </div>
    </form>
  )
}

/* ---- one campaign -------------------------------------------------------- */

type CodesState =
  | { kind: "loading"; offset: number }
  | { kind: "ready"; page: CodePage; refreshing: boolean }
  | { kind: "unavailable" }
  | { kind: "failed"; code: string; offset: number }

function CampaignDetail({ id, row, full, apiOrigin, say, locale, n, go, notice, clearNotice, onRevoked, onIssued, onLostAccess }: {
  id: string
  row: ListedCampaign | null
  full: PromoCampaign | null
  apiOrigin: string
  say: Say
  locale: string
  n: Intl.NumberFormat
  go: Go
  notice: string | null
  clearNotice: () => void
  onRevoked: (name: string) => void
  onIssued: () => void
  onLostAccess: () => void
}) {
  const [codes, setCodes] = useState<CodesState>({ kind: "loading", offset: 0 })
  const [revoking, setRevoking] = useState(false)
  const [revokingCode, setRevokingCode] = useState<{ id: string; label: string } | null>(null)
  const heading = useRef<HTMLHeadingElement>(null)
  const generation = useRef(0)

  const readCodes = useCallback(async (offset: number, keep: boolean) => {
    const mine = ++generation.current
    setCodes((current) => keep && current.kind === "ready" ? { ...current, refreshing: true } : { kind: "loading", offset })
    const result: Read<CodePage> = await loadCodes(send, apiOrigin, id, offset)
    if (mine !== generation.current) return
    if (result.kind === "ready") setCodes({ kind: "ready", page: result.value, refreshing: false })
    else if (result.kind === "no_access") setCodes({ kind: "unavailable" })
    else setCodes({ kind: "failed", code: result.code, offset })
  }, [apiOrigin, id])

  useEffect(() => {
    void readCodes(0, false)
    return () => { generation.current++ }
  }, [readCodes])

  useEffect(() => heading.current?.focus({ preventScroll: true }), [])

  // The freshest facts win: the code page's campaign, then the list's row.
  const fresh = codes.kind === "ready" ? codes.page.campaign : null
  const now = Date.now()
  const facts = fresh
    ? { ...fresh, phase: listedFromCampaign({ ...fresh, tier: null, duration_calendar_months: null, conflict_policy: null, created_at: null }, now).phase }
    : row
  const name = (facts?.name || row?.name || "") || say("campaignUnnamed")
  const issuable = fresh ? canIssue(fresh, now) : full ? canIssue(full, now) : row ? row.phase !== "revoked" && row.phase !== "expired" : false
  const revocable = (fresh?.state ?? full?.state) === "active" || (!fresh && !full && row !== null && row.phase !== "revoked")
  const limit = fresh?.redemption_limit ?? full?.redemption_limit ?? row?.redemption_limit ?? null
  const publicCodes = codes.kind === "ready" ? codes.page.rows.flatMap((code) => code.public_code ? [code.public_code] : []) : []

  return (
    <div className="adm-page adm-detail">
      <RouteLink href={promoHref(null)} go={go} className="adm-crumb">
        <span aria-hidden="true">← </span>{say("backToCampaigns")}
      </RouteLink>

      <section className="adm-panel adm-detail-head" aria-labelledby="adm-detail-h">
        <div className="adm-panel-head">
          <div>
            <h2 id="adm-detail-h" ref={heading} tabIndex={-1} className="adm-h2-big">{name}</h2>
            {facts ? <PhaseBadge phase={facts.phase} say={say} /> : null}
          </div>
          {revocable ? (
            <button type="button" className="adm-btn adm-btn-danger" onClick={() => setRevoking(true)}>{say("revokeOpen")}</button>
          ) : null}
        </div>
        {facts ? (
          <dl className="adm-facts">
            <div><dt>{say("campaignWindow")}</dt><dd>{campaignWindow(facts, say)}</dd></div>
            <div><dt>{say("campaignRedeemed")}</dt><dd>
              {facts.redemption_limit === null
                ? say("campaignNoLimit", { n: n.format(facts.redeemed_count) })
                : say("campaignOf", { n: n.format(facts.redeemed_count), limit: n.format(facts.redemption_limit) })}
            </dd></div>
            {full?.tier ? <div><dt>{say("campaignGrant")}</dt><dd>{say("campaignGrantValue", { tier: full.tier.toUpperCase(), months: full.duration_calendar_months ?? "?" })}</dd></div> : null}
            {full?.conflict_policy ? <div><dt>{say("campaignPolicy")}</dt><dd>{full.conflict_policy === "reject" ? say("policyReject") : full.conflict_policy}</dd></div> : null}
            {full?.created_at ? <div><dt>{say("campaignCreated")}</dt><dd>{utcMinute(full.created_at)}</dd></div> : null}
            {row?.codes ? <div><dt>{say("campaignCodes")}</dt><dd>{say("campaignCodesValue", { available: n.format(row.codes.available), issued: n.format(row.codes.issued) })}</dd></div> : null}
          </dl>
        ) : codes.kind === "loading" ? (
          <p className="adm-fine">{say("campaignLoading")}</p>
        ) : (
          <p className="adm-fine">{say("campaignNotListed")}</p>
        )}
        {publicCodes.length ? (
          <div className="adm-campaign-pages">
            <span>{say("campaignPublicPages")}</span>
            {publicCodes.map((code) => (
              <a key={code} href={publicCampaignPage(code)} target="_blank" rel="noopener noreferrer" className="adm-crumb">
                {say("campaignViewPage", { code })}<span aria-hidden="true"> ↗</span>
              </a>
            ))}
          </div>
        ) : null}
      </section>

      {notice ? <Banner tone="ok" text={notice} onDismiss={clearNotice} say={say} /> : null}

      {issuable && limit !== null ? (
        <IssueCode campaignId={id} campaignLimit={limit} apiOrigin={apiOrigin} say={say} n={n}
          onIssued={() => { onIssued(); void readCodes(0, true) }} onLostAccess={onLostAccess} />
      ) : facts ? (
        <p className="adm-fine adm-panel adm-panel-quiet">{say(facts.phase === "revoked" ? "issueRevoked" : "issueClosed")}</p>
      ) : null}

      <CodeList codes={codes} say={say} n={n} onPage={(offset) => void readCodes(offset, true)} onRetry={() => void readCodes(codes.kind === "failed" ? codes.offset : 0, false)} onRevoke={(code) => setRevokingCode(code)} />

      {revokingCode ? <RevokeCodeDialog campaignId={id} code={revokingCode} apiOrigin={apiOrigin} say={say}
        onClose={() => setRevokingCode(null)} onRevoked={() => { setRevokingCode(null); void readCodes(codes.kind === "ready" ? codes.page.offset : 0, true); onIssued() }}
        onLostAccess={onLostAccess} /> : null}

      {revoking ? (
        <RevokeDialog campaignId={id} name={name} apiOrigin={apiOrigin} say={say}
          onClose={() => setRevoking(false)}
          onRevoked={() => { setRevoking(false); void readCodes(0, true); onRevoked(name) }}
          onLostAccess={onLostAccess} />
      ) : null}
    </div>
  )
}

function IssueCode({ campaignId, campaignLimit, apiOrigin, say, n, onIssued, onLostAccess }: {
  campaignId: string
  campaignLimit: number
  apiOrigin: string
  say: Say
  n: Intl.NumberFormat
  onIssued: () => void
  onLostAccess: () => void
}) {
  const id = useId()
  const [raw, setRaw] = useState("1")
  const [publicCode, setPublicCode] = useState("")
  const [state, setState] = useState<Mutation>({ kind: "idle" })
  const [problem, setProblem] = useState<"limit_not_whole" | "limit_too_large" | "public_code" | null>(null)
  // The plaintext, here and nowhere else, until it is dismissed.
  const [revealed, setRevealed] = useState<{ code: string; codeId: string | null } | null>(null)
  const [replayed, setReplayed] = useState(false)
  const intent = useIntentKey()

  const submit = async (event: React.FormEvent) => {
    event.preventDefault()
    if (state.kind === "busy") return
    const limit = checkCodeLimit(raw, campaignLimit)
    if (typeof limit === "string") {
      setProblem(limit)
      document.getElementById(id + "limit")?.focus()
      return
    }
    const named = publicCode.trim().toUpperCase()
    if (named && (named.length < 6 || named.length > 48 || !/^[A-Z][A-Z0-9]*(?:-[A-Z0-9]+)*$/.test(named))) {
      setProblem("public_code")
      document.getElementById(id + "public")?.focus()
      return
    }
    setProblem(null)
    setState({ kind: "busy" })
    const result = await issueCode(send, apiOrigin, campaignId, limit, intent.take(), named || undefined)
    if (result.kind === "ready") {
      intent.reset()
      setState({ kind: "idle" })
      if (result.value.code) {
        setRevealed({ code: result.value.code, codeId: result.value.codeId })
        setReplayed(false)
      } else {
        setReplayed(true)
      }
      onIssued()
      return
    }
    if (result.kind === "no_access") onLostAccess()
    setState({ kind: "error", code: result.kind === "no_access" ? "no_access" : result.code })
  }
  const busy = state.kind === "busy"

  return (
    <section className="adm-panel adm-issue" aria-labelledby={id + "h"}>
      <h2 id={id + "h"} className="adm-h3">{say("issueTitle")}</h2>
      <p className="adm-fine">{say("issueLead")}</p>
      {revealed ? (
        <OneTimeCode code={revealed.code} codeId={revealed.codeId} publicCode={!!publicCode.trim()} say={say} onDone={() => setRevealed(null)} />
      ) : (
        <form className="adm-issue-form" onSubmit={submit} noValidate aria-busy={busy}>
          <div className={problem && problem !== "public_code" ? "adm-field adm-field-bad" : "adm-field"}>
            <label htmlFor={id + "limit"}>{say("issueLimit")}</label>
            <input id={id + "limit"} type="number" inputMode="numeric" min={1} max={campaignLimit} step={1} value={raw}
              onChange={(e) => { intent.reset(); setRaw(e.target.value); setProblem(null); if (state.kind === "error") setState({ kind: "idle" }) }}
              aria-invalid={problem && problem !== "public_code" ? true : undefined} aria-describedby={id + "hint" + (problem && problem !== "public_code" ? " " + id + "err" : "")} disabled={busy} />
            <p id={id + "hint"} className="adm-field-hint">{say("issueLimitHint", { max: n.format(campaignLimit) })}</p>
            {problem && problem !== "public_code" ? (
              <p id={id + "err"} className="adm-field-error"><span aria-hidden="true">! </span>
                {say(problem === "limit_too_large" ? "problemCodeLimitLarge" : "problemLimitWhole", { max: n.format(campaignLimit) })}
              </p>
            ) : null}
          </div>
          <div className={problem === "public_code" ? "adm-field adm-field-bad" : "adm-field"}>
            <label htmlFor={id + "public"}>{say("issuePublicCode")}</label>
            <input id={id + "public"} type="text" value={publicCode} maxLength={48} autoComplete="off" spellCheck={false}
              placeholder="HACKER-NEWS" onChange={(e) => { intent.reset(); setPublicCode(e.target.value.toUpperCase()); setProblem(null); if (state.kind === "error") setState({ kind: "idle" }) }}
              aria-invalid={problem === "public_code" ? true : undefined} aria-describedby={id + "publichint" + (problem === "public_code" ? " " + id + "publicerr" : "")} disabled={busy} />
            <p id={id + "publichint"} className="adm-field-hint">{say("issuePublicCodeHint")}</p>
            {problem === "public_code" ? <p id={id + "publicerr"} className="adm-field-error">{say("problemPublicCode")}</p> : null}
          </div>
          <button type="submit" className="adm-btn adm-btn-go" disabled={busy} aria-busy={busy}>
            {busy ? say("issueBusy") : state.kind === "error" ? say("issueRetry") : say("issueSubmit")}
          </button>
        </form>
      )}
      {replayed ? <Banner tone="ok" text={say("issueReplayed")} onDismiss={() => setReplayed(false)} say={say} /> : null}
      {state.kind === "error" ? <Banner tone="bad" text={say("issueFailed", { reason: failureText(state.code, say) })} say={say} /> : null}
    </section>
  )
}

function OneTimeCode({ code, codeId, publicCode, say, onDone }: { code: string; codeId: string | null; publicCode: boolean; say: Say; onDone: () => void }) {
  const id = useId()
  const field = useRef<HTMLInputElement>(null)
  const [copied, setCopied] = useState<"no" | "yes" | "failed">("no")
  const [confirming, setConfirming] = useState(false)

  useEffect(() => {
    field.current?.focus()
    field.current?.select()
  }, [])

  useEffect(() => {
    if (publicCode) return
    const release = guardCodeNavigation(() => confirmAction(say("revealLeaveWarning")))
    const beforeUnload = (event: BeforeUnloadEvent) => {
      event.preventDefault()
      event.returnValue = ""
    }
    window.addEventListener("beforeunload", beforeUnload)
    return () => {
      release()
      window.removeEventListener("beforeunload", beforeUnload)
    }
  }, [say, publicCode])

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(code)
      setCopied("yes")
    } catch {
      field.current?.focus()
      field.current?.select()
      setCopied("failed")
    }
  }

  return (
    <div className="adm-reveal" role="group" aria-labelledby={id + "h"}>
      <h3 id={id + "h"} className="adm-reveal-title">{say("revealTitle")}</h3>
      <p className="adm-reveal-warn">{publicCode ? say("revealPublicNote") : <><span aria-hidden="true">⚠ </span>{say("revealWarning")}</>}</p>
      <div className="adm-reveal-row">
        <label htmlFor={id + "c"} className="adm-visually-hidden">{say("revealLabel")}</label>
        <input ref={field} id={id + "c"} className="adm-reveal-code" type="text" readOnly value={code} spellCheck={false} autoComplete="off"
          onFocus={(e) => e.currentTarget.select()} />
        <button type="button" className="adm-btn adm-btn-go" onClick={() => void copy()}>
          {copied === "yes" ? say("revealCopied") : say("revealCopy")}
        </button>
      </div>
      <p className="adm-fine" role="status">
        {copied === "yes" ? say("revealCopiedNote") : copied === "failed" ? say("revealCopyFailed") : ""}
      </p>
      {codeId ? <p className="adm-fine">{say("revealId", { id: codeId })}</p> : null}
      <div className="adm-form-actions">
        {publicCode || confirming || copied === "yes" ? (
          <button type="button" className="adm-btn" onClick={onDone}>{say(publicCode ? "revealPublicDone" : "revealDone")}</button>
        ) : (
          <button type="button" className="adm-btn adm-btn-quiet" onClick={() => setConfirming(true)}>{say("revealClose")}</button>
        )}
        {!publicCode && confirming && copied !== "yes" ? <p className="adm-fine adm-reveal-confirm" role="alert">{say("revealCloseWarning")}</p> : null}
      </div>
    </div>
  )
}

function CodeList({ codes, say, n, onPage, onRetry, onRevoke }: { codes: CodesState; say: Say; n: Intl.NumberFormat; onPage: (offset: number) => void; onRetry: () => void; onRevoke: (code: { id: string; label: string }) => void }) {
  return (
    <section className="adm-panel adm-codes" aria-labelledby="adm-codes-h" aria-busy={codes.kind === "loading" || (codes.kind === "ready" && codes.refreshing)}>
      <h2 id="adm-codes-h" className="adm-h3">{say("codesTitle")}</h2>
      <p className="adm-fine">{say("codesLead")}</p>
      {codes.kind === "loading" ? (
        <div className="adm-list-loading" role="status"><span className="adm-spinner" aria-hidden="true" />{say("codesLoading")}</div>
      ) : codes.kind === "unavailable" ? (
        <p className="adm-fine adm-capped">{say("codesUnavailable")}</p>
      ) : codes.kind === "failed" ? (
        <div className="adm-inline-error" role="alert">
          <p>{say("codesFailed", { reason: failureText(codes.code, say) })}</p>
          <button type="button" className="adm-btn" onClick={onRetry}>{say("tryAgain")}</button>
        </div>
      ) : codes.page.rows.length === 0 ? (
        <div className="adm-empty">
          <p className="adm-empty-title">{say("codesNone")}</p>
          <p className="adm-fine">{say("codesNoneHint")}</p>
        </div>
      ) : (
        <>
          <div className="adm-table-wrap">
            <table className="adm-table adm-table-codes">
              <thead>
                <tr>
                  <th scope="col">{say("codeId")}</th>
                  <th scope="col">{say("codeIssued")}</th>
                  <th scope="col">{say("codeUsed")}</th>
                  <th scope="col">{say("codeState")}</th>
                  <th scope="col">{say("codeAction")}</th>
                </tr>
              </thead>
              <tbody>
                {codes.page.rows.map((row) => {
                  const share = fill(row.redeemed_count, row.redemption_limit)
                  const spent = row.redeemed_count >= row.redemption_limit
                  return (
                    <tr key={row.id}>
                      <th scope="row" data-label={say("codeId")}>
                        {row.public_code ? <><code className="adm-codeid">{row.public_code}</code><br /></> : null}
                        <code className="adm-codeid">{row.id}</code>
                      </th>
                      <td data-label={say("codeIssued")} className="adm-when">{utcMinute(row.issued_at)}</td>
                      <td data-label={say("codeUsed")}>
                        <span className="adm-fillnum">{say("campaignOf", { n: n.format(row.redeemed_count), limit: n.format(row.redemption_limit) })}</span>
                        {share !== null ? <span className="adm-meter" aria-hidden="true"><span style={{ width: `${Math.round(share * 100)}%` }} /></span> : null}
                      </td>
                      <td data-label={say("codeState")}>
                        <span className={"adm-status adm-status-" + (row.revoked_at ? "bad" : spent ? "warn" : row.redeemed_count > 0 ? "ok" : "calm")}>
                          <span className="adm-dot" aria-hidden="true" />
                          {say(row.revoked_at ? "codeRevoked" : spent ? "codeSpent" : row.redeemed_count > 0 ? "codeInUse" : "codeUnused")}
                        </span>
                      </td>
                      <td data-label={say("codeAction")}>{!row.revoked_at ? <button type="button" className="adm-btn adm-btn-quiet adm-btn-small" onClick={() => onRevoke({ id: row.id, label: row.public_code ?? row.id })}>{say("codeRevokeOpen")}</button> : null}</td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
          <Pager offset={codes.page.offset} shown={codes.page.rows.length} total={codes.page.total} onPage={onPage} say={say} n={n} label={say("codesPages")} />
        </>
      )}
    </section>
  )
}

function RevokeCodeDialog({ campaignId, code, apiOrigin, say, onClose, onRevoked, onLostAccess }: {
  campaignId: string; code: { id: string; label: string }; apiOrigin: string; say: Say;
  onClose: () => void; onRevoked: () => void; onLostAccess: () => void
}) {
  const id = useId()
  const dialog = useRef<HTMLDialogElement>(null)
  const cancel = useRef<HTMLButtonElement>(null)
  const [state, setState] = useState<Mutation>({ kind: "idle" })
  const intent = useIntentKey()
  const busy = state.kind === "busy"
  useEffect(() => {
    const d = dialog.current
    if (!d) return
    const opener = document.activeElement as HTMLElement | null
    d.showModal()
    cancel.current?.focus()
    return () => opener?.focus?.()
  }, [])
  const confirm = async () => {
    if (busy) return
    setState({ kind: "busy" })
    const result = await revokeCode(send, apiOrigin, campaignId, code.id, intent.take())
    if (result.kind === "ready") { dialog.current?.close(); onRevoked(); return }
    if (result.kind === "no_access") onLostAccess()
    setState({ kind: "error", code: result.kind === "no_access" ? "no_access" : result.code })
  }
  return <dialog ref={dialog} className="adm-dialog" aria-labelledby={id + "h"} aria-describedby={id + "b"}
    onCancel={(e) => { if (busy) e.preventDefault() }} onClose={onClose}
    onClick={(e) => { if (e.target === dialog.current && !busy) dialog.current?.close() }}>
    <div className="adm-dialog-body">
      <h2 id={id + "h"} className="adm-h3">{say("codeRevokeTitle", { code: code.label })}</h2>
      <p id={id + "b"}>{say("codeRevokeBody")}</p>
      {state.kind === "error" ? <Banner tone="bad" text={say("codeRevokeFailed", { reason: failureText(state.code, say) })} say={say} /> : null}
      <div className="adm-form-actions adm-dialog-actions">
        <button ref={cancel} type="button" className="adm-btn" onClick={() => dialog.current?.close()} disabled={busy}>{say("cancel")}</button>
        <button type="button" className="adm-btn adm-btn-danger" onClick={() => void confirm()} disabled={busy} aria-busy={busy}>{say(busy ? "codeRevokeBusy" : "codeRevokeConfirm")}</button>
      </div>
    </div>
  </dialog>
}

function RevokeDialog({ campaignId, name, apiOrigin, say, onClose, onRevoked, onLostAccess }: {
  campaignId: string
  name: string
  apiOrigin: string
  say: Say
  onClose: () => void
  onRevoked: () => void
  onLostAccess: () => void
}) {
  const id = useId()
  const dialog = useRef<HTMLDialogElement>(null)
  const cancel = useRef<HTMLButtonElement>(null)
  const [state, setState] = useState<Mutation>({ kind: "idle" })
  const intent = useIntentKey()
  const busy = state.kind === "busy"

  useEffect(() => {
    const d = dialog.current
    if (!d) return
    // `showModal` makes everything else inert, traps the focus, and closes on
    // Escape; the safe choice takes the focus first.
    const opener = document.activeElement as HTMLElement | null
    if (!d.open) d.showModal()
    cancel.current?.focus()
    return () => opener?.focus?.()
  }, [])

  const confirm = async () => {
    if (busy) return
    setState({ kind: "busy" })
    const result: Read<{ campaign: PromoCampaign; replay: boolean }> | Failure = await revokeCampaign(send, apiOrigin, campaignId, intent.take())
    if (result.kind === "ready") {
      dialog.current?.close()
      onRevoked()
      return
    }
    if (result.kind === "no_access") onLostAccess()
    setState({ kind: "error", code: result.kind === "no_access" ? "no_access" : result.code })
  }

  return (
    <dialog ref={dialog} className="adm-dialog" aria-labelledby={id + "h"} aria-describedby={id + "b"}
      onCancel={(e) => { if (busy) e.preventDefault() }}
      onClose={onClose}
      onClick={(e) => { if (e.target === dialog.current && !busy) dialog.current?.close() }}>
      <div className="adm-dialog-body">
        <h2 id={id + "h"} className="adm-h3">{say("revokeTitle", { name })}</h2>
        <div id={id + "b"}>
          <p>{say("revokeBody")}</p>
          <ul className="adm-dialog-list">
            <li>{say("revokeEffectCodes")}</li>
            <li>{say("revokeEffectGrants")}</li>
            <li>{say("revokeEffectFinal")}</li>
          </ul>
        </div>
        {state.kind === "error" ? <Banner tone="bad" text={say("revokeFailed", { reason: failureText(state.code, say) })} say={say} /> : null}
        <div className="adm-form-actions adm-dialog-actions">
          <button ref={cancel} type="button" className="adm-btn" onClick={() => dialog.current?.close()} disabled={busy}>{say("cancel")}</button>
          <button type="button" className="adm-btn adm-btn-danger" onClick={() => void confirm()} disabled={busy} aria-busy={busy}>
            {busy ? say("revokeBusy") : state.kind === "error" ? say("revokeRetry") : say("revokeConfirm")}
          </button>
        </div>
      </div>
    </dialog>
  )
}
