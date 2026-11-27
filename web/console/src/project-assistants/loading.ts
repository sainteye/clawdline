/** Bots wait the same 150ms as Sessions before a placeholder appears, so a fast read never flashes one. */
export const SkeletonDelayMs = 150

/**
 * Reports `true` once a wait has lasted `delay`, and `false` the moment it
 * ends. A wait that ends inside the delay is never reported as shown.
 */
export function showAfter(delay: number, onChange: (shown: boolean) => void) {
  let timer: ReturnType<typeof setTimeout> | undefined
  const stop = () => { if (timer !== undefined) clearTimeout(timer); timer = undefined }
  return {
    set(waiting: boolean) {
      stop()
      if (!waiting) { onChange(false); return }
      timer = setTimeout(() => { timer = undefined; onChange(true) }, delay)
    },
    stop,
  }
}

/**
 * An initial failure closes the memory view. A failed reread keeps its last
 * content visible. Only the latest read can report content or a failure.
 */
export function memoryOpener<T>(load: () => Promise<T>, to: {
  open: (open: boolean) => void; loading: (loading: boolean) => void
  loaded: (value: T) => void; fail: (error: unknown, opening: boolean) => void
}) {
  let current = 0
  let ready = false
  async function read(report: typeof to.fail) {
    const mine = ++current
    const opening = !ready
    if (opening) { to.open(true); to.loading(true) }
    let value: T
    try {
      value = await load()
    } catch (error) {
      if (mine !== current) return
      if (opening) { to.loading(false); to.open(false) }
      report(error, opening)
      return
    }
    if (mine !== current) return
    ready = true
    if (opening) to.loading(false)
    to.loaded(value)
  }
  return {
    open: () => read(to.fail),
    refresh: () => read(error => { throw error }),
    close() { current++; ready = false; to.loading(false); to.open(false) },
  }
}
