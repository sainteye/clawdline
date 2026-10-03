/** The hosted fetch adapter may wait for a relay receipt without observing AbortSignal. */
export async function fetchWithDeadline(path: string, init: RequestInit, timeoutMs: number): Promise<Response> {
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), timeoutMs)
  try {
    return await Promise.race([
      fetch(path, { ...init, signal: controller.signal }),
      new Promise<never>((_, reject) => controller.signal.addEventListener("abort", () => reject(new DOMException("Timed out", "AbortError")), { once: true })),
    ])
  } finally {
    clearTimeout(timer)
  }
}
