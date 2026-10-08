import { client } from "../client.js"
import { isDifferentBuild } from "../build-freshness.js"
import { classifyApplyAnswer, classifyUpdateRead, createUpdateReadStore, type Answer, type ApplyAnswer, type UpdateRead } from "./update-model.js"

/**
 * `/v1/update` and `/v1/update/apply`, asked the way `read.ts` asks
 * `/v1/machine/usage`: through `client.url` and the page's `fetch`, which a
 * console reading a machine through Clawdline Cloud answers from the relay as
 * the `update` and `update-apply` words. Each request is spelled beside its
 * method on one line because `cloud/carry.test.ts` reads it there.
 *
 * Nothing here throws: a refusal, a missing route or a machine that did not
 * answer comes back sorted (`update-model.ts`), and the panel says what that
 * means rather than 「讀取失敗」.
 */
export function readUpdate(): Promise<UpdateRead> {
  return call("GET", "/v1/update").then(classifyUpdateRead)
}

/**
 * The press of 「立即更新」: the newest release of the machine's channel. The
 * key is the press's own, so a relay that retries the envelope starts one
 * update, not two.
 */
export function applyUpdate(): Promise<ApplyAnswer> {
  return call("POST", "/v1/update/apply", "{}").then(classifyApplyAnswer)
}

async function call(method: string, path: string, body?: string): Promise<Answer> {
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), 15_000)
  try {
    const headers: Record<string, string> = {}
    if (body !== undefined) {
      headers["Content-Type"] = "application/json"
      headers["Idempotency-Key"] = pressKey()
    }
    const res = await fetch(client.url(path), { method, credentials: "same-origin", signal: controller.signal, headers, body })
    const text = await res.text()
    let parsed: unknown = null
    try {
      parsed = text ? JSON.parse(text) : null
    } catch {
      parsed = null
    }
    return { transport: "answered", status: res.status, parsed }
  } catch {
    return { transport: "failed" }
  } finally {
    clearTimeout(timer)
  }
}

function pressKey(): string {
  try {
    return crypto.randomUUID()
  } catch {
    return "update-" + Date.now().toString(36) + "-" + Math.random().toString(36).slice(2)
  }
}

/*
 * One reading shared by the Settings panel and the session list's banner, so
 * the two never ask twice for the same answer. It reads while somebody is
 * listening and visible: every ten minutes on its own, and immediately after
 * a stale page becomes visible, focused, or online. The panel can also ask
 * sooner (`refreshUpdate`) while an update moves.
 */
const store = createUpdateReadStore(readUpdate, {
  now: () => Date.now(),
  visible: () => document.visibilityState !== "hidden",
  onVisibilityChange: (listener) => {
    document.addEventListener("visibilitychange", listener)
    return () => document.removeEventListener("visibilitychange", listener)
  },
  onFocus: (listener) => {
    window.addEventListener("focus", listener)
    return () => window.removeEventListener("focus", listener)
  },
  onOnline: (listener) => {
    window.addEventListener("online", listener)
    return () => window.removeEventListener("online", listener)
  },
  setInterval: (listener, ms) => setInterval(listener, ms),
  clearInterval: (timer) => clearInterval(timer),
})

/** Read now while visible; a read already on the wire is shared rather than doubled. */
export function refreshUpdate(): Promise<UpdateRead | null> {
  return store.refresh()
}

/** Hand a status the press answered with to every listener, as a read would. */
export function publishUpdateRead(next: UpdateRead): void {
  store.publish(next)
}

export function subscribeUpdate(listener: () => void): () => void {
  return store.subscribe(listener)
}

export function currentUpdateRead(): UpdateRead | null {
  return store.current()
}

/**
 * Whether the machine now serves a console this page is not: the index it
 * names against the files this page loaded (`build-freshness.ts`). An index
 * that could not be read is not an answer, so it is false.
 */
export async function servedConsoleChanged(): Promise<boolean> {
  let served: string
  try {
    const res = await fetch(document.baseURI, { cache: "reload", credentials: "include" })
    if (!res.ok) return false
    served = await res.text()
  } catch {
    return false
  }
  const running = [...document.querySelectorAll("script[src], link[href]")].map(
    (el) => el.getAttribute("src") || el.getAttribute("href") || "",
  )
  return isDifferentBuild(served, running)
}
