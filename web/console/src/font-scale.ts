/**
 * This browser's text size. The console disables pinch zoom because it behaves
 * as an installed app on phones, so this is the deliberate, remembered way to
 * make its words larger or smaller without changing a machine-wide setting.
 */
export const FONT_SCALE_KEY = "clawdline.font-scale"
export const FONT_SCALE_STEPS = [80, 90, 100, 110, 120, 130, 140, 150] as const
export const DEFAULT_FONT_SCALE = 100

type StorageReader = Pick<Storage, "getItem">
type StorageWriter = Pick<Storage, "setItem">
type StyleTarget = { style: Pick<CSSStyleDeclaration, "setProperty"> }

export type FontScaleState = {
  percent: number
  storageAvailable: boolean
}

function browserStorage(): Storage | null {
  try {
    return typeof localStorage === "undefined" ? null : localStorage
  } catch {
    return null
  }
}

/** A stored value is one of the values the controls can actually reach. */
export function parseFontScale(raw: string | null): number {
  const value = Number(raw)
  return FONT_SCALE_STEPS.includes(value as (typeof FONT_SCALE_STEPS)[number]) ? value : DEFAULT_FONT_SCALE
}

/** Read once without turning an unavailable browser store into a broken page. */
export function readFontScale(storage: StorageReader | null = browserStorage()): FontScaleState {
  if (!storage) return { percent: DEFAULT_FONT_SCALE, storageAvailable: false }
  try {
    return { percent: parseFontScale(storage.getItem(FONT_SCALE_KEY)), storageAvailable: true }
  } catch {
    return { percent: DEFAULT_FONT_SCALE, storageAvailable: false }
  }
}

/** Apply immediately; the late stylesheet reads this percentage from the body. */
export function applyFontScale(
  percent: number,
  root: StyleTarget | null = typeof document === "undefined" ? null : document.documentElement,
): void {
  root?.style.setProperty("--clawdline-font-scale", `${parseFontScale(String(percent))}%`)
}

/** Save after applying. A private or restricted browser may refuse the write. */
export function rememberFontScale(percent: number, storage: StorageWriter | null = browserStorage()): boolean {
  if (!storage) return false
  try {
    storage.setItem(FONT_SCALE_KEY, String(parseFontScale(String(percent))))
    return true
  } catch {
    return false
  }
}

/** The next reachable value in the direction of a browser's minus or plus. */
export function stepFontScale(percent: number, direction: -1 | 1): number {
  const current = parseFontScale(String(percent))
  const index = FONT_SCALE_STEPS.indexOf(current as (typeof FONT_SCALE_STEPS)[number])
  return FONT_SCALE_STEPS[Math.max(0, Math.min(FONT_SCALE_STEPS.length - 1, index + direction))]
}

/** Called while the document is still hidden for its translated words. */
export function restoreFontScale(): FontScaleState {
  const state = readFontScale()
  applyFontScale(state.percent)
  return state
}
