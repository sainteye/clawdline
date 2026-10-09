import { catalogWord } from "../../catalog.js"
import { labelled } from "../../punctuation.js"
import type { WorkV2Phase } from "./api.js"
import { workMilestones, workMilestonesShown } from "./work-milestones.js"
import { WorkIcon } from "./WorkIcon.js"

export function WorkMilestones({ phase, verifyGate, onComplete }: { phase: WorkV2Phase; verifyGate: boolean; onComplete?: () => void }) {
  if (!workMilestonesShown(phase)) return null
  return <ol className="work-milestones" aria-label={catalogWord("inline", "7a6e9f7f44ea")}>
    {workMilestones(phase, verifyGate).map(({ label, state }, index, milestones) => {
      const completion = index === milestones.length - 1 && phase !== "done" && !!onComplete
      return <li key={label} data-state={state}
        aria-label={labelled(label, state === "done" ? catalogWord("literal", "d9f7294e59ea") : state === "current" ? catalogWord("literal", "2c65ed1ce8be") : catalogWord("literal", "caa636f04eb1"))}>
        {completion ? <button type="button" onClick={onComplete}>
          <WorkIcon name="circle" />{label}
        </button> : <><WorkIcon name={state === "done" ? "check" : state === "current" ? "dot" : "circle"} />{label}</>}
      </li>
    })}
  </ol>
}
