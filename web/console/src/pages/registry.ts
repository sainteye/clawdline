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

