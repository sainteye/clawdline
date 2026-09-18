import { Diagnostics } from "../legacy/js/core/layout-diagnostics.js"
import * as L from "../legacy/bridge.js"
import { pushKey, pushSubscribe, pushTest, pushUnsubscribe } from "./api.js"

/**
 * Web Push: the phone buzzes when a session is waiting for an answer.
 *
 * This is `input/push.js` and the notification half of `input/settings.js`,
 * ported rather than copied — for the reason `legacy/settings-bridge.ts` gives:
 * those modules bind listeners to elements they look up at load, and here the
 * elements are React's. Every rule below is theirs; what changed is that the
 * drawing is a subscription instead of a write into `els`.
 *
 * **Four things can be true here and only one of them is "on", so the footer
 * says which.** A button that has been pressed and did nothing is the worst of
 * the four — that is what a permission the reader denied looks like from inside
 * the page, and the only cure for it is in the browser's own settings, which is
 * a sentence rather than a control.
 *
 * **On iOS this only works from the home screen.** Not "works badly" — the API
 * is absent in a Safari tab, so there is nothing to press and nothing to
 * explain afterwards. The one sentence that gets somebody from there to a
 * working notification is therefore the whole feature until they have read it,
 * and it is shown instead of a button rather than beside one.
 *
 * **And on this console there is a fifth thing that can be true**, which the
 * Swift app's page never had to say: a service worker needs a secure context,
 * and `http://127.0.0.1:7727` is one while `http://192.0.2.4:7727` is not. So
 * a browser that reached this daemon over the network answers `unsupported`
 * here — correctly, and for a reason nobody can fix from this page.
 * `docs/cross-platform.md` §4.6 constraint 1.
 *
 * The service worker is `/sw.js`, byte for byte the one the Swift app serves
 * (`web/console/public/sw.js`). It already knows how to draw a notification and
 * what to do when one is tapped; this end registers it and hands it a
 * subscription.
 */

const WORKER_READY_TIMEOUT_MS = 15_000
const PERMISSION_TIMEOUT_MS = 60_000
const PUSH_OPERATION_TIMEOUT_MS = 30_000

export type PushState = "unsupported" | "homescreen" | "blocked" | "off" | "on"

interface Shape {
  state: PushState
  busy: boolean
  /**
   * Whether the subscription has actually been looked up. Until it has,
   * `decide()` answers "off" because `subscribed` starts false — which is a
   * default and not a reading, and it is the one state that puts a button on
   * the screen.
   *
   * Only the footer waits on this. The settings block is drawn from `state`
   * either way, so a browser whose worker registers and then never activates —
   * the one case where nothing here ever settles — still has somewhere to turn
   * notifications on from, and the row along the bottom of the list is not
   * offering a button that could not have worked.
   */
  settled: boolean
  testing: boolean
  /** `#settings-notify-said`: what the last test said, or "". */
  said: string
  /** `say(words, calm)`'s second argument: `.said.calm` rather than `.said`. */
  saidCalm: boolean
}

let registration: ServiceWorkerRegistration | null = null
let subscribed = false
let started = false
/**
 * Whether this daemon owns the push routes at all.
 *
 * The original asks `typeof api.pushKey !== "function"`, which is how it tells
 * mock mode from a Mac. Here the call always exists, so the same question is
 * asked of the answer instead: a daemon that has not taken the route over
 * answers `not_implemented`, and that is `unsupported` rather than a failure to
 * apologise for.
 */
let served = true

let shape: Shape = {
  state: "unsupported",
  busy: false,
  settled: false,
  testing: false,
  said: "",
  saidCalm: false,
}

const listeners = new Set<() => void>()

function publish(next: Partial<Shape>): void {
  const merged = { ...shape, ...next }
  if (
    merged.state === shape.state &&
    merged.busy === shape.busy &&
    merged.settled === shape.settled &&
    merged.testing === shape.testing &&
    merged.said === shape.said &&
    merged.saidCalm === shape.saidCalm
  ) {
    return
  }
  shape = merged
  listeners.forEach((listener) => listener())
}

/** In the shape `useSyncExternalStore` takes; answers the unsubscribe. */
export function subscribePush(listener: () => void): () => void {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

export function pushShape(): Shape {
  return shape
}

/** The id the daemon gave this subscription, kept so it can be taken back after a reload. */
function remember(value: string | null): void {
  try {
    if (value == null) localStorage.removeItem("clawdline.push")
    else localStorage.setItem("clawdline.push", String(value))
  } catch {
    /* a private window has no storage, and this is not worth failing over */
  }
}

function recall(): string | null {
  try {
    return localStorage.getItem("clawdline.push")
  } catch {
    return null
  }
}

function standalone(): boolean {
  return (
    (navigator as unknown as { standalone?: boolean }).standalone === true ||
    (typeof window.matchMedia === "function" && window.matchMedia("(display-mode: standalone)").matches)
  )
}

/** iPadOS calls itself a Mac, and a touch screen is the only tell left. */
function iOS(): boolean {
  const platform = navigator.platform || ""
  return /iP(hone|ad|od)/.test(platform) || (/Mac/.test(platform) && navigator.maxTouchPoints > 1)
}

function decide(): PushState {
  if (iOS() && !standalone()) return "homescreen"
  if (!served) return "unsupported"
  if (!window.isSecureContext) return "unsupported"
  if (!("serviceWorker" in navigator) || !("PushManager" in window)) return "unsupported"
  if (typeof Notification === "undefined") return "unsupported"
  if (Notification.permission === "denied") return "blocked"
  return subscribed ? "on" : "off"
}

/**
 * The VAPID key arrives as base64url and `subscribe` wants bytes.
 *
 * Built on an `ArrayBuffer` named up front rather than on `new Uint8Array(n)`,
 * whose buffer TypeScript types as possibly shared — and a `SharedArrayBuffer`
 * is not a `BufferSource`. The bytes are the same either way.
 */
function keyBytes(key: string): Uint8Array<ArrayBuffer> {
  let padded = String(key).replace(/-/g, "+").replace(/_/g, "/")
  while (padded.length % 4) padded += "="
  const raw = atob(padded)
  const out = new Uint8Array(new ArrayBuffer(raw.length))
  for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i)
  return out
}

/** Safari answered this with a callback for years before it answered with a promise. */
function askPermission(): Promise<NotificationPermission> {
  return new Promise((done, fail) => {
    try {
      const maybe = Notification.requestPermission((answer) => done(answer))
      if (maybe && typeof maybe.then === "function") maybe.then(done, fail)
    } catch (e) {
      fail(e)
    }
  })
}

interface StagedError extends Error {
  stage: string
  code: string
}

function taggedError(error: unknown, stage: string): StagedError {
  const tagged = new Error(L.strings.webNotifyOnFailed) as StagedError
  tagged.stage = stage
  const named = error as { code?: unknown; name?: unknown } | null
  tagged.code =
    (typeof named?.code === "string" && named.code) ||
    (typeof named?.name === "string" && named.name) ||
    "push_failed"
  return tagged
}

/**
 * One named, finite boundary.
 *
 * The report keeps only the stage and typed outcome — never the endpoint, key
 * or subscription bytes — so a phone can say where it stopped without leaking
 * the capability it was trying to create.
 */
function timed<T>(stage: string, timeout: number, operation: () => Promise<T> | T): Promise<T> {
  Diagnostics.note("push." + stage + ".begin", {})
  return new Promise<T>((resolve, reject) => {
    let done = false
    const finish = (value: T | null, error: StagedError | null) => {
      if (done) return
      done = true
      clearTimeout(timer)
      if (error) reject(error)
      else resolve(value as T)
    }
    const timer = setTimeout(() => {
      const error = taggedError({ code: "push_timeout" }, stage)
      Diagnostics.note("push." + stage + ".failure", { code: error.code })
      finish(null, error)
    }, timeout)
    Promise.resolve()
      .then(operation)
      .then(
        (value) => {
          Diagnostics.note("push." + stage + ".end", {})
          finish(value, null)
        },
        (error) => {
          const tagged = taggedError(error, stage)
          Diagnostics.note("push." + stage + ".failure", { code: tagged.code })
          finish(null, tagged)
        },
      )
  })
}

/**
 * A registration promise can reject; `navigator.serviceWorker.ready` cannot.
 * The latter waits forever when boot's registration failed or the new worker
 * never activated, which used to leave a granted permission behind an equally
 * permanent "Asking…" button. Register at the moment the reader asks, use that
 * exact registration, and put a bound on activation.
 */
function ensureRegistration(): Promise<ServiceWorkerRegistration> {
  if (registration && registration.active) return Promise.resolve(registration)
  return timed("worker.register", WORKER_READY_TIMEOUT_MS, () =>
    navigator.serviceWorker.register("/sw.js", { updateViaCache: "none" }),
  ).then((r) => {
    registration = r
    if (r.active) return r
    const worker = r.installing || r.waiting
    if (!worker || typeof worker.addEventListener !== "function") {
      throw new Error(L.strings.webNotifyOnFailed)
    }
    return timed(
      "worker.activate",
      WORKER_READY_TIMEOUT_MS,
      () =>
        new Promise<ServiceWorkerRegistration>((resolve, reject) => {
          let settled = false
          const finish = (error: Error | null) => {
            if (settled) return
            settled = true
            worker.removeEventListener("statechange", changed)
            if (error) reject(error)
            else resolve(r)
          }
          const changed = () => {
            if (r.active || worker.state === "activated") finish(null)
            else if (worker.state === "redundant") finish(new Error(L.strings.webNotifyOnFailed))
          }
          worker.addEventListener("statechange", changed)
          changed()
        }),
    )
  })
}

function redraw(): void {
  publish({ state: shape.busy ? shape.state : decide() })
}

/** A `not_implemented` from the daemon means this console has no push to offer. */
function readServed(error: unknown): void {
  const code = (error as { code?: unknown } | null)?.code
  if (code === "not_implemented" || code === "not_found") served = false
}

function enable(): void {
  publish({ busy: true, said: "", saidCalm: false })
  timed("permission", PERMISSION_TIMEOUT_MS, askPermission)
    .then((answer) => {
      if (answer !== "granted") {
        publish({ busy: false })
        redraw()
        return null
      }
      return timed("worker.ready", WORKER_READY_TIMEOUT_MS, ensureRegistration)
        .then((r) => {
          registration = r
          return timed("key", PUSH_OPERATION_TIMEOUT_MS, () => pushKey())
        })
        .then((d) =>
          timed("browser.subscribe", PUSH_OPERATION_TIMEOUT_MS, () =>
            registration!.pushManager.subscribe({
              userVisibleOnly: true,
              applicationServerKey: keyBytes(d.key),
            }),
          ),
        )
        .then((subscription) =>
          timed("server.subscribe", PUSH_OPERATION_TIMEOUT_MS, () =>
            pushSubscribe(subscription.toJSON()),
          ),
        )
        .then((d) => {
          subscribed = true
          remember(d?.id ?? null)
          publish({ busy: false })
          redraw()
          return null
        })
    })
    .catch((e: StagedError) => {
      readServed(e)
      publish({ busy: false })
      redraw()
      Diagnostics.note("push.enable.failure", {
        stage: e?.stage || "enable",
        code: e?.code || "push_failed",
      })
      const detail = " [" + (e?.stage || "enable") + ": " + (e?.code || "push_failed") + "]"
      publish({ said: L.failureSentence(e, L.strings.webNotifyOnFailed) + detail })
    })
}

function disable(): void {
  publish({ busy: true, said: "", saidCalm: false })
  const id = recall()
  timed("worker.ready", WORKER_READY_TIMEOUT_MS, ensureRegistration)
    .then((r) => timed("browser.lookup", PUSH_OPERATION_TIMEOUT_MS, () => r.pushManager.getSubscription()))
    .then((subscription) => (subscription ? subscription.unsubscribe() : null))
    .then(() => {
      // The subscription is already gone from this browser, so notifications
      // stop here whatever the daemon says, and a daemon that never hears about
      // it drops it the first time it pushes to nothing. But a refusal is not a
      // success: it used to be swallowed here, so a daemon that could not be
      // told — two machines, one of them offline — looked exactly like one that
      // was. It is kept and said below.
      if (!id) return null
      return pushUnsubscribe(id).then(
        () => null,
        (e: unknown) => e || {},
      )
    })
    .then((untold) => {
      subscribed = false
      remember(null)
      publish({ busy: false })
      redraw()
      if (untold) {
        const code = (untold as { code?: unknown }).code
        Diagnostics.note("push.disable.untold", { code: (typeof code === "string" && code) || "push_failed" })
        publish({
          said: L.fillString(L.strings.webNotifyOffUntold, {
            why: L.failureSentence(untold, L.strings.webRequestFailed),
          }),
        })
      }
    })
    .catch((e: unknown) => {
      publish({ busy: false })
      redraw()
      publish({ said: L.failureSentence(e, L.strings.webNotifyOffFailed) })
    })
}

/** `Settings.test`: one notification to this device, and what it said. */
export function sendPushTest(sessionId: string | null): void {
  if (shape.testing) return
  publish({ testing: true, said: "", saidCalm: false })
  pushTest(sessionId)
    .then(() => {
      publish({ said: L.strings.webNotifyTestSent, saidCalm: true })
    })
    .catch((e: unknown) => {
      // A 409 is not a failure to apologise for: it means this browser believes
      // notifications are on and the daemon has nothing to send to. That is a
      // state with a one-sentence way out of it, and saying the sentence is
      // more use than an error toast with a number in it.
      const code = (e as { code?: unknown } | null)?.code
      publish({
        said:
          code === "not_subscribed"
            ? L.strings.webNotifyTestNone
            : L.failureSentence(e, L.strings.webNotifyTestFailed),
      })
    })
    .then(() => {
      publish({ testing: false })
      redraw()
    })
}

/**
 * Read this browser once, at boot.
 *
 * The first two answers are read off this browser rather than off a
 * subscription, so they are known now and there is nothing to wait for.
 */
export function startPush(): void {
  if (started) return
  started = true
  redraw()
  const first = decide()
  if (first === "unsupported" || first === "homescreen") {
    publish({ settled: true })
    redraw()
    return
  }
  timed("worker.ready", WORKER_READY_TIMEOUT_MS, ensureRegistration)
    .then((r) => timed("browser.lookup", PUSH_OPERATION_TIMEOUT_MS, () => r.pushManager.getSubscription()))
    .then((subscription) => {
      // Both halves have to agree. A subscription this browser still holds but
      // the daemon has forgotten — reinstalled, state directory cleared — would
      // draw as "on" and never arrive, so what is remembered here is the id the
      // daemon gave back.
      subscribed = !!subscription && !!recall()
      publish({ settled: true })
      redraw()
    })
    .catch(() => {
      // No worker means no notifications, and the footer already has a sentence
      // for it.
      publish({ settled: true })
      redraw()
    })
}

export function togglePush(): void {
  if (shape.busy) return
  if (shape.state === "off") enable()
  else if (shape.state === "on") disable()
}
