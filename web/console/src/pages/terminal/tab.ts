import { newClientID } from "./api.js"

/**
 * This browser tab's own id for leases (`client` in every control and input
 * request). One per tab and never stored: a reopened tab is a new holder, and
 * the lease its predecessor released on `pagehide` is free for it.
 */
export const TAB = newClientID()
