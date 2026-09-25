import { useCallback, useEffect, useRef, useState } from "react"
import type { Icon } from "@clawdline/contract"
import { Mark } from "../../session/List.js"
import { failureSentence } from "../../legacy/bridge.js"
import { projectSyncSeam, type SyncMachine } from "../../cloud/project-sync.js"
import {
  applyProjectMirror, detachProjectMirror, readProjectMirror,
  type SyncEntry, type SyncManifest, type SyncMirror, type SyncResult, type SyncSource,
} from "../work/api.js"
import "./project-sync.css"

const FAILED = "無法完成專案同步，請重試。"

const STATE_WORDS: Record<string, string> = {
  applied: "已同步",
  unchanged: "已是最新",
  missing: "這台沒有這個 repo",
  cloning: "正在 clone",
  clone_failed: "clone 失敗",
}

const KEPT_WORDS: Record<string, string> = {
  local_edit: "這台已修改過，保留",
  tracked: "由 git 追蹤，不覆寫",
  unsafe_path: "路徑經過連結或無法寫入，略過",
}

const SKIP_WORDS: Record<string, string> = {
  no_remote: "沒有 origin remote",
  not_a_repository: "不是 git repo",
  remote_not_portable: "origin 是本機路徑，其他機器無法取得",
  mirrored_here: "這台本身就是鏡像",
  unreadable: "讀不到 git 資訊",
  duplicate_repository: "同一個 repo 的另一份 checkout",
  manifest_full: "超過一次可同步的專案數",
}

function describe(r: SyncResult): string {
  const parts = [STATE_WORDS[r.state] ?? r.state]
  if (r.written.length) parts.push(`寫入 ${r.written.length} 個檔案`)
  if (r.deleted.length) parts.push(`移除 ${r.deleted.length} 個檔案`)
  for (const k of r.kept) parts.push(`${k.path}：${KEPT_WORDS[k.reason] ?? k.reason}`)
  return parts.join("，")
}

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
    setSourceID((was) => was || (others.some((x) => x.id === owner) ? owner : ""))
    return { mirror: m, machines: others }
  }, [seam, here])

  // Bring what is already mirrored up to date with its own source, once per visit.
  const follow = useCallback(async (m: SyncMirror, others: SyncMachine[]) => {
    if (!seam) return
    const bySource = new Map<string, SyncMirror["projects"]>()
    for (const p of m.projects) {
      if (!p.source.machine || !others.some((x) => x.id === p.source.machine)) continue
      bySource.set(p.source.machine, [...(bySource.get(p.source.machine) ?? []), p])
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
          done.push(`${entry.project.label}（${record.repo}）：${describe(answer.result)}`)
        }
      } catch (e) {
        done.push(`${records[0]?.source.name || machine}：${failureSentence(e, FAILED)}`)
      }
    }
    if (done.length) {
      setLines(["已從主要機器更新：", ...done])
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
    }).catch((e) => { if (active) setError(failureSentence(e, FAILED)) })
    return () => { active = false }
  }, [shown, reload, follow])

  async function readSource() {
    if (!seam || !source || busy) return
    setBusy(true); setError(""); setLines([]); setManifest(null)
    try {
      const offer = await seam.read<SyncManifest>(source.id, "project-manifest", {})
      setManifest(offer)
      setPicked(new Set(offer.projects.map((p) => p.repo)))
    } catch (e) {
      setError(failureSentence(e, FAILED))
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
        const entry = await seam.read<{ project: SyncEntry }>(source.id, "project-entry", { repo: p.repo })
        const answer = await applyProjectMirror(from, entry.project, clone)
        out.push(`${p.label}（${p.repo}）：${describe(answer.result)}`)
      } catch (e) {
        out.push(`${p.label}（${p.repo}）：${failureSentence(e, FAILED)}`)
      }
      setLines([...out])
    }
    setBusy(false)
    changed()
    await reload().catch((e) => setError(failureSentence(e, FAILED)))
  }

  async function detach(repo: string) {
    if (busy) return
    setBusy(true); setError("")
    try {
      await detachProjectMirror(repo)
      setLines([`${repo} 已改回這台自己的設定。`])
      changed()
      await reload()
    } catch (e) {
      setError(failureSentence(e, FAILED))
    } finally { setBusy(false) }
  }

  const mirrored = new Map((mirror?.projects ?? []).map((p) => [p.repo, p]))

  return <details className="project-sync" hidden={!shown}>
    <summary>專案設定同步</summary>
    <p>
      選一台主要機器，這台會讀取它的專案名稱、圖示，以及沒有進 git 的
      <code>.claude/skills</code>、<code>.claude/commands</code>、<code>.claude/agents</code> 與 <code>CLAUDE.local.md</code>，
      存成唯讀鏡像：之後在主要機器修改，打開這一頁就會更新過來。專案以 git <code>origin</code> 對應，不看資料夾名稱。
      <code>.claude/settings.local.json</code> 含有這台的權限設定，不會同步。
    </p>
    {!seam && <p>跨機器同步需要從 Clawdline Cloud 開啟。也可以在主要機器執行 <code>clawdline project export --out projects.json</code>，
      再到這台執行 <code>clawdline project import projects.json</code>。</p>}

    {mirror && mirror.projects.length > 0 && <div className="project-sync-list">
      <h4>這台正在鏡像的專案</h4>
      {mirror.projects.map((p) => <div className="project-sync-row" key={p.repo}>
        <Mark icon={p.icon as Icon} cellPx={3} />
        <span><strong>{p.label}</strong> <small>{p.repo} · 來自 {p.source.name || p.source.machine} · {when(p.applied_at)}</small></span>
        <button type="button" disabled={busy} onClick={() => void detach(p.repo)}>改回本機設定</button>
      </div>)}
    </div>}
    {mirror && mirror.clones.length > 0 && <div className="project-sync-list">
      {mirror.clones.map((c) => <p key={c.repo} role={c.state === "clone_failed" ? "alert" : "status"}>
        {c.repo}：{STATE_WORDS[c.state]}{c.error ? `（${c.error}）` : ""} → {c.dest}
      </p>)}
    </div>}

    {seam && <>
      <label>主要機器
        <select value={sourceID} disabled={busy} onChange={(e) => { setSourceID(e.target.value); setManifest(null); setLines([]) }}>
          <option value="">選擇機器</option>
          {machines.map((m) => <option key={m.id} value={m.id} disabled={m.offers === false}>
            {m.name}{m.offers === false ? "（需要更新 Clawdline）" : ""}
          </option>)}
        </select>
      </label>
      <div className="project-sync-actions">
        <button type="button" disabled={!source || busy} onClick={() => void readSource()}>{busy && !manifest ? "讀取中…" : "讀取它的專案"}</button>
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
          <span><strong>{p.label}</strong> <small>{p.repo} · {p.files.length} 個本機檔案
            {had ? (had.revision === p.revision ? " · 已是最新" : " · 有更新") : ""}</small></span>
        </label>
      })}
      {manifest.skipped.length > 0 && <details>
        <summary>不會同步的專案（{manifest.skipped.length}）</summary>
        {manifest.skipped.map((s) => <p key={s.path}>{s.label}：{SKIP_WORDS[s.reason] ?? s.reason}</p>)}
      </details>}
      <label className="project-sync-row">
        <input type="checkbox" checked={clone} disabled={busy} onChange={(e) => setClone(e.target.checked)} />
        <span>這台還沒有的 repo，clone 到 <code>{mirror?.clone_root || "（未知）"}</code></span>
      </label>
      <div className="project-sync-actions">
        <button type="button" disabled={busy || picked.size === 0} onClick={() => void syncPicked()}>
          {busy ? "同步中…" : `同步所選的 ${picked.size} 個專案`}
        </button>
      </div>
    </div>}

    {lines.map((l, i) => <p key={i} role="status">{l}</p>)}
    {error && <p role="alert">{error}</p>}
  </details>
}
