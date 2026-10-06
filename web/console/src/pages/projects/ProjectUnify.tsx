import { useEffect, useRef, useState } from "react"
import type { ProjectUnifyAction, ProjectUnifyPlan } from "@clawdline/contract"
import type { ProjectPlace } from "../work/api.js"
import { applyUnify, readUnifyPlan, ProjectFileError } from "./project-files-api.js"
import {
  MARK_WORDS, actionPictures, actionSentence, conflictViews, mayApply, movesSummary, unifyColumns, unifyStatusLine, unknownReason,
  type ActionPicture, type SeenColumn,
} from "./project-unify.js"
import "./project-unify.css"

function describe(error: unknown): string {
  if (error instanceof ProjectFileError) {
    switch (error.code) {
      case "project_not_found": return "專案已不在這台機器的清單中。請回專案列表重新選擇。"
      case "forbidden": case "file_permission": return "這個連線沒有套用權限；可在機器上執行 clawdline project unify --apply。"
      case "cloud_commands_disabled": return "這台機器關閉了遠端寫入；可在機器上開啟後再試，或在機器上執行 clawdline project unify --apply。"
      case "plan_unknown": return "有檔案讀不到，無法套用；請先在機器上檢查。"
      case "name_taken": return "要建立連結的位置已經有別的東西。"
      case "cloud_not_carried": case "cloud_feature_unavailable": return "這個連線尚未提供共用設定。請確認機器與 Cloud 的版本。"
      default: return "目前無法讀取或套用共用設定；請檢查連線後重試。"
    }
  }
  return "目前無法完成操作。請檢查連線後重試。"
}

/** Refusals the machine or the page sends before any action runs. */
const NOTHING_WRITTEN = new Set([
  "plan_unknown", "forbidden", "bad_request", "project_not_found", "idempotency_key_required",
  "cloud_not_carried", "cloud_feature_unavailable", "cloud_project_machine_mismatch", "cloud_commands_disabled",
])

type Stopped ={ ran: ProjectUnifyAction[]; failed?: ProjectUnifyAction; reason: string }

function Columns({ columns }: { columns: SeenColumn[] }) {
  return <div className="project-unify-columns">
    {columns.map(column => <section key={column.assistant} className="project-unify-column" aria-labelledby={`project-unify-col-${column.assistant}`}>
      <h4 id={`project-unify-col-${column.assistant}`}>{column.title} 看得到</h4>
      <ul>
        {column.rows.length === 0 && <li className="project-unify-empty">沒有規則檔或 skill。</li>}
        {column.rows.map(row => <li key={row.key} className={`project-unify-seen is-${row.mark}`}>
          <span className="project-unify-seen-kind">{row.kind === "rules" ? "規則" : "skill"}</span>
          <span className="project-unify-seen-name"><code>{row.name}</code>{row.note && <small>{row.note}</small>}</span>
          <span className="project-unify-seen-mark"><span aria-hidden="true">{row.mark === "same" ? "＝" : row.mark === "added" ? "＋" : "！"}</span>{MARK_WORDS[row.mark]}</span>
          {row.mark === "added" && <span className="project-unify-seen-flow">現在看不到 → 套用後看得到</span>}
        </li>)}
      </ul>
    </section>)}
  </div>
}

function ActionItem({ picture }: { picture: ActionPicture }) {
  return <li className="project-unify-action">
    <p>{picture.sentence}</p>
    {picture.skill && <div className={`project-unify-arrow is-${picture.skill.arrow}`} role="img"
      aria-label={`${picture.skill.from} ${picture.skill.caption}到 ${picture.skill.to}`}>
      <code>{picture.skill.from}</code>
      <span className="project-unify-arrow-line" aria-hidden="true"><span>{picture.skill.caption}</span></span>
      <code>{picture.skill.to}</code>
    </div>}
    {picture.files.map(file => <figure key={file.path} className="project-unify-file">
      <figcaption><code>{file.path}</code>{file.created ? "（新檔案）" : "（套用後）"}</figcaption>
      <pre tabIndex={0}>{file.lines.map((line, index) => "folded" in line
        ? <span key={index} className="project-unify-line is-folded">…（{line.folded} 行不變）</span>
        : <span key={index} className={`project-unify-line is-${line.change}`}>
          <span className="project-unify-line-sign" aria-label={line.change === "added" ? "新增" : line.change === "removed" ? "移出" : undefined}>{line.change === "added" ? "+" : line.change === "removed" ? "−" : " "}</span>{line.text || " "}
        </span>)}</pre>
    </figure>)}
  </li>
}

function Preview({ plan }: { plan: ProjectUnifyPlan }) {
  const columns = unifyColumns(plan)
  if (plan.status === "unified") return <>
    <p className="project-unify-verdict is-ready">Claude 和 Codex 已看到相同的規則與 skills</p>
    <Columns columns={columns} />
  </>
  const pictures = actionPictures(plan)
  const moves = movesSummary(plan)
  const conflicts = conflictViews(plan)
  return <>
    {plan.status === "unknown" && <p className="project-unify-verdict is-unknown" role="alert">
      無法判斷：{unknownReason(plan)}。讀不到的項目修好之前不能套用；請在機器上檢查權限、大小或編碼後重新檢查。</p>}
    <h3 className="project-unify-section">誰看得到什麼</h3>
    <Columns columns={columns} />
    {pictures.length > 0 && <>
      <h3 className="project-unify-section">會做的事（依順序）</h3>
      <ol className="project-unify-actions">{pictures.map(picture => <ActionItem key={picture.key} picture={picture} />)}</ol>
      <div className="project-unify-moves">
        {moves.moved.length > 0 && <p>會搬移：{moves.moved.map(m => <code key={m}>{m}</code>)}</p>}
        {moves.created.length > 0 && <p>會新增：{moves.created.map(m => <code key={m}>{m}</code>)}</p>}
        <p>{moves.sentence}</p>
      </div>
    </>}
    {conflicts.length > 0 && <>
      <h3 className="project-unify-section">unify 不會替你決定的事</h3>
      <ul className="project-unify-conflicts">{conflicts.map(conflict => <li key={conflict.key}>
        <p><strong>{conflict.sentence}</strong></p>
        <p>你可以：{conflict.remedy}</p>
        {conflict.lines.length > 0 && <pre tabIndex={0} aria-label="Codex 看不到的行">{conflict.lines.join("\n")}</pre>}
      </li>)}</ul>
    </>}
    <p className="project-unify-git">不會 commit 到 git：檔案只在這個專案的工作目錄裡改動，之後由你或某個 Session commit。</p>
  </>
}

/**
 * The 「Claude 與 Codex 共用」 block in a Project's Settings and instructions:
 * one status line, and a preview that is itself the confirmation — apply is
 * one press inside it, with no second dialog.
 */
export function ProjectUnify({ place }: { place: ProjectPlace }) {
  const [plan, setPlan] = useState<ProjectUnifyPlan | null>(null)
  const [loading, setLoading] = useState(false)
  const [loadError, setLoadError] = useState("")
  const [applying, setApplying] = useState(false)
  const [notice, setNotice] = useState("")
  const [error, setError] = useState("")
  const [stopped, setStopped] = useState<Stopped | null>(null)
  const serial = useRef(0)
  const dialog = useRef<HTMLDialogElement>(null)
  const opener = useRef<HTMLButtonElement>(null)
  const heading = useRef<HTMLHeadingElement>(null)

  const reload = async (): Promise<ProjectUnifyPlan | null> => {
    const ticket = ++serial.current
    setLoading(true); setLoadError("")
    try {
      const next = await readUnifyPlan(place.id)
      if (ticket !== serial.current) return null
      setPlan(next)
      return next
    } catch (reason) {
      if (ticket === serial.current) { setPlan(null); setLoadError(describe(reason)) }
      return null
    } finally { if (ticket === serial.current) setLoading(false) }
  }

  useEffect(() => {
    void reload()
    return () => { serial.current++ }
  // The component is keyed by Project in its parent; this effect runs once for that identity.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [place.id])

  const open = () => {
    setNotice(""); setError(""); setStopped(null)
    dialog.current?.showModal()
    heading.current?.focus({ preventScroll: true })
  }

  const apply = async () => {
    if (!plan || !mayApply(plan) || applying) return
    const version = plan.version
    const ticket = ++serial.current
    setApplying(true); setError(""); setStopped(null); setNotice("正在套用…")
    try {
      const answer = await applyUnify(place.id, version, crypto.randomUUID())
      if (ticket !== serial.current) return
      setPlan(answer.plan)
      if (answer.outcome === "stopped") {
        setNotice("")
        setStopped({ ran: answer.ran, failed: answer.failed, reason: describe(new ProjectFileError(answer.error ?? "unavailable", answer.detail ?? "")) })
      } else {
        const line = unifyStatusLine(answer.plan)
        setNotice(`已套用 ${answer.ran.length} 項變更，目前狀態：${line.text}。下方的檔案清單請按「重新讀取清單」更新。`)
      }
    } catch (reason) {
      if (ticket !== serial.current) return
      if (reason instanceof ProjectFileError && reason.code === "plan_changed") {
        setNotice("預覽之後檔案有變動，沒有套用任何東西；正在重新讀取…")
        const next = await reload()
        setNotice(next ? "已重新讀取最新的預覽；請再看一次，確認後再套用。" : "")
      } else if (reason instanceof ProjectFileError && reason.uncertain) {
        setNotice("沒收到機器的回覆，正在重新讀取確認…")
        const next = await reload()
        if (!next) setNotice("")
        else if (next.status === "unified") setNotice("重新讀取後確認：已共用。")
        else { setNotice(""); setError(`無法確認是否已套用。重新讀取的結果是「${unifyStatusLine(next).text}」，請看下方預覽再決定。`) }
      } else if (reason instanceof ProjectFileError && NOTHING_WRITTEN.has(reason.code)) {
        setNotice("")
        setError(`沒有套用任何東西：${describe(reason)}`)
        if (reason.code === "plan_unknown") void reload()
      } else {
        // A run that stopped part-way reaches a Cloud page as a bare refusal:
        // the machine's answer of what ran is not carried, so the screen reads
        // the plan again and says what disk holds now rather than "nothing".
        setNotice("")
        const next = await reload()
        setError(`套用沒有完成：${describe(reason)} 可能已完成其中幾項；${next ? `重新讀取的結果是「${unifyStatusLine(next).text}」，請看下方預覽。` : "目前也讀不到最新狀態，請重新檢查。"}`)
      }
    } finally { setApplying(false) }
  }

  const line = plan ? unifyStatusLine(plan) : null
  const status = loading && !plan ? "正在檢查…"
    : loadError ? `無法判斷（讀不到共用狀態：${loadError}）`
      : line?.text ?? ""
  const tone = loadError ? "unknown" : line?.tone ?? "loading"
  return <section className="project-unify" aria-labelledby="project-unify-title">
    <div className="project-unify-head">
      <div>
        <h3 id="project-unify-title">Claude 與 Codex 共用</h3>
        <p>讓 Claude Code 與 Codex 讀到同一份規則和同一組 skills。</p>
      </div>
      <div className="project-unify-buttons">
        {loadError && <button type="button" disabled={loading} onClick={() => void reload()}>重新檢查</button>}
        <button type="button" ref={opener} aria-haspopup="dialog" disabled={!plan} onClick={open}>檢視變更</button>
      </div>
    </div>
    <p className={`project-unify-status is-${tone}`} role="status">
      <span className="project-unify-dot" aria-hidden="true" />{status}
    </p>
    <dialog className="project-unify-dialog" ref={dialog} aria-labelledby="project-unify-dialog-title"
      // React hands this dialog's cancel (Escape) to the Project settings
      // dialog around it too, which then closes itself: measured in the e2e,
      // Escape here closed both until it stopped here.
      onCancel={event => { event.stopPropagation(); if (applying) event.preventDefault() }}
      onClose={() => opener.current?.focus({ preventScroll: true })}>
      <div className="project-unify-dialog-heading">
        <h2 id="project-unify-dialog-title" ref={heading} tabIndex={-1}>{place.label} · Claude 與 Codex 共用</h2>
        <button type="button" className="project-unify-close" disabled={applying} onClick={() => dialog.current?.close()}>關閉</button>
      </div>
      <div className="project-unify-dialog-body">
        {plan && <p className={`project-unify-status is-${unifyStatusLine(plan).tone}`}><span className="project-unify-dot" aria-hidden="true" />{unifyStatusLine(plan).text}</p>}
        {stopped && <div className="project-unify-stopped" role="alert">
          <p><strong>套用到一半停下來了。</strong>{stopped.reason} 每一項不是完成就是還原，不會留下改一半的檔案。</p>
          {stopped.ran.length > 0 && <><p>已完成：</p><ul>{stopped.ran.map((a, i) => <li key={i} className="is-ran">{actionSentence(a)}</li>)}</ul></>}
          {stopped.failed && <><p>沒有完成：</p><ul><li className="is-failed">{actionSentence(stopped.failed)}</li></ul></>}
          <p>下方是重新讀取後的預覽。</p>
        </div>}
        {error && <p className="project-unify-error" role="alert">{error}</p>}
        {notice && <p className="project-unify-notice" role="status">{notice}</p>}
        {loading && plan && <p className="project-unify-notice" role="status">正在重新讀取…</p>}
        {plan && <Preview plan={plan} />}
      </div>
      {plan && plan.status !== "unified" && <div className="project-unify-dialog-foot">
        {plan.status === "unknown"
          ? <p>有項目讀不到，這次不能套用。</p>
          : plan.actions.length === 0 ? <p>沒有 unify 能自動做的變更；上面列出的項目需要你決定。</p>
            : <button type="button" className="project-unify-apply" disabled={applying || loading || !mayApply(plan)} aria-busy={applying || undefined}
              onClick={() => void apply()}>套用這些變更</button>}
      </div>}
    </dialog>
  </section>
}
