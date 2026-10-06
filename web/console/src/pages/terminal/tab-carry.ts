/**
 * The key under which a tab leaving by reload (or any navigation) hands its lease id to the next
 * page in the same tab. Nothing else is stored, and only for that hand-over.
 */
export const TAB_CARRY_KEY = "clawdline.terminal.tab"

export interface TabCarryScope {
  sessionStorage: { getItem(key: string): string | null; setItem(key: string, value: string): void; removeItem(key: string): void }
  addEventListener(type: "pagehide" | "pageshow", listener: (event: { persisted?: boolean }) => void): void
}

/**
 * This tab's lease id: the one the page before it in the same tab left on `pagehide`, else `fresh()`.
 *
 * A Cloud terminal cannot give its lease back as the page goes: every request is signed and sealed
 * with Web Crypto, which does not finish inside `pagehide`, and the relay socket dies with the page.
 * A reload therefore came back as another holder of its own lease ("另一個分頁") and had to take it
 * over. Carrying the id makes the reloaded page the same holder, so it takes control on entry as a
 * first open does. The id is taken out on load and written back only as the page leaves, so a tab
 * duplicated while this one is open (which copies session storage) never shares it.
 */
export function carriedClientID(scope: TabCarryScope | undefined, fresh: () => string): string {
  let id = ""
  try {
    id = scope?.sessionStorage.getItem(TAB_CARRY_KEY) ?? ""
    scope?.sessionStorage.removeItem(TAB_CARRY_KEY)
  } catch { /* storage refused: a fresh holder, as before */ }
  if (!/^tab-[0-9a-f]{24}$/.test(id)) id = fresh()
  try {
    scope?.addEventListener("pagehide", () => { try { scope.sessionStorage.setItem(TAB_CARRY_KEY, id) } catch { /* nothing to carry */ } })
    // Back from the back-forward cache: this page is live again and the id is its own once more.
    scope?.addEventListener("pageshow", (event) => {
      if (event.persisted) try { scope.sessionStorage.removeItem(TAB_CARRY_KEY) } catch { /* nothing carried */ }
    })
  } catch { /* no page events: a fresh holder next time */ }
  return id
}
