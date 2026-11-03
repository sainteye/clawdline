import { VisitsOverview } from "./VisitsOverview.js"
import { useId } from "react"
import { trend, type CampaignRow, type Comparison, type DailyCount, type Overview, type UsagePeriod } from "./api.js"
import { adminPath, type AdminRoute, type Say } from "./words.js"

/*
 * The public-website report has its own superuser contract, including member visit history.
 * The operations pages that read the aggregate overview: the overview itself, accounts
 * and usage. Each route draws one subject; the overview answers "is anything
 * different today" and points at the page that holds the detail.
 *
 * Every figure the API could not vouch for is shown as the word for unknown,
 * every chart has its numbers in words beside it and a table behind it, and
 * no change is told by colour alone. A sentence of insight is built only from
 * the aggregates the API returned; nothing here estimates what it did not say.
 */

/** Follows an in-page link without reloading; a modified press is left to the browser. */
export type Go = (href: string) => void

export function RouteLink({ href, go, className, current, children }: { href: string; go: Go; className?: string; current?: boolean; children: React.ReactNode }) {
  return (
    <a
      href={href}
      className={className}
      aria-current={current ? "page" : undefined}
      onClick={(event) => {
        if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return
        event.preventDefault()
        go(href)
      }}
    >
      {children}
    </a>
  )
}

interface PageProps {
  overview: Overview
  say: Say
  locale: string
  go: Go
}

/* ---- overview --------------------------------------------------------- */

export function OverviewPage({ overview, say, locale, go, apiOrigin, demo = false }: PageProps & { apiOrigin: string; demo?: boolean }) {
  const n = new Intl.NumberFormat(locale)
  const { accounts, promotions, usage } = overview
  const yesterday = accounts.daily.length ? accounts.daily[accounts.daily.length - 1] : null
  return (
    <div className="adm-page">
      <VisitsOverview apiOrigin={apiOrigin} demo={demo} refreshToken={overview.generated_at} say={say} locale={locale} />
      <div className="adm-tiles adm-tiles-4">
        <Tile label={say("accountsTotal")} value={n.format(accounts.total)} lead />
        <Tile label={say("accountsYesterday")} value={yesterday ? n.format(yesterday.count) : say("usageUnknown")}>
          <p className="adm-tile-note">{yesterday ? longDay(yesterday.day, locale) : say("chartEmpty")}</p>
        </Tile>
        <TrendTile label={say("accountsNew7")} value={accounts.new_7d} say={say} n={n} />
        <Tile label={say("usageActiveNow")} value={usage.current.active_accounts === null ? say("usageUnknown") : n.format(usage.current.active_accounts)}>
          <p className="adm-tile-note">
            {usage.current.coverage === "partial" ? say("usagePartial") : say("usageThroughToday")}
          </p>
        </Tile>
      </div>

      <section className="adm-panel adm-insights" aria-labelledby="adm-insights">
        <h2 id="adm-insights" className="adm-h3">{say("insightsTitle")}</h2>
        <ul className="adm-insight-list">
          {insights(overview, say, locale).map((item) => (
            <li key={item.key} className="adm-insight">
              <span className={"adm-insight-mark adm-insight-" + item.tone} aria-hidden="true" />
              <p>{item.text}</p>
              <RouteLink href={adminPath(item.route)} go={go} className="adm-insight-link">
                {say(item.linkKey)}<span aria-hidden="true"> →</span>
              </RouteLink>
            </li>
          ))}
        </ul>
        <p className="adm-fine">{say("insightsBasis")}</p>
      </section>

      <DailyChart title={say("chartAccounts")} days={accounts.daily} say={say} locale={locale} />

      <div className="adm-dest" role="list">
        <Destination route="accounts" go={go} say={say} title={say("nav_accounts")} body={say("destAccounts", { total: n.format(accounts.total), new30: n.format(accounts.new_30d.current) })} />
        <Destination route="promotions" go={go} say={say} title={say("nav_promotions")} body={say("destPromotions", { redeemed: n.format(promotions.redeemed_30d.current), campaigns: n.format(promotions.campaigns_total) })} />
        <Destination route="usage" go={go} say={say} title={say("nav_usage")} body={
          usage.current.counters.dispatches === null
            ? say("destUsageUnknown")
            : say("destUsage", { dispatches: n.format(usage.current.counters.dispatches) })
        } />
      </div>
    </div>
  )
}

function Destination({ route, go, say, title, body }: { route: AdminRoute; go: Go; say: Say; title: string; body: string }) {
  return (
    <div role="listitem" className="adm-dest-item">
      <RouteLink href={adminPath(route)} go={go} className="adm-dest-card">
        <span className="adm-dest-title">{title}</span>
        <span className="adm-dest-body">{body}</span>
        <span className="adm-dest-go">{say("open")}<span aria-hidden="true"> →</span></span>
      </RouteLink>
    </div>
  )
}

type Insight = { key: string; text: string; tone: "up" | "down" | "flat" | "note"; route: AdminRoute; linkKey: "seeAccounts" | "seePromotions" | "seeUsage" }

function changeSentence(c: Comparison, say: Say, n: Intl.NumberFormat, keys: { up: Parameters<Say>[0]; down: Parameters<Say>[0]; flat: Parameters<Say>[0] }): { text: string; tone: Insight["tone"] } {
  const t = trend(c)
  const values = { current: n.format(c.current), previous: n.format(c.previous) }
  if (t.kind === "flat") return { text: say(keys.flat, values), tone: "flat" }
  const pct = t.percent === null ? "" : say("insightPercent", { percent: n.format(t.percent) })
  return { text: say(t.kind === "up" ? keys.up : keys.down, { ...values, delta: n.format(t.delta), pct }), tone: t.kind }
}

/** What an operator should notice, in sentences, from nothing but the returned aggregates. */
export function insights(overview: Overview, say: Say, locale: string): Insight[] {
  const n = new Intl.NumberFormat(locale)
  const { accounts, promotions, usage } = overview
  const out: Insight[] = []
  const a7 = changeSentence(accounts.new_7d, say, n, { up: "insightAccountsUp", down: "insightAccountsDown", flat: "insightAccountsFlat" })
  out.push({ key: "accounts7", ...a7, route: "accounts", linkKey: "seeAccounts" })
  if (accounts.daily.length) {
    const peak = accounts.daily.reduce((best, d) => (d.count > best.count ? d : best), accounts.daily[0])
    const tied = accounts.daily.filter((d) => d.count === peak.count).length
    out.push(peak.count > 0
      ? { key: "peak", text: say(tied > 1 ? "insightPeakTie" : "insightPeak", { day: longDay(peak.day, locale), count: n.format(peak.count), days: accounts.daily.length, tied: n.format(tied) }), tone: "note", route: "accounts", linkKey: "seeAccounts" }
      : { key: "peak", text: say("insightNoSignups", { days: accounts.daily.length }), tone: "note", route: "accounts", linkKey: "seeAccounts" })
  }
  const r30 = changeSentence(promotions.redeemed_30d, say, n, { up: "insightRedeemedUp", down: "insightRedeemedDown", flat: "insightRedeemedFlat" })
  out.push({ key: "redeemed30", ...r30, route: "promotions", linkKey: "seePromotions" })
  const rows = promotions.campaigns.rows
  if (rows.length) {
    const open = rows.filter((r) => r.status === "redeemable").length
    const blocked = rows.filter((r) => r.status === "campaign_exhausted" || r.status === "no_available_code").length
    const capped = promotions.campaigns.total > rows.length
    const scope = capped ? say("insightScopeLatest", { shown: n.format(rows.length) }) : say("insightScopeAll")
    out.push({
      key: "campaigns",
      text: blocked > 0
        ? say("insightCampaignsBlocked", { scope, open: n.format(open), blocked: n.format(blocked) })
        : say("insightCampaignsOpen", { scope, open: n.format(open) }),
      tone: blocked > 0 ? "down" : "note",
      route: "promotions",
      linkKey: "seePromotions",
    })
  } else {
    out.push({ key: "campaigns", text: say("insightNoCampaigns"), tone: "note", route: "promotions", linkKey: "seePromotions" })
  }
  const cur = usage.current
  const prev = usage.previous
  if (cur.active_accounts !== null && prev.active_accounts !== null) {
    out.push({
      key: "usage",
      text: say(cur.coverage === "partial" ? "insightUsagePartial" : "insightUsage", { now: n.format(cur.active_accounts), last: n.format(prev.active_accounts) }),
      tone: "note",
      route: "usage",
      linkKey: "seeUsage",
    })
  } else {
    out.push({ key: "usage", text: say("insightUsageUnknown"), tone: "note", route: "usage", linkKey: "seeUsage" })
  }
  return out
}

/* ---- accounts --------------------------------------------------------- */

export function AccountsPage({ overview, say, locale }: PageProps) {
  const n = new Intl.NumberFormat(locale)
  const one = new Intl.NumberFormat(locale, { maximumFractionDigits: 1 })
  const { accounts } = overview
  const days = accounts.daily
  const yesterday = days.length ? days[days.length - 1] : null
  const sum = days.reduce((s, d) => s + d.count, 0)
  const peak = days.length ? days.reduce((best, d) => (d.count > best.count ? d : best), days[0]) : null
  const peakTies = peak ? days.filter((d) => d.count === peak.count).length : 0
  const quiet = days.filter((d) => d.count === 0).length
  return (
    <div className="adm-page">
      <div className="adm-tiles adm-tiles-4">
        <Tile label={say("accountsTotal")} value={n.format(accounts.total)} lead />
        <Tile label={say("accountsYesterday")} value={yesterday ? n.format(yesterday.count) : say("usageUnknown")}>
          <p className="adm-tile-note">{yesterday ? longDay(yesterday.day, locale) : say("chartEmpty")}</p>
        </Tile>
        <TrendTile label={say("accountsNew7")} value={accounts.new_7d} say={say} n={n} />
        <TrendTile label={say("accountsNew30")} value={accounts.new_30d} say={say} n={n} />
      </div>
      <p className="adm-fine">{say("comparisonNote")}</p>

      <DailyChart title={say("chartAccounts")} days={days} say={say} locale={locale} tall />

      {days.length ? (
        <section className="adm-stats" aria-labelledby="adm-acc-stats">
          <h2 id="adm-acc-stats" className="adm-h2">{say("accountsPattern")}</h2>
          <div className="adm-tiles adm-tiles-3">
            <Stat label={say("statDailyAverage")} value={one.format(sum / days.length)} note={say("statOverDays", { days: days.length })} />
            <Stat label={say("statBusiest")} value={peak ? n.format(peak.count) : "—"} note={peak ? peakTies > 1 ? say("statBusiestTie", { days: n.format(peakTies) }) : longDay(peak.day, locale) : ""} />
            <Stat label={say("statQuietDays")} value={n.format(quiet)} note={say("statQuietNote", { days: days.length })} />
          </div>
        </section>
      ) : null}
      <p className="adm-fine adm-foot">{say("accountsPrivacy")} {say("windowNote")}</p>
    </div>
  )
}

/* ---- usage ------------------------------------------------------------ */

export function UsagePage({ overview, say, locale }: PageProps) {
  const n = new Intl.NumberFormat(locale)
  const { usage } = overview
  const metrics: { label: string; pick: (p: UsagePeriod) => number | null; format: (v: number) => string }[] = [
    { label: say("usageActive"), pick: (p) => p.active_accounts, format: (v) => n.format(v) },
    { label: say("usageDispatches"), pick: (p) => p.counters.dispatches, format: (v) => n.format(v) },
    { label: say("usageMinutes"), pick: (p) => p.counters.connect_minutes, format: (v) => n.format(v) },
    { label: say("usageIn"), pick: (p) => p.counters.bytes_in, format: (v) => bytes(v, locale) },
    { label: say("usageOut"), pick: (p) => p.counters.bytes_out, format: (v) => bytes(v, locale) },
  ]
  return (
    <div className="adm-page">
      <div className="adm-usage">
        <UsageCard heading={say("usageCurrent")} period={usage.current} say={say} locale={locale} n={n} current asOf={overview.generated_at} />
        <UsageCard heading={say("usagePrevious")} period={usage.previous} say={say} locale={locale} n={n} asOf={overview.generated_at} />
      </div>

      {usage.active_daily === null ? (
        <section className="adm-panel" aria-label={say("chartActiveAccounts")}>
          <h2 className="adm-h3">{say("chartActiveAccounts")}</h2>
          <p className="adm-fine">{say("chartActiveUnavailable")}</p>
        </section>
      ) : (
        <>
          <DailyChart title={say("chartActiveAccounts")} days={usage.active_daily} say={say} locale={locale} tall unique />
          <p className="adm-fine">{say("chartActiveBasis")}</p>
        </>
      )}

      <section className="adm-panel adm-compare" aria-labelledby="adm-usage-side">
        <h2 id="adm-usage-side" className="adm-h3">{say("usageSideBySide")}</h2>
        <p className="adm-fine">{say("usageNoGrowth")}</p>
        <div className="adm-table-wrap">
          <table className="adm-table adm-table-compare">
            <thead>
              <tr>
                <th scope="col">{say("usageMetric")}</th>
                <th scope="col">{say("usagePreviousShort", { month: monthName(usage.previous.period, locale) })}</th>
                <th scope="col">{say("usageCurrentShort", { month: monthName(usage.current.period, locale) })}</th>
              </tr>
            </thead>
            <tbody>
              {metrics.map((m) => {
                const before = m.pick(usage.previous)
                const now = m.pick(usage.current)
                const max = Math.max(1, before ?? 0, now ?? 0)
                return (
                  <tr key={m.label}>
                    <th scope="row">{m.label}</th>
                    <td><Measured value={before} max={max} format={m.format} say={say} /></td>
                    <td><Measured value={now} max={max} format={m.format} say={say} current /></td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      </section>

      <section className="adm-stats" aria-labelledby="adm-usage-per">
        <h2 id="adm-usage-per" className="adm-h2">{say("usagePerAccount")}</h2>
        <div className="adm-tiles adm-tiles-3">
          {([
            ["usageMinutesPer", (p: UsagePeriod) => p.counters.connect_minutes, (v: number) => new Intl.NumberFormat(locale, { maximumFractionDigits: 1 }).format(v)],
            ["usageDispatchesPer", (p: UsagePeriod) => p.counters.dispatches, (v: number) => new Intl.NumberFormat(locale, { maximumFractionDigits: 1 }).format(v)],
            ["usageDataPer", (p: UsagePeriod) => (p.counters.bytes_in === null || p.counters.bytes_out === null ? null : p.counters.bytes_in + p.counters.bytes_out), (v: number) => bytes(v, locale)],
          ] as const).map(([key, pick, format]) => {
            const per = (p: UsagePeriod) => {
              const v = pick(p)
              return v === null || !p.active_accounts ? null : v / p.active_accounts
            }
            const last = per(usage.previous)
            const now = per(usage.current)
            return (
              <Stat key={key} label={say(key)}
                value={last === null ? say("usageUnknown") : format(last)}
                note={say("usagePerNote", { month: monthName(usage.previous.period, locale), now: now === null ? say("usageUnknown") : format(now) })} />
            )
          })}
        </div>
        <p className="adm-fine">{say("usagePerBasis")}</p>
      </section>
    </div>
  )
}

function Measured({ value, max, format, say, current = false }: { value: number | null; max: number; format: (v: number) => string; say: Say; current?: boolean }) {
  if (value === null) {
    return <span className="adm-unknown" title={say("usageUnknownLabel")}><span aria-hidden="true">— </span>{say("usageUnknown")}</span>
  }
  return (
    <span className="adm-measured">
      <span className="adm-measured-num">{format(value)}</span>
      <span className={current ? "adm-meter adm-meter-now" : "adm-meter"} aria-hidden="true">
        <span style={{ width: `${Math.round((value / max) * 100)}%` }} />
      </span>
    </span>
  )
}

/* ---- shared pieces ---------------------------------------------------- */

export function Tile({ label, value, lead, children }: { label: string; value: string; lead?: boolean; children?: React.ReactNode }) {
  return (
    <div className={lead ? "adm-tile adm-tile-lead" : "adm-tile"}>
      <p className="adm-tile-label">{label}</p>
      <p className="adm-tile-value">{value}</p>
      {children}
    </div>
  )
}

function Stat({ label, value, note }: { label: string; value: string; note: string }) {
  return (
    <div className="adm-tile adm-tile-stat">
      <p className="adm-tile-label">{label}</p>
      <p className="adm-tile-value">{value}</p>
      {note ? <p className="adm-tile-note">{note}</p> : null}
    </div>
  )
}

export function TrendTile({ label, value, say, n }: { label: string; value: Comparison; say: Say; n: Intl.NumberFormat }) {
  const t = trend(value)
  const previous = n.format(value.previous)
  let chip: string
  let spoken: string
  if (t.kind === "flat") {
    chip = say("trendFlat")
    spoken = say("trendLabelFlat", { previous })
  } else {
    const delta = n.format(t.delta)
    chip = say(t.kind === "up" ? "trendUp" : "trendDown", { delta })
    if (t.percent !== null) chip += " · " + say("trendPercent", { percent: n.format(t.percent) })
    const key = t.kind === "up"
      ? (t.percent !== null ? "trendLabelUpPercent" : "trendLabelUp")
      : (t.percent !== null ? "trendLabelDownPercent" : "trendLabelDown")
    spoken = say(key, { delta, previous, percent: t.percent ?? 0 })
  }
  return (
    <Tile label={label} value={n.format(value.current)}>
      <p className="adm-trend">
        <span className={"adm-chip adm-chip-" + t.kind} aria-hidden="true">{chip}</span>
        <span className="adm-trend-prev" aria-hidden="true">{say("trendAgainst", { previous })}</span>
        <span className="adm-visually-hidden">{spoken}</span>
      </p>
    </Tile>
  )
}

function shortDay(day: string, locale: string): string {
  return new Intl.DateTimeFormat(locale, { timeZone: "UTC", month: "numeric", day: "numeric" }).format(new Date(day + "T00:00:00Z"))
}

function longDay(day: string, locale: string): string {
  return new Intl.DateTimeFormat(locale, { timeZone: "UTC", month: "long", day: "numeric", weekday: "short" }).format(new Date(day + "T00:00:00Z"))
}

export function DailyChart({ title, days, say, locale, tall = false, unique = false }: { title: string; days: DailyCount[]; say: Say; locale: string; tall?: boolean; unique?: boolean }) {
  const id = useId()
  const n = new Intl.NumberFormat(locale)
  if (days.length === 0) {
    return (
      <figure className="adm-panel adm-chart">
        <figcaption className="adm-chart-title">{title}</figcaption>
        <p className="adm-fine">{say("chartEmpty")}</p>
      </figure>
    )
  }
  const total = days.reduce((s, d) => s + d.count, 0)
  const peak = days.reduce((best, d) => (d.count > best.count ? d : best), days[0])
  const tied = days.filter((d) => d.count === peak.count).length
  const max = Math.max(1, peak.count)
  const from = shortDay(days[0].day, locale)
  const to = shortDay(days[days.length - 1].day, locale)
  const summaryKey = unique
    ? total === 0 ? "chartActiveZero" : tied > 1 ? "chartActiveTie" : "chartActiveSummary"
    : total === 0 ? "chartSummaryZero" : tied > 1 ? "chartSummaryTie" : "chartSummary"
  const summary = say(summaryKey, {
    total: n.format(total), days: days.length, peakDay: shortDay(peak.day, locale), peak: n.format(peak.count), tied: n.format(tied),
  })
  // Fixed coordinates keep the daily points evenly spaced at desktop and phone
  // widths. The seven-day line starts only when seven complete days exist.
  const W = 600
  const H = 150
  const left = 6
  const right = W - 6
  const top = 18
  const bottom = H - 8
  const x = (i: number) => days.length === 1 ? W / 2 : left + i * (right - left) / (days.length - 1)
  const y = (count: number) => bottom - count / max * (bottom - top)
  const dailyPoints = days.map((d, i) => `${x(i)},${y(d.count)}`).join(" ")
  const average = days.map((_, i) => i < 6 ? null : days.slice(i - 6, i + 1).reduce((sum, d) => sum + d.count, 0) / 7)
  const averagePoints = average.flatMap((value, i) => value === null ? [] : [`${x(i)},${y(value)}`]).join(" ")
  const hasAverage = days.length >= 7 && total > 0
  const area = `${left},${bottom} ${dailyPoints} ${right},${bottom}`
  return (
    <figure className="adm-panel adm-chart" aria-labelledby={id + "t"} aria-describedby={id + "s"}>
      <figcaption className="adm-chart-head">
        <span id={id + "t"} className="adm-chart-title">{title}</span>
        <span className="adm-chart-range">{say("chartRange", { from, to })}</span>
      </figcaption>
      <div className="adm-chart-legend" aria-label={say("chartLegend")}>
        <span><i className="adm-legend-daily" aria-hidden="true" />{say("chartDaily")}</span>
        {hasAverage ? <span><i className="adm-legend-average" aria-hidden="true" />{say("chartAverage")}</span> : null}
      </div>
      <div className="adm-chart-body">
        <span className="adm-chart-max" aria-hidden="true">{n.format(peak.count)}</span>
        <svg className={tall ? "adm-svg adm-svg-tall" : "adm-svg"} viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" role="img" aria-label={`${title}. ${summary}`}>
          <line x1="0" x2={W} y1={bottom} y2={bottom} className="adm-axis" />
          <line x1="0" x2={W} y1={top} y2={top} className="adm-axis adm-axis-top" />
          {total > 0 ? <polygon points={area} className="adm-line-area" /> : null}
          <polyline points={dailyPoints} className={total > 0 ? "adm-line-daily" : "adm-line-zero"} />
          {hasAverage ? <polyline points={averagePoints} className="adm-line-average" /> : null}
          {total > 0 ? days.map((d, i) => (
            <circle key={d.day} cx={x(i)} cy={y(d.count)} r={d.count === peak.count && d.count > 0 ? 3.6 : 2.3}
              className={d.count === peak.count && d.count > 0 ? "adm-line-point adm-line-peak" : "adm-line-point"}>
              <title>{`${shortDay(d.day, locale)}: ${n.format(d.count)}`}</title>
            </circle>
          )) : null}
        </svg>
      </div>
      <div className="adm-chart-scale" aria-hidden="true">
        <span>{from}</span>
        <span>{to}</span>
      </div>
      <p id={id + "s"} className="adm-chart-summary">{summary}</p>
      <details className="adm-details">
        <summary>{say("chartTable")}</summary>
        <div className="adm-table-wrap">
          <table className="adm-table adm-table-days">
            <thead><tr><th scope="col">{say("chartDay")}</th><th scope="col" className="num">{say("chartCount")}</th></tr></thead>
            <tbody>
              {[...days].reverse().map((d) => (
                <tr key={d.day}><td>{d.day}</td><td className="num">{n.format(d.count)}</td></tr>
              ))}
            </tbody>
          </table>
        </div>
      </details>
    </figure>
  )
}

/** An instant as the page writes it everywhere: `YYYY-MM-DD HH:MM UTC`. */
export function utcMinute(iso: string): string {
  return new Date(iso).toISOString().slice(0, 16).replace("T", " ") + " UTC"
}

export function campaignWindow(row: Pick<CampaignRow, "not_before" | "expires_at">, say: Say): string {
  const from = row.not_before ? utcMinute(row.not_before) : null
  const to = row.expires_at ? utcMinute(row.expires_at) : null
  if (from && to) return say("windowRange", { from, to })
  if (from) return say("windowFrom", { date: from })
  if (to) return say("windowUntil", { date: to })
  return say("windowAlways")
}

function bytes(value: number, locale: string): string {
  const units = ["byte", "kilobyte", "megabyte", "gigabyte", "terabyte"] as const
  let v = value
  let i = 0
  while (v >= 1000 && i < units.length - 1) {
    v /= 1000
    i++
  }
  return new Intl.NumberFormat(locale, { style: "unit", unit: units[i], unitDisplay: "short", maximumFractionDigits: v < 10 && i > 0 ? 1 : 0 }).format(v)
}

function monthName(period: string, locale: string): string {
  return new Intl.DateTimeFormat(locale, { timeZone: "UTC", year: "numeric", month: "long" }).format(new Date(period + "-01T00:00:00Z"))
}

function UsageCard({ heading, period, say, locale, n, current = false, asOf }: { heading: string; period: UsagePeriod; say: Say; locale: string; n: Intl.NumberFormat; current?: boolean; asOf: string }) {
  const rows: [string, number | null, (v: number) => string][] = [
    [say("usageActive"), period.active_accounts, (v) => n.format(v)],
    [say("usageDispatches"), period.counters.dispatches, (v) => n.format(v)],
    [say("usageMinutes"), period.counters.connect_minutes, (v) => n.format(v)],
    [say("usageIn"), period.counters.bytes_in, (v) => bytes(v, locale)],
    [say("usageOut"), period.counters.bytes_out, (v) => bytes(v, locale)],
  ]
  return (
    <div className="adm-panel adm-usage-card">
      <div className="adm-usage-head">
        <h2 className="adm-h3">{heading}<span className="adm-month">{monthName(period.period, locale)}</span></h2>
        {period.coverage === "partial" ? <span className="adm-status adm-status-warn"><span className="adm-dot" aria-hidden="true" />{say("usagePartial")}</span> : null}
      </div>
      <p className="adm-fine">{current ? say("usageInProgress", { time: utcMinute(asOf) }) : say("usageEnded")}</p>
      {period.coverage === "partial" ? <p className="adm-fine">{say("usagePartialNote")}</p> : null}
      <dl className="adm-dl">
        {rows.map(([label, value, format]) => (
          <div key={label} className="adm-dl-row">
            <dt>{label}</dt>
            <dd>
              {value === null
                ? <span className="adm-unknown" title={say("usageUnknownLabel")}><span aria-hidden="true">— </span>{say("usageUnknown")}</span>
                : format(value)}
            </dd>
          </div>
        ))}
      </dl>
    </div>
  )
}
