// Adapted from Codync 9f67eb09084db61b3cca9c84a11bc03df9de29b7.
// Copyright 2026 Po Kai Lee and the Codync contributors. Apache-2.0.
// See third_party/codync/{LICENSE,NOTICE}.
import { useSyncExternalStore } from 'react'

/**
 * A media query read from one MediaQueryList and listened to on another: Chromium shares a list's
 * `matches` with its change detection, so a read while the window is hidden can swallow the `change`
 * and memoized canvases (avatars) keep the old appearance's ink. Showing or focusing the window
 * checks again, so a missed change never outlives the next look at the window.
 */
function mediaQuery(media: string) {
  // Clawdline also renders these components in its server-side checks.
  if(typeof window === "undefined")return {get:()=>false,subscribe:()=>()=>{}}
  const reads = window.matchMedia(media)
  const events = window.matchMedia(media)
  return {
    get: () => reads.matches,
    subscribe: (cb: () => void) => {
      events.addEventListener('change', cb)
      document.addEventListener('visibilitychange', cb)
      window.addEventListener('focus', cb)
      return () => {
        events.removeEventListener('change', cb)
        document.removeEventListener('visibilitychange', cb)
        window.removeEventListener('focus', cb)
      }
    },
  }
}

const darkQuery = mediaQuery('(prefers-color-scheme: dark)')
const motionQuery = mediaQuery('(prefers-reduced-motion: reduce)')

/**
 * The screen's pixel ratio, for canvases' backing size. It changes when the window moves to another
 * screen or the Mac sleeps with the lid closed; a canvas drawn meanwhile would stay blurry at 1x.
 * The query names the current ratio, so it is renewed on every change.
 */
const pixelRatio = {
  get: () => window.devicePixelRatio || 1,
  subscribe: (cb: () => void) => {
    let query: MediaQueryList | null = null
    const changed = () => {
      watch()
      cb()
    }
    const watch = () => {
      query?.removeEventListener('change', changed)
      query = window.matchMedia(`(resolution: ${pixelRatio.get()}dppx)`)
      query.addEventListener('change', changed)
    }
    watch()
    document.addEventListener('visibilitychange', changed)
    window.addEventListener('focus', changed)
    return () => {
      query?.removeEventListener('change', changed)
      document.removeEventListener('visibilitychange', changed)
      window.removeEventListener('focus', changed)
    }
  },
}

export function useDark() {
  return useSyncExternalStore(darkQuery.subscribe, darkQuery.get,()=>false)
}

export function useReduceMotion() {
  return useSyncExternalStore(motionQuery.subscribe, motionQuery.get,()=>true)
}

export function usePixelRatio() {
  return useSyncExternalStore(pixelRatio.subscribe, pixelRatio.get,()=>1)
}

