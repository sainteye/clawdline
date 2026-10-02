/** Account capability changes for this signed-in browser only. The API fences writes by epoch. */
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

export async function changeTerminalPermission(
  apiOrigin: string, deviceID: string, enabled: boolean, fetcher: Fetch = fetch,
): Promise<TerminalPermissionDevice> {
  const device = await readTerminalPermissionDevice(apiOrigin, deviceID, fetcher)
  const had = device.caps.includes("terminal_control")
  if (had !== enabled) {
    const caps = enabled ? [...device.caps, "terminal_control"] : device.caps.filter((cap) => cap !== "terminal_control")
    const response = await fetcher(new URL(`/v1/devices/${encodeURIComponent(deviceID)}/capabilities`, apiOrigin).href, {
      method: "PATCH", credentials: "include", cache: "no-store",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ expected_capability_epoch: device.capability_epoch, caps }),
    })
    const result: unknown = await response.json().catch(() => null)
    if (!response.ok) {
      const code = result && typeof result === "object" && "error" in result &&
        typeof result.error === "object" && result.error && "code" in result.error ? String(result.error.code) : `http_${response.status}`
      throw new Error(code)
    }
  }
  // The read, rather than a local toggle, confirms that account authority changed.
  const verified = await readTerminalPermissionDevice(apiOrigin, deviceID, fetcher)
  if (verified.caps.includes("terminal_control") !== enabled) throw new Error("permission_unconfirmed")
  return verified
}
