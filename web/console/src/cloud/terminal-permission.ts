/** Read this signed-in browser's existing Cloud account capabilities. */
export type TerminalPermissionDevice = {
  id: string
  caps: string[]
  capability_epoch: number
  revoked_at: string | null
}

type Fetch = (url: string, init?: RequestInit) => Promise<Response>

export async function readTerminalPermissionDevice(apiOrigin: string, deviceID: string, fetcher: Fetch = fetch): Promise<TerminalPermissionDevice> {
  const response = await fetcher(new URL("/v1/devices", apiOrigin).href, { credentials: "include", cache: "no-store" })
  const body: unknown = await response.json().catch(() => null)
  if (!response.ok || !body || typeof body !== "object" || !Array.isArray((body as { devices?: unknown }).devices)) {
    throw new Error("devices_unavailable")
  }
  const row = (body as { devices: unknown[] }).devices.find((value) =>
    value && typeof value === "object" && (value as { id?: unknown }).id === deviceID)
  if (!row || typeof row !== "object") throw new Error("device_unavailable")
  const device = row as Record<string, unknown>
  if (!Array.isArray(device.caps) || !device.caps.every((cap) => typeof cap === "string") ||
      !Number.isSafeInteger(device.capability_epoch) || Number(device.capability_epoch) < 1 || device.revoked_at !== null) {
    throw new Error("device_unavailable")
  }
  return device as TerminalPermissionDevice
}
