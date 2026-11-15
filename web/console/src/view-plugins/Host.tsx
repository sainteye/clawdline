import { useEffect, useLayoutEffect, useRef, useState } from "react"
import { createPortal } from "react-dom"
import { useStatusDockAvailable } from "../project-assistants/BotOrb.js"
import type { ViewSource } from "./bridge.js"
import { PluginError, parsePackage, type ViewPackage } from "./package.js"
import { installedPackages, subscribePackages, installPackage, enablePackage, removePackage, rollbackPackage, type Installed } from "./store.js"
import { PLUGIN_LIMITS as B } from "./limits.js"
import { PluginFrame, readPluginSource, type ViewGrant } from "./Frame.js"
import { pluginWords } from "./words.js"
import { ViewArtwork, ViewPluginIcon, exampleCover, packageCover } from "./Artwork.js"
import { fleetPluginSource } from "./sources.js"
import "./plugins.css"

const code = (e: unknown) => e instanceof PluginError ? e.code : "storage_unavailable"
const name = (p: ViewPackage, language: string) => p.manifest.name[language.startsWith("zh") ? "zh-Hant" : "en"]

export function useViewPlugins(sources: readonly ViewSource[], language: string) {
  const [opened, setOpened] = useState(false), openedRef = useRef(false), dock = useStatusDockAvailable()
  const [mode, setMode] = useState<"manage" | "launch">("launch")
  const [rows, setRows] = useState<Installed[]>([]), [error, setError] = useState("")
  const [busy, setBusy] = useState(false), [review, setReview] = useState<{ archive: ArrayBuffer; pkg: ViewPackage; expected: string | null } | null>(null)
  const [grant, setGrant] = useState<ViewGrant | null>(null)
  const modal = useRef<HTMLDivElement>(null), opener = useRef<HTMLElement | null>(null)
  const owned = useRef<string | null>(null), navigate = useRef<(() => void) | null>(null)
  const closing = useRef(false)
  const operations = useRef(0)
  const w = pluginWords(language), source = fleetPluginSource(sources, w.everything)
  const liveSource = useRef(source); liveSource.current = source
  const finish = () => {
    openedRef.current = false; owned.current = null; closing.current = false; operations.current++
    setOpened(false); setGrant(null); setReview(null); setBusy(false)
    const next = navigate.current; navigate.current = null
    requestAnimationFrame(() => { (opener.current?.isConnected ? opener.current : document.querySelector<HTMLButtonElement>(".vp-dock,#nav-view-plugins"))?.focus({ preventScroll: true });
      try { next?.() } catch { /* A source revoked during Back never receives navigation. */ }
    })
  }
  const finishRef = useRef(finish); finishRef.current = finish
  const close = () => {
    if (closing.current || !openedRef.current) return
    closing.current = true
    setGrant(null)
    if (owned.current && history.state?.viewPlugin === owned.current) history.back()
    else finishRef.current()
  }
  const closeRef = useRef(close); closeRef.current = close
  const refresh = async () => { try { setRows(await installedPackages()) } catch (e) { setRows([]); setError(code(e)) } }
  useEffect(() => subscribePackages(() => {
    // A hint revokes the running grant immediately, before asynchronous storage verification.
    operations.current++; setGrant(null); setReview(null); setBusy(false)
    void refresh()
  }), [])
  useEffect(() => {
    if (!grant || source.key === grant.sourceKey) return
    operations.current++; setGrant(null); setBusy(false); setError("scope_changed")
  }, [source.key, grant])
  useLayoutEffect(() => {
    if (!review) return
    const panel = modal.current?.querySelector<HTMLElement>(".vp-review")
    panel?.scrollIntoView({ block: "nearest" })
    panel?.focus({ preventScroll: true })
  }, [review?.pkg.digest])
  useLayoutEffect(() => {
    if (!opened) return
    const node = modal.current!, siblings = [...document.body.children].filter(el => el !== node && el instanceof HTMLElement) as HTMLElement[]
    const prior = siblings.map(el => [el, el.inert] as const), overflow = document.body.style.overflow
    for (const [el] of prior) el.inert = true
    document.body.style.overflow = "hidden"
    node.querySelector<HTMLButtonElement>("button")?.focus()
    const pop = (e: PopStateEvent) => {
      if (!openedRef.current) return
      e.stopImmediatePropagation(); finishRef.current()
    }
    const key = (e: KeyboardEvent) => {
      if (e.key === "Escape") { e.preventDefault(); e.stopImmediatePropagation(); closeRef.current(); return }
      if (e.key !== "Tab") return
      const all = [...node.querySelectorAll<HTMLElement>("button:not([disabled]),a[href],input,select,summary,iframe")].filter(el => el.getClientRects().length)
      const first = all[0], last = all[all.length - 1]
      if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last?.focus() }
      else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first?.focus() }
    }
    window.addEventListener("popstate", pop, true); window.addEventListener("keydown", key, true)
    return () => {
      window.removeEventListener("popstate", pop, true); window.removeEventListener("keydown", key, true)
      for (const [el, inert] of prior) el.inert = inert
      document.body.style.overflow = overflow
    }
  }, [opened])
  const openRoot = (button: HTMLElement, nextMode: "manage" | "launch") => {
    if (openedRef.current) return
    opener.current = button; owned.current = crypto.randomUUID(); openedRef.current = true
    history.pushState({ ...history.state, viewPlugin: owned.current }, "", location.href)
    setError(""); setRows([]); setMode(nextMode); setOpened(true); setBusy(true)
    const generation = ++operations.current
    void installedPackages().then(installed => {
      if (!openedRef.current || generation !== operations.current) return
      setRows(installed); setBusy(false)
      const enabled = installed.filter(r => r.enabled)
      if (nextMode === "launch" && enabled.length === 1) choose(enabled[0].active)
    }, e => { if (openedRef.current && generation === operations.current) { setError(code(e)); setBusy(false) } })
  }
  const act = async (operation: () => Promise<void>) => {
    if (busy) return
    setBusy(true); setError("")
    try { await operation(); await refresh() } catch (e) { setError(code(e)) }
    finally { setBusy(false) }
  }
  const inspect = async (archive: ArrayBuffer) => {
    const generation = operations.current
    const pkg = await parsePackage(archive), current = (await installedPackages()).find(r => r.id === pkg.manifest.id)
    if (!openedRef.current || generation !== operations.current) return
    setReview({ archive, pkg, expected: current?.active.digest ?? null })
  }
  const exampleInstall = (id: string) => act(async () => {
    const answer = await fetch(`/view-plugins/${id}.zip`, { credentials: "omit", cache: "no-store" })
    if (!answer.ok || !answer.body) throw new PluginError("invalid_package")
    const reader = answer.body.getReader(), chunks: Uint8Array[] = []; let size = 0
    try { while (true) { const part = await reader.read(); if (part.done) break; size += part.value.length
      if (size > B.archiveBytes) throw new PluginError("package_too_large"); chunks.push(part.value) }
    } finally { await reader.cancel() }
    const bytes = new Uint8Array(size); let at = 0; for (const part of chunks) { bytes.set(part, at); at += part.length }
    await inspect(bytes.buffer)
  })
  const choose = (pkg: ViewPackage) => {
    setReview(null)
    void act(async () => {
      const operation = ++operations.current, current = liveSource.current, captured = current.key
      const initial = await readPluginSource(current, new AbortController().signal)
      if (operation !== operations.current || !openedRef.current || liveSource.current.key !== captured) throw new PluginError("scope_changed")
      if (initial.sessions.length > B.sessions) throw new PluginError("too_many_sessions")
      setGrant({ pkg, sourceKey: captured, initial, instanceId: crypto.randomUUID() })
    })
  }
  const entry = <button type="button" className="vp-dock" aria-label={w.entry} title={w.launcher} onClick={e => openRoot(e.currentTarget, "launch")}>
    <ViewPluginIcon />
  </button>
  const enabled = rows.filter(row => row.enabled)
  const content = opened ? createPortal(<div className="vp-host" ref={modal} role="dialog" aria-modal="true" aria-label={mode === "manage" ? w.title : w.launcher}>
    {grant ? <PluginFrame key={grant.instanceId} grant={grant} source={source} language={language} onClose={close}
      onNavigate={fn => { navigate.current = fn }} onFault={error => { setGrant(null); setError(error) }} /> : <>
      <header className="vp-bar"><div className="vp-bar-title"><ViewPluginIcon /><h1>{mode === "manage" ? w.title : w.launcher}</h1></div><button type="button" className="vp-close" aria-label={w.close} onClick={close}>✕</button></header>
      <main className={`vp-manager${mode === "launch" ? " vp-launcher" : ""}`} aria-busy={busy}>
        {error && <p className="vp-error" role="alert">{w.errors[error] || w.unavailable}</p>}
        {busy && <p className="vp-loading" role="status">{w.loading}</p>}
        {mode === "launch" ? <>
          {!busy && !enabled.length ? <div className="vp-empty"><ViewPluginIcon /><h2>{w.launchEmpty}</h2><p>{w.launchEmptyHint}</p></div> : <div className="vp-launch-list">{enabled.map(row => <button key={row.id} type="button" className="vp-launch-item" disabled={busy} onClick={() => choose(row.active)}>
            <ViewArtwork cover={packageCover(row.active)} missing={w.coverRequired} small /><span><b>{name(row.active, language)}</b><small>{w.openView}</small></span><span className="vp-launch-arrow" aria-hidden="true">↗</span>
          </button>)}</div>}
        </> : <>
          <div className="vp-intro"><div><p className="vp-eyebrow">{w.personalize}</p><h2>{w.manageHeading}</h2><p>{w.manageHint}</p></div><label className="vp-import">+ {w.install}<input className="vp-file" type="file" accept=".zip,application/zip" aria-label={w.install} disabled={busy} onChange={e => {
            const file = e.currentTarget.files?.[0]; e.currentTarget.value = ""
            if (file) void act(async () => { if (file.size > B.archiveBytes) throw new PluginError("package_too_large"); await inspect(await file.arrayBuffer()) })
          }} /></label></div>
          {review && <section className="vp-review vp-card" tabIndex={-1} aria-label={w.review}><ViewArtwork cover={review.pkg.manifest.cover} missing={w.coverRequired} small /><p className="vp-eyebrow">{w.review}</p><h2>{name(review.pkg, language)}</h2><p className="vp-package-id">{review.pkg.manifest.id} · {review.pkg.manifest.version}</p>
            <p>{w.permissions}: {w.read}{review.pkg.manifest.capabilities.includes("sessions.open") ? ` · ${w.navigate}` : ""}</p><p className="vp-muted">{w.trust}</p>
            <div className="vp-actions"><button className="vp-primary" disabled={busy} onClick={() => void act(async () => { await installPackage(review.archive, review.expected); setReview(null) })}>{review.expected ? w.replace : w.confirmInstall}</button>
            <button disabled={busy} onClick={() => setReview(null)}>{w.cancel}</button></div></section>}
          <section className="vp-library"><div className="vp-section-head"><h2>{w.installed}</h2><span className="vp-count">{rows.length}</span></div>
            {!rows.length && <div className="vp-empty"><ViewPluginIcon /><h3>{w.empty}</h3><p>{w.emptyHint}</p></div>}
            <div className="vp-library-grid">{rows.map(row => <article className="vp-card vp-installed" key={row.id} data-enabled={row.enabled}>
              <ViewArtwork cover={packageCover(row.active)} missing={w.coverRequired} small /><div className="vp-installed-body"><div className="vp-card-heading"><h3>{name(row.active, language)}</h3><span className="vp-version">v{row.active.manifest.version}</span></div>
                <span className="vp-status"><i />{row.enabled ? w.enabled : w.disabled}</span>
                {!packageCover(row.active) && <span className="vp-muted">{w.coverRequired}</span>}
                <div className="vp-card-footer"><button className="vp-primary" disabled={busy || !row.enabled} onClick={() => choose(row.active)}>{w.open} <span aria-hidden="true">↗</span></button>
                  <details className="vp-options"><summary>{w.manage} <span aria-hidden="true">⌄</span></summary><div className="vp-option-list">
                    <button disabled={busy} onClick={() => void act(() => enablePackage(row, !row.enabled))}>{row.enabled ? w.disable : w.enable}</button>
                    {row.previous && <button disabled={busy} onClick={() => void act(() => rollbackPackage(row))}>{w.rollback} · {row.previous.manifest.version}</button>}
                    <button className="vp-remove" disabled={busy} onClick={() => void act(() => removePackage(row))}>{w.remove}</button>
                    <small className="vp-package-id">{row.id}</small>
                  </div></details>
                </div>
              </div>
            </article>)}</div>
          </section>
          <section className="vp-examples"><div className="vp-section-head"><div><h2>{w.preset}</h2><p>{w.examplesHint}</p></div></div><div className="vp-example-grid">
            {[
              {id:"session-galaxy", title:w.galaxy, description:w.galaxyHint, action:w.addGalaxy},
              {id:"session-orrery", title:w.orrery, description:w.orreryHint, action:w.addOrrery},
              {id:"session-night-city", title:w.nightCity, description:w.nightCityHint, action:w.addNightCity},
              {id:"session-departures", title:w.departures, description:w.departuresHint, action:w.addDepartures},
              {id:"session-territory", title:w.territory, description:w.territoryHint, action:w.addTerritory},
            ].filter(example => !!exampleCover("com.clawdline." + example.id)).map(example => <article className="vp-example" key={example.id}>
              <ViewArtwork cover={exampleCover("com.clawdline." + example.id)} missing={w.coverRequired} /><div><h3>{example.title}</h3><p>{example.description}</p><button disabled={busy || rows.some(r => r.id === "com.clawdline." + example.id)} onClick={() => void exampleInstall(example.id)}>{rows.some(r => r.id === "com.clawdline." + example.id) ? w.added : example.action}</button></div>
            </article>)}
          </div></section>
          <footer className="vp-local-note"><span aria-hidden="true">◌</span><p>{w.local}</p></footer>
        </>}
      </main></>}
  </div>, document.body) : null
  return { opened, openedRef, dock: !opened && dock ? entry : null, entry: null, content,
    openManager: (button: HTMLElement) => openRoot(button, "manage"), managerLabel: w.title }
}
