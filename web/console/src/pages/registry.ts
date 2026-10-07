import type { PageModule } from "./types.js"

type Modules = Record<string, PageModule>

/** A built-in page or a module-backed page can be named in the address. */
export function pageReady(id: string, builtIn: boolean, modules: Modules): boolean {
  return builtIn || id in modules
}

/**
 * Drawer rows are navigation, not the page registry. A module may keep its
 * address while requiring entry through its owning page instead.
 */
export function drawerEntries<T extends { id: string }>(rows: readonly T[], modules: Modules): T[] {
  return rows.filter((row) => modules[row.id]?.drawer !== false)
}


/**
 * Whether a page is withheld because this machine is too old for it: only
 * when the page names a `requiresApiLevel` and the machine's level is *known*
 * and lower. An unknown level — a daemon from before `api_level` — is offered
 * the page, and the routes' own 501 says the rest; hiding on a guess would
 * take a working page away from a machine that has it.
 */
export function pageNeedsUpdate(id: string, modules: Modules, apiLevel: number | undefined): boolean {
  const requires = modules[id]?.requiresApiLevel
  if (typeof requires !== "number" || typeof apiLevel !== "number") return false
  return apiLevel < requires
}
