import type { BrowserCall } from "./browser-call"

type ControlStatus = {
  owner?: string
  mode?: string
  status: { tabs: { tab: number; conversation: string; browser_mode?: string; person: boolean }[] }
}

const refused = (code: string) => Object.assign(new Error(code), { code })

/** Drain changes of control, then return the human-controlled tabs for this panel's owner. */
export function createBrowserHandback(call: BrowserCall, conversation?: string) {
  const writes = new Set<Promise<unknown>>()
  let hadControlWrite = false
  let finished = false
  let active: Promise<void> | null = null
  const path = "/v1/browser/status" + (conversation ? "?conversation=" + encodeURIComponent(conversation) : "")
  const status = async () => {
    const value = await call<ControlStatus>("GET", path)
    if (!Array.isArray(value?.status?.tabs) || (!conversation && !value.owner && value.status.tabs.length)) throw refused("pa_invalid")
    return value
  }
  const controlled = (value: ControlStatus) => value.status.tabs.filter(tab =>
    tab.conversation === (conversation ?? value.owner) && tab.person === true,
  )

  return {
    closing: () => !!active,
    hadControlWrite: () => hadControlWrite,
    track<T>(method: "GET" | "POST", path: string, perform: () => Promise<T>): Promise<T> {
      if (active) return Promise.reject(refused("pa_stale_version"))
      const promise = perform()
      if (method === "POST" && (path === "/v1/browser/control" || path === "/v1/browser/open")) {
        hadControlWrite = true
        writes.add(promise)
        void promise.then(() => writes.delete(promise), () => writes.delete(promise))
      }
      return promise
    },
    exit(): Promise<void> {
      if (finished) return Promise.resolve()
      if (active) return active
      active = (async () => {
        await Promise.allSettled([...writes])
        const before = await status()
        const tabs = controlled(before)
        for (const tab of tabs) {
          const mode = tab.browser_mode || before.mode
          await call("POST", "/v1/browser/control", { tab: tab.tab, person: false, ...(mode ? { browser_mode: mode } : {}) })
        }
        if (tabs.length) {
          const after = await status()
          if (after.owner !== before.owner) throw refused("pa_stale_version")
          if (controlled(after).length) throw refused("browser_person_controlling")
        }
        finished = true
      })().catch(error => { active = null; throw error })
      return active
    },
  }
}
