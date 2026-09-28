import { IconCopy } from "./IconCopy.js"
import { ProjectSetup } from "./ProjectSetup.js"
import type { ProjectSetupHandle } from "./ProjectSetup.js"
import { ProjectSync } from "./ProjectSync.js"
import type { Ref } from "react"
import "./project-tools.css"

/**
 * Configuration is secondary to choosing a Project. Keep the three maintenance
 * surfaces together behind one native disclosure so the Project list remains
 * the page's primary journey while every tool stays keyboard reachable.
 */
export function ProjectTools({ shown, changed, setupRef }: { shown: boolean; changed: () => void; setupRef: Ref<ProjectSetupHandle> }) {
  return <details className="project-tools" hidden={!shown}>
    <summary>
      <span className="project-tools-summary-copy">
        <strong>專案管理</strong>
        <span>設定健檢、跨機器同步與圖示工具</span>
      </span>
    </summary>
    <div className="project-tools-body">
      <p className="project-tools-intro">這些工具會改變專案如何顯示或在不同機器間共用；日常工作請直接從下方選擇專案。</p>
      <ProjectSetup shown={shown} ref={setupRef} />
      <ProjectSync shown={shown} changed={changed} />
      <IconCopy shown={shown} changed={changed} />
    </div>
  </details>
}
