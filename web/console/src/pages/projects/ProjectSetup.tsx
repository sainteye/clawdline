import { useCallback, useEffect, useRef, useState } from "react"
import type { Icon } from "@clawdline/contract"
import { failureSentence } from "../../legacy/bridge.js"
import { Command } from "../../session/Command.js"
import { Mark } from "../../session/List.js"
import { readProjectPlaces, type ProjectPlace } from "../work/api.js"
import {
  projectSetupCapabilities,
  projectSetupInstructions,
  projectSetupProgress,
  type SetupTone,
} from "./project-setup.js"
import "./icon-copy.css"

const toneWords: Record<SetupTone, string> = {
  ready: "已完成",
  missing: "待補",
  attention: "需檢查",
  "not-applicable": "不適用",
  unknown: "未知",
}

function ProjectReadiness({ place, select }: { place: ProjectPlace; select(place: ProjectPlace): void }) {
  const capabilities = projectSetupCapabilities(place)
  const progress = projectSetupProgress(place)
  const needsAttention = capabilities.some(row => row.tone === "attention")
  const incomplete = capabilities.some(row => row.applicable && !row.complete)
  const action = needsAttention ? "檢查設定" : incomplete ? "補齊設定" : "重新檢視"
  return <li className="project-readiness-card">
    <div className="project-readiness-identity">
      <span className="project-readiness-mark" aria-hidden="true">
        {place.icon ? <Mark icon={place.icon as Icon} cellPx={3} /> : null}
      </span>
      <span>
        <strong>{place.label}</strong>
        <span className="project-readiness-path">{place.path}</span>
      </span>
      <span className="project-readiness-score" aria-label={progress ? `${progress.total} 項中已完成 ${progress.complete} 項` : "健檢資料未知"}>
        {progress ? `${progress.complete}/${progress.total}` : "—"}
      </span>
    </div>
    <ul className="project-readiness-capabilities">
      {capabilities.map(row => <li key={row.key} className={`project-readiness-capability is-${row.tone}`}>
        <span className="project-readiness-dot" aria-hidden="true" />
        <span>
          <strong>{row.label}</strong>
          <span>{row.detail}</span>
        </span>
        <span className="project-readiness-state">{toneWords[row.tone]}</span>
      </li>)}
    </ul>
    <button className="project-readiness-action" type="button" onClick={() => select(place)}>{action}</button>
  </li>
}

/**
 * The Projects page keeps only a small readiness summary in its main flow. The
 * complete, independently scrolling audit opens on demand so a long list never
 * pushes the ordinary Project rows below a second copy of every Project. A
 * press from that audit enters the ordinary command draft, where the person
 * still reviews assistant, model and instructions before any Session exists.
 */
export function ProjectSetup({ shown }: { shown: boolean }) {
  const [places, setPlaces] = useState<ProjectPlace[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState("")
  const dialog = useRef<HTMLDialogElement>(null)
  const trigger = useRef<HTMLButtonElement>(null)
  const restoreTrigger = useRef(true)
  const pendingSelection = useRef<ProjectPlace | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    setError("")
    try {
      const answer = await readProjectPlaces()
      setPlaces(answer.places)
    } catch (reason) {
      setPlaces([])
      setError(failureSentence(reason, "讀不到專案配置健檢，請重試。"))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    if (!shown) {
      restoreTrigger.current = false
      dialog.current?.close()
      return
    }
    restoreTrigger.current = true
    void load()
  }, [shown, load])

  const complete = places.filter(place => {
    const progress = projectSetupProgress(place)
    return progress && progress.complete === progress.total
  }).length

  const select = (place: ProjectPlace) => {
    pendingSelection.current = place
    restoreTrigger.current = false
    dialog.current?.close()
  }

  const summary = loading
    ? "讀取專案配置中…"
    : error
      ? "目前讀不到配置狀態"
      : places.length > 0
        ? `${complete}/${places.length} 個專案配置完整`
        : "目前沒有可檢查的專案"

  return <section className="project-setup" hidden={!shown} aria-labelledby="project-setup-launcher-title">
    <div className="project-setup-launcher-copy">
      <p className="project-setup-eyebrow">專案能力健檢</p>
      <h2 id="project-setup-launcher-title">專案設定狀況</h2>
      <p className={error ? "is-error" : ""} role={loading ? "status" : undefined}>{summary}</p>
    </div>
    <button
      className="project-setup-open"
      type="button"
      ref={trigger}
      aria-haspopup="dialog"
      onClick={() => {
        dialog.current?.showModal()
        dialog.current?.focus({ preventScroll: true })
      }}
    >查看健檢</button>
    <dialog
      className="project-setup-dialog"
      ref={dialog}
      tabIndex={-1}
      aria-labelledby="project-setup-title"
      onClose={() => {
        const place = pendingSelection.current
        pendingSelection.current = null
        if (place) {
          restoreTrigger.current = true
          void Command.reviewProjectSetup(place.id, projectSetupInstructions(place))
          return
        }
        if (restoreTrigger.current) trigger.current?.focus({ preventScroll: true })
        restoreTrigger.current = true
      }}
    >
      <div className="project-setup-dialog-heading">
        <div>
          <p className="project-setup-eyebrow">專案能力健檢</p>
          <h2 id="project-setup-title">還差什麼，一眼看懂</h2>
        </div>
        <button className="project-setup-close" type="button" aria-label="關閉專案能力健檢" onClick={() => dialog.current?.close()}>關閉</button>
      </div>
      <div className="project-setup-dialog-body">
        <div className="project-setup-heading">
          <p>只讀取這台機器已有的設定與狀態收據；不會連外、啟動服務或部署。缺少的項目可以先交給 AI 檢查，再由你審閱工作內容。</p>
          {!loading && places.length > 0 && <span className="project-setup-total">{complete}/{places.length}<small>配置完整</small></span>}
        </div>
        {loading && <p className="project-setup-loading" role="status">讀取專案配置中…</p>}
        {!loading && !error && places.length > 0 && <ol className="project-readiness-list">
          {places.map(place => <ProjectReadiness key={place.id} place={place} select={select} />)}
        </ol>}
        {!loading && !error && places.length === 0 && <p className="project-setup-empty" role="status">這台機器還沒有可檢查的專案。先在該目錄開過一次 assistant，或用 <code>clawdline project add</code> 登記。</p>}
        {error && <p role="alert">{error}</p>}
        <button className="project-setup-refresh" type="button" disabled={loading} onClick={() => void load()}>重新讀取</button>
      </div>
    </dialog>
  </section>
}
