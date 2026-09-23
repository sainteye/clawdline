/** One fragment field, decoded without letting one malformed address break routing. */
function valueInHash(hash: string, name: string): string | null {
  const escaped = name.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")
  const found = new RegExp(`(?:^|[#&])${escaped}=([^&]*)`).exec(String(hash || ""))
  if (!found || !found[1]) return null
  try {
    return decodeURIComponent(found[1])
  } catch {
    return found[1]
  }
}

/** `pageInHash` (`core/pages.js`): the page a fragment names, or null when it names none. */
function pageInHash(hash: string): string | null {
  return valueInHash(hash, "page")
}

export interface WorkRoute {
  project: string
  fromProjects: boolean
}

export interface WorkRouteProject {
  id: string
  path: string
}

/** The durable scope carried by a work-page address. */
export function workRouteFromHash(hash: string): WorkRoute {
  if (pageInHash(hash) !== "work") return { project: "", fromProjects: false }
  return {
    project: valueInHash(hash, "project")?.trim() ?? "",
    fromProjects: valueInHash(hash, "from") === "projects",
  }
}

/** A bookmarkable work-page address, including the page that should receive Back. */
export function workPageHash(project = "", from?: "projects"): string {
  let hash = "#page=work"
  if (project.trim()) hash += "&project=" + encodeURIComponent(project.trim())
  if (from === "projects") hash += "&from=projects"
  return hash
}

/** Resolve the Project path carried by a Project-page link to the Board's durable id. */
export function workProjectID(routeProject: string, projects: readonly WorkRouteProject[]): string {
  const wanted = routeProject.trim()
  if (!wanted) return ""
  return projects.find((project) => project.id === wanted || project.path === wanted)?.id ?? ""
}

/** An absent or retired page address (including Dashboard and Board) lands on Sessions. */
export function pageFromHash<Page extends string>(
  hash: string,
  knows: (name: string) => name is Page,
): Page | "sessions" {
  const wanted = pageInHash(hash)
  return wanted && knows(wanted) ? wanted : "sessions"
}
