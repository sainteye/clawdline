/*
 * Reading a Board or to-do reference picture, and how many at once.
 *
 * A card draws the machine's downscaled copy (`?size=thumb`, long edge at most
 * 480 px, a JPEG) and only the full-size viewer or the red pen reads the
 * original. The originals are about 1 MB each, and on a phone reading through
 * Clawdline Cloud every answer shares one 4 MiB reply channel on the machine:
 * a Board that asked for all of them at once filled it, the machine refused
 * the rest `cloud_read_busy`, and reads that had nothing to do with pictures
 * stalled behind them.
 *
 * So through Cloud a page has at most `CLOUD_IMAGE_LOADS` picture reads in
 * flight, and a read refused `cloud_read_busy` is tried again after 1 s, 2 s
 * and 4 s before the card shows the failure. Read from the machine itself the
 * daemon answers each one on its own connection and there is nothing to share,
 * so nothing is held back and nothing is retried: a local page behaves as it
 * did before, only asking for the smaller copy.
 *
 * Kept free of React and of relative imports so the Node suite can drive it
 * with its own fetch and clock (`reference-images.test.ts`).
 */
import { isRefusal, RefusalError } from "@clawdline/core/refusal"

export type ReferenceImageSize = "thumb" | "full"

/** Picture reads a page reading through Cloud keeps in flight at once. */
export const CLOUD_IMAGE_LOADS = 2

/**
 * The waits before each retry of a read the machine refused as busy. Its
 * refusal carries a `retry_after` of its own; the page does not wait that long
 * for a thumbnail, and three short tries are bounded where following the
 * machine's number would not be.
 */
export const CLOUD_BUSY_BACKOFF_MS: readonly number[] = [1000, 2000, 4000]

/** The console's route for one reference picture, the small copy or the original. */
export function referenceImageURL(id: string, size: ReferenceImageSize): string {
  const path = `/v1/work/v2/images/${encodeURIComponent(id)}`
  return size === "thumb" ? `${path}?size=thumb` : path
}

export interface ReferenceImageLoaderOptions {
  /** Whether this page reads through Cloud: only then are reads limited and retried. */
  limited: boolean
  /** Looked up per call by default, because Cloud installs its `fetch` after this module loads (`cloud/install.ts`). */
  fetch?: (url: string, init: RequestInit) => Promise<Response>
  sleep?: (ms: number, signal?: AbortSignal) => Promise<void>
  concurrency?: number
  backoff?: readonly number[]
}

export interface ReferenceImageLoader {
  /** The picture's bytes; rejects with the refusal, or with an AbortError once `signal` fires. */
  load(id: string, size: ReferenceImageSize, signal?: AbortSignal): Promise<Blob>
}

export function createReferenceImageLoader(options: ReferenceImageLoaderOptions): ReferenceImageLoader {
  const fetchImpl = options.fetch ?? ((url, init) => globalThis.fetch(url, init))
  const sleep = options.sleep ?? wait
  const limit = Math.max(1, options.concurrency ?? CLOUD_IMAGE_LOADS)
  const backoff = options.backoff ?? CLOUD_BUSY_BACKOFF_MS
  let active = 0
  const waiting: (() => void)[] = []

  // A card scrolled away or closed leaves the queue, so a long Board does not
  // spend its turns on pictures nobody is looking at any more.
  const acquire = (signal?: AbortSignal): Promise<void> => {
    if (signal?.aborted) return Promise.reject(aborted())
    if (active < limit) {
      active++
      return Promise.resolve()
    }
    return new Promise((resolve, reject) => {
      const wake = () => {
        signal?.removeEventListener("abort", drop)
        resolve()
      }
      const drop = () => {
        const at = waiting.indexOf(wake)
        if (at >= 0) waiting.splice(at, 1)
        reject(aborted())
      }
      waiting.push(wake)
      signal?.addEventListener("abort", drop, { once: true })
    })
  }
  // The slot passes straight to the next waiter, so `active` never dips and
  // lets a third read start between one finishing and the next being woken.
  const release = () => {
    const next = waiting.shift()
    if (next) next()
    else active--
  }

  const once = async (url: string, signal?: AbortSignal): Promise<Blob> => {
    const response = await fetchImpl(url, { credentials: "same-origin", signal })
    if (response.ok) return response.blob()
    throw await failureOf(response, url)
  }

  return {
    async load(id, size, signal) {
      const url = referenceImageURL(id, size)
      if (!options.limited) return once(url, signal)
      await acquire(signal)
      // The slot is held through the waits as well: a read backing off because
      // the channel is full is exactly when another one should not start.
      try {
        for (let attempt = 0; ; attempt++) {
          try {
            return await once(url, signal)
          } catch (error) {
            if (!isBusy(error) || attempt >= backoff.length) throw error
          }
          await sleep(backoff[attempt], signal)
          if (signal?.aborted) throw aborted()
        }
      } finally {
        release()
      }
    },
  }
}

/** Whether `error` is the machine saying its reply channel is full. */
export function isBusy(error: unknown): boolean {
  return error instanceof RefusalError && error.status === 429 && error.code === "cloud_read_busy"
}

/**
 * The refusal a failed read answered, in either spelling, so the card says the
 * code's own sentence (`shared.ts` `failureWords`); a body that is not one is
 * the plain status, as before.
 */
async function failureOf(response: Response, url: string): Promise<Error> {
  const body: unknown = await response.json().catch(() => null)
  if (isRefusal(body)) return new RefusalError(response.status, body, url)
  return new Error(`reference image answered ${response.status}`)
}

function aborted(): Error {
  return new DOMException("the picture is no longer shown", "AbortError")
}

function wait(ms: number, signal?: AbortSignal): Promise<void> {
  return new Promise((resolve) => {
    const timer = setTimeout(done, ms)
    signal?.addEventListener("abort", done, { once: true })
    function done() {
      clearTimeout(timer)
      signal?.removeEventListener("abort", done)
      resolve()
    }
  })
}

/**
 * The hosted console is the only one that reads through Cloud; the build says
 * which it is (`main.tsx`), never the hostname. `env` is absent under Node.
 */
function readsThroughCloud(): boolean {
  return !!(import.meta as { env?: Record<string, unknown> }).env?.VITE_HOSTED_CONSOLE
}

/** The page's one loader: the limit is per page, shared by the Board and the to-dos. */
export const referenceImages: ReferenceImageLoader = createReferenceImageLoader({ limited: readsThroughCloud() })
