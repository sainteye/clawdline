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
import { pluginWords, viewHint } from "./words.js"
import { ViewArtwork, ViewPluginIcon, exampleCover, examplePreview, packageCover, hasCuratedUpdate, curatedPackageIDs, curatedVersion } from "./Artwork.js"
import { fleetPluginSource } from "./sources.js"
import { ManageMenu } from "./ManageMenu.js"
import { DockIcon, EmbeddedView, GearIcon, pinView, useDesk, usePinnedView } from "./Embedded.js"
import { LoadingSkeleton } from "../LoadingSkeleton.js"
import { useDockMotion } from "../ui/dock-motion.js"
import "./plugins.css"

const code = (e: unknown) => e instanceof PluginError ? e.code : "storage_unavailable"
const name = (p: ViewPackage, language: string) => p.manifest.name[language.startsWith("zh") ? "zh-Hant" : "en"]
const curatedID = (slug: string) => "com.clawdline." + slug
const recommendedSlugs = ["session-lantern-deep", "session-departures", "session-codecraft"]

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
  const [loading, setLoading] = useState(false), [loadError, setLoadError] = useState(""), [fetching, setFetching] = useState("")
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
  const openRoot = (button: HTMLElement, nextMode: "manage" | "launch", target?: string, browse = false) => {
    if (openedRef.current) return
    motion.capture(button)
    opener.current = button; owned.current = crypto.randomUUID(); openedRef.current = true
    history.pushState({ ...history.state, viewPlugin: owned.current }, "", location.href)
    setError(""); setLoadError(""); setRows([]); setMode(nextMode); setOpened(true); setBusy(true); setLoading(true)
    const generation = ++operations.current
    void installedPackages().then(installed => {
      if (!openedRef.current || generation !== operations.current) return
      setRows(installed); setBusy(false); setLoading(false)
      const enabled = installed.filter(r => r.enabled), wanted = target && enabled.find(r => r.id === target)
      if (wanted) choose(wanted.active)
      else if (nextMode === "launch" && !browse && enabled.length === 1) choose(enabled[0].active)
    }, e => { if (openedRef.current && generation === operations.current) { setLoadError(code(e)); setBusy(false); setLoading(false) } })
  }
  const reload = () => {
    if (!openedRef.current) return
    const generation = ++operations.current
    setLoadError(""); setError(""); setBusy(true); setLoading(true)
    void installedPackages().then(installed => { if (generation === operations.current) setRows(installed) },
      e => { if (generation === operations.current) setLoadError(code(e)) })
      .finally(() => { if (generation === operations.current) { setBusy(false); setLoading(false) } })
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
    setFetching(id)
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
    }).finally(() => setFetching(""))
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
    {id:"session-lantern-deep", title:w.lanternDeep, description:w.lanternDeepHint, action:w.addLanternDeep},
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
          <div><h3>{example.title}</h3><p>{example.description}</p><button data-unavailable={!!installed && !update} data-fetching={fetching === example.id || undefined} disabled={isBusy || !!installed && !update} onClick={e => void exampleInstall(example.id, e.currentTarget)}>{fetching === example.id ? <><span className="vp-spin" aria-hidden="true" />{w.installingShort}</> : installed ? update ? w.update : w.added : example.action}</button></div>
        </article>
      })}</div>
    </section>
  const importZip = (className: string) => <label className={className}>+ {w.install}<input className="vp-file" type="file" accept=".zip,application/zip" aria-label={w.install} disabled={isBusy} onChange={e => {
            const file = e.currentTarget.files?.[0]; e.currentTarget.value = ""
            if (file) { reviewOpener.current = e.currentTarget.closest<HTMLElement>(".vp-import"); void act(async () => { if (file.size > B.archiveBytes) throw new PluginError("package_too_large"); await inspect(await file.arrayBuffer()) }) }
          }} /></label>
  const disabledCount = rows.length - enabled.length
  const heroArt = ["session-galaxy", "session-departures", "session-codecraft"].map(id => examplePreview(curatedID(id), language)).filter(Boolean) as string[]
  const showRecommendations = () => {
    const target = modal.current?.querySelector<HTMLElement>(".vp-examples button:not([disabled])")
    if (target) reveal(target, modal.current?.querySelector<HTMLElement>(".vp-examples") ?? undefined)
  }
  const viewCard = (row: Installed) => {
    const preview = examplePreview(row.id, language), art = packageCover(row.active), curated = curatedPackageIDs.includes(row.id), here = desk && pinned === row.id
    const release = updates.catalog?.packages.find(item => item.id === row.id)
    const next = release && newerStableVersion(release.version, row.active.manifest.version) ? release.version : hasCuratedUpdate(row.active) ? curatedVersion(row.id) : undefined
    return <article key={row.id} className="vp-view-card" data-package-id={row.id} data-pinned={here}>
      <button type="button" className="vp-view-open" disabled={isBusy} onClick={() => choose(row.active)}>
        <span className={`vp-view-thumb${preview ? "" : " vp-view-thumb-cover"}`} aria-hidden="true">
          {preview ? <img src={preview} alt="" decoding="async" /> : art ? <><span className="vp-view-blur" style={{ backgroundImage: `url("${art}")` }} /><img src={art} alt="" /></> : <ViewPluginIcon />}
        </span>
        <span className="vp-view-meta">
          {art ? <img className="vp-view-icon" src={art} alt="" /> : <span className="vp-view-icon" aria-hidden="true" />}
          <span className="vp-view-text"><b>{name(row.active, language)}</b><small>{viewHint(row.id, w)}</small></span>
          <span className="vp-view-arrow" aria-hidden="true">↗</span>
        </span>
      </button>
      <span className="vp-view-badges">
        {here && <span className="vp-badge vp-badge-pinned"><DockIcon on />{w.pinned}</span>}
        {next && <span className="vp-badge vp-badge-update">{w.updateBadge.replace("{version}", next)}</span>}
        {!curated && <span className="vp-badge">{w.sideloaded}</span>}
      </span>
      {desk && <button type="button" className="vp-launch-pin" disabled={isBusy} aria-pressed={here} aria-label={here ? w.unpin : w.pin} title={here ? w.unpin : w.pin}
        onClick={() => here ? pinView(null) : pinHere(row.id)}><DockIcon on={here} /></button>}
    </article>
  }
  const authorGuide = "https://github.com/sainteye/clawdline/blob/main/docs/plugins/view-plugin-v1.md"
  const available = examples.filter(example => !rows.some(row => row.id === curatedID(example.id)))
  const releaseOf = (row: Installed) => updates.catalog?.packages.find(item => item.id === row.id)
  const newerOf = (row: Installed) => { const release = releaseOf(row); return release && newerStableVersion(release.version, row.active.manifest.version) ? release : undefined }
  const summary = (() => {
    const pending = rows.filter(row => newerOf(row) && updates.states[row.id] !== "updating"), busyRow = rows.find(row => updates.states[row.id] === "updating")
    const minutes = updates.checkedAt === null ? -1 : Math.floor((Date.now() - updates.checkedAt) / 60000)
    const checked = minutes < 0 ? w.notChecked : minutes < 1 ? w.checkedNow : w.checkedAgo.replace("{minutes}", String(minutes))
    if (updates.failure) return { state: "failed", title: w.checkFailedTitle, detail: w.errors[updates.failure] || w.errors.update_check_failed }
    if (busyRow) return { state: "updating", title: w.updatingNamed.replace("{name}", name(busyRow.active, language)), detail: w.updatingHint }
    if (pending.length) return { state: "available", title: w.updatesAvailable.replace("{count}", String(pending.length)), detail: checked }
    return { state: "current", title: updates.checking ? w.checkingUpdates : w.allCurrent, detail: `${checked} · ${w.officialAuto}` }
  })()
  const manageRow = (row: Installed) => {
    const release = releaseOf(row), newer = newerOf(row), official = !!release && (row.official || isOfficialRevision(row, release))
    const status = newer ? (updates.states[row.id] || "available") : "current", art = packageCover(row.active)
    const update = (button: HTMLElement) => void reviewUpdate(row, button)
    return <article className="vp-installed" key={row.id} data-enabled={row.enabled} data-package-id={row.id} data-update-state={status} tabIndex={-1}>
      {art ? <img className="vp-row-cover" src={art} alt="" /> : <span className="vp-row-cover vp-cover-missing" title={w.coverRequired} aria-hidden="true" />}
      <div className="vp-row-body">
        <div className="vp-card-heading"><h3>{name(row.active, language)}</h3><span className="vp-version">v{row.active.manifest.version}{status === "updating" && newer ? ` → ${newer.version}` : ""}</span></div>
        <div className="vp-row-meta">
          {status === "updating" ? <span className="vp-row-note" data-tone="progress">{w.updating}</span>
            : status === "updateFailed" ? <><span className="vp-row-note" data-tone="error">{w.updateFailedRow.replace("{version}", row.active.manifest.version)}</span>
              <button type="button" className="vp-update-button vp-chip-button" disabled={isBusy || updates.checking} onClick={e => update(e.currentTarget)}>{w.retry}</button></>
            : <>
              <span className="vp-status"><i />{row.enabled ? w.enabled : w.disabled}</span>
              {!row.enabled && <span>{w.hiddenFromViews}</span>}
              {newer && status !== "waitingClose" && <button type="button" className="vp-update-button vp-chip-button" title={w.updateTo.replace("{version}", newer.version)} disabled={isBusy || updates.checking} onClick={e => update(e.currentTarget)}>{w.updateChip.replace("{version}", newer.version)}</button>}
              {status === "waitingClose" && <span className="vp-row-note" data-tone="progress">{w.waitingClose}</span>}
              {row.enabled && !newer && (official ? <span>{row.official && !row.autoUpdate ? w.manualUpdate : w.autoUpdate}</span> : !curatedPackageIDs.includes(row.id) && <span className="vp-badge-inline">{w.sideloaded}</span>)}
            </>}
        </div>
        {status === "updating" && <span className="vp-row-progress" aria-hidden="true"><i /></span>}
      </div>
      <div className="vp-row-actions">
        <button type="button" className="vp-primary vp-row-open" aria-label={w.open} title={w.open} disabled={isBusy || !row.enabled} onClick={() => choose(row.active)}><span aria-hidden="true">↗</span></button>
        <button type="button" role="switch" className="vp-switch-toggle" aria-checked={row.enabled} aria-label={`${row.enabled ? w.disable : w.enable} ${name(row.active, language)}`} title={row.enabled ? w.disable : w.enable} disabled={isBusy} onClick={() => void act(() => enablePackage(row, !row.enabled))}><i /></button>
        <ManageMenu label={w.manage} busy={isBusy} packageId={row.id} icon={<svg viewBox="0 0 24 24" width="18" height="18" fill="currentColor" aria-hidden="true"><circle cx="5" cy="12" r="1.8" /><circle cx="12" cy="12" r="1.8" /><circle cx="19" cy="12" r="1.8" /></svg>} items={[
          ...(row.enabled ? [{ label: w.open, onSelect: () => choose(row.active) }] : []),
          ...(official ? [{ label: row.official && !row.autoUpdate ? w.autoUpdateOff : w.autoUpdateOn, onSelect: () => void act(() => officialUpdatePolicy(row, !(row.official ? !!row.autoUpdate : true))) }] : []),
          ...(row.previous ? [{ label: `${w.rollback} · ${row.previous.manifest.version}`, onSelect: () => void act(() => rollbackPackage(row)) }] : []),
        ]} remove={{ label: w.remove, cancel: w.cancel, confirm: w.remove,
          question: `${w.removeQuestion.replace("{name}", name(row.active, language))}${row.previous ? w.removePrevious : ""}`,
          onConfirm: () => void act(() => removePackage(row)) }} />
      </div>
    </article>
  }
  const content = opened ? createPortal(<div className="vp-host" ref={modal} role="dialog" aria-modal="true" aria-label={mode === "manage" ? w.title : w.launcher}>
    {grant ? <PluginFrame key={grant.instanceId} grant={grant} source={source} language={language} onClose={close}
      onPin={desk && pinned !== grant.pkg.manifest.id ? () => pinHere(grant.pkg.manifest.id) : undefined}
      onNavigate={fn => { navigate.current = fn }} onFault={error => { setGrant(null); setError(error) }} /> : <>
      <header className="vp-bar"><div className="vp-bar-title">{mode === "manage" && <button type="button" className="vp-bar-back" onClick={() => { setError(""); setMode("launch") }}><span aria-hidden="true">‹</span>{w.backToViews}</button>}<ViewPluginIcon /><h1>{mode === "manage" ? w.manageTitle : w.launcher}</h1></div>{isBusy && !loading && <span className="vp-progress page-skeleton" role="status" aria-label={w.loading}><i className="page-skeleton-line" aria-hidden="true" /></span>}{mode === "manage" && !loading && rows.length > 0 && importZip("vp-import vp-bar-manage")}{mode === "launch" && <button type="button" className="vp-bar-manage" onClick={() => { setError(""); setMode("manage") }}><GearIcon />{w.manageInstall}</button>}<button type="button" className="vp-close" aria-label={w.close} onClick={close}>✕</button></header>
      <main className={`vp-manager${mode === "launch" ? " vp-launcher" : ""}`} aria-busy={isBusy}>
        {error && !review && !installing && !loadError && <p className="vp-error" role="alert" tabIndex={-1}>{w.errors[error] || w.unavailable}</p>}
        {loading ? <LoadingSkeleton label={w.loading} id="view-plugins" kind="cards" /> : <>
          {panel && <section className="vp-review vp-card" data-package-id={panel.manifest.id} data-phase={installing ? "installing" : review ? "review" : "installed"} tabIndex={-1} aria-label={completed && !review && !installing ? w.added : w.review}>
            <ViewArtwork cover={packageCover(panel)} missing={w.coverRequired} small />
            <div className="vp-review-body">
            <p className="vp-eyebrow" data-install-result={completed && !review && !installing ? true : undefined}>{completed && !review && !installing ? w.added : w.reviewTitle}</p><h2>{name(panel, language)}</h2>
            <p className="vp-package-id">{panel.manifest.id} · {panel.manifest.version}{curatedPackageIDs.includes(panel.manifest.id) ? "" : ` · ${w.communityWork}`}</p>
            {!(completed && !review && !installing) && <ul className="vp-permissions" aria-label={w.permissions}>
              <li data-allowed="true"><i aria-hidden="true">✓</i>{w.permRead}</li>
              {panel.manifest.capabilities.includes("sessions.open") && <li data-allowed="true"><i aria-hidden="true">✓</i>{w.permNavigate}</li>}
              <li data-allowed="false"><i aria-hidden="true">✕</i>{w.permNever}</li>
            </ul>}
            {!(completed && !review && !installing) && <p className="vp-muted">{(review ?? installing)?.official ? w.trustOfficial : w.trustCommunity}</p>}
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
            </>}</div></div></section>}
          {mode === "launch" ? loadError ? <div className="vp-state" role="alert">
            <span className="vp-state-glyph" aria-hidden="true">!</span><h2>{w.loadFailed}</h2>
            <p>{w.errors[loadError] || w.unavailable}<br />{w.sessionsSafe}</p>
            <div className="vp-state-actions"><button className="vp-primary" onClick={reload}>{w.retry}</button><button onClick={() => { setLoadError(""); setMode("manage") }}>{w.openManage}</button></div>
            <code>{loadError}</code>
          </div> : <>
            {!enabled.length ? <section className="vp-hero" aria-label={rows.length ? w.allDisabledTitle : w.heroTitle}>
              {heroArt.length > 0 && <div className="vp-hero-art" aria-hidden="true">{heroArt.map(src => <img key={src} src={src} alt="" decoding="async" />)}</div>}
              <div className="vp-hero-copy"><h2>{rows.length ? w.allDisabledTitle : w.heroTitle}</h2><p>{rows.length ? w.allDisabledText : w.heroText}</p>
                <div className="vp-hero-actions">{rows.length ? <button className="vp-primary" onClick={() => setMode("manage")}>{w.openManage}</button> : <>
                  {recommendations.length > 0 && <button className="vp-primary" disabled={isBusy} onClick={showRecommendations}>{w.heroInstall}</button>}
                  {importZip("vp-import vp-hero-zip")}
                </>}</div></div>
            </section> : <section className="vp-views" aria-label={w.installedShort}>
              <div className="vp-section-head"><h2>{w.installedShort}</h2><span className="vp-count">{enabled.length}</span><p>{desk ? w.launchHint : w.launchHintPhone}</p></div>
              <div className="vp-view-grid">{enabled.map(viewCard)}</div>
              {disabledCount > 0 && <div className="vp-disabled-note"><span>{w.disabledHidden.replace("{count}", String(disabledCount))}</span>
                <button type="button" onClick={() => setMode("manage")}>{w.enableInManage} <span aria-hidden="true">→</span></button></div>}
            </section>}
            {recommendations.length > 0 && gallery(recommendations, w.recommended, w.recommendedHint)}
          </> : <>
          {rows.length ? <>
            <section className="vp-update-summary" data-state={summary.state} role="status">
              <span className="vp-summary-glyph" aria-hidden="true">{summary.state === "current" ? "✓" : summary.state === "failed" ? "!" : "↑"}</span>
              <div><b>{summary.title}</b><small>{summary.detail}</small></div>
              <button className="vp-check-all" disabled={isBusy || updates.checking} onClick={() => void updates.check()}>{updates.checking ? w.checkingUpdates : w.checkUpdates}</button>
            </section>
            <section className={`vp-library${review || installing ? " vp-dimmed" : ""}`}>
              <div className="vp-section-head"><h2>{w.installedShort}</h2><span className="vp-count">{rows.length}</span><p>{w.toggleHint}</p></div>
              <div className="vp-library-list">{rows.map(manageRow)}</div>
            </section>
          </> : <section className="vp-hero vp-manage-empty"><div className="vp-hero-copy"><h2>{w.emptyManageTitle}</h2><p>{w.emptyManageText}</p>
            <div className="vp-hero-actions">{importZip("vp-import")}<a className="vp-docs-link" href={authorGuide} target="_blank" rel="noreferrer">{w.howToMake}</a></div></div></section>}
          {available.length > 0 && gallery(available, rows.length ? w.exploreMore : w.officialViews, w.recommendedHint)}
          <footer className="vp-local-note"><span aria-hidden="true">◌</span><p>{w.local}</p></footer>
          </>}
        </>}
      </main></>}
  </div>, document.body) : null
  /** The pinned view for the Session pane, or null when the pane keeps its home screen. */
  const viewPane = (hidden: boolean) => desk && pinned && !opened ? <EmbeddedView source={source} language={language} pinned={pinned} hidden={hidden}
    onFullscreen={(button, pkg) => openRoot(button, "launch", pkg.manifest.id)}
    onBrowse={button => openRoot(button, "launch", undefined, true)} onManage={button => openRoot(button, "manage")} /> : null
  const pinnedPane = desk && !!pinned
  return { opened, openedRef, dock: entry, content, viewPane, pinnedPane, viewReturn: w.backToView,
    openManager: (button: HTMLElement) => openRoot(button, "manage"), managerLabel: w.title }
}
