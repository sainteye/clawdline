import type { WorkV2Step } from "./api.js"
import { WorkIcon } from "./WorkIcon.js"

export function WorkSteps({ steps }: { steps?: WorkV2Step[] }) {
  if (!steps?.length) return null
  const done = steps.filter((step) => step.done).length
  return <section className="work-item-steps" aria-label={`項目 TODO，已完成 ${done} / ${steps.length}`}>
    <p>TODO · {done} / {steps.length}</p>
    <ol>{steps.map((step) => <li key={step.id} data-state={step.done ? "done" : "todo"}>
      <WorkIcon name={step.done ? "check" : "circle"} /><span>{step.title}</span>
    </li>)}</ol>
  </section>
}
