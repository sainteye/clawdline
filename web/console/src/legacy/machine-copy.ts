/**
 * The copied catalog still describes the retired macOS-only app. Adapt its
 * machine-generic sentences at the boundary where this console reads them.
 * The two platform labels are kept: callers use them only for a known Mac.
 */
export function machineWording(value: string, language: string): string {
  const tag = language.toLowerCase()
  if (tag.startsWith("zh")) {
    return value
      .replace(/Mac App 本身/g, "Clawdline 本機版本身")
      .replace(/Mac App/g, "Clawdline 本機版")
      .replace(/Mac/g, "機器")
      .replace(/([\u3400-\u9fff]) 機器/g, "$1機器")
      .replace(/機器 (?=[\u3400-\u9fff])/g, "機器")
  }
  if (tag.startsWith("en")) {
    return value
      .replace(/the Mac app/gi, "the Clawdline app")
      .replace(/Mac app/g, "Clawdline app")
      .replace(/Macs/g, "machines")
      .replace(/Mac's/g, "machine's")
      .replace(/Mac-wide/g, "Machine-wide")
      .replace(/\bMac\b/g, "machine")
      .replace(/(^|[.!?]\s+)machine\b/g, "$1Machine")
  }
  return value
}

export function adaptLegacyCatalog(catalog: Record<string, string>, language: string): void {
  for (const key of Object.keys(catalog)) {
    if (key === "webMachineMac" || key === "webMachineThisMac") continue
    catalog[key] = machineWording(catalog[key], language)
  }
  if (language.toLowerCase().startsWith("en")) {
    catalog.webPlanRowMacs = "Machines"
    catalog.webShowOnMac = "Show on this machine"
  }
}

export function adaptedWords<T extends object>(source: T, language: string): T {
  const result: Record<string, unknown> = {}
  for (const [key, value] of Object.entries(source)) {
    result[key] = typeof value === "string"
      ? machineWording(value, language)
      : value && typeof value === "object"
        ? adaptedWords(value, language)
        : value
  }
  return result as T
}
