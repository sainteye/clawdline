import { localizedLiteralMap } from "../../catalog.js"
import { catalogFormat } from "../../catalog.js"
import { catalogWord } from "../../catalog.js"
import { fullStop, labelled, listSeparator, openingMark, parenthesized, wordGap } from "../../punctuation.js"
import { useCallback, useEffect, useRef, useState } from "react"
import type { Icon } from "@clawdline/contract"
import { Mark } from "../../session/List.js"
import { failureSentence } from "../../legacy/bridge.js"
import { projectSyncSeam, type SyncMachine } from "../../cloud/project-sync.js"
import {
  applyProjectMirror, detachProjectMirror, readProjectMirror,
  type SyncEntry, type SyncManifest, type SyncMirror, type SyncSource,
} from "../work/api.js"
import { describeSyncResult, syncStateWord } from "./project-sync-result.js"
import "./project-sync.css"

const failed = () => catalogWord("literal", "796b25ecda67")

const SKIP_WORDS: Record<string, string> = localizedLiteralMap({
  no_remote: "00fae213888a",
  not_a_repository: "4367608ea369",
  remote_not_portable: "bb0979aecd71",
  mirrored_here: "fa9704b1899c",
  unreadable: "6ee5a00c192a",
  duplicate_repository: "7b0ab080b9e0",
  manifest_full: "de1c2e60a0be",
})

function when(at: number): string {
  return at ? new Date(at * 1000).toLocaleString() : ""
}

/**
 * Project settings sync, on the machine that mirrors (docs/project-sync.md).
 *
 * One machine owns a project's name, icon and untracked skills; this one reads
 * them and keeps them read-only. The source is read through the Cloud seam
 * (`cloud/project-sync.ts`) and every write lands on this machine's own route.
 * Opening the page refreshes every project already mirrored from a source
 * that is online — the "the other side only reads" half of the design — and
 * adding a project is always a person's choice.
 */
export function ProjectSync({ shown, changed }: { shown: boolean; changed: () => void }) {
  const seam = projectSyncSeam()
  const [mirror, setMirror] = useState<SyncMirror | null>(null)
  const [machines, setMachines] = useState<SyncMachine[]>([])
  const [sourceID, setSourceID] = useState("")
  const [manifest, setManifest] = useState<SyncManifest | null>(null)
  const [picked, setPicked] = useState<Set<string>>(new Set())
  const [clone, setClone] = useState(false)
  const [busy, setBusy] = useState(false)
  const [lines, setLines] = useState<string[]>([])
  const [error, setError] = useState("")
  const refreshed = useRef(false)
  const here = seam?.here() ?? null
  const source = machines.find((m) => m.id === sourceID)

  const reload = useCallback(async () => {
    const [m, list] = await Promise.all([readProjectMirror(), seam ? seam.machines() : Promise.resolve([])])
    setMirror(m)
    const others = list.filter((x) => x.id !== here && x.selectable)
    setMachines(others)
    const owner = m.projects.find((p) => p.source.machine)?.source.machine ?? ""
    // A source known to be offline is not picked for the person; they can still choose it.
    setSourceID((was) => was || (others.some((x) => x.id === owner && x.online !== false) ? owner : ""))
    return { mirror: m, machines: others }
  }, [seam, here])

  // Bring what is already mirrored up to date with its own source, once per visit.
  const follow = useCallback(async (m: SyncMirror, others: SyncMachine[]) => {
    if (!seam) return
    const bySource = new Map<string, SyncMirror["projects"]>()
    const waiting = new Set<string>()
    for (const p of m.projects) {
      const from = others.find((x) => x.id === p.source.machine)
      if (!from) continue
      // A source known to be offline would only answer machine_offline; say so once, by name.
      if (from.online === false) { waiting.add(from.name); continue }
      bySource.set(from.id, [...(bySource.get(from.id) ?? []), p])
    }
    const done: string[] = []
    for (const [machine, records] of bySource) {
      // One source that cannot be read says so and leaves the others to update.
      try {
        const offer = await seam.read<SyncManifest>(machine, "project-manifest", {})
        for (const record of records) {
          const now = offer.projects.find((e) => e.repo === record.repo)
          if (!now || now.revision === record.revision) continue
          const entry = await seam.read<{ project: SyncEntry }>(machine, "project-entry", { repo: record.repo })
          const answer = await applyProjectMirror(record.source, entry.project, false)
          done.push(labelled(entry.project.label + parenthesized(record.repo), describeSyncResult(answer.result)))
        }
      } catch (e) {
        const name = others.find((x) => x.id === machine)?.name || records[0]?.source.name || machine
        done.push(labelled(name, failureSentence(e, failed())))
      }
    }
    const offline = [...waiting].map((name) => catalogFormat("template", "8861d1642b3c", [name]))
    if (done.length || offline.length) setLines([...(done.length ? [catalogWord("literal", "8239a6a8f0e7"), ...done] : []), ...offline])
    if (done.length) {
      changed()
      await reload()
    }
  }, [seam, reload, changed])

  useEffect(() => {
    if (!shown) {
      refreshed.current = false
      return
    }
    let active = true
    void reload().then(async (got) => {
      if (!active || refreshed.current) return
      refreshed.current = true
      await follow(got.mirror, got.machines)
    }).catch((e) => { if (active) setError(failureSentence(e, failed())) })
    return () => { active = false }
  }, [shown, reload, follow])

  async function readSource() {
    if (!seam || !source || source.online === false || busy) return
    setBusy(true); setError(""); setLines([]); setManifest(null)
    try {
      const offer = await seam.read<SyncManifest>(source.id, "project-manifest", {})
      setManifest(offer)
      setPicked(new Set(offer.projects.map((p) => p.repo)))
    } catch (e) {
      setError(labelled(source.name, failureSentence(e, failed())))
    } finally { setBusy(false) }
  }

  async function syncPicked() {
    if (!seam || !source || !manifest || busy) return
    setBusy(true); setError("")
    const from: SyncSource = { machine: source.id, name: source.name }
    const out: string[] = []
    for (const p of manifest.projects) {
      if (!picked.has(p.repo)) continue
      try {
        // A failed read is the source's; name it so the line says which machine did not answer.
        const entry = await seam.read<{ project: SyncEntry }>(source.id, "project-entry", { repo: p.repo })
          .catch((e) => { throw Object.assign(new Error(labelled(source.name, failureSentence(e, failed()))), { named: true }) })
        const answer = await applyProjectMirror(from, entry.project, clone)
        out.push(labelled(p.label + parenthesized(p.repo), describeSyncResult(answer.result)))
      } catch (e) {
        const why = (e as { named?: boolean }).named ? (e as Error).message : failureSentence(e, failed())
        out.push(labelled(p.label + parenthesized(p.repo), why))
      }
      setLines([...out])
    }
    setBusy(false)
    changed()
    await reload().catch((e) => setError(failureSentence(e, failed())))
  }

  async function detach(repo: string) {
    if (busy) return
    setBusy(true); setError("")
    try {
      await detachProjectMirror(repo)
      setLines([catalogFormat("template", "68c86e0aae21", [repo])])
      changed()
      await reload()
    } catch (e) {
      setError(failureSentence(e, failed()))
    } finally { setBusy(false) }
  }

  const mirrored = new Map((mirror?.projects ?? []).map((p) => [p.repo, p]))

  return <details className="project-sync" hidden={!shown}>
    <summary>{catalogWord("inline", "58b55ad71df6")}</summary>
    <p>{catalogWord("inline", "efd366b4e300")}{wordGap()}
      <code>{catalogWord("inline", "791c5105d4bf")}</code>{listSeparator()}<code>{catalogWord("inline", "84daedef39b3")}</code>{listSeparator()}<code>{catalogWord("inline", "a8532c746aa9")}</code>{wordGap()}{catalogWord("inline", "2b30dbf16241")} <code>{catalogWord("inline", "13230e1aea91")}</code>{catalogWord("inline", "955dc54b5b20")} <code>{catalogWord("inline", "181fdd46fc4a")}</code>{catalogWord("inline", "fc80320b2ca5")}{wordGap()}
      <code>{catalogWord("inline", "fca16cae5b0e")}</code>{wordGap()}{catalogWord("inline", "7633dc8d1def")}
    </p>
    {!seam && <p>{catalogWord("inline", "e9bebcc7e5af")} <code>{catalogWord("inline", "5f25c3304e10")}</code>{wordGap()}{catalogWord("inline", "92da784ad71b")} <code>{catalogWord("inline", "321e64eaf5eb")}</code>{fullStop()}</p>}

    {mirror && mirror.projects.length > 0 && <div className="project-sync-list">
      <h4>{catalogWord("inline", "12bc87aee711")}</h4>
      {mirror.projects.map((p) => <div className="project-sync-row" key={p.repo}>
        <Mark icon={p.icon as Icon} cellPx={3} />
        <span><strong>{p.label}</strong> <small>{p.repo}{catalogWord("inline", "752c8278d622")} {p.source.name || p.source.machine} · {when(p.applied_at)}</small></span>
        <button type="button" disabled={busy} onClick={() => void detach(p.repo)}>{catalogWord("inline", "fd63b0b7a79b")}</button>
      </div>)}
    </div>}
    {mirror && mirror.clones.length > 0 && <div className="project-sync-list">
      {mirror.clones.map((c) => <p key={c.repo} role={c.state === "clone_failed" ? "alert" : "status"}>
        {labelled(c.repo, syncStateWord(c.state))}{c.error ? parenthesized(c.error) : ""} → {c.dest}
      </p>)}
    </div>}

    {seam && <>
      <label>{catalogWord("inline", "0622fb891fd0")}
        <select value={sourceID} disabled={busy} onChange={(e) => { setSourceID(e.target.value); setManifest(null); setLines([]) }}>
          <option value="">{catalogWord("inline", "106a58cc83a1")}</option>
          {machines.map((m) => <option key={m.id} value={m.id} disabled={m.offers === false}>
            {m.name}{m.offers === false ? catalogWord("literal", "3c07629e48a7") : catalogWord("literal", "e2a511758f17")}
          </option>)}
        </select>
      </label>
      {source?.online === false && <p className="project-sync-offline" role="status">
        {openingMark(catalogWord("inline", "bee4fb5a17bb"))}{source.name}{catalogWord("inline", "bee4fb5a17bb")}
      </p>}
      <div className="project-sync-actions">
        <button type="button" disabled={!source || source.online === false || busy} onClick={() => void readSource()}>{busy && !manifest ? catalogWord("literal", "0d6fca356d81") : catalogWord("literal", "672c0aa99301")}</button>
      </div>
    </>}

    {manifest && <div className="project-sync-list">
      {manifest.projects.map((p) => {
        const had = mirrored.get(p.repo)
        return <label className="project-sync-row" key={p.repo}>
          <input type="checkbox" checked={picked.has(p.repo)} disabled={busy} onChange={(e) => {
            const next = new Set(picked)
            if (e.target.checked) next.add(p.repo); else next.delete(p.repo)
            setPicked(next)
          }} />
          <Mark icon={p.icon as Icon} cellPx={3} />
          <span><strong>{p.label}</strong> <small>{p.repo} · {catalogFormat("count", "localFiles", [p.files.length])}
            {had ? (had.revision === p.revision ? catalogWord("literal", "ebdff2f8a427") : catalogWord("literal", "7e6a49246391")) : ""}</small></span>
        </label>
      })}
      {manifest.skipped.length > 0 && <details>
        <summary>{catalogFormat("count", "unsyncedProjects", [manifest.skipped.length])}</summary>
        {manifest.skipped.map((s) => <p key={s.path}>{labelled(s.label, SKIP_WORDS[s.reason] ?? s.reason)}</p>)}
      </details>}
      <label className="project-sync-row">
        <input type="checkbox" checked={clone} disabled={busy} onChange={(e) => setClone(e.target.checked)} />
        <span>{catalogWord("inline", "c1f001d63fef")} <code>{mirror?.clone_root || catalogWord("literal", "469e3fc18f6a")}</code></span>
      </label>
      <div className="project-sync-actions">
        <button type="button" disabled={busy || picked.size === 0} onClick={() => void syncPicked()}>
          {busy ? catalogWord("literal", "d2dca5f96f16") : catalogFormat("template", "49c393ba6a87", [picked.size])}
        </button>
      </div>
    </div>}

    {lines.map((l, i) => <p key={i} role="status">{l}</p>)}
    {error && <p role="alert">{error}</p>}
  </details>
}
