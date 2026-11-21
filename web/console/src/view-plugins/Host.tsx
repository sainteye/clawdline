import { useEffect, useLayoutEffect, useRef, useState } from "react"
import { createPortal } from "react-dom"
import type { ViewSource } from "./bridge.js"
import { PluginError, parsePackage, type ViewPackage } from "./package.js"
import { installedPackages, subscribePackages, installPackage, enablePackage, removePackage, rollbackPackage, officialUpdatePolicy, type Installed } from "./store.js"
import { useViewUpdates } from "./useUpdates.js"
import { downloadOfficialRelease, holdViewRuntime, isOfficialRevision } from "./updates.js"
import { newerStableVersion } from "./package.js"
import { PLUGIN_LIMITS as B } from "./limits.js"
import { PluginFrame, readPluginSource, type ViewGrant } from "./Frame.js"
import { pluginWords } from "./words.js"
import { ViewArtwork, ViewPluginIcon, exampleCover, examplePreview, packageCover, hasCuratedUpdate } from "./Artwork.js"
import { fleetPluginSource } from "./sources.js"
import { ManageMenu } from "./ManageMenu.js"
import { EmbeddedView, pinView, useDesk, usePinnedView } from "./Embedded.js"
import { LoadingSkeleton } from "../LoadingSkeleton.js"
import { useDockMotion } from "../ui/dock-motion.js"
import "./plugins.css"

const code = (e: unknown) => e instanceof PluginError ? e.code : "storage_unavailable"
const name = (p: ViewPackage, language: string) => p.manifest.name[language.startsWith("zh") ? "zh-Hant" : "en"]
const curatedID = (slug: string) => "com.clawdline." + slug
const recommendedSlugs = ["session-departures", "session-codecraft"]

type PackageReview = { archive: ArrayBuffer; pkg: ViewPackage; expected: string | null; official: boolean }

export function useViewPlugins(sources: readonly ViewSource[], language: string, onPinned?: () => void) {
  const pinned = usePinnedView(), desk = useDesk()
  const pinnedRef = useRef(onPinned); pinnedRef.current = onPinned
  // Pinning closes the full-screen host and shows the pane, even from an open Session.
  // The pane is shown after the host's own history entry is gone, as a plugin navigation is.
  const pinHere = (id: string) => { pinView(id); navigate.current = () => pinnedRef.current?.(); closeRef.current() }
  const [opened, setOpened] = useState(false), openedRef = useRef(false)
  const [mode, setMode] = useState<"manage" | "launch">("launch")
  const [rows, setRows] = useState<Installed[]>([]), [error, setError] = useState("")
  const [loading, setLoading] = useState(false)
  const [busy, setBusy] = useState(false), [review, setReview] = useState<PackageReview | null>(null)
  // Keep the reviewed card visible while the registry hint revokes stale grants.
  const [installing, setInstalling] = useState<PackageReview | null>(null), [completed, setCompleted] = useState<ViewPackage | null>(null)
  const installingRef = useRef(false), reviewOpener = useRef<HTMLElement | null>(null)
  const attention = useRef<{ target: () => HTMLElement | null; anchor: HTMLElement | null; top: number } | null>(null)
  const isBusy = busy || installing !== null, panel = review?.pkg ?? installing?.pkg ?? completed
  const [grant, setGrant] = useState<ViewGrant | null>(null)
  const updates = useViewUpdates(!!grant || !!review || !!installing || busy)
  const runtime = useRef<{ release?: () => void; control: AbortController } | null>(null)
  const modal = useRef<HTMLDivElement>(null), opener = useRef<HTMLElement | null>(null)
  const motion = useDockMotion(modal)
  const owned = useRef<string | null>(null), navigate = useRef<(() => void) | null>(null)
  const closing = useRef(false)
  const operations = useRef(0)
  const w = pluginWords(language), source = fleetPluginSource(sources, w.everything)
  const liveSource = useRef(source); liveSource.current = source
  const finish = () => {
    runtime.current?.control.abort(); runtime.current?.release?.(); runtime.current = null
    openedRef.current = false; owned.current = null; closing.current = false; operations.current++
    setOpened(false); setGrant(null); setReview(null); setInstalling(null); setCompleted(null); setBusy(false); setLoading(false); attention.current = null
    const next = navigate.current; navigate.current = null
    requestAnimationFrame(() => { (opener.current?.isConnected ? opener.current : document.querySelector<HTMLButtonElement>(".vp-dock,#nav-view-plugins"))?.focus({ preventScroll: true });
      try { next?.() } catch { /* A source revoked during Back never receives navigation. */ }
    })
  }
  const finishRef = useRef(finish); finishRef.current = finish
  const close = () => {
    if (closing.current || !openedRef.current) return
    closing.current = true
    motion.close(() => {
      if (owned.current && history.state?.viewPlugin === owned.current) history.back()
      else finishRef.current()
    })
  }
  const closeRef = useRef(close); closeRef.current = close
  useEffect(() => { if (!grant) { runtime.current?.control.abort(); runtime.current?.release?.(); runtime.current = null } }, [grant])
  useEffect(() => () => { runtime.current?.control.abort(); runtime.current?.release?.() }, [])
  const refresh = async () => { try { setRows(await installedPackages()) } catch (e) { setRows([]); setError(code(e)) } }
  useEffect(() => subscribePackages(() => {
    // A hint revokes the running grant immediately, before asynchronous storage verification.
    operations.current++; setGrant(null); setReview(null); setCompleted(null); setBusy(false); setLoading(false)
    void refresh()
  }), [])
  useEffect(() => {
    if (!grant || source.key === grant.sourceKey) return
    operations.current++; setGrant(null); setBusy(false); setError("scope_changed")
  }, [source.key, grant])
  const queueAttention = (target: () => HTMLElement | null, anchor: HTMLElement | null = null) => {
    attention.current = { target, anchor, top: anchor?.getBoundingClientRect().top ?? 0 }
  }
  const reveal = (target: HTMLElement, context?: HTMLElement) => {
    const scroller = modal.current?.querySelector<HTMLElement>(".vp-manager")
    if (!scroller) return
    target.focus({ preventScroll: true })
    const inset = parseFloat(getComputedStyle(scroller).scrollPaddingTop) || 0
    const destination = context && target.getBoundingClientRect().bottom - context.getBoundingClientRect().top <= scroller.clientHeight - 2 * inset ? context : target
    scroller.scrollTo({ top: scroller.scrollTop + destination.getBoundingClientRect().top - scroller.getBoundingClientRect().top - inset,
      behavior: matchMedia("(prefers-reduced-motion: reduce)").matches ? "instant" : "smooth" })
  }
  useLayoutEffect(() => {
    const next = attention.current
    if (!next) return
    attention.current = null
    const scroller = modal.current?.querySelector<HTMLElement>(".vp-manager"), target = next.target()
    if (!scroller || !target) return
    // Rebase before paint so inserting a review never teleports the gallery first.
    if (next.anchor?.isConnected) scroller.scrollTop += next.anchor.getBoundingClientRect().top - next.top
    reveal(target)
  })
  useLayoutEffect(() => {
    if (!opened) return
    motion.open()
    const node = modal.current!, siblings = [...document.body.children].filter(el => el !== node && el instanceof HTMLElement) as HTMLElement[]
    const prior = siblings.map(el => [el, el.inert] as const), overflow = document.body.style.overflow
    for (const [el] of prior) el.inert = true
    document.body.style.overflow = "hidden"
    node.querySelector<HTMLButtonElement>("button")?.focus()
    const pop = (e: PopStateEvent) => {
      if (!openedRef.current) return
      e.stopImmediatePropagation(); if (closing.current) finishRef.current(); else { closing.current = true; motion.close(() => finishRef.current()) }
    }
    const key = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        // An open Manage menu takes Escape for itself; the next one closes the host.
        if (node.querySelector(".vp-menu-wrap[data-open=true]")) return
        e.preventDefault(); e.stopImmediatePropagation(); closeRef.current(); return
      }
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
  const openRoot = (button: HTMLElement, nextMode: "manage" | "launch", target?: string) => {
    if (openedRef.current) return
    motion.capture(button)
    opener.current = button; owned.current = crypto.randomUUID(); openedRef.current = true
    history.pushState({ ...history.state, viewPlugin: owned.current }, "", location.href)
    setError(""); setRows([]); setMode(nextMode); setOpened(true); setBusy(true); setLoading(true)
    const generation = ++operations.current
    void installedPackages().then(installed => {
      if (!openedRef.current || generation !== operations.current) return
      setRows(installed); setBusy(false); setLoading(false)
      const enabled = installed.filter(r => r.enabled), wanted = target && enabled.find(r => r.id === target)
      if (wanted) choose(wanted.active)
      else if (nextMode === "launch" && enabled.length === 1) choose(enabled[0].active)
    }, e => { if (openedRef.current && generation === operations.current) { setError(code(e)); setBusy(false); setLoading(false) } })
  }
  const act = async (operation: () => Promise<void>) => {
    if (isBusy || installingRef.current) return
    const opening = owned.current
    setBusy(true); setError("")
    try { await operation(); if (owned.current === opening) await refresh() } catch (e) {
      if (owned.current !== opening) return
      queueAttention(() => modal.current?.querySelector<HTMLElement>(".vp-error") ?? null, reviewOpener.current)
      setError(code(e))
    }
    finally { if (owned.current === opening) { setBusy(false); setLoading(false) } }
  }
  const inspect = async (archive: ArrayBuffer, official = false) => {
    const generation = operations.current
    const pkg = await parsePackage(archive), current = (await installedPackages()).find(r => r.id === pkg.manifest.id)
    if (!openedRef.current || generation !== operations.current) return
    queueAttention(() => modal.current?.querySelector<HTMLElement>(".vp-review") ?? null, reviewOpener.current)
    setCompleted(null); setReview({ archive, pkg, expected: current?.active.digest ?? null, official })
  }
  const confirmInstall = async () => {
    if (!review || isBusy || installingRef.current) return
    const selected = review, opening = owned.current
    installingRef.current = true; setInstalling(selected); setError("")
    try {
      await installPackage(selected.archive, selected.expected, { official: selected.official })
      const installed = await installedPackages()
      if (!openedRef.current || owned.current !== opening) return
      setRows(installed); setReview(null); setCompleted(selected.pkg)
      // The success action replaces the confirmation in the same stable card.
      requestAnimationFrame(() => modal.current?.querySelector<HTMLButtonElement>(".vp-review .vp-primary")?.focus({ preventScroll: true }))
    } catch (e) { if (openedRef.current && owned.current === opening) setError(code(e)) }
    finally { installingRef.current = false; if (owned.current === opening) setInstalling(null) }
  }
  const cancelReview = () => {
    const origin = reviewOpener.current
    queueAttention(() => origin?.isConnected ? origin : modal.current?.querySelector<HTMLElement>(".vp-examples") ?? null, origin)
    setReview(null); setError("")
  }
  const exampleInstall = (id: string, button: HTMLElement) => {
    if (isBusy || installingRef.current) return
    reviewOpener.current = button
    const generation = operations.current
    return act(async () => {
      let answer: Response
      try { answer = await fetch(`/view-plugins/${id}.zip`, { credentials: "same-origin", cache: "no-store" }) }
      catch { throw new PluginError("package_download_failed") }
      if (!answer.ok || !answer.body) throw new PluginError("package_download_failed")
      const reader = answer.body.getReader(), chunks: Uint8Array[] = []; let size = 0
      try { while (true) { const part = await reader.read(); if (part.done) break; size += part.value.length
        if (size > B.archiveBytes) throw new PluginError("package_too_large"); chunks.push(part.value) }
      } finally { await reader.cancel() }
      const bytes = new Uint8Array(size); let at = 0; for (const part of chunks) { bytes.set(part, at); at += part.length }
      const pkg = await parsePackage(bytes.buffer), catalog = await updates.check(false)
      if (!openedRef.current || generation !== operations.current) return
      const release = catalog?.packages.find(item => item.id === pkg.manifest.id)
      await inspect(bytes.buffer, !!release && release.sha256 === pkg.digest && release.version === pkg.manifest.version)
    })
  }
  const choose = (pkg: ViewPackage) => {
    if (isBusy || installingRef.current) return
    setLoading(true); setReview(null)
    void act(async () => {
      const current = liveSource.current, captured = current.key
      const lease = { control: new AbortController(), release: undefined as (() => void) | undefined }; runtime.current = lease
      try {
        lease.release = await holdViewRuntime(lease.control.signal)
        if (lease.control.signal.aborted) throw new PluginError("scope_changed")
        const operation = ++operations.current
        const saved = (await installedPackages()).find(row => row.id === pkg.manifest.id)
        if (!saved?.enabled) throw new PluginError("registry_changed")
        const initial = await readPluginSource(current, new AbortController().signal)
        if (operation !== operations.current || !openedRef.current || liveSource.current.key !== captured) throw new PluginError("scope_changed")
        if (initial.sessions.length > B.sessions) throw new PluginError("too_many_sessions")
        setGrant({ pkg: saved.active, sourceKey: captured, initial, instanceId: crypto.randomUUID() })
      } catch (error) { lease.release?.(); if (runtime.current === lease) runtime.current = null; throw error }
    })
  }
  const reviewUpdate = (row: Installed, button: HTMLElement) => {
    reviewOpener.current = button
    return act(async () => {
      const catalog = await updates.check(false), release = catalog?.packages.find(item => item.id === row.id)
      if (!catalog) throw new PluginError("update_check_failed")
      if (!release) throw new PluginError("update_source_missing")
      const current = (await installedPackages()).find(item => item.id === row.id)
      if (!current) throw new PluginError("registry_changed")
      if (!newerStableVersion(release.version, current.active.manifest.version)) return
      const value = await downloadOfficialRelease(release, new AbortController().signal)
      await inspect(value.archive, true)
    })
  }
  const entry = <button type="button" className="vp-dock" aria-label={w.entry} title={w.launcher} onClick={e => openRoot(e.currentTarget, "launch")}>
    <ViewPluginIcon />
  </button>
  const enabled = rows.filter(row => row.enabled)
  const examples = [
    {id:"session-codecraft", title:w.codecraft, description:w.codecraftHint, action:w.addCodecraft},
    {id:"session-kaiju", title:w.kaiju, description:w.kaijuHint, action:w.addKaiju},
    {id:"session-galaxy", title:w.galaxy, description:w.galaxyHint, action:w.addGalaxy},
    {id:"session-orrery", title:w.orrery, description:w.orreryHint, action:w.addOrrery},
    {id:"session-night-city", title:w.nightCity, description:w.nightCityHint, action:w.addNightCity},
    {id:"session-departures", title:w.departures, description:w.departuresHint, action:w.addDepartures},
    {id:"session-territory", title:w.territory, description:w.territoryHint, action:w.addTerritory},
  ].filter(example => !!exampleCover(curatedID(example.id)))
  const recommendations = recommendedSlugs.flatMap(id => examples.filter(example =>
    example.id === id && !rows.some(row => row.id === curatedID(id))))
  const gallery = (items: typeof examples, heading: string, hint?: string) =>
    <section className="vp-examples" aria-label={heading}>
      <div className="vp-section-head"><div><h2>{heading}</h2>{hint && <p>{hint}</p>}</div></div>
      <div className="vp-example-grid">{items.map(example => {
        const installed = rows.find(row => row.id === curatedID(example.id)), update = installed && hasCuratedUpdate(installed.active)
        return <article className="vp-example" key={example.id}>
          <ViewArtwork cover={examplePreview(curatedID(example.id), language)} missing={w.coverRequired} preview />
          <div><h3>{example.title}</h3><p>{example.description}</p><button data-unavailable={!!installed && !update} disabled={isBusy || !!installed && !update} onClick={e => void exampleInstall(example.id, e.currentTarget)}>{installed ? update ? w.update : w.added : example.action}</button></div>
        </article>
      })}</div>
    </section>
  const content = opened ? createPortal(<div className="vp-host" ref={modal} role="dialog" aria-modal="true" aria-label={mode === "manage" ? w.title : w.launcher}>
    {grant ? <PluginFrame key={grant.instanceId} grant={grant} source={source} language={language} onClose={close}
      onPin={desk && pinned !== grant.pkg.manifest.id ? () => pinHere(grant.pkg.manifest.id) : undefined}
      onNavigate={fn => { navigate.current = fn }} onFault={error => { setGrant(null); setError(error) }} /> : <>
      <header className="vp-bar"><div className="vp-bar-title"><ViewPluginIcon /><h1>{mode === "manage" ? w.title : w.launcher}</h1></div>{isBusy && !loading && <span className="vp-progress page-skeleton" role="status" aria-label={w.loading}><i className="page-skeleton-line" aria-hidden="true" /></span>}<button type="button" className="vp-close" aria-label={w.close} onClick={close}>✕</button></header>
      <main className={`vp-manager${mode === "launch" ? " vp-launcher" : ""}`} aria-busy={isBusy}>
        {error && !review && !installing && <p className="vp-error" role="alert" tabIndex={-1}>{w.errors[error] || w.unavailable}</p>}
        {loading ? <LoadingSkeleton label={w.loading} id="view-plugins" kind="cards" /> : <>
          {mode === "manage" && <div className="vp-intro"><div><p className="vp-eyebrow">{w.personalize}</p><h2>{w.manageHeading}</h2><p>{w.manageHint}</p></div><label className="vp-import">+ {w.install}<input className="vp-file" type="file" accept=".zip,application/zip" aria-label={w.install} disabled={isBusy} onChange={e => {
            const file = e.currentTarget.files?.[0]; e.currentTarget.value = ""
            if (file) { reviewOpener.current = e.currentTarget.closest<HTMLElement>(".vp-import"); void act(async () => { if (file.size > B.archiveBytes) throw new PluginError("package_too_large"); await inspect(await file.arrayBuffer()) }) }
          }} /></label></div>}
          {panel && <section className="vp-review vp-card" data-package-id={panel.manifest.id} data-phase={installing ? "installing" : review ? "review" : "installed"} tabIndex={-1} aria-label={completed && !review && !installing ? w.added : w.review}>
            <ViewArtwork cover={panel.manifest.cover} missing={w.coverRequired} small /><p className="vp-eyebrow" data-install-result={completed && !review && !installing ? true : undefined}>{completed && !review && !installing ? w.added : w.review}</p><h2>{name(panel, language)}</h2><p className="vp-package-id">{panel.manifest.id} · {panel.manifest.version}</p>
            <p>{w.permissions}: {w.read}{panel.manifest.capabilities.includes("sessions.open") ? ` · ${w.navigate}` : ""}</p><p className="vp-muted">{w.trust}</p>
            {error && (review || installing) && <p className="vp-error" role="alert" tabIndex={-1}>{w.errors[error] || w.unavailable}</p>}
            <div className="vp-actions">{completed && !review && !installing ? <>
              <button className="vp-primary" disabled={isBusy || !rows.find(row => row.id === completed.manifest.id)?.enabled} onClick={() => choose(completed)}>{w.open} <span aria-hidden="true">↗</span></button>
              {mode === "manage" && <button disabled={isBusy} onClick={() => {
                const card = [...(modal.current?.querySelectorAll<HTMLElement>(".vp-installed") ?? [])].find(el => el.dataset.packageId === completed.manifest.id)
                if (card) { reveal(card, card.closest(".vp-library")?.querySelector<HTMLElement>(".vp-section-head") ?? undefined); card.querySelector<HTMLButtonElement>(".vp-primary")?.focus({ preventScroll: true }) }
              }}>{w.installed}</button>}
            </> : <>
              <button className="vp-primary" disabled={isBusy} onClick={() => void confirmInstall()}>{review?.expected || installing?.expected ? w.replace : w.confirmInstall}</button>
              <button disabled={isBusy} onClick={cancelReview}>{w.cancel}</button>
            </>}</div></section>}
          {mode === "launch" ? <>
            {!enabled.length ? <div className="vp-empty"><ViewPluginIcon /><h2>{w.launchEmpty}</h2>{!recommendations.length && <p>{w.launchEmptyHint}</p>}</div> : <div className="vp-launch-list">{enabled.map(row => <div key={row.id} className="vp-launch-row"><button type="button" className="vp-launch-item" disabled={isBusy} onClick={() => choose(row.active)}>
              <ViewArtwork cover={packageCover(row.active)} missing={w.coverRequired} small /><span><b>{name(row.active, language)}</b><small>{pinned === row.id ? w.pinned : w.openView}</small></span><span className="vp-launch-arrow" aria-hidden="true">↗</span>
            </button>{desk && <button type="button" className="vp-launch-pin" disabled={isBusy} aria-pressed={pinned === row.id}
              onClick={() => pinned === row.id ? pinView(null) : pinHere(row.id)}>{pinned === row.id ? w.unpin : w.pin}</button>}</div>)}</div>}
            {recommendations.length > 0 && gallery(recommendations, w.recommended)}
          </> : <>
          <section className="vp-library"><div className="vp-section-head"><h2>{w.installed}</h2><span className="vp-count">{rows.length}</span><button className="vp-check-all" disabled={isBusy || updates.checking} onClick={() => void updates.check()}>{updates.checking ? w.checkingUpdates : w.checkUpdates}</button></div>
            {updates.failure && <p className="vp-update-error" role="status">{w.errors[updates.failure] || w.errors.update_check_failed}</p>}
            {!rows.length && <div className="vp-empty"><ViewPluginIcon /><h3>{w.empty}</h3><p>{w.emptyHint}</p></div>}
            <div className="vp-library-grid">{rows.map(row => {
              const release=updates.catalog?.packages.find(item=>item.id===row.id), newer=release&&newerStableVersion(release.version,row.active.manifest.version)
              const official=!!release&&(row.official||isOfficialRevision(row,release)), status=newer?(updates.states[row.id]||"available"):"current"
              return <article className="vp-card vp-installed" key={row.id} data-enabled={row.enabled} data-package-id={row.id} data-update-state={status} tabIndex={-1}>
              <ViewArtwork cover={packageCover(row.active)} missing={w.coverRequired} small /><div className="vp-installed-body"><div className="vp-card-heading"><h3>{name(row.active, language)}</h3><span className="vp-version">v{row.active.manifest.version}</span></div>
                <span className="vp-status"><i />{row.enabled ? w.enabled : w.disabled}</span>
                {!packageCover(row.active) && <span className="vp-muted">{w.coverRequired}</span>}
                <p className="vp-update-status" role="status">{updates.failure ? w.updateCheckUnavailable : release ? newer ? w[status] + " · v" + release.version : w.current : updates.checking ? w.checkingUpdates : w.updateSourceUnknown}</p>
                {official && <label className="vp-auto-update"><input type="checkbox" checked={row.official ? !!row.autoUpdate : true} disabled={isBusy} onChange={e=>{const enabled=e.currentTarget.checked;void act(()=>officialUpdatePolicy(row,enabled))}} />{w.autoUpdate}</label>}
                <div className="vp-card-footer"><button className="vp-primary" disabled={isBusy || !row.enabled} onClick={() => choose(row.active)}>{w.open} <span aria-hidden="true">↗</span></button>
                  <button className="vp-update-button" disabled={isBusy || updates.checking} onClick={e=>void reviewUpdate(row,e.currentTarget)}>{newer ? w.updateTo.replace("{version}",release.version) : w.checkUpdates}</button>
                  <ManageMenu label={w.manage} busy={isBusy} packageId={row.id} items={[
                    { label: row.enabled ? w.disable : w.enable, onSelect: () => void act(() => enablePackage(row, !row.enabled)) },
                    ...(row.previous ? [{ label: `${w.rollback} · ${row.previous.manifest.version}`, onSelect: () => void act(() => rollbackPackage(row)) }] : []),
                  ]} remove={{ label: w.remove, cancel: w.cancel, confirm: w.remove,
                    question: `${w.removeQuestion.replace("{name}", name(row.active, language))}${row.previous ? w.removePrevious : ""}`,
                    onConfirm: () => void act(() => removePackage(row)) }} />
                </div>
              </div>
            </article>})}</div>
          </section>
          {gallery(examples, w.preset, w.examplesHint)}
          <footer className="vp-local-note"><span aria-hidden="true">◌</span><p>{w.local}</p></footer>
          </>}
        </>}
      </main></>}
  </div>, document.body) : null
  /** The pinned view for the Session pane, or null when the pane keeps its home screen. */
  const viewPane = (hidden: boolean) => desk && pinned && !opened ? <EmbeddedView source={source} language={language} pinned={pinned} hidden={hidden}
    onFullscreen={(button, pkg) => openRoot(button, "launch", pkg.manifest.id)} /> : null
  const pinnedPane = desk && !!pinned
  return { opened, openedRef, dock: entry, content, viewPane, pinnedPane, viewReturn: w.backToView,
    openManager: (button: HTMLElement) => openRoot(button, "manage"), managerLabel: w.title }
}
