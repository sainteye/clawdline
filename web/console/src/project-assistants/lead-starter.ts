/** What a new conversation with the lead Bot needs before it suggests a first message. */
export type LeadStarterState = {
  thread: boolean
  lead: boolean
  loaded: boolean
  writable: boolean
  messages: number
  earlier: boolean
  sending: boolean
  queued: number
  draft: string
}

/**
 * The first-message suggestion belongs only to an empty, writable lead
 * conversation whose box the person has not started typing in. Earlier
 * history that has not loaded yet means the conversation is not new.
 */
export function showLeadStarter(s: LeadStarterState): boolean {
  return !s.thread && s.lead && s.loaded && s.writable && s.messages === 0 && !s.earlier && !s.sending &&
    s.queued === 0 && s.draft.trim() === ""
}
