import { newClientID } from "./api.js"
import { carriedClientID, type TabCarryScope } from "./tab-carry.js"

/**
 * This browser tab's own id for leases (`client` in every control and input request). One per
 * tab: a reload or a navigation within the same tab keeps it (tab-carry.ts), so the page that
 * comes back holds the same lease; a new or duplicated tab is a new holder, and the lease a closed
 * tab released on `pagehide` (the local terminal does) is free for it.
 */
export const TAB = carriedClientID(typeof window === "undefined" ? undefined : window as unknown as TabCarryScope, newClientID)
