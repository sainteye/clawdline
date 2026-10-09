// @ts-expect-error -- Node's strip-types test runner needs the source extension.
import { catalogFormat, catalogWord, localizedLiteralMap } from "../../catalog.ts"
import type { ProjectSetup, ProjectUnifyStatus } from "@clawdline/contract"
import type { ProjectPlace, ProjectSetupEvidence } from "../work/api.js"

export type SetupTone = "ready" | "missing" | "attention" | "not-applicable" | "unknown"
/** `action: "unify"` is a row whose button opens that Project's unify preview. */
export interface SetupCapability {
  key: string; label: string; detail: string; tone: SetupTone; complete: boolean; applicable: boolean; action?: "unify"
}

/** The places answer's setup with the unify fields the machine adds (api/v1 ProjectSetup). */
type SetupWithUnify = ProjectSetupEvidence & Pick<ProjectSetup, "unify" | "unify_count">

const unifyTone: Record<ProjectUnifyStatus, SetupTone> = { unified: "ready", drifting: "attention", unknown: "unknown" }

/**
 * Whether this Project's Claude and Codex sessions read the same rules and
 * skills, from the machine's unify plan. A daemon older than the field sends
 * none: that is unknown and left out of the score, never shared. A count the
 * machine did not send is not shown as 0.
 */
function unifyCapability(setup: SetupWithUnify): SetupCapability {
  const status = setup.unify
  const detail = status === "unified" ? catalogWord("projects", "setupUnifyShared")
    : status === "drifting" ? (typeof setup.unify_count === "number" && setup.unify_count > 0
      ? catalogFormat("projects", "setupUnifyDriftCount", [setup.unify_count])
      : catalogWord("projects", "setupUnifyDrift"))
      : status === "unknown" ? catalogWord("projects", "setupUnifyUnreadable") : catalogWord("projects", "setupUnifyUnsupported")
  return {
    key: "unify", label: catalogWord("projects", "setupUnifyLabel"), detail, tone: status ? unifyTone[status] : "unknown",
    complete: status === "unified", applicable: status !== undefined, action: status ? "unify" : undefined,
  }
}

const iconWords: Record<ProjectSetupEvidence["icon"], string> = localizedLiteralMap({
  mirrored: "cc665d77a525",
  override: "cd7f6327d0b3",
  registry: "8140c66c9bf8",
  generated: "7c8592c509f9",
})

const activityWords: Record<ProjectSetupEvidence["deploy_activity"], string> = localizedLiteralMap({
  idle: "76bf39159b6b",
  running: "8d7dedc0c9d6",
  succeeded: "97731ef0bc81",
  failed: "d3f4db92141b",
  unknown: "b176bc03af47",
})

/** The visible facts and their human consequence, kept out of JSX so
 * loading an older daemon can be tested as an explicit unknown state. */
export function projectSetupCapabilities(place: ProjectPlace): SetupCapability[] {
  const setup = place.setup
  if (!setup) return [
    { key: "unknown", label: catalogWord("literal", "9a7acd6c7c08"), detail: catalogWord("literal", "94c76925c566"), tone: "unknown", complete: false, applicable: false },
  ]
  const deployTone: SetupTone = setup.deploy === "attention" || (setup.deploy === "ready" && setup.deploy_activity === "failed")
    ? "attention"
    : setup.deploy === "not_applicable" ? "not-applicable" : setup.deploy
  const deployDetail = setup.deploy === "ready"
    ? catalogFormat("template", "aed581d1c3af", [activityWords[setup.deploy_activity]])
    : setup.deploy === "attention" ? catalogWord("literal", "7a805af78cf4")
      : setup.deploy === "not_applicable" ? catalogWord("literal", "fbe78156575f")
        : place.repo ? catalogWord("literal", "3056db4ed234") : catalogWord("literal", "82af552f444c")
  const serverDetail = setup.servers === "ready"
    ? catalogFormat("template", "10f186c469ac", [setup.server_count])
    : setup.servers === "attention" ? catalogWord("literal", "3723c98b41df")
      : setup.servers === "empty" ? catalogWord("literal", "f1c62f33216d")
        : catalogWord("literal", "c1a43ec0edc2")
  return [
    { key: "icon", label: catalogWord("literal", "aa3249e29a5d"), detail: iconWords[setup.icon], tone: setup.icon === "generated" ? "missing" : "ready", complete: setup.icon !== "generated", applicable: true },
    { key: "deploy", label: catalogWord("literal", "6ae06f64f8fa"), detail: deployDetail, tone: deployTone, complete: setup.deploy === "ready", applicable: setup.deploy !== "not_applicable" },
    { key: "servers", label: catalogWord("literal", "7d6a2964df09"), detail: serverDetail, tone: setup.servers === "ready" ? "ready" : setup.servers === "attention" ? "attention" : "missing", complete: setup.servers === "ready", applicable: true },
    { key: "sync", label: catalogWord("literal", "f3e136131a7e"), detail: setup.sync === "ready" ? catalogWord("literal", "9391584df834") : catalogWord("literal", "d14c93fd8dbe"), tone: setup.sync, complete: setup.sync === "ready", applicable: true },
    unifyCapability(setup),
  ]
}

export function projectSetupProgress(place: ProjectPlace): { complete: number; total: number } | null {
  const capabilities = projectSetupCapabilities(place).filter(row => row.applicable)
  if (!place.setup) return null
  return { complete: capabilities.filter(row => row.complete).length, total: capabilities.length }
}

/**
 * The reviewable first message for a Project-setup Session.
 *
 * It names the portable guide rather than copying its evolving contract into
 * the console. The checklist here still states the boundary that matters at
 * review time: configuration is allowed; an operational deploy or restart is
 * not smuggled into the same press.
 */
export function projectSetupInstructions(place: Pick<ProjectPlace, "label" | "path">): string {
  return [
    catalogWord("literal", "7e545a6a9b17"),
    "",
    catalogFormat("template", "767c7030f872", [JSON.stringify(place.label)]),
    catalogFormat("projects", "setupProjectRoot", [JSON.stringify(place.path)]),
    "",
    catalogWord("literal", "0272dbb4a00f"),
    "",
    catalogWord("literal", "81dffa7e36c1"),
    catalogWord("literal", "c08033e523fa"),
    catalogWord("literal", "7aa3b940bfe1"),
    catalogWord("literal", "3ec8c6e1bcd0"),
    catalogWord("literal", "97b97136df1d"),
    "",
    catalogWord("literal", "ee73618ae2cb"),
  ].join("\n")
}
