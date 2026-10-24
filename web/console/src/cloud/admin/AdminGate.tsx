import { SurveyStatistics } from "../../onboarding/Statistics.js"
import { lazy, Suspense, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react"
import { chooseTransport, cloudViewerDeviceMetadata, newCloudSession, readCloudConfig, type CloudConfig, type EnsuredSession } from "../copied.js"
import { DeviceLimitPanel } from "../DeviceLimitPanel.js"
import { currentCatalogTag } from "../../catalog.js"
import { AccountsPage, OverviewPage, RouteLink, UsagePage, type Go } from "./AdminDashboard.js"
import { PromoManagement } from "./PromoManagement.js"
import { DemoPromotions } from "./DemoPromotions.js"
import { demoOverview } from "./demo.js"
import { mayLeaveCode } from "./navigation-guard.js"
import { loadOverview, type Overview } from "./api.js"
import { ADMIN_ROUTES, adminPath, adminRoute, adminWords, bootAdminCatalog, type AdminRoute, type Say } from "./words.js"
// Loaded on first visit, so the other operations pages do not carry its screens and copy.
const ConnectionHealth = lazy(() => import("./health/ConnectionHealth.js").then((m) => ({ default: m.ConnectionHealth })))
import { healthRoute } from "./health/route.js"
import type { Refusal } from "./health/cloud.js"
import "./admin.css"
import { BrowserLanguageControl } from "../../ui-language/BrowserLanguageControl.js"
import { BRAND_MARK } from "../../brand-mark.js"

/*
 * The hosted console's `/admin`: Cloud's operations console, for the accounts
 * the API names superusers. Four pages — overview, accounts, promotions,
 * usage — each at its own address under `/admin`, behind one sign-in; a
 * sidebar on a desktop, a drawer behind a menu button on a phone. It stands apart from `CloudGate` on purpose — an
 * operator with no machine paired, or a relay that is down, still reaches it —
 * so it signs in through the same revocable cookie session and nothing else:
 * no relay, no pairing. Who may see the numbers is the API's decision, made on
 * the GitHub identity behind the session; this page only draws its answer.
 */

type SessionHandle = ReturnType<typeof newCloudSession>

/*
 * The connection-health fixture harness: development builds only, and only when
 * VITE_ADMIN_HEALTH_FIXTURE names a scenario (health/fixture.ts). It skips the
 * session, answers every health read from task-owned example data in memory and
 * says so on every view. A production build drops the branch and the module.
 */
const HEALTH_FIXTURE: string | null = import.meta.env.DEV && import.meta.env.VITE_ADMIN_HEALTH_FIXTURE
  ? String(import.meta.env.VITE_ADMIN_HEALTH_FIXTURE)
  : null

type Phase =
  | { kind: "misdeclared" }
  | { kind: "checking" }
  | { kind: "sign_in"; url: string }
  | { kind: "device_limit"; tier: string; limit: number | null }
  | { kind: "session_failed"; code: string }
  | { kind: "loading" }
  | { kind: "forbidden" }
  | { kind: "failed"; code: string }
  | { kind: "ready"; overview: Overview; refreshing: boolean; stale: { code: string } | null }
  | { kind: "fixture"; get: typeof fetch }

function configFrom(declared: string): CloudConfig | null {
  try {
    const config = readCloudConfig({ __clawdlineCloud: JSON.parse(declared) })
    if (!config) return null
    return chooseTransport({ mock: false, origin: location.origin, config }) === "cloud" ? config : null
  } catch {
    return null
  }
}

function failureCode(error: unknown): string {
  const code = error && typeof error === "object" && "code" in error ? (error as { code: unknown }).code : null
  return typeof code === "string" && code ? code : "unavailable"
}

export function AdminGate({ declared }: { declared: string }) {
  const config = useMemo(() => configFrom(declared), [declared])
  const [locale, setLocale] = useState(currentCatalogTag())
  const say = useMemo(() => adminWords(), [])
  const [phase, setPhase] = useState<Phase>(config || HEALTH_FIXTURE ? { kind: "checking" } : { kind: "misdeclared" })
  const [catalogReady, setCatalogReady] = useState(false)
  const [demo, setDemo] = useState(false)
  const sample = useMemo(() => demoOverview(), [])
  const session = useRef<SessionHandle | null>(null)
  const generation = useRef(0)

  useLayoutEffect(() => {
    if (catalogReady) document.documentElement.classList.remove("booting")
  }, [catalogReady])

  useEffect(() => {
    let active = true
    void bootAdminCatalog(config, document.baseURI).then((selected) => {
      if (!active) return
      setLocale(selected)
      setCatalogReady(true)
    })
    return () => { active = false }
  }, [config])

  const fetchOverview = useCallback(async (keep: Overview | null) => {
    if (!config) return
    const mine = ++generation.current
    if (!keep) setPhase({ kind: "loading" })
    const result = await loadOverview((url, init) => fetch(url, init), config.apiOrigin)
    if (mine !== generation.current) return
    switch (result.kind) {
      case "ready":
        setPhase({ kind: "ready", overview: result.overview, refreshing: false, stale: null })
        return
      case "signed_out":
        setPhase({ kind: "sign_in", url: session.current?.signInURL() ?? config.apiOrigin })
        return
      case "forbidden":
        setPhase({ kind: "forbidden" })
        return
      case "failed":
        // A refresh that fails keeps the figures already confirmed, dated, and
        // says so; a first load that fails shows none rather than zeros.
        setPhase(keep
          ? { kind: "ready", overview: keep, refreshing: false, stale: { code: result.code } }
          : { kind: "failed", code: result.code })
    }
  }, [config])

  const start = useCallback(async () => {
    if (HEALTH_FIXTURE) {
      const { fixtureFetch } = await import("./health/fixture.js")
      setPhase({ kind: "fixture", get: fixtureFetch(HEALTH_FIXTURE) })
      return
    }
    if (!config) return
    const mine = ++generation.current
    setPhase({ kind: "checking" })
    const device = cloudViewerDeviceMetadata(window)
    const next = newCloudSession({
      config,
      deviceKind: device.kind,
      deviceName: device.name,
      // The sign-in comes back to the page it left, so a deep link survives it.
      returnTo: config.appOrigin + (adminRoute(location.pathname) ? location.pathname + location.search : adminPath("overview")),
    })
    session.current = next
    let ensured: EnsuredSession
    try {
      ensured = await next.ensureSession()
    } catch (error) {
      if (mine !== generation.current) return
      const code = failureCode(error)
      // A device the control plane revoked, or no longer lists, is a session
      // that has to be made again: signing in is the way, not a retry.
      setPhase(code === "revoked" ? { kind: "sign_in", url: next.signInURL() } : { kind: "session_failed", code })
      return
    }
    if (mine !== generation.current) return
    if (ensured.state === "sign_in") setPhase({ kind: "sign_in", url: ensured.url })
    else if (ensured.state === "device_limit_reached") setPhase({ kind: "device_limit", tier: ensured.tier, limit: ensured.limit })
    else await fetchOverview(null)
  }, [config, fetchOverview])

  useEffect(() => {
    void start()
    return () => {
      generation.current++
    }
  }, [start])

  const refresh = useCallback(() => {
    if (phase.kind !== "ready") {
      void start()
      return
    }
    setPhase({ ...phase, refreshing: true })
    void fetchOverview(phase.overview)
  }, [phase, start, fetchOverview])

  // A mutation elsewhere changed the totals: read them again behind the page.
  const latest = useRef(phase)
  latest.current = phase
  const reread = useCallback(() => {
    const current = latest.current
    if (current.kind !== "ready") return
    setPhase({ ...current, refreshing: true })
    void fetchOverview(current.overview)
  }, [fetchOverview])

  const { route, pathname, search, go, moved } = useAdminRoute()
  const consoleHref = config ? config.appOrigin + "/" : "/"
  // Connection health reads its own routes, so an overview that failed to load does not hide it.
  const navigable = phase.kind === "ready" || phase.kind === "loading" || phase.kind === "fixture" || (phase.kind === "failed" && route === "health")
  const health = route === "health" ? healthRoute(pathname, search) : null
  // A refused health read ends what this page may show: signed out signs in again, forbidden says so.
  const healthRefused = useCallback((refusal: Refusal) => {
    if (refusal === "forbidden") setPhase({ kind: "forbidden" })
    else void start()
  }, [start])
  const healthFetch = useMemo<typeof fetch>(() => (url, init) => fetch(url, init), [])
  const pageTitle = say(`nav_${route}`)

  useEffect(() => {
    document.title = navigable ? say("docTitlePage", { page: pageTitle }) : say("docTitle")
  }, [navigable, pageTitle, say, locale])

  if (!navigable) {
    return (
      <div className="adm adm-plain">
        <header className="adm-bar">
          <div className="adm-bar-in">
            <a className="adm-back" href={consoleHref}>
              <span aria-hidden="true">←</span> {say("back")}
            </a>
            <div className="adm-heading">
              <h1>{say("title")}</h1>
              <p className="adm-sub">{say("subtitle")}</p>
            </div>
          </div>
        </header>
        <main className="adm-main" aria-live="polite" aria-busy={phase.kind === "checking"}>
          <Body phase={phase} say={say} session={session.current} onRetry={start} catalogReady={catalogReady} />
        </main>
      </div>
    )
  }

  if (route === "health") {
    return (
      <Shell route={route} go={go} say={say} consoleHref={consoleHref} stampText={null} refreshing={false} onRefresh={null}>
        <Suspense fallback={<Skeleton label={say("loading")} />}>
        {health === null ? (
          <Notice title={say("failedTitle")} body={say("healthUnknownAddress")}>
            <RouteLink href={adminPath("health")} go={go} className="adm-btn" current={false}>{say("nav_health")}</RouteLink>
          </Notice>
        ) : phase.kind === "fixture" ? (
          <ConnectionHealth route={health} go={go} apiOrigin="fixture:" get={phase.get} fixture onRefused={() => undefined} arrivedByMove={moved > 0} />
        ) : config ? (
          <ConnectionHealth route={health} go={go} apiOrigin={config.apiOrigin} get={healthFetch} fixture={false} onRefused={healthRefused} arrivedByMove={moved > 0} />
        ) : null}
        </Suspense>
      </Shell>
    )
  }

  if (phase.kind === "fixture") {
    return (
      <Shell route={route} go={go} say={say} consoleHref={consoleHref} stampText={null} refreshing={false} onRefresh={null}>
        <PageHead route={route} say={say} moved={moved} />
        <Notice title={say("fixtureOnlyTitle")} body={say("fixtureOnlyBody")} />
      </Shell>
    )
  }

  return (
    <Shell route={route} go={go} say={say} consoleHref={consoleHref}
      stampText={phase.kind === "ready" ? demo ? say("demoStamp") : say("generated", { time: stamp(phase.overview.generated_at, locale) }) : null}
      refreshing={phase.kind === "ready" && phase.refreshing} onRefresh={phase.kind === "ready" && !demo ? refresh : null}>
      <PageHead route={route} say={say} moved={moved} />
      {phase.kind === "ready" ? (
        <div className={demo ? "adm-demo-control adm-demo-active" : "adm-demo-control"}>
          <div><strong>{demo ? say("demoTitle") : say("liveTitle")}</strong><p>{demo ? say("demoDescription") : say("liveDescription")}</p></div>
          <button type="button" className="adm-btn adm-btn-small" aria-pressed={demo} onClick={() => { if (mayLeaveCode()) setDemo((value) => !value) }}>
            {demo ? say("showLive") : say("showDemo")}
          </button>
        </div>
      ) : null}
      {phase.kind !== "ready" ? (
        <Skeleton label={say("loading")} />
      ) : (
        <>
          {!demo && phase.stale ? (
            <p className="adm-stale" role="status">
              {say("staleNote", { code: phase.stale.code, time: new Intl.DateTimeFormat(locale, { timeZone: "UTC", hour: "2-digit", minute: "2-digit", hourCycle: "h23" }).format(new Date(phase.overview.generated_at)) })}
            </p>
          ) : null}
          {route === "overview" ? <OverviewPage overview={demo ? sample : phase.overview} say={say} locale={locale} go={go} /> : null}
          {route === "survey" && config ? <SurveyStatistics apiOrigin={config.apiOrigin} demo={demo} refreshToken={phase.overview.generated_at} /> : null}
          {route === "accounts" ? <AccountsPage overview={demo ? sample : phase.overview} say={say} locale={locale} go={go} /> : null}
          {route === "usage" ? <UsagePage overview={demo ? sample : phase.overview} say={say} locale={locale} go={go} /> : null}
          {route === "promotions" && demo ? <DemoPromotions overview={sample} say={say} locale={locale} /> : null}
          {route === "promotions" && !demo && config ? (
            <PromoManagement overview={phase.overview} apiOrigin={config.apiOrigin} say={say} locale={locale}
              search={search} go={go} onChanged={reread} onLostAccess={() => void start()} />
          ) : null}
        </>
      )}
    </Shell>
  )
}

/* ---- where the page is ---------------------------------------------------- */

/**
 * The route from the address, kept in step with Back and Forward. `moved`
 * counts the moves after the first draw, so the page heading takes the focus
 * on a move and not on arrival.
 */
function useAdminRoute() {
  const read = () => ({ route: adminRoute(location.pathname), pathname: location.pathname, search: location.search })
  const [where, setWhere] = useState(read)
  const [moved, setMoved] = useState(0)
  const currentAddress = useRef(location.pathname + location.search)

  useEffect(() => {
    // An address under /admin that names no page opens the overview in its place.
    if (where.route === null) {
      history.replaceState(history.state, "", adminPath("overview"))
      setWhere(read())
    }
    const onPop = () => {
      if (!mayLeaveCode()) {
        history.pushState(null, "", currentAddress.current)
        return
      }
      currentAddress.current = location.pathname + location.search
      setWhere(read())
      setMoved((m) => m + 1)
    }
    addEventListener("popstate", onPop)
    return () => removeEventListener("popstate", onPop)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const go = useCallback((href: string) => {
    const target = new URL(href, location.href)
    if (target.pathname + target.search === location.pathname + location.search) return
    if (!mayLeaveCode()) return
    history.pushState(null, "", target.pathname + target.search)
    currentAddress.current = target.pathname + target.search
    setWhere(read())
    setMoved((m) => m + 1)
    scrollTo({ top: 0 })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  return { route: where.route ?? "overview", pathname: where.pathname, search: where.search, go, moved }
}

function useNarrow(): boolean {
  const query = "(max-width: 899px)"
  const [narrow, setNarrow] = useState(() => matchMedia(query).matches)
  useEffect(() => {
    const list = matchMedia(query)
    const on = () => setNarrow(list.matches)
    list.addEventListener("change", on)
    return () => list.removeEventListener("change", on)
  }, [])
  return narrow
}

function PageHead({ route, say, moved }: { route: AdminRoute; say: Say; moved: number }) {
  const heading = useRef<HTMLHeadingElement>(null)
  // A move inside the page is announced by putting the focus on its heading,
  // which a screen reader reads, and from where Tab continues into the page.
  useEffect(() => {
    if (moved > 0) heading.current?.focus({ preventScroll: true })
  }, [moved, route])
  return (
    <header className="adm-pagehead">
      <h1 ref={heading} tabIndex={-1}>{say(`nav_${route}`)}</h1>
      <p className="adm-sub">{say(`lead_${route}`)}</p>
    </header>
  )
}

function Shell({ route, go, say, consoleHref, stampText, refreshing, onRefresh, children }: {
  route: AdminRoute
  go: Go
  say: Say
  consoleHref: string
  stampText: string | null
  refreshing: boolean
  onRefresh: (() => void) | null
  children: React.ReactNode
}) {
  const narrow = useNarrow()
  const [open, setOpen] = useState(false)
  const drawer = useRef<HTMLElement>(null)
  const menu = useRef<HTMLButtonElement>(null)
  const closeButton = useRef<HTMLButtonElement>(null)
  const drawerOpen = narrow && open

  const close = useCallback((restore: boolean) => {
    setOpen(false)
    if (restore) requestAnimationFrame(() => menu.current?.focus())
  }, [])

  useEffect(() => {
    if (!narrow) setOpen(false)
  }, [narrow])

  useEffect(() => {
    if (!drawerOpen) return
    const before = document.body.style.overflow
    document.body.style.overflow = "hidden"
    const current = drawer.current?.querySelector<HTMLElement>('[aria-current="page"]')
    ;(current ?? closeButton.current)?.focus()
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault()
        close(true)
        return
      }
      if (event.key !== "Tab" || !drawer.current) return
      // The rest of the page is inert while the drawer is open; Tab cycles in it.
      const items = [...drawer.current.querySelectorAll<HTMLElement>("a[href], button:not([disabled])")]
      if (!items.length) return
      const first = items[0]
      const last = items[items.length - 1]
      if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus() }
      else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus() }
    }
    document.addEventListener("keydown", onKey)
    return () => {
      document.body.style.overflow = before
      document.removeEventListener("keydown", onKey)
    }
  }, [drawerOpen, close])

  const follow: Go = useCallback((href: string) => {
    // Choosing a page closes the drawer; the page heading then takes the focus.
    setOpen(false)
    go(href)
  }, [go])

  return (
    <div className={drawerOpen ? "adm adm-shell adm-drawer-open" : "adm adm-shell"}>
      <a className="adm-skip" href="#adm-main">{say("skipToContent")}</a>
      <header className="adm-top" inert={drawerOpen || undefined}>
        <button ref={menu} type="button" className="adm-menu" aria-expanded={drawerOpen} aria-controls="adm-side" onClick={() => setOpen(true)}>
          <span className="adm-menu-icon" aria-hidden="true"><span /><span /><span /></span>
          <span className="adm-visually-hidden">{say("menuOpen")}</span>
        </button>
        <p className="adm-top-title"><span className="adm-top-brand">{say("title")}</span><span aria-hidden="true"> / </span>{say(`nav_${route}`)}</p>
        {onRefresh ? (
          <button type="button" className="adm-btn adm-btn-quiet adm-btn-small" onClick={onRefresh} disabled={refreshing} aria-busy={refreshing}>
            {refreshing ? say("refreshing") : say("refresh")}
          </button>
        ) : null}
      </header>
      {drawerOpen ? <div className="adm-scrim" aria-hidden="true" onClick={() => close(true)} /> : null}
      <aside id="adm-side" ref={drawer} className="adm-side"
        role={narrow ? "dialog" : undefined} aria-modal={drawerOpen || undefined} aria-label={narrow ? say("menuLabel") : undefined}
        aria-hidden={narrow && !open ? true : undefined}>
        <div className="adm-brand">
          <svg className="adm-brand-mark" viewBox="0 0 8 4" aria-hidden="true" focusable="false" shapeRendering="crispEdges">
            {BRAND_MARK.cells.flatMap((row, y) => row.map((fill, x) => fill ? (
              <rect key={`${x}-${y}`} x={x} y={y} width="1" height="1" fill={fill} />
            ) : null))}
          </svg>
          <span className="adm-brand-text">
            <span className="adm-brand-name">Clawdline Cloud</span>
            <span className="adm-brand-sub">{say("title")}</span>
          </span>
          {narrow ? (
            <button ref={closeButton} type="button" className="adm-close" onClick={() => close(true)}>
              <span aria-hidden="true">×</span><span className="adm-visually-hidden">{say("menuClose")}</span>
            </button>
          ) : null}
        </div>
        <nav className="adm-nav" aria-label={say("navLabel")}>
          <ul>
            {ADMIN_ROUTES.map((r) => (
              <li key={r}>
                <RouteLink href={adminPath(r)} go={follow} className={r === route ? "adm-nav-link adm-nav-on" : "adm-nav-link"} current={r === route}>
                  <span className={"adm-nav-icon adm-nav-icon-" + r} aria-hidden="true" />
                  <span className="adm-nav-text">
                    <span className="adm-nav-label">{say(`nav_${r}`)}</span>
                    <span className="adm-nav-hint">{say(`hint_${r}`)}</span>
                  </span>
                </RouteLink>
              </li>
            ))}
          </ul>
        </nav>
        <div className="adm-side-foot">
          <BrowserLanguageControl id="admin-ui-language" />
          {stampText ? <p className="adm-stamp">{stampText}</p> : null}
          {onRefresh && !narrow ? (
            <button type="button" className="adm-btn adm-btn-quiet adm-btn-block" onClick={onRefresh} disabled={refreshing} aria-busy={refreshing}>
              {refreshing ? say("refreshing") : say("refresh")}
            </button>
          ) : null}
          <a className="adm-back" href={consoleHref}>
            <span aria-hidden="true">←</span> {say("back")}
          </a>
        </div>
      </aside>
      <div className="adm-content" inert={drawerOpen || undefined}>
        <main id="adm-main" className="adm-main" tabIndex={-1}>
          {children}
        </main>
      </div>
    </div>
  )
}

function stamp(iso: string, locale: string): string {
  return new Intl.DateTimeFormat(locale, {
    timeZone: "UTC", year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", hourCycle: "h23",
  }).format(new Date(iso))
}

function Body({ phase, say, session, onRetry, catalogReady }: {
  phase: Phase
  say: Say
  session: SessionHandle | null
  onRetry: () => void
  catalogReady: boolean
}) {
  switch (phase.kind) {
    case "misdeclared":
      return <Notice title={say("failedTitle")} body={say("misdeclared")} />
    case "checking":
    case "loading":
      return <Skeleton label={say(phase.kind === "checking" ? "checking" : "loading")} />
    case "sign_in":
      return (
        <Notice title={say("signInTitle")} body={say("signInBody")}>
          <a className="adm-btn adm-btn-go" href={phase.url}>{say("signIn")}</a>
        </Notice>
      )
    case "device_limit":
      if (!catalogReady) return <Skeleton label={say("loading")} />
      return (
        <section className="adm-notice adm-notice-wide">
          <DeviceLimitPanel session={session} tier={phase.tier} limit={phase.limit} onContinue={onRetry} signInLabel={say("deviceLimitSignIn")} />
        </section>
      )
    case "session_failed":
      return (
        <Notice title={say("sessionFailedTitle")} body={say("failedBody")} code={say("failedCode", { code: phase.code })}>
          <button type="button" className="adm-btn adm-btn-go" onClick={onRetry}>{say("tryAgain")}</button>
        </Notice>
      )
    case "forbidden":
      return (
        <Notice title={say("forbiddenTitle")} body={say("forbiddenBody")}>
          {session ? <a className="adm-btn" href={session.signInURL()}>{say("signIn")}</a> : null}
        </Notice>
      )
    case "failed":
      return (
        <Notice title={say("failedTitle")} body={say("failedBody")} code={say("failedCode", { code: phase.code })}>
          <button type="button" className="adm-btn adm-btn-go" onClick={onRetry}>{say("tryAgain")}</button>
        </Notice>
      )
    case "ready":
    case "fixture":
      // Drawn by the shell instead.
      return null
  }
}

function Notice({ title, body, code, children }: { title: string; body: string; code?: string; children?: React.ReactNode }) {
  const heading = useRef<HTMLHeadingElement>(null)
  // A state that replaces the page takes the focus, so a keyboard or a screen
  // reader lands on what changed rather than on a button that went away.
  useEffect(() => heading.current?.focus({ preventScroll: true }), [title])
  return (
    <section className="adm-notice">
      <h2 ref={heading} tabIndex={-1}>{title}</h2>
      <p>{body}</p>
      {code ? <p className="adm-code">{code}</p> : null}
      {children ? <div className="adm-notice-actions">{children}</div> : null}
    </section>
  )
}

function Skeleton({ label }: { label: string }) {
  return (
    <div className="adm-skeleton" role="status">
      <span className="adm-visually-hidden">{label}</span>
      <p className="adm-skeleton-say" aria-hidden="true">{label}</p>
      <div className="adm-tiles" aria-hidden="true">
        {[0, 1, 2].map((i) => <div key={i} className="adm-tile adm-shimmer" />)}
      </div>
      <div className="adm-panel adm-shimmer adm-shimmer-tall" aria-hidden="true" />
    </div>
  )
}
