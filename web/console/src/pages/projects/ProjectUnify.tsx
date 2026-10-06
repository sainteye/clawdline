import { catalogFormat } from "../../catalog.js"
import { catalogWord } from "../../catalog.js"
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
      case "project_not_found": return catalogWord("literal", "9a7262b359c8")
      case "forbidden": case "file_permission": return catalogWord("literal", "3e7bd562c1a0")
      case "cloud_commands_disabled": return catalogWord("literal", "629ac7ab4009")
      case "plan_unknown": return catalogWord("literal", "57ca6d4d6bda")
      case "name_taken": return catalogWord("literal", "413ebfdaf106")
      case "cloud_not_carried": case "cloud_feature_unavailable": return catalogWord("literal", "d32e2af285db")
      default: return error.message || catalogWord("literal", "ff95187970b9")
    }
  }
  return catalogWord("literal", "2714b3b0bbd5")
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
      <h4 id={`project-unify-col-${column.assistant}`}>{column.title} {catalogWord("inline", "da38f5af745f")}</h4>
      <ul>
        {column.rows.length === 0 && <li className="project-unify-empty">{catalogWord("inline", "dca1ae03a159")}</li>}
        {column.rows.map(row => <li key={row.key} className={`project-unify-seen is-${row.mark}`}>
          <span className="project-unify-seen-kind">{catalogWord("literal", "09b167988430")}</span>
          <span className="project-unify-seen-name"><code>{row.name}</code>{row.note && <small>{row.note}</small>}</span>
          <span className="project-unify-seen-mark"><span aria-hidden="true">{row.mark === "same" ? "＝" : row.mark === "added" ? "＋" : "！"}</span>{MARK_WORDS[row.mark]}</span>
          {row.mark === "added" && <span className="project-unify-seen-flow">{catalogWord("inline", "da26afc0fb6a")}</span>}
        </li>)}
      </ul>
    </section>)}
  </div>
}

function ActionItem({ picture }: { picture: ActionPicture }) {
  return <li className="project-unify-action">
    <p>{picture.sentence}</p>
    {picture.skill && <div className={`project-unify-arrow is-${picture.skill.arrow}`} role="img"
      aria-label={catalogFormat("template", "bd6a78a26209", [picture.skill.from, picture.skill.caption, picture.skill.to])}>
      <code>{picture.skill.from}</code>
      <span className="project-unify-arrow-line" aria-hidden="true"><span>{picture.skill.caption}</span></span>
      <code>{picture.skill.to}</code>
    </div>}
    {picture.files.map(file => <figure key={file.path} className="project-unify-file">
      <figcaption><code>{file.path}</code>{file.created ? catalogWord("literal", "079d188b7362") : catalogWord("literal", "25efadbde2aa")}</figcaption>
      <pre tabIndex={0}>{file.lines.map((line, index) => "folded" in line
        ? <span key={index} className="project-unify-line is-folded">…（{line.folded}{catalogWord("inline", "eba3e2db2b11")}</span>
        : <span key={index} className={`project-unify-line is-${line.change}`}>
          <span className="project-unify-line-sign" aria-label={line.change === "added" ? catalogWord("literal", "f592c9a3a866") : line.change === "removed" ? catalogWord("literal", "9b71e94cd03d") : undefined}>{line.change === "added" ? "+" : line.change === "removed" ? "−" : " "}</span>{line.text || " "}
        </span>)}</pre>
    </figure>)}
  </li>
}

function Preview({ plan }: { plan: ProjectUnifyPlan }) {
  const columns = unifyColumns(plan)
  if (plan.status === "unified") return <>
    <p className="project-unify-verdict is-ready">{catalogWord("inline", "1de4dda49ddb")}</p>
    <Columns columns={columns} />
  </>
  const pictures = actionPictures(plan)
  const moves = movesSummary(plan)
  const conflicts = conflictViews(plan)
  return <>
    {plan.status === "unknown" && <p className="project-unify-verdict is-unknown" role="alert">{catalogWord("inline", "83cc34a2d090")}{unknownReason(plan)}{catalogWord("inline", "846d711dc077")}</p>}
    <h3 className="project-unify-section">{catalogWord("inline", "da3f4f610025")}</h3>
    <Columns columns={columns} />
    {pictures.length > 0 && <>
      <h3 className="project-unify-section">{catalogWord("inline", "c4dd64880f20")}</h3>
      <ol className="project-unify-actions">{pictures.map(picture => <ActionItem key={picture.key} picture={picture} />)}</ol>
      <div className="project-unify-moves">
        {moves.moved.length > 0 && <p>{catalogWord("inline", "1cc68b0adf7c")}{moves.moved.map(m => <code key={m}>{m}</code>)}</p>}
        {moves.created.length > 0 && <p>{catalogWord("inline", "8c25cba32cc1")}{moves.created.map(m => <code key={m}>{m}</code>)}</p>}
        <p>{moves.sentence}</p>
      </div>
    </>}
    {conflicts.length > 0 && <>
      <h3 className="project-unify-section">{catalogWord("inline", "072bd947d77d")}</h3>
      <ul className="project-unify-conflicts">{conflicts.map(conflict => <li key={conflict.key}>
        <p><strong>{conflict.sentence}</strong></p>
        <p>{catalogWord("inline", "ca99a33b66f8")}{conflict.remedy}</p>
        {conflict.lines.length > 0 && <pre tabIndex={0} aria-label={catalogWord("inline", "e3066f2f2365")}>{conflict.lines.join("\n")}</pre>}
      </li>)}</ul>
    </>}
    <p className="project-unify-git">{catalogWord("inline", "0f82608f5de7")}</p>
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
    setApplying(true); setError(""); setStopped(null); setNotice(catalogWord("literal", "516617a04da2"))
    try {
      const answer = await applyUnify(place.id, version, crypto.randomUUID())
      if (ticket !== serial.current) return
      setPlan(answer.plan)
      if (answer.outcome === "stopped") {
        setNotice("")
        setStopped({ ran: answer.ran, failed: answer.failed, reason: describe(new ProjectFileError(answer.error ?? "unavailable", answer.detail ?? "")) })
      } else {
        const line = unifyStatusLine(answer.plan)
        setNotice(catalogFormat("template", "e3650f6f3874", [answer.ran.length, line.text]))
      }
    } catch (reason) {
      if (ticket !== serial.current) return
      if (reason instanceof ProjectFileError && reason.code === "plan_changed") {
        setNotice(catalogWord("literal", "70e41291ca36"))
        const next = await reload()
        setNotice(catalogWord("literal", "943f6e8d152e"))
      } else if (reason instanceof ProjectFileError && reason.uncertain) {
        setNotice(catalogWord("literal", "8701068aa07d"))
        const next = await reload()
        if (!next) setNotice("")
        else if (next.status === "unified") setNotice(catalogWord("literal", "9297a379c744"))
        else { setNotice(""); setError(catalogFormat("template", "9897d5449ea1", [unifyStatusLine(next).text])) }
      } else if (reason instanceof ProjectFileError && NOTHING_WRITTEN.has(reason.code)) {
        setNotice("")
        setError(catalogFormat("template", "31c3f09e85c7", [describe(reason)]))
        if (reason.code === "plan_unknown") void reload()
      } else {
        // A run that stopped part-way reaches a Cloud page as a bare refusal:
        // the machine's answer of what ran is not carried, so the screen reads
        // the plan again and says what disk holds now rather than "nothing".
        setNotice("")
        const next = await reload()
        setError(catalogFormat("template", "8dc58842b93c", [describe(reason), next ? catalogFormat("template", "bda857e28d78", [unifyStatusLine(next).text]) : catalogWord("literal", "04b8ab6d287d")]))
      }
    } finally { setApplying(false) }
  }

  const line = plan ? unifyStatusLine(plan) : null
  const status = loading && !plan ? catalogWord("literal", "5a02558d001a")
    : loadError ? catalogFormat("template", "5b1772732dce", [loadError])
      : line?.text ?? ""
  const tone = loadError ? "unknown" : line?.tone ?? "loading"
  return <section className="project-unify" aria-labelledby="project-unify-title">
    <div className="project-unify-head">
      <div>
        <h3 id="project-unify-title">{catalogWord("inline", "f5b3ce33e1f4")}</h3>
        <p>{catalogWord("inline", "e0b340925204")}</p>
      </div>
      <div className="project-unify-buttons">
        {loadError && <button type="button" disabled={loading} onClick={() => void reload()}>{catalogWord("inline", "cb3cd0dff7fc")}</button>}
        <button type="button" ref={opener} aria-haspopup="dialog" disabled={!plan} onClick={open}>{catalogWord("inline", "cee2d4509617")}</button>
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
        <h2 id="project-unify-dialog-title" ref={heading} tabIndex={-1}>{place.label}{catalogWord("inline", "b622a21f5371")}</h2>
        <button type="button" className="project-unify-close" disabled={applying} onClick={() => dialog.current?.close()}>{catalogWord("inline", "c7fdddf79eaa")}</button>
      </div>
      <div className="project-unify-dialog-body">
        {plan && <p className={`project-unify-status is-${unifyStatusLine(plan).tone}`}><span className="project-unify-dot" aria-hidden="true" />{unifyStatusLine(plan).text}</p>}
        {stopped && <div className="project-unify-stopped" role="alert">
          <p><strong>{catalogWord("inline", "b9c94d32232f")}</strong>{stopped.reason}{catalogWord("inline", "b14ed542c3c3")}</p>
          {stopped.ran.length > 0 && <><p>{catalogWord("inline", "d097506c04c2")}</p><ul>{stopped.ran.map((a, i) => <li key={i} className="is-ran">{actionSentence(a)}</li>)}</ul></>}
          {stopped.failed && <><p>{catalogWord("inline", "00bd3b95cf68")}</p><ul><li className="is-failed">{actionSentence(stopped.failed)}</li></ul></>}
          <p>{catalogWord("inline", "1f373bfba363")}</p>
        </div>}
        {error && <p className="project-unify-error" role="alert">{error}</p>}
        {notice && <p className="project-unify-notice" role="status">{notice}</p>}
        {loading && plan && <p className="project-unify-notice" role="status">{catalogWord("inline", "44f53aad2129")}</p>}
        {plan && <Preview plan={plan} />}
      </div>
      {plan && plan.status !== "unified" && <div className="project-unify-dialog-foot">
        {plan.status === "unknown"
          ? <p>{catalogWord("inline", "d9309fdbb465")}</p>
          : plan.actions.length === 0 ? <p>{catalogWord("inline", "6b4db2323869")}</p>
            : <button type="button" className="project-unify-apply" disabled={applying || loading || !mayApply(plan)} aria-busy={applying || undefined}
              onClick={() => void apply()}>{catalogWord("inline", "293b2cddd17b")}</button>}
      </div>}
    </dialog>
  </section>
}
