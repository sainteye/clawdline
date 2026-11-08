/*
 * How many conversations one filtered `/v1/squad/events` read may name, the
 * daemon's `store.MaxSquadEventConversations` (`squad.event_conversations` in
 * internal/domain/capacity, which checks this copy), and how long one ID may
 * be (`store.MaxSquadEventIDBytes`). The Session feed splits a longer list
 * into several reads; the relay reader refuses one before it is sealed.
 */
export const MAX_SQUAD_EVENT_CONVERSATIONS = 50
export const MAX_SQUAD_EVENT_ID_BYTES = 128
