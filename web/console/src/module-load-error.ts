// Vite gives us the original error before the import promise rejects. Keep its
// identity, rather than classifying unrelated network/API failures as upgrades.
const preloadFailures = new WeakSet<object>()

export function isModuleLoadError(error: unknown): boolean {
  if (!error || typeof error !== "object") return false
  if (preloadFailures.has(error)) return true
  if (!("message" in error) || typeof error.message !== "string") return false
  return /^(Failed to fetch dynamically imported module:|Importing a module script failed\.?|error loading dynamically imported module:|Unable to preload CSS for )/u.test(error.message)
}

export function observeModulePreloadErrors(target: Pick<EventTarget, "addEventListener" | "removeEventListener">): () => void {
  const observe = (event: Event) => {
    const error = "payload" in event ? event.payload : null
    if (error && typeof error === "object") preloadFailures.add(error)
    // Do not preventDefault: React.lazy and the startup catch need the actual
    // rejection. Vite otherwise resolves the failed import as undefined.
  }
  target.addEventListener("vite:preloadError", observe)
  return () => target.removeEventListener("vite:preloadError", observe)
}
