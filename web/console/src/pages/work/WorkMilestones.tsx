import type { WorkV2Phase } from "./api.js"
import { WORK_MILESTONES, workMilestoneStates } from "./work-milestones.js"
import { WorkIcon } from "./WorkIcon.js"

export function WorkMilestones({ phase }: { phase: WorkV2Phase }) {
  const states = workMilestoneStates(phase)
  return <ol className="work-milestones" aria-label="項目進度">
    {WORK_MILESTONES.map((label, index) => {
      const state = states[index]
      return <li key={label} data-state={state}
        aria-label={`${label}：${state === "done" ? "已完成" : state === "current" ? "進行中" : "尚未完成"}`}>
        <WorkIcon name={state === "done" ? "check" : state === "current" ? "dot" : "circle"} />{label}
      </li>
    })}
  </ol>
}
