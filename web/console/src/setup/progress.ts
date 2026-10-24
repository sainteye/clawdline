import type { CloudStatus, CloudViewer, MachineOnly } from "../pages/settings/cloud.js"
import { CLAWDLINE_APP_DEVICE_NAME } from "../legacy/js/net/cloud-onboarding.js"

/**
 * The things setup is finished by, in the order the wizard asks for them: the
 * macOS app's Cloud tab, a browser on this machine, and a phone, each paired
 * with this machine. A daemon on another platform has no app, so there setup
 * is the browser and the phone.
 *
 * Every one is read from this machine's own device list (`GET /v1/cloud/status`
 * → `devices`), so the count is what this machine accepts, not what the
 * wizard remembers having done. A device revoked later takes its item back.
 */
export type SetupItem = "app" | "browser" | "phone"

export const SETUP_ITEMS: readonly SetupItem[] = ["app", "browser", "phone"]

/** The items this machine's setup is made of: the app only where it runs. */
export function setupItems(hasApp: boolean): readonly SetupItem[] {
  return hasApp ? SETUP_ITEMS : SETUP_ITEMS.filter((item) => item !== "app")
}

/** How long the finished button stays in the header before it folds away. */
export const SETUP_FOLD_AFTER_SECONDS = 24 * 60 * 60

/**
 * Which item a device finishes, or null for one that finishes nothing.
 *
 * Only a device this machine pinned counts: a row that is only in the
 * account's roster was paired with another machine. A pin the account's
 * roster no longer lists has no kind, and is a leftover that can no longer
 * connect, so it finishes nothing either.
 *
 * The app is the device the wizard saw the app's own tab pair as
 * (`setup_app_device`). A Cloud tab registers its device once and keeps it,
 * so one registered before the tab named itself is "Web browser" for good;
 * the name the tab registers under now (`cloudViewerDeviceMetadata`) is the
 * fallback for an app paired without the wizard.
 */
export function deviceItem(device: CloudViewer, appDevice = ""): SetupItem | null {
  if (!device.pinned || device.revoked || !device.kind) return null
  if (device.kind === "ios" || device.kind === "android") return "phone"
  if (appDevice && device.id === appDevice) return "app"
  if ((device.name ?? "").startsWith(CLAWDLINE_APP_DEVICE_NAME)) return "app"
  return "browser"
}

export type SetupReading =
  /** This machine did not say. Unknown is never drawn as 0/3. */
  | { kind: "unknown" }
  | {
    kind: "known"
    cloud: boolean
    /** Paired, read from this machine's device list. */
    done: Record<SetupItem, boolean>
    /** Not paired, and the person chose to skip it (`setup_skipped`). */
    skipped: Record<SetupItem, boolean>
    items: readonly SetupItem[]
    /** Items settled: paired or skipped. */
    count: number
    /** The first item neither paired nor skipped; null when setup is finished. */
    next: SetupItem | null
  }

/** `setup_skipped` as the daemon stores it: items separated by commas. */
export function parseSkipped(list: string | null | undefined): SetupItem[] {
  return (list ?? "").split(",").filter((item): item is SetupItem => (SETUP_ITEMS as readonly string[]).includes(item))
}

export function readSetup(status: CloudStatus | MachineOnly | null | undefined, hasApp = true, appDevice = "",
  skippedItems: readonly SetupItem[] = []): SetupReading {
  if (!status || typeof status !== "object") return { kind: "unknown" }
  const cloud = status.enabled && status.configured
  if (cloud && status.pinned_readable === false) return { kind: "unknown" }
  const done = { app: false, browser: false, phone: false }
  if (cloud) {
    for (const device of status.devices ?? []) {
      const item = deviceItem(device, appDevice)
      if (item) done[item] = true
    }
  }
  const items = setupItems(hasApp)
  const skipped = { app: false, browser: false, phone: false }
  for (const item of skippedItems) skipped[item] = !done[item]
  const settled = (item: SetupItem) => done[item] || skipped[item]
  const count = items.filter(settled).length
  return { kind: "known", cloud, done, skipped, items, count, next: items.find((item) => !settled(item)) ?? null }
}

export type SetupChip =
  | { kind: "hidden" }
  /** The person chose this machine only: a quiet mark, never an unfinished count. */
  | { kind: "local" }
  | { kind: "progress"; count: number; total: number; next: SetupItem }
  | { kind: "done" }

/** What the header shows, from the reading and the two settings (`setup_mode`, `setup_completed_at`). */
export function setupChip(reading: SetupReading, mode: string | null | undefined,
  completedAt: number | null | undefined, nowSeconds: number): SetupChip {
  if (reading.kind === "unknown") return { kind: "hidden" }
  if (!reading.next) {
    const at = completedAt ?? 0
    return at > 0 && nowSeconds - at >= SETUP_FOLD_AFTER_SECONDS ? { kind: "hidden" } : { kind: "done" }
  }
  if (mode === "local") return { kind: "local" }
  return { kind: "progress", count: reading.count, total: reading.items.length, next: reading.next }
}

/**
 * The `setup_completed_at` to write, or null for no write: the first reading
 * with everything paired stamps the time, and a reading with something missing
 * clears it, so pairing again later starts a new day.
 */
export function completionChange(reading: SetupReading, completedAt: number | null | undefined,
  nowSeconds: number): number | null {
  if (reading.kind === "unknown") return null
  const at = completedAt ?? 0
  if (!reading.next) return at > 0 ? null : nowSeconds
  return at > 0 ? 0 : null
}
