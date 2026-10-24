/* The connection-health page inside the admin shell: the window and pause
 * controls, the whole-page states, and the screen the address names, drawn from
 * the Cloud's stored operator reads (`/su/v1/health/…`). It owns no session: the
 * shell signs in, and a refused read here asks the shell to sign in again.
 *
 * The draft snapshot screens (screens.tsx) are the full-product design; they are
 * not reachable from a route until a producer and the Cloud can fill them. */
import { useEffect, useMemo, useRef } from "react"
import "./health.css"
import { healthWords, type HealthLocale, type HealthWord } from "./words"
import { HEALTH_UI_LIMITS, type Refusal } from "./cloud"
import { useStored } from "./useStored"
import { ProbeScreen } from "./probe-view"
import { healthHref, isProbeView, STORED_WINDOWS_S, TREND_RANGE_WORDS, type HealthRoute, type HealthScope, type HealthScreen, type TrendRangeWord } from "./route"
import { TrendScreen } from "./trend-view"
import { exactUTC, StoredScreen, type StoredNav } from "./stored-screens"
import { AttentionPanel } from "./attention-view"

export type ConnectionHealthProps = {
  route: HealthRoute
  /** In-place navigation to an address under /admin; the shell owns history. */
  go: (href: string) => void
  /** Where the operator reads go. Production passes the declared Cloud API origin. */
  apiOrigin: string
  /** The fetch the reads use. Production passes the browser's; the fixture harness its own. */
  get: typeof fetch
  /** True only for the development fixture harness: every view says so. */
  fixture: boolean
  /** The session was refused: the shell decides what signing in again means. */
  onRefused: (refusal: Refusal) => void
  /** Explicit locale for tests; the page reads the public catalog otherwise. */
  locale?: HealthLocale
  /** The page was reached by a move inside admin, so its heading takes the focus at once. */
  arrivedByMove?: boolean
}

export function ConnectionHealth({ route, go, apiOrigin, get, fixture, onRefused, locale, arrivedByMove = false }: ConnectionHealthProps) {
  const say = useMemo(() => healthWords(locale), [locale])
  const { state, refresh, loadOlder, loadOlderIncidents, refuse } = useStored(get, apiOrigin, route)
  const actions = useMemo(() => ({ get, apiOrigin, refresh, refuse, loadOlderIncidents }), [get, apiOrigin, refresh, refuse, loadOlderIncidents])
  const heading = useRef<HTMLHeadingElement>(null)
  const scope = route.scope
  const nav: StoredNav = useMemo(() => ({ href: (s: HealthScreen) => healthHref(s, scope), go: (s: HealthScreen) => go(healthHref(s, scope)) }), [go, scope])
  const setScope = (next: Partial<HealthScope>) => go(healthHref(route, { ...scope, ...next }))
  // The probe subview reads latest evidence only: no window, trend or account control applies.
  const probes = isProbeView(route)
  const trendable = !probes && (route.screen === "M01" || route.screen === "M03" || route.screen === "M06")
  const trend = trendable ? route.trend ?? null : null
  const trendMachine = trendable ? route.trendMachine ?? null : null
  // The trend range is part of the address: a change is a new query, chart and history entry.
  const setTrend = (next: TrendRangeWord | null) => go(healthHref({ ...route, trend: next, ...(next === null ? { trendMachine: null } : {}) } as HealthScreen, scope))

  useEffect(() => {
    if (state.status === "refused" && state.refusal) onRefused(state.refusal)
  }, [state.status, state.refusal, onRefused])

  // A move between screens puts the focus on this page's heading; a refresh or a scope change does not.
  const first = useRef(!arrivedByMove)
  // An incident's revisions are one screen: moving between them keeps the focus where it is.
  const screenKey = route.screen === "M02" ? `M02|${route.machine ?? ""}` : route.screen === "M04" ? `M04|${route.trace}` : route.screen === "M05" ? `M05|${route.incident ?? ""}` : route.screen
  useEffect(() => {
    if (first.current) { first.current = false; return }
    heading.current?.focus({ preventScroll: true })
  }, [screenKey])

  const title = say(`screen_${route.screen}` as HealthWord)
  const anyOk = Object.values(state.parts).some((p) => p?.status === "ok")
  const allFailed = state.status === "ready" && !anyOk

  return (
    <div className="adh">
      {fixture ? <p className="adh-example" role="note">{say("st_fixtureBanner")}</p> : null}
      <header className="adh-head">
        <h1 className="adh-title" tabIndex={-1} ref={heading}>{title}</h1>
        <p className="adm-sub">{say(`lead_${route.screen}` as HealthWord)}</p>
      </header>

      {trendable || probes ? (
        <nav className="adh-switch adh-view-switch" aria-label={say("tr_viewLabel")}>
          {([
            ["raw", !probes && trend === null, probes ? { screen: "M06" } : { ...route, trend: null, trendMachine: null }, "tr_viewRaw"],
            ["trend", !probes && trend !== null, probes ? { screen: "M06", trend: "30d" } : { ...route, trend: trend ?? "30d" }, "tr_viewTrend"],
            ...(route.screen === "M06" ? [["probes", probes, { screen: "M06", probes: true }, "pr_view"]] : []),
          ] as [string, boolean, HealthScreen, HealthWord][]).map(([k, on, target, label]) => {
            const href = healthHref(target, scope)
            return (
              <a key={k} className="adh-switch-btn" aria-current={on ? "page" : undefined} href={href}
                onClick={(e) => { if (e.button === 0 && !e.metaKey && !e.ctrlKey && !e.shiftKey && !e.altKey) { e.preventDefault(); go(href) } }}>{say(label)}</a>
            )
          })}
        </nav>
      ) : null}
      <div className="adh-controls">
        {trend !== null ? (
          <fieldset className="adh-control adh-windows">
            <legend>{say("tr_rangeLabel")}</legend>
            {TREND_RANGE_WORDS.map((r) => (
              <label key={r} className={trend === r ? "adh-window adh-window-on" : "adh-window"}>
                <input type="radio" name="adh-trend" value={r} checked={trend === r} onChange={() => setTrend(r)} />
                {say(`tr_range_${r}` as HealthWord)}
              </label>
            ))}
          </fieldset>
        ) : probes ? null : (
        <fieldset className="adh-control adh-windows">
          <legend>{say("windowLabel")}</legend>
          {STORED_WINDOWS_S.map((w) => (
            <label key={w} className={scope.window_s === w ? "adh-window adh-window-on" : "adh-window"}>
              <input type="radio" name="adh-window" value={w} checked={scope.window_s === w} onChange={() => setScope({ window_s: w })} />
              {say(`window_${w}` as HealthWord)}
            </label>
          ))}
        </fieldset>
        )}
        <div className="adh-control adh-live">
          <button type="button" className="adm-btn adm-btn-small" aria-pressed={scope.until !== null}
            onClick={() => setScope({ until: scope.until === null ? new Date().toISOString() : null })}>
            {scope.until === null ? say("pause") : say("resume")}
          </button>
          <button type="button" className="adm-btn adm-btn-small adm-btn-quiet" onClick={refresh} disabled={state.refreshing} aria-busy={state.refreshing}>
            {state.refreshing ? say("refreshing") : say("refresh")}
          </button>
        </div>
      </div>
      <p className="adm-fine">{say("scopeNote_cloud_operator")}</p>
      {trend !== null && trendMachine ? (
        <p className="adm-fine">{say("tr_machineFilter", { machine: trendMachine })} <a className="adh-link" href={healthHref({ ...route, trendMachine: null } as HealthScreen, scope)} onClick={(e) => { e.preventDefault(); go(healthHref({ ...route, trendMachine: null } as HealthScreen, scope)) }}>{say("st_clearFilter")}</a></p>
      ) : null}
      {scope.account !== null ? (
        <p className="adm-fine">{say("st_accountFilter", { account: scope.account })} <a className="adh-link" href={healthHref(route, { ...scope, account: null })} onClick={(e) => { e.preventDefault(); setScope({ account: null }) }}>{say("st_clearFilter")}</a></p>
      ) : null}
      {probes ? (
        <p className="adm-fine">{scope.until === null ? say("st_liveEvery", { s: HEALTH_UI_LIMITS.refreshMs / 1000 }) : null}</p>
      ) : <p className="adm-fine">
        {state.window ? say("st_window", { from: exactUTC(state.window.from), to: exactUTC(state.window.to) }) : null}
        {" "}{scope.until === null ? say("st_liveEvery", { s: HEALTH_UI_LIMITS.refreshMs / 1000 }) : say("st_pinned")}
      </p>}

      {route.screen === "M01" && !fixture ? <AttentionPanel apiOrigin={apiOrigin} get={get} say={say} go={go} /> : null}

      {state.status === "loading" ? (
        <div className="adm-skeleton" aria-busy="true">
          <p className="adm-skeleton-say" role="status">{say("loading")}</p>
          <div className="adm-tile adm-shimmer" />
          <div className="adm-panel adm-shimmer adm-shimmer-tall" />
        </div>
      ) : state.status === "refused" ? (
        <div className="adm-notice adm-notice-wide" role="alert">
          <h2>{say("failedTitle")}</h2>
          <p>{say(state.refusal === "signed_out" ? "st_refusedSignedOut" : "st_refusedForbidden")}</p>
        </div>
      ) : allFailed ? (
        <div className="adm-notice adm-notice-wide" role="alert">
          <h2>{say("failedTitle")}</h2>
          <p>{say("failedBody")}</p>
          <ul className="adh-fail-list">
            {Object.entries(state.parts).map(([name, p]) => p && p.status === "failed" ? (
              <li key={name}>{say(`st_part_${name}` as HealthWord)}: {say(`st_fail_${p.code}` as HealthWord)} <span className="adm-code">({p.code})</span></li>
            ) : null)}
          </ul>
          <div className="adm-notice-actions">
            <button type="button" className="adm-btn" onClick={refresh} disabled={state.refreshing}>{say("tryAgain")}</button>
          </div>
        </div>
      ) : (
        probes ? <ProbeScreen state={state} route={route} say={say} /> : trend !== null ? <TrendScreen state={state} route={route} say={say} go={go} /> : <StoredScreen state={state} route={route} say={say} nav={nav} fixture={fixture} loadOlder={loadOlder} actions={actions} />
      )}
    </div>
  )
}
