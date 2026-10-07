import { catalogFormat } from "../../catalog.js"
import { catalogWord } from "../../catalog.js"
import type { WorkV2Step } from "./api.js"
import { WorkIcon } from "./WorkIcon.js"

export function WorkSteps({ steps }: { steps?: WorkV2Step[] }) {
  if (!steps?.length) return null
  const done = steps.filter((step) => step.done).length
  return <section className="work-item-steps" aria-label={catalogFormat("template", "540813eb6da2", [done, steps.length])}>
    <p>{catalogWord("inline", "457bc48e85a9")} {done} / {steps.length}</p>
    <ol>{steps.map((step) => <li key={step.id} data-state={step.done ? "done" : "todo"}>
      <WorkIcon name={step.done ? "check" : "circle"} /><span>{step.title}</span>
    </li>)}</ol>
  </section>
}
