import { useEffect, useRef, useSyncExternalStore } from "react"
import * as L from "../legacy/bridge.js"
import { nextWord } from "../next-strings.js"
import { showModalWithoutChoice } from "../modal-focus.js"
import { Start, type StartProgress } from "./Start.js"
import "./start-progress.css"

/**
 * The start the browser is running, drawn over the whole page.
 *
 * Pressing a Project used to close the sheet and leave only the band under the
 * header and a placeholder row, so nothing on screen said the browser was still
 * busy with what was pressed. This dialog follows the same wait `Start` keeps
 * (asking the machine, waiting for the row, opening it) and closes itself when
 * the row opens. "Keep waiting in the background" hands the wait to the band,
 * which keeps its × to let go of it.
 */
export function StartProgressDialog() {
  useSyncExternalStore(Start.subscribe, Start.version)
  const progress = Start.progress()
  const shown = !!progress && !progress.background
  const dialog = useRef<HTMLDialogElement>(null)
  const action = useRef<HTMLButtonElement>(null)

  useEffect(() => {
    const node = dialog.current
    if (!node) return
    if (shown && !node.open) {
      showModalWithoutChoice(node)
    }
    if (!shown && node.open) node.close()
  }, [shown])

  const steps = progress ? stepsOf(progress) : []
  const settled = progress?.stage === "slow" || progress?.stage === "detached"
  return (
    <dialog
      ref={dialog}
      className="start-progress"
      data-stage={progress?.stage}
      aria-labelledby="start-progress-title"
      aria-describedby="start-progress-say"
      onCancel={(e) => {
        e.preventDefault()
        Start.background()
      }}
    >
      {progress && (
        <>
          <Mark progress={progress} />
          <h2 id="start-progress-title">
            {nextWord(progress.resume ? "startProgressResumeTitle" : "startProgressTitle")}
          </h2>
          {progress.place && (
            <p className="start-progress-place">
              <span className="name" style={{ color: progress.place.icon ? L.accentTint(progress.place.icon.accent) : "" }}>
                {progress.place.label || progress.place.path}
              </span>
              {progress.place.path && progress.place.label && <span className="where">{L.path(progress.place.path)}</span>}
            </p>
          )}
          <ol className="start-progress-steps">
            {steps.map((step) => (
              <li key={step.key} data-state={step.state} aria-current={step.state === "active" ? "step" : undefined}>
                <span className="dot" aria-hidden="true" />
                <span>{nextWord(step.key)}</span>
              </li>
            ))}
          </ol>
          <p className="start-progress-say" id="start-progress-say" role="status" aria-live="polite">
            {progress.words || nextWord("startProgressHint")}
          </p>
          <div className="start-progress-actions">
            <button ref={action} type="button" className="chip wide" onClick={() => Start.background()}>
              {nextWord(settled ? "startProgressDone" : "startProgressBackground")}
            </button>
          </div>
        </>
      )}
    </dialog>
  )
}

type StepKey = "startProgressAsk" | "startProgressArrive" | "startProgressOpen"
type StepState = "done" | "active" | "stalled" | "pending"

function stepsOf(progress: StartProgress): { key: StepKey; state: StepState }[] {
  const asked: StepState = progress.stage === "asking" ? "active" : "done"
  const arrive: StepState = progress.stage === "asking" ? "pending"
    : progress.stage === "waiting" ? "active" : "stalled"
  return [
    { key: "startProgressAsk", state: asked },
    { key: "startProgressArrive", state: arrive },
    { key: "startProgressOpen", state: "pending" },
  ]
}

/** The Project's own mark inside a turning ring; the ring stops once the wait has stopped. */
function Mark({ progress }: { progress: StartProgress }) {
  const mark = useRef<HTMLCanvasElement>(null)
  const icon = progress.place?.icon
  useEffect(() => {
    const node = mark.current
    if (!node) return
    node.classList.toggle("none", !L.drawIcon(node, icon, 6))
  }, [icon])
  return (
    <div className="start-progress-mark" aria-hidden="true">
      <span className="ring" />
      <canvas ref={mark} />
    </div>
  )
}
