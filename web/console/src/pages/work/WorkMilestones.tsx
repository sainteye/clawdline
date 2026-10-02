import type { WorkV2Phase } from "./api.js"
import { workMilestones, workMilestonesShown } from "./work-milestones.js"
import { WorkIcon } from "./WorkIcon.js"

export function WorkMilestones({ phase, verifyGate }: { phase: WorkV2Phase; verifyGate: boolean }) {
  if (!workMilestonesShown(phase)) return null
  return <ol className="work-milestones" aria-label="項目進度">
    {workMilestones(phase, verifyGate).map(({ label, state }) => {
      return <li key={label} data-state={state}
        aria-label={`${label}：${state === "done" ? "已完成" : state === "current" ? "進行中" : "尚未完成"}`}>
        <WorkIcon name={state === "done" ? "check" : state === "current" ? "dot" : "circle"} />{label}
      </li>
    })}
  </ol>
}
