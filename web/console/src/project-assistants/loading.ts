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
 * Opening the memory panel: it is open at once and loading, then shows its
 * content; a failure closes it and goes to the chat's error line. Closing
 * while a read is in flight wins over that read, and only the latest opening
 * can settle the panel.
 */
export function memoryOpener(load: () => Promise<void>, to: {
  open: (open: boolean) => void; loading: (loading: boolean) => void; fail: (error: unknown) => void
}) {
  let current = 0
  return {
    open() {
      const mine = ++current
      to.open(true); to.loading(true)
      return load().then(() => { if (mine === current) to.loading(false) }, error => {
        if (mine !== current) return
        to.loading(false); to.open(false); to.fail(error)
      })
    },
    close() { current++; to.loading(false); to.open(false) },
  }
}
