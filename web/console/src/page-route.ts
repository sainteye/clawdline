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

/**
 * The Session list's terminal mode, and the terminal open beside it.
 *
 * `#page=sessions&mode=terminal` is the list; `&project=<id>&terminal=<id>`
 * names the terminal in the second column (a whole screen on a phone). The
 * retired terminal page's addresses (`#page=terminal&project=…&terminal=…`)
 * still arrive from bookmarks and old links; they read as this mode, so a
 * terminal they name opens beside the list instead of on a page of its own.
 */
export function sessionsTerminalMode(hash: string): boolean {
  const page = pageInHash(hash)
  return (page === "sessions" && valueInHash(hash, "mode") === "terminal") || page === "terminal"
}

export interface TerminalRoute {
  project: string
  terminal: string
}

/** The terminal a terminal-mode address opens beside the list; empty when it names none. */
export function sessionsTerminalRoute(hash: string): TerminalRoute {
  if (!sessionsTerminalMode(hash)) return { project: "", terminal: "" }
  const project = valueInHash(hash, "project")?.trim() ?? ""
  const terminal = valueInHash(hash, "terminal")?.trim() ?? ""
  return project && terminal ? { project, terminal } : { project: "", terminal: "" }
}

export function sessionsPageHash(terminal = false, open?: TerminalRoute): string {
  if (!terminal) return "#page=sessions"
  let hash = "#page=sessions&mode=terminal"
  if (open?.project.trim() && open.terminal.trim()) {
    hash += "&project=" + encodeURIComponent(open.project.trim()) + "&terminal=" + encodeURIComponent(open.terminal.trim())
  }
  return hash
}
