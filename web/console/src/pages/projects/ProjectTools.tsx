import { catalogWord } from "../../catalog.js"
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
        <strong>{catalogWord("inline", "3b71e7c5c1b9")}</strong>
        <span>{catalogWord("inline", "0eb9b6dde40e")}</span>
      </span>
    </summary>
    <div className="project-tools-body">
      <p className="project-tools-intro">{catalogWord("inline", "4956bcafb9bc")}</p>
      <ProjectSetup shown={shown} ref={setupRef} />
      <ProjectSync shown={shown} changed={changed} />
      <IconCopy shown={shown} changed={changed} />
    </div>
  </details>
}
